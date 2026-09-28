// Package data holds the static EOL tables, embedded at build time and
// optionally refreshed from endoflife.date when --online is set.
package data

import (
	"embed"
	"fmt"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

//go:embed kubernetes.yaml kernel.yaml os.yaml k8s-deprecations.yaml
var files embed.FS

const DateLayout = "2006-01-02"

type Kubernetes struct {
	Generated string       `json:"generated"`
	Latest    string       `json:"latest"`
	Versions  []K8sVersion `json:"versions"`
}

type K8sVersion struct {
	Minor    string `json:"minor"`
	Released string `json:"released"`
	EOL      string `json:"eol"`
}

type Kernel struct {
	Generated string          `json:"generated"`
	Kernels   []KernelVersion `json:"kernels"`
}

type KernelVersion struct {
	Version string `json:"version"`
	EOL     string `json:"eol"`
}

type OS struct {
	Generated string   `json:"generated"`
	Distros   []Distro `json:"distros"`
}

// Distro is matched by substring against node osImage; longest match wins.
// Empty EOL means rolling release, never flagged.
type Distro struct {
	Match   string `json:"match"`
	Name    string `json:"name"`
	Version string `json:"version"`
	EOL     string `json:"eol,omitempty"`
}

type Deprecations struct {
	Versions []Deprecation `json:"deprecated-versions"`
}

type Deprecation struct {
	Version        string `json:"version"`
	Kind           string `json:"kind"`
	DeprecatedIn   string `json:"deprecated-in"`
	RemovedIn      string `json:"removed-in"`
	ReplacementAPI string `json:"replacement-api"`
	Component      string `json:"component"`
}

type Tables struct {
	Kubernetes   Kubernetes
	Kernel       Kernel
	OS           OS
	Deprecations Deprecations
	// Source is "embedded" or "endoflife.date", shown in the report.
	Source string
}

func Embedded() (*Tables, error) {
	t := &Tables{Source: "embedded"}
	for name, dst := range map[string]any{
		"kubernetes.yaml":       &t.Kubernetes,
		"kernel.yaml":           &t.Kernel,
		"os.yaml":               &t.OS,
		"k8s-deprecations.yaml": &t.Deprecations,
	} {
		b, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(b, dst); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	return t, nil
}

// Generated returns the oldest generated date across the EOL tables.
func (t *Tables) Generated() string {
	oldest := t.Kubernetes.Generated
	for _, g := range []string{t.Kernel.Generated, t.OS.Generated} {
		if g < oldest {
			oldest = g
		}
	}
	return oldest
}

func ParseDate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	d, err := time.Parse(DateLayout, s)
	return d, err == nil
}

func (k Kubernetes) Find(minor string) (K8sVersion, bool) {
	for _, v := range k.Versions {
		if v.Minor == minor {
			return v, true
		}
	}
	return K8sVersion{}, false
}

func (k Kernel) Find(majorMinor string) (KernelVersion, bool) {
	for _, v := range k.Kernels {
		if v.Version == majorMinor {
			return v, true
		}
	}
	return KernelVersion{}, false
}

func (o OS) Match(osImage string) (Distro, bool) {
	var best Distro
	found := false
	for _, d := range o.Distros {
		if strings.Contains(osImage, d.Match) && len(d.Match) > len(best.Match) {
			best, found = d, true
		}
	}
	return best, found
}

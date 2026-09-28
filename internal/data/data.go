// Package data holds the lookup tables. EOL data is always fetched from
// endoflife.date at startup; only the Pluto deprecation table and the chart
// repo mapping are embedded because they have no live source.
package data

import (
	"embed"
	"fmt"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

//go:embed k8s-deprecations.yaml charts.yaml
var files embed.FS

const DateLayout = "2006-01-02"

type Kubernetes struct {
	Latest   string
	Versions []K8sVersion
}

type K8sVersion struct {
	Minor    string
	Released string
	EOL      string
}

type Kernel struct {
	Kernels []KernelVersion
}

type KernelVersion struct {
	Version string
	EOL     string
}

type OS struct {
	Distros []Distro
}

// Distro is matched by substring against node osImage; longest match wins.
// Empty EOL means rolling release, never flagged.
type Distro struct {
	Match   string
	Name    string
	Version string
	EOL     string
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

// Charts maps chart names to their Helm repo, needed because a release
// secret does not record where the chart came from.
type Charts struct {
	Generated string  `json:"generated"`
	Charts    []Chart `json:"charts"`
}

type Chart struct {
	Name string `json:"name"`
	Repo string `json:"repo"`
}

func (c Charts) Repo(name string) (string, bool) {
	for _, ch := range c.Charts {
		if ch.Name == name {
			return ch.Repo, true
		}
	}
	return "", false
}

type Tables struct {
	Kubernetes   Kubernetes
	Kernel       Kernel
	OS           OS
	Deprecations Deprecations
	Charts       Charts
}

// embedded loads the static tables; Load adds the live EOL data.
func embedded() (*Tables, error) {
	t := &Tables{}
	for name, dst := range map[string]any{
		"k8s-deprecations.yaml": &t.Deprecations,
		"charts.yaml":           &t.Charts,
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

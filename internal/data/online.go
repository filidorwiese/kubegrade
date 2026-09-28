package data

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const eolBase = "https://endoflife.date/api/"

// cycle mirrors the endoflife.date product JSON. eol and lts are either
// a string date or a bool, hence RawMessage.
type cycle struct {
	Cycle       string          `json:"cycle"`
	Codename    string          `json:"codename"`
	ReleaseDate string          `json:"releaseDate"`
	EOL         json.RawMessage `json:"eol"`
	LTS         json.RawMessage `json:"lts"`
}

func (c cycle) eolDate() string {
	var s string
	if json.Unmarshal(c.EOL, &s) == nil {
		return s
	}
	return ""
}

func (c cycle) isLTS() bool {
	var b bool
	if json.Unmarshal(c.LTS, &b) == nil {
		return b
	}
	var s string
	return json.Unmarshal(c.LTS, &s) == nil && s != ""
}

// Online fetches the EOL tables from endoflife.date. Deprecations always
// come from the embedded Pluto table. Any fetch error fails the whole call.
func Online(ctx context.Context) (*Tables, error) {
	emb, err := Embedded()
	if err != nil {
		return nil, err
	}
	today := time.Now().UTC().Format(DateLayout)
	t := &Tables{Deprecations: emb.Deprecations, Source: "endoflife.date"}

	k8s, err := fetch(ctx, "kubernetes")
	if err != nil {
		return nil, err
	}
	t.Kubernetes = buildKubernetes(k8s, today)

	linux, err := fetch(ctx, "linux")
	if err != nil {
		return nil, err
	}
	t.Kernel = buildKernel(linux, today)

	t.OS = OS{Generated: today}
	for _, p := range osProducts {
		cycles, err := fetch(ctx, p.product)
		if err != nil {
			return nil, err
		}
		t.OS.Distros = append(t.OS.Distros, p.build(cycles)...)
	}
	t.OS.Distros = append(t.OS.Distros, rollingDistros...)
	return t, nil
}

func fetch(ctx context.Context, product string) ([]cycle, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, eolBase+product+".json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("endoflife.date %s: %w", product, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("endoflife.date %s: HTTP %d", product, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var cycles []cycle
	if err := json.Unmarshal(body, &cycles); err != nil {
		return nil, fmt.Errorf("endoflife.date %s: %w", product, err)
	}
	return cycles, nil
}

func buildKubernetes(cycles []cycle, today string) Kubernetes {
	k := Kubernetes{Generated: today}
	for _, c := range cycles {
		if !strings.HasPrefix(c.Cycle, "1.") {
			continue
		}
		k.Versions = append(k.Versions, K8sVersion{Minor: c.Cycle, Released: c.ReleaseDate, EOL: c.eolDate()})
	}
	sort.Slice(k.Versions, func(i, j int) bool { return MinorLess(k.Versions[i].Minor, k.Versions[j].Minor) })
	if n := len(k.Versions); n > 0 {
		k.Latest = k.Versions[n-1].Minor
	}
	// Keep the table short: oldest rows are noise once well past EOL.
	if len(k.Versions) > 8 {
		k.Versions = k.Versions[len(k.Versions)-8:]
	}
	return k
}

func buildKernel(cycles []cycle, today string) Kernel {
	k := Kernel{Generated: today}
	for _, c := range cycles {
		if c.isLTS() && c.eolDate() != "" {
			k.Kernels = append(k.Kernels, KernelVersion{Version: c.Cycle, EOL: c.eolDate()})
		}
	}
	return k
}

type osProduct struct {
	product string
	build   func([]cycle) []Distro
}

var osProducts = []osProduct{
	{"ubuntu", func(cs []cycle) []Distro {
		return distros(cs, "ubuntu", func(c cycle) string { return "Ubuntu " + c.Cycle })
	}},
	{"debian", func(cs []cycle) []Distro {
		return distros(cs, "debian", func(c cycle) string { return "Debian GNU/Linux " + c.Cycle })
	}},
	{"amazon-linux", func(cs []cycle) []Distro {
		return distros(cs, "amazon-linux", func(c cycle) string { return "Amazon Linux " + c.Cycle })
	}},
}

// Flatcar publishes no EOL per version; matched so it is not "unknown OS".
var rollingDistros = []Distro{{Match: "Flatcar", Name: "flatcar", Version: "rolling"}}

func distros(cs []cycle, name string, match func(cycle) string) []Distro {
	var out []Distro
	for _, c := range cs {
		if eol := c.eolDate(); eol != "" {
			out = append(out, Distro{Match: match(c), Name: name, Version: c.Cycle, EOL: eol})
		}
	}
	return out
}

// MinorLess orders "1.9" before "1.10".
func MinorLess(a, b string) bool {
	var amaj, amin, bmaj, bmin int
	fmt.Sscanf(a, "%d.%d", &amaj, &amin)
	fmt.Sscanf(b, "%d.%d", &bmaj, &bmin)
	if amaj != bmaj {
		return amaj < bmaj
	}
	return amin < bmin
}

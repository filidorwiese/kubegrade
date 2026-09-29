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

	"github.com/filidorwiese/kubegrade/internal/progress"
)

var eolBase = "https://endoflife.date/api/"

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

// staleAfter is how old the built-in tables may be before the fallback
// note asks for an update; a Kubernetes minor lands about every 4 months.
const staleAfter = 90 * 24 * time.Hour

// Load builds the EOL tables from endoflife.date, falling back per product
// to the copy embedded at build time. The note is non-empty when any
// fallback was used and says how old that copy is.
func Load(ctx context.Context, report progress.Func) (*Tables, string, error) {
	t, err := embedded()
	if err != nil {
		return nil, "", err
	}
	const phase = "fetching EOL tables"
	total := 2 + len(osProducts)
	step := 0
	report(phase, step, total)

	var fallback []string
	load := func(product string) ([]cycle, error) {
		cycles, err := fetch(ctx, product)
		if err != nil {
			fallback = append(fallback, product)
			cycles, err = builtin(product)
		}
		step++
		report(phase, step, total)
		return cycles, err
	}

	k8s, err := load("kubernetes")
	if err != nil {
		return nil, "", err
	}
	t.Kubernetes = buildKubernetes(k8s)

	linux, err := load("linux")
	if err != nil {
		return nil, "", err
	}
	t.Kernel = buildKernel(linux)

	t.OS = OS{}
	for _, p := range osProducts {
		cycles, err := load(p.product)
		if err != nil {
			return nil, "", err
		}
		t.OS.Distros = append(t.OS.Distros, p.build(cycles)...)
	}
	t.OS.Distros = append(t.OS.Distros, rollingDistros...)

	note := ""
	if len(fallback) > 0 {
		note = "endoflife.date unreachable for " + strings.Join(fallback, ", ") + ", using built-in tables from " + builtinDate()
		if d, ok := ParseDate(builtinDate()); ok && time.Since(d) > staleAfter {
			note += " (stale, update kubegrade)"
		}
	}
	return t, note, nil
}

// fetch downloads one product and rejects payloads that parse but carry
// nothing usable, so a silent schema change also falls back.
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
	return parseCycles(product, body)
}

func builtin(product string) ([]cycle, error) {
	body, err := files.ReadFile("eol/" + product + ".json")
	if err != nil {
		return nil, err
	}
	return parseCycles(product, body)
}

func builtinDate() string {
	b, _ := files.ReadFile("eol/DATE")
	return strings.TrimSpace(string(b))
}

func parseCycles(product string, body []byte) ([]cycle, error) {
	var cycles []cycle
	if err := json.Unmarshal(body, &cycles); err != nil {
		return nil, fmt.Errorf("endoflife.date %s: %w", product, err)
	}
	usable := 0
	for _, c := range cycles {
		if c.Cycle != "" && c.eolDate() != "" {
			usable++
		}
	}
	if usable < 3 {
		return nil, fmt.Errorf("endoflife.date %s: unexpected payload", product)
	}
	return cycles, nil
}

func buildKubernetes(cycles []cycle) Kubernetes {
	var k Kubernetes
	for _, c := range cycles {
		if !strings.HasPrefix(c.Cycle, "1.") {
			continue
		}
		k.Versions = append(k.Versions, K8sVersion{Minor: c.Cycle, EOL: c.eolDate()})
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

func buildKernel(cycles []cycle) Kernel {
	var k Kernel
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

// Match strings follow each distro's PRETTY_NAME, which is what the kubelet
// reports as osImage, e.g. "Rocky Linux 9.4 (Blue Onyx)".
var osProducts = []osProduct{
	{"ubuntu", prefix("ubuntu", "Ubuntu ")},
	{"debian", prefix("debian", "Debian GNU/Linux ")},
	{"amazon-linux", prefix("amazon-linux", "Amazon Linux ")},
	{"rhel", prefix("rhel", "Red Hat Enterprise Linux ")},
	{"rocky-linux", prefix("rocky", "Rocky Linux ")},
	{"almalinux", prefix("alma", "AlmaLinux ")},
	{"alpine-linux", prefix("alpine", "Alpine Linux v")},
	{"fedora", prefix("fedora", "Fedora Linux ", "Fedora CoreOS ")},
	{"opensuse", prefix("opensuse", "openSUSE Leap ")},
	{"sles", func(cs []cycle) []Distro {
		// SLES names service packs "15 SP6" for cycle "15.6".
		return distros(cs, "sles", func(c cycle) string {
			if maj, sp, ok := strings.Cut(c.Cycle, "."); ok && sp != "0" {
				return "SUSE Linux Enterprise Server " + maj + " SP" + sp
			}
			return "SUSE Linux Enterprise Server " + c.Cycle
		})
	}},
}

// Products is every endoflife.date product the tables use; the refresh
// tool vendors exactly this list.
var Products = func() []string {
	out := []string{"kubernetes", "linux"}
	for _, p := range osProducts {
		out = append(out, p.product)
	}
	return out
}()

// Rolling distros publish no EOL per version; matched so they are not
// reported as unknown.
var rollingDistros = []Distro{
	{Match: "Flatcar", Name: "flatcar", Version: "rolling"},
	{Match: "Talos", Name: "talos", Version: "rolling"},
	{Match: "Bottlerocket", Name: "bottlerocket", Version: "rolling"},
}

// prefix builds distros whose match is one of the prefixes plus the cycle.
func prefix(name string, prefixes ...string) func([]cycle) []Distro {
	return func(cs []cycle) []Distro {
		var out []Distro
		for _, p := range prefixes {
			out = append(out, distros(cs, name, func(c cycle) string { return p + c.Cycle })...)
		}
		return out
	}
}

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

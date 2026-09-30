package collect

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// KernelUpdate is a newer kernel the node's distro ships for the series and
// flavour the node runs. Have and Latest are normalised for comparison:
// "6.12.107" on Debian, "6.8.0-142" on Ubuntu.
type KernelUpdate struct {
	Have   string `json:"have"`
	Latest string `json:"latest"`
	Source string `json:"source"` // e.g. "trixie-security", "noble security"
}

const (
	debianMadison = "https://qa.debian.org/madison.php"
	launchpad     = "https://api.launchpad.net/1.0/ubuntu"
)

// kernelUpdates asks each supported distro's package feed for the newest
// kernel of the node's series. One fetch per distinct feed query; nodes
// on other distros or with unparseable kernels are skipped.
func (c *Collector) kernelUpdates(ctx context.Context, s *Snapshot) error {
	s.KernelLatest = map[string]KernelUpdate{}
	const phase = "resolving kernel updates"
	c.report(phase, 0, len(s.Nodes))
	feed := newFeedCache(ctx)
	for i, n := range s.Nodes {
		info := n.Status.NodeInfo
		var up *KernelUpdate
		var err error
		switch {
		case strings.HasPrefix(info.OSImage, "Debian"):
			up, err = debianKernel(feed, info)
		case strings.HasPrefix(info.OSImage, "Ubuntu"):
			up, err = ubuntuKernel(feed, info)
		}
		if err != nil {
			s.Errors = append(s.Errors, "kernel feed "+n.Name+": "+err.Error())
		}
		if up != nil && lessVersion(up.Have, up.Latest) {
			s.KernelLatest[n.Name] = *up
		}
		c.report(phase, i+1, len(s.Nodes))
	}
	return nil
}

type feedResult struct {
	body []byte
	err  error
}

// feedCache fetches each URL once per scan; a cluster of identical nodes
// asks the same question many times.
type feedCache struct {
	ctx   context.Context
	fetch func(ctx context.Context, u string) ([]byte, error)
	got   map[string]feedResult
}

func newFeedCache(ctx context.Context) *feedCache {
	return &feedCache{ctx: ctx, got: map[string]feedResult{},
		fetch: func(ctx context.Context, u string) ([]byte, error) { return fetch(ctx, u, "") }}
}

func (f *feedCache) get(u string) ([]byte, error) {
	if r, ok := f.got[u]; ok {
		return r.body, r.err
	}
	body, err := f.fetch(f.ctx, u)
	f.got[u] = feedResult{body, err}
	return body, err
}

// --- Debian ---

var (
	debianCodename = regexp.MustCompile(`\(([a-z]+)\)`)
	debianKernelRE = regexp.MustCompile(`^(\d+\.\d+\.\d+)`)
	madisonLine    = regexp.MustCompile(`^\s*\S+\s*\|\s*(\S+)\s*\|\s*(\S+)\s*\|`)
)

// debianKernel reads madison for the linux-image metapackage: one line per
// suite, version like "6.12.111-1". Debian 12 and older report an ABI
// kernel ("6.1.0-37-amd64") whose patch level is not comparable.
func debianKernel(feed *feedCache, info corev1.NodeSystemInfo) (*KernelUpdate, error) {
	m := debianCodename.FindStringSubmatch(info.OSImage)
	have := debianKernelRE.FindString(info.KernelVersion)
	if m == nil || have == "" || strings.HasSuffix(have, ".0") {
		return nil, nil
	}
	codename := m[1]
	body, err := feed.get(debianMadison + "?package=linux-image-" + info.Architecture + "&table=debian&text=on")
	if err != nil {
		return nil, err
	}
	best := &KernelUpdate{Have: have}
	for _, line := range strings.Split(string(body), "\n") {
		lm := madisonLine.FindStringSubmatch(line)
		if lm == nil {
			continue
		}
		version, suite := lm[1], lm[2]
		if suite != codename && suite != codename+"-security" {
			continue
		}
		v := debianKernelRE.FindString(version)
		if v != "" && lessVersion(best.Latest, v) {
			best.Latest, best.Source = v, suite
		}
	}
	if best.Latest == "" {
		return nil, nil
	}
	return best, nil
}

// --- Ubuntu ---

var (
	ubuntuRelease  = regexp.MustCompile(`Ubuntu (\d+\.\d+)`)
	ubuntuKernelRE = regexp.MustCompile(`^(\d+\.\d+\.\d+-\d+)-([a-z0-9-]+)$`)
	ubuntuPkgRE    = regexp.MustCompile(`^\d+\.\d+\.\d+[.-]\d+`)
)

type lpSeries struct {
	Entries []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"entries"`
}

type lpBinaries struct {
	Entries []struct {
		Version string `json:"binary_package_version"`
		Pocket  string `json:"pocket"`
		Arch    string `json:"distro_arch_series_link"`
	} `json:"entries"`
}

// ubuntuKernel reads Launchpad for the linux-image-<flavour> metapackage
// of the node's series. When its kernel series differs from the running
// one the node is on an HWE kernel, tracked by a -hwe-<release> package.
func ubuntuKernel(feed *feedCache, info corev1.NodeSystemInfo) (*KernelUpdate, error) {
	rm := ubuntuRelease.FindStringSubmatch(info.OSImage)
	km := ubuntuKernelRE.FindStringSubmatch(info.KernelVersion)
	if rm == nil || km == nil {
		return nil, nil
	}
	release, have, flavour := rm[1], km[1], km[2]
	body, err := feed.get(launchpad + "/series")
	if err != nil {
		return nil, err
	}
	var series lpSeries
	if err := unmarshal(body, &series); err != nil {
		return nil, err
	}
	name := ""
	for _, e := range series.Entries {
		if e.Version == release {
			name = e.Name
		}
	}
	if name == "" {
		return nil, nil
	}
	for _, pkg := range []string{"linux-image-" + flavour, "linux-image-" + flavour + "-hwe-" + release} {
		up, err := ubuntuPackage(feed, pkg, name, info.Architecture)
		if err != nil {
			return nil, err
		}
		if up != nil && sameSeries(up.Latest, have) {
			up.Have = have
			return up, nil
		}
	}
	return nil, nil
}

func ubuntuPackage(feed *feedCache, pkg, series, arch string) (*KernelUpdate, error) {
	body, err := feed.get(launchpad + "/+archive/primary?ws.op=getPublishedBinaries&exact_match=true&status=Published&binary_name=" +
		pkg + "&distro_series=" + launchpad + "/" + series)
	if err != nil {
		return nil, err
	}
	var bins lpBinaries
	if err := unmarshal(body, &bins); err != nil {
		return nil, err
	}
	var best *KernelUpdate
	for _, e := range bins.Entries {
		if e.Pocket == "Proposed" || !strings.HasSuffix(e.Arch, "/"+arch) {
			continue
		}
		v := ubuntuPkgRE.FindString(e.Version)
		if v == "" || best != nil && !lessVersion(best.Latest, v) {
			continue
		}
		best = &KernelUpdate{Latest: v, Source: series + " " + strings.ToLower(e.Pocket)}
	}
	return best, nil
}

// sameSeries is true when both versions share major.minor.
func sameSeries(a, b string) bool {
	na, nb := VersionNums(a), VersionNums(b)
	return len(na) >= 2 && len(nb) >= 2 && na[0] == nb[0] && na[1] == nb[1]
}

// VersionNums splits "6.8.0-142" into its numbers; anything after the
// first character that is neither digit nor separator is ignored.
func VersionNums(v string) []int {
	var out []int
	num := ""
	flush := func() {
		if num != "" {
			n, _ := strconv.Atoi(num)
			out = append(out, n)
			num = ""
		}
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9':
			num += string(r)
		case r == '.' || r == '-':
			flush()
		default:
			flush()
			return out
		}
	}
	flush()
	return out
}

// lessVersion compares number by number; a missing trailing number counts
// as zero. Empty is less than anything.
func lessVersion(a, b string) bool {
	if a == "" {
		return b != ""
	}
	na, nb := VersionNums(a), VersionNums(b)
	for i := 0; i < max(len(na), len(nb)); i++ {
		x, y := 0, 0
		if i < len(na) {
			x = na[i]
		}
		if i < len(nb) {
			y = nb[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

// fetch GETs a URL with a short timeout and a bounded body.
func fetch(ctx context.Context, u, accept string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, errRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

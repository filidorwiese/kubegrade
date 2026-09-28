package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/filidorwiese/kubegrade/internal/data"
)

const artifactHub = "https://artifacthub.io/api/v1"

// chartUpstream resolves the newest upstream version for every distinct
// chart in use. charts.yaml wins; otherwise Artifact Hub is searched.
func (c *Collector) chartUpstream(ctx context.Context, s *Snapshot) error {
	s.ChartLatest = map[string]ChartUpstream{}
	indexes := map[string]*repoIndex{}
	done := map[string]bool{}
	for _, r := range s.HelmReleases {
		if done[r.Chart] {
			continue
		}
		done[r.Chart] = true

		if repo, ok := c.tables.Charts.Repo(r.Chart); ok {
			idx, seen := indexes[repo]
			if !seen {
				var err error
				if idx, err = fetchIndex(ctx, repo); err != nil {
					s.Errors = append(s.Errors, "chart repo "+repo+": "+err.Error())
				}
				indexes[repo] = idx
			}
			if idx != nil {
				if up, ok := newestStable(idx, r.Chart); ok {
					up.Repo, up.Via = repo, "charts.yaml"
					s.ChartLatest[r.Chart] = up
				}
			}
			continue
		}

		up, err := resolveArtifactHub(ctx, r)
		if err != nil {
			s.Errors = append(s.Errors, "artifacthub "+r.Chart+": "+err.Error())
			continue
		}
		if up != nil {
			s.ChartLatest[r.Chart] = *up
		}
	}
	return nil
}

// --- Artifact Hub ---

type ahSearch struct {
	Packages []ahPackage `json:"packages"`
}

type ahPackage struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Stars      int    `json:"stars"`
	Official   bool   `json:"official"`
	Repository struct {
		Name              string `json:"name"`
		URL               string `json:"url"`
		VerifiedPublisher bool   `json:"verified_publisher"`
		Official          bool   `json:"official"`
	} `json:"repository"`
}

type ahDetail struct {
	Version string `json:"version"`
	HomeURL string `json:"home_url"`
	Links   []struct {
		URL string `json:"url"`
	} `json:"links"`
	Repository struct {
		URL string `json:"url"`
	} `json:"repository"`
	AvailableVersions []struct {
		Version    string `json:"version"`
		Prerelease bool   `json:"prerelease"`
	} `json:"available_versions"`
}

// resolveArtifactHub searches by exact chart name. With several candidates
// the one whose home or source links match the release's Chart.yaml wins,
// then the official package; otherwise the best by verified publisher and
// stars is used and flagged as guessed. Returns nil, nil when nothing
// matches.
func resolveArtifactHub(ctx context.Context, r HelmRelease) (*ChartUpstream, error) {
	var res ahSearch
	q := url.Values{"kind": {"0"}, "ts_query_web": {r.Chart}, "limit": {"20"}}
	if err := getJSON(ctx, artifactHub+"/packages/search?"+q.Encode(), &res); err != nil {
		return nil, err
	}
	var cands []ahPackage
	for _, p := range res.Packages {
		if p.Name == r.Chart {
			cands = append(cands, p)
		}
	}
	if len(cands) == 0 {
		return nil, nil
	}
	// Cheap pre-filter before any detail fetch: a repo name that shows up in
	// the release's home/sources URLs is almost certainly the right one.
	hint := func(p ahPackage) bool {
		n := strings.ToLower(p.Repository.Name)
		if strings.Contains(normURL(r.Home), n) {
			return true
		}
		for _, src := range r.Sources {
			if strings.Contains(normURL(src), n) {
				return true
			}
		}
		return false
	}
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if ha, hb := hint(a), hint(b); ha != hb {
			return ha
		}
		if a.Official != b.Official {
			return a.Official
		}
		if a.Repository.VerifiedPublisher != b.Repository.VerifiedPublisher {
			return a.Repository.VerifiedPublisher
		}
		return a.Stars > b.Stars
	})

	// Fetch details for the best few; stop at the first Chart.yaml match.
	// If none match, fall back to the top-ranked candidate.
	const maxDetails = 5
	var fallback *ChartUpstream
	for i, p := range cands {
		if i >= maxDetails {
			break
		}
		var d ahDetail
		if err := getJSON(ctx, artifactHub+"/packages/helm/"+p.Repository.Name+"/"+p.Name, &d); err != nil {
			continue
		}
		up := fromDetail(d)
		// A Chart.yaml match, a repo-name hint or the official flag all count
		// as confident; anything else is a ranked guess.
		if matchesRelease(d, r) || hint(p) || p.Official || p.Repository.Official {
			return &up, nil
		}
		if fallback == nil {
			fallback = &up
			fallback.Guessed = len(cands) > 1
		}
		if len(cands) == 1 {
			break
		}
	}
	return fallback, nil
}

func fromDetail(d ahDetail) ChartUpstream {
	up := ChartUpstream{Version: d.Version, Repo: d.Repository.URL, Via: "artifacthub"}
	var best data.Version
	for _, av := range d.AvailableVersions {
		v, ok := data.ParseVersion(av.Version)
		if !ok || av.Prerelease || v.Prerelease {
			continue
		}
		if best.Less(v) {
			best = v
			up.Version = av.Version
		}
	}
	for _, l := range d.Links {
		if strings.Contains(l.URL, "github.com") || strings.Contains(l.URL, "gitlab.com") {
			up.Source = l.URL
			break
		}
	}
	if up.Source == "" && len(d.Links) > 0 {
		up.Source = d.Links[0].URL
	}
	return up
}

func matchesRelease(d ahDetail, r HelmRelease) bool {
	if r.Home != "" && normURL(r.Home) == normURL(d.HomeURL) {
		return true
	}
	for _, src := range r.Sources {
		for _, l := range d.Links {
			if normURL(src) == normURL(l.URL) {
				return true
			}
		}
	}
	return false
}

func normURL(u string) string {
	u = strings.TrimSpace(strings.ToLower(u))
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	return strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git")
}

func getJSON(ctx context.Context, u string, dst any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}

// --- Helm repo index (charts.yaml override path) ---

type repoIndex struct {
	Entries map[string][]struct {
		Version string   `json:"version"`
		Sources []string `json:"sources"`
	} `json:"entries"`
}

func fetchIndex(ctx context.Context, repo string) (*repoIndex, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	u := strings.TrimSuffix(repo, "/") + "/index.yaml"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	var idx repoIndex
	if err := yaml.Unmarshal(body, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

func newestStable(idx *repoIndex, chart string) (ChartUpstream, bool) {
	var best ChartUpstream
	var bestV data.Version
	found := false
	for _, e := range idx.Entries[chart] {
		v, ok := data.ParseVersion(e.Version)
		if !ok || v.Prerelease {
			continue
		}
		if !found || bestV.Less(v) {
			bestV, found = v, true
			best = ChartUpstream{Version: v.String()}
			if len(e.Sources) > 0 {
				best.Source = e.Sources[0]
			}
		}
	}
	return best, found
}

// ResolveChart is a convenience for tooling: resolve one release the same
// way a scan does, without a cluster.
func ResolveChart(ctx context.Context, tables *data.Tables, r HelmRelease) (*ChartUpstream, error) {
	if repo, ok := tables.Charts.Repo(r.Chart); ok {
		idx, err := fetchIndex(ctx, repo)
		if err != nil {
			return nil, err
		}
		up, ok := newestStable(idx, r.Chart)
		if !ok {
			return nil, fmt.Errorf("chart %s not in index", r.Chart)
		}
		up.Repo, up.Via = repo, "charts.yaml"
		return &up, nil
	}
	return resolveArtifactHub(ctx, r)
}

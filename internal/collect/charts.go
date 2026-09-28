package collect

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/filidorwiese/kubegrade/internal/data"
)

// repoIndex is the part of a Helm repo index.yaml we read.
type repoIndex struct {
	Entries map[string][]struct {
		Version string `json:"version"`
	} `json:"entries"`
}

// chartUpstream fetches index.yaml once per repo that a running release
// maps to and records the newest stable chart version. Only in --online.
func (c *Collector) chartUpstream(ctx context.Context, s *Snapshot) error {
	if !c.online {
		return nil
	}
	s.ChartLatest = map[string]string{}
	indexes := map[string]*repoIndex{}
	for _, r := range s.HelmReleases {
		repo, ok := c.tables.Charts.Repo(r.Chart)
		if !ok {
			continue
		}
		idx, seen := indexes[repo]
		if !seen {
			var err error
			idx, err = fetchIndex(ctx, repo)
			if err != nil {
				s.Errors = append(s.Errors, "chart repo "+repo+": "+err.Error())
			}
			indexes[repo] = idx
		}
		if idx == nil {
			continue
		}
		if latest, ok := newestStable(idx, r.Chart); ok {
			s.ChartLatest[r.Chart] = latest
		}
	}
	return nil
}

func fetchIndex(ctx context.Context, repo string) (*repoIndex, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	url := strings.TrimSuffix(repo, "/") + "/index.yaml"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

func newestStable(idx *repoIndex, chart string) (string, bool) {
	var best data.Version
	found := false
	for _, e := range idx.Entries[chart] {
		v, ok := data.ParseVersion(e.Version)
		if !ok || v.Prerelease {
			continue
		}
		if !found || best.Less(v) {
			best, found = v, true
		}
	}
	return best.String(), found
}

// NewestUpstream is a convenience for tooling: newest stable version of one
// chart from one repo.
func NewestUpstream(ctx context.Context, repo, chart string) (string, error) {
	idx, err := fetchIndex(ctx, repo)
	if err != nil {
		return "", err
	}
	v, ok := newestStable(idx, chart)
	if !ok {
		return "", fmt.Errorf("chart %s not in index", chart)
	}
	return v, nil
}

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/filidorwiese/kubegrade/internal/data"
)

var releasesURL = "https://api.github.com/repos/filidorwiese/kubegrade/releases/latest"

// newerRelease returns the latest release tag when it is newer than the
// running version. Dev builds and any lookup failure return "" silently;
// an update hint is never worth failing a scan for.
func newerRelease(ctx context.Context) string {
	cur, ok := data.ParseVersion(version)
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var rel struct {
		Tag string `json:"tag_name"`
	}
	if json.NewDecoder(resp.Body).Decode(&rel) != nil {
		return ""
	}
	latest, ok := data.ParseVersion(rel.Tag)
	if !ok || !cur.Less(latest) {
		return ""
	}
	return strings.TrimPrefix(rel.Tag, "v")
}

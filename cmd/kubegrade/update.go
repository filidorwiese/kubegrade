package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/filidorwiese/kubegrade/internal/data"
)

const repo = "filidorwiese/kubegrade"

var (
	releasesURL  = "https://api.github.com/repos/" + repo + "/releases/latest"
	downloadBase = "https://github.com/" + repo + "/releases/download/"
)

// latestRelease returns the newest release tag, e.g. "v0.3.0".
func latestRelease(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github: %s", resp.Status)
	}
	var rel struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	return rel.Tag, nil
}

// newerRelease returns the latest release tag when it is newer than the
// running version. Dev builds and any lookup failure return "" silently;
// an update hint is never worth failing a scan for.
func newerRelease(ctx context.Context) string {
	cur, ok := data.ParseVersion(version)
	if !ok {
		return ""
	}
	tag, err := latestRelease(ctx)
	if err != nil {
		return ""
	}
	latest, ok := data.ParseVersion(tag)
	if !ok || !cur.Less(latest) {
		return ""
	}
	return strings.TrimPrefix(tag, "v")
}

// selfUpdate replaces the running binary with the latest release after
// checking it against the published sha256 sums.
func selfUpdate(ctx context.Context) error {
	cur, ok := data.ParseVersion(version)
	if !ok {
		return errors.New("this is a dev build, reinstall with go install github.com/" + repo + "/cmd/kubegrade@latest")
	}
	tag, err := latestRelease(ctx)
	if err != nil {
		return err
	}
	latest, ok := data.ParseVersion(tag)
	if !ok {
		return fmt.Errorf("unexpected release tag %q", tag)
	}
	if !cur.Less(latest) {
		fmt.Printf("already up to date (%s)\n", version)
		return nil
	}

	asset := "kubegrade_" + runtime.GOOS + "_" + runtime.GOARCH
	base := downloadBase + tag + "/"
	want, err := publishedSum(ctx, base+"checksums.txt", asset)
	if err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	// Write next to the binary so the final rename is atomic and on the
	// same filesystem; a failed download never touches the old file.
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".kubegrade-*")
	if err != nil {
		return fmt.Errorf("cannot write %s: %w (try sudo)", filepath.Dir(exe), err)
	}
	defer os.Remove(tmp.Name())

	sum, err := download(ctx, base+asset, tmp)
	tmp.Close()
	if err != nil {
		return err
	}
	if sum != want {
		return errors.New("checksum mismatch, download discarded")
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		return fmt.Errorf("cannot replace %s: %w (try sudo)", exe, err)
	}
	fmt.Printf("updated %s to %s\n", version, strings.TrimPrefix(tag, "v"))
	return nil
}

func publishedSum(ctx context.Context, url, asset string) (string, error) {
	var b strings.Builder
	if _, err := download(ctx, url, &b); err != nil {
		return "", err
	}
	for _, line := range strings.Split(b.String(), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == asset {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("no release asset for %s", asset)
}

// download streams url into w and returns its sha256.
func download(ctx context.Context, url string, w io.Writer) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", url, resp.Status)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), resp.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

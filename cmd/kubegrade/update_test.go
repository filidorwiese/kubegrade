package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

// fakeRelease serves a release API and download tree; the checksum file
// lists sum for the asset of this platform.
func fakeRelease(t *testing.T, tag, sum string, asset []byte) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			w.Write([]byte(sum + "  kubegrade_" + platform() + "\n"))
			return
		}
		w.Write(asset)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	oldR, oldD, oldV := releasesURL, downloadBase, version
	releasesURL, downloadBase = srv.URL+"/latest", srv.URL+"/dl/"
	t.Cleanup(func() { releasesURL, downloadBase, version = oldR, oldD, oldV })
}

func platform() string { return runtime.GOOS + "_" + runtime.GOARCH }

func TestNewerRelease(t *testing.T) {
	fakeRelease(t, "v0.3.0", "", nil)
	version = "0.2.0"
	if got, err := newerRelease(context.Background()); got != "0.3.0" || err != nil {
		t.Errorf("got %q, %v; want 0.3.0", got, err)
	}
	version = "0.3.0"
	if got, err := newerRelease(context.Background()); got != "" || err != nil {
		t.Errorf("same version: got %q, %v", got, err)
	}
	version = "dev"
	if got, err := newerRelease(context.Background()); got != "" || err == nil {
		t.Errorf("dev build: got %q, %v", got, err)
	}
	releasesURL = strings.Replace(releasesURL, "/latest", "/missing", 1)
	if _, err := newerRelease(context.Background()); err == nil {
		t.Error("no redirect must be an error, not silence")
	}
}

func TestSelfUpdateRefusesDevBuild(t *testing.T) {
	fakeRelease(t, "v0.3.0", "", nil)
	version = "dev"
	if err := selfUpdate(context.Background()); err == nil {
		t.Error("dev build must refuse to update")
	}
}

// A binary whose hash differs from checksums.txt must never replace the
// running executable.
func TestSelfUpdateChecksumMismatch(t *testing.T) {
	fakeRelease(t, "v0.3.0", strings.Repeat("0", 64), []byte("binary"))
	version = "0.2.0"
	err := selfUpdate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("got %v", err)
	}
}

func TestPublishedSum(t *testing.T) {
	want := sha256.Sum256([]byte("binary"))
	fakeRelease(t, "v0.3.0", hex.EncodeToString(want[:]), []byte("binary"))
	got, err := publishedSum(context.Background(), downloadBase+"v0.3.0/checksums.txt", "kubegrade_"+platform())
	if err != nil || got != hex.EncodeToString(want[:]) {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := publishedSum(context.Background(), downloadBase+"v0.3.0/checksums.txt", "kubegrade_other"); err == nil {
		t.Error("missing asset must error")
	}
}

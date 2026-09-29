package collect

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestParseReleaseName(t *testing.T) {
	name, rev, ok := parseReleaseName("sh.helm.release.v1.my.app.v12")
	if !ok || name != "my.app" || rev != 12 {
		t.Errorf("got %q %d %v", name, rev, ok)
	}
	if _, _, ok := parseReleaseName("other-secret"); ok {
		t.Error("non-helm secret must not parse")
	}
}

// Helm stores base64(gzip(json)); older releases skip the gzip layer.
func TestDecodeRelease(t *testing.T) {
	payload := []byte(`{"name":"app","version":3,"info":{"status":"deployed"},"chart":{"metadata":{"name":"app","version":"1.2.3"}}}`)
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write(payload)
	w.Close()
	for _, raw := range [][]byte{payload, gz.Bytes()} {
		rel, err := decodeRelease([]byte(base64.StdEncoding.EncodeToString(raw)))
		if err != nil {
			t.Fatal(err)
		}
		if rel.Version != 3 || rel.Info.Status != "deployed" || rel.Chart.Metadata.Version != "1.2.3" {
			t.Errorf("decoded %+v", rel)
		}
	}
}

// Listing a deprecated version proves nothing (the server converts on
// read); only write evidence in managedFields or last-applied counts.
func TestWrittenWith(t *testing.T) {
	u := unstructured.Unstructured{Object: map[string]any{}}
	if writtenWith(u, "policy/v1beta1") {
		t.Error("object without write evidence must not count")
	}
	u.SetManagedFields([]metav1.ManagedFieldsEntry{{APIVersion: "policy/v1beta1"}})
	if !writtenWith(u, "policy/v1beta1") {
		t.Error("managedFields evidence ignored")
	}
	u = unstructured.Unstructured{Object: map[string]any{}}
	u.SetAnnotations(map[string]string{lastApplied: `{"apiVersion":"policy/v1beta1"}`})
	if !writtenWith(u, "policy/v1beta1") {
		t.Error("last-applied evidence ignored")
	}
}

func TestMatchesRelease(t *testing.T) {
	var d ahDetail
	d.HomeURL = "https://Example.org/chart/"
	r := HelmRelease{Home: "http://example.org/chart"}
	if !matchesRelease(d, r) {
		t.Error("home URLs differing only in scheme, case and slash must match")
	}
	if matchesRelease(d, HelmRelease{Home: "https://other.org"}) {
		t.Error("different home must not match")
	}
}

func TestNewestStableSkipsPrerelease(t *testing.T) {
	idx := &repoIndex{}
	idx.Entries = map[string][]struct {
		Version string   `json:"version"`
		Sources []string `json:"sources"`
	}{
		"app": {{Version: "2.0.0-rc1"}, {Version: "1.9.0", Sources: []string{"https://src"}}, {Version: "1.10.0"}},
	}
	up, ok := newestStable(idx, "app")
	if !ok || up.Version != "1.10.0" {
		t.Errorf("got %+v", up)
	}
	if _, ok := newestStable(idx, "missing"); ok {
		t.Error("unknown chart must not resolve")
	}
}

func TestFromDetailPrefersHighestStable(t *testing.T) {
	var d ahDetail
	d.Version = "1.0.0"
	d.AvailableVersions = []struct {
		Version    string `json:"version"`
		Prerelease bool   `json:"prerelease"`
	}{{Version: "1.2.0"}, {Version: "2.0.0-beta", Prerelease: true}, {Version: "1.1.0"}}
	d.Links = []struct {
		URL string `json:"url"`
	}{{URL: "https://example.org"}, {URL: "https://github.com/org/repo"}}
	up := fromDetail(d)
	if up.Version != "1.2.0" || up.Source != "https://github.com/org/repo" {
		t.Errorf("got %+v", up)
	}
}

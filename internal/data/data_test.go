package data

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want Version
		ok   bool
	}{
		{"v1.34.4+k3s1", Version{Major: 1, Minor: 34, Patch: 4}, true},
		{"6.12", Version{Major: 6, Minor: 12}, true},
		{"2.0.0-rc1", Version{Major: 2, Prerelease: true}, true},
		{"1", Version{}, false},
		{"a.b", Version{}, false},
	}
	for _, c := range cases {
		got, ok := ParseVersion(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseVersion(%q) = %+v, %v; want %+v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
	if !(Version{1, 9, 9, false}).Less(Version{1, 10, 0, false}) {
		t.Error("1.9.9 should be less than 1.10.0")
	}
}

func TestMinorLess(t *testing.T) {
	if !MinorLess("1.9", "1.10") || MinorLess("1.10", "1.9") {
		t.Error("MinorLess must compare numerically, not lexically")
	}
}

func TestOSMatchLongestWins(t *testing.T) {
	os := OS{Distros: []Distro{
		{Match: "Ubuntu 22.04", Version: "22.04", EOL: "2027-04-01"},
		{Match: "Ubuntu 22.04.5", Version: "22.04.5", EOL: "2027-04-02"},
		{Match: "Talos", Version: "rolling"},
	}}
	d, ok := os.Match("Ubuntu 22.04.5 LTS")
	if !ok || d.Version != "22.04.5" {
		t.Errorf("got %+v, want the longest match", d)
	}
	if d, ok := os.Match("Talos (v1.8.0)"); !ok || d.EOL != "" {
		t.Errorf("rolling distro should match with empty EOL, got %+v", d)
	}
	if _, ok := os.Match("NixOS 24.05"); ok {
		t.Error("unknown distro must not match")
	}
}

func TestParseCyclesRejectsUnusable(t *testing.T) {
	if _, err := parseCycles("x", []byte(`[{"cycle":"1"},{"cycle":"2"}]`)); err == nil {
		t.Error("payload without eol dates must be rejected")
	}
	if _, err := parseCycles("x", []byte(`{"not":"a list"}`)); err == nil {
		t.Error("wrong shape must be rejected")
	}
}

// The vendored tables must yield usable data: the refresh tool can only
// validate shape, not that the builders still read the fields.
func TestBuiltinTables(t *testing.T) {
	k8s, err := builtin("kubernetes")
	if err != nil {
		t.Fatal(err)
	}
	if k := buildKubernetes(k8s); k.Latest == "" || len(k.Versions) != 8 || k.Versions[7].Minor != k.Latest {
		t.Errorf("kubernetes table: %+v", k)
	}
	linux, err := builtin("linux")
	if err != nil {
		t.Fatal(err)
	}
	kernel := buildKernel(linux)
	lts := 0
	for _, k := range kernel.Kernels {
		if k.LTS {
			lts++
			if k.EOL == "" {
				t.Errorf("LTS %s without EOL", k.Version)
			}
		}
		if k.Latest == "" || k.LatestDate == "" {
			t.Errorf("%s: latest release missing", k.Version)
		}
	}
	if lts < 3 {
		t.Error("expected several LTS kernels")
	}
	for _, p := range osProducts {
		cs, err := builtin(p.product)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.build(cs)) == 0 {
			t.Errorf("%s: no distros built", p.product)
		}
	}
	sles, _ := builtin("sles")
	found := false
	for _, d := range osProducts[len(osProducts)-1].build(sles) {
		found = found || d.Match == "SUSE Linux Enterprise Server 15 SP6"
	}
	if !found {
		t.Error("SLES cycle 15.6 should match PRETTY_NAME '15 SP6'")
	}
}

// Load must survive one product failing online by using the embedded copy
// and saying so, while the other products still come from the server.
func TestLoadFallsBackPerProduct(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		product := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".json")
		if product == "kubernetes" {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		b, err := files.ReadFile("eol/" + product + ".json")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	defer srv.Close()
	old := eolBase
	eolBase = srv.URL + "/"
	defer func() { eolBase = old }()

	tables, note, err := Load(context.Background(), func(string, int, int) {})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(note, "endoflife.date unreachable for kubernetes, using built-in tables from "+builtinDate()) {
		t.Errorf("note = %q", note)
	}
	if tables.Kubernetes.Latest == "" || len(tables.OS.Distros) == 0 {
		t.Error("tables incomplete after fallback")
	}
}

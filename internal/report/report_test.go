package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/filidorwiese/kubegrade/internal/check"
	"github.com/filidorwiese/kubegrade/internal/grade"
)

func TestBuildSortsBySeverityThenCategory(t *testing.T) {
	fs := []check.Finding{
		{ID: "a", Category: check.Health, Severity: check.Low},
		{ID: "b", Category: check.Versions, Severity: check.Low},
		{ID: "c", Category: check.Hygiene, Severity: check.Critical},
		{ID: "d", Category: check.Health, Severity: check.OK},
		{ID: "e", Category: check.Versions, Severity: check.Info},
	}
	r := Build(Input{Findings: fs, Result: grade.Compute(fs)})
	var ids []string
	for _, f := range r.Findings {
		ids = append(ids, f.ID)
	}
	if got := strings.Join(ids, ""); got != "cbaed" {
		t.Errorf("order %s, want cbaed", got)
	}
}

func TestColumnWidths(t *testing.T) {
	long := strings.Repeat("x", 80)
	fs := []Finding{{Resource: "node a", What: long, Fix: "fix it"}}
	resW, whatW, fixW := columnWidths(fs, 120)
	if resW != 6 || fixW != 6 || whatW != 120-32-6-6 {
		t.Errorf("wide: %d %d %d", resW, whatW, fixW)
	}
	// Too narrow for a fix column: it is dropped and listed below instead.
	if _, _, fixW := columnWidths(fs, 70); fixW != 0 {
		t.Errorf("narrow: fixW %d, want 0", fixW)
	}
	// A link widens the fix column only as far as the finding floor of 30 allows.
	link := []Finding{{Resource: "helm x", What: long, Fix: "up", Link: "https://github.com/org/" + strings.Repeat("y", 60)}}
	_, whatW, fixW = columnWidths(link, 120)
	if fixW != 52 || whatW != 30 {
		t.Errorf("link: whatW %d fixW %d", whatW, fixW)
	}
}

func TestWrapURLBreaksAfterSlash(t *testing.T) {
	got := wrapURL("https://github.com/org/repo/releases", 20)
	want := []string{"https://github.com/", "org/repo/releases"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q", got)
	}
	if got := wrapURL(strings.Repeat("a", 25), 10); len(got) != 3 {
		t.Errorf("segment longer than width must be cut: %q", got)
	}
}

func TestWrap(t *testing.T) {
	got := wrap("upgrade the node soon", 11)
	if strings.Join(got, "|") != "upgrade the|node soon" {
		t.Errorf("got %q", got)
	}
	if got := wrap("abcdefghij", 4); strings.Join(got, "|") != "abcd|efgh|ij" {
		t.Errorf("long word: %q", got)
	}
}

// Info and ok rows stay in JSON but are hidden in text unless asked for,
// with a hint saying how many were hidden.
func TestWriteTextVerbosity(t *testing.T) {
	fs := []check.Finding{
		{ID: "x", Category: check.Health, Severity: check.Low, Resource: "pod a", What: "bad", Fix: "fix"},
		{ID: "y", Category: check.Health, Severity: check.Info, Resource: "pod b", What: "fyi"},
		{ID: "z", Category: check.Health, Severity: check.OK, Resource: "nodes", What: "all ready"},
	}
	r := Build(Input{Findings: fs, Result: grade.Compute(fs)})
	render := func(v int) string {
		var b bytes.Buffer
		if err := WriteText(&b, r, TextOptions{Width: 120, Verbose: v}); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	out := render(0)
	if !strings.Contains(out, "1 info hidden") || !strings.Contains(out, "1 passed hidden") || strings.Contains(out, "fyi") {
		t.Errorf("verbose 0:\n%s", out)
	}
	out = render(2)
	if strings.Contains(out, "hidden") || !strings.Contains(out, "all ready") {
		t.Errorf("verbose 2:\n%s", out)
	}
}

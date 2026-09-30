package grade

import (
	"testing"

	"github.com/filidorwiese/kubegrade/internal/check"
)

func TestPointsCount(t *testing.T) {
	if got := Points(check.Finding{Severity: check.Low, Count: 3}); got != 6 {
		t.Errorf("low x3 = %d, want 6", got)
	}
	if got := Points(check.Finding{Severity: check.OK}); got != 0 {
		t.Errorf("ok = %d, want 0", got)
	}
}

func TestComputeClean(t *testing.T) {
	r := Compute(nil)
	if r.Grade != "A+" || r.Score != 100 || r.CappedBy != "" {
		t.Errorf("clean cluster: %+v", r)
	}
}

// One bad category must drag the overall letter down to its own, and the
// score must never go below zero however many findings pile up.
func TestComputeCapsToWorstCategory(t *testing.T) {
	var fs []check.Finding
	for i := 0; i < 10; i++ {
		fs = append(fs, check.Finding{Category: check.Health, Severity: check.Critical})
	}
	r := Compute(fs)
	if r.Grade != "F" || r.CappedBy != check.Health {
		t.Errorf("grade %s capped by %q, want F by health", r.Grade, r.CappedBy)
	}
	if r.Categories[2].Score != 0 {
		t.Errorf("health score %d, want 0", r.Categories[2].Score)
	}
	if r.Score != 39 {
		t.Errorf("capped score %d, want top of F band", r.Score)
	}
}

func TestComputeUncapped(t *testing.T) {
	r := Compute([]check.Finding{{Category: check.Versions, Severity: check.Low}})
	if r.Grade != "A+" || r.CappedBy != "" {
		t.Errorf("single low finding: %+v", r)
	}
}

func TestSummaryOrdersByPoints(t *testing.T) {
	fs := []check.Finding{
		{Category: check.Health, Severity: check.Low, Resource: "a", What: "minor"},
		{Category: check.Health, Severity: check.High, Resource: "b", What: "major"},
		{Category: check.Health, Severity: check.Info, Resource: "c", What: "noise"},
	}
	if got := summary(fs, check.Health); got != "b: major, +1 more" {
		t.Errorf("summary = %q", got)
	}
}

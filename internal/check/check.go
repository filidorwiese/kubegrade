// Package check defines findings and the checks that produce them. Checks
// only read the Snapshot; points are assigned later by grade.
package check

import (
	"context"
	"time"

	"github.com/filidorwiese/kubegrade/internal/collect"
)

type Severity string

const (
	Info     Severity = "info"
	Low      Severity = "low"
	Medium   Severity = "medium"
	High     Severity = "high"
	Critical Severity = "critical"
)

// Categories answer a question each: is it current, is it tidy, is it up.
const (
	Versions = "versions"
	Hygiene  = "hygiene"
	Health   = "health"
)

var Categories = []string{Versions, Hygiene, Health}

var CategoryNames = map[string]string{
	Versions: "Versions",
	Hygiene:  "Hygiene",
	Health:   "Health",
}

type Finding struct {
	ID       string
	Category string
	Severity Severity
	Resource string
	What     string
	Fix      string
	// Link is an optional URL shown under the fix.
	Link string
	// Since is set when the finding describes a condition with a start time.
	Since *time.Time
	// Count multiplies the severity points; zero means one.
	Count int
}

type Check interface {
	Run(ctx context.Context, s *collect.Snapshot) []Finding
}

// All returns every check in report order.
func All() []Check {
	return []Check{
		k8sVersion{}, k8sDeprecated{},
		helmStatus{}, chartOutdated{}, imageTags{},
		kubeletSkew{}, kernelEOL{}, osEOL{}, nodeDrift{}, nodeInfo{}, nodeNotReady{},
		crashLoop{}, podRestarts{}, imagePull{}, podPending{}, deployUnavailable{}, pvcUsage{},
		nodePressure{}, nodeDisk{}, cruft{}, nodeCordoned{}, pvcUnused{}, certExpiry{},
	}
}

func Run(ctx context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, c := range All() {
		out = append(out, c.Run(ctx, s)...)
	}
	return out
}

func days(d time.Duration) int { return int(d.Hours() / 24) }

func humanDuration(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmtInt(days(d)) + "d"
	case d >= time.Hour:
		return fmtInt(int(d.Hours())) + "h"
	default:
		return fmtInt(int(d.Minutes())) + "m"
	}
}

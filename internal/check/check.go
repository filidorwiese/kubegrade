// Package check defines findings and the checks that produce them. Checks
// only read the Snapshot; points are assigned later by grade.
package check

import (
	"context"
	"time"

	"github.com/filidorwiese/kubegrade/internal/collect"
	"github.com/filidorwiese/kubegrade/internal/state"
)

type Severity string

const (
	Info     Severity = "info"
	Low      Severity = "low"
	Medium   Severity = "medium"
	High     Severity = "high"
	Critical Severity = "critical"
)

const (
	ControlPlane = "control-plane"
	Workloads    = "workloads"
	Nodes        = "nodes"
	Certificates = "certificates"
	Sustained    = "sustained"
)

var Categories = []string{ControlPlane, Workloads, Nodes, Certificates, Sustained}

var CategoryNames = map[string]string{
	ControlPlane: "Control plane",
	Workloads:    "Workloads",
	Nodes:        "Nodes",
	Certificates: "Certificates",
	Sustained:    "Sustained conditions",
}

type Finding struct {
	ID       string
	Category string
	Severity Severity
	Resource string
	What     string
	Fix      string
	// Since is set when the finding describes a condition with a start time.
	Since *time.Time
	// Count multiplies the severity points; zero means one.
	Count int
}

type Check interface {
	ID() string
	Category() string
	Run(ctx context.Context, s *collect.Snapshot, st *state.Store) []Finding
}

// All returns every check in report order.
func All() []Check {
	return []Check{
		k8sVersion{}, k8sDataStale{}, k8sDeprecated{},
		helmStatus{}, chartOutdated{}, imageTags{},
		kubeletSkew{}, kernelEOL{}, osEOL{}, runtimeVersion{}, nodeNotReady{},
		apiServerCert{}, tlsSecrets{}, certManager{},
		crashLoop{}, podPending{}, deployUnavailable{}, pvcUsage{},
	}
}

func Run(ctx context.Context, s *collect.Snapshot, st *state.Store) []Finding {
	var out []Finding
	for _, c := range All() {
		out = append(out, c.Run(ctx, s, st)...)
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

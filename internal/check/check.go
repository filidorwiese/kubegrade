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
	// OK marks a check that ran and found nothing; only shown with -vv.
	OK       Severity = "ok"
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

// entry pairs a check with the row shown when it finds nothing. An empty
// what means the check is informational and never "passes".
type entry struct {
	id, category, resource, what string
	check                        Check
}

// All returns every check in report order.
func All() []entry {
	return []entry{
		{"k8s-version", Versions, "kubernetes", "supported and current", k8sVersion{}},
		{"k8s-api-deprecated", Hygiene, "api objects", "no deprecated api versions in use", k8sDeprecated{}},
		{"helm-status", Health, "helm releases", "all deployed", helmStatus{}},
		{"chart-outdated", Versions, "helm charts", "all at the latest upstream version", chartOutdated{}},
		{"image-tags", Hygiene, "deployments", "images pinned by tag", imageTags{}},
		{"kubelet-skew", Versions, "nodes", "kubelet versions match the api server", kubeletSkew{}},
		{"kernel-eol", Versions, "nodes", "kernels within support", kernelEOL{}},
		{"os-eol", Versions, "nodes", "os releases within support", osEOL{}},
		{"node-drift", Hygiene, "nodes", "kernel, os, kubelet and runtime in sync", nodeDrift{}},
		{"node-info", Versions, "", "", nodeInfo{}},
		{"node-notready", Health, "nodes", "all ready", nodeNotReady{}},
		{"pod-crashloop", Health, "pods", "no crash loops", crashLoop{}},
		{"pod-restarts", Health, "pods", "no frequent restarts", podRestarts{}},
		{"image-pull", Health, "pods", "all images pulled", imagePull{}},
		{"pod-pending", Health, "pods", "none pending over 1h", podPending{}},
		{"deploy-unavailable", Health, "deployments", "all fully available", deployUnavailable{}},
		{"pvc-usage", Health, "volumes", "all under 90% used", pvcUsage{}},
		{"node-pressure", Health, "nodes", "no disk, memory or pid pressure", nodePressure{}},
		{"node-disk", Health, "nodes", "disks under 90% used", nodeDisk{}},
		{"pods-leftover", Hygiene, "pods", "no evicted, stale or controller-less pods", cruft{}},
		{"node-cordoned", Hygiene, "nodes", "none cordoned", nodeCordoned{}},
		{"pvc-unused", Hygiene, "volumes", "all bound claims mounted", pvcUnused{}},
		{"cert-expiry", Health, "certificates", "none expiring within 30d", certExpiry{}},
	}
}

// Run executes every check. A check that found nothing adds an OK row so
// the report can show what was covered.
func Run(ctx context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, e := range All() {
		fs := e.check.Run(ctx, s)
		if len(fs) == 0 && e.what != "" {
			fs = []Finding{{ID: e.id, Category: e.category, Severity: OK, Resource: e.resource, What: e.what}}
		}
		out = append(out, fs...)
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

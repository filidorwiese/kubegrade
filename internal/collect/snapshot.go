// Package collect fetches cluster data once per scan into a Snapshot that
// the checks read. Checks never touch the API themselves.
package collect

import (
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/filidorwiese/kubegrade/internal/data"
)

type Snapshot struct {
	ScannedAt     time.Time
	ServerVersion string // e.g. "v1.34.4+k3s1"

	Nodes       []corev1.Node
	Pods        []corev1.Pod
	Deployments []appsv1.Deployment
	ReplicaSets []appsv1.ReplicaSet
	PVCs        []corev1.PersistentVolumeClaim

	HelmReleases []HelmRelease
	VolumeStats  []VolumeStat
	NodeFS       []NodeFS
	// NodeStart is the kubelet start time per node from stats/summary; on
	// k3s that is effectively the boot time.
	NodeStart  map[string]time.Time
	Deprecated []DeprecatedUse
	// ChartLatest is chart name -> newest stable upstream version.
	ChartLatest map[string]ChartUpstream

	// Tables are the EOL tables used for this scan.
	Tables *data.Tables

	// Errors from non-fatal collectors; surfaced as info findings.
	Errors []string
}

type HelmRelease struct {
	Namespace string
	Name      string
	Revision  int
	Revisions int
	Status    string
	Chart     string
	Version   string
	// Home and Sources come from Chart.yaml and are used to pick the right
	// Artifact Hub package when several share the chart name.
	Home     string
	Sources  []string
	Deployed time.Time
}

type ChartUpstream struct {
	Version string
	// Repo is the Helm repository URL the version was resolved from.
	Repo string
	// Source is the chart's source link, usually the repo holding the
	// changelog. Empty when unknown.
	Source string
	// Guessed is set when several Artifact Hub packages share the name and
	// none matched the release's home/sources; the best-ranked one was used.
	Guessed bool
}

// NodeFS is a node filesystem from the kubelet: Kind "root" or "image".
type NodeFS struct {
	Node     string
	Kind     string
	Used     uint64
	Capacity uint64
}

type VolumeStat struct {
	Node      string
	Namespace string
	PVC       string
	Used      uint64
	Capacity  uint64
}

type DeprecatedUse struct {
	Namespace string
	Name      string
	Dep       data.Deprecation
}

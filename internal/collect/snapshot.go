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
	Deprecated   []DeprecatedUse
	// ChartLatest is chart name -> newest stable upstream; nil when not
	// running --online.
	ChartLatest map[string]ChartUpstream

	// Tables are the EOL tables used for this scan.
	Tables *data.Tables

	// Errors from non-fatal collectors; surfaced as info findings.
	Errors []string
}

type HelmRelease struct {
	Namespace  string
	Name       string
	Revision   int
	Revisions  int
	Status     string
	Chart      string
	Version    string
	AppVersion string
	Deployed   time.Time
}

type ChartUpstream struct {
	Version string
	// Source is the first entry of the chart's sources list, usually the
	// repo holding the changelog.
	Source string
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

// Package collect fetches cluster data once per scan into a Snapshot that
// the checks read. Checks never touch the API themselves.
package collect

import (
	"crypto/x509"
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

	HelmReleases  []HelmRelease
	TLSSecrets    []TLSSecret
	APIServerCert *x509.Certificate
	CertManager   CertManager
	VolumeStats   []VolumeStat
	Deprecated    []DeprecatedUse

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

type TLSSecret struct {
	Namespace string
	Name      string
	Cert      *x509.Certificate
	// CertManaged is true when cert-manager owns the secret.
	CertManaged bool
}

type CertManager struct {
	Installed    bool
	Certificates []Certificate
}

type Certificate struct {
	Namespace     string
	Name          string
	Ready         bool
	ReadyReason   string
	IssuingFailed bool
	IssuingReason string
	NotAfter      *time.Time
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

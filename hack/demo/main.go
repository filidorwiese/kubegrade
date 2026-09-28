// Command demo renders a representative report with colour for screenshots.
// It goes through the real report package, so it always reflects the
// current layout. Run: go run ./hack/demo
package main

import (
	"os"
	"time"

	"github.com/filidorwiese/kubegrade/internal/check"
	"github.com/filidorwiese/kubegrade/internal/grade"
	"github.com/filidorwiese/kubegrade/internal/report"
)

func main() {
	findings := []check.Finding{
		{ID: "k8s-version-eol", Category: check.Versions, Severity: check.Medium, Resource: "kubernetes 1.34.4",
			What: "end of support 2026-10-27 (29 days)", Fix: "upgrade to 1.37"},
		{ID: "chart-outdated", Category: check.Versions, Severity: check.Medium, Resource: "helm traefik/traefik",
			What: "major 41.6.0 available, have 40.3.0", Fix: "major upgrade to 41.6.0",
			Link: "https://github.com/traefik/traefik-helm-chart"},
		{ID: "chart-outdated", Category: check.Versions, Severity: check.Low, Resource: "helm kube-system/kured",
			What: "1 minor behind 6.1.0, have 6.0.0", Fix: "helm upgrade to 6.1.0"},
		{ID: "node-info", Category: check.Versions, Severity: check.Info, Resource: "4 nodes",
			What: "Debian GNU/Linux 13 (trixie), kernel 6.12.107 LTS until 2028-12-31, up 3d to 12d"},
		{ID: "image-tag-latest", Category: check.Hygiene, Severity: check.Low, Resource: "deploy shop/checkout-api",
			What: "image checkout-api:latest", Fix: "pin a version tag"},
		{ID: "node-drift", Category: check.Hygiene, Severity: check.High, Resource: "node worker-2",
			What: "kernel 6.12.63 differs from 4 nodes on 6.12.107, up 109d", Fix: "pending reboot or upgrade"},
		{ID: "image-no-digest", Category: check.Hygiene, Severity: check.Info, Resource: "deployments",
			What: "images by tag without digest: 19 deployments"},
		{ID: "pod-crashloop", Category: check.Health, Severity: check.Medium, Resource: "deploy shop/plausible",
			What: "CrashLoopBackOff 1/1 pods, 5 restarts (OOMKilled)", Fix: "raise the memory limit or fix the leak"},
	}
	r := report.Build(report.Input{
		Agent: "0.1.0", Cluster: "demo", ScannedAt: time.Date(2026, 9, 28, 14, 0, 12, 0, time.UTC),
		Duration: 5300 * time.Millisecond, Findings: findings, Result: grade.Compute(findings),
	})
	if err := report.WriteText(os.Stdout, r, report.TextOptions{Color: true, Width: 130}); err != nil {
		os.Exit(1)
	}
}

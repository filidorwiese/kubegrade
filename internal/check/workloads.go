package check

import (
	"context"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/filidorwiese/kubegrade/internal/collect"
	"github.com/filidorwiese/kubegrade/internal/data"
)

type helmStatus struct{}

func (helmStatus) ID() string       { return "helm-status" }
func (helmStatus) Category() string { return Health }

func (helmStatus) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, r := range s.HelmReleases {
		res := "helm " + r.Namespace + "/" + r.Name
		rev := " (rev " + fmtInt(r.Revision) + ")"
		switch {
		case r.Status == "failed":
			out = append(out, Finding{ID: "helm-status", Category: Health, Severity: Medium,
				Resource: res, What: "status failed" + rev, Fix: "helm rollback or reinstall"})
		case strings.HasPrefix(r.Status, "pending-") && s.ScannedAt.Sub(r.Deployed) > time.Hour:
			since := r.Deployed
			out = append(out, Finding{ID: "helm-status", Category: Health, Severity: Low,
				Resource: res, What: "status " + r.Status + " for " + humanDuration(s.ScannedAt.Sub(since)) + rev,
				Fix: "helm rollback or reinstall", Since: &since})
		}
		if r.Revisions > 10 {
			out = append(out, Finding{ID: "helm-revisions", Category: Hygiene, Severity: Info,
				Resource: res, What: fmtInt(r.Revisions) + " revisions kept", Fix: "helm history / --history-max"})
		}
	}
	return out
}

// k3s ships these in kube-system and versions them with itself.
var k3sBundled = map[string]bool{
	"traefik": true, "coredns": true, "local-path-provisioner": true, "metrics-server": true,
}

type imageTags struct{}

func (imageTags) ID() string       { return "image-tag-latest" }
func (imageTags) Category() string { return Hygiene }

func (imageTags) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	noDigest := 0
	for _, d := range s.Deployments {
		if d.Namespace == "kube-system" && k3sBundled[d.Name] {
			continue
		}
		var unpinned []string
		tagOnly := false
		for _, img := range images(d) {
			ref := parseImage(img)
			if ref.digest == "" {
				tagOnly = true
			}
			if ref.digest == "" && (ref.tag == "" || ref.tag == "latest") {
				unpinned = append(unpinned, img)
			}
		}
		if tagOnly {
			noDigest++
		}
		if len(unpinned) > 0 {
			out = append(out, Finding{ID: "image-tag-latest", Category: Hygiene, Severity: Medium,
				Resource: "deploy " + d.Namespace + "/" + d.Name,
				What:     "image " + strings.Join(unpinned, ", "), Fix: "pin a version tag"})
		}
	}
	if noDigest > 0 {
		out = append(out, Finding{ID: "image-no-digest", Category: Hygiene, Severity: Info,
			Resource: "deployments", What: "images by tag without digest: " + plural(noDigest, "deployment")})
	}
	return out
}

func images(d appsv1.Deployment) []string {
	var out []string
	for _, c := range d.Spec.Template.Spec.InitContainers {
		out = append(out, c.Image)
	}
	for _, c := range d.Spec.Template.Spec.Containers {
		out = append(out, c.Image)
	}
	return out
}

type imageRef struct{ tag, digest string }

// parseImage splits registry/name[:tag][@digest]. A colon before the last
// slash belongs to a registry port, not a tag.
func parseImage(img string) imageRef {
	var ref imageRef
	if i := strings.Index(img, "@"); i >= 0 {
		ref.digest = img[i+1:]
		img = img[:i]
	}
	slash := strings.LastIndex(img, "/")
	if colon := strings.LastIndex(img, ":"); colon > slash {
		ref.tag = img[colon+1:]
	}
	return ref
}

type chartOutdated struct{}

func (chartOutdated) ID() string       { return "chart-outdated" }
func (chartOutdated) Category() string { return Versions }

func (chartOutdated) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, r := range s.HelmReleases {
		res := "helm " + r.Namespace + "/" + r.Name
		up, ok := s.ChartLatest[r.Chart]
		if !ok {
			out = append(out, Finding{ID: "chart-unresolved", Category: Versions, Severity: Info,
				Resource: res, What: "chart " + r.Chart + " not found on Artifact Hub", Fix: "map it in internal/data/charts.yaml"})
			continue
		}
		cur, okCur := data.ParseVersion(r.Version)
		latest, okUp := data.ParseVersion(up.Version)
		if !okCur || !okUp || !cur.Less(latest) {
			continue
		}
		f := Finding{ID: "chart-outdated", Category: Versions, Resource: res, Fix: "helm upgrade to " + up.Version}
		switch {
		case latest.Major > cur.Major:
			f.Severity = Medium
			f.What = "chart " + r.Chart + " " + r.Version + ", major " + up.Version + " available"
			f.Fix = "major upgrade, read the changelog first"
			if up.Source != "" {
				f.Fix += ": " + up.Source
			}
		case latest.Minor > cur.Minor:
			f.Severity = Low
			f.Count = min(latest.Minor-cur.Minor, 3)
			f.What = "chart " + r.Chart + " " + r.Version + ", " + plural(latest.Minor-cur.Minor, "minor") + " behind " + up.Version
		default:
			f.Severity = Info
			f.What = "chart " + r.Chart + " " + r.Version + ", patch " + up.Version + " available"
		}
		if up.Guessed {
			f.Fix += "; upstream guessed as " + up.Repo + ", pin it in charts.yaml if wrong"
		}
		out = append(out, f)
	}
	return out
}

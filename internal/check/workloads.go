package check

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/filidorwiese/kubegrade/internal/collect"
	"github.com/filidorwiese/kubegrade/internal/data"
)

type helmStatus struct{}

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

func (imageTags) Run(_ context.Context, s *collect.Snapshot) []Finding {
	noDigest := 0
	// One finding per unpinned image, not per workload: the same
	// nginx:latest in five places is one thing to fix.
	var order []string
	users := map[string][]string{}
	for _, w := range s.Workloads() {
		if w.Namespace == "kube-system" && k3sBundled[w.Name] {
			continue
		}
		tagOnly := false
		for _, img := range w.Images() {
			ref := collect.ParseImage(img)
			if ref.Digest == "" {
				tagOnly = true
			}
			if ref.Digest == "" && (ref.Tag == "" || ref.Tag == "latest") {
				if _, seen := users[img]; !seen {
					order = append(order, img)
				}
				users[img] = append(users[img], w.Kind+" "+w.Namespace+"/"+w.Name)
			}
		}
		if tagOnly {
			noDigest++
		}
	}
	var out []Finding
	for _, img := range order {
		out = append(out, Finding{ID: "image-tag-latest", Category: Hygiene, Severity: Low,
			Resource: "image " + shortImage(img),
			What:     "used by " + strings.Join(users[img], ", "), Fix: "pin a version tag"})
	}
	if noDigest > 0 {
		out = append(out, Finding{ID: "image-no-digest", Category: Hygiene, Severity: Info,
			Resource: "workloads", What: "images by tag without digest: " + plural(noDigest, "workload")})
	}
	return out
}

// shortImage drops the registry and path: "ghcr.io/org/app:latest" -> "app:latest".
func shortImage(img string) string {
	if i := strings.LastIndex(img, "/"); i >= 0 {
		return img[i+1:]
	}
	return img
}

type imageOutdated struct{}

// imageOutdated compares each Docker Hub image tag against the newest tag
// of the same shape. Helm-managed workloads are the chart's concern and
// k3s-bundled ones follow the k3s version.
func (imageOutdated) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var order []string
	users := map[string][]string{}
	for _, w := range s.Workloads() {
		if w.Helm || w.Namespace == "kube-system" && k3sBundled[w.Name] {
			continue
		}
		for _, img := range w.Images() {
			if _, seen := users[img]; !seen {
				order = append(order, img)
			}
			users[img] = append(users[img], w.Kind+" "+w.Namespace+"/"+w.Name)
		}
	}
	var out []Finding
	for _, img := range order {
		ref := collect.ParseImage(img)
		tags, ok := s.ImageTags[ref.HubRepo]
		if !ok {
			continue
		}
		newest, seg, ok := newerTag(ref.Tag, tags)
		if !ok {
			continue
		}
		f := Finding{ID: "image-outdated", Category: Versions, Resource: "image " + shortImage(img),
			Fix: "bump tag to " + newest, Link: hubLink(ref.HubRepo)}
		if ref.Digest != "" {
			f.Fix = "bump tag and digest to " + newest
		}
		by := ", used by " + strings.Join(users[img], ", ")
		switch seg {
		case 0:
			f.Severity = Medium
			f.What = "major " + newest + " available" + by
		case 1:
			f.Severity = Low
			f.What = "minor " + newest + " available" + by
		default:
			f.Severity = Info
			f.What = "patch " + newest + " available" + by
		}
		out = append(out, f)
	}
	return out
}

func hubLink(repo string) string {
	if name, ok := strings.CutPrefix(repo, "library/"); ok {
		return "https://hub.docker.com/_/" + name
	}
	return "https://hub.docker.com/r/" + repo
}

// tagParts splits "5.2.1-apache" into its numbers and a shape "#.#.#-apache".
// Tags of equal shape are the same variant and compare numerically.
func tagParts(tag string) (nums []int, shape string, ok bool) {
	var b strings.Builder
	for i := 0; i < len(tag); {
		if tag[i] < '0' || tag[i] > '9' {
			b.WriteByte(tag[i])
			i++
			continue
		}
		j := i
		for j < len(tag) && tag[j] >= '0' && tag[j] <= '9' {
			j++
		}
		n, err := strconv.Atoi(tag[i:j])
		if err != nil {
			return nil, "", false
		}
		nums = append(nums, n)
		b.WriteByte('#')
		i = j
	}
	return nums, b.String(), len(nums) > 0
}

// newerTag returns the highest same-shape tag above cur and the index of
// the first segment that differs: 0 is a major bump, 1 a minor.
func newerTag(cur string, tags []string) (string, int, bool) {
	curNums, shape, ok := tagParts(cur)
	if !ok {
		return "", 0, false
	}
	best, bestNums := "", curNums
	for _, t := range tags {
		nums, sh, ok := tagParts(t)
		if !ok || sh != shape || !lessInts(bestNums, nums) {
			continue
		}
		best, bestNums = t, nums
	}
	if best == "" {
		return "", 0, false
	}
	for i := range curNums {
		if curNums[i] != bestNums[i] {
			return best, i, true
		}
	}
	return best, len(curNums), true
}

func lessInts(a, b []int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

type chartOutdated struct{}

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
		if up.Deprecated && !up.Guessed {
			out = append(out, Finding{ID: "chart-deprecated", Category: Versions, Severity: Medium,
				Resource: res, What: "chart " + r.Chart + " is deprecated upstream",
				Fix: "move to its successor", Link: up.Source})
		}
		cur, okCur := data.ParseVersion(r.Version)
		latest, okUp := data.ParseVersion(up.Version)
		if !okCur || !okUp || !cur.Less(latest) {
			continue
		}
		// The source repo holds the changelog; the Helm repo is the fallback.
		f := Finding{ID: "chart-outdated", Category: Versions, Resource: res, Fix: "helm upgrade to " + up.Version, Link: up.Source}
		if f.Link == "" {
			f.Link = up.Repo
		}
		switch {
		case latest.Major > cur.Major:
			f.Severity = Medium
			f.What = "major " + up.Version + " available, have " + r.Version
		case latest.Minor > cur.Minor:
			f.Severity = Low
			f.Count = min(latest.Minor-cur.Minor, 3)
			f.What = plural(latest.Minor-cur.Minor, "minor") + " behind " + up.Version + ", have " + r.Version
		default:
			f.Severity = Info
			f.What = "patch " + up.Version + " available, have " + r.Version
		}
		if up.Guessed {
			f.Fix += " (upstream guessed, pin in charts.yaml if wrong)"
		}
		out = append(out, f)
	}
	return out
}

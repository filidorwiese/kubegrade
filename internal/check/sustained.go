package check

import (
	"context"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/filidorwiese/kubegrade/internal/collect"
)

type crashLoop struct{}

// crashLoop groups crash-looping pods by owner. A single run cannot measure
// how long the loop has lasted, so restart count stands in for duration.
func (crashLoop) Run(_ context.Context, s *collect.Snapshot) []Finding {
	type group struct {
		pods     int
		restarts int32
		reasons  reasonCount
	}
	groups := map[string]*group{}
	owned := map[string]int{}
	for _, p := range s.Pods {
		owner := ownerOf(p, s.ReplicaSets)
		owned[owner]++
		looping := false
		var restarts int32
		var reasons reasonCount
		for _, cs := range p.Status.ContainerStatuses {
			if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
				looping = true
				restarts += cs.RestartCount
				reasons.add(cs)
			}
		}
		if !looping {
			continue
		}
		g := groups[owner]
		if g == nil {
			g = &group{}
			groups[owner] = g
		}
		g.pods++
		g.restarts += restarts
		g.reasons.merge(reasons)
	}

	var out []Finding
	for owner, g := range groups {
		sev := Medium
		if g.restarts >= 20 {
			sev = High
		}
		reason := g.reasons.top()
		out = append(out, Finding{ID: "pod-crashloop", Category: Health, Severity: sev, Resource: owner,
			What: "CrashLoopBackOff " + fmtInt(g.pods) + "/" + fmtInt(owned[owner]) + " pods, " + fmtInt(int(g.restarts)) + " restarts" + reason.suffix(),
			Fix:  reason.fix("check pod logs")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Resource < out[j].Resource })
	return out
}

// ownerOf resolves pod -> ReplicaSet -> Deployment, else the direct owner.
func ownerOf(p corev1.Pod, rss []appsv1.ReplicaSet) string {
	if len(p.OwnerReferences) == 0 {
		return "pod " + p.Namespace + "/" + p.Name
	}
	ref := p.OwnerReferences[0]
	if ref.Kind != "ReplicaSet" {
		return kindPrefix(ref.Kind) + " " + p.Namespace + "/" + ref.Name
	}
	for _, rs := range rss {
		if rs.Namespace != p.Namespace || rs.Name != ref.Name {
			continue
		}
		for _, rref := range rs.OwnerReferences {
			if rref.Kind == "Deployment" {
				return "deploy " + p.Namespace + "/" + rref.Name
			}
		}
	}
	return "rs " + p.Namespace + "/" + ref.Name
}

func kindPrefix(kind string) string {
	switch kind {
	case "StatefulSet":
		return "sts"
	case "DaemonSet":
		return "ds"
	case "Job":
		return "job"
	}
	return kind
}

// podRestarts flags workloads whose pods restart without being in
// CrashLoopBackOff (those are covered by crashLoop). Restart count is
// cumulative, so recency of the last termination decides the severity.
type podRestarts struct{}

const restartThreshold = 5

func (podRestarts) Run(_ context.Context, s *collect.Snapshot) []Finding {
	type group struct {
		restarts int32
		pods     int
		last     time.Time
		reasons  reasonCount
	}
	groups := map[string]*group{}
	var order []string
	for _, p := range s.Pods {
		var restarts int32
		var last time.Time
		var reasons reasonCount
		looping := false
		for _, cs := range p.Status.ContainerStatuses {
			if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
				looping = true
			}
			restarts += cs.RestartCount
			reasons.add(cs)
			if t := cs.LastTerminationState.Terminated; t != nil && t.FinishedAt.Time.After(last) {
				last = t.FinishedAt.Time
			}
		}
		if looping || restarts == 0 {
			continue
		}
		owner := ownerOf(p, s.ReplicaSets)
		g := groups[owner]
		if g == nil {
			g = &group{}
			groups[owner] = g
			order = append(order, owner)
		}
		g.restarts += restarts
		g.pods++
		g.reasons.merge(reasons)
		if last.After(g.last) {
			g.last = last
		}
	}
	var out []Finding
	for _, owner := range order {
		g := groups[owner]
		if g.restarts < restartThreshold {
			continue
		}
		// Counts are cumulative for the pod's lifetime, so a day without a
		// restart means the incident is over: keep it visible, stop grading it.
		sev := Info
		what := fmtInt(int(g.restarts)) + " restarts across " + plural(g.pods, "pod")
		if !g.last.IsZero() {
			ago := s.ScannedAt.Sub(g.last)
			what += ", last " + humanDuration(ago) + " ago"
			if ago < 24*time.Hour {
				sev = Medium
			}
		}
		reason := g.reasons.top()
		// "Unknown" means the node went away (reboot, kured), not the app.
		// Restarts older than a week are history, not a current problem.
		if reason == "Unknown" || (!g.last.IsZero() && s.ScannedAt.Sub(g.last) > 7*24*time.Hour) {
			continue
		}
		out = append(out, Finding{ID: "pod-restarts", Category: Health, Severity: sev, Resource: owner,
			What: what + reason.suffix(), Fix: reason.fix("check pod logs and probes")})
	}
	return out
}

// reasonCount tallies why containers last terminated. Only the most recent
// termination per container is known, so this is a sample, not history.
type reasonCount map[string]int

func (rc *reasonCount) add(cs corev1.ContainerStatus) {
	t := cs.LastTerminationState.Terminated
	if t == nil {
		return
	}
	r := t.Reason
	if r == "" || (r == "Error" && t.ExitCode != 0) {
		r = "exit " + fmtInt(int(t.ExitCode))
	}
	if *rc == nil {
		*rc = reasonCount{}
	}
	(*rc)[r]++
}

func (rc *reasonCount) merge(o reasonCount) {
	for r, n := range o {
		if *rc == nil {
			*rc = reasonCount{}
		}
		(*rc)[r] += n
	}
}

func (rc reasonCount) top() termReason {
	best, n := "", 0
	for r, c := range rc {
		if c > n || (c == n && r < best) {
			best, n = r, c
		}
	}
	return termReason(best)
}

type termReason string

func (r termReason) suffix() string {
	if r == "" {
		return ""
	}
	return " (" + string(r) + ")"
}

// fix prefers a targeted hint for the reasons that have an obvious one.
func (r termReason) fix(fallback string) string {
	switch r {
	case "OOMKilled":
		return "raise the memory limit or fix the leak"
	case "exit 137":
		return "killed (SIGKILL): OOM or probe timeout, check limits"
	}
	return fallback
}

// pullFailures are container waiting reasons that mean the image never
// arrives; they hold a pod in Pending forever with no scheduling problem.
var pullFailures = map[string]bool{"ImagePullBackOff": true, "ErrImagePull": true, "InvalidImageName": true}

// stuckImage returns the first container image that cannot be pulled.
func stuckImage(p corev1.Pod) (image, reason string) {
	all := append(p.Status.InitContainerStatuses, p.Status.ContainerStatuses...)
	for _, cs := range all {
		if cs.State.Waiting != nil && pullFailures[cs.State.Waiting.Reason] {
			return cs.Image, cs.State.Waiting.Reason
		}
	}
	return "", ""
}

type imagePull struct{}

func (imagePull) Run(_ context.Context, s *collect.Snapshot) []Finding {
	type group struct {
		pods          int
		image, reason string
	}
	groups := map[string]*group{}
	var order []string
	for _, p := range s.Pods {
		image, reason := stuckImage(p)
		if image == "" {
			continue
		}
		owner := ownerOf(p, s.ReplicaSets)
		g := groups[owner]
		if g == nil {
			g = &group{image: image, reason: reason}
			groups[owner] = g
			order = append(order, owner)
		}
		g.pods++
	}
	var out []Finding
	for _, owner := range order {
		g := groups[owner]
		out = append(out, Finding{ID: "image-pull", Category: Health, Severity: Medium, Resource: owner,
			What: "cannot pull " + shortImage(g.image) + " on " + plural(g.pods, "pod") + " (" + g.reason + ")",
			Fix:  "check image name, tag and pull secret"})
	}
	return out
}

type podPending struct{}

func (podPending) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, p := range s.Pods {
		if p.Status.Phase != corev1.PodPending {
			continue
		}
		if img, _ := stuckImage(p); img != "" {
			continue // imagePull names the real cause
		}
		since := p.CreationTimestamp.Time
		dur := s.ScannedAt.Sub(since)
		if dur < time.Hour {
			continue
		}
		out = append(out, Finding{ID: "pod-pending", Category: Health, Severity: Medium,
			Resource: "pod " + p.Namespace + "/" + p.Name, What: "Pending for " + humanDuration(dur),
			Fix: "check scheduling and resources", Since: &since})
	}
	return out
}

type deployUnavailable struct{}

func (deployUnavailable) Run(_ context.Context, s *collect.Snapshot) []Finding {
	// A crash-looping or unpullable deployment is unavailable by definition;
	// the check naming the cause is the one finding.
	looping := map[string]bool{}
	for _, p := range s.Pods {
		if img, _ := stuckImage(p); img != "" {
			looping[ownerOf(p, s.ReplicaSets)] = true
		}
		for _, cs := range p.Status.ContainerStatuses {
			if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
				looping[ownerOf(p, s.ReplicaSets)] = true
			}
		}
	}
	var out []Finding
	for _, d := range s.Deployments {
		if looping["deploy "+d.Namespace+"/"+d.Name] {
			continue
		}
		want := int32(1)
		if d.Spec.Replicas != nil {
			want = *d.Spec.Replicas
		}
		if want == 0 || d.Status.AvailableReplicas >= want {
			continue
		}
		since := d.CreationTimestamp.Time
		for _, c := range d.Status.Conditions {
			if c.Type == appsv1.DeploymentAvailable {
				since = c.LastTransitionTime.Time
			}
		}
		dur := s.ScannedAt.Sub(since)
		sev := Low
		if dur > 24*time.Hour {
			sev = Medium
		}
		out = append(out, Finding{ID: "deploy-unavailable", Category: Health, Severity: sev,
			Resource: "deploy " + d.Namespace + "/" + d.Name,
			What:     fmtInt(int(d.Status.AvailableReplicas)) + "/" + fmtInt(int(want)) + " available for " + humanDuration(dur),
			Fix:      "check rollout", Since: &since})
	}
	return out
}

type pvcUsage struct{}

func (pvcUsage) Run(_ context.Context, s *collect.Snapshot) []Finding {
	seen := map[string]bool{}
	var out []Finding
	for _, v := range s.VolumeStats {
		pct := int(v.Used * 100 / v.Capacity)
		if pct < 90 {
			continue
		}
		key := v.Namespace + "/" + v.PVC
		if seen[key] {
			continue // same PVC mounted by several pods
		}
		seen[key] = true
		sev := Medium
		if pct >= 95 {
			sev = High
		}
		out = append(out, Finding{ID: "pvc-usage", Category: Health, Severity: sev,
			Resource: "pvc " + key, What: fmtInt(pct) + "% used",
			Fix: "expand the volume or prune data"})
	}
	return out
}

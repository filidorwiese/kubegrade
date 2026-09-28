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

func (crashLoop) ID() string       { return "pod-crashloop" }
func (crashLoop) Category() string { return Health }

// crashLoop groups crash-looping pods by owner. A single run cannot measure
// how long the loop has lasted, so restart count stands in for duration.
func (crashLoop) Run(_ context.Context, s *collect.Snapshot) []Finding {
	type group struct {
		pods, total int
		restarts    int32
	}
	groups := map[string]*group{}
	owned := map[string]int{}
	for _, p := range s.Pods {
		owner := ownerOf(p, s.ReplicaSets)
		owned[owner]++
		looping := false
		var restarts int32
		for _, cs := range p.Status.ContainerStatuses {
			if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
				looping = true
				restarts += cs.RestartCount
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
	}

	var out []Finding
	for owner, g := range groups {
		sev := Medium
		if g.restarts >= 20 {
			sev = High
		}
		out = append(out, Finding{ID: "pod-crashloop", Category: Health, Severity: sev, Resource: owner,
			What: "CrashLoopBackOff " + fmtInt(g.pods) + "/" + fmtInt(owned[owner]) + " pods, " + fmtInt(int(g.restarts)) + " restarts",
			Fix:  "kubectl logs -p"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Resource < out[j].Resource })
	return out
}

// ownerOf resolves pod -> ReplicaSet -> Deployment, else the direct owner.
func ownerOf(p corev1.Pod, rss []appsv1.ReplicaSet) string {
	for _, ref := range p.OwnerReferences {
		if ref.Kind == "ReplicaSet" {
			for _, rs := range rss {
				if rs.Namespace == p.Namespace && rs.Name == ref.Name {
					for _, rref := range rs.OwnerReferences {
						if rref.Kind == "Deployment" {
							return "deploy " + p.Namespace + "/" + rref.Name
						}
					}
				}
			}
			return "rs " + p.Namespace + "/" + ref.Name
		}
		return kindPrefix(ref.Kind) + " " + p.Namespace + "/" + ref.Name
	}
	return "pod " + p.Namespace + "/" + p.Name
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

type podPending struct{}

func (podPending) ID() string       { return "pod-pending" }
func (podPending) Category() string { return Health }

func (podPending) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, p := range s.Pods {
		if p.Status.Phase != corev1.PodPending {
			continue
		}
		since := p.CreationTimestamp.Time
		dur := s.ScannedAt.Sub(since)
		if dur < time.Hour {
			continue
		}
		out = append(out, Finding{ID: "pod-pending", Category: Health, Severity: Medium,
			Resource: "pod " + p.Namespace + "/" + p.Name, What: "Pending for " + humanDuration(dur),
			Fix: "kubectl describe pod", Since: &since})
	}
	return out
}

type deployUnavailable struct{}

func (deployUnavailable) ID() string       { return "deploy-unavailable" }
func (deployUnavailable) Category() string { return Health }

func (deployUnavailable) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, d := range s.Deployments {
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
			Fix:      "kubectl rollout status", Since: &since})
	}
	return out
}

type pvcUsage struct{}

func (pvcUsage) ID() string       { return "pvc-usage" }
func (pvcUsage) Category() string { return Health }

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

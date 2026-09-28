package check

import (
	"context"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/filidorwiese/kubegrade/internal/collect"
)

type cruft struct{}

// cruft finds pods nobody cleaned up. Each kind is one row: the fix is one
// sweep, not one action per pod, so the count is shown but not scored.
func (cruft) Run(_ context.Context, s *collect.Snapshot) []Finding {
	// CronJobs prune their own history, so their finished pods are expected.
	cron := map[string]bool{}
	for _, j := range s.Jobs {
		for _, o := range j.OwnerReferences {
			if o.Kind == "CronJob" {
				cron[j.Namespace+"/"+j.Name] = true
			}
		}
	}
	evicted := map[string]int{}
	finished := map[string]int{}
	bare := map[string]int{}
	for _, p := range s.Pods {
		done := p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed
		switch {
		case p.Status.Reason == "Evicted":
			evicted[p.Namespace]++
		case done && !cronOwned(p, cron) && s.ScannedAt.Sub(p.CreationTimestamp.Time) > 7*24*time.Hour:
			finished[p.Namespace]++
		case !done && len(p.OwnerReferences) == 0 && p.Namespace != "kube-system":
			bare[p.Namespace]++
		}
	}
	var out []Finding
	if n := sum(evicted); n > 0 {
		out = append(out, Finding{ID: "pods-evicted", Category: Hygiene, Severity: Low,
			Resource: "pods", What: plural(n, "evicted pod") + " left in " + nsList(evicted),
			Fix: "delete them, they hold nothing"})
	}
	if n := sum(finished); n > 0 {
		out = append(out, Finding{ID: "pods-finished", Category: Hygiene, Severity: Low,
			Resource: "pods", What: plural(n, "finished pod") + " older than 7d in " + nsList(finished),
			Fix: "set ttlSecondsAfterFinished on jobs"})
	}
	if n := sum(bare); n > 0 {
		out = append(out, Finding{ID: "pods-bare", Category: Hygiene, Severity: Low,
			Resource: "pods", What: plural(n, "pod") + " without a controller in " + nsList(bare),
			Fix: "delete leftovers or manage them with a deployment"})
	}
	return out
}

func cronOwned(p corev1.Pod, cron map[string]bool) bool {
	for _, o := range p.OwnerReferences {
		if o.Kind == "Job" && cron[p.Namespace+"/"+o.Name] {
			return true
		}
	}
	return false
}

func sum(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// nsList names up to three namespaces, most affected first.
func nsList(m map[string]int) string {
	names := make([]string, 0, len(m))
	for ns := range m {
		names = append(names, ns)
	}
	sort.Slice(names, func(i, j int) bool {
		if m[names[i]] != m[names[j]] {
			return m[names[i]] > m[names[j]]
		}
		return names[i] < names[j]
	})
	if len(names) > 3 {
		return names[0] + ", " + names[1] + ", " + names[2] + " +" + fmtInt(len(names)-3)
	}
	out := names[0]
	for _, n := range names[1:] {
		out += ", " + n
	}
	return out
}

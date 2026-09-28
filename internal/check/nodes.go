package check

import (
	"context"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/filidorwiese/kubegrade/internal/collect"
	"github.com/filidorwiese/kubegrade/internal/data"
)

type kubeletSkew struct{}

func (kubeletSkew) Run(_ context.Context, s *collect.Snapshot) []Finding {
	server := minorInt(minor(s.ServerVersion))
	var out []Finding
	for _, n := range s.Nodes {
		kv := n.Status.NodeInfo.KubeletVersion
		behind := server - minorInt(minor(kv))
		if behind <= 0 {
			continue
		}
		sev := Medium
		if behind > 2 {
			sev = High
		}
		out = append(out, Finding{ID: "kubelet-skew", Category: Versions, Severity: sev,
			Resource: "node " + n.Name, What: "kubelet " + trimV(kv) + " is " + plural(behind, "minor") + " behind API server",
			Fix: "upgrade node"})
	}
	return out
}

type kernelEOL struct{}

// shortKernel drops the distro build suffix: "6.12.63+deb13-amd64" -> "6.12.63".
func shortKernel(v string) string {
	if i := strings.IndexAny(v, "+"); i >= 0 {
		return v[:i]
	}
	return v
}

func (kernelEOL) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		kv := n.Status.NodeInfo.KernelVersion
		f := Finding{ID: "kernel-eol", Category: Versions, Resource: "node " + n.Name}
		k, ok := s.Tables.Kernel.Find(minor(kv))
		if !ok {
			continue // non-LTS: reported on the node-info line
		}
		eol, ok := data.ParseDate(k.EOL)
		if !ok {
			continue
		}
		left := eol.Sub(s.ScannedAt)
		switch {
		case left < 0:
			f.Severity, f.What = Medium, "kernel "+kv+" unsupported since "+k.EOL
		case left < 180*24*time.Hour:
			f.Severity, f.What = Low, "kernel "+kv+" end of support "+k.EOL+" ("+fmtInt(days(left))+" days)"
		default:
			continue
		}
		f.Fix = "upgrade kernel"
		out = append(out, f)
	}
	return out
}

type osEOL struct{}

func (osEOL) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		img := n.Status.NodeInfo.OSImage
		f := Finding{ID: "os-eol", Category: Versions, Resource: "node " + n.Name}
		d, ok := s.Tables.OS.Match(img)
		if !ok {
			continue // unknown OS: reported on the node-info line
		}
		eol, ok := data.ParseDate(d.EOL)
		if !ok || eol.After(s.ScannedAt) {
			continue
		}
		f.Severity, f.What, f.Fix = Medium, img+" unsupported since "+d.EOL, "upgrade OS"
		out = append(out, f)
	}
	return out
}

// nodeInfo prints what the nodes run. Nodes with identical OS, kernel and
// runtime share one line, so a uniform cluster is one row and any odd one
// out stands alone next to its node-drift finding.
type nodeInfo struct{}

func (nodeInfo) Run(_ context.Context, s *collect.Snapshot) []Finding {
	type group struct {
		what  string
		nodes []string
	}
	var order []string
	groups := map[string]*group{}
	for _, n := range s.Nodes {
		ni := n.Status.NodeInfo
		os := ni.OSImage
		if _, ok := s.Tables.OS.Match(os); !ok {
			os += " (not in OS table)"
		}
		kernel := "kernel " + shortKernel(ni.KernelVersion)
		if k, ok := s.Tables.Kernel.Find(minor(ni.KernelVersion)); ok {
			kernel += " LTS until " + k.EOL
		} else {
			kernel += " not LTS"
		}
		what := os + ", " + kernel
		g, ok := groups[what]
		if !ok {
			g = &group{what: what}
			groups[what] = g
			order = append(order, what)
		}
		g.nodes = append(g.nodes, n.Name)
	}
	majority := 0
	for _, g := range groups {
		majority = max(majority, len(g.nodes))
	}
	var out []Finding
	for _, what := range order {
		g := groups[what]
		if len(g.nodes) < majority {
			continue // odd ones out are reported by node-drift, not twice
		}
		res := "node " + g.nodes[0]
		if len(g.nodes) > 1 {
			res = fmtInt(len(g.nodes)) + " nodes"
		}
		if up := uptime(s, g.nodes); up != "" {
			what += ", up " + up
		}
		out = append(out, Finding{ID: "node-info", Category: Versions, Severity: Info, Resource: res, What: what})
	}
	return out
}

// uptime renders the node uptime, as a range when the group spans several
// nodes. Empty when the kubelet stats gave no start time.
func uptime(s *collect.Snapshot, nodes []string) string {
	var lo, hi time.Duration
	found := false
	for _, n := range nodes {
		t, ok := s.NodeStart[n]
		if !ok {
			continue
		}
		d := s.ScannedAt.Sub(t)
		if !found || d < lo {
			lo = d
		}
		if !found || d > hi {
			hi = d
		}
		found = true
	}
	switch {
	case !found:
		return ""
	case humanDuration(lo) == humanDuration(hi):
		return humanDuration(lo)
	default:
		return humanDuration(lo) + " to " + humanDuration(hi)
	}
}

// nodeDrift flags nodes whose kernel, OS, kubelet or runtime differs from
// the majority. Kernel drift is rated by the size of the gap; a long uptime
// on the odd node means it has been skipping reboots and bumps the
// severity one level. Skipped on single-node clusters.
type nodeDrift struct{}

func (nodeDrift) Run(_ context.Context, s *collect.Snapshot) []Finding {
	if len(s.Nodes) < 2 {
		return nil
	}
	fields := []struct {
		label string
		get   func(corev1.NodeSystemInfo) string
	}{
		{"kernel", func(i corev1.NodeSystemInfo) string { return shortKernel(i.KernelVersion) }},
		{"OS", func(i corev1.NodeSystemInfo) string { return i.OSImage }},
		{"kubelet", func(i corev1.NodeSystemInfo) string { return i.KubeletVersion }},
		{"runtime", func(i corev1.NodeSystemInfo) string { return i.ContainerRuntimeVersion }},
	}
	var out []Finding
	for _, f := range fields {
		counts := map[string]int{}
		for _, n := range s.Nodes {
			counts[f.get(n.Status.NodeInfo)]++
		}
		if len(counts) < 2 {
			continue
		}
		majority, majorityN := "", 0
		for v, n := range counts {
			if n > majorityN || (n == majorityN && v > majority) {
				majority, majorityN = v, n
			}
		}
		for _, n := range s.Nodes {
			v := f.get(n.Status.NodeInfo)
			if v == majority {
				continue
			}
			what := f.label + " " + v + " differs from " + plural(majorityN, "node") + " on " + majority
			sev := Low
			if f.label == "kernel" {
				sev = kernelGapSeverity(v, majority)
			}
			if t, ok := s.NodeStart[n.Name]; ok {
				up := s.ScannedAt.Sub(t)
				what += ", up " + humanDuration(up)
				if up > 30*24*time.Hour {
					sev = bump(sev)
				}
			}
			out = append(out, Finding{ID: "node-drift", Category: Hygiene, Severity: sev,
				Resource: "node " + n.Name, What: what, Fix: "pending reboot or upgrade"})
		}
	}
	return out
}

// kernelGapSeverity: a different major.minor series is a different LTS
// branch (high); within a series, patch releases land about weekly, so
// more than 20 behind is months of missed fixes (medium).
func kernelGapSeverity(have, want string) Severity {
	a, okA := data.ParseVersion(have)
	b, okB := data.ParseVersion(want)
	switch {
	case !okA || !okB:
		return Low
	case a.Major != b.Major || a.Minor != b.Minor:
		return High
	case b.Patch-a.Patch > 20:
		return Medium
	}
	return Low
}

func bump(s Severity) Severity {
	switch s {
	case Low:
		return Medium
	case Medium:
		return High
	case High:
		return Critical
	}
	return s
}

type nodeNotReady struct{}

func (nodeNotReady) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		for _, c := range n.Status.Conditions {
			if c.Type != corev1.NodeReady || c.Status == corev1.ConditionTrue {
				continue
			}
			since := c.LastTransitionTime.Time
			dur := s.ScannedAt.Sub(since)
			sev := Medium
			if dur > 24*time.Hour {
				sev = High
			}
			out = append(out, Finding{ID: "node-notready", Category: Health, Severity: sev,
				Resource: "node " + n.Name, What: "NotReady for " + humanDuration(dur) + " (" + strings.TrimSpace(c.Reason) + ")",
				Fix: "check node", Since: &since})
		}
	}
	return out
}

type nodePressure struct{}

// nodePressure reports kubelet pressure conditions. Disk usage itself is
// graded by nodeDisk; a pressure flag means eviction is already happening.
func (nodePressure) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		for _, c := range n.Status.Conditions {
			if c.Status != corev1.ConditionTrue || !pressureFix[c.Type] {
				continue
			}
			since := c.LastTransitionTime.Time
			out = append(out, Finding{ID: "node-pressure", Category: Health, Severity: High,
				Resource: "node " + n.Name, What: string(c.Type) + " for " + humanDuration(s.ScannedAt.Sub(since)),
				Fix: "pods are being evicted, free " + pressureWhat[c.Type], Since: &since})
		}
	}
	return out
}

var pressureFix = map[corev1.NodeConditionType]bool{
	corev1.NodeDiskPressure: true, corev1.NodeMemoryPressure: true, corev1.NodePIDPressure: true,
}

var pressureWhat = map[corev1.NodeConditionType]string{
	corev1.NodeDiskPressure: "disk", corev1.NodeMemoryPressure: "memory", corev1.NodePIDPressure: "processes",
}

type nodeDisk struct{}

// nodeDisk grades the node root and image filesystems like PVCs. A node
// already under DiskPressure is reported by nodePressure instead.
func (nodeDisk) Run(_ context.Context, s *collect.Snapshot) []Finding {
	pressured := map[string]bool{}
	for _, n := range s.Nodes {
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeDiskPressure && c.Status == corev1.ConditionTrue {
				pressured[n.Name] = true
			}
		}
	}
	var out []Finding
	for _, fs := range s.NodeFS {
		pct := int(fs.Used * 100 / fs.Capacity)
		if pct < 90 || pressured[fs.Node] {
			continue
		}
		sev := Medium
		if pct >= 95 {
			sev = High
		}
		fix := "free disk space"
		if fs.Kind == "image" {
			fix = "prune unused images"
		}
		out = append(out, Finding{ID: "node-disk", Category: Health, Severity: sev,
			Resource: "node " + fs.Node, What: fs.Kind + " disk " + fmtInt(pct) + "% used", Fix: fix})
	}
	return out
}

type nodeCordoned struct{}

func (nodeCordoned) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		if n.Spec.Unschedulable {
			out = append(out, Finding{ID: "node-cordoned", Category: Hygiene, Severity: Low,
				Resource: "node " + n.Name, What: "cordoned", Fix: "uncordon after maintenance or remove the node"})
		}
	}
	return out
}

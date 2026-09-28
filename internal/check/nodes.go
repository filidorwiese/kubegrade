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

func (kubeletSkew) ID() string       { return "kubelet-skew" }
func (kubeletSkew) Category() string { return UpToDate }

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
		out = append(out, Finding{ID: "kubelet-skew", Category: UpToDate, Severity: sev,
			Resource: "node " + n.Name, What: "kubelet " + trimV(kv) + " is " + plural(behind, "minor") + " behind API server",
			Fix: "upgrade node"})
	}
	return out
}

type kernelEOL struct{}

func (kernelEOL) ID() string       { return "kernel-eol" }
func (kernelEOL) Category() string { return UpToDate }

func (kernelEOL) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		kv := n.Status.NodeInfo.KernelVersion
		f := Finding{ID: "kernel-eol", Category: UpToDate, Resource: "node " + n.Name}
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

func (osEOL) ID() string       { return "os-eol" }
func (osEOL) Category() string { return UpToDate }

func (osEOL) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		img := n.Status.NodeInfo.OSImage
		f := Finding{ID: "os-eol", Category: UpToDate, Resource: "node " + n.Name}
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

// nodeInfo is one info line per node with OS, kernel and runtime, so the
// report always shows what the node runs even when nothing is wrong.
type nodeInfo struct{}

func (nodeInfo) ID() string       { return "node-info" }
func (nodeInfo) Category() string { return UpToDate }

func (nodeInfo) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		ni := n.Status.NodeInfo
		os := ni.OSImage
		if _, ok := s.Tables.OS.Match(os); !ok {
			os += " (not in OS table)"
		}
		kernel := "kernel " + ni.KernelVersion
		if k, ok := s.Tables.Kernel.Find(minor(ni.KernelVersion)); ok {
			kernel += " LTS until " + k.EOL
		} else {
			kernel += " not LTS"
		}
		out = append(out, Finding{ID: "node-info", Category: UpToDate, Severity: Info,
			Resource: "node " + n.Name, What: os + ", " + kernel + ", " + ni.ContainerRuntimeVersion})
	}
	return out
}

// nodeDrift flags nodes whose kernel, OS, kubelet or runtime differs from
// the majority. Skipped on single-node clusters.
type nodeDrift struct{}

func (nodeDrift) ID() string       { return "node-drift" }
func (nodeDrift) Category() string { return Hygiene }

func (nodeDrift) Run(_ context.Context, s *collect.Snapshot) []Finding {
	if len(s.Nodes) < 2 {
		return nil
	}
	fields := []struct {
		label string
		get   func(corev1.NodeSystemInfo) string
	}{
		{"kernel", func(i corev1.NodeSystemInfo) string { return i.KernelVersion }},
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
			out = append(out, Finding{ID: "node-drift", Category: Hygiene, Severity: Low,
				Resource: "node " + n.Name,
				What:     f.label + " " + v + " differs from " + plural(majorityN, "node") + " on " + majority,
				Fix:      "pending reboot or upgrade"})
		}
	}
	return out
}

type nodeNotReady struct{}

func (nodeNotReady) ID() string       { return "node-notready" }
func (nodeNotReady) Category() string { return Health }

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
				Fix: "kubectl describe node", Since: &since})
		}
	}
	return out
}

package check

import (
	"context"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/filidorwiese/kubegrade/internal/collect"
	"github.com/filidorwiese/kubegrade/internal/data"
	"github.com/filidorwiese/kubegrade/internal/state"
)

type kubeletSkew struct{}

func (kubeletSkew) ID() string       { return "kubelet-skew" }
func (kubeletSkew) Category() string { return Nodes }

func (kubeletSkew) Run(_ context.Context, s *collect.Snapshot, _ *state.Store) []Finding {
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
		out = append(out, Finding{ID: "kubelet-skew", Category: Nodes, Severity: sev,
			Resource: "node " + n.Name, What: "kubelet " + trimV(kv) + " is " + plural(behind, "minor") + " behind API server",
			Fix: "upgrade node"})
	}
	return out
}

type kernelEOL struct{}

func (kernelEOL) ID() string       { return "kernel-eol" }
func (kernelEOL) Category() string { return Nodes }

func (kernelEOL) Run(_ context.Context, s *collect.Snapshot, _ *state.Store) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		kv := n.Status.NodeInfo.KernelVersion
		f := Finding{ID: "kernel-eol", Category: Nodes, Resource: "node " + n.Name}
		k, ok := s.Tables.Kernel.Find(minor(kv))
		if !ok {
			f.Severity, f.What = Info, "kernel "+kv+" is not an LTS kernel"
			out = append(out, f)
			continue
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
func (osEOL) Category() string { return Nodes }

func (osEOL) Run(_ context.Context, s *collect.Snapshot, _ *state.Store) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		img := n.Status.NodeInfo.OSImage
		f := Finding{ID: "os-eol", Category: Nodes, Resource: "node " + n.Name}
		d, ok := s.Tables.OS.Match(img)
		if !ok {
			f.Severity, f.What = Info, "unknown OS: "+img
			out = append(out, f)
			continue
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

type runtimeVersion struct{}

func (runtimeVersion) ID() string       { return "runtime-version" }
func (runtimeVersion) Category() string { return Nodes }

func (runtimeVersion) Run(_ context.Context, s *collect.Snapshot, _ *state.Store) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		out = append(out, Finding{ID: "runtime-version", Category: Nodes, Severity: Info,
			Resource: "node " + n.Name, What: "runtime " + n.Status.NodeInfo.ContainerRuntimeVersion})
	}
	return out
}

type nodeNotReady struct{}

func (nodeNotReady) ID() string       { return "node-notready" }
func (nodeNotReady) Category() string { return Nodes }

func (nodeNotReady) Run(_ context.Context, s *collect.Snapshot, _ *state.Store) []Finding {
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
			out = append(out, Finding{ID: "node-notready", Category: Nodes, Severity: sev,
				Resource: "node " + n.Name, What: "NotReady for " + humanDuration(dur) + " (" + strings.TrimSpace(c.Reason) + ")",
				Fix: "kubectl describe node", Since: &since})
		}
	}
	return out
}

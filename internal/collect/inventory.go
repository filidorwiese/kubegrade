package collect

import (
	"sort"
	"strings"
)

// Inventory summarises what the cluster runs, for the report header.
type Inventory struct {
	Version      string  `json:"version"`
	ControlPlane int     `json:"control_plane"`
	Workers      int     `json:"workers"`
	OS           []Count `json:"os"`
	Kernels      []Count `json:"kernels"`
}

// Count is a value and how many nodes have it, most common first.
type Count struct {
	Value string `json:"value"`
	Nodes int    `json:"nodes"`
}

func (s *Snapshot) Inventory() Inventory {
	inv := Inventory{Version: s.ServerVersion}
	os, kernels := map[string]int{}, map[string]int{}
	for _, n := range s.Nodes {
		if _, cp := n.Labels["node-role.kubernetes.io/control-plane"]; cp {
			inv.ControlPlane++
		} else if _, master := n.Labels["node-role.kubernetes.io/master"]; master {
			inv.ControlPlane++
		} else {
			inv.Workers++
		}
		os[n.Status.NodeInfo.OSImage]++
		kernels[ShortKernel(n.Status.NodeInfo.KernelVersion)]++
	}
	inv.OS, inv.Kernels = counts(os), counts(kernels)
	return inv
}

func counts(m map[string]int) []Count {
	out := make([]Count, 0, len(m))
	for v, n := range m {
		out = append(out, Count{v, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Nodes != out[j].Nodes {
			return out[i].Nodes > out[j].Nodes
		}
		return out[i].Value < out[j].Value
	})
	return out
}

// ShortKernel drops the distro suffix: "6.12.107+deb13-amd64" -> "6.12.107".
func ShortKernel(v string) string {
	if i := strings.IndexAny(v, "+"); i >= 0 {
		return v[:i]
	}
	return v
}

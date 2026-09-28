package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// statsSummary is the slice of the kubelet /stats/summary we need.
type statsSummary struct {
	Node struct {
		StartTime time.Time `json:"startTime"`
		Fs        fsStats   `json:"fs"`
		Runtime   struct {
			ImageFs fsStats `json:"imageFs"`
		} `json:"runtime"`
	} `json:"node"`
	Pods []struct {
		Volume []struct {
			UsedBytes     uint64 `json:"usedBytes"`
			CapacityBytes uint64 `json:"capacityBytes"`
			PVCRef        *struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"pvcRef"`
		} `json:"volume"`
	} `json:"pods"`
}

type fsStats struct {
	UsedBytes     uint64 `json:"usedBytes"`
	CapacityBytes uint64 `json:"capacityBytes"`
}

// kubeletStats reads node disks, PVC usage and node start time through
// nodes/proxy. One failing node is recorded and skipped; the rest still report.
func (c *Collector) kubeletStats(ctx context.Context, s *Snapshot) error {
	s.NodeStart = map[string]time.Time{}
	for _, node := range s.Nodes {
		raw, err := c.cs.CoreV1().RESTClient().Get().
			Resource("nodes").Name(node.Name).
			SubResource("proxy").Suffix("stats/summary").
			DoRaw(ctx)
		if err != nil {
			s.Errors = append(s.Errors, fmt.Sprintf("kubelet stats %s: %v", node.Name, err))
			continue
		}
		var sum statsSummary
		if err := json.Unmarshal(raw, &sum); err != nil {
			s.Errors = append(s.Errors, fmt.Sprintf("kubelet stats %s: %v", node.Name, err))
			continue
		}
		if !sum.Node.StartTime.IsZero() {
			s.NodeStart[node.Name] = sum.Node.StartTime
		}
		for kind, fs := range map[string]fsStats{"root": sum.Node.Fs, "image": sum.Node.Runtime.ImageFs} {
			if fs.CapacityBytes > 0 {
				s.NodeFS = append(s.NodeFS, NodeFS{Node: node.Name, Kind: kind, Used: fs.UsedBytes, Capacity: fs.CapacityBytes})
			}
		}
		for _, p := range sum.Pods {
			for _, v := range p.Volume {
				if v.PVCRef == nil || v.CapacityBytes == 0 {
					continue
				}
				s.VolumeStats = append(s.VolumeStats, VolumeStat{
					Node: node.Name, Namespace: v.PVCRef.Namespace, PVC: v.PVCRef.Name,
					Used: v.UsedBytes, Capacity: v.CapacityBytes,
				})
			}
		}
	}
	return nil
}

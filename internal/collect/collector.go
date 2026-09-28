package collect

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/filidorwiese/kubegrade/internal/data"
	"github.com/filidorwiese/kubegrade/internal/progress"
)

type Collector struct {
	cs     kubernetes.Interface
	dyn    dynamic.Interface
	tables *data.Tables
	log    *slog.Logger
	report progress.Func
}

func New(cfg *rest.Config, tables *data.Tables, log *slog.Logger, report progress.Func) (*Collector, error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Collector{cs: cs, dyn: dyn, tables: tables, log: log, report: report}, nil
}

// Collect builds the snapshot. Core list calls are fatal; everything else
// records an error and moves on so one broken collector never hides a report.
func (c *Collector) Collect(ctx context.Context) (*Snapshot, error) {
	s := &Snapshot{ScannedAt: time.Now().UTC(), Tables: c.tables}
	all := metav1.ListOptions{}
	const phase = "collecting cluster data"
	step, total := 0, 9
	tick := func() {
		step++
		c.report(phase, step, total)
	}
	c.report(phase, 0, total)

	v, err := c.cs.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("server version: %w", err)
	}
	s.ServerVersion = v.GitVersion
	tick()

	nodes, err := c.cs.CoreV1().Nodes().List(ctx, all)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	s.Nodes = nodes.Items
	tick()

	pods, err := c.cs.CoreV1().Pods("").List(ctx, all)
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	s.Pods = pods.Items
	tick()

	deps, err := c.cs.AppsV1().Deployments("").List(ctx, all)
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	s.Deployments = deps.Items
	tick()

	rss, err := c.cs.AppsV1().ReplicaSets("").List(ctx, all)
	if err != nil {
		return nil, fmt.Errorf("list replicasets: %w", err)
	}
	s.ReplicaSets = rss.Items
	tick()

	pvcs, err := c.cs.CoreV1().PersistentVolumeClaims("").List(ctx, all)
	if err != nil {
		return nil, fmt.Errorf("list pvcs: %w", err)
	}
	s.PVCs = pvcs.Items
	tick()

	c.try(s, "helm releases", func() error { return c.helmReleases(ctx, s) })
	tick()
	c.try(s, "kubelet stats", func() error { return c.kubeletStats(ctx, s) })
	tick()
	c.try(s, "deprecated apis", func() error { return c.deprecatedAPIs(ctx, s) })
	tick()
	c.try(s, "chart upstream", func() error { return c.chartUpstream(ctx, s) })
	return s, nil
}

func (c *Collector) try(s *Snapshot, name string, fn func() error) {
	if err := fn(); err != nil {
		c.log.Warn("collector failed", "collector", name, "err", err)
		s.Errors = append(s.Errors, name+": "+err.Error())
	}
}

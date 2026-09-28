// Command kubegrade scans the cluster in your kubeconfig once and prints
// findings plus a letter grade to stdout. It fetches public EOL tables from
// endoflife.date and Helm repo indexes; nothing about the cluster is sent.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/filidorwiese/kubegrade/internal/check"
	"github.com/filidorwiese/kubegrade/internal/collect"
	"github.com/filidorwiese/kubegrade/internal/data"
	"github.com/filidorwiese/kubegrade/internal/grade"
	"github.com/filidorwiese/kubegrade/internal/report"
)

// version is set with -ldflags "-X main.version=..."
var version = "dev"

type options struct {
	kubeconfig  string
	format      string
	clusterName string
}

func main() {
	var o options
	flag.StringVar(&o.kubeconfig, "kubeconfig", "", "path to kubeconfig; defaults to $KUBECONFIG or ~/.kube/config")
	flag.StringVar(&o.format, "format", "text", "output format: text or json")
	flag.StringVar(&o.clusterName, "cluster-name", "", "cluster name in the report")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if o.format != "text" && o.format != "json" {
		fmt.Fprintln(os.Stderr, "--format must be text or json")
		os.Exit(2)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(o, log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(o options, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, ctxName, err := loadConfig(o.kubeconfig)
	if err != nil {
		return err
	}
	if o.clusterName == "" {
		o.clusterName = ctxName
	}

	tables, err := data.Load(ctx)
	if err != nil {
		return fmt.Errorf("load EOL tables: %w", err)
	}
	collector, err := collect.New(cfg, tables, log)
	if err != nil {
		return err
	}
	return scan(ctx, o, collector)
}

func scan(ctx context.Context, o options, c *collect.Collector) error {
	start := time.Now()
	snap, err := c.Collect(ctx)
	if err != nil {
		return err
	}
	findings := check.Run(ctx, snap)
	result := grade.Compute(findings)

	r := report.Build(report.Input{
		Agent: version, Cluster: o.clusterName, ScannedAt: snap.ScannedAt, Duration: time.Since(start),
		Findings: findings, Result: result, Errors: snap.Errors,
	})
	if o.format == "json" {
		return report.WriteJSON(os.Stdout, r)
	}
	return report.WriteText(os.Stdout, r)
}

// loadConfig loads the kubeconfig. The second return is the cluster name
// fallback: the current context name.
func loadConfig(path string) (*rest.Config, string, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = path
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	raw, err := cc.RawConfig()
	if err != nil {
		return nil, "", err
	}
	cfg, err := cc.ClientConfig()
	if err != nil {
		return nil, "", err
	}
	name := raw.CurrentContext
	if name == "" {
		name = "unknown"
	}
	return cfg, name, nil
}

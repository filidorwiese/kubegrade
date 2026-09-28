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

	"golang.org/x/term"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/filidorwiese/kubegrade/internal/check"
	"github.com/filidorwiese/kubegrade/internal/collect"
	"github.com/filidorwiese/kubegrade/internal/data"
	"github.com/filidorwiese/kubegrade/internal/grade"
	"github.com/filidorwiese/kubegrade/internal/progress"
	"github.com/filidorwiese/kubegrade/internal/report"
)

// version is set with -ldflags "-X main.version=..."
var version = "dev"

type options struct {
	kubeconfig  string
	format      string
	clusterName string
	noColor     bool
	verbose     bool
}

func main() {
	var o options
	flag.StringVar(&o.kubeconfig, "kubeconfig", "", "path to kubeconfig; defaults to $KUBECONFIG or ~/.kube/config")
	flag.StringVar(&o.format, "format", "text", "output format: text or json")
	flag.StringVar(&o.clusterName, "cluster-name", "", "cluster name in the report")
	flag.BoolVar(&o.noColor, "no-color", false, "disable coloured output (NO_COLOR env also works)")
	flag.BoolVar(&o.verbose, "v", false, "show info findings")
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

	fmt.Fprintf(os.Stderr, "cluster: %s (%s)\n", o.clusterName, cfg.Host)
	if v := newerRelease(ctx); v != "" {
		fmt.Fprintf(os.Stderr, "update available: %s (running %s), https://github.com/filidorwiese/kubegrade/releases/latest\n", v, version)
	}
	bar, tick := progress.New()
	defer bar.Done()

	start := time.Now()
	tables, err := data.Load(ctx, tick)
	if err != nil {
		return fmt.Errorf("load EOL tables: %w", err)
	}
	collector, err := collect.New(cfg, tables, log, tick)
	if err != nil {
		return err
	}
	snap, err := collector.Collect(ctx)
	if err != nil {
		return err
	}
	bar.Done()
	findings := check.Run(ctx, snap)
	result := grade.Compute(findings)

	rep := report.Build(report.Input{
		Agent: version, Cluster: o.clusterName, ScannedAt: snap.ScannedAt, Duration: time.Since(start),
		Findings: findings, Result: result, Errors: snap.Errors,
	})
	if o.format == "json" {
		return report.WriteJSON(os.Stdout, rep)
	}
	return report.WriteText(os.Stdout, rep, report.TextOptions{Color: useColor(o), Width: termWidth(), Verbose: o.verbose})
}

// termWidth is the stdout terminal width, or 0 when stdout is not a tty.
func termWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0
	}
	return w
}

// useColor is on for a terminal stdout unless --no-color or NO_COLOR is set.
func useColor(o options) bool {
	if o.noColor || os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
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

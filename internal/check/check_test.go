package check

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/filidorwiese/kubegrade/internal/collect"
	"github.com/filidorwiese/kubegrade/internal/data"
)

var now = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func day(n int) string { return now.AddDate(0, 0, n).Format(data.DateLayout) }

func snap() *collect.Snapshot {
	return &collect.Snapshot{
		ScannedAt:     now,
		ServerVersion: "v1.34.4+k3s1",
		NodeStart:     map[string]time.Time{},
		Tables: &data.Tables{Kubernetes: data.Kubernetes{Latest: "1.37", Versions: []data.K8sVersion{
			{Minor: "1.33", EOL: day(-10)}, {Minor: "1.34", EOL: day(50)}, {Minor: "1.35", EOL: day(100)},
			{Minor: "1.36", EOL: day(200)}, {Minor: "1.37", EOL: day(300)},
		}}},
	}
}

func run(c Check, s *collect.Snapshot) []Finding { return c.Run(context.Background(), s) }

func sevs(fs []Finding) map[string]Severity {
	out := map[string]Severity{}
	for _, f := range fs {
		out[f.ID] = f.Severity
	}
	return out
}

func TestRunAddsOKRows(t *testing.T) {
	s := snap()
	s.ServerVersion = "v1.37.0"
	ids := map[string]bool{}
	for _, f := range Run(context.Background(), s) {
		ids[f.ID] = true
	}
	for _, e := range All() {
		if e.what != "" && !ids[e.id] {
			t.Errorf("%s: no row on a clean snapshot", e.id)
		}
	}
}

func TestK8sVersion(t *testing.T) {
	cases := []struct {
		server string
		eol    Severity
		behind Severity
		count  int
	}{
		{"v1.33.0", High, Low, 3},   // unsupported, 4 behind capped at 3
		{"v1.34.4", Medium, Low, 2}, // 50 days left, 3 behind
		{"v1.35.0", Low, Low, 1},    // 100 days left
		{"v1.36.0", "", Info, 0},    // one behind is normal
		{"v1.37.0", "", "", 0},
	}
	for _, c := range cases {
		s := snap()
		s.ServerVersion = c.server
		fs := run(k8sVersion{}, s)
		got := sevs(fs)
		if got["k8s-version-eol"] != c.eol || got["k8s-version-behind"] != c.behind {
			t.Errorf("%s: %v", c.server, got)
		}
		for _, f := range fs {
			if f.ID == "k8s-version-behind" && f.Count != c.count {
				t.Errorf("%s: behind count %d, want %d", c.server, f.Count, c.count)
			}
		}
	}
	if fs := run(k8sVersion{}, snap()); fs[0].Link != "https://github.com/k3s-io/k3s/releases" {
		t.Errorf("k3s link: %q", fs[0].Link)
	}
}

func TestNewerTag(t *testing.T) {
	tags := []string{"latest", "6", "5.2.2", "5.2.2-apache", "5.10", "5.9", "5.2", "5", "fpm", "abc"}
	cases := []struct {
		cur, want string
		seg       int
	}{
		{"5.2.1", "5.2.2", 2},
		{"5.2.1-apache", "5.2.2-apache", 2},
		{"5.2.1-fpm", "", 0},  // no such variant upstream
		{"5.9", "5.10", 1},    // numeric, not lexical
		{"5", "6", 0},         // floating major only moves on a major
		{"6.0.0", "", 0},      // current
		{"fpm", "", 0},        // no digits, nothing to compare
		{"4.9.9", "5.2.2", 0}, // same shape only, 5.10 is #.#
		{"4.9", "5.10", 0},
	}
	for _, c := range cases {
		got, seg, ok := newerTag(c.cur, tags)
		if got != c.want || (ok && seg != c.seg) {
			t.Errorf("%s: got %q seg %d, want %q seg %d", c.cur, got, seg, c.want, c.seg)
		}
	}
}

func TestImageOutdatedSkipsHelmAndBundled(t *testing.T) {
	s := snap()
	helm := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "web", Name: "chart",
		Labels: map[string]string{"app.kubernetes.io/managed-by": "Helm"}}}
	helm.Spec.Template.Spec.Containers = []corev1.Container{{Image: "phpmyadmin:5.2.1"}}
	sts := appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Namespace: "db", Name: "pma"}}
	sts.Spec.Template.Spec.Containers = []corev1.Container{{Image: "docker.io/phpmyadmin:5.2.1@sha256:abc"}}
	bundled := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "coredns"}}
	bundled.Spec.Template.Spec.Containers = []corev1.Container{{Image: "rancher/mirrored-coredns-coredns:1.14.3"}}
	s.Deployments = []appsv1.Deployment{helm, bundled}
	s.StatefulSets = []appsv1.StatefulSet{sts}
	s.ImageTags = map[string][]string{"library/phpmyadmin": {"5.2.2", "5.2.2-apache"},
		"rancher/mirrored-coredns-coredns": {"1.14.7"}}

	fs := run(imageOutdated{}, s)
	if len(fs) != 1 {
		t.Fatalf("got %+v, want one finding", fs)
	}
	if fs[0].Fix != "bump tag and digest to 5.2.2" || fs[0].What != "patch 5.2.2 available, used by sts db/pma" {
		t.Errorf("got %q / %q", fs[0].What, fs[0].Fix)
	}
	if fs[0].Link != "https://hub.docker.com/_/phpmyadmin" {
		t.Errorf("link: %q", fs[0].Link)
	}
}

func TestImageTagsDedupePerImage(t *testing.T) {
	dep := func(ns, name string, imgs ...string) appsv1.Deployment {
		d := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
		for _, img := range imgs {
			d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, corev1.Container{Image: img})
		}
		return d
	}
	s := snap()
	s.Deployments = []appsv1.Deployment{
		dep("web", "app", "nginx:latest"),
		dep("web", "api", "nginx:latest", "redis:1.2"),
		dep("cache", "redis", "redis"),
	}
	var latest []Finding
	for _, f := range run(imageTags{}, s) {
		if f.ID == "image-tag-latest" {
			latest = append(latest, f)
		}
	}
	if len(latest) != 2 {
		t.Fatalf("got %d findings, want 2 (one per image): %+v", len(latest), latest)
	}
	if latest[0].Resource != "image nginx:latest" || latest[0].What != "used by deploy web/app, deploy web/api" {
		t.Errorf("nginx: %q / %q", latest[0].Resource, latest[0].What)
	}
	if latest[1].Resource != "image redis" || latest[1].What != "used by deploy cache/redis" {
		t.Errorf("redis: %q / %q", latest[1].Resource, latest[1].What)
	}
}

func TestChartOutdated(t *testing.T) {
	s := snap()
	s.HelmReleases = []collect.HelmRelease{
		{Namespace: "a", Name: "major", Chart: "major", Version: "1.0.0"},
		{Namespace: "a", Name: "minor", Chart: "minor", Version: "1.0.0"},
		{Namespace: "a", Name: "patch", Chart: "patch", Version: "1.0.0"},
		{Namespace: "a", Name: "old", Chart: "old", Version: "1.0.0"},
		{Namespace: "a", Name: "gone", Chart: "gone", Version: "1.0.0"},
	}
	s.ChartLatest = map[string]collect.ChartUpstream{
		"major": {Version: "2.0.0", Source: "https://src"},
		"minor": {Version: "1.5.0", Guessed: true, Repo: "https://repo"},
		"patch": {Version: "1.0.1", Source: "https://src", Repo: "https://repo"},
		"old":   {Version: "1.0.0", Deprecated: true},
	}
	by := map[string]Finding{}
	for _, f := range run(chartOutdated{}, s) {
		by[f.Resource+" "+f.ID] = f
	}
	if f := by["helm a/major chart-outdated"]; f.Severity != Medium || f.Link != "https://src" {
		t.Errorf("major: %+v", f)
	}
	if f := by["helm a/minor chart-outdated"]; f.Severity != Low || f.Count != 3 || f.Link != "https://repo" {
		t.Errorf("minor: %+v", f)
	}
	if f := by["helm a/patch chart-outdated"]; f.Severity != Info || f.Link != "https://src" {
		t.Errorf("patch: %+v", f)
	}
	if f := by["helm a/old chart-deprecated"]; f.Severity != Medium {
		t.Errorf("deprecated: %+v", f)
	}
	if f := by["helm a/gone chart-unresolved"]; f.Severity != Info {
		t.Errorf("unresolved: %+v", f)
	}
}

func pod(ns, name string, owner *metav1.OwnerReference) corev1.Pod {
	p := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, CreationTimestamp: metav1.NewTime(now.Add(-48 * time.Hour))}}
	if owner != nil {
		p.OwnerReferences = []metav1.OwnerReference{*owner}
	}
	return p
}

func restarted(restarts int32, reason string, ago time.Duration) corev1.ContainerStatus {
	return corev1.ContainerStatus{RestartCount: restarts, LastTerminationState: corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{Reason: reason, FinishedAt: metav1.NewTime(now.Add(-ago))},
	}}
}

// Restart counts are cumulative, so only a recent termination is a live
// problem; node-caused (Unknown) and week-old restarts are skipped.
func TestPodRestarts(t *testing.T) {
	cases := []struct {
		name string
		cs   corev1.ContainerStatus
		want Severity
	}{
		{"recent", restarted(6, "OOMKilled", time.Hour), Medium},
		{"settled", restarted(6, "Error", 3*24*time.Hour), Info},
		{"below-threshold", restarted(4, "Error", time.Hour), ""},
		{"node-reboot", restarted(9, "Unknown", time.Hour), ""},
		{"ancient", restarted(9, "Error", 8*24*time.Hour), ""},
	}
	for _, c := range cases {
		s := snap()
		p := pod("ns", c.name, nil)
		p.Status.ContainerStatuses = []corev1.ContainerStatus{c.cs}
		s.Pods = []corev1.Pod{p}
		if got := sevs(run(podRestarts{}, s))["pod-restarts"]; got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	s := snap()
	p := pod("ns", "oom", nil)
	p.Status.ContainerStatuses = []corev1.ContainerStatus{restarted(6, "OOMKilled", time.Hour)}
	s.Pods = []corev1.Pod{p}
	if fs := run(podRestarts{}, s); fs[0].Fix != "raise the memory limit or fix the leak" {
		t.Errorf("OOM fix: %q", fs[0].Fix)
	}
}

func TestCruftSkipsCronJobHistory(t *testing.T) {
	s := snap()
	s.Jobs = []batchv1.Job{{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "backup-1",
		OwnerReferences: []metav1.OwnerReference{{Kind: "CronJob", Name: "backup"}}}}}
	old := metav1.NewTime(now.Add(-8 * 24 * time.Hour))
	cron := pod("ns", "backup-1-x", &metav1.OwnerReference{Kind: "Job", Name: "backup-1"})
	cron.CreationTimestamp, cron.Status.Phase = old, corev1.PodSucceeded
	manual := pod("ns", "once-x", &metav1.OwnerReference{Kind: "Job", Name: "once"})
	manual.CreationTimestamp, manual.Status.Phase = old, corev1.PodSucceeded
	evicted := pod("ns", "ev", nil)
	evicted.Status.Reason = "Evicted"
	bare := pod("ns", "debug", nil)
	bare.Status.Phase = corev1.PodRunning
	sys := pod("kube-system", "static", nil)
	sys.Status.Phase = corev1.PodRunning
	terminating := pod("ns", "web-x", &metav1.OwnerReference{Kind: "ReplicaSet", Name: "web"})
	terminating.CreationTimestamp, terminating.Status.Phase = old, corev1.PodSucceeded
	terminating.DeletionTimestamp = &metav1.Time{Time: now}
	longRun := pod("ns", "once-y", &metav1.OwnerReference{Kind: "Job", Name: "once"})
	longRun.CreationTimestamp, longRun.Status.Phase = old, corev1.PodSucceeded
	longRun.Status.ContainerStatuses = []corev1.ContainerStatus{{State: corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{FinishedAt: metav1.NewTime(now.Add(-time.Hour))}}}}
	s.Pods = []corev1.Pod{cron, manual, evicted, bare, sys, terminating, longRun}

	got := map[string]string{}
	for _, f := range run(cruft{}, s) {
		got[f.ID] = f.What
	}
	if got["pods-finished"] != "1 finished pod older than 7d in ns" {
		t.Errorf("finished: %q", got["pods-finished"])
	}
	if got["pods-evicted"] != "1 evicted pod left in ns" || got["pods-bare"] != "1 pod without a controller in ns" {
		t.Errorf("got %v", got)
	}
}

func TestCertExpiry(t *testing.T) {
	cert := func(kind string, days int, managed, referenced bool) collect.Cert {
		return collect.Cert{Kind: kind, Name: "c", Namespace: "ns", NotAfter: now.AddDate(0, 0, days), Managed: managed, Referenced: referenced}
	}
	cases := []struct {
		name string
		cert collect.Cert
		id   string
		want Severity
	}{
		{"apiserver-soon", cert("apiserver", 5, false, false), "cert-expiry", Critical},
		{"apiserver-month", cert("apiserver", 20, false, false), "cert-expiry", High},
		{"apiserver-fine", cert("apiserver", 60, false, false), "", ""},
		{"unused-expired", cert("secret", -1, false, false), "cert-unused", Low},
		{"unused-expiring", cert("secret", 3, false, false), "", ""},
		{"managed-renewing", cert("secret", 20, true, true), "", ""},
		{"managed-failed", cert("secret", 5, true, true), "cert-expiry", High},
		{"manual-month", cert("secret", 20, false, true), "cert-expiry", Medium},
		{"manual-week", cert("secret", 5, false, true), "cert-expiry", High},
	}
	for _, c := range cases {
		s := snap()
		s.Certs = []collect.Cert{c.cert}
		fs := run(certExpiry{}, s)
		switch {
		case c.id == "" && len(fs) != 0:
			t.Errorf("%s: unexpected %+v", c.name, fs)
		case c.id != "" && (len(fs) != 1 || fs[0].ID != c.id || fs[0].Severity != c.want):
			t.Errorf("%s: %+v", c.name, fs)
		}
	}
}

func node(name, kernel, os string) corev1.Node {
	n := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}
	n.Status.NodeInfo = corev1.NodeSystemInfo{KernelVersion: kernel, OSImage: os, KubeletVersion: "v1.34.4+k3s1", ContainerRuntimeVersion: "containerd://2.0"}
	return n
}

// Nodes behind the newest kernel are one row per version; a different
// kernel series is high. The OS image is compared against the majority.
func TestNodeDrift(t *testing.T) {
	s := snap()
	s.Nodes = []corev1.Node{
		node("a", "6.12.107+deb13-amd64", "Debian 13"),
		node("b", "6.12.107+deb13-amd64", "Debian 13"),
		node("c", "6.12.80+deb13-amd64", "Debian 13"),
		node("d", "6.6.10", "Debian 12"),
	}
	s.NodeStart["d"] = now.Add(-40 * 24 * time.Hour)
	got := map[string]Finding{}
	for _, f := range run(nodeDrift{}, s) {
		got[f.Resource+" "+f.What[:min(len(f.What), 6)]] = f
	}
	want := map[string]struct {
		sev  Severity
		what string
	}{
		"node c kernel": {Medium, "kernel 6.12.80 version drift, 2 nodes already on 6.12.107"}, // 27 patches behind
		"node d kernel": {High, "kernel 6.6.10 version drift, 2 nodes already on 6.12.107"},    // other series
		"node d OS Deb": {Low, "OS Debian 12 differs from 3 nodes on Debian 13"},
	}
	for k, w := range want {
		if f := got[k]; f.Severity != w.sev || f.What != w.what {
			t.Errorf("%s: %q %q, want %q %q", k, f.Severity, f.What, w.sev, w.what)
		}
	}
	if len(got) != 3 {
		t.Errorf("got %d findings: %v", len(got), got)
	}
	if got["node c kernel"].Fix != "reboot onto 6.12.107" {
		t.Errorf("fix: %q", got["node c kernel"].Fix)
	}
	s.Nodes = s.Nodes[:1]
	if fs := run(nodeDrift{}, s); len(fs) != 0 {
		t.Error("single node cannot drift")
	}
}

// One freshly rebooted node on a newer kernel means the others are behind,
// not the new one. They share a row, and a held kured lock names the node
// being rebooted.
func TestNodeDriftNewest(t *testing.T) {
	s := snap()
	s.Nodes = []corev1.Node{
		node("old1", "6.12.107", "Debian 13"), node("old2", "6.12.107", "Debian 13"), node("new", "6.12.111", "Debian 13"),
	}
	s.Nodes[2].Status.NodeInfo.ContainerRuntimeVersion = "containerd://2.1"
	s.NodeStart["new"] = now.Add(-4 * time.Hour)
	s.NodeStart["old1"] = now.Add(-12 * 24 * time.Hour)
	s.NodeStart["old2"] = now.Add(-13 * 24 * time.Hour)

	fs := run(nodeDrift{}, s)
	if len(fs) != 2 {
		t.Fatalf("got %d findings: %+v", len(fs), fs)
	}
	k := fs[0]
	if k.Resource != "2 nodes" || k.Severity != Low || k.Count != 2 || k.Fix != "reboot onto 6.12.111" ||
		k.What != "kernel 6.12.107 version drift, 1 node already on 6.12.111 for 4h" {
		t.Errorf("kernel: %+v", k)
	}
	if r := fs[1]; r.What != "runtime 2.0 version drift, 1 node already on 2.1 for 4h" || r.Fix != "upgrade runtime to 2.1" {
		t.Errorf("runtime: %+v", r)
	}

	s.DaemonSets = []appsv1.DaemonSet{{ObjectMeta: metav1.ObjectMeta{Name: "kured", Namespace: "kube-system",
		Annotations: map[string]string{"weave.works/kured-node-lock": `{"nodeID":"old2","created":"2026-09-30T08:00:00Z"}`}}}}
	fs = run(nodeDrift{}, s)
	if !strings.HasSuffix(fs[0].What, ", kured rebooting old2") || strings.Contains(fs[1].What, "kured") {
		t.Errorf("kured lock: %q / %q", fs[0].What, fs[1].What)
	}
	s.DaemonSets[0].Annotations["weave.works/kured-node-lock"] = `{"maxOwners":2,"locks":[{"nodeID":"old1"},{"nodeID":"old2"}]}`
	if fs = run(nodeDrift{}, s); !strings.HasSuffix(fs[0].What, ", kured rebooting old1, old2") {
		t.Errorf("multi lock: %q", fs[0].What)
	}
}

func TestNodeDiskDefersToPressure(t *testing.T) {
	s := snap()
	s.Nodes = []corev1.Node{node("a", "6.12.1", "x"), node("b", "6.12.1", "x")}
	s.Nodes[1].Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue}}
	s.NodeFS = []collect.NodeFS{
		{Node: "a", Kind: "root", Used: 91, Capacity: 100},
		{Node: "a", Kind: "image", Used: 96, Capacity: 100},
		{Node: "b", Kind: "root", Used: 99, Capacity: 100},
	}
	fs := run(nodeDisk{}, s)
	if len(fs) != 2 || fs[0].Severity != Medium || fs[1].Severity != High || fs[1].Fix != "prune unused images" {
		t.Errorf("got %+v", fs)
	}
	if len(run(nodePressure{}, s)) != 1 {
		t.Error("pressured node must be reported by nodePressure")
	}
}

// A deployment whose pods crash or cannot pull is reported by the check
// naming the cause, not twice.
func TestDeployUnavailableSuppressed(t *testing.T) {
	s := snap()
	one := int32(1)
	deploy := func(name string) appsv1.Deployment {
		d := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name, CreationTimestamp: metav1.NewTime(now.Add(-2 * 24 * time.Hour))}}
		d.Spec.Replicas = &one
		return d
	}
	s.Deployments = []appsv1.Deployment{deploy("crash"), deploy("pull"), deploy("plain")}
	s.ReplicaSets = []appsv1.ReplicaSet{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "crash-rs", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "crash"}}}},
		{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "pull-rs", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "pull"}}}},
	}
	crash := pod("ns", "crash-1", &metav1.OwnerReference{Kind: "ReplicaSet", Name: "crash-rs"})
	crash.Status.ContainerStatuses = []corev1.ContainerStatus{{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}
	pull := pod("ns", "pull-1", &metav1.OwnerReference{Kind: "ReplicaSet", Name: "pull-rs"})
	pull.Status.Phase = corev1.PodPending
	pull.Status.ContainerStatuses = []corev1.ContainerStatus{{Image: "x/y:z", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}}}
	s.Pods = []corev1.Pod{crash, pull}

	fs := run(deployUnavailable{}, s)
	if len(fs) != 1 || fs[0].Resource != "deploy ns/plain" || fs[0].Severity != Medium {
		t.Errorf("got %+v", fs)
	}
	if fs := run(imagePull{}, s); len(fs) != 1 || fs[0].Resource != "deploy ns/pull" {
		t.Errorf("imagePull: %+v", fs)
	}
	if fs := run(podPending{}, s); len(fs) != 0 {
		t.Errorf("pending must yield to imagePull: %+v", fs)
	}
	if fs := run(crashLoop{}, s); len(fs) != 1 || fs[0].Resource != "deploy ns/crash" {
		t.Errorf("crashLoop: %+v", fs)
	}
}

// Uptime alone is a weak signal, so it starts at info; a newer patch
// release in the running series that the node never loaded makes it low.
func TestNodeUptime(t *testing.T) {
	s := snap()
	s.Tables.Kernel = data.Kernel{Kernels: []data.KernelVersion{
		{Version: "6.12", LTS: true, Latest: "6.12.110", LatestDate: day(-45)},
		{Version: "6.8", Latest: "6.8.12", LatestDate: day(-45)},
		{Version: "6.6", LTS: true, Latest: "6.6.50", LatestDate: day(-5)},
	}}
	s.Nodes = []corev1.Node{
		node("fresh", "6.12.100+deb13-amd64", "Debian"),
		node("month", "6.12.110+deb13-amd64", "Debian"),
		node("stale", "6.12.100+deb13-amd64", "Debian"),
		node("ubuntu", "6.8.0-45-generic", "Ubuntu"),
		node("recent-release", "6.6.40-talos", "Talos"),
		node("long", "6.12.110+deb13-amd64", "Debian"),
		node("unknown", "6.12.100", "x"),
	}
	for name, days := range map[string]int{"fresh": 10, "month": 40, "stale": 40, "ubuntu": 40, "recent-release": 40, "long": 130} {
		s.NodeStart[name] = now.AddDate(0, 0, -days)
	}
	got := map[string]Finding{}
	for _, f := range run(nodeUptime{}, s) {
		got[f.Resource] = f
	}
	want := map[string]struct {
		sev  Severity
		what string
	}{
		"node month":          {Info, "up 40d without a reboot"},
		"node stale":          {Low, "up 40d without a reboot, 6.12.110 out since " + day(-45)},
		"node ubuntu":         {Info, "up 40d without a reboot"},
		"node recent-release": {Info, "up 40d without a reboot"},
		"node long":           {Low, "up 130d without a reboot"},
	}
	if len(got) != len(want) {
		t.Errorf("got %d findings, want %d: %v", len(got), len(want), got)
	}
	for res, w := range want {
		if f := got[res]; f.Severity != w.sev || f.What != w.what {
			t.Errorf("%s: %q %q, want %q %q", res, f.Severity, f.What, w.sev, w.what)
		}
	}
}

package check

import (
	"context"
	"encoding/json"
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

func (kernelEOL) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		kv := n.Status.NodeInfo.KernelVersion
		f := Finding{ID: "kernel-eol", Category: Versions, Resource: "node " + n.Name}
		k, ok := s.Tables.Kernel.Find(minor(kv))
		if !ok || !k.LTS {
			f.Severity, f.What = Info, "kernel "+kv+" is not an LTS series, support ends with the next release"
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

func (osEOL) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		img := n.Status.NodeInfo.OSImage
		f := Finding{ID: "os-eol", Category: Versions, Resource: "node " + n.Name}
		d, ok := s.Tables.OS.Match(img)
		if !ok {
			f.Severity, f.What, f.Fix = Info, img+" not in the OS table, support unknown", "add it in hack/refresh-data"
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

func uptimeRange(s *collect.Snapshot, nodes []string) (lo, hi time.Duration, ok bool) {
	for _, n := range nodes {
		t, found := s.NodeStart[n]
		if !found {
			continue
		}
		d := s.ScannedAt.Sub(t)
		if !ok || d < lo {
			lo = d
		}
		if !ok || d > hi {
			hi = d
		}
		ok = true
	}
	return lo, hi, ok
}

// nodeDrift flags nodes whose kernel, OS, kubelet or runtime differs from
// the majority. Kernel drift is rated by the size of the gap; a long uptime
// on the odd node means it has been skipping reboots and bumps the
// severity one level. Skipped on single-node clusters.
type nodeDrift struct{}

// driftGroup is one set of nodes sharing a value that is not the cluster's
// reference for that field: the newest version where values are orderable
// (ahead), otherwise the majority.
type driftGroup struct {
	field    string
	value    string
	ref      string
	ahead    bool
	nodes    []string
	refNodes []string
}

var driftFields = []struct {
	label     string
	versioned bool
	get       func(corev1.NodeSystemInfo) string
}{
	{"kernel", true, func(i corev1.NodeSystemInfo) string { return collect.ShortKernel(i.KernelVersion) }},
	{"OS", false, func(i corev1.NodeSystemInfo) string { return i.OSImage }},
	{"kubelet", true, func(i corev1.NodeSystemInfo) string { return i.KubeletVersion }},
	{"runtime", true, func(i corev1.NodeSystemInfo) string { return shortRuntime(i.ContainerRuntimeVersion) }},
}

func driftGroups(s *collect.Snapshot) []driftGroup {
	if len(s.Nodes) < 2 {
		return nil
	}
	var out []driftGroup
	for _, f := range driftFields {
		counts := map[string]int{}
		for _, n := range s.Nodes {
			if _, pending := s.KernelLatest[n.Name]; pending && f.label == "kernel" {
				continue
			}
			counts[f.get(n.Status.NodeInfo)]++
		}
		if len(counts) < 2 {
			continue
		}
		ref, ahead := "", false
		if f.versioned {
			ref, ahead = newestVersion(counts)
		}
		if !ahead {
			ref = majorityVersion(counts)
		}
		var refNodes, order []string
		groups := map[string]*driftGroup{}
		for _, n := range s.Nodes {
			if _, pending := s.KernelLatest[n.Name]; pending && f.label == "kernel" {
				continue // kernel-update names the real target for this node
			}
			v := f.get(n.Status.NodeInfo)
			if v == ref {
				refNodes = append(refNodes, n.Name)
				continue
			}
			g, ok := groups[v]
			if !ok {
				g = &driftGroup{field: f.label, value: v, ref: ref, ahead: ahead}
				groups[v] = g
				order = append(order, v)
			}
			g.nodes = append(g.nodes, n.Name)
		}
		for _, v := range order {
			groups[v].refNodes = refNodes
			out = append(out, *groups[v])
		}
	}
	return out
}

// nodeDrift reports each group of nodes lagging the newest kernel, kubelet
// or runtime in the cluster as one row; the OS image is not orderable, so
// there the odd ones out are measured against the majority. How long the
// node has been up is node-uptime's business.
func (nodeDrift) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	rebooting := kuredRebooting(s)
	kured := kuredInstalled(s)
	for _, g := range driftGroups(s) {
		res := "node " + g.nodes[0]
		if len(g.nodes) > 1 {
			res = "nodes " + strings.Join(g.nodes, ", ")
		}
		peers := plural(len(g.refNodes), "node")
		what, fix := g.field+" "+g.value+" differs from "+peers+" on "+g.ref, "pending reboot or upgrade"
		if g.ahead {
			what = g.field + " " + g.value + " version drift, " + peers + " already on " + g.ref
			if _, hi, ok := uptimeRange(s, g.refNodes); ok {
				what += " for " + humanDuration(hi)
			}
			fix = "upgrade " + g.field + " to " + g.ref
			if g.field == "kernel" {
				fix = "reboot onto " + g.ref
			}
		}
		if (g.field == "kernel" || g.field == "OS") && kured {
			fix += " or wait for kured"
		}
		sev := Low
		if g.field == "kernel" {
			sev = kernelGapSeverity(g.value, g.ref)
		}
		if (g.field == "kernel" || g.field == "OS") && len(rebooting) > 0 {
			what += ", kured rebooting " + strings.Join(rebooting, ", ")
		}
		out = append(out, Finding{ID: "node-drift", Category: Versions, Severity: sev, Count: min(len(g.nodes), 3),
			Resource: res, What: what, Fix: fix})
	}
	return out
}

// kuredInstalled is true when a DaemonSet runs a kured image. Kured only
// reboots once the host asks for it, so waiting is a suggestion, not a fix.
func kuredInstalled(s *collect.Snapshot) bool {
	for _, d := range s.DaemonSets {
		for _, c := range d.Spec.Template.Spec.Containers {
			if strings.Contains(c.Image, "kured") {
				return true
			}
		}
	}
	return false
}

// kuredRebooting names the nodes kured is rebooting right now. Kured holds
// a lock as a JSON annotation on its own DaemonSet while a node reboots
// and deletes it afterwards; with --concurrency the value lists several.
func kuredRebooting(s *collect.Snapshot) []string {
	var out []string
	for _, d := range s.DaemonSets {
		raw, ok := d.Annotations["weave.works/kured-node-lock"]
		if !ok {
			continue
		}
		var lock struct {
			NodeID string `json:"nodeID"`
			Locks  []struct {
				NodeID string `json:"nodeID"`
			} `json:"locks"`
		}
		if json.Unmarshal([]byte(raw), &lock) != nil {
			continue
		}
		if lock.NodeID != "" {
			out = append(out, lock.NodeID)
		}
		for _, l := range lock.Locks {
			if l.NodeID != "" {
				out = append(out, l.NodeID)
			}
		}
	}
	return out
}

// newestVersion is the highest parseable version; false when any value
// does not parse, so the caller falls back to the majority.
func newestVersion(counts map[string]int) (string, bool) {
	best, ok := "", false
	var bestV data.Version
	for v := range counts {
		pv, parsed := data.ParseVersion(v)
		if !parsed {
			return "", false
		}
		if !ok || bestV.Less(pv) {
			best, bestV, ok = v, pv, true
		}
	}
	return best, ok
}

func majorityVersion(counts map[string]int) string {
	majority, majorityN := "", 0
	for v, n := range counts {
		if n > majorityN || (n == majorityN && v > majority) {
			majority, majorityN = v, n
		}
	}
	return majority
}

// shortRuntime drops the scheme: "containerd://2.0.5" -> "2.0.5".
func shortRuntime(v string) string {
	if i := strings.Index(v, "://"); i >= 0 {
		return v[i+3:]
	}
	return v
}

// kernelGapSeverity: a different major.minor series is a different LTS
// branch (high); within a series, patch releases land about weekly, so
// more than 20 behind is months of missed fixes (medium). Ubuntu keeps the
// patch at 0 and bumps its ABI counter every few weeks instead.
func kernelGapSeverity(have, want string) Severity {
	a, b := collect.VersionNums(have), collect.VersionNums(want)
	switch {
	case len(a) < 2 || len(b) < 2:
		return Low
	case a[0] != b[0] || a[1] != b[1]:
		return High
	case len(a) > 2 && len(b) > 2 && b[2]-a[2] > 20:
		return Medium
	case len(a) > 3 && len(b) > 3 && b[3]-a[3] > 7:
		return Medium
	}
	return Low
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

type nodeUptime struct{}

// nodeUptime nudges about nodes that have not rebooted in four months.
// Uptime alone proves nothing about missed updates (kernel-update does
// that where a distro feed exists), so this stays informational.
func (nodeUptime) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		start, ok := s.NodeStart[n.Name]
		if !ok {
			continue
		}
		up := s.ScannedAt.Sub(start)
		if up <= 120*24*time.Hour {
			continue
		}
		out = append(out, Finding{ID: "node-uptime", Category: Hygiene, Severity: Info, Resource: "node " + n.Name,
			What: "up " + humanDuration(up) + " without a reboot",
			Fix:  "check for a pending kernel update and reboot if available", Since: &start})
	}
	return out
}

type kernelUpdate struct{}

// kernelUpdate reports nodes whose distro ships a newer kernel for the
// series they run, one row per version pair. Peers already on the newer
// kernel are counted as proof it installs fine.
func (kernelUpdate) Run(_ context.Context, s *collect.Snapshot) []Finding {
	type key struct{ have, latest, source string }
	var order []key
	groups := map[key][]string{}
	for _, n := range s.Nodes {
		up, ok := s.KernelLatest[n.Name]
		if !ok {
			continue
		}
		k := key{up.Have, up.Latest, up.Source}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], n.Name)
	}
	kured := kuredInstalled(s)
	var out []Finding
	for _, k := range order {
		nodes := groups[k]
		res := "node " + nodes[0]
		if len(nodes) > 1 {
			res = "nodes " + strings.Join(nodes, ", ")
		}
		what := "kernel " + k.have + ", " + k.latest + " available in " + k.source
		if n := onKernel(s, k.latest); n > 0 {
			what += ", " + plural(n, "node") + " already on it"
		}
		fix := "upgrade and reboot onto " + k.latest
		if kured {
			fix += " or wait for kured"
		}
		out = append(out, Finding{ID: "kernel-update", Category: Versions, Severity: kernelGapSeverity(k.have, k.latest), Count: min(len(nodes), 3),
			Resource: res, What: what, Fix: fix})
	}
	return out
}

// onKernel counts nodes whose kernel string starts with the version, e.g.
// "6.12.111" matches "6.12.111+deb13-amd64" and "6.8.0-142" matches
// "6.8.0-142-generic".
func onKernel(s *collect.Snapshot, version string) int {
	n := 0
	for _, node := range s.Nodes {
		kv := node.Status.NodeInfo.KernelVersion
		if strings.HasPrefix(kv, version) && (len(kv) == len(version) || !isDigit(kv[len(version)])) {
			n++
		}
	}
	return n
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

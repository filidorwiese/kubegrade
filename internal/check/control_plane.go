package check

import (
	"context"
	"time"

	"github.com/filidorwiese/kubegrade/internal/collect"
	"github.com/filidorwiese/kubegrade/internal/data"
	"github.com/filidorwiese/kubegrade/internal/state"
)

// k8sVersion covers both the EOL window and versions-behind-latest.
type k8sVersion struct{}

func (k8sVersion) ID() string       { return "k8s-version-eol" }
func (k8sVersion) Category() string { return ControlPlane }

func (k8sVersion) Run(_ context.Context, s *collect.Snapshot, _ *state.Store) []Finding {
	tbl := s.Tables.Kubernetes
	cur := minor(s.ServerVersion)
	resource := "kubernetes " + trimV(s.ServerVersion)
	var out []Finding

	v, ok := tbl.Find(cur)
	if !ok {
		return []Finding{{ID: "k8s-version-eol", Category: ControlPlane, Severity: Info,
			Resource: resource, What: "version " + cur + " not in EOL table"}}
	}
	eol, ok := data.ParseDate(v.EOL)
	if ok {
		left := eol.Sub(s.ScannedAt)
		sev, hit := Severity(""), false
		switch {
		case left < 0:
			sev, hit = High, true
		case left < 60*24*time.Hour:
			sev, hit = Medium, true
		case left < 120*24*time.Hour:
			sev, hit = Low, true
		}
		if hit {
			what := "end of support " + v.EOL + " (" + fmtInt(days(left)) + " days)"
			if left < 0 {
				what = "unsupported since " + v.EOL + " (" + fmtInt(-days(left)) + " days ago)"
			}
			out = append(out, Finding{ID: "k8s-version-eol", Category: ControlPlane, Severity: sev,
				Resource: resource, What: what, Fix: "upgrade to " + tbl.Latest})
		}
	}

	// One minor behind is the normal place to be; points start at two.
	behind := minorInt(tbl.Latest) - minorInt(cur)
	if behind > 0 {
		f := Finding{ID: "k8s-version-behind", Category: ControlPlane, Severity: Info,
			Resource: "kubernetes " + cur, What: plural(behind, "minor") + " behind latest known (" + tbl.Latest + ")"}
		if behind > 1 {
			f.Severity, f.Count = Low, min(behind-1, 3)
		}
		out = append(out, f)
	}
	return out
}

type k8sDataStale struct{}

func (k8sDataStale) ID() string       { return "k8s-data-stale" }
func (k8sDataStale) Category() string { return ControlPlane }

func (k8sDataStale) Run(_ context.Context, s *collect.Snapshot, _ *state.Store) []Finding {
	gen := s.Tables.Generated()
	f := Finding{ID: "k8s-data-stale", Category: ControlPlane, Severity: Info, Resource: "data tables"}
	t, ok := data.ParseDate(gen)
	if !ok {
		f.What = "generated date unreadable"
		return []Finding{f}
	}
	age := days(s.ScannedAt.Sub(t))
	f.What = "generated " + gen + " (" + fmtInt(age) + " days old, " + s.Tables.Source + ")"
	if age > 90 {
		f.Severity = Low
		f.Fix = "run hack/refresh-data.sh or use --online"
	}
	return []Finding{f}
}

type k8sDeprecated struct{}

func (k8sDeprecated) ID() string       { return "k8s-api-deprecated" }
func (k8sDeprecated) Category() string { return ControlPlane }

func (k8sDeprecated) Run(_ context.Context, s *collect.Snapshot, _ *state.Store) []Finding {
	next := minorInt(minor(s.ServerVersion)) + 1
	var out []Finding
	for _, d := range s.Deprecated {
		sev := Low
		if minorInt(minor(d.Dep.RemovedIn)) <= next {
			sev = Medium
		}
		res := d.Dep.Kind + " " + d.Name
		if d.Namespace != "" {
			res = d.Dep.Kind + " " + d.Namespace + "/" + d.Name
		}
		out = append(out, Finding{ID: "k8s-api-deprecated", Category: ControlPlane, Severity: sev,
			Resource: res, What: d.Dep.Version + " removed in " + minor(d.Dep.RemovedIn),
			Fix: "migrate to " + d.Dep.ReplacementAPI})
	}
	return out
}

func trimV(v string) string {
	if len(v) > 0 && v[0] == 'v' {
		return v[1:]
	}
	return v
}

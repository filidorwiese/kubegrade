package check

import (
	"context"
	"time"

	"github.com/filidorwiese/kubegrade/internal/collect"
)

type certExpiry struct{}

// certExpiry grades certificate lifetime. cert-manager renews at 30 days
// by default, so its secrets only count once renewal has clearly failed.
func (certExpiry) Run(_ context.Context, s *collect.Snapshot) []Finding {
	var out []Finding
	for _, c := range s.Certs {
		left := c.NotAfter.Sub(s.ScannedAt)
		f := Finding{ID: "cert-expiry", Category: Health, Resource: certResource(c), What: certWhat(c, left)}
		switch {
		case c.Kind == "apiserver":
			f.Fix = "rotate the control plane certificates (k3s renews them on restart)"
			switch {
			case left < 7*24*time.Hour:
				f.Severity = Critical
			case left < 30*24*time.Hour:
				f.Severity = High
			default:
				continue
			}
		case !c.Referenced:
			// Not served by an Ingress: nothing breaks, it is just clutter.
			if left > 0 {
				continue
			}
			f.ID, f.Category, f.Severity = "cert-unused", Hygiene, Low
			f.What += ", not used by an ingress"
			f.Fix = "delete the secret"
		case c.Managed:
			if left >= 7*24*time.Hour {
				continue
			}
			f.Severity = High
			f.Fix = "cert-manager did not renew it, check its Certificate"
		default:
			switch {
			case left < 7*24*time.Hour:
				f.Severity = High
			case left < 30*24*time.Hour:
				f.Severity = Medium
			default:
				continue
			}
			f.Fix = "upload a renewed certificate"
		}
		out = append(out, f)
	}
	for _, i := range s.CertIssues {
		what := "not ready"
		if i.Reason != "" {
			what += " (" + i.Reason + ")"
		}
		out = append(out, Finding{ID: "cert-not-ready", Category: Health, Severity: Medium,
			Resource: "certificate " + i.Namespace + "/" + i.Name, What: what,
			Fix: "check the issuer and its challenge"})
	}
	return out
}

func certResource(c collect.Cert) string {
	if c.Kind == "apiserver" {
		return "apiserver " + c.Name
	}
	return "secret " + c.Namespace + "/" + c.Name
}

func certWhat(c collect.Cert, left time.Duration) string {
	var what string
	if left <= 0 {
		what = "expired " + humanDuration(-left) + " ago"
	} else {
		what = "expires in " + humanDuration(left)
	}
	if c.Subject != "" {
		what += " (" + c.Subject + ")"
	}
	return what
}

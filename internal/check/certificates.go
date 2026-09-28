package check

import (
	"context"

	"github.com/filidorwiese/kubegrade/internal/collect"
	"github.com/filidorwiese/kubegrade/internal/state"
)

type apiServerCert struct{}

func (apiServerCert) ID() string       { return "apiserver-cert-expiry" }
func (apiServerCert) Category() string { return Certificates }

func (apiServerCert) Run(_ context.Context, s *collect.Snapshot, _ *state.Store) []Finding {
	if s.APIServerCert == nil {
		return nil
	}
	sev, ok := expirySeverity(s.APIServerCert.NotAfter.Sub(s.ScannedAt))
	if !ok {
		return nil
	}
	return []Finding{{ID: "apiserver-cert-expiry", Category: Certificates, Severity: sev,
		Resource: "apiserver certificate", What: expiryWhat(s.APIServerCert.NotAfter, s.ScannedAt),
		Fix: "k3s rotates on restart within 90 days of expiry"}}
}

type tlsSecrets struct{}

func (tlsSecrets) ID() string       { return "tls-secret-expiry" }
func (tlsSecrets) Category() string { return Certificates }

func (tlsSecrets) Run(_ context.Context, s *collect.Snapshot, _ *state.Store) []Finding {
	var out []Finding
	for _, sec := range s.TLSSecrets {
		// cert-manager owned secrets are reported via the Certificate CR instead.
		if sec.CertManaged && s.CertManager.Installed {
			continue
		}
		sev, ok := expirySeverity(sec.Cert.NotAfter.Sub(s.ScannedAt))
		if !ok {
			continue
		}
		out = append(out, Finding{ID: "tls-secret-expiry", Category: Certificates, Severity: sev,
			Resource: "secret " + sec.Namespace + "/" + sec.Name, What: expiryWhat(sec.Cert.NotAfter, s.ScannedAt),
			Fix: "renew and update the secret"})
	}
	return out
}

type certManager struct{}

func (certManager) ID() string       { return "certmanager-not-ready" }
func (certManager) Category() string { return Certificates }

func (certManager) Run(_ context.Context, s *collect.Snapshot, _ *state.Store) []Finding {
	var out []Finding
	for _, c := range s.CertManager.Certificates {
		res := "certificate " + c.Namespace + "/" + c.Name
		if !c.Ready {
			out = append(out, Finding{ID: "certmanager-not-ready", Category: Certificates, Severity: Medium,
				Resource: res, What: "not Ready (" + c.ReadyReason + ")", Fix: "kubectl describe certificate"})
		}
		if c.IssuingFailed {
			out = append(out, Finding{ID: "certmanager-issuing-failed", Category: Certificates, Severity: Medium,
				Resource: res, What: "renewal failing (" + c.IssuingReason + ")", Fix: "check issuer and challenges"})
		}
		if c.NotAfter != nil {
			if sev, ok := expirySeverity(c.NotAfter.Sub(s.ScannedAt)); ok {
				out = append(out, Finding{ID: "certmanager-cert-expiry", Category: Certificates, Severity: sev,
					Resource: res, What: expiryWhat(*c.NotAfter, s.ScannedAt), Fix: "cmctl renew"})
			}
		}
	}
	return out
}

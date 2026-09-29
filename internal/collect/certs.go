package collect

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"net/url"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const certManagerAnnotation = "cert-manager.io/certificate-name"

var certificatesGVR = schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}

// certificates reads the API server serving cert from the TLS handshake,
// every kubernetes.io/tls secret, and cert-manager Certificate readiness.
func (c *Collector) certificates(ctx context.Context, s *Snapshot) error {
	if cert, err := serverCert(c.host, c.serverName); err != nil {
		s.Errors = append(s.Errors, "api server cert: "+err.Error())
	} else {
		s.Certs = append(s.Certs, Cert{Kind: "apiserver", Name: c.host, NotAfter: cert.NotAfter, Referenced: true})
	}

	ingresses, err := c.cs.NetworkingV1().Ingresses("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	used := map[string]bool{}
	for _, ing := range ingresses.Items {
		for _, t := range ing.Spec.TLS {
			used[ing.Namespace+"/"+t.SecretName] = true
		}
	}

	secrets, err := c.cs.CoreV1().Secrets("").List(ctx, metav1.ListOptions{FieldSelector: "type=kubernetes.io/tls"})
	if err != nil {
		return err
	}
	for _, sec := range secrets.Items {
		cert := parseCert(sec.Data["tls.crt"])
		if cert == nil {
			continue
		}
		_, managed := sec.Annotations[certManagerAnnotation]
		s.Certs = append(s.Certs, Cert{
			Kind: "secret", Namespace: sec.Namespace, Name: sec.Name, NotAfter: cert.NotAfter,
			Subject: certSubject(cert), Managed: managed, Referenced: used[sec.Namespace+"/"+sec.Name],
		})
	}

	// cert-manager is optional; only a missing CRD is not an error.
	list, err := c.dyn.Resource(certificatesGVR).Namespace("").List(ctx, metav1.ListOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, u := range list.Items {
		conds, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
		for _, raw := range conds {
			cond, _ := raw.(map[string]interface{})
			if cond["type"] == "Ready" && cond["status"] != "True" {
				reason, _ := cond["reason"].(string)
				s.CertIssues = append(s.CertIssues, CertIssue{Namespace: u.GetNamespace(), Name: u.GetName(), Reason: reason})
			}
		}
	}
	return nil
}

// serverCert reads the leaf cert the API server presents. Verification is
// skipped on purpose: the real client already verifies, this only inspects
// the expiry, and an expired cert is exactly what must be visible here.
func serverCert(host, serverName string) (*x509.Certificate, error) {
	u, err := url.Parse(host)
	if err != nil {
		return nil, err
	}
	addr := u.Host
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "443")
	}
	if serverName == "" && net.ParseIP(u.Hostname()) == nil {
		serverName = u.Hostname()
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr,
		&tls.Config{InsecureSkipVerify: true, ServerName: serverName})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, errors.New("no certificate presented")
	}
	return certs[0], nil
}

func parseCert(data []byte) *x509.Certificate {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}
	return cert
}

func certSubject(cert *x509.Certificate) string {
	if len(cert.DNSNames) > 0 {
		return cert.DNSNames[0]
	}
	return cert.Subject.CommonName
}

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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const certManagerAnnotation = "cert-manager.io/certificate-name"

// tlsSecrets lists only kubernetes.io/tls secrets and keeps just the parsed
// leaf certificate; the key material is dropped immediately.
func (c *Collector) tlsSecrets(ctx context.Context, s *Snapshot) error {
	list, err := c.cs.CoreV1().Secrets("").List(ctx, metav1.ListOptions{
		FieldSelector: "type=" + string(corev1.SecretTypeTLS),
	})
	if err != nil {
		return err
	}
	for _, sec := range list.Items {
		cert, err := parseLeaf(sec.Data[corev1.TLSCertKey])
		if err != nil {
			s.Errors = append(s.Errors, "tls secret "+sec.Namespace+"/"+sec.Name+": "+err.Error())
			continue
		}
		_, managed := sec.Annotations[certManagerAnnotation]
		s.TLSSecrets = append(s.TLSSecrets, TLSSecret{
			Namespace: sec.Namespace, Name: sec.Name, Cert: cert, CertManaged: managed,
		})
	}
	return nil
}

func parseLeaf(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM certificate")
	}
	return x509.ParseCertificate(block.Bytes)
}

// apiServerCert dials the host the client already uses and grabs the leaf
// certificate. Verification is skipped on purpose: we only read NotAfter.
func (c *Collector) apiServerCert(ctx context.Context, s *Snapshot) error {
	u, err := url.Parse(c.cfg.Host)
	if err != nil {
		return err
	}
	addr := u.Host
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "443")
	}
	d := tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true}} // #nosec G402 read-only expiry probe
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := d.DialContext(dctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return errors.New("no peer certificate")
	}
	s.APIServerCert = certs[0]
	return nil
}

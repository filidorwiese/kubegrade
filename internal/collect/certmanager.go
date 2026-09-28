package collect

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var certificateGVR = schema.GroupVersionResource{
	Group: "cert-manager.io", Version: "v1", Resource: "certificates",
}

// certManager lists Certificates only when the CRD is served.
func (c *Collector) certManager(ctx context.Context, s *Snapshot) error {
	resources, err := c.cs.Discovery().ServerResourcesForGroupVersion("cert-manager.io/v1")
	if err != nil || resources == nil {
		return nil // not installed
	}
	s.CertManager.Installed = true

	list, err := c.dyn.Resource(certificateGVR).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range list.Items {
		s.CertManager.Certificates = append(s.CertManager.Certificates, parseCertificate(item))
	}
	return nil
}

func parseCertificate(u unstructured.Unstructured) Certificate {
	cert := Certificate{Namespace: u.GetNamespace(), Name: u.GetName()}
	conds, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	for _, raw := range conds {
		cond, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := cond["type"].(string)
		status, _ := cond["status"].(string)
		reason, _ := cond["reason"].(string)
		switch typ {
		case "Ready":
			cert.Ready = status == "True"
			cert.ReadyReason = reason
		case "Issuing":
			cert.IssuingFailed = status == "False"
			cert.IssuingReason = reason
		}
	}
	if na, ok, _ := unstructured.NestedString(u.Object, "status", "notAfter"); ok {
		if t, err := time.Parse(time.RFC3339, na); err == nil {
			cert.NotAfter = &t
		}
	}
	return cert
}

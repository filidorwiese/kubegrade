package collect

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/filidorwiese/kubegrade/internal/data"
)

const lastApplied = "kubectl.kubernetes.io/last-applied-configuration"

// deprecatedAPIs lists every deprecated k8s group/version the server still
// serves. The server converts objects on read, so listing alone proves
// nothing; an object counts only if managedFields or last-applied show it
// was written with the deprecated version.
func (c *Collector) deprecatedAPIs(ctx context.Context, s *Snapshot) error {
	served := map[string]map[string]string{} // groupVersion -> kind -> resource
	for _, dep := range c.tables.Deprecations.Versions {
		if dep.Component != "k8s" {
			continue
		}
		kinds, ok := served[dep.Version]
		if !ok {
			kinds = map[string]string{}
			served[dep.Version] = kinds
			list, err := c.cs.Discovery().ServerResourcesForGroupVersion(dep.Version)
			if err == nil && list != nil {
				for _, r := range list.APIResources {
					if !containsVerb(r.Verbs, "list") {
						continue
					}
					kinds[r.Kind] = r.Name
				}
			}
		}
		resource, ok := kinds[dep.Kind]
		if !ok {
			continue // not served: already removed, nothing can use it
		}
		gv, err := schema.ParseGroupVersion(dep.Version)
		if err != nil {
			continue
		}
		gvr := gv.WithResource(resource)
		items, err := c.dyn.Resource(gvr).Namespace("").List(ctx, metav1.ListOptions{})
		if err != nil {
			s.Errors = append(s.Errors, fmt.Sprintf("list %s %s: %v", dep.Version, dep.Kind, err))
			continue
		}
		for _, u := range items.Items {
			if writtenWith(u, dep.Version) {
				s.Deprecated = append(s.Deprecated, DeprecatedUse{
					Namespace: u.GetNamespace(), Name: u.GetName(), Dep: dep,
				})
			}
		}
	}
	return nil
}

func writtenWith(u unstructured.Unstructured, version string) bool {
	for _, mf := range u.GetManagedFields() {
		if mf.APIVersion == version {
			return true
		}
	}
	if raw, ok := u.GetAnnotations()[lastApplied]; ok {
		var obj struct {
			APIVersion string `json:"apiVersion"`
		}
		if json.Unmarshal([]byte(raw), &obj) == nil && obj.APIVersion == version {
			return true
		}
	}
	return false
}

func containsVerb(verbs []string, verb string) bool {
	for _, v := range verbs {
		if v == verb {
			return true
		}
	}
	return false
}

// K8sDeprecations returns the k8s rows, used by hack/gen-rbac.
func K8sDeprecations(t *data.Tables) []data.Deprecation {
	var out []data.Deprecation
	for _, d := range t.Deprecations.Versions {
		if d.Component == "k8s" {
			out = append(out, d)
		}
	}
	return out
}

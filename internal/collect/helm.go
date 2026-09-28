package collect

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// releaseJSON is the subset of the Helm release payload we read. The Helm
// SDK is deliberately not imported.
type releaseJSON struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Version   int    `json:"version"`
	Info      struct {
		Status       string    `json:"status"`
		LastDeployed time.Time `json:"last_deployed"`
	} `json:"info"`
	Chart struct {
		Metadata struct {
			Name       string `json:"name"`
			Version    string `json:"version"`
			AppVersion string `json:"appVersion"`
		} `json:"metadata"`
	} `json:"chart"`
}

// helmReleases reads only secrets labelled owner=helm of the Helm release
// type. Contents are decoded in memory and never logged.
func (c *Collector) helmReleases(ctx context.Context, s *Snapshot) error {
	list, err := c.cs.CoreV1().Secrets("").List(ctx, metav1.ListOptions{
		LabelSelector: "owner=helm",
		FieldSelector: "type=helm.sh/release.v1",
	})
	if err != nil {
		return err
	}

	type key struct{ ns, name string }
	latest := map[key]*corev1.Secret{}
	count := map[key]int{}
	for i := range list.Items {
		sec := &list.Items[i]
		name, rev, ok := parseReleaseName(sec.Name)
		if !ok {
			continue
		}
		k := key{sec.Namespace, name}
		count[k]++
		if cur, exists := latest[k]; !exists || revisionOf(cur.Name) < rev {
			latest[k] = sec
		}
	}

	for k, sec := range latest {
		rel, err := decodeRelease(sec.Data["release"])
		if err != nil {
			s.Errors = append(s.Errors, "helm "+sec.Namespace+"/"+sec.Name+": "+err.Error())
			continue
		}
		s.HelmReleases = append(s.HelmReleases, HelmRelease{
			Namespace:  k.ns,
			Name:       k.name,
			Revision:   rel.Version,
			Revisions:  count[k],
			Status:     rel.Info.Status,
			Chart:      rel.Chart.Metadata.Name,
			Version:    rel.Chart.Metadata.Version,
			AppVersion: rel.Chart.Metadata.AppVersion,
			Deployed:   rel.Info.LastDeployed,
		})
	}
	return nil
}

// parseReleaseName splits "sh.helm.release.v1.<release>.v<revision>".
func parseReleaseName(secret string) (name string, rev int, ok bool) {
	const prefix = "sh.helm.release.v1."
	if !strings.HasPrefix(secret, prefix) {
		return "", 0, false
	}
	rest := strings.TrimPrefix(secret, prefix)
	i := strings.LastIndex(rest, ".v")
	if i < 0 {
		return "", 0, false
	}
	rev, err := strconv.Atoi(rest[i+2:])
	if err != nil {
		return "", 0, false
	}
	return rest[:i], rev, true
}

func revisionOf(secret string) int {
	_, rev, _ := parseReleaseName(secret)
	return rev
}

// decodeRelease: client-go already base64-decoded Data; the payload is
// base64 again, then gzip, then JSON.
func decodeRelease(raw []byte) (*releaseJSON, error) {
	b, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		return nil, err
	}
	var r io.Reader = bytes.NewReader(b)
	if len(b) > 2 && b[0] == 0x1f && b[1] == 0x8b {
		gz, err := gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		r = gz
	}
	var rel releaseJSON
	if err := json.NewDecoder(r).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

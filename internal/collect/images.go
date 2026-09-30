package collect

import (
	"context"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
)

const (
	dockerHub    = "https://hub.docker.com/v2/repositories"
	imageWorkers = 8
)

// ImageRef is a parsed container image. HubRepo is the Docker Hub
// repository ("library/nginx") when the image lives there, else empty.
type ImageRef struct {
	HubRepo, Tag, Digest string
}

// ParseImage splits registry/name[:tag][@digest]. A colon before the last
// slash belongs to a registry port, not a tag.
func ParseImage(img string) ImageRef {
	var ref ImageRef
	if i := strings.Index(img, "@"); i >= 0 {
		ref.Digest = img[i+1:]
		img = img[:i]
	}
	slash := strings.LastIndex(img, "/")
	if colon := strings.LastIndex(img, ":"); colon > slash {
		ref.Tag = img[colon+1:]
		img = img[:colon]
	}
	ref.HubRepo = hubRepo(img)
	return ref
}

// hubRepo maps an image name to its Docker Hub repository. A first path
// segment with a dot or port is another registry; a bare name is an
// official image under "library".
func hubRepo(name string) string {
	parts := strings.Split(name, "/")
	switch first := parts[0]; {
	case first == "docker.io" || first == "index.docker.io":
		parts = parts[1:]
	case strings.ContainsAny(first, ".:") || first == "localhost":
		return ""
	}
	switch len(parts) {
	case 1:
		return "library/" + parts[0]
	case 2:
		return parts[0] + "/" + parts[1]
	}
	return ""
}

// Workload is a pod-template owner. Helm marks workloads a chart manages;
// their images are the chart's concern.
type Workload struct {
	Kind      string // deploy, sts, ds
	Namespace string
	Name      string
	Helm      bool
	Spec      corev1.PodSpec
}

func (s *Snapshot) Workloads() []Workload {
	var out []Workload
	for _, d := range s.Deployments {
		out = append(out, Workload{"deploy", d.Namespace, d.Name, helmManaged(d.Labels, d.Annotations), d.Spec.Template.Spec})
	}
	for _, d := range s.StatefulSets {
		out = append(out, Workload{"sts", d.Namespace, d.Name, helmManaged(d.Labels, d.Annotations), d.Spec.Template.Spec})
	}
	for _, d := range s.DaemonSets {
		out = append(out, Workload{"ds", d.Namespace, d.Name, helmManaged(d.Labels, d.Annotations), d.Spec.Template.Spec})
	}
	return out
}

func helmManaged(labels, ann map[string]string) bool {
	return labels["app.kubernetes.io/managed-by"] == "Helm" || ann["meta.helm.sh/release-name"] != ""
}

// Images lists init and app container images in order.
func (w Workload) Images() []string {
	var out []string
	for _, c := range w.Spec.InitContainers {
		out = append(out, c.Image)
	}
	for _, c := range w.Spec.Containers {
		out = append(out, c.Image)
	}
	return out
}

type hubTags struct {
	Results []struct {
		Name string `json:"name"`
	} `json:"results"`
}

// imageUpstream fetches the newest-pushed tags for every Docker Hub
// repository used outside Helm. One page is enough: a release pushes all
// its variants together, so the newest of each shape is near the top.
func (c *Collector) imageUpstream(ctx context.Context, s *Snapshot) error {
	s.ImageTags = map[string][]string{}
	var repos []string
	seen := map[string]bool{}
	for _, w := range s.Workloads() {
		if w.Helm {
			continue
		}
		for _, img := range w.Images() {
			ref := ParseImage(img)
			if ref.HubRepo == "" || ref.Tag == "" || ref.Tag == "latest" || seen[ref.HubRepo] {
				continue
			}
			seen[ref.HubRepo] = true
			repos = append(repos, ref.HubRepo)
		}
	}
	const phase = "resolving image tags"
	c.report(phase, 0, len(repos))

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		done    int
		rateHit bool
		sem     = make(chan struct{}, imageWorkers)
	)
	for _, repo := range repos {
		wg.Add(1)
		go func(repo string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			mu.Lock()
			skip := rateHit
			mu.Unlock()
			var res hubTags
			var err error
			if !skip {
				err = getJSON(ctx, dockerHub+"/"+repo+"/tags?page_size=100&ordering=last_updated", &res)
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == errRateLimited:
				rateHit = true
				s.Errors = append(s.Errors, "docker hub "+repo+": "+err.Error())
			case err != nil:
				s.Errors = append(s.Errors, "docker hub "+repo+": "+err.Error())
			case !skip:
				tags := make([]string, 0, len(res.Results))
				for _, t := range res.Results {
					tags = append(tags, t.Name)
				}
				s.ImageTags[repo] = tags
			}
			done++
			c.report(phase, done, len(repos))
		}(repo)
	}
	wg.Wait()
	return nil
}

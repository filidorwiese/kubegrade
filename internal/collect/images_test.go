package collect

import "testing"

func TestParseImage(t *testing.T) {
	cases := map[string]ImageRef{
		"nginx":                         {HubRepo: "library/nginx"},
		"nginx:1.27":                    {HubRepo: "library/nginx", Tag: "1.27"},
		"docker.io/bitnami/redis:7":     {HubRepo: "bitnami/redis", Tag: "7"},
		"registry:5000/app":             {},
		"registry:5000/app:latest":      {Tag: "latest"},
		"localhost/app:1":               {Tag: "1"},
		"ghcr.io/org/app:v1@sha256:abc": {Tag: "v1", Digest: "sha256:abc"},
		"ghcr.io/org/app@sha256:abc":    {Digest: "sha256:abc"},
		"a/b/c:1":                       {Tag: "1"},
	}
	for img, want := range cases {
		if got := ParseImage(img); got != want {
			t.Errorf("%s: %+v, want %+v", img, got, want)
		}
	}
}

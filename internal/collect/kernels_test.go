package collect

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// stubFeed serves canned bodies to any URL containing the key.
func stubFeed(bodies map[string]string) *feedCache {
	f := newFeedCache(context.Background())
	f.fetch = func(_ context.Context, u string) ([]byte, error) {
		for k, v := range bodies {
			if strings.Contains(u, k) {
				return []byte(v), nil
			}
		}
		return nil, fmt.Errorf("no stub for %s", u)
	}
	return f
}

const madisonSample = ` linux-image-amd64 | 6.1.176-1         | bookworm           | amd64
 linux-image-amd64 | 6.1.187-1         | bookworm-security  | amd64
 linux-image-amd64 | 6.12.95-1~bpo12+1 | bookworm-backports | amd64
 linux-image-amd64 | 6.12.107-1        | trixie             | amd64
 linux-image-amd64 | 6.12.111-1        | trixie-security    | amd64
 linux-image-amd64 | 7.1.13-1~bpo13+1  | trixie-backports   | amd64
 linux-image-amd64 | 7.2.6-1           | forky              | amd64
`

func TestDebianKernel(t *testing.T) {
	feed := stubFeed(map[string]string{"package=linux-image-amd64": madisonSample})
	info := func(os, kernel string) corev1.NodeSystemInfo {
		return corev1.NodeSystemInfo{OSImage: os, KernelVersion: kernel, Architecture: "amd64"}
	}
	up, err := debianKernel(feed, info("Debian GNU/Linux 13 (trixie)", "6.12.107+deb13-amd64"))
	if err != nil || up == nil || up.Latest != "6.12.111" || up.Source != "trixie-security" || up.Have != "6.12.107" {
		t.Errorf("trixie: %+v %v", up, err)
	}
	// Security beats stable even when stable is listed later; backports never count.
	up, _ = debianKernel(feed, info("Debian GNU/Linux 13 (trixie)", "6.12.111+deb13-amd64"))
	if up == nil || up.Latest != "6.12.111" {
		t.Errorf("current: %+v", up)
	}
	// Bookworm's ABI kernel string cannot be compared to a package version.
	if up, _ = debianKernel(feed, info("Debian GNU/Linux 12 (bookworm)", "6.1.0-37-amd64")); up != nil {
		t.Errorf("bookworm: %+v", up)
	}
}

const lpSeriesSample = `{"entries":[{"name":"noble","version":"24.04"},{"name":"jammy","version":"22.04"}]}`

const lpNobleGeneric = `{"entries":[
 {"binary_package_version":"7.0.0-38.38.1~24.04.1","pocket":"Proposed","distro_arch_series_link":"https://api.launchpad.net/1.0/ubuntu/noble/amd64"},
 {"binary_package_version":"6.8.0-142.142","pocket":"Security","distro_arch_series_link":"https://api.launchpad.net/1.0/ubuntu/noble/s390x"},
 {"binary_package_version":"6.8.0-142.142","pocket":"Security","distro_arch_series_link":"https://api.launchpad.net/1.0/ubuntu/noble/amd64"},
 {"binary_package_version":"6.8.0-140.140","pocket":"Updates","distro_arch_series_link":"https://api.launchpad.net/1.0/ubuntu/noble/amd64"}]}`

const lpJammyGeneric = `{"entries":[
 {"binary_package_version":"5.15.0-160.170","pocket":"Security","distro_arch_series_link":"https://api.launchpad.net/1.0/ubuntu/jammy/amd64"}]}`

const lpJammyHWE = `{"entries":[
 {"binary_package_version":"6.8.0-146.146~22.04.1","pocket":"Security","distro_arch_series_link":"https://api.launchpad.net/1.0/ubuntu/jammy/amd64"}]}`

func TestUbuntuKernel(t *testing.T) {
	feed := stubFeed(map[string]string{
		"/series": lpSeriesSample,
		"binary_name=linux-image-generic&distro_series=https://api.launchpad.net/1.0/ubuntu/noble":           lpNobleGeneric,
		"binary_name=linux-image-generic&distro_series=https://api.launchpad.net/1.0/ubuntu/jammy":           lpJammyGeneric,
		"binary_name=linux-image-generic-hwe-22.04&distro_series=https://api.launchpad.net/1.0/ubuntu/jammy": lpJammyHWE,
	})
	info := func(os, kernel string) corev1.NodeSystemInfo {
		return corev1.NodeSystemInfo{OSImage: os, KernelVersion: kernel, Architecture: "amd64"}
	}
	up, err := ubuntuKernel(feed, info("Ubuntu 24.04.2 LTS", "6.8.0-131-generic"))
	if err != nil || up == nil || up.Latest != "6.8.0-142" || up.Source != "noble security" || up.Have != "6.8.0-131" {
		t.Errorf("noble: %+v %v", up, err)
	}
	// Jammy on a 6.8 HWE kernel: the GA package is 5.15, so the HWE one applies.
	up, err = ubuntuKernel(feed, info("Ubuntu 22.04.5 LTS", "6.8.0-140-generic"))
	if err != nil || up == nil || up.Latest != "6.8.0-146" || up.Have != "6.8.0-140" {
		t.Errorf("jammy hwe: %+v %v", up, err)
	}
	if up, _ = ubuntuKernel(feed, info("Ubuntu 24.04.2 LTS", "6.8.0-142-generic")); up == nil || up.Latest != "6.8.0-142" {
		t.Errorf("current: %+v", up)
	}
}

func TestLessVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"6.12.107", "6.12.111", true}, {"6.12.111", "6.12.107", false}, {"6.12.111", "6.12.111", false},
		{"6.8.0-142", "6.8.0-146", true}, {"6.8.0-142", "6.8.0-142", false}, {"6.8.0-9", "6.8.0-10", true},
		{"6.12", "6.12.0", false}, {"", "6.1", true}, {"6.1", "", false}, {"6.12.107", "7.2.6", true},
	}
	for _, c := range cases {
		if got := lessVersion(c.a, c.b); got != c.want {
			t.Errorf("%q < %q = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

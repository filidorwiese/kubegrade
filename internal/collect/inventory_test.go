package collect

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestInventory(t *testing.T) {
	node := func(kernel string, labels map[string]string) corev1.Node {
		n := corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: labels}}
		n.Status.NodeInfo = corev1.NodeSystemInfo{KernelVersion: kernel, OSImage: "Debian 13"}
		return n
	}
	cp := map[string]string{"node-role.kubernetes.io/control-plane": "true"}
	master := map[string]string{"node-role.kubernetes.io/master": ""}
	s := &Snapshot{ServerVersion: "v1.34.4+k3s1", Nodes: []corev1.Node{
		node("6.12.111+deb13-amd64", cp), node("6.12.107+deb13-amd64", master),
		node("6.12.107+deb13-amd64", nil), node("6.12.107", nil),
	}}
	want := Inventory{Version: "v1.34.4+k3s1", ControlPlane: 2, Workers: 2,
		OS: []Count{{"Debian 13", 4}}, Kernels: []Count{{"6.12.107", 3}, {"6.12.111", 1}}}
	if got := s.Inventory(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

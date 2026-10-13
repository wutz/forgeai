package pools

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
)

func node(name, pool string) *corev1.Node {
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{}}}
	if pool != "" {
		n.Labels[naming.LabelNodePool] = pool
	}
	return n
}

func newClient(t *testing.T, objs ...runtime.Object) *Client {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v1.AddToScheme(s)
	k := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).WithStatusSubresource(&v1.PoolRequest{}).Build()
	return &Client{K8s: k, Namespace: "pod-prod"}
}

func TestListAndPods(t *testing.T) {
	ctx := context.Background()
	pod := func(name, nodeName string, labels map[string]string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "pod-prod", Labels: labels},
			Spec:       corev1.PodSpec{NodeName: nodeName},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		}
	}
	c := newClient(t,
		node("b", "p1"), node("a", "p1"), node("c", ""),
		pod("w-0", "a", map[string]string{naming.LabelUserID: "u1", naming.LabelJobName: "j", naming.LabelResourceID: "g1"}),
		pod("system", "a", nil),
		pod("other", "c", map[string]string{naming.LabelUserID: "u2"}),
	)
	pools, err := c.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p := pools["p1"]; p.NodeCount != 2 || p.Nodes[0] != "a" || pools[NoPool].NodeCount != 1 {
		t.Fatalf("pools=%+v", pools)
	}
	pods, err := c.Pods(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 || pods[0].Name != "w-0" || pods[0].Type != "job" || pods[0].ResourceID != "g1" {
		t.Fatalf("pods=%+v", pods)
	}
	if pods, _ := c.Pods(ctx, "nope"); len(pods) != 0 {
		t.Fatalf("pods=%+v", pods)
	}
}

func TestRequests(t *testing.T) {
	ctx := context.Background()
	cfg, err := platform.Parse([]byte(`
releaseVersion: test
resources:
  - {id: g1, gpu: {count: 1}, cpu: {cores: 8}, memory: {sizeGB: 64}}
`))
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(t)

	spec := RequestSpec{UserID: "u1", RequestID: "r1", DstPool: "train", ResourceID: "g1"}
	if _, err := c.CreateRequest(ctx, cfg, RequestSpec{UserID: "u1", RequestID: "r1", ResourceID: "g1"}); err == nil {
		t.Fatal("src == dst accepted")
	}
	if _, err := c.CreateRequest(ctx, cfg, RequestSpec{UserID: "u1", RequestID: "r1", DstPool: "train", ResourceID: "x"}); err == nil {
		t.Fatal("unknown resource accepted")
	}
	r, err := c.CreateRequest(ctx, cfg, spec)
	if err != nil {
		t.Fatal(err)
	}
	if r.SrcPool != naming.DefaultPool || r.NNodes != 1 || r.Status != "Pending" || r.NodeList == nil {
		t.Fatalf("r=%+v", r)
	}
	if _, err := c.CreateRequest(ctx, cfg, spec); err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	other := spec
	other.NNodes = 2
	if _, err := c.CreateRequest(ctx, cfg, other); !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}

	pr := &v1.PoolRequest{}
	key := types.NamespacedName{Namespace: "pod-prod", Name: naming.PoolRequestName("pod-prod", "u1", "r1")}
	if err := c.K8s.Get(ctx, key, pr); err != nil {
		t.Fatal(err)
	}
	pr.Status.Phase, pr.Status.Nodes = v1.PoolRequestSuccess, []string{"a"}
	if err := c.K8s.Status().Update(ctx, pr); err != nil {
		t.Fatal(err)
	}
	if r, _ := c.GetRequest(ctx, "u1", "r1"); r.Status != "Success" || r.NodeList[0] != "a" {
		t.Fatalf("r=%+v", r)
	}
	if err := c.DeleteRequest(ctx, "u1", "r1"); !errors.Is(err, ErrCompleted) {
		t.Fatalf("err=%v", err)
	}
	if err := c.DeleteRequest(ctx, "u1", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}

	if _, err := c.CreateRequest(ctx, cfg, RequestSpec{UserID: "u1", RequestID: "r2", DstPool: "train", ResourceID: "g1"}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteRequest(ctx, "u1", "r2"); err != nil {
		t.Fatal(err)
	}
}

func TestDeletePool(t *testing.T) {
	ctx := context.Background()
	c := newClient(t, node("a", "train"), node("b", "train"), node("c", "other"))
	for _, p := range []string{naming.DefaultPool, NoPool} {
		if _, err := c.DeletePool(ctx, p); err == nil {
			t.Fatalf("deleting %q accepted", p)
		}
	}
	moved, err := c.DeletePool(ctx, "train")
	if err != nil || len(moved) != 2 {
		t.Fatalf("moved=%v err=%v", moved, err)
	}
	pools, _ := c.List(ctx)
	if pools[naming.DefaultPool].NodeCount != 2 || pools["other"].NodeCount != 1 {
		t.Fatalf("pools=%+v", pools)
	}
	if _, ok := pools["train"]; ok {
		t.Fatal("pool still has nodes")
	}
}

package capacity

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
)

const gi = int64(1) << 30

var testConfig = []byte(`
releaseVersion: test
resources:
  - {id: g1, gpu: {count: 1}, cpu: {cores: 8}, memory: {sizeGB: 64}, labels: {scclabs.dev/gpu: rtx-5090}}
  - {id: g8, gpu: {count: 8}, cpu: {cores: 64}, memory: {sizeGB: 512}, labels: {scclabs.dev/gpu: rtx-5090}}
  - {id: c4, cpu: {cores: 4}, memory: {sizeGB: 16}, usageFrom: g1}
  - {id: a100, gpu: {count: 1}, cpu: {cores: 8}, memory: {sizeGB: 64}, labels: {scclabs.dev/gpu: a100}}
`)

func gpuNode() Node {
	return Node{
		Name:        "n1",
		Labels:      map[string]string{"scclabs.dev/gpu": "rtx-5090"},
		Taints:      []string{"nvidia.com/gpu:NoSchedule"},
		Allocatable: Resources{MilliCPU: 64000, Memory: 512 * gi, GPU: 8},
		Requested:   Resources{MilliCPU: 16000, Memory: 128 * gi, GPU: 2},
	}
}

func TestCount(t *testing.T) {
	cfg, err := platform.Parse(testConfig)
	if err != nil {
		t.Fatal(err)
	}
	g1, _ := cfg.Resource("g1")
	g8, _ := cfg.Resource("g8")
	c4, _ := cfg.Resource("c4")

	n := gpuNode()
	if total, free := Count(&n, g1); total != 8 || free != 6 {
		t.Fatalf("g1 total=%d free=%d", total, free)
	}
	if total, free := Count(&n, g8); total != 1 || free != 0 {
		t.Fatalf("g8 total=%d free=%d", total, free)
	}
	// CPU-only models skip tainted nodes.
	if total, _ := Count(&n, c4); total != 0 {
		t.Fatalf("c4 on tainted node total=%d", total)
	}

	n.Taints = append(n.Taints, "node.kubernetes.io/unreachable:NoSchedule")
	if total, _ := Count(&n, g1); total != 0 {
		t.Fatal("unexpected taint should exclude the node")
	}
	n = gpuNode()
	n.Unschedulable = true
	if total, _ := Count(&n, g1); total != 0 {
		t.Fatal("cordoned node should be excluded")
	}
	n = gpuNode()
	n.Requested.GPU = 9
	if u := Compute([]Node{n}, g1, false); u.Free != 0 || u.Total != 8 {
		t.Fatalf("over-committed usage=%+v", u)
	}
}

func TestModels(t *testing.T) {
	cfg, err := platform.Parse(testConfig)
	if err != nil {
		t.Fatal(err)
	}
	ms := Models(cfg, []Node{gpuNode()}, "", true)
	ids := []string{}
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	// a100 has no capacity and is omitted; c4 borrows g1's availability.
	if len(ms) != 3 || ids[0] != "g1" || ids[1] != "g8" || ids[2] != "c4" {
		t.Fatalf("models=%v", ids)
	}
	if c4 := ms[2]; c4.UsageFrom != "" || c4.Usage.Total != 8 || c4.Usage.Free != 6 {
		t.Fatalf("c4=%+v", c4)
	}
	if d := ms[0].Usage.Details; len(d) != 1 || d[0].NodeName != "n1" || d[0].FreeCount != 6 {
		t.Fatalf("details=%+v", d)
	}
	if ms := Models(cfg, []Node{gpuNode()}, "g8", false); len(ms) != 1 || ms[0].Usage.Details != nil {
		t.Fatalf("filtered=%+v", ms)
	}
}

func TestPodRequests(t *testing.T) {
	req := func(cpu string) corev1.ResourceRequirements {
		return corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu)}}
	}
	p := &corev1.Pod{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{Resources: req("1")}, {Resources: req("1")}},
		InitContainers: []corev1.Container{
			{Resources: req("500m"), RestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways)},
			{Resources: req("4")},
		},
		Overhead: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
	}}
	// max(1+1+0.5, 0.5+4) + 0.1
	reqs := PodRequests(p)
	if got := reqs.Cpu().MilliValue(); got != 4600 {
		t.Fatalf("cpu=%dm", got)
	}
}

func TestNodes(t *testing.T) {
	alloc := corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("64"), corev1.ResourceMemory: resource.MustParse("512Gi"),
		resourceGPU: resource.MustParse("8"),
	}
	gpuReq := corev1.ResourceRequirements{Requests: corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("8"), resourceGPU: resource.MustParse("1"),
	}}
	pod := func(name, node string, phase corev1.PodPhase) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "pod-prod"},
			Spec:       corev1.PodSpec{NodeName: node, Containers: []corev1.Container{{Name: "c", Resources: gpuReq}}},
			Status:     corev1.PodStatus{Phase: phase},
		}
	}
	k := fake.NewClientBuilder().WithObjects(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "n1", Labels: map[string]string{naming.LabelNodePool: "p1"}},
			Spec:       corev1.NodeSpec{Taints: []corev1.Taint{{Key: "nvidia.com/gpu", Effect: corev1.TaintEffectNoSchedule}}},
			Status:     corev1.NodeStatus{Allocatable: alloc},
		},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n2"}, Status: corev1.NodeStatus{Allocatable: alloc}},
		pod("running", "n1", corev1.PodRunning),
		pod("done", "n1", corev1.PodSucceeded),
		pod("unscheduled", "", corev1.PodPending),
	).Build()
	c := &Client{K8s: k}

	nodes, err := c.Nodes(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Name != "n1" {
		t.Fatalf("nodes=%+v", nodes)
	}
	n := nodes[0]
	if n.Requested.GPU != 1 || n.Requested.MilliCPU != 8000 || n.Allocatable.GPU != 8 || n.Taints[0] != "nvidia.com/gpu:NoSchedule" {
		t.Fatalf("node=%+v", n)
	}
	if all, _ := c.Nodes(context.Background(), ""); len(all) != 2 {
		t.Fatalf("all=%d", len(all))
	}
}

package builder

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/kf"
	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
)

const testConfig = `
releaseVersion: 0.1.0
defaultQueue: forgeai-queue
serviceAccount: pod-default
extraResources: {nextflow.io/fuse: "1"}
multiNode:
  fullNodeGPUs: 8
  env: {NCCL_IB_HCA: mlx5, NCCL_SOCKET_IFNAME: eth0}
  rdma:
    rtx-5090: {networks: [{name: hca, namespace: spiderpool}], resources: {rdma/hca: "1"}}
  podSetTopology: {kueue.x-k8s.io/podset-preferred-topology: kubernetes.io/hostname}
resources:
  - {id: g1, gpu: {count: 1}, cpu: {cores: 7}, memory: {sizeGB: 100}, labels: {scclabs.dev/gpu: rtx-5090}}
  - {id: c4, cpu: {cores: 4}, memory: {sizeGB: 16}, labels: {scclabs.dev/cpu: xeon-gold}}
`

func newJob(workers int32, resourceID string) *v1.TrainingJob {
	return &v1.TrainingJob{
		ObjectMeta: metav1.ObjectMeta{Name: naming.JobName("pod-prod", "u1", "ackjob-1"), Namespace: "pod-prod"},
		Spec: v1.TrainingJobSpec{
			UserID: "u1", JobID: "ackjob-1", Image: "img:1",
			Command: "python train.py", ResourceID: resourceID, Workers: workers,
			Env:          []v1.EnvVar{{Name: "FOO", Value: "bar"}},
			VolumeMounts: []v1.VolumeMount{{VolumeID: "data", MountPath: "/data"}, {VolumeID: "data", MountPath: "/data2", SubPath: "x", ReadOnly: true}},
		},
	}
}

func build(t *testing.T, job *v1.TrainingJob) (*Builder, *kf.TrainingRuntime) {
	t.Helper()
	cfg, err := platform.Parse([]byte(testConfig))
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(job, cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := b.TrainingRuntime()
	if err != nil {
		t.Fatal(err)
	}
	return b, rt
}

func container(rt *kf.TrainingRuntime) (corev1.PodTemplateSpec, corev1.Container) {
	pod := rt.Spec.Template.Spec.ReplicatedJobs[0].Template.Spec.Template
	return pod, pod.Spec.Containers[0]
}

func envMap(c corev1.Container) map[string]string {
	m := map[string]string{}
	for _, e := range c.Env {
		m[e.Name] = e.Value
	}
	return m
}

func TestSingleWorker(t *testing.T) {
	b, rt := build(t, newJob(1, "g1"))
	pod, c := container(rt)

	if got := c.Resources.Limits.Name("nvidia.com/gpu", "").String(); got != "1" {
		t.Errorf("gpu=%s", got)
	}
	if got := c.Resources.Requests.Memory().String(); got != "100Gi" {
		t.Errorf("memory=%s", got)
	}
	if _, ok := c.Resources.Limits["rdma/hca"]; ok {
		t.Error("single worker must not request rdma")
	}
	if _, ok := pod.Spec.NodeSelector["scclabs.dev/pool"]; ok || pod.Spec.NodeSelector[naming.LabelGPU] != "rtx-5090" {
		t.Errorf("nodeSelector=%v", pod.Spec.NodeSelector)
	}
	if _, ok := pod.Annotations[kf.AnnotationNetworks]; ok {
		t.Error("single worker must not attach RDMA networks")
	}
	if len(c.Command) != 2 || c.Args[0] != "python train.py" {
		t.Errorf("command=%q args=%q", c.Command, c.Args)
	}
	if pod.Labels[naming.LabelComponent] != naming.ComponentTraining {
		t.Errorf("component label missing: %v", pod.Labels)
	}
	if env := envMap(c); env["FOO"] != "bar" || env["NCCL_IB_HCA"] != "" {
		t.Errorf("env=%v", env)
	}
	// Duplicate volumeID -> one volume, two mounts.
	claims := 0
	for _, v := range pod.Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			claims++
			if v.PersistentVolumeClaim.ClaimName != naming.VolumeClaimName("pod-prod", "u1", "data") {
				t.Errorf("claim=%s", v.PersistentVolumeClaim.ClaimName)
			}
		}
	}
	if claims != 1 || len(c.VolumeMounts) != 3 {
		t.Errorf("claims=%d mounts=%d", claims, len(c.VolumeMounts))
	}

	tj := b.TrainJob()
	if tj.Labels[kf.LabelKueueQueue] != "forgeai-queue" {
		t.Errorf("queue=%s", tj.Labels[kf.LabelKueueQueue])
	}
	if tj.Spec.Trainer == nil || *tj.Spec.Trainer.NumProcPerNode != 1 {
		t.Errorf("numProcPerNode=%v", tj.Spec.Trainer)
	}
	if tj.Spec.RuntimeRef.Name != rt.Name || *rt.Spec.MLPolicy.NumNodes != 1 {
		t.Errorf("runtimeRef=%v numNodes=%d", tj.Spec.RuntimeRef, *rt.Spec.MLPolicy.NumNodes)
	}
}

// TestNoPodAccess checks workers expose nothing users could use to get into a pod.
func TestNoPodAccess(t *testing.T) {
	_, rt := build(t, newJob(1, "g1"))
	pod, c := container(rt)

	if len(c.Ports) != 0 {
		t.Errorf("ports=%v: workers must not expose endpoints", c.Ports)
	}
	if sc := c.SecurityContext; sc == nil || sc.Capabilities != nil || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
		t.Errorf("securityContext=%+v", sc)
	}
	for _, e := range c.Env {
		if e.ValueFrom != nil || strings.Contains(e.Name, "SSH") || strings.Contains(e.Name, "JUPYTER") {
			t.Errorf("unexpected env %s", e.Name)
		}
	}
	for _, v := range pod.Spec.Volumes {
		if v.HostPath != nil {
			t.Errorf("hostPath volume %s", v.Name)
		}
	}
}

func TestMultiWorker(t *testing.T) {
	b, rt := build(t, newJob(2, "g1"))
	pod, c := container(rt)

	if got := c.Resources.Limits.Name("nvidia.com/gpu", "").String(); got != "8" {
		t.Errorf("multi-node gpu=%s, want full node", got)
	}
	if got := c.Resources.Limits.Name("rdma/hca", "").String(); got != "1" {
		t.Errorf("rdma/hca=%s", got)
	}
	if pod.Annotations[kf.AnnotationNetworks] != `[{"name":"hca","namespace":"spiderpool"}]` {
		t.Errorf("networks=%s", pod.Annotations[kf.AnnotationNetworks])
	}
	if pod.Annotations["kueue.x-k8s.io/podset-preferred-topology"] != "kubernetes.io/hostname" {
		t.Errorf("annotations=%v", pod.Annotations)
	}
	if env := envMap(c); env["NCCL_IB_HCA"] != "mlx5" {
		t.Errorf("env=%v", env)
	}
	if *b.TrainJob().Spec.Trainer.NumProcPerNode != 8 || *rt.Spec.MLPolicy.NumNodes != 2 {
		t.Error("numProcPerNode/numNodes")
	}

}

func TestCPUMultiWorker(t *testing.T) {
	// scc-k8s-v2 crashed here (range over None GPUs).
	b, rt := build(t, newJob(2, "c4"))
	pod, c := container(rt)
	if _, ok := c.Resources.Limits["nvidia.com/gpu"]; ok {
		t.Error("cpu job requests gpu")
	}
	if _, ok := pod.Annotations[kf.AnnotationNetworks]; ok {
		t.Error("cpu job attaches rdma")
	}
	if b.TrainJob().Spec.Trainer != nil {
		t.Error("cpu job sets numProcPerNode")
	}
}

func TestUnknownResource(t *testing.T) {
	cfg, _ := platform.Parse([]byte(testConfig))
	if _, err := New(newJob(1, "nope"), cfg); err == nil {
		t.Fatal("expected error")
	}
}

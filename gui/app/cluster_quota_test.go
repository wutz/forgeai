package app

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/workspace/pkg/instancetype"
)

func quotaApp(t *testing.T, objs ...*corev1.Pod) *App {
	t.Helper()
	rq := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: naming.ResourceQuotaName, Namespace: testNamespace},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourceRequestsStorage:         resource.MustParse("200Gi"),
				corev1.ResourcePersistentVolumeClaims:  resource.MustParse("10"),
				corev1.ResourcePods:                    resource.MustParse("20"),
				"requests." + instancetype.ResourceGPU: resource.MustParse("4"),
			},
			Used: corev1.ResourceList{
				corev1.ResourceRequestsStorage:         resource.MustParse("50Gi"),
				corev1.ResourcePersistentVolumeClaims:  resource.MustParse("3"),
				corev1.ResourcePods:                    resource.MustParse("2"),
				"requests." + instancetype.ResourceGPU: resource.MustParse("3"),
			},
		},
	}
	builder := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(rq)
	for _, p := range objs {
		builder = builder.WithObjects(p)
	}
	k8s := builder.Build()
	return &App{ctx: context.Background(), conn: &Connection{Info: ConnInfo{Namespace: testNamespace}, k8s: k8s}}
}

func gpuPod(name string, phase corev1.PodPhase, gpus string, product string) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "c",
			Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
				instancetype.ResourceGPU: resource.MustParse(gpus),
			}},
		}}},
		Status: corev1.PodStatus{Phase: phase},
	}
	if product != "" {
		p.Spec.NodeSelector = map[string]string{naming.LabelGPUProduct: product}
	}
	return p
}

// The Resources page shows the limits and use of the ResourceQuota, and the
// GPUs in use per class from the live pods. Finished pods hold no GPU.
func TestQuotaReportsLimitsAndGPUClasses(t *testing.T) {
	a := quotaApp(t,
		gpuPod("rtx", corev1.PodRunning, "2", "NVIDIA-GeForce-RTX-5090"),
		gpuPod("pending", corev1.PodPending, "1", ""),
		gpuPod("done", corev1.PodSucceeded, "5", "NVIDIA-GeForce-RTX-5090"),
	)
	q, err := a.Quota()
	if err != nil {
		t.Fatal(err)
	}
	if q.GPUQuota != 4 || q.Storage != "200Gi" || q.StorageUsed != "50Gi" {
		t.Errorf("limits = gpu %d storage %q used %q, want 4 200Gi 50Gi", q.GPUQuota, q.Storage, q.StorageUsed)
	}
	if q.Volumes != 3 || q.VolumeMax != 10 || q.Pods != 2 || q.PodMax != 20 {
		t.Errorf("volumes %d/%d pods %d/%d, want 3/10 2/20", q.Volumes, q.VolumeMax, q.Pods, q.PodMax)
	}
	want := []GPUUsage{
		{Class: "*", Used: 1, Pods: 1},
		{Class: "rtx-5090", Used: 2, Pods: 1},
	}
	if !reflect.DeepEqual(q.GPUClasses, want) {
		t.Errorf("GPUClasses = %+v, want %+v", q.GPUClasses, want)
	}
}

// With no pod to attribute, the quota's GPU use is shown unclassed rather
// than as no use at all.
func TestQuotaFallsBackToUnclassedGPUUse(t *testing.T) {
	a := quotaApp(t)
	q, err := a.Quota()
	if err != nil {
		t.Fatal(err)
	}
	want := []GPUUsage{{Class: "*", Used: 3}}
	if !reflect.DeepEqual(q.GPUClasses, want) {
		t.Errorf("GPUClasses = %+v, want %+v", q.GPUClasses, want)
	}
}

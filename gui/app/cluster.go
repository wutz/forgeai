package app

import (
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/instancetype"
	"github.com/scclabs/forgeai/pkg/naming"
)

// InstanceType is the view model of a GPU instance type.
type InstanceType struct {
	Name     string `json:"name"`
	Class    string `json:"class"`
	Product  string `json:"product"`
	GPUCount int32  `json:"gpuCount"`
	GPUMemMi int64  `json:"gpuMemoryMiB"`
	CPU      string `json:"cpu"`
	Memory   string `json:"memory"`
	Total    int32  `json:"total"`
	Free     int32  `json:"free"`
}

// ListInstanceTypes lists the GPU instance types offered by the cluster.
func (a *App) ListInstanceTypes() ([]InstanceType, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	list := &v1.InstanceTypeList{}
	if err := c.k8s.List(ctx, list); err != nil {
		return nil, err
	}
	out := make([]InstanceType, 0, len(list.Items))
	for i := range list.Items {
		it := &list.Items[i]
		out = append(out, InstanceType{
			Name: it.Name, Class: it.Spec.GPU.Class, Product: it.Spec.GPU.Product,
			GPUCount: it.Spec.GPU.Count, GPUMemMi: it.Spec.GPU.MemoryMiB,
			CPU: it.Spec.CPU.String(), Memory: it.Spec.Memory.String(),
			Total: it.Status.Total, Free: it.Status.Free,
		})
	}
	return out, nil
}

// pvcRequests is the ResourceQuota name counting volume claims.
const pvcRequests = "requests.persistentvolumeclaims"

// QuotaUsage is what the project may consume and currently runs.
type QuotaUsage struct {
	// gpuClasses holds the quota and use of each GPU class.
	GPUClasses []GPUUsage `json:"gpuClasses"`
	// Storage allocated by volumes vs the limit.
	Storage     string `json:"storage"`
	StorageUsed string `json:"storageUsed"`
	Volumes     int64  `json:"volumes"`
	VolumeMax   int64  `json:"volumeMax"`
	Pods        int64  `json:"pods"`
	PodMax      int64  `json:"podMax"`
}

// GPUUsage is the quota of one GPU class and its current use.
type GPUUsage struct {
	Class string `json:"class"`
	Quota int32  `json:"quota"`
	Used  int32  `json:"used"`
	Pods  int    `json:"pods"`
}

// Quota reads the project's ResourceQuota to show what the project may still
// start. The Kueue ClusterQueue holds the GPU quota; the ResourceQuota is
// its backstop and also carries storage, volume and pod limits.
func (a *App) Quota() (*QuotaUsage, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	rq := &corev1.ResourceQuota{}
	if err := c.k8s.Get(ctx, client.ObjectKey{Namespace: c.namespace(), Name: naming.ResourceQuotaName}, rq); err != nil {
		return nil, err
	}
	hard, used := rq.Status.Hard, rq.Status.Used
	out := &QuotaUsage{
		StorageUsed: quantityString(used, corev1.ResourceRequestsStorage, "0"),
		Storage:     quantityString(hard, corev1.ResourceRequestsStorage, "unlimited"),
		Volumes:     quantityValue(used, pvcRequests),
		VolumeMax:   quantityValue(hard, pvcRequests),
		Pods:        quantityValue(used, string(corev1.ResourcePods)),
		PodMax:      quantityValue(hard, string(corev1.ResourcePods)),
	}

	// GPU use per class from the pods of the project namespace.
	pods := &corev1.PodList{}
	if err := c.k8s.List(ctx, pods, client.InNamespace(c.namespace())); err != nil {
		return nil, err
	}
	gpuUsed := map[string]int32{}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending {
			continue
		}
		gpus := int32(0)
		for j := range pod.Spec.Containers {
			if q, ok := pod.Spec.Containers[j].Resources.Requests[instancetype.ResourceGPU]; ok {
				n, _ := q.AsInt64()
				gpus += int32(n)
			}
		}
		if gpus == 0 {
			continue
		}
		class := naming.GPUClass(pod.Spec.NodeSelector[naming.LabelGPUProduct])
		gpuUsed[class] += gpus
	}
	// InstanceType status has the free counts per class; the pod scan gives
	// the current use.
	classes := map[string]GPUUsage{}
	for class, n := range gpuUsed {
		u := classes[class]
		u.Class, u.Used = class, n
		classes[class] = u
	}
	for _, g := range slices.Sorted(maps.Keys(classes)) {
		out.GPUClasses = append(out.GPUClasses, classes[g])
	}
	gpus := quantityValue32(used, "requests."+string(instancetype.ResourceGPU))
	if len(out.GPUClasses) == 0 && gpus > 0 {
		out.GPUClasses = append(out.GPUClasses, GPUUsage{Class: "*", Quota: gpus})
	} else {
		for i := range out.GPUClasses {
			out.GPUClasses[i].Quota = gpus
		}
	}
	return out, nil
}

func quantityString(list corev1.ResourceList, name corev1.ResourceName, zero string) string {
	if v, ok := list[name]; ok {
		return v.String()
	}
	return zero
}

func quantityValue(list corev1.ResourceList, name string) int64 {
	v, ok := list[corev1.ResourceName(name)]
	if !ok {
		return 0
	}
	n, _ := v.AsInt64()
	return n
}

func quantityValue32(list corev1.ResourceList, name string) int32 {
	return int32(quantityValue(list, name))
}

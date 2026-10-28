package app

import (
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wutz/forgeai/pkg/naming"
	projectv1 "github.com/wutz/project/api/v1alpha1"
	"github.com/wutz/project/pkg/instancetype"
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
	list := &projectv1.InstanceTypeList{}
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

// pvcRequests is the ResourceQuota name counting volume claims. The operator
// writes it under the bare name, so the constant comes from the same API key.
var pvcRequests = string(corev1.ResourcePersistentVolumeClaims)

// QuotaUsage is what the project may consume and currently runs.
type QuotaUsage struct {
	// GPUQuota is the project's GPU limit across all classes; 0 when unset.
	GPUQuota int32 `json:"gpuQuota"`
	// GPUClasses holds the current GPU use of each class.
	GPUClasses []GPUUsage `json:"gpuClasses"`
	// Storage allocated by volumes vs the limit. An empty limit is unlimited.
	Storage     string `json:"storage"`
	StorageUsed string `json:"storageUsed"`
	Volumes     int64  `json:"volumes"`
	VolumeMax   int64  `json:"volumeMax"`
	Pods        int64  `json:"pods"`
	PodMax      int64  `json:"podMax"`
}

// GPUUsage is the current use of one GPU class.
type GPUUsage struct {
	Class string `json:"class"`
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
		Storage:     quantityString(hard, corev1.ResourceRequestsStorage, ""),
		Volumes:     quantityValue(used, pvcRequests),
		VolumeMax:   quantityValue(hard, pvcRequests),
		Pods:        quantityValue(used, string(corev1.ResourcePods)),
		PodMax:      quantityValue(hard, string(corev1.ResourcePods)),
		GPUQuota:    quantityValue32(hard, "requests."+string(instancetype.ResourceGPU)),
	}

	// GPU use per class from the pods of the project namespace.
	pods := &corev1.PodList{}
	if err := c.k8s.List(ctx, pods, client.InNamespace(c.namespace())); err != nil {
		return nil, err
	}
	usage := map[string]GPUUsage{}
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
		class := gpuUsageClass(pod.Spec.NodeSelector[naming.LabelGPUProduct])
		u := usage[class]
		u.Class, u.Used, u.Pods = class, u.Used+gpus, u.Pods+1
		usage[class] = u
	}
	// Sorted by class so the list keeps its order between polls.
	for _, class := range slices.Sorted(maps.Keys(usage)) {
		out.GPUClasses = append(out.GPUClasses, usage[class])
	}
	// Without a running pod to attribute, the quota's use is shown unclassed.
	if n := quantityValue32(used, "requests."+string(instancetype.ResourceGPU)); len(out.GPUClasses) == 0 && n > 0 {
		out.GPUClasses = append(out.GPUClasses, GPUUsage{Class: "*", Used: n})
	}
	return out, nil
}

// unclassedGPU is the class of GPUs whose pod names no product, so the class
// is not known. Resources shows it as unclassed use.
const unclassedGPU = "*"

// gpuUsageClass is the class a pod's GPUs count against, from the product
// label on its node selector. A pod without one is unclassed, not a class "".
func gpuUsageClass(product string) string {
	if class := naming.GPUClass(product); class != "" {
		return class
	}
	return unclassedGPU
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

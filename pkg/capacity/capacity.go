// Package capacity computes how many instances of each resource model fit on
// the cluster's nodes, using the same per-node arithmetic as scc-k8s-v2
// (util/resource.py) but reading nodes and pod requests straight from the API.
package capacity

import (
	"context"
	"math"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scclabs/forgeai/pkg/platform"
)

const resourceGPU corev1.ResourceName = "nvidia.com/gpu"

// allowedGPUTaints may be present on GPU nodes; any other taint excludes the node.
var allowedGPUTaints = map[string]bool{
	"nvidia.com/gpu:NoSchedule":   true,
	"scclabs.dev/vgpu:NoSchedule": true,
}

// Node is the scheduling view of one node.
type Node struct {
	Name          string
	Labels        map[string]string
	Unschedulable bool
	// Taints as key:effect.
	Taints []string
	// Allocatable and Requested in milli-CPU, bytes and GPUs.
	Allocatable, Requested Resources
}

type Resources struct {
	MilliCPU int64
	Memory   int64
	GPU      int64
}

// NodeUsage is the per-node count of one model.
type NodeUsage struct {
	NodeName   string `json:"node_name"`
	FreeCount  int64  `json:"free_count"`
	TotalCount int64  `json:"total_count"`
}

// Usage is how many instances of a model fit in total and right now.
type Usage struct {
	Total   int64       `json:"total"`
	Free    int64       `json:"free"`
	Details []NodeUsage `json:"details,omitempty"`
}

func floorDiv(a, b float64) int64 {
	if b <= 0 {
		return 0
	}
	return int64(math.Floor(a / b))
}

func matches(n *Node, r platform.Resource) bool {
	for k, v := range r.Labels {
		if n.Labels[k] != v {
			return false
		}
	}
	return true
}

// Count returns how many instances of r the node can hold (total) and can hold
// now (free, may be negative when the node is over-committed).
func Count(n *Node, r platform.Resource) (total, free int64) {
	if n.Unschedulable || !matches(n, r) {
		return 0, 0
	}
	cpu := r.CPU.Cores * 1000
	mem := r.Memory.SizeGB * (1 << 30)
	totalCPU := floorDiv(float64(n.Allocatable.MilliCPU), cpu)
	freeCPU := floorDiv(float64(n.Allocatable.MilliCPU-n.Requested.MilliCPU), cpu)
	totalMem := floorDiv(float64(n.Allocatable.Memory), mem)
	freeMem := floorDiv(float64(n.Allocatable.Memory-n.Requested.Memory), mem)

	gpus := r.GPUCount()
	if gpus == 0 {
		// CPU-only models only run on untainted nodes.
		if len(n.Taints) > 0 {
			return 0, 0
		}
		return min(totalCPU, totalMem), min(freeCPU, freeMem)
	}
	for _, t := range n.Taints {
		if !allowedGPUTaints[t] {
			return 0, 0
		}
	}
	totalGPU := floorDiv(float64(n.Allocatable.GPU), float64(gpus))
	freeGPU := floorDiv(float64(n.Allocatable.GPU-n.Requested.GPU), float64(gpus))
	return min(totalCPU, totalMem, totalGPU), min(freeCPU, freeMem, freeGPU)
}

// Compute sums Count over nodes. Negative per-node free counts are clamped to 0.
func Compute(nodes []Node, r platform.Resource, details bool) Usage {
	u := Usage{}
	if details {
		u.Details = []NodeUsage{}
	}
	for i := range nodes {
		total, free := Count(&nodes[i], r)
		free = max(free, 0)
		u.Total += total
		u.Free += free
		if details && total > 0 {
			u.Details = append(u.Details, NodeUsage{NodeName: nodes[i].Name, FreeCount: free, TotalCount: total})
		}
	}
	return u
}

// Client reads nodes and pods.
type Client struct {
	K8s client.Reader
}

// Nodes returns the scheduling view of all nodes.
func (c *Client) Nodes(ctx context.Context) ([]Node, error) {
	nodes := &corev1.NodeList{}
	if err := c.K8s.List(ctx, nodes); err != nil {
		return nil, err
	}
	pods := &corev1.PodList{}
	if err := c.K8s.List(ctx, pods); err != nil {
		return nil, err
	}
	requested := map[string]*Resources{}
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.Spec.NodeName == "" || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		r := requested[p.Spec.NodeName]
		if r == nil {
			r = &Resources{}
			requested[p.Spec.NodeName] = r
		}
		req := PodRequests(p)
		r.MilliCPU += req.Cpu().MilliValue()
		r.Memory += req.Memory().Value()
		r.GPU += quantity(req, resourceGPU)
	}

	out := make([]Node, 0, len(nodes.Items))
	for _, n := range nodes.Items {
		node := Node{
			Name: n.Name, Labels: n.Labels, Unschedulable: n.Spec.Unschedulable,
			Allocatable: Resources{
				MilliCPU: n.Status.Allocatable.Cpu().MilliValue(),
				Memory:   n.Status.Allocatable.Memory().Value(),
				GPU:      quantity(n.Status.Allocatable, resourceGPU),
			},
		}
		for _, t := range n.Spec.Taints {
			node.Taints = append(node.Taints, t.Key+":"+string(t.Effect))
		}
		if r := requested[n.Name]; r != nil {
			node.Requested = *r
		}
		out = append(out, node)
	}
	slices.SortFunc(out, func(a, b Node) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func quantity(l corev1.ResourceList, name corev1.ResourceName) int64 {
	if q, ok := l[name]; ok {
		return q.Value()
	}
	return 0
}

// PodRequests returns the effective requests of a pod as the scheduler sees
// them: max(sum(containers)+sidecars, each init container) + overhead.
func PodRequests(p *corev1.Pod) corev1.ResourceList {
	reqs := corev1.ResourceList{}
	add := func(dst corev1.ResourceList, src corev1.ResourceList) {
		for k, v := range src {
			q := dst[k]
			q.Add(v)
			dst[k] = q
		}
	}
	for _, c := range p.Spec.Containers {
		add(reqs, c.Resources.Requests)
	}
	sidecars := corev1.ResourceList{}
	for _, c := range p.Spec.InitContainers {
		if c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			add(reqs, c.Resources.Requests)
			add(sidecars, c.Resources.Requests)
			continue
		}
		// A regular init container runs alongside sidecars started before it.
		need := sidecars.DeepCopy()
		add(need, c.Resources.Requests)
		for k, v := range need {
			if cur, ok := reqs[k]; !ok || v.Cmp(cur) > 0 {
				reqs[k] = v.DeepCopy()
			}
		}
	}
	add(reqs, p.Spec.Overhead)
	return reqs
}

// ModelUsage is a resource model with its availability, the shape scc-k8s-v2
// returned from /apis/pod/v1alpha2/resources.
type ModelUsage struct {
	platform.Resource
	Usage Usage `json:"usage"`
}

// Models computes availability of every model (or only resourceID). Like
// scc-k8s-v2, models with no capacity at all are omitted.
func Models(cfg *platform.Config, nodes []Node, resourceID string, details bool) []ModelUsage {
	out := []ModelUsage{}
	for _, r := range cfg.Resources {
		if resourceID != "" && r.ID != resourceID {
			continue
		}
		src := r
		if r.UsageFrom != "" {
			if s, err := cfg.Resource(r.UsageFrom); err == nil {
				src = s
			}
		}
		u := Compute(nodes, src, details)
		if u.Total == 0 {
			continue
		}
		r.UsageFrom = ""
		out = append(out, ModelUsage{Resource: r, Usage: u})
	}
	return out
}

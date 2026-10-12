// Package pools implements node pools (the scclabs.dev/pool node label) and
// pool requests that move nodes between pools, compatible with scc-k8s-v2.
package pools

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
)

// NoPool is reported for nodes without a pool label (scc-k8s-v2 used "NA").
const NoPool = "NA"

var (
	ErrNotFound = errors.New("pool request not found")
	// ErrCompleted is returned when deleting a request that already moved nodes.
	ErrCompleted = errors.New("pool request was already completed")
	// ErrConflict is returned when a request ID is reused with another spec.
	ErrConflict = errors.New("pool request already exists with a different spec")
)

var idPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// Pool is the node list of one pool.
type Pool struct {
	NodeCount int      `json:"node_count"`
	Nodes     []string `json:"nodes"`
}

// PodInfo is one pod running in a pool.
type PodInfo struct {
	Name               string `json:"name"`
	Type               string `json:"type"`
	Status             string `json:"status"`
	Node               string `json:"node"`
	UserID             string `json:"user_id"`
	PodID              string `json:"pod_id"`
	JobName            string `json:"job_name"`
	JobCompletionIndex string `json:"job_completion_index"`
	ResourceID         string `json:"resource_id"`
	CreationTime       string `json:"creation_time"`
}

// Request is the scc-k8s-v2 PoolRequestOut.
type Request struct {
	RequestID  string   `json:"request_id"`
	SrcPool    string   `json:"src_pool"`
	DstPool    string   `json:"dst_pool"`
	ResourceID string   `json:"resourceID"`
	NNodes     int32    `json:"nnodes"`
	Status     string   `json:"status"`
	UserID     string   `json:"userID"`
	NodeList   []string `json:"node_list"`
	// Message explains a Pending or Failed request (forgeai extension).
	Message string `json:"message,omitempty"`
}

// Client works with nodes cluster-wide and PoolRequests in Namespace.
type Client struct {
	K8s       client.Client
	Namespace string
}

func poolOf(n *corev1.Node) string {
	if p := n.Labels[naming.LabelNodePool]; p != "" {
		return p
	}
	return NoPool
}

// List returns all pools with their nodes.
func (c *Client) List(ctx context.Context) (map[string]Pool, error) {
	nodes := &corev1.NodeList{}
	if err := c.K8s.List(ctx, nodes); err != nil {
		return nil, err
	}
	out := map[string]Pool{}
	for i := range nodes.Items {
		p := poolOf(&nodes.Items[i])
		pool := out[p]
		pool.Nodes = append(pool.Nodes, nodes.Items[i].Name)
		pool.NodeCount++
		out[p] = pool
	}
	for _, p := range out {
		sort.Strings(p.Nodes)
	}
	return out, nil
}

// Pods lists platform pods (pods with a user label) on nodes of the pool.
func (c *Client) Pods(ctx context.Context, pool string) ([]PodInfo, error) {
	nodes := &corev1.NodeList{}
	if err := c.K8s.List(ctx, nodes); err != nil {
		return nil, err
	}
	inPool := map[string]bool{}
	for i := range nodes.Items {
		if poolOf(&nodes.Items[i]) == pool {
			inPool[nodes.Items[i].Name] = true
		}
	}
	out := []PodInfo{}
	if len(inPool) == 0 {
		return out, nil
	}
	pods := &corev1.PodList{}
	if err := c.K8s.List(ctx, pods, client.InNamespace(c.Namespace), client.HasLabels{naming.LabelUserID}); err != nil {
		return nil, err
	}
	for _, p := range pods.Items {
		if !inPool[p.Spec.NodeName] {
			continue
		}
		l := p.Labels
		typ := "pod"
		if l[naming.LabelJobName] != "" {
			typ = "job"
		}
		out = append(out, PodInfo{
			Name: p.Name, Type: typ, Status: string(p.Status.Phase), Node: p.Spec.NodeName,
			UserID: l[naming.LabelUserID], PodID: l[naming.LabelPodID], JobName: l[naming.LabelJobName],
			JobCompletionIndex: l[naming.LabelCompletion], ResourceID: l[naming.LabelResourceID],
			CreationTime: p.CreationTimestamp.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// RequestSpec is a new pool request.
type RequestSpec struct {
	UserID, RequestID, SrcPool, DstPool, ResourceID string
	NNodes                                          int32
}

func (s *RequestSpec) defaults() {
	if s.SrcPool == "" {
		s.SrcPool = naming.DefaultPool
	}
	if s.DstPool == "" {
		s.DstPool = naming.DefaultPool
	}
	if s.NNodes < 1 {
		s.NNodes = 1
	}
}

// Validate checks a request against the platform config.
func Validate(cfg *platform.Config, s RequestSpec) error {
	if !idPattern.MatchString(s.RequestID) || len(s.RequestID) > 40 {
		return fmt.Errorf("invalid request_id %q: lowercase alphanumeric or '-', at most 40 characters", s.RequestID)
	}
	if s.SrcPool == s.DstPool {
		return fmt.Errorf("src_pool and dst_pool must differ")
	}
	for _, p := range []string{s.SrcPool, s.DstPool} {
		if !idPattern.MatchString(p) || len(p) > 63 {
			return fmt.Errorf("invalid pool name %q", p)
		}
	}
	_, err := cfg.Resource(s.ResourceID)
	return err
}

func toRequest(pr *v1.PoolRequest) *Request {
	status := string(pr.Status.Phase)
	if status == "" {
		status = string(v1.PoolRequestPending)
	}
	nodes := pr.Status.Nodes
	if nodes == nil {
		nodes = []string{}
	}
	return &Request{
		RequestID: pr.Spec.RequestID, SrcPool: pr.Spec.SrcPool, DstPool: pr.Spec.DstPool,
		ResourceID: pr.Spec.ResourceID, NNodes: pr.Spec.NNodes, Status: status,
		UserID: pr.Spec.UserID, NodeList: nodes, Message: pr.Status.Message,
	}
}

func (c *Client) name(userID, requestID string) string {
	return naming.PoolRequestName(c.Namespace, userID, requestID)
}

// CreateRequest submits a pool request. Re-submitting the same spec is a no-op.
func (c *Client) CreateRequest(ctx context.Context, cfg *platform.Config, s RequestSpec) (*Request, error) {
	s.defaults()
	if err := Validate(cfg, s); err != nil {
		return nil, err
	}
	pr := &v1.PoolRequest{Spec: v1.PoolRequestSpec{
		UserID: s.UserID, RequestID: s.RequestID, SrcPool: s.SrcPool, DstPool: s.DstPool,
		ResourceID: s.ResourceID, NNodes: s.NNodes,
	}}
	pr.Name, pr.Namespace = c.name(s.UserID, s.RequestID), c.Namespace
	pr.Labels = map[string]string{
		naming.LabelUserID: s.UserID, naming.LabelPoolRequestID: s.RequestID,
		naming.LabelManagedBy: naming.ManagedBy,
	}
	err := c.K8s.Create(ctx, pr)
	if apierrors.IsAlreadyExists(err) {
		cur := &v1.PoolRequest{}
		if err := c.K8s.Get(ctx, client.ObjectKeyFromObject(pr), cur); err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(cur.Spec, pr.Spec) {
			return nil, ErrConflict
		}
		return toRequest(cur), nil
	}
	if err != nil {
		return nil, err
	}
	return toRequest(pr), nil
}

func (c *Client) get(ctx context.Context, userID, requestID string) (*v1.PoolRequest, error) {
	pr := &v1.PoolRequest{}
	err := c.K8s.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: c.name(userID, requestID)}, pr)
	if apierrors.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return pr, err
}

// GetRequest returns a pool request.
func (c *Client) GetRequest(ctx context.Context, userID, requestID string) (*Request, error) {
	pr, err := c.get(ctx, userID, requestID)
	if err != nil {
		return nil, err
	}
	return toRequest(pr), nil
}

// DeleteRequest cancels a request that has not moved nodes yet.
func (c *Client) DeleteRequest(ctx context.Context, userID, requestID string) error {
	pr, err := c.get(ctx, userID, requestID)
	if err != nil {
		return err
	}
	if pr.Status.Phase == v1.PoolRequestSuccess {
		return ErrCompleted
	}
	if err := c.K8s.Delete(ctx, pr); err != nil {
		if apierrors.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// DeletePool moves every node of the pool back to the default pool and
// returns the moved nodes.
func (c *Client) DeletePool(ctx context.Context, pool string) ([]string, error) {
	if pool == naming.DefaultPool || pool == NoPool {
		return nil, fmt.Errorf("pool %q can not be deleted", pool)
	}
	nodes := &corev1.NodeList{}
	if err := c.K8s.List(ctx, nodes, client.MatchingLabels{naming.LabelNodePool: pool}); err != nil {
		return nil, err
	}
	moved := []string{}
	for i := range nodes.Items {
		if err := SetNodePool(ctx, c.K8s, &nodes.Items[i], naming.DefaultPool); err != nil {
			return moved, err
		}
		moved = append(moved, nodes.Items[i].Name)
	}
	return moved, nil
}

// SetNodePool sets the pool label of a node.
func SetNodePool(ctx context.Context, k client.Client, n *corev1.Node, pool string) error {
	if n.Labels[naming.LabelNodePool] == pool {
		return nil
	}
	patch := client.MergeFrom(n.DeepCopy())
	if n.Labels == nil {
		n.Labels = map[string]string{}
	}
	n.Labels[naming.LabelNodePool] = pool
	return k.Patch(ctx, n, patch)
}

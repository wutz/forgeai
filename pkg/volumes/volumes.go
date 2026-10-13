// Package volumes manages user volumes (PVCs) the way scc-k8s-v2 did: one PVC
// p-{userHash}-{volumeID} per volume, labelled with the user and volume ID.
package volumes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
)

var (
	ErrNotFound = errors.New("volume not found")
	// ErrInUse is returned when deleting a volume mounted by a job or pod.
	ErrInUse = errors.New("volume is in use")
)

var idPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// Volume is the scc-k8s-v2 volume view.
type Volume struct {
	ID     string `json:"id"`
	UserID string `json:"userID"`
	Status string `json:"status"`
	SizeGB int64  `json:"sizeGB"`
	// Type is the volume type when known (set on create).
	Type string `json:"type,omitempty"`
}

// Client manages volumes in one namespace.
type Client struct {
	K8s       client.Client
	Namespace string
}

func (c *Client) claimName(userID, volumeID string) string {
	return naming.VolumeClaimName(c.Namespace, userID, volumeID)
}

// Validate checks a volume ID and size.
func Validate(volumeID string, sizeGB int64) error {
	if !idPattern.MatchString(volumeID) || len(volumeID) > 40 {
		return fmt.Errorf("invalid volumeID %q: lowercase alphanumeric or '-', at most 40 characters", volumeID)
	}
	if sizeGB < 1 {
		return fmt.Errorf("sizeGB must be at least 1")
	}
	return nil
}

// Create creates the PVC; an existing volume is left unchanged (kubectl apply
// in scc-k8s-v2 was idempotent too).
func (c *Client) Create(
	ctx context.Context, cfg *platform.Config, userID, volumeID, volType string, sizeGB int64,
) (*Volume, error) {
	if err := Validate(volumeID, sizeGB); err != nil {
		return nil, err
	}
	st, err := cfg.StorageType(volType)
	if err != nil {
		return nil, err
	}
	name := c.claimName(userID, volumeID)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: c.Namespace,
			Labels: map[string]string{
				naming.LabelAppInstance: name,
				naming.LabelAppName:     naming.AppNameVolume,
				naming.LabelVolumeID:    volumeID,
				naming.LabelUserID:      userID,
				naming.LabelManagedBy:   naming.ManagedBy,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.PersistentVolumeAccessMode(st.DefaultAccessMode)},
			StorageClassName: &st.StorageClass,
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceStorage: *resource.NewQuantity(sizeGB<<30, resource.BinarySI),
			}},
		},
	}
	if err := c.K8s.Create(ctx, pvc); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, err
	}
	return &Volume{ID: volumeID, UserID: userID, Status: "Pending", SizeGB: sizeGB, Type: volType}, nil
}

// Status maps the PVC state to scc-k8s-v2 volume status.
func Status(pvc *corev1.PersistentVolumeClaim) string {
	if !pvc.DeletionTimestamp.IsZero() {
		return "Deleting"
	}
	switch pvc.Status.Phase {
	case corev1.ClaimPending:
		return "Pending"
	case corev1.ClaimBound:
		return "Available"
	}
	return "Failed"
}

func toVolume(pvc *corev1.PersistentVolumeClaim) Volume {
	v := Volume{
		ID: pvc.Labels[naming.LabelVolumeID], UserID: pvc.Labels[naming.LabelUserID], Status: Status(pvc),
	}
	if q, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
		v.SizeGB = q.Value() >> 30
	}
	return v
}

// List returns the user's volumes, optionally only volumeID.
func (c *Client) List(ctx context.Context, userID, volumeID string) ([]Volume, error) {
	sel := client.MatchingLabels{naming.LabelAppName: naming.AppNameVolume, naming.LabelUserID: userID}
	if volumeID != "" {
		sel[naming.LabelVolumeID] = volumeID
	}
	list := &corev1.PersistentVolumeClaimList{}
	if err := c.K8s.List(ctx, list, client.InNamespace(c.Namespace), sel); err != nil {
		return nil, err
	}
	out := make([]Volume, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, toVolume(&list.Items[i]))
	}
	return out, nil
}

func (c *Client) getPVC(ctx context.Context, userID, volumeID string) (*corev1.PersistentVolumeClaim, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	err := c.K8s.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: c.claimName(userID, volumeID)}, pvc)
	if apierrors.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return pvc, err
}

// Get returns one volume.
func (c *Client) Get(ctx context.Context, userID, volumeID string) (*Volume, error) {
	pvc, err := c.getPVC(ctx, userID, volumeID)
	if err != nil {
		return nil, err
	}
	v := toVolume(pvc)
	return &v, nil
}

// Resize sets the requested size. Shrinking is rejected by the API server.
func (c *Client) Resize(ctx context.Context, userID, volumeID string, sizeGB int64) (*Volume, error) {
	if err := Validate(volumeID, sizeGB); err != nil {
		return nil, err
	}
	pvc, err := c.getPVC(ctx, userID, volumeID)
	if err != nil {
		return nil, err
	}
	patch := client.MergeFrom(pvc.DeepCopy())
	pvc.Spec.Resources.Requests[corev1.ResourceStorage] = *resource.NewQuantity(sizeGB<<30, resource.BinarySI)
	if err := c.K8s.Patch(ctx, pvc, patch); err != nil {
		return nil, err
	}
	v := toVolume(pvc)
	return &v, nil
}

// Users returns the names of TrainingJobs and live pods mounting the PVC.
func (c *Client) Users(ctx context.Context, claim string) ([]string, error) {
	var users []string
	jobs := &v1.TrainingJobList{}
	if err := c.K8s.List(ctx, jobs, client.InNamespace(c.Namespace)); err != nil {
		return nil, err
	}
	for _, j := range jobs.Items {
		for _, m := range j.Spec.VolumeMounts {
			name := m.ClaimName
			if name == "" {
				owner := m.UserID
				if owner == "" {
					owner = j.Spec.UserID
				}
				name = c.claimName(owner, m.VolumeID)
			}
			if name == claim {
				users = append(users, "trainingjob/"+j.Name)
				break
			}
		}
	}
	pods := &corev1.PodList{}
	if err := c.K8s.List(ctx, pods, client.InNamespace(c.Namespace)); err != nil {
		return nil, err
	}
	for _, p := range pods.Items {
		if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, v := range p.Spec.Volumes {
			if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == claim {
				users = append(users, "pod/"+p.Name)
				break
			}
		}
	}
	return users, nil
}

// Delete removes the PVC unless a TrainingJob or a live pod uses it.
func (c *Client) Delete(ctx context.Context, userID, volumeID string) error {
	pvc, err := c.getPVC(ctx, userID, volumeID)
	if err != nil {
		return err
	}
	users, err := c.Users(ctx, pvc.Name)
	if err != nil {
		return err
	}
	if len(users) > 0 {
		return fmt.Errorf("%w: %s were referred by %v", ErrInUse, pvc.Name, users)
	}
	if err := c.K8s.Delete(ctx, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// UsageInfo is the scc-k8s-v2 VolumeUsageInfo.
type UsageInfo struct {
	VolumeID       string  `json:"volumeID"`
	TotalBytes     int64   `json:"total_bytes"`
	UsedBytes      int64   `json:"used_bytes"`
	TotalGB        float64 `json:"total_gb"`
	UsedGB         float64 `json:"used_gb"`
	UsedPercentage float64 `json:"used_percentage"`
}

// UsageClient queries scc-monitor-querier (GET /v1/pvc/size/query).
type UsageClient struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

type usageResponse struct {
	Status string `json:"status"`
	Code   int    `json:"code"`
	Data   []struct {
		TotalBytes int64 `json:"total_bytes"`
		UsedBytes  int64 `json:"used_bytes"`
	} `json:"data"`
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

func (u *UsageClient) query(ctx context.Context, claim, namespace, lookback string) (*usageResponse, error) {
	q := url.Values{"pvc_name": {claim}, "namespace": {namespace}}
	if lookback != "" {
		q.Set("lookback", lookback)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.BaseURL+"/v1/pvc/size/query?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+u.Token)
	hc := u.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("volume usage query: HTTP %d", resp.StatusCode)
	}
	out := &usageResponse{}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return nil, err
	}
	if out.Status != "success" || out.Code != 0 || len(out.Data) == 0 {
		return nil, nil
	}
	return out, nil
}

// Usage returns the volume usage, or ErrNotFound when the monitor has no data
// (retrying with lookback=30d like scc-k8s-v2).
func (u *UsageClient) Usage(ctx context.Context, namespace, userID, volumeID string) (*UsageInfo, error) {
	if u.BaseURL == "" || u.Token == "" {
		return nil, fmt.Errorf("volume usage service is not configured")
	}
	claim := naming.VolumeClaimName(namespace, userID, volumeID)
	r, err := u.query(ctx, claim, namespace, "")
	if err != nil || r == nil {
		r, err = u.query(ctx, claim, namespace, "30d")
	}
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrNotFound
	}
	d := r.Data[0]
	info := &UsageInfo{
		VolumeID: volumeID, TotalBytes: d.TotalBytes, UsedBytes: d.UsedBytes,
		TotalGB: round2(float64(d.TotalBytes) / (1 << 30)), UsedGB: round2(float64(d.UsedBytes) / (1 << 30)),
	}
	if d.TotalBytes > 0 {
		info.UsedPercentage = round2(float64(d.UsedBytes) / float64(d.TotalBytes) * 100)
	}
	return info, nil
}

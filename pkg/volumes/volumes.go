// Package volumes manages project volumes: shared filesystem PVCs in a
// project namespace. Deleting a volume moves it to the trash (a label); the
// operator purges it once it expires, until then it can be restored.
package volumes

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
)

var (
	ErrNotFound = errors.New("volume not found")
	// ErrInUse is returned when trashing a volume mounted by a running pod.
	ErrInUse = errors.New("volume is in use")
	// ErrInTrash is returned for operations on a trashed volume.
	ErrInTrash = errors.New("volume is in the trash")
)

var nameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// Volume is the GUI view of a PVC.
type Volume struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	StorageClass string `json:"storageClass"`
	// Phase is Pending, Bound or Lost; Resizing while an expansion is in progress.
	Phase        string `json:"phase"`
	RequestedGiB int64  `json:"requestedGiB"`
	CapacityGiB  int64  `json:"capacityGiB"`
	AccessMode   string `json:"accessMode"`
	Created      string `json:"created"`
	// Users are the running pods mounting the volume.
	Users []string `json:"users"`
	// Trash fields.
	InTrash   bool   `json:"inTrash"`
	TrashedAt string `json:"trashedAt,omitempty"`
	ExpireAt  string `json:"expireAt,omitempty"`
}

// Client manages volumes in one project namespace.
type Client struct {
	K8s       client.Client
	Namespace string
	Now       func() time.Time
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func gib(q resource.Quantity) int64 { return (q.Value() + (1 << 30) - 1) >> 30 }

// InTrash reports whether a PVC is in the trash.
func InTrash(pvc *corev1.PersistentVolumeClaim) bool { return pvc.Labels[naming.LabelTrash] == "true" }

// ExpireAt returns when a trashed PVC is purged.
func ExpireAt(pvc *corev1.PersistentVolumeClaim) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, pvc.Annotations[naming.AnnotationExpireAt])
	return t, err == nil
}

func toVolume(pvc *corev1.PersistentVolumeClaim, users []string) Volume {
	v := Volume{
		Name: pvc.Name, Type: pvc.Labels[naming.LabelVolumeType], Phase: string(pvc.Status.Phase),
		Created: pvc.CreationTimestamp.UTC().Format(time.RFC3339), Users: users,
		InTrash:   InTrash(pvc),
		TrashedAt: pvc.Annotations[naming.AnnotationTrashedAt],
		ExpireAt:  pvc.Annotations[naming.AnnotationExpireAt],
	}
	if v.Users == nil {
		v.Users = []string{}
	}
	if pvc.Spec.StorageClassName != nil {
		v.StorageClass = *pvc.Spec.StorageClassName
	}
	if len(pvc.Spec.AccessModes) > 0 {
		v.AccessMode = string(pvc.Spec.AccessModes[0])
	}
	if q, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		v.RequestedGiB = gib(q)
	}
	if q, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
		v.CapacityGiB = gib(q)
	}
	if v.Phase == string(corev1.ClaimBound) && v.CapacityGiB < v.RequestedGiB {
		v.Phase = "Resizing"
	}
	if !pvc.DeletionTimestamp.IsZero() {
		v.Phase = "Deleting"
	}
	return v
}

// Create creates a volume of the given type and size.
func (c *Client) Create(
	ctx context.Context, cfg *platform.Config, name, volType string, sizeGiB int64,
) (*Volume, error) {
	if !nameRE.MatchString(name) || len(name) > 50 {
		return nil, fmt.Errorf("invalid volume name %q: lowercase alphanumeric or '-', at most 50 characters", name)
	}
	if sizeGiB < 1 {
		return nil, fmt.Errorf("size must be at least 1 GiB")
	}
	volType, st, err := cfg.StorageType(volType)
	if err != nil {
		return nil, err
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: c.Namespace,
			Labels: map[string]string{naming.LabelManagedBy: naming.ManagedBy, naming.LabelVolumeType: volType},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{st.AccessMode},
			StorageClassName: &st.StorageClass,
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceStorage: *resource.NewQuantity(sizeGiB<<30, resource.BinarySI),
			}},
		},
	}
	if err := c.K8s.Create(ctx, pvc); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("volume %q already exists (check the trash)", name)
		}
		return nil, err
	}
	v := toVolume(pvc, nil)
	return &v, nil
}

func (c *Client) get(ctx context.Context, name string) (*corev1.PersistentVolumeClaim, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := c.K8s.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: name}, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return pvc, nil
}

// Users maps PVC names to the active pods mounting them.
func Users(pods []corev1.Pod) map[string][]string {
	out := map[string][]string{}
	for _, p := range pods {
		if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, v := range p.Spec.Volumes {
			if v.PersistentVolumeClaim != nil {
				out[v.PersistentVolumeClaim.ClaimName] = append(out[v.PersistentVolumeClaim.ClaimName], p.Name)
			}
		}
	}
	return out
}

func (c *Client) users(ctx context.Context) (map[string][]string, error) {
	pods := &corev1.PodList{}
	if err := c.K8s.List(ctx, pods, client.InNamespace(c.Namespace)); err != nil {
		return nil, err
	}
	return Users(pods.Items), nil
}

// List returns the volumes of the namespace; trash selects trashed or live volumes.
func (c *Client) List(ctx context.Context, trash bool) ([]Volume, error) {
	list := &corev1.PersistentVolumeClaimList{}
	if err := c.K8s.List(ctx, list, client.InNamespace(c.Namespace)); err != nil {
		return nil, err
	}
	users, err := c.users(ctx)
	if err != nil {
		return nil, err
	}
	out := []Volume{}
	for i := range list.Items {
		pvc := &list.Items[i]
		if InTrash(pvc) != trash {
			continue
		}
		u := users[pvc.Name]
		slices.Sort(u)
		out = append(out, toVolume(pvc, u))
	}
	slices.SortFunc(out, func(a, b Volume) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// Resize grows a volume. Shrinking is rejected.
func (c *Client) Resize(ctx context.Context, name string, sizeGiB int64) (*Volume, error) {
	pvc, err := c.get(ctx, name)
	if err != nil {
		return nil, err
	}
	if InTrash(pvc) {
		return nil, ErrInTrash
	}
	cur := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if sizeGiB<<30 < cur.Value() {
		return nil, fmt.Errorf("volumes can only grow: %s is %d GiB", name, gib(cur))
	}
	patch := client.MergeFrom(pvc.DeepCopy())
	pvc.Spec.Resources.Requests[corev1.ResourceStorage] = *resource.NewQuantity(sizeGiB<<30, resource.BinarySI)
	if err := c.K8s.Patch(ctx, pvc, patch); err != nil {
		return nil, err
	}
	v := toVolume(pvc, nil)
	return &v, nil
}

// Trash moves a volume to the trash; it is purged after retention. Volumes
// mounted by running pods cannot be trashed.
func (c *Client) Trash(ctx context.Context, name string, retention time.Duration) (*Volume, error) {
	pvc, err := c.get(ctx, name)
	if err != nil {
		return nil, err
	}
	if InTrash(pvc) {
		return nil, ErrInTrash
	}
	users, err := c.users(ctx)
	if err != nil {
		return nil, err
	}
	if u := users[name]; len(u) > 0 {
		return nil, fmt.Errorf("%w: mounted by %s", ErrInUse, strings.Join(u, ", "))
	}
	now := c.now().UTC()
	patch := client.MergeFrom(pvc.DeepCopy())
	if pvc.Labels == nil {
		pvc.Labels = map[string]string{}
	}
	if pvc.Annotations == nil {
		pvc.Annotations = map[string]string{}
	}
	pvc.Labels[naming.LabelTrash] = "true"
	pvc.Annotations[naming.AnnotationTrashedAt] = now.Format(time.RFC3339)
	pvc.Annotations[naming.AnnotationExpireAt] = now.Add(retention).Format(time.RFC3339)
	if err := c.K8s.Patch(ctx, pvc, patch); err != nil {
		return nil, err
	}
	v := toVolume(pvc, nil)
	return &v, nil
}

// SetExpiry changes when a trashed volume is purged.
func (c *Client) SetExpiry(ctx context.Context, name string, at time.Time) (*Volume, error) {
	pvc, err := c.get(ctx, name)
	if err != nil {
		return nil, err
	}
	if !InTrash(pvc) {
		return nil, fmt.Errorf("volume %s is not in the trash", name)
	}
	patch := client.MergeFrom(pvc.DeepCopy())
	pvc.Annotations[naming.AnnotationExpireAt] = at.UTC().Format(time.RFC3339)
	if err := c.K8s.Patch(ctx, pvc, patch); err != nil {
		return nil, err
	}
	v := toVolume(pvc, nil)
	return &v, nil
}

// Restore takes a volume out of the trash.
func (c *Client) Restore(ctx context.Context, name string) (*Volume, error) {
	pvc, err := c.get(ctx, name)
	if err != nil {
		return nil, err
	}
	if !InTrash(pvc) {
		return nil, fmt.Errorf("volume %s is not in the trash", name)
	}
	patch := client.MergeFrom(pvc.DeepCopy())
	delete(pvc.Labels, naming.LabelTrash)
	delete(pvc.Annotations, naming.AnnotationTrashedAt)
	delete(pvc.Annotations, naming.AnnotationExpireAt)
	if err := c.K8s.Patch(ctx, pvc, patch); err != nil {
		return nil, err
	}
	v := toVolume(pvc, nil)
	return &v, nil
}

// Purge permanently deletes a trashed volume.
func (c *Client) Purge(ctx context.Context, name string) error {
	pvc, err := c.get(ctx, name)
	if err != nil {
		return err
	}
	if !InTrash(pvc) {
		return fmt.Errorf("move volume %s to the trash first", name)
	}
	if err := c.K8s.Delete(ctx, pvc, client.Preconditions{UID: &pvc.UID}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

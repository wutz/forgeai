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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/training"
	projectv1 "github.com/wutz/workspace/api/v1alpha1"
)

var (
	ErrNotFound = errors.New("volume not found")
	// ErrInUse is returned when trashing a volume mounted by a running pod.
	ErrInUse = errors.New("volume is in use")
	// ErrInTrash is returned for operations on a trashed volume.
	ErrInTrash = errors.New("volume is in the trash")
	// ErrNotReady is returned when growing a volume that is not bound yet;
	// Kubernetes only expands bound claims.
	ErrNotReady = errors.New("volume is not ready")
	// ErrNotExpandable is returned when the storage class cannot grow its
	// volumes. The API refuses such a grow as Forbidden, which reads like a
	// missing permission, so the refusal is named here.
	ErrNotExpandable = errors.New("storage class does not allow growing volumes")
	// ErrPastExpiry is returned when a purge time is not in the future: the
	// volume would be purged at the next reconcile.
	ErrPastExpiry = errors.New("purge time must be in the future")
	// ErrNotShared is returned when a workload on several nodes mounts a
	// volume that only one node can attach.
	ErrNotShared = errors.New("volume is not shared between nodes")
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
	// Users are the running pods mounting the volume, file server excluded.
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

// InTrash reports whether a PVC is in the trash. A volume trashed before
// fileserver 0.3.0 counts as trashed until MigrateLegacyTrash moves its label.
func InTrash(pvc *corev1.PersistentVolumeClaim) bool {
	return pvc.Labels[naming.LabelFileServerIgnore] == naming.LiteralTrue ||
		pvc.Labels[naming.LabelTrashLegacy] == naming.LiteralTrue
}

// MigrateLegacyTrash moves a volume trashed before fileserver 0.3.0 from the
// legacy trash label to the ignore label the file server reads. Other volumes
// are left alone.
func MigrateLegacyTrash(ctx context.Context, k8s client.Client, pvc *corev1.PersistentVolumeClaim) error {
	if pvc.Labels[naming.LabelTrashLegacy] != naming.LiteralTrue {
		return nil
	}
	patch := client.MergeFrom(pvc.DeepCopy())
	pvc.Labels[naming.LabelFileServerIgnore] = naming.LiteralTrue
	delete(pvc.Labels, naming.LabelTrashLegacy)
	return k8s.Patch(ctx, pvc, patch)
}

// ExpireAt returns when a trashed PVC is purged.
func ExpireAt(pvc *corev1.PersistentVolumeClaim) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, pvc.Annotations[naming.AnnotationExpireAt])
	return t, err == nil
}

// Retention returns how long a volume trashed in namespace ns is kept: the
// owning project's trashRetention when it sets one, else platformDefault. A
// namespace or project that does not exist falls back to platformDefault.
func Retention(ctx context.Context, c client.Reader, ns string, platformDefault time.Duration) (time.Duration, error) {
	nsObj := &corev1.Namespace{}
	if err := c.Get(ctx, types.NamespacedName{Name: ns}, nsObj); err != nil {
		return platformDefault, client.IgnoreNotFound(err)
	}
	// A namespace without a project label has no project to ask: that is the
	// platform default, not a lookup of a project with an empty name.
	name := nsObj.Labels[naming.LabelProject]
	if name == "" {
		return platformDefault, nil
	}
	p := &projectv1.Project{}
	if err := c.Get(ctx, types.NamespacedName{Name: name}, p); err != nil {
		return platformDefault, client.IgnoreNotFound(err)
	}
	if p.Spec.TrashRetention != nil {
		return p.Spec.TrashRetention.Duration, nil
	}
	return platformDefault, nil
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

// maxSizeGiB bounds a volume size: larger sizes overflow the byte count
// (sizeGiB<<30) that a claim is sized with.
const maxSizeGiB = 1 << 20

// checkSize rejects a size outside 1 GiB..maxSizeGiB.
func checkSize(sizeGiB int64) error {
	if sizeGiB < 1 {
		return fmt.Errorf("size must be at least 1 GiB")
	}
	if sizeGiB > maxSizeGiB {
		return fmt.Errorf("size must be at most %d GiB (1 PiB)", maxSizeGiB)
	}
	return nil
}

// Create creates a volume of the given type and size.
func (c *Client) Create(
	ctx context.Context, cfg *platform.Config, name, volType string, sizeGiB int64,
) (*Volume, error) {
	if !nameRE.MatchString(name) || len(name) > 50 {
		return nil, fmt.Errorf("invalid volume name %q: lowercase alphanumeric or '-', at most 50 characters", name)
	}
	if err := checkSize(sizeGiB); err != nil {
		return nil, err
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

// Mountable checks that a workload can mount the named volumes: each must be a
// live volume that is bound or pending. A pending claim binds when the first pod
// uses it on a WaitForFirstConsumer class (local-path), so refusing it would leave
// a new volume unmountable for good; the pod waits on the claim until it binds.
// A workload on several nodes needs ReadWriteMany volumes, since a ReadWriteOnce
// volume attaches to one node. Trashed volumes are refused: they are about to be purged.
func (c *Client) Mountable(ctx context.Context, names []string, nodes int32) error {
	for _, name := range names {
		pvc, err := c.get(ctx, name)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return fmt.Errorf("%w: %s is not a volume of this project", ErrNotFound, name)
			}
			return err
		}
		if InTrash(pvc) {
			return fmt.Errorf("%w: %s", ErrInTrash, name)
		}
		if !pvc.DeletionTimestamp.IsZero() {
			return fmt.Errorf("volume %s is being deleted", name)
		}
		switch pvc.Status.Phase {
		case corev1.ClaimBound, corev1.ClaimPending:
		default:
			return fmt.Errorf("volume %s is %s and cannot be mounted", name, pvc.Status.Phase)
		}
		if nodes > 1 && !slices.Contains(pvc.Spec.AccessModes, corev1.ReadWriteMany) {
			return fmt.Errorf("%w: %s is ReadWriteOnce; a job with several workers needs a shared volume",
				ErrNotShared, name)
		}
	}
	return nil
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

// users maps PVC names to the active pods and unfinished jobs that block them.
// File server pods do not count: the file server drops trashed volumes when it
// reconciles, and its Recreate rollout stops the pod that mounted them.
func (c *Client) users(ctx context.Context) (map[string][]string, error) {
	pods := &corev1.PodList{}
	if err := c.K8s.List(ctx, pods, client.InNamespace(c.Namespace)); err != nil {
		return nil, err
	}
	var others []corev1.Pod
	for _, p := range pods.Items {
		if p.Labels[naming.LabelComponent] != naming.ComponentFileServer {
			others = append(others, p)
		}
	}
	out := Users(others)
	jobs, err := c.jobUsers(ctx)
	if err != nil {
		return nil, err
	}
	for pvc, names := range jobs {
		out[pvc] = append(out[pvc], names...)
	}
	return out, nil
}

// jobUsers maps PVC names to the unfinished jobs that mount them. A queued job has
// no pods yet, but it mounts its volumes once admitted, and a stopped job resumes
// where it left off, so both hold their volumes. A job that cannot be read is
// skipped: it cannot show the mounts it holds.
func (c *Client) jobUsers(ctx context.Context) (map[string][]string, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(kf.TrainJobGVK.GroupVersion().WithKind("TrainJobList"))
	if err := c.K8s.List(ctx, list, client.InNamespace(c.Namespace)); err != nil {
		if meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, err
	}
	out := map[string][]string{}
	for i := range list.Items {
		tj := &kf.TrainJob{}
		if err := kf.FromUnstructured(list.Items[i].Object, tj); err != nil {
			continue
		}
		if phase, _ := training.JobPhase(tj, nil); phase == training.PhaseSucceeded || phase == training.PhaseFailed {
			continue
		}
		f, err := training.TrainJobForm(tj)
		if err != nil {
			continue
		}
		for _, m := range f.Volumes {
			out[m.Volume] = append(out[m.Volume], tj.Name)
		}
	}
	return out, nil
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
	if pvc.Status.Phase != corev1.ClaimBound {
		return nil, fmt.Errorf("%w: %s is still being provisioned, grow it once it is Bound", ErrNotReady, name)
	}
	if err := checkSize(sizeGiB); err != nil {
		return nil, err
	}
	cur := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if sizeGiB<<30 < cur.Value() {
		return nil, fmt.Errorf("volumes can only grow: %s is %d GiB", name, gib(cur))
	}
	patch := client.MergeFrom(pvc.DeepCopy())
	pvc.Spec.Resources.Requests[corev1.ResourceStorage] = *resource.NewQuantity(sizeGiB<<30, resource.BinarySI)
	if err := c.K8s.Patch(ctx, pvc, patch); err != nil {
		if apierrors.IsForbidden(err) && strings.Contains(err.Error(), "must support resize") {
			sc := ""
			if pvc.Spec.StorageClassName != nil {
				sc = *pvc.Spec.StorageClassName
			}
			return nil, fmt.Errorf("%w: %s", ErrNotExpandable, sc)
		}
		return nil, err
	}
	v := toVolume(pvc, nil)
	return &v, nil
}

// Trash moves a volume to the trash; it is purged after retention. Volumes
// mounted by running pods cannot be trashed, but file server pods do not
// count: they are rebuilt without the trashed volume.
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
	pvc.Labels[naming.LabelFileServerIgnore] = naming.LiteralTrue
	pvc.Annotations[naming.AnnotationTrashedAt] = now.Format(time.RFC3339)
	pvc.Annotations[naming.AnnotationExpireAt] = now.Add(retention).Format(time.RFC3339)
	if err := c.K8s.Patch(ctx, pvc, patch); err != nil {
		return nil, err
	}
	v := toVolume(pvc, nil)
	return &v, nil
}

// SetExpiry changes when a trashed volume is purged. The time must be in the
// future, so a mistyped date cannot purge the volume at the next reconcile.
func (c *Client) SetExpiry(ctx context.Context, name string, at time.Time) (*Volume, error) {
	if !at.After(c.now()) {
		return nil, ErrPastExpiry
	}
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

// ResetExpiry puts a trashed volume's purge time back to the default: retention
// after it was trashed, under the retention its project has now. A volume with
// no trash time counts from now, as the controller does for one labelled by hand.
func (c *Client) ResetExpiry(ctx context.Context, name string, retention time.Duration) (*Volume, error) {
	pvc, err := c.get(ctx, name)
	if err != nil {
		return nil, err
	}
	if !InTrash(pvc) {
		return nil, fmt.Errorf("volume %s is not in the trash", name)
	}
	now := c.now()
	base := now
	if trashedAt, err := time.Parse(time.RFC3339, pvc.Annotations[naming.AnnotationTrashedAt]); err == nil {
		base = trashedAt
	}
	at := base.Add(retention)
	if !at.After(now) {
		return nil, ErrPastExpiry
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
	delete(pvc.Labels, naming.LabelFileServerIgnore)
	delete(pvc.Labels, naming.LabelTrashLegacy)
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

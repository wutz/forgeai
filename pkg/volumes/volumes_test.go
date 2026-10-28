package volumes

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	instancetypev1 "github.com/wutz/forgeai-instancetype-operator/api/v1alpha1"
	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/training"
	projectv1 "github.com/wutz/project-operator/api/v1alpha1"
)

func TestLifecycle(t *testing.T) {
	ctx := context.Background()
	k8s := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	c := &Client{K8s: k8s, Namespace: "forge-a", Now: func() time.Time { return now }}
	cfg, _ := platform.Parse([]byte("storageTypes: {shared: {storageClass: sc}}\ndefaultStorageType: shared"))

	if _, err := c.Create(ctx, cfg, "Bad_Name", "", 10); err == nil {
		t.Error("expected invalid name")
	}
	if _, err := c.Create(ctx, cfg, "data", "nvme", 10); err == nil {
		t.Error("expected unknown type")
	}
	v, err := c.Create(ctx, cfg, "data", "", 10)
	if err != nil || v.Type != "shared" || v.StorageClass != "sc" || v.RequestedGiB != 10 || v.AccessMode != "ReadWriteMany" {
		t.Fatalf("create %+v %v", v, err)
	}
	if _, err := c.Create(ctx, cfg, "data", "", 10); err == nil {
		t.Error("expected already exists")
	}
	if _, err := c.Resize(ctx, "data", 20); !errors.Is(err, ErrNotReady) {
		t.Fatalf("resize before bound: %v", err)
	}
	pvc := &corev1.PersistentVolumeClaim{}
	if err := k8s.Get(ctx, types.NamespacedName{Namespace: "forge-a", Name: "data"}, pvc); err != nil {
		t.Fatal(err)
	}
	pvc.Status.Phase = corev1.ClaimBound
	pvc.Status.Capacity = corev1.ResourceList{corev1.ResourceStorage: pvc.Spec.Resources.Requests[corev1.ResourceStorage]}
	if err := k8s.Status().Update(ctx, pvc); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Resize(ctx, "data", 5); err == nil {
		t.Error("expected shrink error")
	}
	if v, err := c.Resize(ctx, "data", 20); err != nil || v.RequestedGiB != 20 {
		t.Fatalf("resize %+v %v", v, err)
	}

	// In use by a running pod.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "job-0", Namespace: "forge-a"},
		Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	if err := k8s.Create(ctx, pod); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Trash(ctx, "data", time.Hour); !errors.Is(err, ErrInUse) {
		t.Fatalf("expected in use, got %v", err)
	}
	if list, _ := c.List(ctx, false); len(list) != 1 || list[0].Users[0] != "job-0" {
		t.Fatalf("list %+v", list)
	}
	pod.Status.Phase = corev1.PodSucceeded
	if err := k8s.Status().Update(ctx, pod); err != nil {
		t.Fatal(err)
	}

	v, err = c.Trash(ctx, "data", 48*time.Hour)
	if err != nil || !v.InTrash || v.ExpireAt != "2026-10-03T00:00:00Z" {
		t.Fatalf("trash %+v %v", v, err)
	}
	if live, _ := c.List(ctx, false); len(live) != 0 {
		t.Fatalf("live %+v", live)
	}
	if trash, _ := c.List(ctx, true); len(trash) != 1 {
		t.Fatalf("trash %+v", trash)
	}
	if _, err := c.Resize(ctx, "data", 30); !errors.Is(err, ErrInTrash) {
		t.Fatalf("resize in trash: %v", err)
	}
	if _, err := c.SetExpiry(ctx, "data", now.Add(-time.Hour)); !errors.Is(err, ErrPastExpiry) {
		t.Errorf("SetExpiry in the past: %v", err)
	}
	if _, err := c.SetExpiry(ctx, "data", time.Time{}); !errors.Is(err, ErrPastExpiry) {
		t.Errorf("SetExpiry zero time: %v", err)
	}
	if v, err := c.SetExpiry(ctx, "data", now.Add(time.Hour)); err != nil || v.ExpireAt != "2026-10-01T01:00:00Z" {
		t.Fatalf("expiry %+v %v", v, err)
	}
	if v, err := c.Restore(ctx, "data"); err != nil || v.InTrash || v.ExpireAt != "" {
		t.Fatalf("restore %+v %v", v, err)
	}
	if err := c.Purge(ctx, "data"); err == nil {
		t.Fatal("purge must require the trash")
	}
	if _, err := c.Trash(ctx, "data", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := c.Purge(ctx, "data"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.get(ctx, "data"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after purge: %v", err)
	}
}

func TestRetentionUsesProjectOverride(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := v1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := projectv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	k8s := fake.NewClientBuilder().WithScheme(s).WithObjects(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "forge-a", Labels: map[string]string{naming.LabelProject: "alpha"}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "forge-b", Labels: map[string]string{naming.LabelProject: "beta"}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "forge-c"}},
		&projectv1.Project{ObjectMeta: metav1.ObjectMeta{Name: "alpha"}, Spec: projectv1.ProjectSpec{
			TrashRetention: &metav1.Duration{Duration: time.Hour}}},
		&projectv1.Project{ObjectMeta: metav1.ObjectMeta{Name: "beta"}},
	).Build()
	const platformDefault = 7 * 24 * time.Hour

	for _, tc := range []struct {
		name, ns string
		want     time.Duration
	}{
		{"project override", "forge-a", time.Hour},
		{"project without override", "forge-b", platformDefault},
		{"unknown namespace", "missing", platformDefault},
		{"namespace without project label", "forge-c", platformDefault},
	} {
		got, err := Retention(ctx, k8s, tc.ns, platformDefault)
		if err != nil || got != tc.want {
			t.Errorf("%s: Retention = %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
}

func TestTrashIgnoresFileServerPods(t *testing.T) {
	ctx := context.Background()
	k8s := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	c := &Client{K8s: k8s, Namespace: "forge-a"}
	cfg, _ := platform.Parse([]byte("storageTypes: {shared: {storageClass: sc}}\ndefaultStorageType: shared"))
	if _, err := c.Create(ctx, cfg, "data", "", 10); err != nil {
		t.Fatal(err)
	}
	mount := []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}
	fsPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "forgeai-files-default-0", Namespace: "forge-a", Labels: map[string]string{
			naming.LabelComponent: naming.ComponentFileServer}},
		Spec:   corev1.PodSpec{Volumes: mount},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	if err := k8s.Create(ctx, fsPod); err != nil {
		t.Fatal(err)
	}
	// The GUI disables Delete while Users is non-empty, so the file server must not show up there.
	if live, _ := c.List(ctx, false); len(live) != 1 || len(live[0].Users) != 0 {
		t.Fatalf("file server pod listed as user: %+v", live)
	}
	if _, err := c.Trash(ctx, "data", time.Hour); err != nil {
		t.Fatalf("file server pod must not block trash: %v", err)
	}

	// Any other running pod still blocks it.
	job := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "job-0", Namespace: "forge-a"},
		Spec:       corev1.PodSpec{Volumes: mount},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	if err := k8s.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Restore(ctx, "data"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Trash(ctx, "data", time.Hour); !errors.Is(err, ErrInUse) {
		t.Fatalf("job pod must block trash, got %v", err)
	}
}

func TestMountable(t *testing.T) {
	ctx := context.Background()
	pvc := func(name string, phase corev1.PersistentVolumeClaimPhase, mode corev1.PersistentVolumeAccessMode, trashed bool) *corev1.PersistentVolumeClaim {
		p := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "forge-a"},
			Spec:       corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{mode}},
			Status:     corev1.PersistentVolumeClaimStatus{Phase: phase},
		}
		if trashed {
			p.Labels = map[string]string{naming.LabelFileServerIgnore: "true"}
		}
		return p
	}
	k8s := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(
		pvc("shared", corev1.ClaimBound, corev1.ReadWriteMany, false),
		pvc("local", corev1.ClaimBound, corev1.ReadWriteOnce, false),
		pvc("pending", corev1.ClaimPending, corev1.ReadWriteMany, false),
		pvc("pending-local", corev1.ClaimPending, corev1.ReadWriteOnce, false),
		pvc("gone-soon", corev1.ClaimBound, corev1.ReadWriteMany, true),
	).Build()
	c := &Client{K8s: k8s, Namespace: "forge-a"}

	cases := []struct {
		name  string
		names []string
		nodes int32
		want  error
	}{
		{"no volumes", nil, 4, nil},
		{"bound shared on many nodes", []string{"shared"}, 4, nil},
		{"bound single-node volume on one node", []string{"local"}, 1, nil},
		{"single-node volume on several nodes", []string{"local"}, 2, ErrNotShared},
		{"missing volume", []string{"nope"}, 1, ErrNotFound},
		{"trashed volume", []string{"gone-soon"}, 1, ErrInTrash},
		{"pending volume binds at first use", []string{"pending"}, 1, nil},
		{"pending single-node volume on several nodes", []string{"pending-local"}, 2, ErrNotShared},
	}
	for _, tc := range cases {
		err := c.Mountable(ctx, tc.names, tc.nodes)
		if tc.want == nil && err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}
		if tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
}

// trainJob returns a TrainJob mounting the named volume, as the GUI renders it,
// stored the way the cluster client reads it.
func trainJob(t *testing.T, name, volume string, conditions ...metav1.Condition) *unstructured.Unstructured {
	t.Helper()
	cfg, err := platform.Parse([]byte("storageTypes: {shared: {storageClass: sc}}\ndefaultStorageType: shared"))
	if err != nil {
		t.Fatal(err)
	}
	form := training.Form{
		Name: name, Image: "img:1", InstanceType: "cpu.small", Workers: 1,
		Volumes: []training.VolumeMount{{Volume: volume, MountPath: "/data"}},
	}
	it := &instancetypev1.InstanceType{ObjectMeta: metav1.ObjectMeta{Name: "cpu.small"}}
	tj, err := training.TrainJob(&form, training.Context{Namespace: "forge-a", InstanceType: it, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if len(conditions) > 0 {
		tj.Status = &kf.TrainJobStatus{Conditions: conditions}
	}
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tj)
	if err != nil {
		t.Fatal(err)
	}
	u := &unstructured.Unstructured{Object: obj}
	u.SetGroupVersionKind(kf.TrainJobGVK)
	return u
}

// An unfinished TrainJob holds the volumes it mounts, queued or running; a
// finished one releases them.
func TestTrashWaitsForUnfinishedJobs(t *testing.T) {
	ctx := context.Background()
	cfg, _ := platform.Parse([]byte("storageTypes: {shared: {storageClass: sc}}\ndefaultStorageType: shared"))
	suspended := metav1.Condition{Type: kf.ConditionSuspended, Status: metav1.ConditionTrue, Reason: "Suspended"}
	complete := metav1.Condition{Type: kf.ConditionComplete, Status: metav1.ConditionTrue, Reason: "Completed"}
	failed := metav1.Condition{Type: kf.ConditionFailed, Status: metav1.ConditionTrue, Reason: "Failed"}

	for _, tc := range []struct {
		name    string
		job     *unstructured.Unstructured
		blocked bool
	}{
		{"queued", trainJob(t, "queued", "data", suspended), true},
		{"pending", trainJob(t, "pending", "data"), true},
		{"succeeded", trainJob(t, "done", "data", complete), false},
		{"failed", trainJob(t, "broken", "data", failed), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k8s := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			c := &Client{K8s: k8s, Namespace: "forge-a"}
			if _, err := c.Create(ctx, cfg, "data", "", 10); err != nil {
				t.Fatal(err)
			}
			if err := k8s.Create(ctx, tc.job); err != nil {
				t.Fatal(err)
			}
			live, err := c.List(ctx, false)
			if err != nil {
				t.Fatal(err)
			}
			if got := live[0].Users; (len(got) > 0) != tc.blocked {
				t.Fatalf("Users = %v, blocked want %v", got, tc.blocked)
			}
			_, err = c.Trash(ctx, "data", time.Hour)
			if tc.blocked && !errors.Is(err, ErrInUse) {
				t.Fatalf("Trash = %v, want ErrInUse", err)
			}
			if !tc.blocked && err != nil {
				t.Fatalf("Trash = %v, a finished job must not block it", err)
			}
		})
	}
}

// TestResetExpiry checks that the default purge time is retention after the
// volume was trashed, under the retention in force now, and that it is refused
// once that time has passed.
func TestResetExpiry(t *testing.T) {
	ctx := context.Background()
	k8s := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	c := &Client{K8s: k8s, Namespace: "forge-a", Now: func() time.Time { return now }}
	cfg, _ := platform.Parse([]byte("storageTypes: {shared: {storageClass: sc}}\ndefaultStorageType: shared"))
	if _, err := c.Create(ctx, cfg, "data", "", 10); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ResetExpiry(ctx, "data", time.Hour); err == nil {
		t.Fatal("reset must require the trash")
	}
	if _, err := c.Trash(ctx, "data", 48*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetExpiry(ctx, "data", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	// The project now keeps trash for 7 days: the default counts from the trash time.
	v, err := c.ResetExpiry(ctx, "data", 7*24*time.Hour)
	if err != nil || v.ExpireAt != "2026-10-08T00:00:00Z" {
		t.Fatalf("reset %+v %v", v, err)
	}
	if v.TrashedAt != "2026-10-01T00:00:00Z" {
		t.Errorf("trashed-at changed: %q", v.TrashedAt)
	}

	// A default that has already passed is refused, and the purge time is kept.
	now = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	if _, err := c.ResetExpiry(ctx, "data", 7*24*time.Hour); !errors.Is(err, ErrPastExpiry) {
		t.Fatalf("reset past its default: %v", err)
	}
	live, _ := c.List(ctx, true)
	if len(live) != 1 || live[0].ExpireAt != "2026-10-08T00:00:00Z" {
		t.Fatalf("expiry changed after refusal: %+v", live)
	}
}

// A storage class that cannot grow its volumes is named as such. The API
// refuses the patch as Forbidden, and that must not read as a missing permission.
func TestResizeNamesNonExpandableClass(t *testing.T) {
	ctx := context.Background()
	refused := apierrors.NewForbidden(corev1.Resource("persistentvolumeclaims"), "data", errors.New(
		"only dynamically provisioned pvc can be resized and the storageclass that provisions the pvc must support resize"))
	k8s := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithInterceptorFuncs(interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			return refused
		},
	}).Build()
	c := &Client{K8s: k8s, Namespace: "forge-a", Now: time.Now}
	cfg, _ := platform.Parse([]byte("storageTypes: {shared: {storageClass: sc}}\ndefaultStorageType: shared"))
	if _, err := c.Create(ctx, cfg, "data", "", 10); err != nil {
		t.Fatal(err)
	}
	pvc := &corev1.PersistentVolumeClaim{}
	if err := k8s.Get(ctx, types.NamespacedName{Namespace: "forge-a", Name: "data"}, pvc); err != nil {
		t.Fatal(err)
	}
	pvc.Status.Phase = corev1.ClaimBound
	if err := k8s.Status().Update(ctx, pvc); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Resize(ctx, "data", 20); !errors.Is(err, ErrNotExpandable) {
		t.Fatalf("resize on a class without expansion: %v", err)
	}
}

func TestLegacyTrashMigrates(t *testing.T) {
	ctx := context.Background()
	legacy := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
		Name: "old", Namespace: "forge-a", Labels: map[string]string{naming.LabelTrashLegacy: "true"},
	}}
	k8s := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(legacy).Build()
	c := &Client{K8s: k8s, Namespace: "forge-a"}
	key := client.ObjectKey{Namespace: "forge-a", Name: "old"}

	pvc := &corev1.PersistentVolumeClaim{}
	if err := k8s.Get(ctx, key, pvc); err != nil {
		t.Fatal(err)
	}
	if !InTrash(pvc) {
		t.Fatal("a volume with the legacy label must count as trashed")
	}
	if err := MigrateLegacyTrash(ctx, k8s, pvc); err != nil {
		t.Fatal(err)
	}
	if _, legacyLeft := pvc.Labels[naming.LabelTrashLegacy]; legacyLeft || pvc.Labels[naming.LabelFileServerIgnore] != "true" {
		t.Fatalf("labels after migration = %v", pvc.Labels)
	}
	if err := MigrateLegacyTrash(ctx, k8s, pvc); err != nil {
		t.Fatalf("migrating twice: %v", err)
	}

	if _, err := c.Restore(ctx, "old"); err != nil {
		t.Fatal(err)
	}
	if err := k8s.Get(ctx, key, pvc); err != nil {
		t.Fatal(err)
	}
	if InTrash(pvc) {
		t.Fatalf("restore must clear both labels, got %v", pvc.Labels)
	}
}

package volumes

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
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
	k8s := fake.NewClientBuilder().WithScheme(s).WithObjects(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "forge-a", Labels: map[string]string{naming.LabelProject: "alpha"}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "forge-b", Labels: map[string]string{naming.LabelProject: "beta"}}},
		&v1.Project{ObjectMeta: metav1.ObjectMeta{Name: "alpha"}, Spec: v1.ProjectSpec{
			TrashRetention: &metav1.Duration{Duration: time.Hour}}},
		&v1.Project{ObjectMeta: metav1.ObjectMeta{Name: "beta"}},
	).Build()
	const platformDefault = 7 * 24 * time.Hour

	for _, tc := range []struct {
		name, ns string
		want     time.Duration
	}{
		{"project override", "forge-a", time.Hour},
		{"project without override", "forge-b", platformDefault},
		{"unknown namespace", "missing", platformDefault},
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

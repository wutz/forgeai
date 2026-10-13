package volumes

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/scclabs/forgeai/pkg/platform"
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

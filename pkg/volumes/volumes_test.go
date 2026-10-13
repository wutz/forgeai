package volumes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
)

const testNS = "pod-prod"

func newClient(t *testing.T, objs ...runtime.Object) *Client {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v1.AddToScheme(s)
	return &Client{K8s: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build(), Namespace: testNS}
}

func testConfig(t *testing.T) *platform.Config {
	t.Helper()
	cfg, err := platform.Parse([]byte(`
releaseVersion: test
storageTypes:
  high-perf: {storageClass: shared-vast-cs}
`))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestLifecycle(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	cfg := testConfig(t)

	if _, err := c.Create(ctx, cfg, "u1", "Bad_ID", "high-perf", 1); err == nil {
		t.Fatal("invalid ID accepted")
	}
	if _, err := c.Create(ctx, cfg, "u1", "data", "nope", 1); err == nil {
		t.Fatal("unknown storage type accepted")
	}
	v, err := c.Create(ctx, cfg, "u1", "data", "high-perf", 10)
	if err != nil || v.Status != "Pending" {
		t.Fatalf("v=%+v err=%v", v, err)
	}
	// Idempotent.
	if _, err := c.Create(ctx, cfg, "u1", "data", "high-perf", 10); err != nil {
		t.Fatal(err)
	}

	name := naming.VolumeClaimName(testNS, "u1", "data")
	pvc := &corev1.PersistentVolumeClaim{}
	if err := c.K8s.Get(ctx, types.NamespacedName{Namespace: testNS, Name: name}, pvc); err != nil {
		t.Fatal(err)
	}
	if *pvc.Spec.StorageClassName != "shared-vast-cs" || pvc.Spec.AccessModes[0] != corev1.ReadWriteMany {
		t.Fatalf("spec=%+v", pvc.Spec)
	}
	if q := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; q.Value() != 10<<30 {
		t.Fatalf("size=%s", q.String())
	}

	pvc.Status.Phase = corev1.ClaimBound
	pvc.Status.Capacity = corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")}
	if err := c.K8s.Status().Update(ctx, pvc); err != nil {
		t.Fatal(err)
	}
	list, err := c.List(ctx, "u1", "")
	if err != nil || len(list) != 1 || list[0].Status != "Available" || list[0].SizeGB != 10 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if list, _ := c.List(ctx, "u2", ""); len(list) != 0 {
		t.Fatalf("other user sees %+v", list)
	}

	if _, err := c.Resize(ctx, "u1", "data", 20); err != nil {
		t.Fatal(err)
	}
	_ = c.K8s.Get(ctx, types.NamespacedName{Namespace: testNS, Name: name}, pvc)
	if q := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; q.Value() != 20<<30 {
		t.Fatalf("resized=%s", q.String())
	}

	if _, err := c.Get(ctx, "u1", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	if err := c.Delete(ctx, "u1", "data"); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, "u1", "data"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestDeleteInUse(t *testing.T) {
	ctx := context.Background()
	name := naming.VolumeClaimName(testNS, "u1", "data")
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNS}}
	job := &v1.TrainingJob{
		ObjectMeta: metav1.ObjectMeta{Name: "j1", Namespace: testNS},
		Spec:       v1.TrainingJobSpec{UserID: "u1", VolumeMounts: []v1.VolumeMount{{VolumeID: "data", MountPath: "/data"}}},
	}
	c := newClient(t, pvc, job)
	if err := c.Delete(ctx, "u1", "data"); !errors.Is(err, ErrInUse) {
		t.Fatalf("err=%v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: testNS},
		Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: name},
		}}}},
	}
	c = newClient(t, pvc, pod)
	users, err := c.Users(ctx, name)
	if err != nil || len(users) != 1 || users[0] != "pod/p1" {
		t.Fatalf("users=%v err=%v", users, err)
	}
	pod.Status.Phase = corev1.PodSucceeded
	c = newClient(t, pvc, pod)
	if err := c.Delete(ctx, "u1", "data"); err != nil {
		t.Fatalf("finished pod should not block deletion: %v", err)
	}
}

func TestUsage(t *testing.T) {
	var lookbacks []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.URL.Query().Get("pvc_name") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		lookbacks = append(lookbacks, r.URL.Query().Get("lookback"))
		if r.URL.Query().Get("lookback") == "" {
			_, _ = w.Write([]byte(`{"status":"success","code":0,"data":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","code":0,"data":[{"total_bytes":10737418240,"used_bytes":3221225472}]}`))
	}))
	defer srv.Close()

	u := &UsageClient{BaseURL: srv.URL, Token: "tok"}
	info, err := u.Usage(context.Background(), testNS, "u1", "data")
	if err != nil {
		t.Fatal(err)
	}
	if info.TotalGB != 10 || info.UsedGB != 3 || info.UsedPercentage != 30 ||
		len(lookbacks) != 2 || lookbacks[1] != "30d" {
		t.Fatalf("info=%+v lookbacks=%v", info, lookbacks)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","code":0,"data":[]}`))
	}))
	defer empty.Close()
	u.BaseURL = empty.URL
	if _, err := u.Usage(context.Background(), testNS, "u1", "data"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/naming"
)

func gpuNode(name, pool string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{naming.LabelNodePool: pool}},
		Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("64"), corev1.ResourceMemory: resource.MustParse("1000Gi"),
			"nvidia.com/gpu": resource.MustParse("8"),
		}},
	}
}

func TestVolumes(t *testing.T) {
	s, k := newServer(t)
	h := s.Handler()
	vol := "/ngc/apis/pod/v1alpha2/users/" + user + "/volumes"

	code, out := do(t, h, "POST", vol+"?volumeID=data", apiKey, map[string]any{"sizeGB": 10})
	if code != 200 || out["id"] != "data" || out["status"] != "Pending" || out["type"] != "high-perf" {
		t.Fatalf("create: %d %v", code, out)
	}
	if code, out = do(t, h, "POST", vol+"?volumeID=data2", apiKey, map[string]any{"sizeGB": 10, "type": "nope"}); code != 409 {
		t.Fatalf("unknown type: %d %v", code, out)
	}
	if code, out = do(t, h, "GET", vol, apiKey, nil); code != 200 || len(out["volumes"].([]any)) != 1 {
		t.Fatalf("list: %d %v", code, out)
	}
	if code, out = do(t, h, "GET", vol+"/nope", apiKey, nil); code != 404 || out["msg"] != "Volume 'nope' was not found" {
		t.Fatalf("get missing: %d %v", code, out)
	}
	if code, out = do(t, h, "POST", vol+"/data/resize", apiKey, map[string]any{"sizeGB": 20}); code != 200 {
		t.Fatalf("resize: %d %v", code, out)
	}
	if code, out = do(t, h, "GET", vol+"/data/usage", apiKey, nil); code != 404 {
		t.Fatalf("usage without monitor: %d %v", code, out)
	}

	job := &v1.TrainingJob{
		ObjectMeta: metav1.ObjectMeta{Name: "j1", Namespace: "pod-prod"},
		Spec:       v1.TrainingJobSpec{UserID: user, VolumeMounts: []v1.VolumeMount{{VolumeID: "data", MountPath: "/data"}}},
	}
	if err := k.Create(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if code, out = do(t, h, "DELETE", vol+"/data", apiKey, nil); code != 409 {
		t.Fatalf("delete in use: %d %v", code, out)
	}
	_ = k.Delete(context.Background(), job)
	if code, out = do(t, h, "DELETE", vol+"/data", apiKey, nil); code != 200 || out["status"] != "success" {
		t.Fatalf("delete: %d %v", code, out)
	}
}

func TestImages(t *testing.T) {
	s, k := newServer(t)
	h := s.Handler()
	code, out := do(t, h, "GET", "/ngc/apis/pod/v1alpha2/users/"+user+"/images", apiKey, nil)
	if imgs, ok := out["images"].([]any); code != 200 || !ok || len(imgs) != 0 {
		t.Fatalf("images without registry: %d %v", code, out)
	}

	req := httptestBody(t, h, "POST", "/ngc/apis/pod/v1alpha2/images/warmup", []map[string]string{{"id": "img:1"}})
	if req.Code != 200 || req.Body.String() == "" {
		t.Fatalf("warmup: %d %s", req.Code, req.Body.String())
	}
	ds := &appsv1.DaemonSet{}
	if err := k.Get(context.Background(), client.ObjectKey{Namespace: "pod-prod", Name: naming.ImageWarmupName("img:1")}, ds); err != nil {
		t.Fatal(err)
	}
}

func TestPoolsAndResources(t *testing.T) {
	s, k := newServer(t, gpuNode("n1", "default"), gpuNode("n2", "train"))
	h := s.Handler()

	code, out := do(t, h, "GET", "/ngc/apis/pool/v1alpha2/pools", apiKey, nil)
	ps, _ := out["pools"].(map[string]any)
	if code != 200 || len(ps) != 2 || ps["train"].(map[string]any)["node_count"] != 1.0 {
		t.Fatalf("pools: %d %v", code, out)
	}
	if code, out = do(t, h, "GET", "/ngc/apis/pool/v1alpha2/train/pods", apiKey, nil); code != 200 || out["pool_id"] != "train" {
		t.Fatalf("pool pods: %d %v", code, out)
	}

	code, out = do(t, h, "GET", "/ngc/apis/pod/v1alpha2/resources?include_details=true", apiKey, nil)
	rs, _ := out["resources"].([]any)
	if code != 200 || len(rs) != 1 {
		t.Fatalf("resources: %d %v", code, out)
	}
	usage := rs[0].(map[string]any)["usage"].(map[string]any)
	if usage["total"] != 16.0 || usage["free"] != 16.0 || len(usage["details"].([]any)) != 2 {
		t.Fatalf("usage: %v", usage)
	}
	if code, out = do(t, h, "GET", "/ngc/apis/pod/v1alpha2/resources/g1?pool_id=train", apiKey, nil); code != 200 {
		t.Fatalf("resource: %d %v", code, out)
	}
	if u := out["resources"].([]any)[0].(map[string]any)["usage"].(map[string]any); u["total"] != 8.0 {
		t.Fatalf("pool usage: %v", u)
	}

	reqs := "/ngc/apis/node/v1alpha2/users/" + user + "/pools/requests"
	body := map[string]any{"dst_pool": "train", "resourceID": "g1", "nnodes": 1}
	if code, out = do(t, h, "POST", reqs+"?request_id=r1", apiKey, map[string]any{"dst_pool": "train", "resourceID": "nope"}); code != 400 {
		t.Fatalf("bad request: %d %v", code, out)
	}
	if code, out = do(t, h, "POST", reqs+"?request_id=r1", apiKey, body); code != 200 || out["status"] != "Pending" || out["src_pool"] != "default" {
		t.Fatalf("create request: %d %v", code, out)
	}
	if code, out = do(t, h, "GET", reqs+"/r1", apiKey, nil); code != 200 || out["request_id"] != "r1" {
		t.Fatalf("get request: %d %v", code, out)
	}
	if code, out = do(t, h, "GET", reqs+"/nope", apiKey, nil); code != 200 || len(out) != 0 {
		t.Fatalf("get missing request: %d %v", code, out)
	}

	pr := &v1.PoolRequest{}
	if err := k.Get(context.Background(), client.ObjectKey{Namespace: "pod-prod", Name: naming.PoolRequestName("pod-prod", user, "r1")}, pr); err != nil {
		t.Fatal(err)
	}
	pr.Status.Phase = v1.PoolRequestSuccess
	if err := k.Status().Update(context.Background(), pr); err != nil {
		t.Fatal(err)
	}
	if code, out = do(t, h, "DELETE", reqs+"/r1", apiKey, nil); code != 403 || out["request_id"] != "r1" {
		t.Fatalf("delete completed: %d %v", code, out)
	}
	if code, out = do(t, h, "DELETE", reqs+"/nope", apiKey, nil); code != 404 || out["msg"] != "Pool request was not found" {
		t.Fatalf("delete missing: %d %v", code, out)
	}

	pool := "/ngc/apis/node/v1alpha2/users/" + user + "/pools/"
	if code, out = do(t, h, "DELETE", pool+"default", apiKey, nil); code != 400 {
		t.Fatalf("delete default pool: %d %v", code, out)
	}
	if code, out = do(t, h, "DELETE", pool+"train", apiKey, nil); code != 200 || out["msg"] != "Pool 'train' was deleted successfully" {
		t.Fatalf("delete pool: %d %v", code, out)
	}
	n := &corev1.Node{}
	_ = k.Get(context.Background(), client.ObjectKey{Name: "n2"}, n)
	if n.Labels[naming.LabelNodePool] != "default" {
		t.Fatalf("labels: %v", n.Labels)
	}
}

func httptestBody(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

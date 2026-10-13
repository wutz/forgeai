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
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/naming"
)

func gpuNode(name string) *corev1.Node {
	return &corev1.Node{
		Name: name,
		Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("64"), corev1.ResourceMemory: resource.MustParse("1000Gi"),
			"nvidia.com/gpu": resource.MustParse("8"),
		}},
	}
}

// volumeBody is a create or resize request; an empty type is omitted.
func volumeBody(sizeGB int, typ string) map[string]any {
	b := map[string]any{"sizeGB": sizeGB}
	if typ != "" {
		b["type"] = typ
	}
	return b
}

func TestVolumes(t *testing.T) {
	s, k := newServer(t)
	h := s.Handler()
	vol := "/ngc/apis/pod/v1alpha2/users/" + user + "/volumes"

	code, out := do(t, h, "POST", vol+"?volumeID=data", apiKey, volumeBody(10, "high-perf"))
	if code != 200 || out["id"] != volumeID || out["status"] != "Pending" || out["type"] != "high-perf" {
		t.Fatalf("create: %d %v", code, out)
	}
	if code, out = do(t, h, "POST", vol+"?volumeID=data2", apiKey, volumeBody(10, "nope")); code != 409 {
		t.Fatalf("unknown type: %d %v", code, out)
	}
	if code, out = do(t, h, "GET", vol, apiKey, nil); code != 200 || len(out["volumes"].([]any)) != 1 {
		t.Fatalf("list: %d %v", code, out)
	}
	if code, out = do(t, h, "GET", vol+"/nope", apiKey, nil); code != 404 || out["msg"] != "Volume 'nope' was not found" {
		t.Fatalf("get missing: %d %v", code, out)
	}
	if code, out = do(t, h, "POST", vol+"/data/resize", apiKey, volumeBody(20, "")); code != 200 {
		t.Fatalf("resize: %d %v", code, out)
	}
	if code, out = do(t, h, "GET", vol+"/data/usage", apiKey, nil); code != 404 {
		t.Fatalf("usage without monitor: %d %v", code, out)
	}

	job := &v1.TrainingJob{
		Name: "j1", Namespace: ns,
		Spec: v1.TrainingJobSpec{UserID: user, VolumeMounts: []v1.VolumeMount{{VolumeID: volumeID, MountPath: "/data"}}},
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
	if err := k.Get(context.Background(), client.ObjectKey{Namespace: ns, Name: naming.ImageWarmupName("img:1")}, ds); err != nil {
		t.Fatal(err)
	}
}

func TestResources(t *testing.T) {
	s, _ := newServer(t, gpuNode("n1"), gpuNode("n2"))
	h := s.Handler()

	code, out := do(t, h, "GET", "/ngc/apis/pod/v1alpha2/resources?include_details=true", apiKey, nil)
	rs, _ := out["resources"].([]any)
	if code != 200 || len(rs) != 1 {
		t.Fatalf("resources: %d %v", code, out)
	}
	usage := rs[0].(map[string]any)["usage"].(map[string]any)
	if usage["total"] != 16.0 || usage["free"] != 16.0 || len(usage["details"].([]any)) != 2 {
		t.Fatalf("usage: %v", usage)
	}
	if code, out = do(t, h, "GET", "/ngc/apis/pod/v1alpha2/resources/g1", apiKey, nil); code != 200 || len(out["resources"].([]any)) != 1 {
		t.Fatalf("resource: %d %v", code, out)
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

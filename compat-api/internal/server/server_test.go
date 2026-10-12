package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/jobs"
	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
)

const (
	apiKey = "secret-key"
	user   = "SELF-Y7bM92P1CRvbTeCDmb2jSdzF7KhEQNSPyEeVELNgHGg"
)

func newServer(t *testing.T) (*Server, client.Client) {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v1.AddToScheme(s)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: platform.ConfigMapName, Namespace: "forgeai-system"},
		Data: map[string]string{platform.ConfigKey: `
rootDomain: dev1.example.com
resources:
  - {id: g1, gpu: {count: 1}, cpu: {cores: 7}, memory: {sizeGB: 100}}
`},
	}
	k := fake.NewClientBuilder().WithScheme(s).WithObjects(cm).WithStatusSubresource(&v1.TrainingJob{}).Build()
	return &Server{
		Jobs:    &jobs.Client{K8s: k, Namespace: "pod-prod", ConfigNamespace: "forgeai-system"},
		APIKeys: []string{apiKey},
		Now:     func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) },
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, k
}

func do(t *testing.T, h http.Handler, method, path, key string, body any) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

const base = "/ngc/apis/job/v1alpha2/users/" + user + "/jobs"

func TestAuth(t *testing.T) {
	s, _ := newServer(t)
	h := s.Handler()
	if code, _ := do(t, h, "GET", base, "", nil); code != 422 {
		t.Errorf("missing key: %d", code)
	}
	if code, body := do(t, h, "GET", base, "wrong", nil); code != 401 || body["detail"] != "unauthorized" {
		t.Errorf("wrong key: %d %v", code, body)
	}
	if code, _ := do(t, h, "GET", "/healthz", "", nil); code != 200 {
		t.Errorf("healthz: %d", code)
	}
	if code, _ := do(t, h, "GET", "/ngc/apis/openapi.json", "", nil); code != 200 {
		t.Errorf("openapi: %d", code)
	}
}

func TestJobLifecycle(t *testing.T) {
	s, k := newServer(t)
	h := s.Handler()
	body := map[string]any{
		"image": "img:1", "resourceID": "g1", "cmd": "python train.py", "workers": 2,
		"envs":         map[string]any{"A": "1", "B": 2},
		"volumeMounts": []any{map[string]any{"volumeID": "data", "userID": nil, "mountPath": "/data", "subPath": nil, "readOnly": false}},
		"id":           "ignored-extra-field",
	}

	code, out := do(t, h, "POST", base+"?jobID=ackjob-001", apiKey, body)
	if code != 200 {
		t.Fatalf("create: %d %v", code, out)
	}
	if out["id"] != "ackjob-001" || out["status"] != "Pending" || out["cmd"] != "" || out["pool_id"] != "default" || out["userGroup"] != "bj8" {
		t.Fatalf("create out: %v", out)
	}

	job := &v1.TrainingJob{}
	name := naming.JobName("pod-prod", user, "ackjob-001")
	if err := k.Get(context.Background(), client.ObjectKey{Namespace: "pod-prod", Name: name}, job); err != nil {
		t.Fatal(err)
	}
	if job.Spec.Command != "python train.py" || len(job.Spec.Env) != 2 || job.Spec.Env[1].Value != "2" || job.Spec.Workers != 2 {
		t.Fatalf("spec: %+v", job.Spec)
	}

	// Simulate the operator.
	job.Status.Phase = v1.PhaseRunning
	job.Status.Active = 2
	job.Status.StartTime = &metav1.Time{Time: time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC)}
	job.Status.Workers = []v1.WorkerStatus{{Index: 0, PodName: "p0", NodeName: "n1", Phase: "Running"}, {Index: 1}}
	if err := k.Status().Update(context.Background(), job); err != nil {
		t.Fatal(err)
	}

	code, out = do(t, h, "GET", base, apiKey, nil)
	list, _ := out["jobs"].([]any)
	if code != 200 || len(list) != 1 {
		t.Fatalf("list: %d %v", code, out)
	}
	j := list[0].(map[string]any)
	if j["status"] != "Running" || j["active"] != "2" || j["completions"] != "0/2" || j["duration"] != "60m" {
		t.Fatalf("list job: %v", j)
	}
	workers := j["workers_info"].([]any)
	if len(workers) != 1 || workers[0].(map[string]any)["worker_index"] != "0" {
		t.Fatalf("workers_info: %v", workers)
	}

	if code, out = do(t, h, "GET", base+"/ackjob-001", apiKey, nil); code != 200 || out["id"] != "ackjob-001" {
		t.Fatalf("get: %d %v", code, out)
	}
	if code, out = do(t, h, "GET", base+"/nope", apiKey, nil); code != 200 || len(out) != 0 {
		t.Fatalf("get missing: %d %v", code, out)
	}

	if code, _ = do(t, h, "POST", base+"/ackjob-001/stop", apiKey, nil); code != 200 {
		t.Fatalf("stop: %d", code)
	}
	_ = k.Get(context.Background(), client.ObjectKey{Namespace: "pod-prod", Name: name}, job)
	if !job.Spec.Suspend {
		t.Fatal("not suspended")
	}
	if code, _ = do(t, h, "POST", base+"?jobID=ackjob-001&method=start", apiKey, body); code != 200 {
		t.Fatalf("start: %d", code)
	}
	_ = k.Get(context.Background(), client.ObjectKey{Namespace: "pod-prod", Name: name}, job)
	if job.Spec.Suspend {
		t.Fatal("still suspended")
	}
	if code, _ = do(t, h, "POST", base+"/ackjob-001/restart", apiKey, nil); code != 200 {
		t.Fatalf("restart: %d", code)
	}
	if code, out = do(t, h, "POST", base+"/nope/stop", apiKey, nil); code != 404 || out["msg"] != "Job 'nope' was not found" {
		t.Fatalf("stop missing: %d %v", code, out)
	}
	if code, _ = do(t, h, "DELETE", base+"/ackjob-001", apiKey, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	if code, out = do(t, h, "GET", base, apiKey, nil); code != 200 || len(out["jobs"].([]any)) != 0 {
		t.Fatalf("list after delete: %v", out)
	}
}

func TestCreateValidation(t *testing.T) {
	s, _ := newServer(t)
	h := s.Handler()
	if code, out := do(t, h, "POST", base, apiKey, map[string]any{"image": "img", "resourceID": "nope"}); code != 400 {
		t.Fatalf("unknown resource: %d %v", code, out)
	}
	if code, _ := do(t, h, "POST", base, apiKey, map[string]any{"resourceID": "g1"}); code != 422 {
		t.Fatalf("missing image: %d", code)
	}
	// Generated job ID, no prefix.
	code, out := do(t, h, "POST", "/apis/job/v1alpha2/users/"+user+"/jobs", apiKey, map[string]any{"image": "img", "resourceID": "g1"})
	if code != 200 || len(out["id"].(string)) != 6 {
		t.Fatalf("generated id: %d %v", code, out)
	}
}

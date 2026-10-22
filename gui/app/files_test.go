package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/naming"
)

// The heartbeat must write the last-active annotation on an object that
// already has annotations; the operator reads it to keep the server awake.
func TestHeartbeatUpdatesExistingAnnotation(t *testing.T) {
	stale := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	fs := &v1.FileServer{ObjectMeta: metav1.ObjectMeta{
		Name: naming.FileServerName, Namespace: testNamespace,
		Annotations: map[string]string{naming.AnnotationLastActive: stale, "keep": "me"},
	}}
	a := testApp(t, fs)
	ctx := context.Background()

	if err := a.Heartbeat(); err != nil {
		t.Fatal(err)
	}
	got := &v1.FileServer{}
	if err := a.conn.k8s.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: naming.FileServerName}, got); err != nil {
		t.Fatal(err)
	}
	fresh := got.Annotations[naming.AnnotationLastActive]
	if fresh == stale {
		t.Fatalf("heartbeat did not move last-active off %s", stale)
	}
	if got.Annotations["keep"] != "me" {
		t.Errorf("heartbeat dropped other annotations: %v", got.Annotations)
	}

	// A second call inside the throttle window leaves the object alone.
	if err := a.Heartbeat(); err != nil {
		t.Fatal(err)
	}
	if err := a.conn.k8s.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: naming.FileServerName}, got); err != nil {
		t.Fatal(err)
	}
	if got.Annotations[naming.AnnotationLastActive] != fresh {
		t.Errorf("throttled heartbeat wrote again: %s", got.Annotations[naming.AnnotationLastActive])
	}
}

// Reading the phase must not write the heartbeat: the recycle bin asks before
// trashing a volume, and a heartbeat would wake the server it is checking.
func TestFileServerPhaseDoesNotHeartbeat(t *testing.T) {
	stale := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	fs := &v1.FileServer{
		ObjectMeta: metav1.ObjectMeta{
			Name: naming.FileServerName, Namespace: testNamespace,
			Annotations: map[string]string{naming.AnnotationLastActive: stale},
		},
		Status: v1.FileServerStatus{Phase: v1.FileServerReady},
	}
	a := testApp(t, fs)
	ctx := context.Background()
	got := &v1.FileServer{}
	phase, err := a.FileServerPhase()
	if err != nil || phase != string(v1.FileServerReady) {
		t.Fatalf("phase = %q, %v; want Ready", phase, err)
	}
	if err := a.conn.k8s.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: naming.FileServerName}, got); err != nil {
		t.Fatal(err)
	}
	if got.Annotations[naming.AnnotationLastActive] != stale {
		t.Errorf("FileServerPhase refreshed last-active: %s", got.Annotations[naming.AnnotationLastActive])
	}
}

func TestFileServerPhaseWithoutServer(t *testing.T) {
	a := testApp(t)
	phase, err := a.FileServerPhase()
	if err != nil || phase != "" {
		t.Fatalf("phase = %q, %v; want empty", phase, err)
	}
}

// Files creates the server on first use, before the operator has written any
// status. The view must read that as Starting, not as an empty phase.
func TestFilesNewServerIsStarting(t *testing.T) {
	a := testApp(t)
	got, err := a.Files()
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != string(v1.FileServerStarting) || got.Address != "" {
		t.Fatalf("new server: phase %q address %q; want Starting and no address", got.Phase, got.Address)
	}
}

// The pod can be Ready before the ingress routes to it. The browser must keep
// seeing Starting, and get no address, until the server answers through the
// ingress; otherwise the frame loads a 503 and never recovers.
func TestFilesStaysStartingUntilIngressServes(t *testing.T) {
	var healthy atomic.Bool
	ingress := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__dufs__/health" && healthy.Load() {
			_, _ = w.Write([]byte(`{"status":"OK"}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ingress.Close()

	fs := &v1.FileServer{
		ObjectMeta: metav1.ObjectMeta{Name: naming.FileServerName, Namespace: testNamespace},
		Status:     v1.FileServerStatus{Phase: v1.FileServerReady, URL: ingress.URL},
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: naming.FileServerSecretName, Namespace: testNamespace},
		Data: map[string][]byte{
			corev1.BasicAuthUsernameKey: []byte("forgeai"),
			corev1.BasicAuthPasswordKey: []byte("secret"),
		},
	}
	a := testApp(t, fs, sec)

	got, err := a.Files()
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != string(v1.FileServerStarting) || got.Address != "" {
		t.Fatalf("before ingress serves: phase %q address %q; want Starting and no address", got.Phase, got.Address)
	}

	healthy.Store(true)
	got, err = a.Files()
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != string(v1.FileServerReady) || got.Address == "" {
		t.Fatalf("after ingress serves: phase %q address %q; want Ready with address", got.Phase, got.Address)
	}
}

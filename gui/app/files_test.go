package app

import (
	"context"
	"testing"
	"time"

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

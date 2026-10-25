package app

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/sandbox"
)

// sandboxObject is a sandbox of the test namespace, created at created, as the
// API server would hold it.
func sandboxObject(t *testing.T, name string, created time.Time, mutate func(*sandbox.Sandbox)) *unstructured.Unstructured {
	t.Helper()
	sb := &sandbox.Sandbox{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace}}
	if mutate != nil {
		mutate(sb)
	}
	u, err := sandbox.ToUnstructured(sb)
	if err != nil {
		t.Fatal(err)
	}
	u.SetCreationTimestamp(metav1.NewTime(created))
	return u
}

// platformConfigMap holds the platform config the sandbox reads its options from.
func platformConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: platform.ConfigMapName, Namespace: testNamespace},
		Data: map[string]string{platform.ConfigKey: `
sandbox:
  defaultImage: ghcr.io/example/agent:1
  runtimeClasses: [gvisor]
  idleTimeout: 30m
`},
	}
}

func fetchSandbox(t *testing.T, a *App, name string) *sandbox.Sandbox {
	t.Helper()
	u, err := getSandbox(context.Background(), a.conn, name)
	if err != nil {
		t.Fatal(err)
	}
	sb := &sandbox.Sandbox{}
	if err := sandbox.FromUnstructured(u.Object, sb); err != nil {
		t.Fatal(err)
	}
	return sb
}

func TestStopThenStartSandbox(t *testing.T) {
	shutdown := metav1.NewTime(time.Now().Add(-time.Hour))
	a := testApp(t, sandboxObject(t, "box", time.Now(), func(sb *sandbox.Sandbox) {
		sb.Annotations = map[string]string{naming.AnnotationIdleSince: "2026-10-01T00:00:00Z"}
		sb.Spec.ShutdownTime = &shutdown
	}))

	if _, err := a.StopSandbox("box"); err != nil {
		t.Fatal(err)
	}
	sb := fetchSandbox(t, a, "box")
	if sb.Spec.OperatingMode != sandbox.OperatingSuspended {
		t.Errorf("operating mode %q after stop, want suspended", sb.Spec.OperatingMode)
	}
	if sb.Annotations[naming.AnnotationStopped] != naming.LiteralTrue {
		t.Errorf("stopped annotation %q after stop", sb.Annotations[naming.AnnotationStopped])
	}

	if _, err := a.StartSandbox("box"); err != nil {
		t.Fatal(err)
	}
	sb = fetchSandbox(t, a, "box")
	if sb.Spec.OperatingMode != sandbox.OperatingRunning {
		t.Errorf("operating mode %q after start, want running", sb.Spec.OperatingMode)
	}
	if _, ok := sb.Annotations[naming.AnnotationStopped]; ok {
		t.Error("stopped annotation survived start")
	}
	if _, ok := sb.Annotations[naming.AnnotationIdleSince]; ok {
		t.Error("idle-since survived start")
	}
	// A shutdown time already passed would expire the sandbox at once.
	if sb.Spec.ShutdownTime != nil && !sb.Spec.ShutdownTime.IsZero() {
		t.Errorf("elapsed shutdown time %v kept on start", sb.Spec.ShutdownTime)
	}
	last, err := time.Parse(time.RFC3339, sb.Annotations[naming.AnnotationLastActive])
	if err != nil {
		t.Fatalf("last-active annotation: %v", err)
	}
	if time.Since(last) > time.Minute {
		t.Errorf("last-active %s, want about now", last)
	}
}

func TestKeepSandboxAwakeResetsOnlyTheHeartbeat(t *testing.T) {
	old := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	a := testApp(t, sandboxObject(t, "box", time.Now(), func(sb *sandbox.Sandbox) {
		sb.Annotations = map[string]string{
			naming.AnnotationLastActive: old,
			naming.AnnotationIdleSince:  old,
		}
	}))
	if err := a.KeepSandboxAwake("box"); err != nil {
		t.Fatal(err)
	}
	sb := fetchSandbox(t, a, "box")
	if sb.Annotations[naming.AnnotationLastActive] == old {
		t.Error("heartbeat not renewed")
	}
	if sb.Annotations[naming.AnnotationIdleSince] != old {
		t.Errorf("idle-since changed to %q; only the heartbeat is renewed", sb.Annotations[naming.AnnotationIdleSince])
	}
}

func TestListSandboxesNewestFirst(t *testing.T) {
	now := time.Now()
	a := testApp(t,
		sandboxObject(t, "old", now.Add(-time.Hour), nil),
		sandboxObject(t, "new", now, nil),
		sandboxObject(t, "mid", now.Add(-time.Minute), nil),
	)
	list, err := a.ListSandboxes()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(list))
	for _, sb := range list {
		names = append(names, sb.Name)
	}
	if strings.Join(names, " ") != "new mid old" {
		t.Errorf("order %v, want newest first", names)
	}
}

func TestListSandboxesEmptyNamespace(t *testing.T) {
	list, err := testApp(t).ListSandboxes()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("listed %d sandboxes in an empty namespace", len(list))
	}
}

func TestGetSandboxMissing(t *testing.T) {
	_, err := testApp(t).GetSandbox("nope")
	if !apierrors.IsNotFound(err) {
		t.Errorf("got %v, want NotFound", err)
	}
}

func TestGetSandboxReturnsItsView(t *testing.T) {
	a := testApp(t, sandboxObject(t, "box", time.Now(), func(sb *sandbox.Sandbox) {
		sb.Spec.PodTemplate.Spec.Containers = []corev1.Container{{
			Name:  sandbox.ContainerName,
			Image: "ghcr.io/example/agent:1",
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")},
			},
		}}
	}))
	sb, err := a.GetSandbox("box")
	if err != nil {
		t.Fatal(err)
	}
	if sb.Image != "ghcr.io/example/agent:1" {
		t.Errorf("image %q", sb.Image)
	}
	// With only limits set, the view still reports what the sandbox requests.
	if sb.CPU != "2" || sb.Memory != "4Gi" {
		t.Errorf("cpu %q memory %q, want 2 and 4Gi from the limits", sb.CPU, sb.Memory)
	}
}

func TestDeleteSandbox(t *testing.T) {
	a := testApp(t, sandboxObject(t, "box", time.Now(), nil))
	if err := a.DeleteSandbox("box"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetSandbox("box"); !apierrors.IsNotFound(err) {
		t.Errorf("sandbox still there after delete: %v", err)
	}
	// A repeated delete finds nothing and succeeds.
	if err := a.DeleteSandbox("box"); err != nil {
		t.Errorf("delete of a missing sandbox: %v", err)
	}
}

func TestStopMissingSandboxIsNotFound(t *testing.T) {
	if _, err := testApp(t).StopSandbox("nope"); !apierrors.IsNotFound(err) {
		t.Errorf("stop of a missing sandbox = %v, want NotFound", err)
	}
}

func TestSandboxLogsNeedAPod(t *testing.T) {
	a := testApp(t, sandboxObject(t, "box", time.Now(), func(sb *sandbox.Sandbox) {
		sb.Spec.OperatingMode = sandbox.OperatingSuspended
	}))
	_, err := a.SandboxLogs("box", 0)
	if err == nil || !strings.Contains(err.Error(), "has no pod") {
		t.Errorf("logs of a stopped sandbox = %v, want a no-pod error", err)
	}
}

func TestSandboxOptionsFromPlatform(t *testing.T) {
	a := testApp(t, platformConfigMap())
	o, err := a.SandboxOptions()
	if err != nil {
		t.Fatal(err)
	}
	if o.DefaultImage != "ghcr.io/example/agent:1" {
		t.Errorf("default image %q", o.DefaultImage)
	}
	// The first runtime class is preselected when the platform names no default.
	if o.DefaultRuntimeClass != "gvisor" || len(o.RuntimeClasses) != 1 || o.RuntimeClasses[0] != "gvisor" {
		t.Errorf("runtime classes %v default %q", o.RuntimeClasses, o.DefaultRuntimeClass)
	}
	if o.IdleTimeout != (30 * time.Minute).String() {
		t.Errorf("idle timeout %q", o.IdleTimeout)
	}
}

func TestDeleteSandboxTemplateOnlyOwnTemplates(t *testing.T) {
	managed := &unstructured.Unstructured{}
	managed.SetGroupVersionKind(sandbox.SandboxTemplateGVK)
	managed.SetNamespace(testNamespace)
	managed.SetName("ours")
	managed.SetLabels(map[string]string{naming.LabelManagedBy: naming.ManagedBy})

	foreign := managed.DeepCopy()
	foreign.SetName("theirs")
	foreign.SetLabels(nil)

	a := testApp(t, managed, foreign)
	if err := a.DeleteSandboxTemplate("theirs"); err == nil || !strings.Contains(err.Error(), "not created by forgeai") {
		t.Errorf("delete of a foreign template = %v, want a refusal", err)
	}
	if err := a.DeleteSandboxTemplate("ours"); err != nil {
		t.Fatal(err)
	}
	gone := &unstructured.Unstructured{}
	gone.SetGroupVersionKind(sandbox.SandboxTemplateGVK)
	if err := a.conn.k8s.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "ours"}, gone); !apierrors.IsNotFound(err) {
		t.Errorf("managed template still there: %v", err)
	}
	if err := a.DeleteSandboxTemplate("ours"); err != nil {
		t.Errorf("delete of a missing template: %v", err)
	}
}

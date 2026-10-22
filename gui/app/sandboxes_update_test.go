package app

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/sandbox"
)

// TestStopSandboxKeepsUnmodelledFields checks that a stop writes back only the
// operating mode and annotations, so spec fields the typed model does not carry
// and unrelated annotations survive.
func TestStopSandboxKeepsUnmodelledFields(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": sandbox.Group + "/" + sandbox.Version,
		"kind":       "Sandbox",
		"metadata": map[string]any{
			"name": "box", "namespace": testNamespace,
			"annotations": map[string]any{"keep": "me"},
		},
		"spec": map[string]any{
			"operatingMode":   "Running",
			"someFutureField": map[string]any{"a": "b"},
		},
	}}
	a := testApp(t, u)
	if _, err := a.StopSandbox("box"); err != nil {
		t.Fatal(err)
	}
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(sandbox.SandboxGVK)
	if err := a.conn.k8s.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "box"}, got); err != nil {
		t.Fatal(err)
	}
	if mode, _, _ := unstructured.NestedString(got.Object, "spec", "operatingMode"); mode != string(sandbox.OperatingSuspended) {
		t.Fatalf("operatingMode = %q, want Suspended", mode)
	}
	if v, ok, _ := unstructured.NestedFieldNoCopy(got.Object, "spec", "someFutureField", "a"); !ok || v != "b" {
		t.Fatalf("spec.someFutureField.a = %v, want b", v)
	}
	if got.GetAnnotations()["keep"] != "me" || got.GetAnnotations()[naming.AnnotationStopped] != naming.LiteralTrue {
		t.Fatalf("annotations = %v, want keep and stopped", got.GetAnnotations())
	}
}

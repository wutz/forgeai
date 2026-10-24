package app

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// StatefulSet and DaemonSet selectors are immutable too, so checkSelectors covers them as it does Deployments.
func TestCheckSelectorsWorkloadKinds(t *testing.T) {
	s := scheme()
	bundle := func(kind, app string) *depBundle {
		obj := map[string]any{
			"apiVersion": "apps/v1",
			"kind":       kind,
			"metadata":   map[string]any{"namespace": "web-system", "name": "web"},
			"spec":       map[string]any{"selector": map[string]any{"matchLabels": map[string]any{"app": app}}},
		}
		return &depBundle{Name: "web", objects: []*unstructured.Unstructured{{Object: obj}}}
	}
	live := func(kind, app string) client.Client {
		o := &unstructured.Unstructured{}
		o.SetGroupVersionKind(appsv1.SchemeGroupVersion.WithKind(kind))
		o.SetNamespace("web-system")
		o.SetName("web")
		_ = unstructured.SetNestedMap(o.Object, map[string]any{"matchLabels": map[string]any{"app": app}}, "spec", "selector")
		return fake.NewClientBuilder().WithScheme(s).WithObjects(o).Build()
	}
	for _, kind := range []string{"StatefulSet", "DaemonSet"} {
		t.Run(kind+" with the same selector", func(t *testing.T) {
			if err := checkSelectors(context.Background(), live(kind, "web"), bundle(kind, "web")); err != nil {
				t.Fatalf("checkSelectors = %v; want no error", err)
			}
		})
		t.Run(kind+" with another selector", func(t *testing.T) {
			err := checkSelectors(context.Background(), live(kind, "db"), bundle(kind, "web"))
			if err == nil || !strings.Contains(err.Error(), "delete the "+kind+" before installing web") {
				t.Fatalf("checkSelectors = %v; want a refusal naming the %s", err, kind)
			}
		})
	}
}

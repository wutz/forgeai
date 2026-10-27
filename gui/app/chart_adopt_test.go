package app

import (
	"context"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestAdoptCRDs checks that the operator chart takes over the InstanceType CRD
// from the forgeai release that shipped it, and leaves CRDs of other releases alone.
func TestAdoptCRDs(t *testing.T) {
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := apiextensionsv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	crd := func(name, release string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(crdGVK)
		u.SetName(name)
		u.SetAnnotations(map[string]string{helmReleaseNameKey: release})
		u.SetLabels(map[string]string{"app.kubernetes.io/name": "forgeai"})
		return u
	}
	k8s := fake.NewClientBuilder().WithScheme(s).WithObjects(
		crd("instancetypes.forgeai.wutz.dev", forgeaiRelease),
		crd("projects.forgeai.wutz.dev", "other"),
	).Build()

	if err := adoptCRDs(context.Background(), k8s, instanceTypeOperatorChart, "forgeai-system"); err != nil {
		t.Fatalf("adoptCRDs: %v", err)
	}

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(crdGVK)
	if err := k8s.Get(context.Background(), client.ObjectKey{Name: "instancetypes.forgeai.wutz.dev"}, got); err != nil {
		t.Fatal(err)
	}
	if got.GetAnnotations()[helmReleaseNameKey] != instanceTypeRelease {
		t.Errorf("release-name = %q, want %q", got.GetAnnotations()[helmReleaseNameKey], instanceTypeRelease)
	}
	if got.GetAnnotations()[helmReleaseNamespaceKey] != "forgeai-system" {
		t.Errorf("release-namespace = %q, want forgeai-system", got.GetAnnotations()[helmReleaseNamespaceKey])
	}
	if got.GetLabels()[helmManagedByKey] != "Helm" {
		t.Errorf("managed-by = %q, want Helm", got.GetLabels()[helmManagedByKey])
	}

	other := &unstructured.Unstructured{}
	other.SetGroupVersionKind(crdGVK)
	if err := k8s.Get(context.Background(), client.ObjectKey{Name: "projects.forgeai.wutz.dev"}, other); err != nil {
		t.Fatal(err)
	}
	if other.GetAnnotations()[helmReleaseNameKey] != "other" || other.GetLabels()[helmManagedByKey] != "" {
		t.Errorf("CRD of another release was changed: annotations %v labels %v",
			other.GetAnnotations(), other.GetLabels())
	}
}

// TestDependencyCatalogOrder checks that the InstanceType operator is installed
// before the forgeai operator, which reads InstanceTypes.
func TestDependencyCatalogOrder(t *testing.T) {
	pos := func(name string) int {
		for i, d := range depCatalog {
			if d.name == name {
				return i
			}
		}
		t.Fatalf("catalog lacks %s", name)
		return -1
	}
	if pos("instancetype-operator") > pos("forgeai") {
		t.Errorf("instancetype-operator is listed after forgeai")
	}
}

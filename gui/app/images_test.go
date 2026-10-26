package app

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/wutz/forgeai/pkg/platform"
)

// TestRegistryForViewOnlyMember checks that a member who cannot read the robot
// Secret still gets the push address, without credentials, and no image list.
func TestRegistryForViewOnlyMember(t *testing.T) {
	cfg := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: platform.ConfigMapName, Namespace: testNamespace},
		Data:       map[string]string{platform.ConfigKey: "registry:\n  domain: cr.example.com:7443\n"},
	}
	k8s := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(cfg).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*corev1.Secret); ok {
					return apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, key.Name, nil)
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).Build()
	a := &App{ctx: context.Background(), conn: &Connection{Info: ConnInfo{Namespace: testNamespace}, k8s: k8s}}

	reg, err := a.Registry()
	if err != nil {
		t.Fatal(err)
	}
	if !reg.Enabled || reg.Credentials || reg.Username != "" || reg.Password != "" {
		t.Fatalf("registry = %+v, want enabled without credentials", reg)
	}
	if want := "cr.example.com:7443/" + testNamespace; reg.URL != want {
		t.Fatalf("URL = %q, want %q", reg.URL, want)
	}
	repos, err := a.ListImages()
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 0 {
		t.Fatalf("ListImages = %v, want none without credentials", repos)
	}
}

// TestRegistryNotConfigured checks that a project without a robot Secret, on a
// platform with no registry, reports no registry and lists no images.
func TestRegistryNotConfigured(t *testing.T) {
	cfg := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: platform.ConfigMapName, Namespace: testNamespace},
		Data:       map[string]string{platform.ConfigKey: "namespacePrefix: forge-\n"},
	}
	k8s := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(cfg).Build()
	a := &App{ctx: context.Background(), conn: &Connection{Info: ConnInfo{Namespace: testNamespace}, k8s: k8s}}

	reg, err := a.Registry()
	if err != nil {
		t.Fatal(err)
	}
	if reg.Enabled || reg.Credentials || reg.URL != "" {
		t.Fatalf("registry = %+v, want not enabled and no address", reg)
	}
	repos, err := a.ListImages()
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 0 {
		t.Fatalf("ListImages = %v, want none without a registry", repos)
	}
}

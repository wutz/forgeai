package app

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
)

// TestConfigExposesNamespacePrefix checks that the GUI receives the prefix the
// project controller uses, defaulted when the platform config leaves it unset.
func TestConfigExposesNamespacePrefix(t *testing.T) {
	tests := []struct {
		name, data, want string
	}{
		{"default", "registry:\n  domain: cr.example.com:7443\n", "forge-"},
		{"custom", "namespacePrefix: team-\n", "team-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: platform.ConfigMapName, Namespace: testNamespace},
				Data:       map[string]string{platform.ConfigKey: tt.data},
			}
			a := testApp(t, cm)

			cfg, err := a.Config()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.NamespacePrefix != tt.want {
				t.Fatalf("NamespacePrefix = %q, want %q", cfg.NamespacePrefix, tt.want)
			}
		})
	}
}

// TestResolveProject checks which project a connection opens, for a user who
// can read the project namespaces and for one with no project role at all.
func TestResolveProject(t *testing.T) {
	project := func(name, proj string) *corev1.Namespace {
		return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
			naming.LabelManagedBy: naming.ManagedBy, naming.LabelProject: proj,
		}}}
	}
	plain := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, "", errors.New("no role"))
	// noRole makes every namespace read and list forbidden, as it is for a user
	// bound to no project.
	noRole := interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.Namespace); ok {
				return forbidden
			}
			return c.Get(ctx, key, obj, opts...)
		},
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*corev1.NamespaceList); ok {
				return forbidden
			}
			return c.List(ctx, list, opts...)
		},
	}
	tests := []struct {
		name, namespace, wantNS, wantProject string
		objs                                 []client.Object
		noRole, explicit, wantErr            bool
	}{
		{name: "default is a project", namespace: "forge-a", wantNS: "forge-a", wantProject: "a",
			objs: []client.Object{project("forge-a", "a")}},
		{name: "first visible project when the default is not one", namespace: "default", wantNS: "forge-b", wantProject: "b",
			objs: []client.Object{plain, project("forge-b", "b")}},
		{name: "no project role opens nothing", namespace: "default", noRole: true,
			objs: []client.Object{project("forge-a", "a")}},
		{name: "no project visible opens nothing", namespace: "default",
			objs: []client.Object{plain}},
		{name: "explicit namespace that is not a project is an error", namespace: "default", explicit: true, wantErr: true,
			objs: []client.Object{plain}},
		{name: "explicit namespace without access is an error", namespace: "forge-a", explicit: true, noRole: true, wantErr: true,
			objs: []client.Object{project("forge-a", "a")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(tt.objs...)
			if tt.noRole {
				b = b.WithInterceptorFuncs(noRole)
			}
			ns, proj, err := resolveProject(context.Background(), b.Build(), tt.namespace, tt.explicit)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if ns != tt.wantNS || proj != tt.wantProject {
				t.Errorf("resolved %q/%q, want %q/%q", ns, proj, tt.wantNS, tt.wantProject)
			}
		})
	}
}

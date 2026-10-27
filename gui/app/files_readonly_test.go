package app

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	fsv1 "github.com/wutz/fileserver-operator/api/v1alpha1"
)

// viewerApp is an App whose client refuses the calls a view member has no
// role for. The fake client does not enforce RBAC, so the refusals are injected.
func viewerApp(funcs interceptor.Funcs, objs ...client.Object) *App {
	k8s := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(objs...).WithInterceptorFuncs(funcs).Build()
	return &App{ctx: context.Background(), conn: &Connection{Info: ConnInfo{Namespace: testNamespace}, k8s: k8s}}
}

func forbiddenErr(resource, name string) error {
	return apierrors.NewForbidden(schema.GroupResource{Resource: resource}, name, errors.New("denied"))
}

// A view member reads the file server but cannot wake it: the heartbeat patch
// is refused, and Files reports the state read-only instead of failing.
func TestFilesReadOnlyWhenHeartbeatIsRefused(t *testing.T) {
	stale := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	fs := &fsv1.FileServer{ObjectMeta: metav1.ObjectMeta{
		Name: fsv1.DefaultName, Namespace: testNamespace,
		Annotations: map[string]string{fsv1.AnnotationLastActive: stale},
	}}
	a := viewerApp(interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
			return forbiddenErr("fileservers", obj.GetName())
		},
	}, fs)

	got, err := a.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if !got.ReadOnly {
		t.Errorf("Files not read-only for a refused heartbeat: %+v", got)
	}
	if got.Address != "" {
		t.Errorf("read-only Files published an address: %q", got.Address)
	}
}

// A view member cannot create the file server; with none in the project it
// reports read-only rather than the create error.
func TestFilesReadOnlyWhenServerCannotBeCreated(t *testing.T) {
	a := viewerApp(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			return forbiddenErr("fileservers", obj.GetName())
		},
	})

	got, err := a.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if !got.ReadOnly {
		t.Errorf("Files not read-only when the server cannot be created: %+v", got)
	}
}

// A view member sees a ready server but gets no address: the Secret with the
// basic-auth password is refused, so the page shows the state only.
func TestFilesReadOnlyWhenSecretIsRefused(t *testing.T) {
	fresh := time.Now().UTC().Format(time.RFC3339)
	fs := &fsv1.FileServer{
		ObjectMeta: metav1.ObjectMeta{
			Name: fsv1.DefaultName, Namespace: testNamespace,
			Annotations: map[string]string{fsv1.AnnotationLastActive: fresh},
		},
		Status: fsv1.FileServerStatus{Phase: fsv1.FileServerReady, URL: "https://files.example.com"},
	}
	a := viewerApp(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.Secret); ok {
				return forbiddenErr("secrets", key.Name)
			}
			return c.Get(ctx, key, obj, opts...)
		},
	}, fs)

	got, err := a.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if !got.ReadOnly || got.Address != "" {
		t.Errorf("Files with a refused Secret = %+v, want read-only without an address", got)
	}
	if got.Phase != string(fsv1.FileServerReady) {
		t.Errorf("Phase = %q, want Ready", got.Phase)
	}
}

package app

import (
	"context"
	"testing"

	authv1 "k8s.io/api/authorization/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/sandbox"
)

// reviewGrants answers each create review from grants, a set of
// "group/resource" pairs the user may create in testNamespace. Other reviews
// pass through to the fake client.
func reviewGrants(grants ...string) interceptor.Funcs {
	allow := map[string]bool{}
	for _, g := range grants {
		allow[g] = true
	}
	return interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			sar, ok := obj.(*authv1.SelfSubjectAccessReview)
			if !ok {
				return c.Create(ctx, obj, opts...)
			}
			ra := sar.Spec.ResourceAttributes
			sar.Status.Allowed = ra != nil && ra.Verb == "create" && ra.Namespace == testNamespace &&
				allow[ra.Group+"/"+ra.Resource]
			return nil
		},
	}
}

// A project editor may create every kind the GUI offers; the reviews name the
// resources that the project-edit role grants.
func TestCreateAccessForEditor(t *testing.T) {
	a := viewerApp(reviewGrants(
		kf.TrainJobGVK.Group+"/trainjobs",
		kf.TrainingRuntimeGVK.Group+"/trainingruntimes",
		"/persistentvolumeclaims",
		"forgeai.wutz.dev/inferenceservices",
		sandbox.Group+"/sandboxes",
		sandbox.ExtensionsGroup+"/sandboxtemplates",
	))

	got := createAccess(context.Background(), a.conn.k8s, testNamespace)
	want := CreateAccess{Jobs: true, JobTemplates: true, Volumes: true, Inference: true, Sandboxes: true, SandboxTemplates: true}
	if got != want {
		t.Errorf("createAccess for an editor = %+v, want %+v", got, want)
	}
}

// A view member has no create grant, so no create button is offered.
func TestCreateAccessForViewer(t *testing.T) {
	a := viewerApp(reviewGrants())

	got := createAccess(context.Background(), a.conn.k8s, testNamespace)
	if got != (CreateAccess{}) {
		t.Errorf("createAccess for a viewer = %+v, want none", got)
	}
}

// A grant in another namespace does not allow a create in the project: the
// reviews name the project namespace.
func TestCreateAccessIsScopedToProjectNamespace(t *testing.T) {
	a := viewerApp(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if sar, ok := obj.(*authv1.SelfSubjectAccessReview); ok {
				sar.Status.Allowed = sar.Spec.ResourceAttributes.Namespace == "elsewhere"
				return nil
			}
			return c.Create(ctx, obj, opts...)
		},
	})

	if got := createAccess(context.Background(), a.conn.k8s, testNamespace); got != (CreateAccess{}) {
		t.Errorf("createAccess allowed in another namespace = %+v, want none", got)
	}
}

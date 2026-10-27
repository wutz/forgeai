package app

import (
	"context"
	"testing"

	authv1 "k8s.io/api/authorization/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/sandbox"
)

// reviewVerbs answers each review from grants, a set of "group/resource/verb"
// triples the user may do in testNamespace. Other reviews pass through to the
// fake client.
func reviewVerbs(grants ...string) interceptor.Funcs {
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
			sar.Status.Allowed = ra != nil && ra.Namespace == testNamespace &&
				allow[ra.Group+"/"+ra.Resource+"/"+ra.Verb]
			return nil
		},
	}
}

// A project editor may change and remove every kind the GUI offers, so each
// row action is shown.
func TestChangeAccessForEditor(t *testing.T) {
	var grants []string
	for _, r := range []struct{ group, resource string }{
		{kf.TrainJobGVK.Group, "trainjobs"},
		{kf.TrainingRuntimeGVK.Group, "trainingruntimes"},
		{"", "persistentvolumeclaims"},
		{v1.GroupVersion.Group, "inferenceservices"},
		{sandbox.Group, "sandboxes"},
		{sandbox.ExtensionsGroup, "sandboxtemplates"},
	} {
		grants = append(grants, r.group+"/"+r.resource+"/update", r.group+"/"+r.resource+"/delete")
	}
	a := viewerApp(reviewVerbs(grants...))

	got := changeAccess(context.Background(), a.conn.k8s, testNamespace)
	want := ChangeAccess{Jobs: true, JobTemplates: true, Volumes: true, Inference: true, Sandboxes: true, SandboxTemplates: true}
	if got != want {
		t.Errorf("changeAccess for an editor = %+v, want %+v", got, want)
	}
}

// Changing an object needs both update and delete: a member who can do only
// one of them is not offered the row actions for that kind.
func TestChangeAccessNeedsUpdateAndDelete(t *testing.T) {
	a := viewerApp(reviewVerbs(
		sandbox.Group+"/sandboxes/update",
		"/persistentvolumeclaims/delete",
		sandbox.Group+"/sandboxes/delete",
	))

	got := changeAccess(context.Background(), a.conn.k8s, testNamespace)
	want := ChangeAccess{Sandboxes: true}
	if got != want {
		t.Errorf("changeAccess = %+v, want only sandboxes (update and delete)", got)
	}
}

// Create grants do not allow a change, and a view member has none of either,
// so no row action is offered.
func TestChangeAccessForViewer(t *testing.T) {
	a := viewerApp(reviewVerbs(
		kf.TrainJobGVK.Group+"/trainjobs/create",
		"/persistentvolumeclaims/create",
	))

	if got := changeAccess(context.Background(), a.conn.k8s, testNamespace); got != (ChangeAccess{}) {
		t.Errorf("changeAccess for a viewer = %+v, want none", got)
	}
}

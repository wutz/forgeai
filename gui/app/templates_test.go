package app

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/sandbox"
	"github.com/wutz/forgeai/pkg/training"
)

const testNamespace = "forge-team"

// testApp returns an App connected to a fake cluster holding objs.
func testApp(t *testing.T, objs ...client.Object) *App {
	t.Helper()
	k8s := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(objs...).Build()
	return &App{ctx: context.Background(), conn: &Connection{Info: ConnInfo{Namespace: testNamespace}, k8s: k8s}}
}

func testTemplateForm(name, image string) training.Form {
	return training.Form{
		Name: name, Image: image, InstanceType: "cpu.small", Workers: 1, RetryOn: training.RetryInfra,
	}
}

func TestUpdateTemplateReplacesForm(t *testing.T) {
	f := testTemplateForm("torch", "img:1")
	rt, err := training.Template(&f, testNamespace)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := runtimeObject(rt)
	if err != nil {
		t.Fatal(err)
	}
	a := testApp(t, obj)

	edited := testTemplateForm("ignored", "img:2")
	edited.Command = "python train.py"
	got, err := a.UpdateTemplate("torch", templateVersion(t, a, "torch"), edited)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "torch" || got.Form.Image != "img:2" || got.Form.Command != "python train.py" {
		t.Fatalf("UpdateTemplate returned %+v", got)
	}

	stored := &unstructured.Unstructured{}
	stored.SetGroupVersionKind(kf.TrainingRuntimeGVK)
	if err := a.conn.k8s.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "torch"}, stored); err != nil {
		t.Fatal(err)
	}
	stored2 := &kf.TrainingRuntime{}
	if err := kf.FromUnstructured(stored.Object, stored2); err != nil {
		t.Fatal(err)
	}
	form, err := training.TemplateForm(stored2)
	if err != nil {
		t.Fatal(err)
	}
	if form.Image != "img:2" || form.Command != "python train.py" {
		t.Errorf("stored template = %+v, want the edited form", form)
	}
}

func TestUpdateTemplateRefusesForeignRuntime(t *testing.T) {
	foreign := &kf.TrainingRuntime{}
	foreign.SetGroupVersionKind(kf.TrainingRuntimeGVK)
	foreign.Name, foreign.Namespace = "kubectl-made", testNamespace
	u, err := runtimeObject(foreign)
	if err != nil {
		t.Fatal(err)
	}
	a := testApp(t, u)
	_, err = a.UpdateTemplate("kubectl-made", "1", testTemplateForm("kubectl-made", "img:2"))
	if err == nil || !strings.Contains(err.Error(), "not created by forgeai") {
		t.Fatalf("UpdateTemplate of a foreign runtime: err = %v", err)
	}
}

func TestUpdateSandboxTemplateReplacesForm(t *testing.T) {
	cfg := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: platform.ConfigMapName}}
	in := sandbox.TemplateForm{Name: "agent", Image: "agent:1", Command: "python agent.py"}
	u, err := sandbox.RenderTemplate(&in, sandbox.Context{Namespace: testNamespace})
	if err != nil {
		t.Fatal(err)
	}
	a := testApp(t, cfg, u)

	got, err := a.UpdateSandboxTemplate("agent", sandboxTemplateVersion(t, a, "agent"), sandbox.TemplateForm{Image: "agent:2", Command: "python agent.py --once"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "agent" || got.Form.Image != "agent:2" || got.Form.Command != "python agent.py --once" {
		t.Fatalf("UpdateSandboxTemplate returned %+v", got)
	}
}

// A created template reports the version the apiserver stored, so an update
// made right after the create is accepted.
func TestCreateTemplateReturnsStoredVersion(t *testing.T) {
	a := testApp(t)
	created, err := a.CreateTemplate(testTemplateForm("torch", "img:1"))
	if err != nil {
		t.Fatal(err)
	}
	if created.ResourceVersion == "" {
		t.Fatal("CreateTemplate returned no resource version")
	}
	if created.ResourceVersion != templateVersion(t, a, "torch") {
		t.Fatalf("CreateTemplate version = %q, the listed version is %q", created.ResourceVersion, templateVersion(t, a, "torch"))
	}
	if _, err := a.UpdateTemplate("torch", created.ResourceVersion, testTemplateForm("torch", "img:2")); err != nil {
		t.Fatalf("update with the created version: %v", err)
	}
}

// A created sandbox template reports a version an update can use.
func TestCreateSandboxTemplateReturnsStoredVersion(t *testing.T) {
	cfg := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: platform.ConfigMapName}}
	a := testApp(t, cfg)
	created, err := a.CreateSandboxTemplate(sandbox.TemplateForm{Name: "agent", Image: "agent:1", Command: "python agent.py"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ResourceVersion == "" || created.ResourceVersion != sandboxTemplateVersion(t, a, "agent") {
		t.Fatalf("CreateSandboxTemplate version = %q, the listed version is %q", created.ResourceVersion, sandboxTemplateVersion(t, a, "agent"))
	}
	if _, err := a.UpdateSandboxTemplate("agent", created.ResourceVersion, sandbox.TemplateForm{Image: "agent:2"}); err != nil {
		t.Fatalf("update with the created version: %v", err)
	}
}

// templateVersion is the version a job template was listed at, which the GUI
// sends back with its update.
func templateVersion(t *testing.T, a *App, name string) string {
	t.Helper()
	list, err := a.ListTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range list {
		if tpl.Name == name {
			return tpl.ResourceVersion
		}
	}
	t.Fatalf("template %s is not listed", name)
	return ""
}

func sandboxTemplateVersion(t *testing.T, a *App, name string) string {
	t.Helper()
	list, err := a.SandboxTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range list {
		if tpl.Name == name {
			return tpl.ResourceVersion
		}
	}
	t.Fatalf("sandbox template %s is not listed", name)
	return ""
}

// An update made from a version that a later change has already replaced
// conflicts instead of overwriting that change.
func TestUpdateTemplateRejectsStaleVersion(t *testing.T) {
	f := testTemplateForm("torch", "img:1")
	rt, err := training.Template(&f, testNamespace)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := runtimeObject(rt)
	if err != nil {
		t.Fatal(err)
	}
	a := testApp(t, obj)
	loaded := templateVersion(t, a, "torch")

	if _, err := a.UpdateTemplate("torch", loaded, testTemplateForm("torch", "img:2")); err != nil {
		t.Fatalf("first update: %v", err)
	}
	if _, err := a.UpdateTemplate("torch", loaded, testTemplateForm("torch", "img:3")); !apierrors.IsConflict(err) {
		t.Fatalf("stale update: err = %v, want a conflict", err)
	}
	if _, err := a.UpdateTemplate("torch", "", testTemplateForm("torch", "img:3")); err == nil {
		t.Fatal("update without a loaded version succeeded")
	}
}

func TestUpdateSandboxTemplateRejectsStaleVersion(t *testing.T) {
	cfg := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: platform.ConfigMapName}}
	in := sandbox.TemplateForm{Name: "agent", Image: "agent:1", Command: "python agent.py"}
	u, err := sandbox.RenderTemplate(&in, sandbox.Context{Namespace: testNamespace})
	if err != nil {
		t.Fatal(err)
	}
	a := testApp(t, cfg, u)
	loaded := sandboxTemplateVersion(t, a, "agent")

	if _, err := a.UpdateSandboxTemplate("agent", loaded, sandbox.TemplateForm{Image: "agent:2"}); err != nil {
		t.Fatalf("first update: %v", err)
	}
	if _, err := a.UpdateSandboxTemplate("agent", loaded, sandbox.TemplateForm{Image: "agent:3"}); !apierrors.IsConflict(err) {
		t.Fatalf("stale update: err = %v, want a conflict", err)
	}
}

// A sandbox template the app cannot read is listed with its error, so it can
// still be deleted, instead of vanishing from the list.
func TestListSandboxTemplatesShowsUnreadable(t *testing.T) {
	broken := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "broken", "namespace": testNamespace},
		"spec":     map[string]any{"podTemplate": "not-an-object"},
	}}
	broken.SetGroupVersionKind(sandbox.SandboxTemplateGVK)
	a := testApp(t, broken)

	list, err := a.SandboxTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "broken" || list[0].Error == "" {
		t.Fatalf("SandboxTemplates = %+v, want the unreadable template with its error", list)
	}
}

// A template whose spec annotation does not parse is still listed by name, so
// the user can delete or rewrite it; the other templates are listed too.
func TestListTemplatesKeepsMalformedSpec(t *testing.T) {
	broken := testTemplateForm("broken-spec", "img:1")
	brt, err := training.Template(&broken, testNamespace)
	if err != nil {
		t.Fatal(err)
	}
	brt.Annotations[training.AnnotationTemplateSpec] = "{not json"
	brokenObj, err := runtimeObject(brt)
	if err != nil {
		t.Fatal(err)
	}
	good := testTemplateForm("good-spec", "img:2")
	grt, err := training.Template(&good, testNamespace)
	if err != nil {
		t.Fatal(err)
	}
	goodObj, err := runtimeObject(grt)
	if err != nil {
		t.Fatal(err)
	}
	a := testApp(t, brokenObj, goodObj)

	list, err := a.ListTemplates()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tpl := range list {
		names[tpl.Name] = true
		if tpl.Name == "broken-spec" && (tpl.Error == "" || tpl.Form != nil) {
			t.Errorf("malformed template = %+v, want its error and no form", tpl)
		}
	}
	if len(list) != 2 || !names["broken-spec"] || !names["good-spec"] {
		t.Fatalf("ListTemplates = %+v, want both templates listed", list)
	}
	// Only the readable template can prefill a job, so only it is offered.
	offered, err := a.TemplateNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(offered) != 1 || offered[0] != "good-spec" {
		t.Fatalf("TemplateNames = %v, want only the readable template", offered)
	}
}

func TestDeleteTemplateOnlyOwnTemplates(t *testing.T) {
	checkOwnTemplatesOnly(t, kf.TrainingRuntimeGVK, func(a *App, name string) error { return a.DeleteTemplate(name) })
}

// checkOwnTemplatesOnly checks that del removes the templates of kind gvk that
// forgeai created, refuses the others and succeeds for a template already gone.
func checkOwnTemplatesOnly(t *testing.T, gvk schema.GroupVersionKind, del func(*App, string) error) {
	t.Helper()
	managed := &unstructured.Unstructured{}
	managed.SetGroupVersionKind(gvk)
	managed.SetNamespace(testNamespace)
	managed.SetName("ours")
	managed.SetLabels(map[string]string{naming.LabelManagedBy: naming.ManagedBy})

	foreign := managed.DeepCopy()
	foreign.SetName("theirs")
	foreign.SetLabels(nil)

	a := testApp(t, managed, foreign)
	if err := del(a, "theirs"); err == nil || !strings.Contains(err.Error(), "not created by forgeai") {
		t.Errorf("delete of a foreign template = %v, want a refusal", err)
	}
	if err := del(a, "ours"); err != nil {
		t.Fatal(err)
	}
	gone := &unstructured.Unstructured{}
	gone.SetGroupVersionKind(gvk)
	if err := a.conn.k8s.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "ours"}, gone); !apierrors.IsNotFound(err) {
		t.Errorf("managed template still there: %v", err)
	}
	if err := del(a, "ours"); err != nil {
		t.Errorf("delete of a missing template: %v", err)
	}
}

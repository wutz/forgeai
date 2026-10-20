package app

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/wutz/forgeai/pkg/kf"
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
	got, err := a.UpdateTemplate("torch", edited)
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
	_, err = a.UpdateTemplate("kubectl-made", testTemplateForm("kubectl-made", "img:2"))
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

	got, err := a.UpdateSandboxTemplate("agent", sandbox.TemplateForm{Image: "agent:2", Command: "python agent.py --once"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "agent" || got.Form.Image != "agent:2" || got.Form.Command != "python agent.py --once" {
		t.Fatalf("UpdateSandboxTemplate returned %+v", got)
	}
}

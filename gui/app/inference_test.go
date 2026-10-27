package app

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	instancetypev1 "github.com/wutz/forgeai-instancetype-operator/api/v1alpha1"
	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/naming"
)

func testInferenceForm() InferenceForm {
	return InferenceForm{
		Name: "qwen", Model: "Qwen/Qwen3.8-27B-FP8", InstanceType: "rtx-5090.x2", Replicas: 1,
	}
}

func testGPUInstanceType() *instancetypev1.InstanceType {
	return &instancetypev1.InstanceType{
		ObjectMeta: metav1.ObjectMeta{Name: "rtx-5090.x2"},
		Spec:       instancetypev1.InstanceTypeSpec{GPU: instancetypev1.InstanceTypeGPU{Class: "rtx-5090", Count: 2}},
	}
}

func TestCheckInferenceFormRefusesBadInput(t *testing.T) {
	tests := map[string]func(*InferenceForm){
		"name not a DNS label": func(f *InferenceForm) { f.Name = "Qwen_1" },
		"endpoint host too long": func(f *InferenceForm) {
			f.Name = strings.Repeat("a", 60)
		},
		"model without owner":  func(f *InferenceForm) { f.Model = "Qwen3" },
		"no instance type":     func(f *InferenceForm) { f.InstanceType = "" },
		"zero replicas":        func(f *InferenceForm) { f.Replicas = 0 },
		"bad HF secret name":   func(f *InferenceForm) { f.HuggingFaceSecret = "Bad_Secret" },
		"served name too long": func(f *InferenceForm) { f.ServedName = strings.Repeat("s", 201) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			f := normalizeInferenceForm(testInferenceForm())
			mutate(&f)
			if err := checkInferenceForm(f, testNamespace); err == nil {
				t.Errorf("checkInferenceForm(%+v) accepted the form", f)
			}
		})
	}
	if err := checkInferenceForm(normalizeInferenceForm(testInferenceForm()), testNamespace); err != nil {
		t.Errorf("valid form refused: %v", err)
	}
}

func TestNormalizeInferenceFormSplitsArgs(t *testing.T) {
	f := testInferenceForm()
	f.Replicas = 0
	f.Args = []string{" --max-model-len 65536 ", "", "   "}
	got := normalizeInferenceForm(f)
	if !slices.Equal(got.Args, []string{"--max-model-len", "65536"}) {
		t.Errorf("args %q, want the flag and its value as separate arguments", got.Args)
	}
	if got.Replicas != 1 {
		t.Errorf("replicas %d, want 1 when unset", got.Replicas)
	}
}

func TestCreateInferenceServiceRendersSpec(t *testing.T) {
	a := testApp(t, testGPUInstanceType())
	f := testInferenceForm()
	f.ServedName = "qwen"
	f.Args = []string{"--max-model-len 65536"}
	got, err := a.CreateInferenceService(f)
	if err != nil {
		t.Fatal(err)
	}
	if got.ServedName != "qwen" || got.Model != f.Model || got.Replicas != 1 {
		t.Errorf("view %+v, want the form", got)
	}
	stored := &v1.InferenceService{}
	if err := a.conn.k8s.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "qwen"}, stored); err != nil {
		t.Fatal(err)
	}
	if stored.Spec.ServedModelName != "qwen" || len(stored.Spec.Args) != 2 {
		t.Errorf("stored spec %+v, want the served name and args", stored.Spec)
	}
}

func TestCreateInferenceServiceRefusesCPUInstanceType(t *testing.T) {
	cpu := testGPUInstanceType()
	cpu.Spec.GPU.Count = 0
	a := testApp(t, cpu)
	if _, err := a.CreateInferenceService(testInferenceForm()); err == nil {
		t.Error("created a service on an instance type without GPUs")
	}
}

func TestStopAndStartInferenceServiceToggleAnnotation(t *testing.T) {
	svc := renderInferenceService(testInferenceForm(), testNamespace)
	a := testApp(t, svc)
	stopped, err := a.StopInferenceService("qwen")
	if err != nil {
		t.Fatal(err)
	}
	if !stopped.Stopped {
		t.Error("view not stopped after StopInferenceService")
	}
	started, err := a.StartInferenceService("qwen")
	if err != nil {
		t.Fatal(err)
	}
	if started.Stopped {
		t.Error("view still stopped after StartInferenceService")
	}
}

func TestInferenceAPIKeyReadsSecret(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: naming.InferenceAPIKeySecret("qwen"), Namespace: testNamespace},
		Data:       map[string][]byte{naming.InferenceAPIKey: []byte("key-1")},
	}
	a := testApp(t, secret)
	key, err := a.InferenceAPIKey("qwen")
	if err != nil {
		t.Fatal(err)
	}
	if key != "key-1" {
		t.Errorf("key %q, want key-1", key)
	}
}

func TestToInferenceServiceReadsReadyCondition(t *testing.T) {
	svc := renderInferenceService(testInferenceForm(), testNamespace)
	svc.Status.ReadyReplicas = 1
	meta.SetStatusCondition(&svc.Status.Conditions, metav1.Condition{
		Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready", Message: "1 model servers ready",
	})
	got := toInferenceService(svc)
	if !got.Ready || got.Reason != "Ready" || got.ReadyReplicas != 1 {
		t.Errorf("view %+v, want Ready with one replica", got)
	}
}

func TestListInferenceServicesNewestFirst(t *testing.T) {
	now := time.Now()
	old := renderInferenceService(testInferenceForm(), testNamespace)
	old.Name, old.CreationTimestamp = "old", metav1.NewTime(now.Add(-time.Hour))
	fresh := renderInferenceService(testInferenceForm(), testNamespace)
	fresh.Name, fresh.CreationTimestamp = "fresh", metav1.NewTime(now)
	a := testApp(t, old, fresh)

	list, err := a.ListInferenceServices()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(list))
	for _, svc := range list {
		names = append(names, svc.Name)
	}
	if !slices.Equal(names, []string{"fresh", "old"}) {
		t.Errorf("order %v, want newest first", names)
	}
	empty, err := testApp(t).ListInferenceServices()
	if err != nil || len(empty) != 0 {
		t.Errorf("empty namespace = %d services, %v", len(empty), err)
	}
}

func TestGetAndDeleteInferenceService(t *testing.T) {
	a := testApp(t, renderInferenceService(testInferenceForm(), testNamespace))
	got, err := a.GetInferenceService("qwen")
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "Qwen/Qwen3.8-27B-FP8" {
		t.Errorf("view %+v", got)
	}
	if _, err := a.GetInferenceService("nope"); !apierrors.IsNotFound(err) {
		t.Errorf("get of a missing service = %v, want NotFound", err)
	}
	if err := a.DeleteInferenceService("qwen"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetInferenceService("qwen"); !apierrors.IsNotFound(err) {
		t.Errorf("service still there after delete: %v", err)
	}
	// A repeated delete finds nothing and succeeds.
	if err := a.DeleteInferenceService("qwen"); err != nil {
		t.Errorf("delete of a missing service: %v", err)
	}
}

func TestCreateInferenceServiceRefusesUnknownInstanceType(t *testing.T) {
	if _, err := testApp(t).CreateInferenceService(testInferenceForm()); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("create on an unknown instance type = %v, want not found", err)
	}
}

func TestInferenceAPIKeyNeedsAKey(t *testing.T) {
	a := testApp(t)
	if _, err := a.InferenceAPIKey("qwen"); !apierrors.IsNotFound(err) {
		t.Errorf("key of a service without a secret = %v, want NotFound", err)
	}
	empty := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: naming.InferenceAPIKeySecret("qwen"), Namespace: testNamespace},
	}
	a = testApp(t, empty)
	if _, err := a.InferenceAPIKey("qwen"); err == nil || !strings.Contains(err.Error(), "no API key yet") {
		t.Errorf("key of an empty secret = %v, want a not-yet error", err)
	}
}

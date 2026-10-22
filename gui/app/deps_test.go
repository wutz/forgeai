package app

import (
	"bytes"
	"context"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/wutz/forgeai/pkg/platform"
)

func TestEmbeddedBundlesVerify(t *testing.T) {
	bs, err := bundles()
	if err != nil {
		t.Fatalf("bundles: %v", err)
	}
	var names []string
	for _, b := range bs {
		names = append(names, b.Name)
		if len(b.objects) == 0 {
			t.Errorf("%s: no objects", b.Name)
		}
	}
	// Kueue's TrainJob integration needs the Trainer CRDs, so Trainer comes first.
	if got, want := strings.Join(names, ","), "trainer,kueue,agent-sandbox"; got != want {
		t.Errorf("bundle order = %s, want %s", got, want)
	}
	wantCRDs := map[string]string{
		"trainer":       "trainjobs.trainer.kubeflow.org",
		"kueue":         "workloads.kueue.x-k8s.io",
		"agent-sandbox": "sandboxes.agents.x-k8s.io",
	}
	for _, b := range bs {
		if !slices.Contains(b.crdNames(), wantCRDs[b.Name]) {
			t.Errorf("%s: CRDs %v lack %s", b.Name, b.crdNames(), wantCRDs[b.Name])
		}
	}
}

func TestLoadBundlesRejectsTamperedManifest(t *testing.T) {
	// Copy the embedded deps, changing one byte of Kueue's manifest.
	copied := fstest.MapFS{}
	err := fs.WalkDir(depsFS, "deps", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(depsFS, p)
		if err != nil {
			return err
		}
		if p == "deps/kueue.yaml" {
			data = append(bytes.Clone(data), '\n', '#')
		}
		copied[p] = &fstest.MapFile{Data: data}
		return nil
	})
	if err != nil {
		t.Fatalf("copy deps: %v", err)
	}
	if _, err := loadBundles(copied); err == nil || !strings.Contains(err.Error(), "digest does not match") {
		t.Fatalf("loadBundles with tampered kueue.yaml: err = %v, want digest mismatch", err)
	}
}

func TestInstallStage(t *testing.T) {
	custom := map[schema.GroupKind]bool{
		{Group: "trainer.kubeflow.org", Kind: "ClusterTrainingRuntime"}: true,
	}
	tests := []struct {
		name   string
		apiVer string
		kind   string
		want   int
	}{
		{"namespace", "v1", "Namespace", 0},
		{"CRD", "apiextensions.k8s.io/v1", "CustomResourceDefinition", 0},
		{"deployment", "apps/v1", "Deployment", 1},
		{"RBAC", "rbac.authorization.k8s.io/v1", "ClusterRole", 1},
		{"validating webhook", "admissionregistration.k8s.io/v1", "ValidatingWebhookConfiguration", 2},
		{"mutating webhook", "admissionregistration.k8s.io/v1", "MutatingWebhookConfiguration", 2},
		{"custom resource of a bundled CRD", "trainer.kubeflow.org/v1alpha1", "ClusterTrainingRuntime", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &unstructured.Unstructured{Object: map[string]any{"apiVersion": tt.apiVer, "kind": tt.kind}}
			if got := installStage(u, custom); got != tt.want {
				t.Errorf("installStage(%s) = %d, want %d", tt.kind, got, tt.want)
			}
		})
	}
}

func testDeployment(version string, available bool) appsv1.Deployment {
	status := corev1.ConditionFalse
	if available {
		status = corev1.ConditionTrue
	}
	d := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name: "controller", Namespace: "system", Generation: 1,
		Labels: map[string]string{depVersionLabel: version},
	}}
	d.Status.ObservedGeneration = 1
	d.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: status}}
	return d
}

func TestAssess(t *testing.T) {
	bundled := Dependency{Name: "kueue", Pinned: "v0.20.2", Installable: true}
	operator := Dependency{Name: "forgeai", Installable: true, Pinned: forgeaiChartVersion}
	sandbox := Dependency{Name: "agent-sandbox", Pinned: "v1.0.7", Installable: true}
	tests := []struct {
		name      string
		dep       Dependency
		h         depHealth
		status    string
		installed string
	}{
		{"nothing installed", bundled, depHealth{crdsWanted: 2}, "missing", ""},
		{"some CRDs", bundled, depHealth{crdsWanted: 2, crdsFound: 1}, "degraded", ""},
		{"CRDs without the operator", operator, depHealth{crdsWanted: 2, crdsFound: 2}, "degraded", ""},
		{"CRDs without a labelled deployment", bundled, depHealth{crdsWanted: 2, crdsFound: 2}, "unmanaged", ""},
		{"pinned and available", bundled, depHealth{crdsWanted: 2, crdsFound: 2,
			deployments: []appsv1.Deployment{testDeployment("v0.20.2", true)}}, "ready", "v0.20.2"},
		{"older version", bundled, depHealth{crdsWanted: 2, crdsFound: 2,
			deployments: []appsv1.Deployment{testDeployment("v0.18.1", true)}}, "outdated", "v0.18.1"},
		{"not available", bundled, depHealth{crdsWanted: 2, crdsFound: 2,
			deployments: []appsv1.Deployment{testDeployment("v0.20.2", false)}}, "degraded", "v0.20.2"},
		{"operator from the chart", operator, depHealth{crdsWanted: 2, crdsFound: 2,
			deployments: []appsv1.Deployment{testDeployment(forgeaiChartVersion, true)}}, "ready", forgeaiChartVersion},
		{"older operator chart", operator, depHealth{crdsWanted: 2, crdsFound: 2,
			deployments: []appsv1.Deployment{testDeployment("0.0.9", true)}}, "outdated", "0.0.9"},
		{"operator deployed outside Helm", operator, depHealth{crdsWanted: 2, crdsFound: 2,
			deployments: []appsv1.Deployment{testDeployment("", true)}}, "unmanaged", ""},
		{"runs where the config expects", sandbox, depHealth{crdsWanted: 1, crdsFound: 1,
			deployments: []appsv1.Deployment{testDeployment("v1.0.7", true)},
			namespace:   "agent-sandbox-system", expectNamespace: "agent-sandbox-system"}, "ready", "v1.0.7"},
		{"runs elsewhere than the config expects", sandbox, depHealth{crdsWanted: 1, crdsFound: 1,
			deployments: []appsv1.Deployment{testDeployment("v1.0.7", true)},
			namespace:   "other", expectNamespace: "agent-sandbox-system"}, "mismatch", "v1.0.7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := tt.dep
			assess(&dep, tt.h)
			if dep.Status != tt.status || dep.Installed != tt.installed {
				t.Errorf("assess = status %q installed %q, want %q %q (message %q)",
					dep.Status, dep.Installed, tt.status, tt.installed, dep.Message)
			}
		})
	}
}

// TestAssessReason checks the reason codes the GUI explains in its own words,
// and the namespace a mismatched component should run in.
func TestAssessReason(t *testing.T) {
	bundled := Dependency{Name: "kueue", Pinned: "v0.20.2", Installable: true}
	operator := Dependency{Name: "forgeai", Installable: true, Pinned: forgeaiChartVersion}
	sandbox := Dependency{Name: "agent-sandbox", Pinned: "v1.0.7", Installable: true}
	tests := []struct {
		name   string
		dep    Dependency
		h      depHealth
		reason string
		expect string
	}{
		{"installed outside forgeai", bundled, depHealth{crdsWanted: 2, crdsFound: 2}, "unmanaged", ""},
		{"operator deployed outside Helm", operator, depHealth{crdsWanted: 2, crdsFound: 2,
			deployments: []appsv1.Deployment{testDeployment("", true)}}, "unmanagedHelm", ""},
		{"runs elsewhere than the config expects", sandbox, depHealth{crdsWanted: 1, crdsFound: 1,
			deployments: []appsv1.Deployment{testDeployment("v1.0.7", true)},
			namespace:   "other", expectNamespace: "agent-sandbox-system"}, "namespaceMismatch", "agent-sandbox-system"},
		{"operator deployment missing", operator, depHealth{crdsWanted: 2, crdsFound: 2}, "operatorMissing", ""},
		{"pinned and available", bundled, depHealth{crdsWanted: 2, crdsFound: 2,
			deployments: []appsv1.Deployment{testDeployment("v0.20.2", true)}}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := tt.dep
			assess(&dep, tt.h)
			if dep.Reason != tt.reason || dep.ExpectedNamespace != tt.expect {
				t.Errorf("assess = reason %q expected namespace %q, want %q %q",
					dep.Reason, dep.ExpectedNamespace, tt.reason, tt.expect)
			}
		})
	}
}

func TestRecordRoundTrip(t *testing.T) {
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	k8s := fake.NewClientBuilder().WithScheme(s).Build()
	ctx := context.Background()

	if got, err := readRecords(ctx, k8s); err != nil || len(got) != 0 {
		t.Fatalf("readRecords without the ConfigMap = %v, %v; want empty", got, err)
	}
	trainer := depRecord{Version: "v2.3.0", Namespace: "kubeflow-system", ManifestSHA256: "aa",
		AppliedBy: "alice", AppliedAt: "2026-10-22T00:00:00Z"}
	kueue := depRecord{Version: "v0.20.2", Namespace: "kueue-system", ManifestSHA256: "bb",
		AppliedAt: "2026-10-22T01:00:00Z"}
	if err := writeRecord(ctx, k8s, "trainer", trainer); err != nil {
		t.Fatalf("create record: %v", err)
	}
	if err := writeRecord(ctx, k8s, "kueue", kueue); err != nil {
		t.Fatalf("add record: %v", err)
	}
	// Reinstalling a component replaces its entry and keeps the others.
	trainer.Version = "v2.4.0"
	if err := writeRecord(ctx, k8s, "trainer", trainer); err != nil {
		t.Fatalf("update record: %v", err)
	}
	got, err := readRecords(ctx, k8s)
	if err != nil {
		t.Fatalf("readRecords: %v", err)
	}
	if want := map[string]depRecord{"trainer": trainer, "kueue": kueue}; !reflect.DeepEqual(got, want) {
		t.Errorf("records = %+v, want %+v", got, want)
	}
}

func TestCheckInstallTarget(t *testing.T) {
	bs, err := bundles()
	if err != nil {
		t.Fatalf("bundles: %v", err)
	}
	var sandbox *depBundle
	for _, b := range bs {
		if b.Name == "agent-sandbox" {
			sandbox = b
		}
	}
	if sandbox == nil {
		t.Fatal("no agent-sandbox bundle")
	}
	if got := bundleNamespace(sandbox); got != "agent-sandbox-system" {
		t.Fatalf("bundleNamespace = %q, want agent-sandbox-system", got)
	}
	defaults, err := platform.Parse([]byte{})
	if err != nil {
		t.Fatalf("parse defaults: %v", err)
	}
	if err := checkInstallTarget(sandbox, defaults); err != nil {
		t.Errorf("default config refused: %v", err)
	}
	moved, err := platform.Parse([]byte("sandbox:\n  namespace: sandbox-system\n"))
	if err != nil {
		t.Fatalf("parse sandbox.namespace: %v", err)
	}
	if err := checkInstallTarget(sandbox, moved); err == nil {
		t.Error("install accepted while sandbox.namespace names another namespace")
	}
}

func TestCheckChartTarget(t *testing.T) {
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	operator := func(labels map[string]string) client.Client {
		d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
			Namespace: "forgeai-system", Name: forgeaiDeployment, Labels: labels,
		}}
		return fake.NewClientBuilder().WithScheme(s).WithObjects(d).Build()
	}
	tests := []struct {
		name    string
		k8s     client.Client
		wantErr bool
	}{
		{"nothing installed", fake.NewClientBuilder().WithScheme(s).Build(), false},
		{"installed from the chart", operator(map[string]string{depLabel: "forgeai"}), false},
		{"deployed outside Helm", operator(map[string]string{"app": "forgeai"}), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkChartTarget(context.Background(), tt.k8s)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkChartTarget = %v, want error %v", err, tt.wantErr)
			}
		})
	}
}

func TestDepInstallsRejectsSecondStart(t *testing.T) {
	var d depInstalls
	if !d.start("kueue") {
		t.Fatal("first start rejected")
	}
	if d.start("kueue") {
		t.Fatal("second start of the same component accepted")
	}
	if !d.active("kueue") {
		t.Fatal("kueue not active while installing")
	}
	d.finish("kueue")
	if d.active("kueue") || !d.start("kueue") {
		t.Fatal("component not reusable after finish")
	}
}

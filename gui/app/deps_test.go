package app

import (
	"bytes"
	"context"
	"fmt"
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
	if got, want := strings.Join(names, ","), "trainer,kueue,agent-sandbox,gateway-api-inference-extension,sshpiper"; got != want {
		t.Errorf("bundle order = %s, want %s", got, want)
	}
	wantCRDs := map[string]string{
		"trainer":                         "trainjobs.trainer.kubeflow.org",
		"kueue":                           "workloads.kueue.x-k8s.io",
		"agent-sandbox":                   "sandboxes.agents.x-k8s.io",
		"gateway-api-inference-extension": "inferencepools.inference.networking.k8s.io",
		"sshpiper":                        "pipes.sshpiper.com",
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

// helmDeployment returns a Deployment installed by the SSH gateway chart at chartVersion.
func helmDeployment(chartVersion string, available bool) appsv1.Deployment {
	return helmChartDeployment("sshgateway", chartVersion, available)
}

// helmChartDeployment returns a Deployment installed by the chart chart at chartVersion.
func helmChartDeployment(chart, chartVersion string, available bool) appsv1.Deployment {
	d := testDeployment("", available)
	d.Labels = map[string]string{helmChartLabel: chart + "-" + chartVersion}
	return d
}

func TestAssess(t *testing.T) {
	bundled := Dependency{Name: "kueue", Pinned: "v0.20.2", Installable: true}
	operator := Dependency{Name: "forgeai", Installable: true, Pinned: forgeaiChartVersion}
	sshGateway := Dependency{Name: "sshgateway", Installable: true, Pinned: sshGatewayChartVersion}
	project := Dependency{Name: "workspace", Installable: true, Pinned: projectChartVersion}
	sandbox := Dependency{Name: "agent-sandbox", Pinned: "v1.0.7", Installable: true}
	gatewayAPI := Dependency{Name: "gateway-api-inference-extension", Pinned: "v1.6.2", Installable: true}
	tests := []struct {
		name      string
		dep       Dependency
		h         depHealth
		status    string
		installed string
	}{
		{"nothing installed", bundled, depHealth{crdsWanted: 2}, "missing", ""},
		{"some CRDs", bundled, depHealth{crdsWanted: 2, crdsFound: 1}, "degraded", ""},
		{"CRDs without the operator", operator, depHealth{crdsWanted: 2, crdsFound: 2, chart: forgeaiSpec}, "degraded", ""},
		{"CRDs without a labelled deployment", bundled, depHealth{crdsWanted: 2, crdsFound: 2}, "unmanaged", ""},
		{"CRD-only bundle installed by forgeai", gatewayAPI, depHealth{crdsWanted: 1, crdsFound: 1,
			crdVersion: "v1.6.2"}, "ready", "v1.6.2"},
		{"CRD-only bundle at an older version", gatewayAPI, depHealth{crdsWanted: 1, crdsFound: 1,
			crdVersion: "v1.5.0"}, "outdated", "v1.5.0"},
		{"CRD-only bundle installed outside forgeai", gatewayAPI, depHealth{crdsWanted: 1, crdsFound: 1}, "unmanaged", ""},
		{"pinned and available", bundled, depHealth{crdsWanted: 2, crdsFound: 2,
			deployments: []appsv1.Deployment{testDeployment("v0.20.2", true)}}, "ready", "v0.20.2"},
		{"older version", bundled, depHealth{crdsWanted: 2, crdsFound: 2,
			deployments: []appsv1.Deployment{testDeployment("v0.18.1", true)}}, "outdated", "v0.18.1"},
		{"not available", bundled, depHealth{crdsWanted: 2, crdsFound: 2,
			deployments: []appsv1.Deployment{testDeployment("v0.20.2", false)}}, "degraded", "v0.20.2"},
		{"operator from the chart", operator, depHealth{crdsWanted: 2, crdsFound: 2, chart: forgeaiSpec,
			deployments: []appsv1.Deployment{testDeployment(forgeaiChartVersion, true)}}, "ready", forgeaiChartVersion},
		{"older operator chart", operator, depHealth{crdsWanted: 2, crdsFound: 2, chart: forgeaiSpec,
			deployments: []appsv1.Deployment{testDeployment("0.0.9", true)}}, "outdated", "0.0.9"},
		{"operator deployed outside Helm", operator, depHealth{crdsWanted: 2, crdsFound: 2, chart: forgeaiSpec,
			deployments: []appsv1.Deployment{testDeployment("", true)}}, "unmanaged", ""},
		{"SSH gateway from its chart", sshGateway, depHealth{crdsWanted: 3, crdsFound: 3, chart: sshGatewaySpec,
			deployments: []appsv1.Deployment{helmDeployment(sshGatewayChartVersion, true)}}, "ready", sshGatewayChartVersion},
		{"older SSH gateway chart", sshGateway, depHealth{crdsWanted: 3, crdsFound: 3, chart: sshGatewaySpec,
			deployments: []appsv1.Deployment{helmDeployment("0.0.9", true)}}, "outdated", "0.0.9"},
		{"SSH gateway deployed outside Helm", sshGateway, depHealth{crdsWanted: 3, crdsFound: 3, chart: sshGatewaySpec,
			deployments: []appsv1.Deployment{testDeployment("", true)}}, "unmanaged", ""},
		{"Project operator from its chart", project, depHealth{crdsWanted: 2, crdsFound: 2,
			chart: projectSpec, deployments: []appsv1.Deployment{
				helmChartDeployment("workspace", projectChartVersion, true)}},
			"ready", projectChartVersion},
		{"older Project operator chart", project, depHealth{crdsWanted: 2, crdsFound: 2,
			chart: projectSpec, deployments: []appsv1.Deployment{
				helmChartDeployment("workspace", "0.0.9", true)}}, "outdated", "0.0.9"},
		{"Project operator deployed outside Helm", project, depHealth{crdsWanted: 2, crdsFound: 2,
			chart: projectSpec, deployments: []appsv1.Deployment{testDeployment("", true)}}, "unmanaged", ""},
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
		{"operator deployed outside Helm", operator, depHealth{crdsWanted: 2, crdsFound: 2, chart: forgeaiSpec,
			deployments: []appsv1.Deployment{testDeployment("", true)}}, "unmanagedHelm", ""},
		{"runs elsewhere than the config expects", sandbox, depHealth{crdsWanted: 1, crdsFound: 1,
			deployments: []appsv1.Deployment{testDeployment("v1.0.7", true)},
			namespace:   "other", expectNamespace: "agent-sandbox-system"}, "namespaceMismatch", "agent-sandbox-system"},
		{"operator deployment missing", operator, depHealth{crdsWanted: 2, crdsFound: 2, chart: forgeaiSpec}, "operatorMissing", ""},
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

// TestAssessRunningVersion checks that a component the GUI did not install
// shows the tag of its image, while its status stays unmanaged.
func TestAssessRunningVersion(t *testing.T) {
	sandbox := Dependency{Name: "agent-sandbox", Pinned: "v1.0.7", Installable: true}
	withImage := func(image string) appsv1.Deployment {
		d := testDeployment("", true)
		d.Spec.Template.Spec.Containers = []corev1.Container{{Image: image}}
		return d
	}
	tests := []struct {
		name    string
		deploy  appsv1.Deployment
		status  string
		running string
	}{
		{"tagged image outside forgeai", withImage("registry.k8s.io/agent-sandbox/agent-sandbox-controller:v1.0.7"), "unmanaged", "v1.0.7"},
		{"digest-pinned image", withImage("ghcr.io/wutz/gatessh@sha256:36d4c4acba9e432a2459563c51a1a10ddabe2f9de663b78be9d53f56cf742cef"), "unmanaged", ""},
		{"managed deployment has no running version", testDeployment("v1.0.7", true), "ready", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := sandbox
			assess(&dep, depHealth{crdsWanted: 1, crdsFound: 1, deployments: []appsv1.Deployment{tt.deploy}})
			if dep.Status != tt.status || dep.Running != tt.running {
				t.Errorf("assess = status %q running %q, want %q %q", dep.Status, dep.Running, tt.status, tt.running)
			}
		})
	}
}

func TestImageTag(t *testing.T) {
	tests := []struct{ image, tag string }{
		{"registry.k8s.io/agent-sandbox/controller:v1.0.7", "v1.0.7"},
		{"nginx:1.25", "1.25"},
		{"localhost:5000/app", ""},
		{"localhost:5000/app:2", "2"},
		{"app@sha256:abc", ""},
		{"app:1.0@sha256:abc", ""},
		{"app", ""},
	}
	for _, tt := range tests {
		d := appsv1.Deployment{}
		d.Spec.Template.Spec.Containers = []corev1.Container{{Image: tt.image}}
		if got := imageTag(&d); got != tt.tag {
			t.Errorf("imageTag(%q) = %q, want %q", tt.image, got, tt.tag)
		}
	}
}

func TestBundleRunningTags(t *testing.T) {
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Each bundle Deployment runs an image tagged with its own name, so the
	// expected string lists every one of them in bundle order.
	for _, name := range []string{"agent-sandbox", "trainer"} {
		t.Run(name, func(t *testing.T) {
			b, err := bundleByName(name)
			if err != nil {
				t.Fatal(err)
			}
			var objs []client.Object
			var want []string
			for _, o := range b.objects {
				if o.GetKind() != "Deployment" || o.GroupVersionKind().Group != appsv1.GroupName {
					continue
				}
				tag := "v" + o.GetName()
				d := testDeployment("", true)
				d.Namespace, d.Name = o.GetNamespace(), o.GetName()
				d.Spec.Template.Spec.Containers = []corev1.Container{{Image: "registry.example/" + o.GetName() + ":" + tag}}
				objs = append(objs, &d)
				want = append(want, tag)
			}
			if len(objs) == 0 {
				t.Fatal("bundle declares no Deployment")
			}
			if got, err := bundleRunningTags(ctx, fake.NewClientBuilder().WithScheme(s).Build(), b); err != nil || got != "" {
				t.Fatalf("bundleRunningTags without the Deployments = %q, %v; want empty", got, err)
			}
			k8s := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
			if got, err := bundleRunningTags(ctx, k8s, b); err != nil || got != strings.Join(want, ", ") {
				t.Fatalf("bundleRunningTags = %q, %v; want %q", got, err, strings.Join(want, ", "))
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
	// A fresh cluster has no platform namespace before its first install.
	if err := writeRecord(ctx, k8s, "trainer", trainer); err != nil {
		t.Fatalf("create record: %v", err)
	}
	if err := k8s.Get(ctx, client.ObjectKey{Name: platform.Namespace()}, &corev1.Namespace{}); err != nil {
		t.Fatalf("record namespace: %v", err)
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
	withObjects := func(objs ...client.Object) client.Client {
		return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
	}
	deployment := func(spec *chartSpec, labels map[string]string) *appsv1.Deployment {
		return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
			Namespace: spec.installNamespace(), Name: spec.deployment, Labels: labels,
		}}
	}
	pipeCRD := &unstructured.Unstructured{}
	pipeCRD.SetGroupVersionKind(crdGVK)
	pipeCRD.SetName("pipes.sshpiper.com")
	tests := []struct {
		name    string
		spec    *chartSpec
		k8s     client.Client
		wantErr bool
	}{
		{"nothing installed", forgeaiSpec, withObjects(), false},
		{"installed from the chart", forgeaiSpec,
			withObjects(deployment(forgeaiSpec, map[string]string{helmChartLabel: "forgeai-" + forgeaiChartVersion})), false},
		{"deployed outside Helm", forgeaiSpec,
			withObjects(deployment(forgeaiSpec, map[string]string{"app": "forgeai"})), true},
		{"SSH gateway without the Pipe CRD", sshGatewaySpec, withObjects(), false},
		{"SSH gateway installed from another chart", sshGatewaySpec, withObjects(
			deployment(sshGatewaySpec, map[string]string{helmChartLabel: "forgeai-" + forgeaiChartVersion})), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkChartTarget(context.Background(), tt.k8s, tt.spec)
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

func TestForEachConcurrentlyRunsEachIndexAndReturnsFirstError(t *testing.T) {
	const n = 8
	ran := make([]bool, n)
	err := forEachConcurrently(n, func(i int) error {
		ran[i] = true
		if i == 5 || i == 2 {
			return fmt.Errorf("probe %d failed", i)
		}
		return nil
	})
	for i, ok := range ran {
		if !ok {
			t.Errorf("index %d did not run", i)
		}
	}
	if err == nil || err.Error() != "probe 2 failed" {
		t.Errorf("err = %v, want the first failure by index (probe 2)", err)
	}
	if err := forEachConcurrently(n, func(int) error { return nil }); err != nil {
		t.Errorf("no failures: err = %v", err)
	}
}

// TestProbeCRDOnlyBundleVersion checks that a CRD-only bundle the GUI installed
// reads as installed at its pinned version, from the labels its CRDs carry.
func TestProbeCRDOnlyBundleVersion(t *testing.T) {
	b, err := bundleByName("gateway-api-inference-extension")
	if err != nil || b == nil {
		t.Fatalf("bundleByName = %v, %v; want the bundle", b, err)
	}
	var objs []client.Object
	for _, o := range b.objects {
		if o.GetKind() != "CustomResourceDefinition" {
			continue
		}
		u := o.DeepCopy()
		labels := u.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[depVersionLabel] = b.Version
		u.SetLabels(labels)
		objs = append(objs, u)
	}
	if len(objs) == 0 {
		t.Fatal("bundle has no CRDs")
	}
	k8s := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(objs...).Build()
	info, ok := depInfoByName(b.Name)
	if !ok {
		t.Fatalf("no catalog entry for %s", b.Name)
	}
	dep, err := probeDependency(context.Background(), k8s, info, b, "")
	if err != nil {
		t.Fatal(err)
	}
	if dep.Status != "ready" || dep.Installed != b.Version {
		t.Errorf("probe = status %q installed %q, want ready at %q (message %q)", dep.Status, dep.Installed, b.Version, dep.Message)
	}
}

// TestDependencyCatalogOrder checks that the project operator is installed
// before the forgeai operator, which reads the InstanceTypes it derives.
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
	if pos("workspace") > pos("forgeai") {
		t.Errorf("project is listed after forgeai")
	}
}

func TestRecordReplacesLegacyName(t *testing.T) {
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	k8s := fake.NewClientBuilder().WithScheme(s).Build()
	ctx := context.Background()

	old := depRecord{Version: "0.2.0", Namespace: platform.Namespace(), AppliedAt: "2026-10-27T00:00:00Z"}
	kueue := depRecord{Version: "v0.20.2", Namespace: "kueue-system", AppliedAt: "2026-10-22T01:00:00Z"}
	if err := writeRecord(ctx, k8s, "fileserver-operator", old); err != nil {
		t.Fatalf("create legacy record: %v", err)
	}
	if err := writeRecord(ctx, k8s, "kueue", kueue); err != nil {
		t.Fatalf("add record: %v", err)
	}
	fs := depRecord{Version: "0.3.0", Namespace: platform.Namespace(), AppliedAt: "2026-10-28T00:00:00Z"}
	if err := writeRecord(ctx, k8s, "fileserver", fs, "fileserver-operator"); err != nil {
		t.Fatalf("replace record: %v", err)
	}
	got, err := readRecords(ctx, k8s)
	if err != nil {
		t.Fatalf("readRecords: %v", err)
	}
	if want := map[string]depRecord{"fileserver": fs, "kueue": kueue}; !reflect.DeepEqual(got, want) {
		t.Errorf("records = %+v, want %+v", got, want)
	}
}

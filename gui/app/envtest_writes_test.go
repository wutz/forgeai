package app

import (
	"context"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/sandbox"
)

const testPlatformConfig = `namespacePrefix: forge-
apiServer: https://127.0.0.1:6443
ingress: {className: nginx, domain: local.test, port: 7443}
registry: {domain: localhost:5000, insecure: true, adminSecret: forgeai-harbor}
storageTypes:
  shared: {storageClass: standard, description: Shared test storage}
defaultStorageType: shared
instanceTypes:
  reserved: {cpu: "1", memory: 1Gi}
  sizes: [1, 2, 4, 8]
defaultQuota:
  gpus: {"*": 8}
  storage: 2Ti
  volumes: 20
  pods: 100
  headroom: {cpu: "1", memory: 1Gi}
network:
  systemNamespaces: [kube-system]
fileServer:
  idleTimeout: 30m
`

// Opt-in: needs a kube-apiserver and etcd in bin/k8s (FORGEAI_ENVTEST=1).
func TestEnvtestWrites(t *testing.T) {
	if os.Getenv("FORGEAI_ENVTEST") == "" {
		t.Skip("set FORGEAI_ENVTEST=1")
	}
	env := &envtest.Environment{
		BinaryAssetsDirectory: "../../bin/k8s/1.37.0-darwin-arm64",
		CRDDirectoryPaths:     []string{"../../config/crd/bases"},
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("envtest start: %v", err)
	}
	defer func() { _ = env.Stop() }()

	ctx := context.Background()
	c, err := client.New(cfg, client.Options{Scheme: scheme()})
	if err != nil {
		t.Fatal(err)
	}
	// Labelled as the project controller labels a project namespace.
	if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "forge-demo", Labels: map[string]string{
		naming.LabelManagedBy: naming.ManagedBy,
		naming.LabelProject:   "demo",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "forge-demo", Name: "forgeai-platform"},
		Data:       map[string]string{"config.yaml": testPlatformConfig},
	}); err != nil {
		t.Fatal(err)
	}
	it := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "forgeai.wutz.dev/v1alpha1", "kind": "InstanceType",
		"metadata": map[string]any{"name": "cpu.small"},
		"spec":     map[string]any{"cpu": "2", "memory": "8Gi", "gpu": map[string]any{"class": "", "count": int64(0), "product": ""}, "nodeSelector": map[string]any{}},
	}}
	if err := c.Create(ctx, it); err != nil {
		t.Fatalf("create InstanceType: %v", err)
	}
	a := &App{ctx: ctx, conn: &Connection{Info: ConnInfo{Namespace: "forge-demo", ClusterAdmin: true}, k8s: c, rest: cfg}}

	step := func(name string, err error) {
		t.Helper()
		if err != nil {
			t.Errorf("%s: %v", name, err)
		} else {
			t.Logf("OK %s", name)
		}
	}

	// InstallDependency emits Wails events, which need the app's context, so
	// the embedded bundles are applied directly: the same objects, same order.
	bs, err := bundles()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bs {
		step("apply bundle "+b.Name, applyAll(ctx, c, b.objects))
		// Each bundle registers admission webhooks served by controllers that do
		// not run here; drop them before the next bundle's objects are applied.
		vwc := &admissionregistrationv1.ValidatingWebhookConfigurationList{}
		_ = c.List(ctx, vwc)
		for i := range vwc.Items {
			_ = c.Delete(ctx, &vwc.Items[i])
		}
		mwc := &admissionregistrationv1.MutatingWebhookConfigurationList{}
		_ = c.List(ctx, mwc)
		for i := range mwc.Items {
			_ = c.Delete(ctx, &mwc.Items[i])
		}
	}

	_, err = a.CreateVolume("scratch", "shared", 10)
	step("CreateVolume scratch", err)
	_, err = a.TrashVolume("scratch")
	step("TrashVolume scratch", err)
	_, err = a.RestoreVolume("scratch")
	step("RestoreVolume scratch", err)
	_, err = a.TrashVolume("scratch")
	step("TrashVolume scratch again", err)
	step("PurgeVolume scratch", a.PurgeVolume("scratch"))

	_, err = a.CreateTemplate(testTemplateForm("torch", "img:1"))
	step("CreateTemplate torch", err)
	_, err = a.CreateJob(testTemplateForm("job-1", "img:1"))
	step("CreateJob job-1", err)
	step("DeleteJob job-1", a.DeleteJob("job-1"))

	_, err = a.CreateSandbox(sandbox.Form{Name: "agent", Image: "img"})
	step("CreateSandbox agent", err)
	step("DeleteSandbox agent", a.DeleteSandbox("agent"))
}

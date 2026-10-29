package app

import (
	"io"
	"slices"
	"testing"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/wutz/forgeai/pkg/platform"
)

// TestForgeaiChartRenders renders the chart the GUI installs and checks what
// the GUI relies on: the operator Deployment carries the dependency labels and
// runs with the platform namespace, and the chart installs every CRD the
// Dependencies view counts.
func TestForgeaiChartRenders(t *testing.T) {
	const ns = "forgeai-system"
	chrt, err := loader.Load("../../charts/chart")
	if err != nil {
		t.Fatalf("load chart: %v", err)
	}
	if chrt.Metadata.Version != forgeaiChartVersion {
		t.Errorf("chart version = %s, want %s", chrt.Metadata.Version, forgeaiChartVersion)
	}
	cfg := &action.Configuration{
		Capabilities: chartutil.DefaultCapabilities,
		KubeClient:   &kubefake.PrintingKubeClient{Out: io.Discard},
		Releases:     storage.Init(driver.NewMemory()),
		Log:          func(string, ...any) {},
	}
	ins := action.NewInstall(cfg)
	ins.ClientOnly = true
	ins.DryRun = true
	ins.ReleaseName = forgeaiRelease
	ins.Namespace = ns
	rel, err := ins.Run(chrt, nil)
	if err != nil {
		t.Fatalf("render chart: %v", err)
	}
	objs, err := decodeObjects([]byte(rel.Manifest))
	if err != nil {
		t.Fatalf("decode manifest: %v", err)
	}

	var crds []string
	var operator *unstructured.Unstructured
	for _, o := range objs {
		switch {
		case o.GroupVersionKind().GroupKind() == crdGK:
			crds = append(crds, o.GetName())
		case o.GetKind() == "Deployment" && o.GetName() == forgeaiDeployment:
			operator = o
		}
	}
	for _, name := range forgeaiSpec.crds {
		if !slices.Contains(crds, name) {
			t.Errorf("chart lacks CRD %s; rendered %v", name, crds)
		}
	}
	if operator == nil {
		t.Fatalf("chart lacks Deployment %s", forgeaiDeployment)
	}
	if operator.GetNamespace() != ns {
		t.Errorf("operator namespace = %s, want %s", operator.GetNamespace(), ns)
	}
	labels := operator.GetLabels()
	if labels[depLabel] != "forgeai" || labels[depVersionLabel] != forgeaiChartVersion {
		t.Errorf("operator labels = %v, want %s=forgeai %s=%s", labels, depLabel, depVersionLabel, forgeaiChartVersion)
	}
	if labels[helmChartLabel] != "forgeai-"+forgeaiChartVersion {
		t.Errorf("operator %s = %q, want forgeai-%s", helmChartLabel, labels[helmChartLabel], forgeaiChartVersion)
	}

	containers, _, _ := unstructured.NestedSlice(operator.Object, "spec", "template", "spec", "containers")
	var env []any
	for _, c := range containers {
		envs, _, _ := unstructured.NestedSlice(c.(map[string]any), "env")
		env = append(env, envs...)
	}
	if !slices.ContainsFunc(env, func(e any) bool {
		m := e.(map[string]any)
		return m["name"] == "FORGEAI_NAMESPACE" && m["value"] == ns
	}) {
		t.Errorf("operator lacks FORGEAI_NAMESPACE=%s in its env %v", ns, env)
	}
}

// TestSSHGatewaySpec checks what the GUI tells the SSH gateway chart: the
// platform namespace its gateways are served in, and the Pipe CRD it needs.
func TestSSHGatewaySpec(t *testing.T) {
	if got := sshGatewaySpec.installNamespace(); got != sshGatewayNamespace {
		t.Errorf("installNamespace = %s, want %s", got, sshGatewayNamespace)
	}
	ns, _, _ := unstructured.NestedString(sshGatewaySpec.values(nil), "gateway", "namespace")
	if ns != platform.Namespace() {
		t.Errorf("gateway.namespace = %q, want %s", ns, platform.Namespace())
	}
	if len(sshGatewaySpec.needs) != 0 {
		t.Errorf("needs = %v, want empty (sshgateway v0.4.0 uses yaml plugin instead of Pipe CRD)", sshGatewaySpec.needs)
	}
}

// TestImagebuildSpec checks that the imagebuild chart is told the registry the
// platform config names: the chart's own default is empty, and a build against
// an empty registry fails.
func TestImagebuildSpec(t *testing.T) {
	cfg := &platform.Config{}
	cfg.Registry.Domain = "harbor.example.com:7443"
	cfg.Registry.Insecure = true
	values := imagebuildSpec.values(cfg)
	for key, want := range map[string]any{"registry": "harbor.example.com:7443", "registryInsecure": true} {
		got, _, _ := unstructured.NestedFieldNoCopy(values, "config", key)
		if got != want {
			t.Errorf("config.%s = %v, want %v", key, got, want)
		}
	}
	if !slices.Equal(imagebuildSpec.needs, []string{"projects.workspace.wutz.dev"}) {
		t.Errorf("needs = %v, want the project CRD", imagebuildSpec.needs)
	}
	// The chart names its Deployment after the chart, not <release>-controller-manager
	// like the other operators; a wrong name leaves the component degraded forever.
	if imagebuildSpec.deployment != "imagebuild" {
		t.Errorf("deployment = %q, want the chart's Deployment imagebuild", imagebuildSpec.deployment)
	}
}

// TestSSHPiperBundle checks that the sshpiper component is a CRD-only bundle
// of the Pipe CRD the SSH gateway needs.
func TestSSHPiperBundle(t *testing.T) {
	b, err := bundleByName("sshpiper")
	if err != nil || b == nil {
		t.Fatalf("bundleByName(sshpiper) = %v, %v", b, err)
	}
	if got := b.crdNames(); !slices.Equal(got, []string{"pipes.sshpiper.com"}) {
		t.Errorf("crdNames = %v, want the Pipe CRD only", got)
	}
	if bundleHasDeployment(b) {
		t.Error("sshpiper bundle declares a Deployment; it should be CRD-only")
	}
}

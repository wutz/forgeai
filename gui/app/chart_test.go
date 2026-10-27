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
	for _, name := range forgeaiCRDs {
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

// TestSSHOperatorSpec checks what the GUI tells the SSH operator chart: the
// platform namespace its gateways are served in, and the Pipe CRD it needs.
func TestSSHOperatorSpec(t *testing.T) {
	if got := sshOperatorSpec.installNamespace(); got != sshOperatorNamespace {
		t.Errorf("installNamespace = %s, want %s", got, sshOperatorNamespace)
	}
	ns, _, _ := unstructured.NestedString(sshOperatorSpec.values(nil), "platform", "namespace")
	if ns != platform.Namespace() {
		t.Errorf("platform.namespace = %q, want %s", ns, platform.Namespace())
	}
	if !slices.Contains(sshOperatorSpec.needs, "pipes.sshpiper.com") {
		t.Errorf("needs = %v, want the sshpiper Pipe CRD", sshOperatorSpec.needs)
	}
}

// TestSSHPiperBundle checks that the sshpiper component is a CRD-only bundle
// of the Pipe CRD the SSH operator needs.
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

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
	if chrt.Metadata.Version != forgeaiChart.version {
		t.Errorf("chart version = %s, want %s", chrt.Metadata.Version, forgeaiChart.version)
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
	ins.ReleaseName = forgeaiChart.release
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
		case o.GetKind() == "Deployment" && o.GetName() == forgeaiChart.deployment:
			operator = o
		}
	}
	for _, name := range forgeaiChart.crds {
		if !slices.Contains(crds, name) {
			t.Errorf("chart lacks CRD %s; rendered %v", name, crds)
		}
	}
	if operator == nil {
		t.Fatalf("chart lacks Deployment %s", forgeaiChart.deployment)
	}
	if operator.GetNamespace() != ns {
		t.Errorf("operator namespace = %s, want %s", operator.GetNamespace(), ns)
	}
	labels := operator.GetLabels()
	if labels[depLabel] != "forgeai" || labels[depVersionLabel] != forgeaiChart.version {
		t.Errorf("operator labels = %v, want %s=forgeai %s=%s", labels, depLabel, depVersionLabel, forgeaiChart.version)
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

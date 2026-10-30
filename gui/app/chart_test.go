package app

import (
	"context"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
// platform namespace its gateways are served in, and that it needs no CRD of
// another component (v0.4.0 serves gateways with sshpiper's yaml plugin).
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
	if !slices.Equal(imagebuildSpec.needs, []string{"projects.workspace.wutz.dev", seccompProfileCRD}) {
		t.Errorf("needs = %v, want the project and seccomp profile CRDs", imagebuildSpec.needs)
	}
	// The chart names its Deployment after the chart, not <release>-controller-manager
	// like the other operators; a wrong name leaves the component degraded forever.
	if imagebuildSpec.deployment != "imagebuild" {
		t.Errorf("deployment = %q, want the chart's Deployment imagebuild", imagebuildSpec.deployment)
	}
}

// TestImagebuildValues checks what the GUI tells the imagebuild chart: the
// Localhost seccomp profile it installs, and the mirrors and excluded nodes of
// the platform config, which are left out when the config has none.
func TestImagebuildValues(t *testing.T) {
	cfg := &platform.Config{}
	values := imagebuildValues(cfg)
	if got, _, _ := unstructured.NestedBool(values, "seccompProfile", "install"); !got {
		t.Errorf("seccompProfile.install = %v, want true", got)
	}
	if got, _, _ := unstructured.NestedString(values, "config", "seccompProfile"); got != imagebuildSeccompProfile {
		t.Errorf("config.seccompProfile = %q, want %q", got, imagebuildSeccompProfile)
	}
	for _, key := range []string{"registryMirrors", "excludeNodes"} {
		if _, ok, _ := unstructured.NestedFieldNoCopy(values, "config", key); ok {
			t.Errorf("config.%s is set without a platform config for it", key)
		}
	}

	cfg.Build.RegistryMirrors = map[string]string{"docker.io": "mir.example.com/docker.io"}
	cfg.Build.ExcludeNodes = []string{"gn-1"}
	values = imagebuildValues(cfg)
	mirrors, _, _ := unstructured.NestedFieldNoCopy(values, "config", "registryMirrors")
	if !reflect.DeepEqual(mirrors, map[string]any{"docker.io": "mir.example.com/docker.io"}) {
		t.Errorf("config.registryMirrors = %v", mirrors)
	}
	nodes, _, _ := unstructured.NestedFieldNoCopy(values, "config", "excludeNodes")
	if !reflect.DeepEqual(nodes, []any{"gn-1"}) {
		t.Errorf("config.excludeNodes = %v", nodes)
	}
}

// TestSecurityProfilesOperatorSpec checks how the GUI installs the Security
// Profiles Operator: from the release archive it is pinned to, into a namespace
// that admits its privileged daemon, and without waiting for the daemon.
func TestSecurityProfilesOperatorSpec(t *testing.T) {
	if spoSpec.installNamespace() != spoNamespace {
		t.Errorf("installNamespace = %s, want %s", spoSpec.installNamespace(), spoNamespace)
	}
	if spoSpec.namespaceLabels["pod-security.kubernetes.io/enforce"] != "privileged" {
		t.Errorf("namespace labels = %v, want the privileged enforce level", spoSpec.namespaceLabels)
	}
	if !spoSpec.noWait {
		t.Error("noWait = false: one node whose daemon cannot start would fail the release")
	}
	if spoSpec.sha256 == "" || spoSpec.ref != spoChart {
		t.Errorf("chart %s is not pinned by digest", spoSpec.ref)
	}
	if !slices.Contains(spoSpec.crds, seccompProfileCRD) {
		t.Errorf("crds = %v, want %s", spoSpec.crds, seccompProfileCRD)
	}
	if !slices.Contains(spoSpec.clusterCRDs, seccompProfileCRD) {
		t.Errorf("clusterCRDs = %v, want %s", spoSpec.clusterCRDs, seccompProfileCRD)
	}
	// The catalog installs the operator before the imagebuild chart that needs its CRD.
	var order []string
	for _, d := range depCatalog {
		order = append(order, d.name)
	}
	if slices.Index(order, "security-profiles-operator") > slices.Index(order, "imagebuild") {
		t.Errorf("catalog order = %v, want the Security Profiles Operator before imagebuild", order)
	}
}

// TestCheckDigest checks that a chart archive is accepted only when it hashes to
// the digest its spec pins.
func TestCheckDigest(t *testing.T) {
	const digest = "cc57fc1903e444cf6a726490b43b27ee9f87facc037f86872201847c565b45fb" // sha256 of "chart"
	if err := checkDigest([]byte("chart"), digest); err != nil {
		t.Errorf("matching digest: %v", err)
	}
	if err := checkDigest([]byte("chart2"), digest); err == nil {
		t.Error("a digest that does not match was accepted")
	}
}

// TestEnsureNamespaceLabels checks that the namespace the chart installs into
// carries the labels the chart needs, without dropping the labels it has.
func TestEnsureNamespaceLabels(t *testing.T) {
	ctx := context.Background()
	k8s := fake.NewClientBuilder().WithObjects(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "existing", Labels: map[string]string{"team": "gpu"},
	}}).Build()
	labels := map[string]string{"pod-security.kubernetes.io/enforce": "privileged"}
	for _, name := range []string{"created", "existing"} {
		if err := ensureNamespaceLabels(ctx, k8s, name, labels); err != nil {
			t.Fatalf("ensureNamespaceLabels(%s): %v", name, err)
		}
		ns := &corev1.Namespace{}
		if err := k8s.Get(ctx, client.ObjectKey{Name: name}, ns); err != nil {
			t.Fatal(err)
		}
		if ns.Labels["pod-security.kubernetes.io/enforce"] != "privileged" {
			t.Errorf("%s labels = %v, want the enforce level", name, ns.Labels)
		}
	}
	ns := &corev1.Namespace{}
	if err := k8s.Get(ctx, client.ObjectKey{Name: "existing"}, ns); err != nil {
		t.Fatal(err)
	}
	if ns.Labels["team"] != "gpu" {
		t.Errorf("existing labels = %v, want team=gpu kept", ns.Labels)
	}
}

// TestRecreateScopedCRDs checks the namespaced CRD an older chart left behind is
// replaced by the cluster-scoped one, and that a CRD still holding objects is
// refused rather than deleted with them.
func TestRecreateScopedCRDs(t *testing.T) {
	const crd = "seccompprofiles.security-profiles-operator.x-k8s.io"
	namespaced := func(name string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinition",
			"metadata": map[string]any{"name": name},
			"spec": map[string]any{
				"group": "security-profiles-operator.x-k8s.io", "scope": "Namespaced",
				"names":    map[string]any{"kind": "SeccompProfile", "plural": "seccompprofiles"},
				"versions": []any{map[string]any{"name": "v1beta1", "served": true, "storage": true}},
			},
		}}
		return u
	}
	profile := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "security-profiles-operator.x-k8s.io/v1beta1", "kind": "SeccompProfile",
		"metadata": map[string]any{"name": "old", "namespace": "ns"},
	}}
	spec := &chartSpec{name: "security-profiles-operator", clusterCRDs: []string{crd}}

	t.Run("no objects", func(t *testing.T) {
		k8s := fake.NewClientBuilder().WithObjects(namespaced(crd)).Build()
		if err := recreateScopedCRDs(context.Background(), k8s, spec); err != nil {
			t.Fatalf("recreateScopedCRDs: %v", err)
		}
		err := k8s.Get(context.Background(), client.ObjectKey{Name: crd}, namespaced(crd))
		if !apierrors.IsNotFound(err) {
			t.Errorf("CRD after recreate: %v, want it deleted", err)
		}
	})
	t.Run("holds objects", func(t *testing.T) {
		k8s := fake.NewClientBuilder().WithObjects(namespaced(crd), profile).Build()
		err := recreateScopedCRDs(context.Background(), k8s, spec)
		if err == nil || !strings.Contains(err.Error(), "holds SeccompProfile objects") {
			t.Errorf("recreateScopedCRDs = %v, want a refusal naming the objects", err)
		}
		if err := k8s.Get(context.Background(), client.ObjectKey{Name: crd}, namespaced(crd)); err != nil {
			t.Errorf("the CRD was deleted: %v", err)
		}
	})
}

// TestRunRelease checks that the error of the install, and of the upgrade of the
// release it made, reaches the caller. A chart whose template fails to render
// fails both, against Helm's in-memory storage.
func TestRunRelease(t *testing.T) {
	cfg := &action.Configuration{
		Releases:     storage.Init(driver.NewMemory()),
		KubeClient:   &kubefake.PrintingKubeClient{Out: io.Discard},
		Capabilities: chartutil.DefaultCapabilities,
		Log:          func(string, ...any) {},
	}
	spec := &chartSpec{name: "test", release: "spo", namespace: "ns", noWait: true}
	ok := &chart.Chart{Metadata: &chart.Metadata{APIVersion: "v2", Name: "test", Version: "1.0.0"}}
	broken := &chart.Chart{
		Metadata:  &chart.Metadata{APIVersion: "v2", Name: "test", Version: "1.0.0"},
		Templates: []*chart.File{{Name: "templates/fail.yaml", Data: []byte(`{{ fail "boom" }}`)}},
	}
	up := action.NewUpgrade(cfg)
	up.Namespace = spec.namespace
	ctx := context.Background()

	if err := runRelease(ctx, cfg, up, spec, broken, nil); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("install of a broken chart: err = %v, want the render error", err)
	}
	if _, err := cfg.Releases.Last(spec.release); !errors.Is(err, driver.ErrReleaseNotFound) {
		t.Fatalf("failed install left a release: %v", err)
	}
	if err := runRelease(ctx, cfg, up, spec, ok, nil); err != nil {
		t.Fatalf("install of a valid chart: %v", err)
	}
	if err := runRelease(ctx, cfg, up, spec, broken, nil); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("upgrade to a broken chart: err = %v, want the render error", err)
	}
}

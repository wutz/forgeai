package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wutz/forgeai/pkg/platform"
)

// chartSpec is a component installed from a Helm chart: where the chart is, the
// version the GUI pins, and what the cluster must hold for the install.
type chartSpec struct {
	// name is the chart name, which Helm stamps on each object as helm.sh/chart.
	name    string
	ref     string
	version string
	release string
	// namespace is where the chart is installed; empty for the platform namespace.
	namespace  string
	deployment string
	crds       []string
	// needs are CRDs of other components the chart's controller requires.
	needs []string
	// values returns the chart values of the install, nil for the chart's defaults.
	values func() map[string]any
}

// installNamespace is the namespace the chart is installed into.
func (s *chartSpec) installNamespace() string {
	if s.namespace != "" {
		return s.namespace
	}
	return platform.Namespace()
}

// The forgeai operator is installed from its Helm chart. forgeaiChartVersion
// is the chart version the GUI pins; the chart's app version is the operator
// image tag, so a release bumps both together.
const (
	forgeaiChart        = "oci://ghcr.io/wutz/charts/forgeai"
	forgeaiChartVersion = "0.4.0"
	forgeaiRelease      = "forgeai"
)

var forgeaiSpec = &chartSpec{
	name: "forgeai", ref: forgeaiChart, version: forgeaiChartVersion, release: forgeaiRelease,
	deployment: forgeaiDeployment, crds: forgeaiCRDs,
}

// The SSH operator is installed from its own Helm chart into its namespace. Its
// SSHGateways are served in the platform namespace, which the chart is told, and
// it needs the Pipe CRD of sshpiper, a bundled component installed first.
const (
	sshOperatorChart        = "oci://ghcr.io/wutz/charts/ssh-operator"
	sshOperatorChartVersion = "0.1.0"
	sshOperatorRelease      = "ssh-operator"
	sshOperatorNamespace    = "ssh-operator-system"
	sshOperatorDeployment   = "ssh-operator-controller-manager"
)

var sshOperatorSpec = &chartSpec{
	name: "ssh-operator", ref: sshOperatorChart, version: sshOperatorChartVersion, release: sshOperatorRelease,
	namespace: sshOperatorNamespace, deployment: sshOperatorDeployment,
	crds:  []string{"sshgatewayclasses.forgeai.wutz.dev", "sshgateways.forgeai.wutz.dev", "sshroutes.forgeai.wutz.dev"},
	needs: []string{"pipes.sshpiper.com"},
	values: func() map[string]any {
		return map[string]any{"platform": map[string]any{"namespace": platform.Namespace()}}
	},
}

// The Project operator is installed from its own Helm chart into the platform
// namespace. It reads the platform config that the forgeai chart installs, so
// it installs second.
const (
	projectOperatorChart        = "oci://ghcr.io/wutz/charts/project-operator"
	projectOperatorChartVersion = "0.4.1"
	projectOperatorRelease      = "project-operator"
	projectOperatorDeployment   = "project-operator-controller-manager"
)

var projectOperatorSpec = &chartSpec{
	name: "project-operator", ref: projectOperatorChart, version: projectOperatorChartVersion, release: projectOperatorRelease,
	deployment: projectOperatorDeployment, crds: []string{"projects.forgeai.wutz.dev"},
}

// installChart installs the chart of spec into its namespace, or upgrades the
// release already there. The chart is pulled from the OCI registry at the
// pinned version and waited on until its Deployment is up.
func installChart(ctx context.Context, c *Connection, spec *chartSpec, progress func(string)) error {
	ns := spec.installNamespace()
	progress(stepChartNamespace)
	if err := ensureNamespace(ctx, c.k8s, ns); err != nil {
		return err
	}
	registryClient, err := registry.NewClient(registry.ClientOptWriter(io.Discard))
	if err != nil {
		return err
	}
	cfg := new(action.Configuration)
	if err := cfg.Init(restGetter{rest: c.rest, namespace: ns}, ns, "secret", func(string, ...any) {}); err != nil {
		return err
	}
	cfg.RegistryClient = registryClient
	up := action.NewUpgrade(cfg)
	up.Install = true
	up.Namespace = ns
	up.Version = spec.version
	up.Wait = true
	up.Atomic = true
	up.Timeout = deploymentReadyTimeout

	progress(stepChartPull)
	path, err := up.ChartPathOptions.LocateChart(spec.ref, cli.New())
	if err != nil {
		return err
	}
	chrt, err := loader.Load(path)
	if err != nil {
		return err
	}
	var values map[string]any
	if spec.values != nil {
		values = spec.values()
	}
	progress(stepChartInstall)
	_, err = up.RunWithContext(ctx, spec.release, chrt, values)
	return err
}

// checkChartTarget refuses an install that the cluster cannot take: a needed
// CRD that is missing, or an operator the chart did not install. Helm does not
// adopt objects it did not create.
func checkChartTarget(ctx context.Context, k8s client.Client, spec *chartSpec) error {
	for _, crd := range spec.needs {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(crdGVK)
		err := k8s.Get(ctx, client.ObjectKey{Name: crd}, u)
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("%s needs the %s CRD; install the component that provides it first", spec.name, crd)
		}
		if err != nil {
			return err
		}
	}
	d := &appsv1.Deployment{}
	err := k8s.Get(ctx, client.ObjectKey{Namespace: spec.installNamespace(), Name: spec.deployment}, d)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if chartLabelVersion(d.Labels, spec.name) == "" {
		return fmt.Errorf("%s is deployed outside Helm; remove it before installing the chart", spec.deployment)
	}
	return nil
}

// chartLabelVersion returns the version of the chart named chartName that
// installed an object, from its helm.sh/chart label. It is empty when another
// chart, or none, installed the object.
func chartLabelVersion(labels map[string]string, chartName string) string {
	v, ok := strings.CutPrefix(labels[helmChartLabel], chartName+"-")
	if !ok {
		return ""
	}
	return v
}

// ensureNamespace creates the namespace unless it exists.
func ensureNamespace(ctx context.Context, k8s client.Client, name string) error {
	err := k8s.Get(ctx, client.ObjectKey{Name: name}, &corev1.Namespace{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	err = k8s.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// restGetter serves the connection's REST config to Helm, which takes its
// clients from a RESTClientGetter rather than from a kubeconfig file.
type restGetter struct {
	rest      *rest.Config
	namespace string
}

func (g restGetter) ToRESTConfig() (*rest.Config, error) {
	return rest.CopyConfig(g.rest), nil
}

func (g restGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(g.rest)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(dc), nil
}

func (g restGetter) ToRESTMapper() (meta.RESTMapper, error) {
	dc, err := g.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	return restmapper.NewDeferredDiscoveryRESTMapper(dc), nil
}

func (g restGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return clientcmd.NewDefaultClientConfig(*clientcmdapi.NewConfig(), &clientcmd.ConfigOverrides{
		Context: clientcmdapi.Context{Namespace: g.namespace},
	})
}

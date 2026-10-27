package app

import (
	"context"
	"fmt"
	"io"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wutz/forgeai/pkg/platform"
)

// The operators installed from a Helm chart. Each chart version is pinned
// here; the chart's app version is the operator image tag, so a release bumps
// both together.
const (
	forgeaiChart        = "oci://ghcr.io/wutz/charts/forgeai"
	forgeaiChartVersion = "0.5.0"
	forgeaiRelease      = "forgeai"

	fileServerChart        = "oci://ghcr.io/wutz/charts/fileserver-operator"
	fileServerChartVersion = "0.1.0"
	fileServerRelease      = "fileserver-operator"
	// fileServerDeployment is the operator Deployment: the chart names it after the release.
	fileServerDeployment = fileServerRelease
)

// chartSpec describes a component installed from a Helm chart into the
// platform namespace.
type chartSpec struct {
	// ref is the OCI reference of the chart, installed at version.
	ref, version string
	// release is the Helm release the chart is installed as.
	release string
	// deployment is the operator Deployment in the platform namespace.
	deployment string
	// crds are the CRDs the chart installs.
	crds []string
	// ownerLabel is set on the operator Deployment of an install from the chart.
	ownerLabel string
	// versionLabel carries the chart version on the operator Deployment.
	versionLabel string
	// values renders the chart values from the platform config; nil when the
	// chart takes none.
	values func(cfg *platform.Config) map[string]any
}

var (
	forgeaiChartSpec = &chartSpec{
		ref: forgeaiChart, version: forgeaiChartVersion, release: forgeaiRelease,
		deployment: forgeaiDeployment, crds: forgeaiCRDs,
		ownerLabel: depLabel, versionLabel: depVersionLabel,
	}
	fileServerChartSpec = &chartSpec{
		ref: fileServerChart, version: fileServerChartVersion, release: fileServerRelease,
		deployment: fileServerDeployment, crds: []string{"fileservers.forgeai.wutz.dev"},
		// The chart labels its objects with the standard Helm and app labels.
		ownerLabel: "helm.sh/chart", versionLabel: "app.kubernetes.io/version",
		values: fileServerValues,
	}
)

// fileServerValues maps the platform config onto the operator config the
// chart renders into its ConfigMap. The file servers are published under the
// platform ingress, so the operator reads the same domain and class.
func fileServerValues(cfg *platform.Config) map[string]any {
	return map[string]any{"config": map[string]any{
		"ingress": map[string]any{
			"className": cfg.Ingress.ClassName, "domain": cfg.Ingress.Domain,
			"port": cfg.Ingress.Port, "tlsSecret": cfg.Ingress.TLSSecret,
		},
		"fileServer": map[string]any{
			"image":       cfg.FileServer.Image,
			"idleTimeout": cfg.FileServer.IdleTimeout.Duration.String(),
			"hostPrefix":  cfg.FileServer.HostPrefix,
		},
	}}
}

// installChart installs the chart of spec into the platform namespace, or
// upgrades the release already there. The chart is pulled from the OCI
// registry at spec.version and waited on until its Deployment is up.
func installChart(ctx context.Context, c *Connection, spec *chartSpec, cfg *platform.Config, progress func(string)) error {
	ns := platform.Namespace()
	progress(stepChartNamespace)
	if err := ensureNamespace(ctx, c.k8s, ns); err != nil {
		return err
	}
	registryClient, err := registry.NewClient(registry.ClientOptWriter(io.Discard))
	if err != nil {
		return err
	}
	acfg := new(action.Configuration)
	if err := acfg.Init(restGetter{rest: c.rest, namespace: ns}, ns, "secret", func(string, ...any) {}); err != nil {
		return err
	}
	acfg.RegistryClient = registryClient
	up := action.NewUpgrade(acfg)
	up.Install = true
	up.Namespace = ns
	up.Version = spec.version
	up.Wait = true
	up.Atomic = true
	up.Timeout = deploymentReadyTimeout

	var values map[string]any
	if spec.values != nil {
		values = spec.values(cfg)
	}
	progress(stepChartPull)
	path, err := up.ChartPathOptions.LocateChart(spec.ref, cli.New())
	if err != nil {
		return err
	}
	chrt, err := loader.Load(path)
	if err != nil {
		return err
	}
	progress(stepChartInstall)
	_, err = up.RunWithContext(ctx, spec.release, chrt, values)
	return err
}

// checkChartTarget refuses to install the chart over an operator that was not
// installed from it: Helm does not adopt objects it did not create.
func checkChartTarget(ctx context.Context, k8s client.Client, spec *chartSpec) error {
	d := &appsv1.Deployment{}
	err := k8s.Get(ctx, client.ObjectKey{Namespace: platform.Namespace(), Name: spec.deployment}, d)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, ok := d.Labels[spec.ownerLabel]; !ok {
		return fmt.Errorf("the %s operator was deployed outside Helm; remove it before installing the chart", spec.release)
	}
	return nil
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

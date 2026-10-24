package app

import (
	"context"
	"errors"
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

// The forgeai operator is installed from its Helm chart. forgeaiChartVersion
// is the chart version the GUI pins; the chart's app version is the operator
// image tag, so a release bumps both together.
const (
	forgeaiChart        = "oci://ghcr.io/wutz/charts/forgeai"
	forgeaiChartVersion = "0.4.0"
	forgeaiRelease      = "forgeai"
)

// installChart installs the forgeai chart into the platform namespace, or
// upgrades the release already there. The chart is pulled from the OCI
// registry at forgeaiChartVersion and waited on until its Deployment is up.
func installChart(ctx context.Context, c *Connection, progress func(string)) error {
	ns := platform.Namespace()
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
	up.Version = forgeaiChartVersion
	up.Wait = true
	up.Atomic = true
	up.Timeout = deploymentReadyTimeout

	progress(stepChartPull)
	path, err := up.ChartPathOptions.LocateChart(forgeaiChart, cli.New())
	if err != nil {
		return err
	}
	chrt, err := loader.Load(path)
	if err != nil {
		return err
	}
	progress(stepChartInstall)
	_, err = up.RunWithContext(ctx, forgeaiRelease, chrt, nil)
	return err
}

// checkChartTarget refuses to install the chart over an operator that was not
// installed from it: Helm does not adopt objects it did not create.
func checkChartTarget(ctx context.Context, k8s client.Client) error {
	d := &appsv1.Deployment{}
	err := k8s.Get(ctx, client.ObjectKey{Namespace: platform.Namespace(), Name: forgeaiDeployment}, d)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, ok := d.Labels[depLabel]; !ok {
		return errors.New("the forgeai operator was deployed outside Helm (make deploy-dev); " +
			"remove it before installing the chart")
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

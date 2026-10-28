package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/storage/driver"
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
	values func(platformCfg *platform.Config) map[string]any
}

// Helm bookkeeping on the objects a release owns.
const (
	helmReleaseNameKey      = "meta.helm.sh/release-name"
	helmReleaseNamespaceKey = "meta.helm.sh/release-namespace"
	helmManagedByKey        = "app.kubernetes.io/managed-by"
)

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
	forgeaiChartVersion = "0.5.0"
	forgeaiRelease      = "forgeai"
)

var forgeaiSpec = &chartSpec{
	name: "forgeai", ref: forgeaiChart, version: forgeaiChartVersion, release: forgeaiRelease,
	deployment: forgeaiDeployment, crds: forgeaiCRDs,
}

// The SSH gateway operator is installed from its own Helm chart into its
// namespace. Its SSHGateways are served in the platform namespace, which the
// chart is told, and it needs the Pipe CRD of sshpiper, a bundled component
// installed first.
const (
	sshGatewayChart        = "oci://ghcr.io/wutz/charts/sshgateway"
	sshGatewayChartVersion = "0.2.0"
	sshGatewayRelease      = "sshgateway"
	sshGatewayNamespace    = "sshgateway-system"
	sshGatewayDeployment   = "sshgateway-controller-manager"
)

var sshGatewaySpec = &chartSpec{
	name: "sshgateway", ref: sshGatewayChart, version: sshGatewayChartVersion, release: sshGatewayRelease,
	namespace: sshGatewayNamespace, deployment: sshGatewayDeployment,
	crds:  []string{"sshgatewayclasses.sshgateway.wutz.dev", "sshgateways.sshgateway.wutz.dev", "sshroutes.sshgateway.wutz.dev"},
	needs: []string{"pipes.sshpiper.com"},
	values: func(*platform.Config) map[string]any {
		return map[string]any{"gateway": map[string]any{"namespace": platform.Namespace()}}
	},
}

// The FileServer operator is installed from its own Helm chart into the platform
// namespace. Its file servers are published under the platform ingress, so the
// chart takes the platform's ingress and file server settings. Its CRD is in the
// fileserver.wutz.dev group, which forgeai never shipped, so there is nothing to
// adopt; the 0.1.0 CRD in the forgeai.wutz.dev group is removed by the upgrade.
const (
	fileServerChart        = "oci://ghcr.io/wutz/charts/fileserver-operator"
	fileServerChartVersion = "0.2.0"
	fileServerRelease      = "fileserver-operator"
	fileServerDeployment   = "fileserver-operator"
)

var fileServerSpec = &chartSpec{
	name: "fileserver-operator", ref: fileServerChart, version: fileServerChartVersion,
	release: fileServerRelease, deployment: fileServerDeployment,
	crds:   []string{"fileservers.fileserver.wutz.dev"},
	values: fileServerValues,
}

// fileServerValues maps the platform config onto the operator config the chart
// renders into its ConfigMap.
func fileServerValues(platformCfg *platform.Config) map[string]any {
	return map[string]any{"config": map[string]any{
		"ingress": map[string]any{
			"className": platformCfg.Ingress.ClassName, "domain": platformCfg.Ingress.Domain,
			"port": platformCfg.Ingress.Port, "tlsSecret": platformCfg.Ingress.TLSSecret,
		},
		"fileServer": map[string]any{
			"image":       platformCfg.FileServer.Image,
			"idleTimeout": platformCfg.FileServer.IdleTimeout.Duration.String(),
			"hostPrefix":  platformCfg.FileServer.HostPrefix,
		},
	}}
}

// The project operator is installed from its own Helm chart into the platform
// namespace. It owns the Project and InstanceType CRDs, and the InstanceTypes
// that forgeai reads come from it, so it installs before forgeai. Its platform
// config ConfigMap is rendered by the forgeai chart; until that is installed the
// operator waits for it.
const (
	projectChart        = "oci://ghcr.io/wutz/charts/project"
	projectChartVersion = "0.5.0"
	projectRelease      = "project"
	projectDeployment   = "project-controller-manager"
)

var projectSpec = &chartSpec{
	name: "project", ref: projectChart, version: projectChartVersion, release: projectRelease,
	deployment: projectDeployment, crds: []string{"projects.project.wutz.dev", "instancetypes.project.wutz.dev"},
}

// installChart installs the chart of spec into its namespace, or upgrades the
// release already there. The chart is pulled from the OCI registry at the
// pinned version and waited on until its Deployment is up.
func installChart(ctx context.Context, c *Connection, spec *chartSpec, platformCfg *platform.Config, progress func(string)) error {
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
		values = spec.values(platformCfg)
	}
	progress(stepChartInstall)
	// Upgrade refuses a release that does not exist and does not install one by
	// itself, so a first install takes the Install action.
	if _, err := cfg.Releases.Last(spec.release); errors.Is(err, driver.ErrReleaseNotFound) {
		ins := action.NewInstall(cfg)
		ins.ReleaseName = spec.release
		ins.Namespace = ns
		ins.Wait = true
		ins.Atomic = true
		ins.Timeout = deploymentReadyTimeout
		_, err = ins.RunWithContext(ctx, chrt, values)
		return err
	}
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

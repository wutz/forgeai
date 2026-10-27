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

// helmChart is a component installed from a Helm chart rather than from an
// embedded bundle. Every chart installs into the platform namespace, where the
// operators read the platform config.
type helmChart struct {
	// name is the chart name, the prefix of the helm.sh/chart label its
	// objects carry: "<name>-<version>".
	name string
	// ref is the OCI reference the chart is pulled from.
	ref string
	// version is the chart version the GUI pins. For a chart whose app version
	// is an operator image tag, a release bumps both together.
	version string
	// release is the Helm release the chart is installed as.
	release string
	// deployment is the controller Deployment the chart runs.
	deployment string
	// crds are the CRDs the chart installs; the Dependencies view counts them.
	crds []string
	// ownerLabel must be set on an existing controller Deployment for the GUI to
	// upgrade it: a Deployment without it was not installed from the chart.
	ownerLabel string
	// adoptFrom is a release whose chart shipped crds before they moved to this
	// chart. Helm refuses to install objects another release owns, so the
	// install hands them over first (see adoptCRDs).
	adoptFrom string
}

// Helm bookkeeping on the objects a release owns.
const (
	helmReleaseNameKey      = "meta.helm.sh/release-name"
	helmReleaseNamespaceKey = "meta.helm.sh/release-namespace"
	helmManagedByKey        = "app.kubernetes.io/managed-by"
	helmChartLabel          = "helm.sh/chart"
)

const (
	forgeaiChartVersion = "0.4.0"
	forgeaiRelease      = "forgeai"

	instanceTypeChartVersion = "0.1.0"
	instanceTypeRelease      = "forgeai-instancetype-operator"
	// instanceTypeDeployment is the operator Deployment: the chart prefixes its
	// name with the release.
	instanceTypeDeployment = "forgeai-instancetype-operator-controller-manager"
)

// forgeaiOperatorChart is the forgeai operator. It no longer ships the
// InstanceType CRD, which the InstanceType operator's chart installs.
var forgeaiOperatorChart = helmChart{
	name:       "forgeai",
	ref:        "oci://ghcr.io/wutz/charts/forgeai",
	version:    forgeaiChartVersion,
	release:    forgeaiRelease,
	deployment: forgeaiDeployment,
	crds:       forgeaiCRDs,
	ownerLabel: depLabel,
}

// instanceTypeOperatorChart is the InstanceType operator, published from
// github.com/wutz/forgeai-instancetype-operator. It takes over the
// InstanceType CRD from the forgeai release that shipped it.
var instanceTypeOperatorChart = helmChart{
	name:       "forgeai-instancetype-operator",
	ref:        "oci://ghcr.io/wutz/charts/forgeai-instancetype-operator",
	version:    instanceTypeChartVersion,
	release:    instanceTypeRelease,
	deployment: instanceTypeDeployment,
	crds:       []string{"instancetypes.forgeai.wutz.dev"},
	ownerLabel: helmManagedByKey,
	adoptFrom:  forgeaiRelease,
}

// installChart installs the chart into the platform namespace, or upgrades the
// release already there. The chart is pulled from the OCI registry at its
// pinned version and waited on until its Deployment is up.
func installChart(ctx context.Context, c *Connection, ch helmChart, progress func(string)) error {
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
	up.Version = ch.version
	up.Wait = true
	up.Atomic = true
	up.Timeout = deploymentReadyTimeout

	progress(stepChartPull)
	path, err := up.ChartPathOptions.LocateChart(ch.ref, cli.New())
	if err != nil {
		return err
	}
	chrt, err := loader.Load(path)
	if err != nil {
		return err
	}
	progress(stepChartInstall)
	if err := adoptCRDs(ctx, c.k8s, ch, ns); err != nil {
		return err
	}
	_, err = up.RunWithContext(ctx, ch.release, chrt, nil)
	return err
}

// adoptCRDs hands the CRDs of ch.adoptFrom over to ch. A chart keeps its CRDs
// when its release is uninstalled (helm.sh/resource-policy: keep), so after the
// old release drops them they still carry its ownership. Helm would refuse to
// install them for ch; re-labelling them lets the install take them over.
func adoptCRDs(ctx context.Context, k8s client.Client, ch helmChart, ns string) error {
	if ch.adoptFrom == "" {
		return nil
	}
	for _, name := range ch.crds {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(crdGVK)
		err := k8s.Get(ctx, client.ObjectKey{Name: name}, u)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		if u.GetAnnotations()[helmReleaseNameKey] != ch.adoptFrom {
			continue
		}
		annotations := u.GetAnnotations()
		annotations[helmReleaseNameKey] = ch.release
		annotations[helmReleaseNamespaceKey] = ns
		u.SetAnnotations(annotations)
		labels := u.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[helmManagedByKey] = "Helm"
		u.SetLabels(labels)
		if err := k8s.Update(ctx, u); err != nil {
			return err
		}
	}
	return nil
}

// checkChartTarget refuses to install the chart over an operator that was not
// installed from it: Helm does not adopt objects it did not create.
func checkChartTarget(ctx context.Context, k8s client.Client, ch helmChart) error {
	d := &appsv1.Deployment{}
	err := k8s.Get(ctx, client.ObjectKey{Namespace: platform.Namespace(), Name: ch.deployment}, d)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, ok := d.Labels[ch.ownerLabel]; !ok {
		return fmt.Errorf("the %s operator was deployed outside Helm; remove it before installing the chart", ch.release)
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

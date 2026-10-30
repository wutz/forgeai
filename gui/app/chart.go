package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/volumes"
)

// chartSpec is a component installed from a Helm chart: where the chart is, the
// version the GUI pins, and what the cluster must hold for the install.
type chartSpec struct {
	// name is the chart name, which Helm stamps on each object as helm.sh/chart.
	name string
	// ref is an oci:// chart reference, or the https:// URL of a chart archive
	// when the chart is published only as a release asset.
	ref string
	// sha256 is the hex digest the archive at an https:// ref must have. Unset
	// for an oci:// ref, which Helm pulls by tag.
	sha256  string
	version string
	release string
	// namespace is where the chart is installed; empty for the platform namespace.
	namespace string
	// namespaceLabels are set on the install namespace, created or not by the GUI.
	namespaceLabels map[string]string
	deployment      string
	crds            []string
	// clusterCRDs are the CRDs the chart ships cluster-scoped. A CRD's scope cannot
	// change in place, so an older namespaced one is deleted first, when it holds
	// no objects (see recreateScopedCRDs).
	clusterCRDs []string
	// noWait installs the release without waiting for its workloads, and without
	// rolling it back when they do not come up, then waits only for its CRDs. For a
	// chart whose DaemonSet cannot start on every node, which would otherwise fail
	// the release; the Dependencies view reports the state of its Deployment.
	noWait bool
	// needs are CRDs of other components the chart's controller requires.
	needs []string
	// values returns the chart values of the install, nil for the chart's defaults.
	values func(platformCfg *platform.Config) map[string]any
	// legacy is the release an earlier version of this component was installed
	// as, when this chart cannot take it over. It is uninstalled first (see
	// replaceLegacy).
	legacy string
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
	forgeaiChartVersion = "0.6.2"
	forgeaiRelease      = "forgeai"
)

var forgeaiSpec = &chartSpec{
	name: "forgeai", ref: forgeaiChart, version: forgeaiChartVersion, release: forgeaiRelease,
	deployment: forgeaiDeployment, crds: forgeaiCRDs,
}

// The SSH gateway operator is installed from its own Helm chart into its
// namespace. Its SSHGateways are served in the platform namespace, which the
// chart is told.
const (
	sshGatewayChart        = "oci://ghcr.io/wutz/charts/sshgateway"
	sshGatewayChartVersion = "0.4.0"
	sshGatewayRelease      = "sshgateway"
	sshGatewayNamespace    = "sshgateway-system"
	sshGatewayDeployment   = "sshgateway-controller-manager"
)

var sshGatewaySpec = &chartSpec{
	name: "sshgateway", ref: sshGatewayChart, version: sshGatewayChartVersion, release: sshGatewayRelease,
	namespace: sshGatewayNamespace, deployment: sshGatewayDeployment,
	crds:  []string{"sshgatewayclasses.sshgateway.wutz.dev", "sshgateways.sshgateway.wutz.dev", "sshroutes.sshgateway.wutz.dev"},
	needs: []string{},
	values: func(*platform.Config) map[string]any {
		return map[string]any{"gateway": map[string]any{"namespace": platform.Namespace()}}
	},
}

// The FileServer operator is installed from its own Helm chart into the platform
// namespace. Its file servers are published under the platform ingress, so the
// chart takes the platform's ingress and file server settings. Its CRD is in the
// fileserver.wutz.dev group, which forgeai never shipped, so there is nothing to
// adopt. Before 0.3.0 the chart was fileserver-operator, whose release owns the
// CRD, so that release is uninstalled first.
const (
	fileServerChart         = "oci://ghcr.io/wutz/charts/fileserver"
	fileServerChartVersion  = "0.3.0"
	fileServerRelease       = "fileserver"
	fileServerLegacyRelease = "fileserver-operator"
	fileServerDeployment    = "fileserver"
)

var fileServerSpec = &chartSpec{
	name: "fileserver", ref: fileServerChart, version: fileServerChartVersion,
	release: fileServerRelease, deployment: fileServerDeployment,
	crds:   []string{"fileservers.fileserver.wutz.dev"},
	legacy: fileServerLegacyRelease,
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

// The workspace operator is installed from its own Helm chart into the platform
// namespace. It owns the Project and InstanceType CRDs, and the InstanceTypes
// that forgeai reads come from it, so it installs before forgeai. Its platform
// config ConfigMap is rendered by the forgeai chart; until that is installed the
// operator waits for it.
const (
	projectChart        = "oci://ghcr.io/wutz/charts/workspace"
	projectChartVersion = "0.5.2"
	projectRelease      = "workspace"
	projectDeployment   = "workspace-controller-manager"
)

var projectSpec = &chartSpec{
	name: "workspace", ref: projectChart, version: projectChartVersion, release: projectRelease,
	deployment: projectDeployment, crds: []string{"projects.workspace.wutz.dev", "instancetypes.workspace.wutz.dev"},
}

// The Security Profiles Operator installs the seccomp profiles of the nodes,
// which the imagebuild builds run with. Its chart is a release asset of its
// GitHub release, not an OCI chart, so it is pulled from that URL and checked
// against the digest of the archive. It installs into its own namespace, which
// needs the privileged Pod Security level: the operator runs a daemon on every
// node that writes the profiles into the kubelet directory.
//
// The release is not waited on: its DaemonSet cannot start on every node (a node
// whose pod sandbox fails keeps its daemon pod in Init), and that must not fail
// the release. The Deployment is what the Dependencies view reports.
const (
	spoChart        = "https://github.com/kubernetes-sigs/security-profiles-operator/releases/download/v1.1.2/security-profiles-operator-1.1.2.tgz"
	spoChartSHA256  = "e14044e7bf25378967ee1feb64ba4260448879383ed31a7e2c4de5dfd8c6e623"
	spoChartVersion = "1.1.2"
	// The release is named spo, as the operator was first installed by hand on
	// bz1-dev1: a release under another name cannot take over its objects.
	spoRelease    = "spo"
	spoNamespace  = "security-profiles-operator"
	spoDeployment = "security-profiles-operator"
)

var spoSpec = &chartSpec{
	name: "security-profiles-operator", ref: spoChart, sha256: spoChartSHA256, version: spoChartVersion,
	release: spoRelease, namespace: spoNamespace, deployment: spoDeployment,
	namespaceLabels: map[string]string{
		"pod-security.kubernetes.io/enforce": "privileged",
		"pod-security.kubernetes.io/audit":   "privileged",
		"pod-security.kubernetes.io/warn":    "privileged",
	},
	crds: []string{
		"profilebindings.security-profiles-operator.x-k8s.io",
		"profilerecordings.security-profiles-operator.x-k8s.io",
		"seccompprofiles.security-profiles-operator.x-k8s.io",
		"securityprofilenodestatuses.security-profiles-operator.x-k8s.io",
		"securityprofilesoperatordaemons.security-profiles-operator.x-k8s.io",
		"rawselinuxprofiles.security-profiles-operator.x-k8s.io",
		"selinuxprofiles.security-profiles-operator.x-k8s.io",
		"apparmorprofiles.security-profiles-operator.x-k8s.io",
	},
	clusterCRDs: []string{
		"seccompprofiles.security-profiles-operator.x-k8s.io",
		"securityprofilenodestatuses.security-profiles-operator.x-k8s.io",
		"rawselinuxprofiles.security-profiles-operator.x-k8s.io",
		"selinuxprofiles.security-profiles-operator.x-k8s.io",
		"apparmorprofiles.security-profiles-operator.x-k8s.io",
	},
	noWait: true,
}

// The imagebuild operator is installed from its own Helm chart into the platform
// namespace. It owns the ImageBuild CRD and manages container image builds using BuildKit.
// It installs after the project operator so builds can be created within projects,
// and after the Security Profiles Operator, which installs the seccomp profile
// its build pods run with.
const (
	imagebuilderChart      = "oci://ghcr.io/wutz/charts/imagebuild"
	imagebuildChartVersion = "0.2.0"
	imagebuildRelease      = "imagebuild"
	// The chart names the Deployment after the chart, not <release>-controller-manager
	// like the other operators: the Dependencies view finds the operator by this name.
	imagebuildDeployment = "imagebuild"
	// seccompProfileCRD is the CRD of the SeccompProfile the chart renders.
	seccompProfileCRD = "seccompprofiles.security-profiles-operator.x-k8s.io"
	// imagebuildSeccompProfile is the Localhost profile the build pods run with,
	// relative to the kubelet's seccomp directory: the file the Security Profiles
	// Operator writes for the chart's SeccompProfile imagebuild.
	imagebuildSeccompProfile = "operator/imagebuild.json"
)

var imagebuildSpec = &chartSpec{
	name: "imagebuild", ref: imagebuilderChart, version: imagebuildChartVersion, release: imagebuildRelease,
	deployment: imagebuildDeployment, crds: []string{"imagebuilds.imagebuild.wutz.dev"},
	needs:  []string{"projects.workspace.wutz.dev", seccompProfileCRD},
	values: imagebuildValues,
}

// imagebuildValues tells the chart which registry the builds push to. Without
// it every build fails with a message saying the registry is not set: the
// chart's own default is empty, and the platform config is where the domain and
// its scheme are configured.
//
// It also installs the seccomp profile the build pods run with, and sends the
// builds through the registry mirrors and away from the nodes the platform
// config excludes.
func imagebuildValues(platformCfg *platform.Config) map[string]any {
	cfg := map[string]any{
		"registry":         platformCfg.Registry.Domain,
		"registryInsecure": platformCfg.Registry.Insecure,
		"seccompProfile":   imagebuildSeccompProfile,
	}
	// Helm merges the values as generic maps and lists, so the typed ones are converted.
	if len(platformCfg.Build.RegistryMirrors) > 0 {
		mirrors := map[string]any{}
		for host, mirror := range platformCfg.Build.RegistryMirrors {
			mirrors[host] = mirror
		}
		cfg["registryMirrors"] = mirrors
	}
	if len(platformCfg.Build.ExcludeNodes) > 0 {
		nodes := []any{}
		for _, node := range platformCfg.Build.ExcludeNodes {
			nodes = append(nodes, node)
		}
		cfg["excludeNodes"] = nodes
	}
	return map[string]any{
		"config":         cfg,
		"seccompProfile": map[string]any{"install": true},
	}
}

// The volume trash operator is installed from its own Helm chart into the
// platform namespace. It purges trashed volumes once their retention has passed,
// and reads the Project CRD for each project's retention, so it installs after the
// project operator. It reads the platform config that forgeai renders, so it is
// pointed at the same ConfigMap.
const (
	volumetrashChart        = "oci://ghcr.io/wutz/charts/volumetrash"
	volumetrashChartVersion = "0.2.0"
	volumetrashRelease      = "volumetrash"
	volumetrashDeployment   = "volumetrash"
)

var volumetrashSpec = &chartSpec{
	name: "volumetrash", ref: volumetrashChart, version: volumetrashChartVersion,
	release: volumetrashRelease, deployment: volumetrashDeployment,
	needs:  []string{"projects.workspace.wutz.dev"},
	values: volumetrashValues,
}

// volumetrashValues points the operator at the platform config ConfigMap, which
// lives in the namespace the chart is installed into.
func volumetrashValues(_ *platform.Config) map[string]any {
	return map[string]any{"platform": map[string]any{
		"namespace": platform.Namespace(), "configMap": platform.ConfigMapName,
	}}
}

// installChart installs the chart of spec into its namespace, or upgrades the
// release already there. The chart is pulled at the pinned version and, unless
// the spec says noWait, waited on until its Deployment is up.
func installChart(ctx context.Context, c *Connection, spec *chartSpec, platformCfg *platform.Config, progress func(string)) error {
	ns := spec.installNamespace()
	progress(stepChartNamespace)
	if err := ensureNamespaceLabels(ctx, c.k8s, ns, spec.namespaceLabels); err != nil {
		return err
	}
	if err := recreateScopedCRDs(ctx, c.k8s, spec); err != nil {
		return err
	}
	cfg, err := helmConfig(c, ns)
	if err != nil {
		return err
	}
	up := action.NewUpgrade(cfg)
	up.Namespace = ns
	up.Version = spec.version
	up.Wait = !spec.noWait
	up.Atomic = !spec.noWait
	up.Timeout = deploymentReadyTimeout

	progress(stepChartPull)
	chrt, err := loadChart(ctx, spec, up)
	if err != nil {
		return err
	}
	var values map[string]any
	if spec.values != nil {
		values = spec.values(platformCfg)
	}
	progress(stepChartInstall)
	if err := runRelease(ctx, cfg, up, spec, chrt, values); err != nil || !spec.noWait {
		return err
	}
	return waitCRDNames(ctx, c.k8s, spec.crds)
}

// runRelease installs the release of spec, or upgrades the one already there,
// and returns the error of that action. Upgrade refuses a release that does not
// exist and does not install one by itself, so a first install takes the Install
// action.
func runRelease(ctx context.Context, cfg *action.Configuration, up *action.Upgrade, spec *chartSpec, chrt *chart.Chart, values map[string]any) error {
	_, err := cfg.Releases.Last(spec.release)
	switch {
	case errors.Is(err, driver.ErrReleaseNotFound):
		ins := action.NewInstall(cfg)
		ins.ReleaseName = spec.release
		ins.Namespace = spec.installNamespace()
		ins.Wait = !spec.noWait
		ins.Atomic = !spec.noWait
		ins.Timeout = deploymentReadyTimeout
		_, err = ins.RunWithContext(ctx, chrt, values)
	case err == nil:
		_, err = up.RunWithContext(ctx, spec.release, chrt, values)
	}
	return err
}

// maxChartArchive bounds the download of a chart archive.
const maxChartArchive = 64 << 20

// loadChart returns the chart of spec at its pinned version. An https:// chart is
// the archive of a release asset: it is downloaded and checked against its
// digest, because a release asset can be replaced and Helm pins only OCI charts
// by tag. Helm does not read it itself, so the chart does not depend on the
// Helm repositories configured on this machine. Other charts are located by Helm.
func loadChart(ctx context.Context, spec *chartSpec, up *action.Upgrade) (*chart.Chart, error) {
	if spec.sha256 == "" {
		path, err := up.ChartPathOptions.LocateChart(spec.ref, cli.New())
		if err != nil {
			return nil, err
		}
		return loader.Load(path)
	}
	data, err := fetchChartArchive(ctx, spec.ref)
	if err != nil {
		return nil, err
	}
	if err := checkDigest(data, spec.sha256); err != nil {
		return nil, fmt.Errorf("chart %s: %w", spec.ref, err)
	}
	return loader.LoadArchive(bytes.NewReader(data))
}

// fetchChartArchive downloads the chart archive at url.
func fetchChartArchive(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download chart %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxChartArchive+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxChartArchive {
		return nil, fmt.Errorf("download chart %s: larger than %d bytes", url, maxChartArchive)
	}
	return data, nil
}

// checkDigest reports whether data hashes to the hex SHA-256 digest want.
func checkDigest(data []byte, want string) error {
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("digest %s does not match the pinned %s", got, want)
	}
	return nil
}

// helmConfig returns the Helm configuration that acts on the releases of the
// namespace ns over the connection.
func helmConfig(c *Connection, ns string) (*action.Configuration, error) {
	registryClient, err := registry.NewClient(registry.ClientOptWriter(io.Discard))
	if err != nil {
		return nil, err
	}
	cfg := new(action.Configuration)
	if err := cfg.Init(restGetter{rest: c.rest, namespace: ns}, ns, "secret", func(string, ...any) {}); err != nil {
		return nil, err
	}
	cfg.RegistryClient = registryClient
	return cfg, nil
}

// replaceLegacy uninstalls the release spec was installed as before its chart
// was renamed. Uninstalling its chart deletes its CRD, which deletes the
// FileServers, and their objects are garbage-collected with them. Then the
// volumes trashed under the old label move to the label the new operator reads.
// That waits for the old operator to be gone: moved first, it would serve them.
func replaceLegacy(ctx context.Context, c *Connection, spec *chartSpec) error {
	cfg, err := helmConfig(c, spec.installNamespace())
	if err != nil {
		return err
	}
	if _, err := cfg.Releases.Last(spec.legacy); err == nil {
		if _, err := action.NewUninstall(cfg).Run(spec.legacy); err != nil {
			return err
		}
	} else if !errors.Is(err, driver.ErrReleaseNotFound) {
		return err
	}
	return migrateTrashLabels(ctx, c.k8s)
}

// migrateTrashLabels moves the volumes trashed before fileserver 0.3.0 to the
// ignore label, so the file servers do not serve them.
func migrateTrashLabels(ctx context.Context, k8s client.Client) error {
	list := &corev1.PersistentVolumeClaimList{}
	if err := k8s.List(ctx, list, client.MatchingLabels{naming.LabelTrashLegacy: naming.LiteralTrue}); err != nil {
		return err
	}
	for i := range list.Items {
		if err := volumes.MigrateLegacyTrash(ctx, k8s, &list.Items[i]); err != nil {
			return err
		}
	}
	return nil
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
	return ensureNamespaceLabels(ctx, k8s, name, nil)
}

// ensureNamespaceLabels creates the namespace with labels unless it exists, and
// sets the labels on one that does. Other labels of an existing namespace stay.
func ensureNamespaceLabels(ctx context.Context, k8s client.Client, name string, labels map[string]string) error {
	ns := &corev1.Namespace{}
	err := k8s.Get(ctx, client.ObjectKey{Name: name}, ns)
	if apierrors.IsNotFound(err) {
		err = k8s.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}})
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		// Another install created it first; set the labels on what is there.
		if err = k8s.Get(ctx, client.ObjectKey{Name: name}, ns); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	orig := ns.DeepCopy()
	for k, v := range labels {
		if ns.Labels == nil {
			ns.Labels = map[string]string{}
		}
		ns.Labels[k] = v
	}
	if maps.Equal(orig.Labels, ns.Labels) {
		return nil
	}
	return k8s.Patch(ctx, ns, client.MergeFrom(orig))
}

// recreateScopedCRDs deletes the CRDs of spec.clusterCRDs that exist as
// namespaced, so the chart installs them cluster-scoped. A CRD that holds objects
// is refused: deleting it would delete them.
func recreateScopedCRDs(ctx context.Context, k8s client.Client, spec *chartSpec) error {
	for _, name := range spec.clusterCRDs {
		crd := &unstructured.Unstructured{}
		crd.SetGroupVersionKind(crdGVK)
		err := k8s.Get(ctx, client.ObjectKey{Name: name}, crd)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		if scope, _, _ := unstructured.NestedString(crd.Object, "spec", "scope"); scope == "Cluster" {
			continue
		}
		group, _, _ := unstructured.NestedString(crd.Object, "spec", "group")
		kind, _, _ := unstructured.NestedString(crd.Object, "spec", "names", "kind")
		version := servedVersion(crd)
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: version, Kind: kind + "List"})
		if err := k8s.List(ctx, list, client.Limit(1)); err != nil {
			return fmt.Errorf("list %s: %w", name, err)
		}
		if len(list.Items) > 0 {
			return fmt.Errorf("the CRD %s is namespaced and the chart needs it cluster-scoped, but it holds %s objects; delete them before installing %s",
				name, kind, spec.name)
		}
		if err := k8s.Delete(ctx, crd); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		err = wait.PollUntilContextTimeout(ctx, time.Second, crdEstablishedTimeout, true, func(ctx context.Context) (bool, error) {
			err := k8s.Get(ctx, client.ObjectKey{Name: name}, &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": crdGVK.GroupVersion().String(), "kind": crdGVK.Kind,
			}})
			return apierrors.IsNotFound(err), nil
		})
		if err != nil {
			return fmt.Errorf("wait for the CRD %s to be deleted: %w", name, err)
		}
	}
	return nil
}

// waitCRDNames waits until the named CRDs are established.
func waitCRDNames(ctx context.Context, k8s client.Client, names []string) error {
	objs := make([]*unstructured.Unstructured, 0, len(names))
	for _, name := range names {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(crdGVK)
		u.SetName(name)
		objs = append(objs, u)
	}
	return waitCRDs(ctx, k8s, objs)
}

// servedVersion returns the first version a CRD serves.
func servedVersion(crd *unstructured.Unstructured) string {
	versions, _, _ := unstructured.NestedSlice(crd.Object, "spec", "versions")
	for _, v := range versions {
		m, _ := v.(map[string]any)
		if served, _ := m["served"].(bool); served {
			name, _ := m["name"].(string)
			return name
		}
	}
	return ""
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

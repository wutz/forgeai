package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	authnv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wutz/forgeai/pkg/platform"
)

// The cluster components forgeai runs on. Their install manifests are
// embedded from deps/, rendered at the pinned versions by hack/gui-deps.sh.
// The operators are installed from their Helm charts (chart.go).
//
//go:embed deps
var depsFS embed.FS

const (
	// depLabel and depVersionLabel mark every object an install applies, so
	// the GUI finds the deployed version of a component on the cluster.
	depLabel        = "forgeai.wutz.dev/dependency"
	depVersionLabel = "forgeai.wutz.dev/dependency-version"
	// helmChartLabel is the label Helm stamps on the objects of a chart, with the
	// chart name and version: the GUI finds the version of a chart-installed operator in it.
	helmChartLabel = "helm.sh/chart"
	// fieldOwner is the server-side apply manager of the installs.
	fieldOwner = "forgeai-gui"
	// depProgressEvent carries DependencyProgress to the frontend.
	depProgressEvent = "deps:progress"
	// depRecordName is the ConfigMap in the platform namespace that records how
	// each component was installed: one JSON entry per component. It lives in
	// the cluster so every client sees the same record.
	depRecordName = "forgeai-dependencies"

	depInstallTimeout      = 30 * time.Minute
	crdEstablishedTimeout  = 2 * time.Minute
	deploymentReadyTimeout = 10 * time.Minute
	// forgeaiDeployment is the operator Deployment: config/default prefixes
	// its names with forgeai-.
	forgeaiDeployment = "forgeai-controller-manager"
)

// Install steps, the values of DependencyProgress.Step. The GUI keys its text
// on them as deps.step.<step>.
const (
	stepNamespaces     = "namespaces"
	stepCRDs           = "crds"
	stepControllers    = "controllers"
	stepDeployments    = "deployments"
	stepWebhooks       = "webhooks"
	stepChartNamespace = "chart-namespace"
	stepChartPull      = "chart-pull"
	stepChartInstall   = "chart-install"
	stepRecord         = "record"
	stepRestart        = "restart"
)

var (
	crdGVK      = schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"}
	namespaceGK = schema.GroupKind{Kind: "Namespace"}
	crdGK       = schema.GroupKind{Group: "apiextensions.k8s.io", Kind: "CustomResourceDefinition"}
	webhookGKs  = []schema.GroupKind{
		{Group: "admissionregistration.k8s.io", Kind: "MutatingWebhookConfiguration"},
		{Group: "admissionregistration.k8s.io", Kind: "ValidatingWebhookConfiguration"},
	}
	forgeaiCRDs = []string{"projects.forgeai.wutz.dev", "fileservers.forgeai.wutz.dev", "inferenceservices.forgeai.wutz.dev"}
)

// depInfo describes a component on the admin page.
type depInfo struct {
	name        string
	title       string
	description string
	// chart is set for components installed from a Helm chart rather than from an embedded bundle.
	chart *chartSpec
}

// depCatalog lists the components in install order: Kueue's TrainJob
// integration needs the Trainer CRDs, so Trainer comes first, and the SSH
// operator needs the sshpiper Pipe CRD.
var depCatalog = []depInfo{
	{name: "trainer", title: "Kubeflow Trainer v2", description: "TrainJobs and the ClusterTrainingRuntimes, with JobSet"},
	{name: "kueue", title: "Kueue", description: "GPU quota queues that admit TrainJobs"},
	{name: "agent-sandbox", title: "agent-sandbox", description: "Sandbox controller and CRDs for the Sandboxes view"},
	{name: "gateway-api-inference-extension", title: "Gateway API Inference Extension", description: "InferencePool CRD that the llm-d routers of InferenceServices use"},
	{name: "forgeai", title: "forgeai operator", description: "Projects, instance types and the controllers", chart: forgeaiSpec},
	{name: "sshpiper", title: "sshpiper Pipe CRD", description: "Pipe CRD that the SSH gateway routes logins with"},
	{name: "ssh-operator", title: "SSH operator", description: "SSHGateways and SSHRoutes that serve sandbox SSH logins", chart: sshOperatorSpec},
}

// depInfoByName returns the catalog entry of a component.
func depInfoByName(name string) (depInfo, bool) {
	for _, info := range depCatalog {
		if info.name == name {
			return info, true
		}
	}
	return depInfo{}, false
}

// Dependency is one cluster component as the Dependencies view shows it.
type Dependency struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Pinned is the version the GUI installs; empty for components it only reports on.
	Pinned string `json:"pinned"`
	// Installed is the version the cluster runs, empty when the GUI did not install it.
	Installed string `json:"installed"`
	// Running is the image tag of a component the GUI did not install, so its
	// version still shows. Empty when the image is pinned by digest or untagged.
	Running string `json:"running,omitempty"`
	// Namespace is where the cluster runs the component, empty when unknown.
	Namespace string `json:"namespace"`
	// AppliedBy and AppliedAt come from the install record: who installed the
	// component and when. Empty for components installed outside forgeai.
	AppliedBy string `json:"appliedBy"`
	AppliedAt string `json:"appliedAt"`
	// Status is missing, degraded, mismatch, unmanaged, outdated, installing or ready.
	// mismatch means the component runs where the platform config does not expect it.
	Status string `json:"status"`
	// Reason names the state the GUI explains in its own words: unmanaged,
	// unmanagedHelm, namespaceMismatch or operatorMissing. Empty for the other
	// states. Message stays the English diagnostic.
	Reason string `json:"reason,omitempty"`
	// ExpectedNamespace is where the platform config expects the component when
	// Reason is namespaceMismatch.
	ExpectedNamespace string `json:"expectedNamespace,omitempty"`
	// Message is the English diagnostic. MessageKey and MessageArgs name the same
	// diagnostic for the GUI to word in the user's language. Empty when the GUI
	// words the state from Status or Reason.
	Message     string            `json:"message"`
	MessageKey  string            `json:"messageKey,omitempty"`
	MessageArgs map[string]string `json:"messageArgs,omitempty"`
	Installable bool              `json:"installable"`
}

// diagnose sets the diagnostic of a state: the English text, and the key and
// values the GUI words it with.
func diagnose(dep *Dependency, key string, args map[string]string, english string) {
	dep.Message, dep.MessageKey, dep.MessageArgs = english, key, args
}

// depRecord is the provenance of one install, stored in depRecordName.
type depRecord struct {
	Version   string `json:"version"`
	Namespace string `json:"namespace"`
	// Source is the chart reference of a component installed from a Helm chart.
	Source         string `json:"source,omitempty"`
	ManifestSHA256 string `json:"manifestSHA256,omitempty"`
	AppliedBy      string `json:"appliedBy,omitempty"`
	AppliedAt      string `json:"appliedAt"`
}

// Dependencies is the Dependencies view: the components and whether the user
// may install them. Only cluster administrators may read or change them.
type Dependencies struct {
	Cluster      string       `json:"cluster"`
	ClusterAdmin bool         `json:"clusterAdmin"`
	Items        []Dependency `json:"items"`
}

// DependencyProgress is one step of an install, or its outcome when Done is set.
type DependencyProgress struct {
	// Cluster is the context the install runs on; a view of another cluster
	// ignores the event.
	Cluster string `json:"cluster"`
	Name    string `json:"name"`
	// Step is the install step under way, one of the step constants. The GUI
	// shows its translated text, so the backend sends no English prose here.
	Step  string `json:"step,omitempty"`
	Done  bool   `json:"done"`
	Error string `json:"error,omitempty"`
}

// depBundle is an embedded install manifest of one component at its pinned version.
type depBundle struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Source  string `json:"source"`
	File    string `json:"file"`
	SHA256  string `json:"sha256"`

	objects []*unstructured.Unstructured
}

// bundles returns the embedded bundles in install order, parsed once.
var bundles = sync.OnceValues(func() ([]*depBundle, error) { return loadBundles(depsFS) })

// loadBundles reads deps/manifest.json and its manifests, and checks each
// manifest against the digest recorded for it.
func loadBundles(fsys fs.FS) ([]*depBundle, error) {
	raw, err := fs.ReadFile(fsys, "deps/manifest.json")
	if err != nil {
		return nil, err
	}
	var m struct {
		Bundles []*depBundle `json:"bundles"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("deps/manifest.json: %w", err)
	}
	for _, b := range m.Bundles {
		data, err := fs.ReadFile(fsys, "deps/"+b.File)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != b.SHA256 {
			return nil, fmt.Errorf("%s: digest does not match deps/manifest.json", b.File)
		}
		if b.objects, err = decodeObjects(data); err != nil {
			return nil, fmt.Errorf("%s: %w", b.File, err)
		}
	}
	return m.Bundles, nil
}

// decodeObjects splits a multi-document manifest into objects.
func decodeObjects(data []byte) ([]*unstructured.Unstructured, error) {
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var out []*unstructured.Unstructured
	for {
		m := map[string]any{}
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return nil, err
		}
		if len(m) == 0 {
			continue
		}
		out = append(out, &unstructured.Unstructured{Object: m})
	}
}

// crdNames lists the CRDs a bundle installs.
func (b *depBundle) crdNames() []string {
	var out []string
	for _, o := range b.objects {
		if o.GroupVersionKind().GroupKind() == crdGK {
			out = append(out, o.GetName())
		}
	}
	return out
}

// bundleByName returns the bundle of a component, or nil when the GUI does
// not install it.
func bundleByName(name string) (*depBundle, error) {
	bs, err := bundles()
	if err != nil {
		return nil, err
	}
	for _, b := range bs {
		if b.Name == name {
			return b, nil
		}
	}
	return nil, nil
}

// depHealth is what the cluster holds for one component.
type depHealth struct {
	crdsWanted, crdsFound int
	deployments           []appsv1.Deployment
	// chart is set for a component installed from a Helm chart.
	chart *chartSpec
	// crdVersion is the version label of the bundle's CRDs. A bundle with no
	// Deployments, such as the Gateway API CRDs, has nothing else to carry it.
	crdVersion string
	// namespace is where the component runs; expectNamespace is where the
	// platform config says it must. The check is skipped when expectNamespace is empty.
	namespace, expectNamespace string
}

// assess sets the status of a component from its cluster state.
func assess(dep *Dependency, h depHealth) {
	if h.crdsFound == 0 && len(h.deployments) == 0 {
		dep.Status, dep.Message = "missing", "Not installed"
		return
	}
	if h.crdsFound < h.crdsWanted {
		dep.Status = "degraded"
		diagnose(dep, "crdsInstalled", map[string]string{
			"found": fmt.Sprint(h.crdsFound), "wanted": fmt.Sprint(h.crdsWanted),
		}, fmt.Sprintf("%d of %d CRDs installed", h.crdsFound, h.crdsWanted))
		return
	}
	if h.chart != nil && len(h.deployments) == 0 {
		dep.Status, dep.Message = "degraded", "Operator Deployment not found"
		dep.Reason = "operatorMissing"
		return
	}
	if dep.Installed == "" {
		dep.Installed = h.crdVersion
	}
	var notReady []string
	for i := range h.deployments {
		d := &h.deployments[i]
		if v := deployedVersion(d, h.chart); v != "" {
			if dep.Installed == "" {
				dep.Installed = v
			}
		} else if dep.Running == "" {
			dep.Running = imageTag(d)
		}
		if !deploymentAvailable(d) {
			notReady = append(notReady, d.Namespace+"/"+d.Name)
		}
	}
	if len(notReady) > 0 {
		dep.Status = "degraded"
		diagnose(dep, "notAvailable", map[string]string{
			"deployments": strings.Join(notReady, ", "),
		}, fmt.Sprintf("Not available: %v", notReady))
		return
	}
	if h.expectNamespace != "" && h.namespace != "" && h.namespace != h.expectNamespace {
		dep.Status = "mismatch"
		dep.Reason = "namespaceMismatch"
		dep.ExpectedNamespace = h.expectNamespace
		dep.Message = fmt.Sprintf("Runs in %s, but the platform config sets sandbox.namespace to %s",
			h.namespace, h.expectNamespace)
		return
	}
	if dep.Installable && dep.Installed == "" {
		dep.Status = "unmanaged"
		dep.Reason = "unmanaged"
		dep.Message = "Installed outside forgeai; installing replaces it with the pinned version"
		if h.chart != nil {
			dep.Reason = "unmanagedHelm"
			dep.Message = fmt.Sprintf("%s is deployed outside Helm; remove it before installing the chart", h.chart.deployment)
		}
		return
	}
	if dep.Installable && dep.Installed != dep.Pinned {
		dep.Status = "outdated"
		dep.Message = fmt.Sprintf("Runs %s, the pinned version is %s", dep.Installed, dep.Pinned)
		return
	}
	dep.Status = "ready"
}

// imageTag returns the tag of the Deployment's first container image, or ""
// when the image has no tag or is pinned by digest.
func imageTag(d *appsv1.Deployment) string {
	cs := d.Spec.Template.Spec.Containers
	if len(cs) == 0 {
		return ""
	}
	image := cs[0].Image
	if strings.Contains(image, "@") {
		return ""
	}
	// The last colon is a tag separator only when it comes after the last slash;
	// otherwise it is a registry port.
	i := strings.LastIndex(image, ":")
	if i < 0 || i < strings.LastIndex(image, "/") {
		return ""
	}
	return image[i+1:]
}

// deployedVersion returns the version a Deployment was installed at: the version
// label of a bundle, or the version of the chart that installed it. It is empty
// when neither says.
func deployedVersion(d *appsv1.Deployment, chart *chartSpec) string {
	if v := d.Labels[depVersionLabel]; v != "" {
		return v
	}
	if chart == nil {
		return ""
	}
	return chartLabelVersion(d.Labels, chart.name)
}

// deploymentAvailable reports whether the Deployment has rolled out and is available.
func deploymentAvailable(d *appsv1.Deployment) bool {
	if d.Status.ObservedGeneration < d.Generation {
		return false
	}
	for _, c := range d.Status.Conditions {
		if c.Type == appsv1.DeploymentAvailable {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// ListDependencies reports the cluster components. Only cluster administrators
// get the list; for anyone else it is empty.
func (a *App) ListDependencies() (*Dependencies, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	out := &Dependencies{Cluster: c.Info.Context, ClusterAdmin: mustBeClusterAdmin(ctx, c.k8s), Items: []Dependency{}}
	if !out.ClusterAdmin {
		return out, nil
	}
	cfg, err := loadPlatform(ctx, c.k8s)
	if err != nil {
		return nil, err
	}
	records, err := readRecords(ctx, c.k8s)
	if err != nil {
		return nil, err
	}
	// The components are probed at once: each probe is a few independent reads, so
	// the page waits for the slowest component, not the sum of all of them.
	items := make([]Dependency, len(depCatalog))
	err = forEachConcurrently(len(depCatalog), func(i int) error {
		info := depCatalog[i]
		b, err := bundleByName(info.name)
		if err != nil {
			return err
		}
		dep, err := probeDependency(ctx, c.k8s, info, b, expectedNamespace(cfg, info.name))
		if err != nil {
			return err
		}
		if r, ok := records[info.name]; ok {
			dep.AppliedBy, dep.AppliedAt = r.AppliedBy, r.AppliedAt
		}
		if a.installs.active(installKey(c, info.name)) {
			dep.Status = "installing"
			diagnose(&dep, "installing", nil, "Installing")
		}
		items[i] = dep
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Items = append(out.Items, items...)
	return out, nil
}

// forEachConcurrently runs fn for each index in [0, n) at once and returns the
// first error by index. Each fn writes only its own slot, so callers need no lock.
func forEachConcurrently(n int, fn func(i int) error) error {
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { errs[i] = fn(i) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// probeDependency reads the CRDs and Deployments of a component. Bundled
// components are found by their labels; forgeai by its operator Deployment.
func probeDependency(ctx context.Context, k8s client.Client, info depInfo, b *depBundle, expectNamespace string) (Dependency, error) {
	dep := Dependency{
		Name: info.name, Title: info.title, Description: info.description, Installable: b != nil || info.chart != nil,
	}
	var h depHealth
	var crds []string
	switch {
	case b != nil:
		dep.Pinned = b.Version
		crds = b.crdNames()
	case info.chart != nil:
		dep.Pinned = info.chart.version
		crds = info.chart.crds
		h.chart = info.chart
	}
	h.crdsWanted = len(crds)
	// Each CRD is read at once; a slot per CRD keeps the count free of a lock.
	found := make([]bool, len(crds))
	versions := make([]string, len(crds))
	if err := forEachConcurrently(len(crds), func(i int) error {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(crdGVK)
		err := k8s.Get(ctx, client.ObjectKey{Name: crds[i]}, u)
		switch {
		case err == nil:
			found[i] = true
			versions[i] = u.GetLabels()[depVersionLabel]
		case !apierrors.IsNotFound(err):
			return err
		}
		return nil
	}); err != nil {
		return dep, err
	}
	for i, ok := range found {
		if ok {
			h.crdsFound++
		}
		if h.crdVersion == "" && b != nil && !bundleHasDeployment(b) {
			h.crdVersion = versions[i]
		}
	}
	if b != nil {
		var list appsv1.DeploymentList
		if err := k8s.List(ctx, &list, client.MatchingLabels{depLabel: b.Name}); err != nil {
			return dep, err
		}
		h.deployments = list.Items
	} else if info.chart != nil {
		d := &appsv1.Deployment{}
		err := k8s.Get(ctx, client.ObjectKey{Namespace: info.chart.installNamespace(), Name: info.chart.deployment}, d)
		switch {
		case err == nil:
			h.deployments = []appsv1.Deployment{*d}
		case !apierrors.IsNotFound(err):
			return dep, err
		}
	}
	h.expectNamespace = expectNamespace
	if len(h.deployments) > 0 {
		h.namespace = h.deployments[0].Namespace
	}
	dep.Namespace = h.namespace
	assess(&dep, h)
	// An install outside forgeai carries no labels, so its Deployments are not
	// found above. Its running version comes from the Deployments the bundle declares.
	if b != nil && len(h.deployments) == 0 && dep.Status != "missing" {
		running, err := bundleRunningTags(ctx, k8s, b)
		if err != nil {
			return dep, err
		}
		dep.Running = running
	}
	return dep, nil
}

// bundleHasDeployment reports whether the bundle declares a Deployment.
func bundleHasDeployment(b *depBundle) bool {
	for _, o := range b.objects {
		if o.GetKind() == "Deployment" && o.GroupVersionKind().Group == appsv1.GroupName {
			return true
		}
	}
	return false
}

// bundleRunningTags returns the distinct image tags of the Deployments the
// bundle declares that exist in the cluster, joined in bundle order. A
// component with several Deployments, such as Trainer with JobSet, lists each.
func bundleRunningTags(ctx context.Context, k8s client.Reader, b *depBundle) (string, error) {
	var tags []string
	for _, o := range b.objects {
		if o.GetKind() != "Deployment" || o.GroupVersionKind().Group != appsv1.GroupName {
			continue
		}
		d := &appsv1.Deployment{}
		err := k8s.Get(ctx, client.ObjectKey{Namespace: o.GetNamespace(), Name: o.GetName()}, d)
		switch {
		case err == nil:
			if tag := imageTag(d); tag != "" && !slices.Contains(tags, tag) {
				tags = append(tags, tag)
			}
		case !apierrors.IsNotFound(err):
			return "", err
		}
	}
	return strings.Join(tags, ", "), nil
}

// loadPlatform reads the platform config. Without the ConfigMap the defaults apply.
func loadPlatform(ctx context.Context, k8s client.Reader) (*platform.Config, error) {
	cfg, err := platform.Load(ctx, k8s, platform.Namespace())
	if apierrors.IsNotFound(err) {
		return platform.Parse([]byte{})
	}
	return cfg, err
}

// expectedNamespace is where the platform config says a component runs, empty
// when it says nothing. agent-sandbox's namespace is sandbox.namespace.
func expectedNamespace(cfg *platform.Config, name string) string {
	if name == "agent-sandbox" {
		return cfg.Sandbox.NamespaceOrDefault()
	}
	return ""
}

// bundleNamespace is the namespace a bundle's Deployments run in, empty when it has none.
func bundleNamespace(b *depBundle) string {
	for _, o := range b.objects {
		if o.GetKind() == "Deployment" && o.GroupVersionKind().Group == appsv1.GroupName {
			return o.GetNamespace()
		}
	}
	return ""
}

// checkInstallTarget refuses an install that would run a component where the
// platform config does not expect it. The GUI installs agent-sandbox into its
// bundled namespace only, so sandbox.namespace must name that namespace.
func checkInstallTarget(b *depBundle, cfg *platform.Config) error {
	want, got := expectedNamespace(cfg, b.Name), bundleNamespace(b)
	if want != "" && got != "" && want != got {
		return fmt.Errorf("%s installs into %s, but the platform config sets sandbox.namespace to %s",
			b.Name, got, want)
	}
	return nil
}

// selectorKinds are the apps/v1 workload kinds whose spec.selector the API server
// makes immutable. A bundle that names one of them with a different selector
// cannot be applied over the live object.
var selectorKinds = []string{"Deployment", "StatefulSet", "DaemonSet"}

// checkSelectors refuses a bundle whose workloads would change a selector the
// cluster already runs with. Kubernetes makes selectors immutable, so the apply
// would fail partway through the stages. A workload installed by Helm names its
// release, which has to be uninstalled before the bundle can take its place.
func checkSelectors(ctx context.Context, k8s client.Reader, b *depBundle) error {
	for _, o := range b.objects {
		if o.GroupVersionKind().Group != appsv1.GroupName || !slices.Contains(selectorKinds, o.GetKind()) {
			continue
		}
		want, _, _ := unstructured.NestedMap(o.Object, "spec", "selector")
		live := &unstructured.Unstructured{}
		live.SetGroupVersionKind(o.GroupVersionKind())
		err := k8s.Get(ctx, client.ObjectKey{Namespace: o.GetNamespace(), Name: o.GetName()}, live)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		got, _, _ := unstructured.NestedMap(live.Object, "spec", "selector")
		if reflect.DeepEqual(got, want) {
			continue
		}
		ref := live.GetNamespace() + "/" + live.GetName()
		if release := live.GetAnnotations()["meta.helm.sh/release-name"]; live.GetLabels()["app.kubernetes.io/managed-by"] == "Helm" && release != "" {
			return fmt.Errorf("%s is installed by Helm release %s with a different selector; uninstall that release before installing %s", ref, release, b.Name)
		}
		return fmt.Errorf("%s has a selector the %s bundle cannot change in place; delete the %s before installing %s", ref, b.Name, o.GetKind(), b.Name)
	}
	return nil
}

// readRecords returns the install records by component. A missing record
// ConfigMap means nothing was recorded.
func readRecords(ctx context.Context, k8s client.Reader) (map[string]depRecord, error) {
	cm := &corev1.ConfigMap{}
	err := k8s.Get(ctx, client.ObjectKey{Namespace: platform.Namespace(), Name: depRecordName}, cm)
	if apierrors.IsNotFound(err) {
		return map[string]depRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(map[string]depRecord, len(cm.Data))
	for name, raw := range cm.Data {
		var r depRecord
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, fmt.Errorf("%s: record of %s: %w", depRecordName, name, err)
		}
		out[name] = r
	}
	return out, nil
}

// writeRecord stores the record of a component's install, creating the record
// ConfigMap on first use. Other components' entries are left as they are.
func writeRecord(ctx context.Context, k8s client.Client, name string, r depRecord) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	key := client.ObjectKey{Namespace: platform.Namespace(), Name: depRecordName}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm := &corev1.ConfigMap{}
		err := k8s.Get(ctx, key, cm)
		switch {
		case apierrors.IsNotFound(err):
			cm = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name},
				Data:       map[string]string{name: string(raw)},
			}
			// Another install may create the ConfigMap first; a conflict makes
			// RetryOnConflict read it again and update it.
			err = k8s.Create(ctx, cm)
			if apierrors.IsAlreadyExists(err) {
				return apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, key.Name, err)
			}
			return err
		case err != nil:
			return err
		}
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data[name] = string(raw)
		return k8s.Update(ctx, cm)
	})
}

// currentUser names the user the connection authenticates as, for the install
// record. It is empty when the API server does not say.
func currentUser(ctx context.Context, k8s client.Client) string {
	r := &authnv1.SelfSubjectReview{}
	if err := k8s.Create(ctx, r); err != nil {
		return ""
	}
	return r.Status.UserInfo.Username
}

// InstallDependency installs or upgrades a bundled component on the connected
// cluster. The install runs in the background: its steps and the outcome
// arrive as deps:progress events, the last one with Done set.
func (a *App) InstallDependency(name string) error {
	c, err := a.current()
	if err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()
	if !mustBeClusterAdmin(ctx, c.k8s) {
		return errors.New("installing cluster components needs cluster-admin")
	}
	info, ok := depInfoByName(name)
	if !ok {
		return fmt.Errorf("unknown component %s", name)
	}
	b, err := bundleByName(name)
	if err != nil {
		return err
	}
	if b == nil && info.chart == nil {
		return fmt.Errorf("%s is not installed from the GUI", name)
	}
	cfg, err := loadPlatform(ctx, c.k8s)
	if err != nil {
		return err
	}
	if b != nil {
		if err := checkInstallTarget(b, cfg); err != nil {
			return err
		}
		if err := checkSelectors(ctx, c.k8s, b); err != nil {
			return err
		}
	} else if err := checkChartTarget(ctx, c.k8s, info.chart); err != nil {
		return err
	}
	key := installKey(c, name)
	cluster := c.Info.Context
	if !a.installs.start(key) {
		return fmt.Errorf("%s is already being installed", name)
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, depInstallTimeout)
		defer cancel()
		progress := func(step string) {
			a.emitEvent(a.ctx, depProgressEvent, DependencyProgress{Cluster: cluster, Name: name, Step: step})
		}
		rec, err := installComponent(ctx, c, info, b, progress)
		if err == nil {
			progress(stepRecord)
			rec.AppliedBy = currentUser(ctx, c.k8s)
			rec.AppliedAt = time.Now().UTC().Format(time.RFC3339)
			err = writeRecord(ctx, c.k8s, name, rec)
		}
		if err == nil && name == "agent-sandbox" {
			progress(stepRestart)
			err = restartOperator(ctx, c.k8s)
		}
		// Release the component before the outcome is emitted: the view
		// refreshes on it and must not still see the install in progress.
		a.installs.finish(key)
		done := DependencyProgress{Cluster: cluster, Name: name, Done: true}
		if err != nil {
			done.Error = err.Error()
		}
		a.emitEvent(a.ctx, depProgressEvent, done)
	}()
	return nil
}

// installComponent installs a bundled component, or the chart of a component
// that has none. It returns the record of the install.
func installComponent(ctx context.Context, c *Connection, info depInfo, b *depBundle, progress func(string)) (depRecord, error) {
	if info.chart != nil {
		spec := info.chart
		rec := depRecord{Version: spec.version, Namespace: spec.installNamespace(), Source: spec.ref}
		return rec, installChart(ctx, c, spec, progress)
	}
	rec := depRecord{Version: b.Version, Namespace: bundleNamespace(b), ManifestSHA256: b.SHA256}
	return rec, installBundle(ctx, c.k8s, b, progress)
}

// installBundle applies a bundle in stages. Namespaces and CRDs come first,
// then the CRDs are awaited; workloads and RBAC follow, and the Deployments
// are awaited before webhooks and custom resources, which their controllers
// must be running to admit.
func installBundle(ctx context.Context, k8s client.Client, b *depBundle, progress func(string)) error {
	custom := customGroupKinds(b.objects)
	var stages [3][]*unstructured.Unstructured
	for _, o := range b.objects {
		u := o.DeepCopy()
		labels := u.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[depLabel] = b.Name
		labels[depVersionLabel] = b.Version
		u.SetLabels(labels)
		s := installStage(u, custom)
		stages[s] = append(stages[s], u)
	}

	progress(stepNamespaces)
	if err := applyAll(ctx, k8s, stages[0]); err != nil {
		return err
	}
	progress(stepCRDs)
	if err := waitCRDs(ctx, k8s, stages[0]); err != nil {
		return err
	}
	progress(stepControllers)
	if err := applyAll(ctx, k8s, stages[1]); err != nil {
		return err
	}
	progress(stepDeployments)
	if err := waitDeployments(ctx, k8s, stages[1]); err != nil {
		return err
	}
	progress(stepWebhooks)
	return applyAll(ctx, k8s, stages[2])
}

// installStage places an object in its install stage: 0 for namespaces and
// CRDs, 2 for webhook configurations and custom resources, 1 for the rest.
func installStage(u *unstructured.Unstructured, custom map[schema.GroupKind]bool) int {
	gk := u.GroupVersionKind().GroupKind()
	switch {
	case gk == namespaceGK || gk == crdGK:
		return 0
	case custom[gk]:
		return 2
	}
	for _, w := range webhookGKs {
		if gk == w {
			return 2
		}
	}
	return 1
}

// customGroupKinds lists the kinds of the CRDs in a bundle.
func customGroupKinds(objs []*unstructured.Unstructured) map[schema.GroupKind]bool {
	out := map[schema.GroupKind]bool{}
	for _, o := range objs {
		if o.GroupVersionKind().GroupKind() != crdGK {
			continue
		}
		group, _, _ := unstructured.NestedString(o.Object, "spec", "group")
		kind, _, _ := unstructured.NestedString(o.Object, "spec", "names", "kind")
		out[schema.GroupKind{Group: group, Kind: kind}] = true
	}
	return out
}

// applyAll server-side applies objects in order, under the forgeai field manager.
func applyAll(ctx context.Context, k8s client.Client, objs []*unstructured.Unstructured) error {
	for _, u := range objs {
		if err := k8s.Patch(ctx, u, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
			return fmt.Errorf("apply %s %s: %w", u.GetKind(), u.GetName(), err)
		}
	}
	return nil
}

// waitCRDs waits until the CRDs among objs are established.
func waitCRDs(ctx context.Context, k8s client.Client, objs []*unstructured.Unstructured) error {
	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, crdEstablishedTimeout, true,
		func(ctx context.Context) (bool, error) {
			for _, o := range objs {
				if o.GroupVersionKind().GroupKind() != crdGK {
					continue
				}
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(crdGVK)
				if err := k8s.Get(ctx, client.ObjectKey{Name: o.GetName()}, u); err != nil || !crdEstablished(u) {
					return false, nil
				}
			}
			return true, nil
		})
	if err != nil {
		return fmt.Errorf("wait for CRDs: %w", err)
	}
	return nil
}

// crdEstablished reports whether a CRD serves its resources.
func crdEstablished(u *unstructured.Unstructured) bool {
	conds, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	for _, c := range conds {
		m, ok := c.(map[string]any)
		if ok && m["type"] == "Established" && m["status"] == "True" {
			return true
		}
	}
	return false
}

// waitDeployments waits until the Deployments among objs are available.
func waitDeployments(ctx context.Context, k8s client.Client, objs []*unstructured.Unstructured) error {
	err := wait.PollUntilContextTimeout(ctx, 3*time.Second, deploymentReadyTimeout, true,
		func(ctx context.Context) (bool, error) {
			for _, o := range objs {
				if o.GetKind() != "Deployment" || o.GroupVersionKind().Group != appsv1.GroupName {
					continue
				}
				d := &appsv1.Deployment{}
				if err := k8s.Get(ctx, client.ObjectKey{Namespace: o.GetNamespace(), Name: o.GetName()}, d); err != nil {
					return false, nil
				}
				if !deploymentAvailable(d) {
					return false, nil
				}
			}
			return true, nil
		})
	if err != nil {
		return fmt.Errorf("wait for Deployments: %w", err)
	}
	return nil
}

// restartOperator restarts the forgeai operator. It registers the Sandbox
// controller only for agent-sandbox CRDs present when it starts.
func restartOperator(ctx context.Context, k8s client.Client) error {
	d := &appsv1.Deployment{}
	key := client.ObjectKey{Namespace: platform.Namespace(), Name: forgeaiDeployment}
	if err := k8s.Get(ctx, key, d); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	orig := d.DeepCopy()
	if d.Spec.Template.Annotations == nil {
		d.Spec.Template.Annotations = map[string]string{}
	}
	d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339)
	return k8s.Patch(ctx, d, client.MergeFrom(orig))
}

// installKey names an install by its cluster context as well as its component:
// the same component installing on another cluster is not this one.
func installKey(c *Connection, name string) string { return c.Info.Context + "/" + name }

// depInstalls tracks the installs in progress, by component.
type depInstalls struct {
	mu      sync.Mutex
	running map[string]bool
}

// start marks a component as installing; it reports false when it already is.
func (d *depInstalls) start(name string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running[name] {
		return false
	}
	if d.running == nil {
		d.running = map[string]bool{}
	}
	d.running[name] = true
	return true
}

func (d *depInstalls) finish(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.running, name)
}

func (d *depInstalls) active(name string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.running[name]
}

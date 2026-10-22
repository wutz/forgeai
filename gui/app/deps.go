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

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/wutz/forgeai/pkg/platform"
)

// The cluster components forgeai runs on. Their install manifests are
// embedded from deps/, rendered at the pinned versions by hack/gui-deps.sh.
// The forgeai operator itself is installed from its Helm chart (chart.go).
//
//go:embed deps
var depsFS embed.FS

const (
	// depLabel and depVersionLabel mark every object an install applies, so
	// the GUI finds the deployed version of a component on the cluster.
	depLabel        = "forgeai.wutz.dev/dependency"
	depVersionLabel = "forgeai.wutz.dev/dependency-version"
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

var (
	crdGVK      = schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"}
	namespaceGK = schema.GroupKind{Kind: "Namespace"}
	crdGK       = schema.GroupKind{Group: "apiextensions.k8s.io", Kind: "CustomResourceDefinition"}
	webhookGKs  = []schema.GroupKind{
		{Group: "admissionregistration.k8s.io", Kind: "MutatingWebhookConfiguration"},
		{Group: "admissionregistration.k8s.io", Kind: "ValidatingWebhookConfiguration"},
	}
	forgeaiCRDs = []string{"projects.forgeai.wutz.dev", "instancetypes.forgeai.wutz.dev", "fileservers.forgeai.wutz.dev"}
)

// depInfo describes a component on the admin page.
type depInfo struct {
	name        string
	title       string
	description string
	// chart is set for components installed from the forgeai Helm chart
	// rather than from an embedded bundle.
	chart bool
}

// depCatalog lists the components in install order: Kueue's TrainJob
// integration needs the Trainer CRDs, so Trainer comes first.
var depCatalog = []depInfo{
	{name: "trainer", title: "Kubeflow Trainer v2", description: "TrainJobs and the ClusterTrainingRuntimes, with JobSet"},
	{name: "kueue", title: "Kueue", description: "GPU quota queues that admit TrainJobs"},
	{name: "agent-sandbox", title: "agent-sandbox", description: "Sandbox controller and CRDs for the Sandboxes view"},
	{name: "forgeai", title: "forgeai operator", description: "Projects, instance types and the controllers", chart: true},
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
	// Namespace is where the cluster runs the component, empty when unknown.
	Namespace string `json:"namespace"`
	// AppliedBy and AppliedAt come from the install record: who installed the
	// component and when. Empty for components installed outside forgeai.
	AppliedBy string `json:"appliedBy"`
	AppliedAt string `json:"appliedAt"`
	// Status is missing, degraded, mismatch, unmanaged, outdated, installing or ready.
	// mismatch means the component runs where the platform config does not expect it.
	Status      string `json:"status"`
	Message     string `json:"message"`
	Installable bool   `json:"installable"`
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
	ClusterAdmin bool         `json:"clusterAdmin"`
	Items        []Dependency `json:"items"`
}

// DependencyProgress is one step of an install, or its outcome when Done is set.
type DependencyProgress struct {
	Name    string `json:"name"`
	Message string `json:"message"`
	Done    bool   `json:"done"`
	Error   string `json:"error,omitempty"`
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
		dep.Message = fmt.Sprintf("%d of %d CRDs installed", h.crdsFound, h.crdsWanted)
		return
	}
	if dep.Name == "forgeai" && len(h.deployments) == 0 {
		dep.Status, dep.Message = "degraded", "Operator Deployment not found"
		return
	}
	var notReady []string
	for i := range h.deployments {
		d := &h.deployments[i]
		if v := d.Labels[depVersionLabel]; v != "" && dep.Installed == "" {
			dep.Installed = v
		}
		if !deploymentAvailable(d) {
			notReady = append(notReady, d.Namespace+"/"+d.Name)
		}
	}
	if len(notReady) > 0 {
		dep.Status = "degraded"
		dep.Message = fmt.Sprintf("Not available: %v", notReady)
		return
	}
	if h.expectNamespace != "" && h.namespace != "" && h.namespace != h.expectNamespace {
		dep.Status = "mismatch"
		dep.Message = fmt.Sprintf("Runs in %s, but the platform config sets sandbox.namespace to %s",
			h.namespace, h.expectNamespace)
		return
	}
	if dep.Installable && dep.Installed == "" {
		dep.Status = "unmanaged"
		dep.Message = "Installed outside forgeai; installing replaces it with the pinned version"
		if dep.Name == "forgeai" {
			dep.Message = "Deployed outside Helm (make deploy-dev); remove it before installing the chart"
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
	out := &Dependencies{ClusterAdmin: mustBeClusterAdmin(ctx, c.k8s), Items: []Dependency{}}
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
	for _, info := range depCatalog {
		b, err := bundleByName(info.name)
		if err != nil {
			return nil, err
		}
		dep, err := probeDependency(ctx, c.k8s, info, b, expectedNamespace(cfg, info.name))
		if err != nil {
			return nil, err
		}
		if r, ok := records[info.name]; ok {
			dep.AppliedBy, dep.AppliedAt = r.AppliedBy, r.AppliedAt
		}
		if a.installs.active(info.name) {
			dep.Status, dep.Message = "installing", "Installing"
		}
		out.Items = append(out.Items, dep)
	}
	return out, nil
}

// probeDependency reads the CRDs and Deployments of a component. Bundled
// components are found by their labels; forgeai by its operator Deployment.
func probeDependency(ctx context.Context, k8s client.Client, info depInfo, b *depBundle, expectNamespace string) (Dependency, error) {
	dep := Dependency{
		Name: info.name, Title: info.title, Description: info.description, Installable: b != nil || info.chart,
	}
	var h depHealth
	crds := forgeaiCRDs
	switch {
	case b != nil:
		dep.Pinned = b.Version
		crds = b.crdNames()
	case info.chart:
		dep.Pinned = forgeaiChartVersion
	}
	h.crdsWanted = len(crds)
	for _, name := range crds {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(crdGVK)
		err := k8s.Get(ctx, client.ObjectKey{Name: name}, u)
		switch {
		case err == nil:
			h.crdsFound++
		case !apierrors.IsNotFound(err):
			return dep, err
		}
	}
	if b != nil {
		var list appsv1.DeploymentList
		if err := k8s.List(ctx, &list, client.MatchingLabels{depLabel: b.Name}); err != nil {
			return dep, err
		}
		h.deployments = list.Items
	} else {
		d := &appsv1.Deployment{}
		err := k8s.Get(ctx, client.ObjectKey{Namespace: platform.Namespace(), Name: forgeaiDeployment}, d)
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
	return dep, nil
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
	if b == nil && !info.chart {
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
	} else if err := checkChartTarget(ctx, c.k8s); err != nil {
		return err
	}
	if !a.installs.start(name) {
		return fmt.Errorf("%s is already being installed", name)
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, depInstallTimeout)
		defer cancel()
		progress := func(msg string) {
			runtime.EventsEmit(a.ctx, depProgressEvent, DependencyProgress{Name: name, Message: msg})
		}
		rec, err := installComponent(ctx, c, b, progress)
		if err == nil {
			progress("Recording the install")
			rec.AppliedBy = currentUser(ctx, c.k8s)
			rec.AppliedAt = time.Now().UTC().Format(time.RFC3339)
			err = writeRecord(ctx, c.k8s, name, rec)
		}
		if err == nil && name == "agent-sandbox" {
			progress("Restarting the forgeai operator to start the Sandbox controller")
			err = restartOperator(ctx, c.k8s)
		}
		// Release the component before the outcome is emitted: the view
		// refreshes on it and must not still see the install in progress.
		a.installs.finish(name)
		done := DependencyProgress{Name: name, Message: "Installed " + rec.Version, Done: true}
		if err != nil {
			done = DependencyProgress{Name: name, Message: "Install failed", Done: true, Error: err.Error()}
		}
		runtime.EventsEmit(a.ctx, depProgressEvent, done)
	}()
	return nil
}

// installComponent installs a bundled component, or the forgeai operator from
// its chart when there is no bundle. It returns the record of the install.
func installComponent(ctx context.Context, c *Connection, b *depBundle, progress func(string)) (depRecord, error) {
	if b == nil {
		rec := depRecord{Version: forgeaiChartVersion, Namespace: platform.Namespace(), Source: forgeaiChart}
		return rec, installChart(ctx, c, progress)
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

	progress("Applying namespaces and CRDs")
	if err := applyAll(ctx, k8s, stages[0]); err != nil {
		return err
	}
	progress("Waiting for the CRDs to be established")
	if err := waitCRDs(ctx, k8s, stages[0]); err != nil {
		return err
	}
	progress("Applying controllers and RBAC")
	if err := applyAll(ctx, k8s, stages[1]); err != nil {
		return err
	}
	progress("Waiting for the Deployments to be available")
	if err := waitDeployments(ctx, k8s, stages[1]); err != nil {
		return err
	}
	progress("Applying webhooks and custom resources")
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

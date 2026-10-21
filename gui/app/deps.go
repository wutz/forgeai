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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/wutz/forgeai/pkg/platform"
)

// The cluster components forgeai runs on. Their install manifests are
// embedded from deps/, rendered at the pinned versions by hack/gui-deps.sh.
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
	forgeaiCRDs = []string{"projects.forgeai.wutz.dev", "instancetypes.forgeai.wutz.dev"}
)

// depInfo describes a component on the admin page. Components without a
// bundle are only reported on.
type depInfo struct {
	name        string
	title       string
	description string
}

// depCatalog lists the components in install order: Kueue's TrainJob
// integration needs the Trainer CRDs, so Trainer comes first.
var depCatalog = []depInfo{
	{"trainer", "Kubeflow Trainer v2", "TrainJobs and the ClusterTrainingRuntimes, with JobSet"},
	{"kueue", "Kueue", "GPU quota queues that admit TrainJobs"},
	{"agent-sandbox", "agent-sandbox", "Sandbox controller and CRDs for the Sandboxes view"},
	{"forgeai", "forgeai operator", "Projects, instance types and the controllers; installed with make deploy-dev"},
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
	// Status is missing, degraded, unmanaged, outdated, installing or ready.
	Status      string `json:"status"`
	Message     string `json:"message"`
	Installable bool   `json:"installable"`
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
	if !dep.Installable && len(h.deployments) == 0 {
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
	if dep.Installable && dep.Installed == "" {
		dep.Status = "unmanaged"
		dep.Message = "Installed outside forgeai; installing replaces it with the pinned version"
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
	for _, info := range depCatalog {
		b, err := bundleByName(info.name)
		if err != nil {
			return nil, err
		}
		dep, err := probeDependency(ctx, c.k8s, info, b)
		if err != nil {
			return nil, err
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
func probeDependency(ctx context.Context, k8s client.Client, info depInfo, b *depBundle) (Dependency, error) {
	dep := Dependency{
		Name: info.name, Title: info.title, Description: info.description, Installable: b != nil,
	}
	var h depHealth
	crds := forgeaiCRDs
	if b != nil {
		dep.Pinned = b.Version
		crds = b.crdNames()
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
	assess(&dep, h)
	return dep, nil
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
	b, err := bundleByName(name)
	if err != nil {
		return err
	}
	if b == nil {
		return fmt.Errorf("%s is not installed from the GUI", name)
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
		err := installBundle(ctx, c.k8s, b, progress)
		if err == nil && name == "agent-sandbox" {
			progress("Restarting the forgeai operator to start the Sandbox controller")
			err = restartOperator(ctx, c.k8s)
		}
		// Release the component before the outcome is emitted: the view
		// refreshes on it and must not still see the install in progress.
		a.installs.finish(name)
		done := DependencyProgress{Name: name, Message: "Installed " + b.Version, Done: true}
		if err != nil {
			done = DependencyProgress{Name: name, Message: "Install failed", Done: true, Error: err.Error()}
		}
		runtime.EventsEmit(a.ctx, depProgressEvent, done)
	}()
	return nil
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

package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/sandbox"
)

// Sandbox is the view model of an agent sandbox (agents.x-k8s.io Sandbox).
type Sandbox struct {
	Name         string            `json:"name"`
	Phase        string            `json:"phase"`
	Message      string            `json:"message,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	Image        string            `json:"image"`
	Command      string            `json:"command,omitempty"`
	Env          map[string]string `json:"env"`
	InstanceType string            `json:"instanceType,omitempty"`
	RuntimeClass string            `json:"runtimeClass,omitempty"`
	Service      bool              `json:"service"`
	ServiceFQDN  string            `json:"serviceFQDN,omitempty"`
	PodIPs       []string          `json:"podIPs,omitempty"`
	NodeName     string            `json:"nodeName,omitempty"`
	PodName      string            `json:"podName,omitempty"`
	PodPhase     string            `json:"podPhase,omitempty"`
	// ContainerRunning is set when the sandbox container runs; ContainerReason
	// says why it does not otherwise (CrashLoopBackOff, ImagePullBackOff, ...).
	ContainerRunning bool   `json:"containerRunning"`
	ContainerReason  string `json:"containerReason,omitempty"`
	// PodUnknown is set when the pod could not be read this time; the container
	// state above is then not known, not stopped.
	PodUnknown   bool     `json:"podUnknown,omitempty"`
	CPU          string   `json:"cpu,omitempty"`
	Memory       string   `json:"memory,omitempty"`
	GPU          int64    `json:"gpu,omitempty"`
	Volumes      []string `json:"volumes,omitempty"`
	Stopped      bool     `json:"stopped"`
	IdleSince    string   `json:"idleSince,omitempty"`
	ShutdownTime string   `json:"shutdownTime,omitempty"`
	Created      string   `json:"created"`
	Age          string   `json:"age"`
}

// SandboxOptions are the platform defaults offered by the create dialog.
type SandboxOptions struct {
	DefaultImage        string   `json:"defaultImage"`
	Images              []string `json:"images"`
	RuntimeClasses      []string `json:"runtimeClasses"`
	DefaultRuntimeClass string   `json:"defaultRuntimeClass"`
	IdleTimeout         string   `json:"idleTimeout,omitempty"`
}

// ListSandboxes lists the sandboxes of the project, newest first.
func (a *App) ListSandboxes() ([]*Sandbox, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	items, err := listSandboxes(ctx, c)
	if err != nil {
		return nil, err
	}
	pods := listSandboxPods(ctx, c)
	out := make([]*Sandbox, 0, len(items))
	for i := range items {
		out = append(out, toSandbox(items[i], pods))
	}
	return out, nil
}

// GetSandbox returns one sandbox.
func (a *App) GetSandbox(name string) (*Sandbox, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	u, err := getSandbox(ctx, c, name)
	if err != nil {
		return nil, err
	}
	return toSandbox(u, sandboxPodOf(ctx, c)), nil
}

// SandboxOptions returns the platform settings the create dialog needs.
func (a *App) SandboxOptions() (*SandboxOptions, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	cfg, err := platform.Load(ctx, c.k8s, c.namespace())
	if err != nil {
		return nil, err
	}
	o := &SandboxOptions{
		DefaultImage:        cfg.Sandbox.DefaultImage,
		Images:              cfg.Sandbox.Images,
		RuntimeClasses:      cfg.Sandbox.RuntimeClasses,
		DefaultRuntimeClass: cfg.Sandbox.DefaultRuntimeClass,
	}
	if d := cfg.Sandbox.IdleTimeout.Duration; d > 0 {
		o.IdleTimeout = d.String()
	}
	return o, nil
}

// CreateSandbox renders a sandbox from the form and creates it.
func (a *App) CreateSandbox(f sandbox.Form) (*Sandbox, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	// A deleted sandbox's pod can outlive its object while it shuts down. A new
	// sandbox of the same name would then fail, since the controller finds the
	// pod owned by the old one, so the create waits for the pod to go. A live
	// sandbox of the name is a clash, not a shutdown.
	if f.Name != "" {
		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(sandbox.SandboxGVK)
		if err := c.k8s.Get(ctx, client.ObjectKey{Namespace: c.namespace(), Name: f.Name}, existing); err == nil {
			return nil, fmt.Errorf("sandbox %s already exists", f.Name)
		} else if !apierrors.IsNotFound(err) {
			return nil, err
		}
		if err := c.k8s.Get(ctx, client.ObjectKey{Namespace: c.namespace(), Name: f.Name}, &corev1.Pod{}); err == nil {
			return nil, fmt.Errorf("sandbox %s is still shutting down; try again once its pod is gone", f.Name)
		} else if !apierrors.IsNotFound(err) {
			return nil, err
		}
	}
	cfg, err := platform.Load(ctx, c.k8s, c.namespace())
	if err != nil {
		return nil, err
	}
	it, err := lookupInstanceType(ctx, c, f.InstanceType)
	if err != nil {
		return nil, err
	}
	if f.InstanceType != "" && it == nil {
		return nil, fmt.Errorf("instance type %q not found", f.InstanceType)
	}
	names := make([]string, 0, len(f.Volumes))
	for _, m := range f.Volumes {
		names = append(names, m.Volume)
	}
	if err := volumesClient(c).Mountable(ctx, names, 1); err != nil {
		return nil, err
	}
	regSecret, err := registrySecretName(ctx, c)
	if err != nil {
		return nil, err
	}
	u, err := sandbox.Render(&f, sandbox.Context{
		Namespace: c.namespace(), Project: c.Info.Project, InstanceType: it, Platform: cfg,
		RegistrySecret: regSecret,
	})
	if err != nil {
		return nil, err
	}
	if err := c.k8s.Create(ctx, u); err != nil {
		return nil, err
	}
	return toSandbox(u, sandboxPodOf(ctx, c)), nil
}

// StopSandbox suspends a sandbox: its pod is deleted, the object and its
// volumes are kept so it can be started again.
func (a *App) StopSandbox(name string) (*Sandbox, error) {
	return a.mutateSandbox(name, func(sb *sandbox.Sandbox) {
		sb.Spec.OperatingMode = sandbox.OperatingSuspended
		setAnnotation(sb, naming.AnnotationStopped, naming.LiteralTrue)
	})
}

// StartSandbox resumes a suspended sandbox and restarts its idle clock.
func (a *App) StartSandbox(name string) (*Sandbox, error) {
	return a.mutateSandbox(name, func(sb *sandbox.Sandbox) {
		sb.Spec.OperatingMode = sandbox.OperatingRunning
		// An expired sandbox would expire again at once; starting it is the user's
		// choice to run on, so its shutdown time is cleared.
		sandbox.ReopenElapsed(sb, time.Now())
		delete(sb.Annotations, naming.AnnotationStopped)
		delete(sb.Annotations, naming.AnnotationIdleSince)
		setAnnotation(sb, naming.AnnotationLastActive, time.Now().UTC().Format(time.RFC3339))
	})
}

// KeepSandboxAwake resets the idle clock: the GUI calls it while the user
// works in the sandbox view, so a sandbox in use is not suspended.
func (a *App) KeepSandboxAwake(name string) error {
	_, err := a.mutateSandbox(name, func(sb *sandbox.Sandbox) {
		setAnnotation(sb, naming.AnnotationLastActive, time.Now().UTC().Format(time.RFC3339))
	})
	return err
}

// DeleteSandbox deletes a sandbox, its pod, its service and the volumes the
// sandbox created from its volume claim templates.
func (a *App) DeleteSandbox(name string) error {
	c, err := a.current()
	if err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()
	u, err := getSandbox(ctx, c, name)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return c.k8s.Delete(ctx, u)
}

// SandboxLogs returns the last tailLines of the sandbox container log.
func (a *App) SandboxLogs(name string, tailLines int64) (string, error) {
	c, err := a.current()
	if err != nil {
		return "", err
	}
	ctx, cancel := a.call()
	defer cancel()
	u, err := getSandbox(ctx, c, name)
	if err != nil {
		return "", err
	}
	sb := &sandbox.Sandbox{}
	if err := sandbox.FromUnstructured(u.Object, sb); err != nil {
		return "", err
	}
	pod := &corev1.Pod{}
	if err := c.k8s.Get(ctx, types.NamespacedName{Namespace: c.namespace(), Name: sandbox.PodName(sb)}, pod); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("sandbox %s has no pod: it is stopped or still starting", name)
		}
		return "", err
	}
	if tailLines <= 0 {
		tailLines = 500
	}
	opts := &corev1.PodLogOptions{TailLines: &tailLines, Container: sandbox.ContainerName}
	rc, err := c.core.CoreV1().Pods(c.namespace()).GetLogs(pod.Name, opts).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()
	return readTail(rc, logTailLimit)
}

// SandboxTemplates lists the SandboxTemplates of the project, for the GUI.
func (a *App) SandboxTemplates() ([]SandboxTemplate, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(sandbox.SandboxTemplateGVK.GroupVersion().WithKind("SandboxTemplateList"))
	if err := c.k8s.List(ctx, list, client.InNamespace(c.namespace())); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return []SandboxTemplate{}, nil
		}
		return nil, err
	}
	out := make([]SandboxTemplate, 0, len(list.Items))
	for i := range list.Items {
		t := &sandbox.SandboxTemplate{}
		if err := sandbox.FromUnstructured(list.Items[i].Object, t); err != nil {
			out = append(out, SandboxTemplate{Name: list.Items[i].GetName(), Error: err.Error()})
			continue
		}
		out = append(out, toSandboxTemplate(t))
	}
	return out, nil
}

// CreateSandboxTemplate saves a SandboxTemplate from a form.
func (a *App) CreateSandboxTemplate(f sandbox.TemplateForm) (*SandboxTemplate, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	cfg, err := platform.Load(ctx, c.k8s, c.namespace())
	if err != nil {
		return nil, err
	}
	u, err := sandbox.RenderTemplate(&f, sandbox.Context{Namespace: c.namespace(), Project: c.Info.Project, Platform: cfg})
	if err != nil {
		return nil, err
	}
	if err := c.k8s.Create(ctx, u); err != nil {
		return nil, err
	}
	// The controller updates a new SandboxTemplate right after it is created, so
	// the version Create returned is already stale: read the stored object.
	if err := c.k8s.Get(ctx, client.ObjectKeyFromObject(u), u); err != nil {
		return nil, err
	}
	t := &sandbox.SandboxTemplate{}
	if err := sandbox.FromUnstructured(u.Object, t); err != nil {
		return nil, err
	}
	out := toSandboxTemplate(t)
	return &out, nil
}

// UpdateSandboxTemplate replaces the form of an existing SandboxTemplate. The
// name is the template's identity and does not change. Sandboxes already
// created from it are not affected.
func (a *App) UpdateSandboxTemplate(name, resourceVersion string, f sandbox.TemplateForm) (*SandboxTemplate, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	cur := &unstructured.Unstructured{}
	cur.SetGroupVersionKind(sandbox.SandboxTemplateGVK)
	cur.SetNamespace(c.namespace())
	cur.SetName(name)
	if err := c.k8s.Get(ctx, client.ObjectKeyFromObject(cur), cur); err != nil {
		return nil, err
	}
	if cur.GetLabels()[naming.LabelManagedBy] != naming.ManagedBy {
		return nil, fmt.Errorf("sandbox template %s was not created by forgeai and cannot be edited here", name)
	}
	cfg, err := platform.Load(ctx, c.k8s, c.namespace())
	if err != nil {
		return nil, err
	}
	f.Name = name
	u, err := sandbox.RenderTemplate(&f, sandbox.Context{Namespace: c.namespace(), Project: c.Info.Project, Platform: cfg})
	if err != nil {
		return nil, err
	}
	// The stored object keeps its metadata (annotations, owner references and
	// labels set by others); the form replaces the spec and the labels it
	// renders. Updating against the version the form was loaded at makes a
	// concurrent change fail with a conflict, instead of being overwritten.
	if resourceVersion == "" {
		return nil, fmt.Errorf("sandbox template %s has no loaded version to update from; reload it and try again", name)
	}
	upd := cur.DeepCopy()
	upd.SetResourceVersion(resourceVersion)
	upd.Object["spec"] = u.Object["spec"]
	labels := upd.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	for k, v := range u.GetLabels() {
		labels[k] = v
	}
	upd.SetLabels(labels)
	if err := c.k8s.Update(ctx, upd); err != nil {
		return nil, err
	}
	t := &sandbox.SandboxTemplate{}
	if err := sandbox.FromUnstructured(upd.Object, t); err != nil {
		return nil, err
	}
	out := toSandboxTemplate(t)
	return &out, nil
}

// DeleteSandboxTemplate deletes a SandboxTemplate. Sandboxes already created
// from it are not affected.
func (a *App) DeleteSandboxTemplate(name string) error {
	c, err := a.current()
	if err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(sandbox.SandboxTemplateGVK)
	u.SetNamespace(c.namespace())
	u.SetName(name)
	if err := c.k8s.Get(ctx, client.ObjectKeyFromObject(u), u); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if u.GetLabels()[naming.LabelManagedBy] != naming.ManagedBy {
		return fmt.Errorf("sandbox template %s was not created by forgeai and cannot be deleted here", name)
	}
	err = c.k8s.Delete(ctx, u)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func toSandboxTemplate(t *sandbox.SandboxTemplate) SandboxTemplate {
	st := SandboxTemplate{
		Name:            t.Name,
		Description:     t.Annotations[naming.AnnotationDescription],
		Created:         t.CreationTimestamp.UTC().Format(time.RFC3339),
		Form:            sandbox.TemplateFormOf(t),
		ResourceVersion: t.ResourceVersion,
	}
	for _, ctr := range t.Spec.PodTemplate.Spec.Containers {
		st.Images = append(st.Images, ctr.Image)
	}
	return st
}

// SandboxTemplate is a SandboxTemplate as listed in the GUI.
type SandboxTemplate struct {
	Name            string                `json:"name"`
	Description     string                `json:"description,omitempty"`
	Images          []string              `json:"images,omitempty"`
	Created         string                `json:"created"`
	Form            *sandbox.TemplateForm `json:"form"`
	ResourceVersion string                `json:"resourceVersion,omitempty"`
	// Error is set for a template the app cannot read; it is listed so it can
	// still be deleted.
	Error string `json:"error,omitempty"`
}

// mutateSandbox reads, changes and updates a sandbox. The heartbeat writes the
// same object, so an update can meet a conflict; the change is then applied to
// the object as it is now.
func (a *App) mutateSandbox(name string, fn func(*sandbox.Sandbox)) (*Sandbox, error) {
	var out *Sandbox
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var err error
		out, err = a.mutateSandboxOnce(name, fn)
		return err
	})
	return out, err
}

// mutateSandboxOnce is one read-change-update attempt of mutateSandbox.
func (a *App) mutateSandboxOnce(name string, fn func(*sandbox.Sandbox)) (*Sandbox, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	u, err := getSandbox(ctx, c, name)
	if err != nil {
		return nil, err
	}
	sb := &sandbox.Sandbox{}
	if err := sandbox.FromUnstructured(u.Object, sb); err != nil {
		return nil, err
	}
	if sb.Annotations == nil {
		sb.Annotations = map[string]string{}
	}
	mode := sb.Spec.OperatingMode
	fn(sb)
	// Only the annotations and the operating mode are written back. A typed
	// round trip would drop the fields the Go model does not carry.
	out := u.DeepCopy()
	out.SetAnnotations(sb.Annotations)
	if sb.Spec.OperatingMode != mode {
		if err := unstructured.SetNestedField(out.Object, string(sb.Spec.OperatingMode), "spec", "operatingMode"); err != nil {
			return nil, err
		}
	}
	if err := c.k8s.Update(ctx, out); err != nil {
		return nil, err
	}
	return toSandbox(out, sandboxPodOf(ctx, c)), nil
}

func listSandboxes(ctx context.Context, c *Connection) ([]*unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(sandbox.SandboxGVK.GroupVersion().WithKind("SandboxList"))
	if err := c.k8s.List(ctx, list, client.InNamespace(c.namespace())); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]*unstructured.Unstructured, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, &list.Items[i])
	}
	// Newest first.
	slices.SortStableFunc(out, func(a, b *unstructured.Unstructured) int {
		return b.GetCreationTimestamp().Time.Compare(a.GetCreationTimestamp().Time)
	})
	return out, nil
}

func getSandbox(ctx context.Context, c *Connection, name string) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(sandbox.SandboxGVK)
	if err := c.k8s.Get(ctx, client.ObjectKey{Namespace: c.namespace(), Name: name}, u); err != nil {
		return nil, err
	}
	return u, nil
}

// podLookup returns the pod named name: nil when there is none, and unknown when
// the pod could not be read at all.
type podLookup func(name string) (pod *corev1.Pod, unknown bool)

// sandboxPodOf reads one pod with a GET; a single sandbox read needs no list.
func sandboxPodOf(ctx context.Context, c *Connection) podLookup {
	return func(name string) (*corev1.Pod, bool) {
		pod := &corev1.Pod{}
		if err := c.k8s.Get(ctx, types.NamespacedName{Namespace: c.namespace(), Name: name}, pod); err != nil {
			return nil, !apierrors.IsNotFound(err)
		}
		return pod, false
	}
}

// listSandboxPods lists the namespace's pods once, so a poll of the whole list
// costs one read rather than one per sandbox.
func listSandboxPods(ctx context.Context, c *Connection) podLookup {
	list := &corev1.PodList{}
	if err := c.k8s.List(ctx, list, client.InNamespace(c.namespace())); err != nil {
		return func(string) (*corev1.Pod, bool) { return nil, true }
	}
	byName := make(map[string]*corev1.Pod, len(list.Items))
	for i := range list.Items {
		byName[list.Items[i].Name] = &list.Items[i]
	}
	return func(name string) (*corev1.Pod, bool) { return byName[name], false }
}

// toSandbox derives the view model from the sandbox and its pod.
func toSandbox(u *unstructured.Unstructured, pods podLookup) *Sandbox {
	sb := &sandbox.Sandbox{}
	if err := sandbox.FromUnstructured(u.Object, sb); err != nil {
		return &Sandbox{Name: u.GetName(), Phase: string(sandbox.PhaseFailed), Message: err.Error()}
	}
	phase, msg := sandbox.PhaseOf(sb)
	out := &Sandbox{
		Name:         sb.Name,
		Phase:        string(phase),
		Message:      sandbox.TrimMessages(msg),
		Reason:       sandbox.PhaseReason(sb),
		InstanceType: sb.Labels[naming.LabelInstanceType],
		RuntimeClass: ptr(sb.Spec.PodTemplate.Spec.RuntimeClassName),
		Service:      sb.Spec.Service != nil && *sb.Spec.Service,
		ServiceFQDN:  sb.Status.ServiceFQDN,
		PodIPs:       sb.Status.PodIPs,
		NodeName:     sb.Status.NodeName,
		Stopped:      sandbox.IsSandboxPaused(sb),
		IdleSince:    sb.Annotations[naming.AnnotationIdleSince],
		Env:          map[string]string{},
		Created:      sb.CreationTimestamp.UTC().Format(time.RFC3339),
		Age:          shortDur(time.Since(sb.CreationTimestamp.Time)),
	}
	if sb.Spec.ShutdownTime != nil && !sb.Spec.ShutdownTime.IsZero() {
		out.ShutdownTime = sb.Spec.ShutdownTime.UTC().Format(time.RFC3339)
	}
	for _, ctr := range sb.Spec.PodTemplate.Spec.Containers {
		if ctr.Name != sandbox.ContainerName {
			continue
		}
		out.Image = ctr.Image
		out.Command = sandbox.ScriptOf(ctr.Command)
		for _, e := range ctr.Env {
			out.Env[e.Name] = e.Value
		}
		for _, m := range ctr.VolumeMounts {
			if m.Name != "dshm" {
				out.Volumes = append(out.Volumes, strings.TrimPrefix(m.Name, "vol-"))
			}
		}
	}
	if cpu, mem := sandbox.CPUAndMemory(sb); !cpu.IsZero() || !mem.IsZero() {
		out.CPU, out.Memory = cpu.String(), mem.String()
	}
	out.GPU = sandbox.GPU(sb)
	pod, unknown := pods(sandbox.PodName(sb))
	out.PodUnknown = unknown
	if pod != nil {
		out.PodName, out.PodPhase = pod.Name, string(pod.Status.Phase)
		out.ContainerRunning, out.ContainerReason = sandboxContainerState(pod)
		if out.NodeName == "" {
			out.NodeName = pod.Spec.NodeName
		}
	}
	return out
}

// sandboxContainerState reports whether the sandbox container runs. A pod can
// stay Running while its container crash-loops, so the pod phase is not
// enough; when the container does not run, the reason says why it waits.
func sandboxContainerState(pod *corev1.Pod) (bool, string) {
	for _, st := range pod.Status.ContainerStatuses {
		if st.Name != sandbox.ContainerName {
			continue
		}
		switch {
		case st.State.Running != nil:
			return true, ""
		case st.State.Waiting != nil:
			return false, st.State.Waiting.Reason
		case st.State.Terminated != nil:
			return false, st.State.Terminated.Reason
		}
	}
	return false, ""
}

func ptr[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// registrySecretName is the pull secret of the project, or "" when the
// registry is not set up.
func registrySecretName(ctx context.Context, c *Connection) (string, error) {
	sec := &corev1.Secret{}
	key := client.ObjectKey{Namespace: c.namespace(), Name: naming.RegistrySecretName}
	if err := c.k8s.Get(ctx, key, sec); err != nil {
		// A missing secret means no pull secret; any other failure must not
		// quietly create a sandbox that cannot pull its private images.
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return naming.RegistrySecretName, nil
}

package app

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
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
	CPU          string            `json:"cpu,omitempty"`
	Memory       string            `json:"memory,omitempty"`
	GPU          int64             `json:"gpu,omitempty"`
	Volumes      []string          `json:"volumes,omitempty"`
	Stopped      bool              `json:"stopped"`
	IdleSince    string            `json:"idleSince,omitempty"`
	ShutdownTime string            `json:"shutdownTime,omitempty"`
	Created      string            `json:"created"`
	Age          string            `json:"age"`
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
	out := make([]*Sandbox, 0, len(items))
	for i := range items {
		out = append(out, toSandbox(ctx, c, items[i]))
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
	return toSandbox(ctx, c, u), nil
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
	return toSandbox(ctx, c, u), nil
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
	b, err := io.ReadAll(io.LimitReader(rc, 4<<20))
	return string(b), err
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
func (a *App) UpdateSandboxTemplate(name string, f sandbox.TemplateForm) (*SandboxTemplate, error) {
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
	// The resource version makes the update fail if someone changed the
	// template since it was loaded into the form.
	u.SetResourceVersion(cur.GetResourceVersion())
	if err := c.k8s.Update(ctx, u); err != nil {
		return nil, err
	}
	t := &sandbox.SandboxTemplate{}
	if err := sandbox.FromUnstructured(u.Object, t); err != nil {
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
	err = c.k8s.Delete(ctx, u)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func toSandboxTemplate(t *sandbox.SandboxTemplate) SandboxTemplate {
	st := SandboxTemplate{
		Name:        t.Name,
		Description: t.Annotations[naming.AnnotationDescription],
		Created:     t.CreationTimestamp.UTC().Format(time.RFC3339),
		Form:        sandbox.TemplateFormOf(t),
	}
	for _, ctr := range t.Spec.PodTemplate.Spec.Containers {
		st.Images = append(st.Images, ctr.Image)
	}
	return st
}

// SandboxTemplate is a SandboxTemplate as listed in the GUI.
type SandboxTemplate struct {
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Images      []string              `json:"images,omitempty"`
	Created     string                `json:"created"`
	Form        *sandbox.TemplateForm `json:"form"`
}

// mutateSandbox reads, changes and updates a sandbox.
func (a *App) mutateSandbox(name string, fn func(*sandbox.Sandbox)) (*Sandbox, error) {
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
	fn(sb)
	out, err := sandbox.ToUnstructured(sb)
	if err != nil {
		return nil, err
	}
	out.SetResourceVersion(u.GetResourceVersion())
	if err := c.k8s.Update(ctx, out); err != nil {
		return nil, err
	}
	return toSandbox(ctx, c, out), nil
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

// toSandbox derives the view model from the sandbox and its pod.
func toSandbox(ctx context.Context, c *Connection, u *unstructured.Unstructured) *Sandbox {
	sb := &sandbox.Sandbox{}
	if err := sandbox.FromUnstructured(u.Object, sb); err != nil {
		return &Sandbox{Name: u.GetName(), Phase: string(sandbox.PhaseFailed), Message: err.Error()}
	}
	phase, msg := sandbox.PhaseOf(sb)
	out := &Sandbox{
		Name:         sb.Name,
		Phase:        string(phase),
		Message:      sandbox.TrimMessages(msg),
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
	pod := &corev1.Pod{}
	name := sandbox.PodName(sb)
	if err := c.k8s.Get(ctx, types.NamespacedName{Namespace: c.namespace(), Name: name}, pod); err == nil {
		out.PodName, out.PodPhase = pod.Name, string(pod.Status.Phase)
		if out.NodeName == "" {
			out.NodeName = pod.Spec.NodeName
		}
	}
	return out
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

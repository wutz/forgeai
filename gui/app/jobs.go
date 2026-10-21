package app

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/training"
)

// Job is the view model of a TrainJob. Retries stay under their original job
// group (Group); RetryOf is empty on the first attempt.
type Job struct {
	Name       string            `json:"name"`
	Group      string            `json:"group"`
	RetryOf    string            `json:"retryOf,omitempty"`
	Template   string            `json:"template,omitempty"`
	Image      string            `json:"image"`
	Command    string            `json:"command"`
	Env        map[string]string `json:"env"`
	Instance   string            `json:"instanceType"`
	Workers    int32             `json:"workers"`
	Phase      string            `json:"phase"`
	Message    string            `json:"message,omitempty"`
	Reason     string            `json:"failureReason,omitempty"`
	Stopped    bool              `json:"stopped"`
	Superseded bool              `json:"superseded"`
	Ready      int32             `json:"ready"`
	CPU        string            `json:"cpu,omitempty"`
	GPUClass   string            `json:"gpuClass,omitempty"`
	GPUCount   int32             `json:"gpuCount,omitempty"`
	Created    string            `json:"created"`
	Age        string            `json:"age"`
	Duration   string            `json:"duration,omitempty"`
	WorkerPods []Worker          `json:"workerPods"`
	Volumes    []string          `json:"volumes,omitempty"`
	MaxRetries int32             `json:"maxRetries,omitempty"`
	RetryOn    string            `json:"retryOn,omitempty"`
	RetriedAs  string            `json:"retriedAs,omitempty"`
	Retries    []*Job            `json:"retries"`
}

// Worker is one worker pod of a job.
type Worker struct {
	Index    int32  `json:"index"`
	PodName  string `json:"podName"`
	NodeName string `json:"nodeName"`
	Phase    string `json:"phase"`
	Message  string `json:"message,omitempty"`
}

// ListJobs lists the TrainJobs of the project, newest first. Retries created
// by the operator's failure handling are nested under their group leader.
func (a *App) ListJobs() ([]*Job, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	items, err := listTrainJobs(ctx, c)
	if err != nil {
		return nil, err
	}
	out := make([]*Job, 0, len(items))
	for i := range items {
		j, err := toJob(ctx, c, &items[i])
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	group(out)
	slices.SortFunc(out, func(a, b *Job) int { return strings.Compare(b.Created, a.Created) })
	return out, nil
}

// GetJob returns the named job. A group leader comes with every retry of its
// group nested under Retries; a retry is returned on its own, so the detail
// view can drill into it from the list.
func (a *App) GetJob(name string) (*Job, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	items, err := listTrainJobs(ctx, c)
	if err != nil {
		return nil, err
	}
	var self *kf.TrainJob
	for i := range items {
		if items[i].Name == name {
			self = &items[i]
			break
		}
	}
	if self == nil {
		return nil, fmt.Errorf("job %q not found", name)
	}
	j, err := toJob(ctx, c, self)
	if err != nil {
		return nil, err
	}
	if j.Group != j.Name {
		return j, nil
	}
	for i := range items {
		if items[i].Name == name || groupOf(&items[i]) != name {
			continue
		}
		r, err := toJob(ctx, c, &items[i])
		if err != nil {
			return nil, err
		}
		j.Retries = append(j.Retries, r)
	}
	slices.SortFunc(j.Retries, func(a, b *Job) int { return strings.Compare(a.Name, b.Name) })
	return j, nil
}

// group nests retries (job-group != own name) under their group leader.
func group(jobs []*Job) {
	leaders := map[string]*Job{}
	for _, j := range jobs {
		if j.Group == j.Name {
			leaders[j.Name] = j
		}
	}
	kept := jobs[:0]
	for _, j := range jobs {
		leader, ok := leaders[j.Group]
		if ok && j.Group != j.Name {
			leader.Retries = append(leader.Retries, j)
			continue
		}
		kept = append(kept, j)
	}
	slices.SortFunc(kept, func(a, b *Job) int { return strings.Compare(a.Name, b.Name) })
	for _, j := range kept {
		slices.SortFunc(j.Retries, func(a, b *Job) int { return strings.Compare(a.Name, b.Name) })
	}
}

// CreateJob submits a TrainJob rendered from the form.
func (a *App) CreateJob(f training.Form) (*Job, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	tj, err := a.renderTrainJob(ctx, c, f)
	if err != nil {
		return nil, err
	}
	if tj, err = createTrainJob(ctx, c, tj); err != nil {
		return nil, err
	}
	return toJob(ctx, c, tj)
}

// DeleteJob deletes a TrainJob (pods go with it, Kueue releases the quota).
func (a *App) DeleteJob(name string) error {
	c, err := a.current()
	if err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()
	return c.k8s.Delete(ctx, trainJobStub(c, name))
}

// StopJob stops a running or queued job: the operator deactivates its Kueue
// workload, which releases the pods while the job spec is kept.
func (a *App) StopJob(name string) (*Job, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	return a.mutateJob(c, name, func(tj *kf.TrainJob) {
		setAnnotation(tj, training.AnnotationStopped, naming.LiteralTrue)
	})
}

// StartJob resumes a stopped job where it left off.
func (a *App) StartJob(name string) (*Job, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	return a.mutateJob(c, name, func(tj *kf.TrainJob) {
		delete(tj.Annotations, training.AnnotationStopped)
	})
}

// RerunJob submits a fresh job from the form of an existing one.
func (a *App) RerunJob(name, newName string) (*Job, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	if newName == "" {
		newName = name + "-r2"
	}
	ctx, cancel := a.call()
	defer cancel()
	src, err := getTrainJob(ctx, c, name)
	if err != nil {
		return nil, err
	}
	f, err := rerunForm(src, newName)
	if err != nil {
		return nil, err
	}
	tj, err := a.renderTrainJob(ctx, c, f)
	if err != nil {
		return nil, err
	}
	if tj, err = createTrainJob(ctx, c, tj); err != nil {
		return nil, err
	}
	return toJob(ctx, c, tj)
}

// rerunForm is the form of a new job copied from src. It keeps src's job
// template, so the rerun stays grouped with the template it came from.
func rerunForm(src *kf.TrainJob, name string) (training.Form, error) {
	f, err := training.TrainJobForm(src)
	if err != nil {
		return training.Form{}, err
	}
	f.Name = name
	return *f, nil
}

// Logs returns the last tailLines of a worker pod log.
func (a *App) Logs(name string, worker int32, tailLines int64) (string, error) {
	c, err := a.current()
	if err != nil {
		return "", err
	}
	ctx, cancel := a.call()
	defer cancel()
	pods, err := jobPods(ctx, c, name)
	if err != nil {
		return "", err
	}
	pod := ""
	for i := range pods {
		if int32(training.WorkerIndex(&pods[i])) == worker {
			pod = pods[i].Name
		}
	}
	if pod == "" {
		return "", fmt.Errorf("worker %d has no pod", worker)
	}
	if tailLines <= 0 {
		tailLines = 500
	}
	rc, err := c.core.CoreV1().Pods(c.namespace()).GetLogs(pod, &corev1.PodLogOptions{TailLines: &tailLines}).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(io.LimitReader(rc, 4<<20))
	return string(b), err
}

func (a *App) mutateJob(c *Connection, name string, fn func(*kf.TrainJob)) (*Job, error) {
	ctx, cancel := a.call()
	defer cancel()
	tj, err := getTrainJob(ctx, c, name)
	if err != nil {
		return nil, err
	}
	fn(tj)
	u, err := encodeTrainJob(tj)
	if err != nil {
		return nil, err
	}
	if err := c.k8s.Update(ctx, u); err != nil {
		return nil, err
	}
	return toJob(ctx, c, tj)
}

func setAnnotation(obj metav1.Object, key, value string) {
	ann := obj.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann[key] = value
	obj.SetAnnotations(ann)
}

func jobPods(ctx context.Context, c *Connection, name string) ([]corev1.Pod, error) {
	pods := &corev1.PodList{}
	if err := c.k8s.List(ctx, pods, client.InNamespace(c.namespace()),
		client.MatchingLabels{kf.LabelJobSetName: name}); err != nil {
		return nil, err
	}
	return pods.Items, nil
}

// renderTrainJob builds a TrainJob from a form, resolving the instance type,
// platform config and registry pull secret from the cluster.
func (a *App) renderTrainJob(ctx context.Context, c *Connection, f training.Form) (*kf.TrainJob, error) {
	if f.Workers < 1 {
		f.Workers = 1
	}
	it := &v1.InstanceType{}
	if err := c.k8s.Get(ctx, client.ObjectKey{Name: f.InstanceType}, it); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("instance type %q not found", f.InstanceType)
		}
		return nil, err
	}
	cfg, err := platform.Load(ctx, c.k8s, c.namespace())
	if err != nil {
		return nil, err
	}
	// Pull through the project robot account when the registry is set up.
	registrySecret := ""
	if cfg.Registry.Enabled() {
		sec := &corev1.Secret{}
		secKey := client.ObjectKey{Namespace: c.namespace(), Name: naming.RegistrySecretName}
		if err := c.k8s.Get(ctx, secKey, sec); err == nil {
			registrySecret = naming.RegistrySecretName
		}
	}
	names := make([]string, 0, len(f.Volumes))
	for _, m := range f.Volumes {
		names = append(names, m.Volume)
	}
	if err := volumesClient(c).Mountable(ctx, names, f.Workers); err != nil {
		return nil, err
	}
	return training.TrainJob(&f, training.Context{
		Namespace: c.namespace(), InstanceType: it, Config: cfg, RegistrySecret: registrySecret,
	})
}

// toJob derives the view model: phase from conditions and pods, form fields
// from the trainer overrides and the forgeai runtime patch.
func toJob(ctx context.Context, c *Connection, tj *kf.TrainJob) (*Job, error) {
	pods, err := jobPods(ctx, c, tj.Name)
	if err != nil {
		return nil, err
	}
	phase, msg := training.JobPhase(tj, pods)
	out := &Job{
		Name:       tj.Name,
		Group:      groupOf(tj),
		Template:   tj.Labels[naming.LabelTemplate],
		Instance:   tj.Labels[naming.LabelInstanceType],
		Phase:      string(phase),
		Message:    msg,
		Reason:     tj.Annotations[naming.AnnotationFailureReason],
		RetriedAs:  tj.Annotations[naming.AnnotationRetriedAs],
		Superseded: tj.Annotations[naming.AnnotationRetriedAs] != "",
		Stopped:    phase == training.PhaseStopped,
		Workers:    1,
		Env:        map[string]string{},
		Created:    tj.CreationTimestamp.UTC().Format(time.RFC3339),
		Age:        shortDur(time.Since(tj.CreationTimestamp.Time)),
	}
	if n, err := strconv.ParseInt(tj.Annotations[naming.AnnotationMaxRetries], 10, 32); err == nil {
		out.MaxRetries = int32(n)
	}
	out.RetryOn = tj.Annotations[naming.AnnotationRetryOn]
	if t := tj.Spec.Trainer; t != nil {
		if t.NumNodes != nil {
			out.Workers = *t.NumNodes
		}
		out.Image = t.Image
		if len(t.Command) >= 3 && t.Command[0] == "/bin/bash" {
			out.Command = strings.Join(t.Command[2:], "\n")
		} else {
			out.Command = strings.Join(t.Command, " ")
		}
		for _, e := range t.Env {
			out.Env[e.Name] = e.Value
		}
		if r := t.ResourcesPerNode; r != nil {
			out.CPU = r.Requests.Cpu().String()
		}
	}
	if g := tj.Labels[naming.LabelJobGroup]; g != "" && g != tj.Name {
		out.RetryOf = g
	}
	if p := patchOf(tj); p != nil {
		claims := map[string]bool{}
		for i := range p.Volumes {
			if v := p.Volumes[i].PersistentVolumeClaim; v != nil {
				claims[v.ClaimName] = true
			}
		}
		out.Volumes = slices.Sorted(maps.Keys(claims))
	}
	slices.SortStableFunc(pods, func(a, b corev1.Pod) int {
		return training.WorkerIndex(&a) - training.WorkerIndex(&b)
	})
	out.WorkerPods = make([]Worker, 0, len(pods))
	for i := range pods {
		p := &pods[i]
		out.WorkerPods = append(out.WorkerPods, Worker{
			Index: int32(training.WorkerIndex(p)), PodName: p.Name, NodeName: p.Spec.NodeName,
			Phase: string(p.Status.Phase), Message: podMessage(p),
		})
		if p.Status.Phase == corev1.PodRunning && p.DeletionTimestamp.IsZero() {
			out.Ready++
		}
	}
	if it, err := lookupInstanceType(ctx, c, out.Instance); err == nil && it != nil {
		out.GPUClass = it.Spec.GPU.Class
		out.GPUCount = it.Spec.GPU.Count * out.Workers
		out.CPU = it.Spec.CPU.String()
	}
	return out, nil
}

// podMessage explains a pod that is not running.
func podMessage(p *corev1.Pod) string {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse {
			return strings.TrimSuffix(c.Reason+": "+c.Message, ": ")
		}
	}
	for _, cs := range p.Status.ContainerStatuses {
		if w := cs.State.Waiting; w != nil && w.Reason != "ContainerCreating" {
			return strings.TrimSuffix(w.Reason+": "+w.Message, ": ")
		}
	}
	return ""
}

func groupOf(tj *kf.TrainJob) string {
	if g := tj.Labels[naming.LabelJobGroup]; g != "" {
		return g
	}
	return tj.Name
}

// patchOf returns the forgeai runtime patch holding placement and volumes, or
// nil when the job carries none. A hand-edited TrainJob may leave any level
// empty; that must not fail the whole job list.
func patchOf(tj *kf.TrainJob) *kf.PodSpecPatch {
	for i := range tj.Spec.RuntimePatches {
		p := &tj.Spec.RuntimePatches[i]
		if p.Manager != kf.PatchManager || p.TrainingRuntimeSpec == nil ||
			p.TrainingRuntimeSpec.Template == nil || p.TrainingRuntimeSpec.Template.Spec == nil {
			continue
		}
		rjs := p.TrainingRuntimeSpec.Template.Spec.ReplicatedJobs
		if len(rjs) == 0 || rjs[0].Template == nil || rjs[0].Template.Spec == nil || rjs[0].Template.Spec.Template == nil {
			return nil
		}
		return rjs[0].Template.Spec.Template.Spec
	}
	return nil
}

func lookupInstanceType(ctx context.Context, c *Connection, name string) (*v1.InstanceType, error) {
	if name == "" {
		return nil, nil
	}
	it := &v1.InstanceType{}
	if err := c.k8s.Get(ctx, client.ObjectKey{Name: name}, it); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	return it, nil
}

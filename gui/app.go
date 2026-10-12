package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/jobs"
	"github.com/scclabs/forgeai/pkg/platform"
)

// App is bound to the frontend. All calls go straight to the Kubernetes API
// of the selected kubeconfig context; the forgeai operator does the rest.
type App struct {
	ctx context.Context

	mu   sync.RWMutex
	conn *connection
}

type connection struct {
	info ConnInfo
	jobs *jobs.Client
	core kubernetes.Interface
}

// NewApp creates the bound application.
func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// KubeContext is a kubeconfig context.
type KubeContext struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Current   bool   `json:"current"`
}

// ConnInfo describes the active connection.
type ConnInfo struct {
	Context   string `json:"context"`
	Server    string `json:"server"`
	Namespace string `json:"namespace"`
	UserID    string `json:"userID"`
}

// ResourceInfo is a resource model (flavor) from the platform config.
type ResourceInfo struct {
	ID       string  `json:"id"`
	GPUs     int     `json:"gpus"`
	GPUModel string  `json:"gpuModel"`
	CPU      float64 `json:"cpu"`
	MemoryGB float64 `json:"memoryGB"`
}

// Worker is one worker pod of a job.
type Worker struct {
	Index       int32    `json:"index"`
	PodName     string   `json:"podName"`
	NodeName    string   `json:"nodeName"`
	PodIP       string   `json:"podIP"`
	Phase       string   `json:"phase"`
	SSHUser     string   `json:"sshUser"`
	JupyterURLs []string `json:"jupyterURLs"`
}

// Job is the view model of a TrainingJob.
type Job struct {
	Name        string            `json:"name"`
	JobID       string            `json:"jobID"`
	UserID      string            `json:"userID"`
	Image       string            `json:"image"`
	Command     string            `json:"command"`
	ResourceID  string            `json:"resourceID"`
	PoolID      string            `json:"poolID"`
	Workers     int32             `json:"workers"`
	Env         map[string]string `json:"env"`
	Phase       string            `json:"phase"`
	Message     string            `json:"message"`
	Ready       int32             `json:"ready"`
	Succeeded   int32             `json:"succeeded"`
	Failed      int32             `json:"failed"`
	Created     string            `json:"created"`
	Started     string            `json:"started"`
	Completed   string            `json:"completed"`
	Age         string            `json:"age"`
	Duration    string            `json:"duration"`
	WorkerPods  []Worker          `json:"workerPods"`
	Suspended   bool              `json:"suspended"`
	RestartGen  int64             `json:"restartGeneration"`
	TrainJob    string            `json:"trainJob"`
	SSHEnabled  bool              `json:"sshEnabled"`
	JupyterOn   bool              `json:"jupyterEnabled"`
	VolumeCount int               `json:"volumeCount"`
}

// JobForm is the create form.
type JobForm struct {
	JobID      string            `json:"jobID"`
	UserID     string            `json:"userID"`
	Image      string            `json:"image"`
	Command    string            `json:"command"`
	ResourceID string            `json:"resourceID"`
	PoolID     string            `json:"poolID"`
	Workers    int32             `json:"workers"`
	Env        map[string]string `json:"env"`
	SSH        bool              `json:"ssh"`
	Jupyter    bool              `json:"jupyter"`
}

var errNotConnected = errors.New("not connected: select a kubeconfig context first")

func loadingRules() *clientcmd.ClientConfigLoadingRules {
	return clientcmd.NewDefaultClientConfigLoadingRules()
}

// Contexts lists the contexts of the default kubeconfig ($KUBECONFIG or ~/.kube/config).
func (a *App) Contexts() ([]KubeContext, error) {
	raw, err := loadingRules().Load()
	if err != nil {
		return nil, err
	}
	out := make([]KubeContext, 0, len(raw.Contexts))
	for name, c := range raw.Contexts {
		out = append(out, KubeContext{Name: name, Cluster: c.Cluster, Namespace: c.Namespace, Current: name == raw.CurrentContext})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Connect switches to a kubeconfig context, a job namespace and an optional
// user filter (empty lists jobs of all users).
func (a *App) Connect(contextName, namespace, userID string) (ConnInfo, error) {
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: contextName})
	cfg, err := cc.ClientConfig()
	if err != nil {
		return ConnInfo{}, err
	}
	if namespace == "" {
		if namespace, _, err = cc.Namespace(); err != nil {
			return ConnInfo{}, err
		}
	}
	cfg.Timeout = 30 * time.Second

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return ConnInfo{}, err
	}
	if err := v1.AddToScheme(scheme); err != nil {
		return ConnInfo{}, err
	}
	k8s, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return ConnInfo{}, err
	}
	core, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return ConnInfo{}, err
	}
	jc := &jobs.Client{K8s: k8s, Namespace: namespace, ConfigNamespace: platform.Namespace()}
	// Probe the connection and the CRD.
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	if _, err := jc.List(ctx, "", "__probe__"); err != nil {
		return ConnInfo{}, fmt.Errorf("connect to %s: %w", cfg.Host, err)
	}

	info := ConnInfo{Context: contextName, Server: cfg.Host, Namespace: namespace, UserID: strings.TrimSpace(userID)}
	a.mu.Lock()
	a.conn = &connection{info: info, jobs: jc, core: core}
	a.mu.Unlock()
	return info, nil
}

func (a *App) current() (*connection, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.conn == nil {
		return nil, errNotConnected
	}
	return a.conn, nil
}

func (a *App) call() (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.ctx, 30*time.Second)
}

// Resources lists resource models from the platform config.
func (a *App) Resources() ([]ResourceInfo, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	cfg, err := c.jobs.Config(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ResourceInfo, 0, len(cfg.Resources))
	for _, r := range cfg.Resources {
		ri := ResourceInfo{ID: r.ID, GPUs: r.GPUCount(), CPU: r.CPU.Cores, MemoryGB: r.Memory.SizeGB}
		if r.GPU != nil {
			ri.GPUModel = r.GPU.Model
		}
		out = append(out, ri)
	}
	return out, nil
}

// ListJobs lists jobs of the connected user (or all users), newest first.
func (a *App) ListJobs() ([]Job, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	items, err := c.jobs.List(ctx, c.info.UserID, "")
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		return items[j].CreationTimestamp.Before(&items[i].CreationTimestamp)
	})
	out := make([]Job, 0, len(items))
	for i := range items {
		out = append(out, toJob(&items[i]))
	}
	return out, nil
}

// GetJob returns one job by TrainingJob name.
func (a *App) GetJob(name string) (Job, error) {
	c, err := a.current()
	if err != nil {
		return Job{}, err
	}
	ctx, cancel := a.call()
	defer cancel()
	j, err := c.jobs.GetByName(ctx, name)
	if err != nil {
		return Job{}, err
	}
	return toJob(j), nil
}

// CreateJob submits a new job. An empty job ID gets a random one.
func (a *App) CreateJob(f JobForm) (Job, error) {
	c, err := a.current()
	if err != nil {
		return Job{}, err
	}
	if f.UserID == "" {
		f.UserID = c.info.UserID
	}
	if f.JobID == "" {
		b := make([]byte, 3)
		_, _ = rand.Read(b)
		f.JobID = hex.EncodeToString(b)
	}
	if f.PoolID == "" {
		f.PoolID = "default"
	}
	if f.Workers < 1 {
		f.Workers = 1
	}
	ssh, jupyter := f.SSH, f.Jupyter
	spec := v1.TrainingJobSpec{
		UserID:     f.UserID,
		JobID:      f.JobID,
		PoolID:     f.PoolID,
		Image:      f.Image,
		Command:    f.Command,
		ResourceID: f.ResourceID,
		Workers:    f.Workers,
		Access:     v1.AccessSpec{SSH: &ssh, Jupyter: &jupyter},
	}
	keys := make([]string, 0, len(f.Env))
	for k := range f.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.TrimSpace(k) != "" {
			spec.Env = append(spec.Env, v1.EnvVar{Name: k, Value: f.Env[k]})
		}
	}

	ctx, cancel := a.call()
	defer cancel()
	cfg, err := c.jobs.Config(ctx)
	if err != nil {
		return Job{}, err
	}
	if err := jobs.Validate(cfg, &spec); err != nil {
		return Job{}, err
	}
	if _, err := c.jobs.Get(ctx, spec.UserID, spec.JobID); err == nil {
		return Job{}, fmt.Errorf("job %q already exists", spec.JobID)
	}
	j, err := c.jobs.Apply(ctx, spec)
	if err != nil {
		return Job{}, err
	}
	return toJob(j), nil
}

func (a *App) mutate(name string, fn func(*jobs.Client, context.Context, string) (*v1.TrainingJob, error)) (Job, error) {
	c, err := a.current()
	if err != nil {
		return Job{}, err
	}
	ctx, cancel := a.call()
	defer cancel()
	j, err := fn(c.jobs, ctx, name)
	if err != nil {
		return Job{}, err
	}
	return toJob(j), nil
}

// StopJob suspends a job (pods are removed, the spec is kept).
func (a *App) StopJob(name string) (Job, error) {
	return a.mutate(name, (*jobs.Client).Stop)
}

// StartJob resumes a stopped or finished job.
func (a *App) StartJob(name string) (Job, error) {
	return a.mutate(name, (*jobs.Client).Start)
}

// RestartJob recreates all workers.
func (a *App) RestartJob(name string) (Job, error) {
	return a.mutate(name, (*jobs.Client).Restart)
}

// DeleteJob deletes a job and everything it owns.
func (a *App) DeleteJob(name string) error {
	_, err := a.mutate(name, (*jobs.Client).Delete)
	return err
}

// SSH returns SSH endpoints of running workers.
func (a *App) SSH(name string) ([]jobs.SSHConnection, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	j, err := c.jobs.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	cfg, err := c.jobs.Config(ctx)
	if err != nil {
		return nil, err
	}
	return c.jobs.SSH(ctx, j, cfg)
}

// Logs returns the last tailLines of a worker pod log.
func (a *App) Logs(name string, worker int32, tailLines int64) (string, error) {
	c, err := a.current()
	if err != nil {
		return "", err
	}
	ctx, cancel := a.call()
	defer cancel()
	j, err := c.jobs.GetByName(ctx, name)
	if err != nil {
		return "", err
	}
	pod := ""
	for _, w := range j.Status.Workers {
		if w.Index == worker {
			pod = w.PodName
		}
	}
	if pod == "" {
		return "", fmt.Errorf("worker %d has no pod yet", worker)
	}
	if tailLines <= 0 {
		tailLines = 500
	}
	rc, err := c.core.CoreV1().Pods(j.Namespace).GetLogs(pod, &corev1.PodLogOptions{TailLines: &tailLines}).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, 4<<20))
	return string(b), err
}

func toJob(j *v1.TrainingJob) Job {
	s := j.Spec
	out := Job{
		Name:        j.Name,
		JobID:       s.JobID,
		UserID:      s.UserID,
		Image:       s.Image,
		Command:     s.Command,
		ResourceID:  s.ResourceID,
		PoolID:      s.PoolID,
		Workers:     s.Workers,
		Env:         map[string]string{},
		Phase:       string(j.Status.Phase),
		Message:     j.Status.Message,
		Ready:       j.Status.Ready,
		Succeeded:   j.Status.Succeeded,
		Failed:      j.Status.Failed,
		Created:     j.CreationTimestamp.Format(time.RFC3339),
		Age:         short(time.Since(j.CreationTimestamp.Time)),
		Suspended:   s.Suspend,
		RestartGen:  s.RestartGeneration,
		TrainJob:    j.Status.TrainJobName,
		SSHEnabled:  s.Access.SSH == nil || *s.Access.SSH,
		JupyterOn:   s.Access.Jupyter == nil || *s.Access.Jupyter,
		VolumeCount: len(s.VolumeMounts),
		WorkerPods:  []Worker{},
	}
	if out.Phase == "" {
		out.Phase = string(v1.PhasePending)
	}
	for _, e := range s.Env {
		out.Env[e.Name] = e.Value
	}
	if t := j.Status.StartTime; t != nil {
		out.Started = t.Format(time.RFC3339)
		end := time.Now()
		if ct := j.Status.CompletionTime; ct != nil {
			end = ct.Time
		}
		out.Duration = short(end.Sub(t.Time))
	}
	if t := j.Status.CompletionTime; t != nil {
		out.Completed = t.Format(time.RFC3339)
	}
	for _, w := range j.Status.Workers {
		urls := w.JupyterURLs
		if urls == nil {
			urls = []string{}
		}
		out.WorkerPods = append(out.WorkerPods, Worker{
			Index: w.Index, PodName: w.PodName, NodeName: w.NodeName, PodIP: w.PodIP,
			Phase: w.Phase, SSHUser: w.SSHUser, JupyterURLs: urls,
		})
	}
	return out
}

func short(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

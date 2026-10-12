/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"sync"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/internal/builder"
	"github.com/scclabs/forgeai/pkg/kf"
	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
)

const fieldOwner = client.FieldOwner("forgeai")

var workloadGVK = schema.GroupVersionKind{Group: "kueue.x-k8s.io", Version: "v1beta2", Kind: "Workload"}

// TrainingJobReconciler reconciles a TrainingJob object
type TrainingJobReconciler struct {
	client.Client
	// APIReader reads uncached objects (platform config, Kueue workloads).
	APIReader client.Reader
	Scheme    *runtime.Scheme
	// ConfigNamespace holds the forgeai-platform ConfigMap.
	ConfigNamespace string

	cfgMu   sync.Mutex
	cfg     *platform.Config
	cfgTime time.Time
}

// +kubebuilder:rbac:groups=forgeai.scclabs.dev,resources=trainingjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=forgeai.scclabs.dev,resources=trainingjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=forgeai.scclabs.dev,resources=trainingjobs/finalizers,verbs=update
// +kubebuilder:rbac:groups=trainer.kubeflow.org,resources=trainjobs;trainingruntimes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sshpiper.com,resources=pipes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods;configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses;networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads,verbs=get;list;watch

func (r *TrainingJobReconciler) config(ctx context.Context) (*platform.Config, error) {
	r.cfgMu.Lock()
	defer r.cfgMu.Unlock()
	if r.cfg != nil && time.Since(r.cfgTime) < 15*time.Second {
		return r.cfg, nil
	}
	cfg, err := platform.Load(ctx, r.APIReader, r.ConfigNamespace)
	if err != nil {
		return nil, err
	}
	r.cfg, r.cfgTime = cfg, time.Now()
	return cfg, nil
}

// Reconcile drives a TrainingJob towards its desired state:
//   - suspend: delete the TrainJob and access objects, keep the spec and secret
//   - restartGeneration bumped: delete the TrainJob, then recreate it
//   - otherwise: ensure secret, runtime, TrainJob and per-worker access objects exist
func (r *TrainingJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	job := &v1.TrainingJob{}
	if err := r.Get(ctx, req.NamespacedName, job); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !job.DeletionTimestamp.IsZero() {
		// Children are garbage collected through owner references.
		return ctrl.Result{}, nil
	}
	orig := job.DeepCopy()

	cfg, err := r.config(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	b, err := builder.New(job, cfg)
	if err != nil {
		r.setInvalid(job, err)
		return ctrl.Result{}, r.patchStatus(ctx, orig, job)
	}
	meta.RemoveStatusCondition(&job.Status.Conditions, "InvalidSpec")

	tj, err := r.getTrainJob(ctx, job)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Stop.
	if job.Spec.Suspend {
		if err := r.pruneAccess(ctx, job, nil); err != nil {
			return ctrl.Result{}, err
		}
		if tj != nil {
			if err := r.deleteTrainJob(ctx, tj); err != nil {
				return ctrl.Result{}, err
			}
			r.setPhase(job, v1.PhaseStopping, "deleting training pods")
			_ = r.patchStatus(ctx, orig, job)
			return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
		}
		job.Status.ObservedRestartGeneration = job.Spec.RestartGeneration
		job.Status.Workers = nil
		job.Status.Active, job.Status.Ready = 0, 0
		r.setPhase(job, v1.PhaseStopped, "")
		return ctrl.Result{}, r.patchStatus(ctx, orig, job)
	}

	// Restart: delete a TrainJob created for an older restartGeneration, recreate
	// on a later pass. The generation is read from the TrainJob itself so a stale
	// cached TrainingJob status can never restart twice.
	if tj != nil && trainJobRestartGeneration(tj) < job.Spec.RestartGeneration {
		if err := r.deleteTrainJob(ctx, tj); err != nil {
			return ctrl.Result{}, err
		}
		r.setPhase(job, v1.PhaseRestarting, "deleting previous run")
		job.Status.StartTime, job.Status.CompletionTime = nil, nil
		job.Status.Succeeded, job.Status.Failed = 0, 0
		_ = r.patchStatus(ctx, orig, job)
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}
	if tj != nil && !tj.GetDeletionTimestamp().IsZero() {
		// Previous run is still terminating.
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	if cfg.UserNetworkPolicy {
		if err := r.ensureNetworkPolicy(ctx, job); err != nil {
			return ctrl.Result{}, err
		}
	}
	if err := r.ensureSecret(ctx, job, b); err != nil {
		return ctrl.Result{}, err
	}
	if tj == nil {
		rt, err := b.TrainingRuntime()
		if err != nil {
			r.setInvalid(job, err)
			return ctrl.Result{}, r.patchStatus(ctx, orig, job)
		}
		if err := r.apply(ctx, job, rt, kf.TrainingRuntimeGVK); err != nil {
			return ctrl.Result{}, fmt.Errorf("apply runtime: %w", err)
		}
		if err := r.create(ctx, job, b.TrainJob(), kf.TrainJobGVK); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, fmt.Errorf("create trainjob: %w", err)
		}
		log.Info("created TrainJob", "name", job.Name)
		if tj, err = r.getTrainJob(ctx, job); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.ensureAccess(ctx, job, b); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.updateStatus(ctx, job, b, tj); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.patchStatus(ctx, orig, job); err != nil {
		return ctrl.Result{}, err
	}
	switch job.Status.Phase {
	case v1.PhaseQueued, v1.PhasePending:
		// Refresh Kueue / scheduling messages.
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *TrainingJobReconciler) setPhase(job *v1.TrainingJob, phase v1.TrainingJobPhase, msg string) {
	job.Status.Phase = phase
	job.Status.Message = msg
	job.Status.ObservedGeneration = job.Generation
}

func (r *TrainingJobReconciler) setInvalid(job *v1.TrainingJob, err error) {
	r.setPhase(job, v1.PhaseFailed, err.Error())
	meta.SetStatusCondition(&job.Status.Conditions, metav1.Condition{
		Type: "InvalidSpec", Status: metav1.ConditionTrue, Reason: "InvalidSpec",
		Message: err.Error(), ObservedGeneration: job.Generation,
	})
}

func (r *TrainingJobReconciler) patchStatus(ctx context.Context, orig, job *v1.TrainingJob) error {
	return client.IgnoreNotFound(r.Status().Patch(ctx, job, client.MergeFrom(orig)))
}

func (r *TrainingJobReconciler) getTrainJob(ctx context.Context, job *v1.TrainingJob) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(kf.TrainJobGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: job.Namespace, Name: job.Name}, u); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if !metav1.IsControlledBy(u, job) {
		return nil, fmt.Errorf("TrainJob %s exists and is not owned by this TrainingJob", job.Name)
	}
	return u, nil
}

func trainJobRestartGeneration(tj *unstructured.Unstructured) int64 {
	n, _ := strconv.ParseInt(tj.GetAnnotations()[naming.AnnotationRestartGeneration], 10, 64)
	return n
}

func (r *TrainingJobReconciler) deleteTrainJob(ctx context.Context, tj *unstructured.Unstructured) error {
	if !tj.GetDeletionTimestamp().IsZero() {
		return nil
	}
	return client.IgnoreNotFound(r.Delete(ctx, tj, client.PropagationPolicy(metav1.DeletePropagationForeground)))
}

func toUnstructured(obj any, gvk schema.GroupVersionKind) (*unstructured.Unstructured, error) {
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	u := &unstructured.Unstructured{Object: m}
	u.SetGroupVersionKind(gvk)
	unstructured.RemoveNestedField(u.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(u.Object, "status")
	return u, nil
}

// apply server-side applies obj with a controller reference to job.
func (r *TrainingJobReconciler) apply(ctx context.Context, job *v1.TrainingJob, obj any, gvk schema.GroupVersionKind) error {
	u, err := toUnstructured(obj, gvk)
	if err != nil {
		return err
	}
	if err := controllerutil.SetControllerReference(job, u, r.Scheme); err != nil {
		return err
	}
	return r.Client.Apply(ctx, client.ApplyConfigurationFromUnstructured(u), fieldOwner, client.ForceOwnership)
}

func (r *TrainingJobReconciler) create(ctx context.Context, job *v1.TrainingJob, obj any, gvk schema.GroupVersionKind) error {
	u, err := toUnstructured(obj, gvk)
	if err != nil {
		return err
	}
	if err := controllerutil.SetControllerReference(job, u, r.Scheme); err != nil {
		return err
	}
	return r.Create(ctx, u, fieldOwner)
}

func randomToken() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// ensureSecret creates credentials once; existing values are never rotated.
func (r *TrainingJobReconciler) ensureSecret(ctx context.Context, job *v1.TrainingJob, b *builder.Builder) error {
	s := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Namespace: job.Namespace, Name: naming.SecretName(job.Name)}, s)
	if err == nil || !apierrors.IsNotFound(err) {
		return err
	}
	secret := b.Secret(randomToken(), randomToken())
	if err := controllerutil.SetControllerReference(job, secret, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, secret, fieldOwner); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (r *TrainingJobReconciler) ensureNetworkPolicy(ctx context.Context, job *v1.TrainingJob) error {
	// Shared by all jobs of the user, so it is not owned by this TrainingJob.
	np := builder.UserNetworkPolicy(job.Namespace, job.Spec.UserID)
	if err := r.Create(ctx, np, fieldOwner); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (r *TrainingJobReconciler) ensureAccess(ctx context.Context, job *v1.TrainingJob, b *builder.Builder) error {
	services, ingresses, pipes := b.AccessObjects()
	keep := map[string]bool{}
	for _, s := range services {
		keep["Service/"+s.Name] = true
		if err := r.apply(ctx, job, s, corev1.SchemeGroupVersion.WithKind("Service")); err != nil {
			return fmt.Errorf("apply service %s: %w", s.Name, err)
		}
	}
	for _, i := range ingresses {
		keep["Ingress/"+i.Name] = true
		if err := r.apply(ctx, job, i, networkingv1.SchemeGroupVersion.WithKind("Ingress")); err != nil {
			return fmt.Errorf("apply ingress %s: %w", i.Name, err)
		}
	}
	for _, p := range pipes {
		keep["Pipe/"+p.Name] = true
		if err := r.apply(ctx, job, p, kf.PipeGVK); err != nil {
			return fmt.Errorf("apply pipe %s: %w", p.Name, err)
		}
	}
	return r.pruneAccess(ctx, job, keep)
}

// pruneAccess deletes owned Services, Ingresses and Pipes not in keep.
func (r *TrainingJobReconciler) pruneAccess(ctx context.Context, job *v1.TrainingJob, keep map[string]bool) error {
	sel := client.MatchingLabels{naming.LabelTrainingJob: job.Name}
	ns := client.InNamespace(job.Namespace)
	var objs []client.Object

	svcs := &corev1.ServiceList{}
	if err := r.List(ctx, svcs, ns, sel); err != nil {
		return err
	}
	for i := range svcs.Items {
		if !keep["Service/"+svcs.Items[i].Name] {
			objs = append(objs, &svcs.Items[i])
		}
	}
	ings := &networkingv1.IngressList{}
	if err := r.List(ctx, ings, ns, sel); err != nil {
		return err
	}
	for i := range ings.Items {
		if !keep["Ingress/"+ings.Items[i].Name] {
			objs = append(objs, &ings.Items[i])
		}
	}
	pipes := &unstructured.UnstructuredList{}
	pipes.SetGroupVersionKind(kf.PipeGVK.GroupVersion().WithKind("PipeList"))
	if err := r.List(ctx, pipes, ns, sel); err != nil {
		return err
	}
	for i := range pipes.Items {
		if !keep["Pipe/"+pipes.Items[i].GetName()] {
			objs = append(objs, &pipes.Items[i])
		}
	}
	for _, o := range objs {
		if !metav1.IsControlledBy(o, job) {
			continue
		}
		if err := r.Delete(ctx, o); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// updateStatus summarises the TrainJob, its batch Jobs and pods.
func (r *TrainingJobReconciler) updateStatus(ctx context.Context, job *v1.TrainingJob, b *builder.Builder, tj *unstructured.Unstructured) error {
	job.Status.TrainJobName = job.Name
	if tj != nil {
		job.Status.ObservedRestartGeneration = trainJobRestartGeneration(tj)
	}
	sel := client.MatchingLabels{naming.LabelTrainingJob: job.Name}
	ns := client.InNamespace(job.Namespace)

	tjs := &kf.TrainJob{}
	if tj != nil {
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(tj.Object, tjs); err != nil {
			return err
		}
	}
	var conds []metav1.Condition
	if tjs.Status != nil {
		conds = tjs.Status.Conditions
	}
	for _, c := range conds {
		c.ObservedGeneration = job.Generation
		meta.SetStatusCondition(&job.Status.Conditions, c)
	}

	jobs := &batchv1.JobList{}
	if err := r.List(ctx, jobs, ns, sel); err != nil {
		return err
	}
	var active, ready, succeeded, failed int32
	var start, done *metav1.Time
	for _, j := range jobs.Items {
		active += j.Status.Active
		if j.Status.Ready != nil {
			ready += *j.Status.Ready
		}
		succeeded += j.Status.Succeeded
		failed += j.Status.Failed
		if j.Status.StartTime != nil && (start == nil || j.Status.StartTime.Before(start)) {
			start = j.Status.StartTime
		}
		if j.Status.CompletionTime != nil && (done == nil || done.Before(j.Status.CompletionTime)) {
			done = j.Status.CompletionTime
		}
	}
	if len(jobs.Items) > 0 {
		job.Status.Active, job.Status.Ready = active, ready
		job.Status.Succeeded, job.Status.Failed = succeeded, failed
	} else {
		job.Status.Active, job.Status.Ready = 0, 0
	}
	if start != nil {
		job.Status.StartTime = start
	}

	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, ns, sel); err != nil {
		return err
	}
	byIndex := map[string]*corev1.Pod{}
	for i := range pods.Items {
		p := &pods.Items[i]
		idx := p.Labels[naming.LabelCompletion]
		if cur, ok := byIndex[idx]; !ok || cur.CreationTimestamp.Before(&p.CreationTimestamp) {
			byIndex[idx] = p
		}
	}
	workers := make([]v1.WorkerStatus, 0, job.Spec.Workers)
	anyRunning := false
	podMsg := ""
	for i := int32(0); i < max(job.Spec.Workers, 1); i++ {
		w := v1.WorkerStatus{Index: i}
		if b.SSHEnabled() {
			w.SSHUser = naming.SSHUser(job.Spec.JobID, i)
		}
		if b.JupyterEnabled() {
			for _, h := range b.JupyterHosts(i) {
				w.JupyterURLs = append(w.JupyterURLs, "https://"+h)
			}
		}
		if p, ok := byIndex[fmt.Sprint(i)]; ok {
			w.PodName, w.NodeName = p.Name, p.Spec.NodeName
			w.PodIP, w.NodeIP = p.Status.PodIP, p.Status.HostIP
			w.Phase = string(p.Status.Phase)
			if p.Status.Phase == corev1.PodRunning {
				anyRunning = true
			}
			if m := podWaitingMessage(p); m != "" && podMsg == "" {
				podMsg = fmt.Sprintf("worker-%d: %s", i, m)
			}
		}
		workers = append(workers, w)
	}
	job.Status.Workers = workers

	switch {
	case meta.IsStatusConditionTrue(conds, kf.ConditionComplete):
		job.Status.CompletionTime = firstNonNil(done, condTime(conds, kf.ConditionComplete))
		r.setPhase(job, v1.PhaseSucceeded, condMessage(conds, kf.ConditionComplete))
	case meta.IsStatusConditionTrue(conds, kf.ConditionFailed):
		job.Status.CompletionTime = firstNonNil(done, condTime(conds, kf.ConditionFailed))
		r.setPhase(job, v1.PhaseFailed, condMessage(conds, kf.ConditionFailed))
	case meta.IsStatusConditionTrue(conds, kf.ConditionSuspended):
		r.setPhase(job, v1.PhaseQueued, r.workloadMessage(ctx, tj))
	case anyRunning:
		r.setPhase(job, v1.PhaseRunning, "")
	default:
		r.setPhase(job, v1.PhasePending, podMsg)
	}
	return nil
}

func podWaitingMessage(p *corev1.Pod) string {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse {
			return c.Reason + ": " + c.Message
		}
	}
	for _, cs := range p.Status.ContainerStatuses {
		if w := cs.State.Waiting; w != nil {
			if w.Message != "" {
				return w.Reason + ": " + w.Message
			}
			return w.Reason
		}
	}
	return ""
}

// workloadMessage explains why Kueue has not admitted the job yet.
func (r *TrainingJobReconciler) workloadMessage(ctx context.Context, tj *unstructured.Unstructured) string {
	if tj == nil {
		return ""
	}
	wls := &unstructured.UnstructuredList{}
	wls.SetGroupVersionKind(workloadGVK.GroupVersion().WithKind("WorkloadList"))
	if err := r.APIReader.List(ctx, wls, client.InNamespace(tj.GetNamespace()),
		client.MatchingLabels{"kueue.x-k8s.io/job-uid": string(tj.GetUID())}); err != nil || len(wls.Items) == 0 {
		return "waiting for Kueue admission"
	}
	conds, _, _ := unstructured.NestedSlice(wls.Items[0].Object, "status", "conditions")
	for _, c := range conds {
		m, _ := c.(map[string]any)
		if m["type"] == "QuotaReserved" && m["status"] == "False" {
			if msg, _ := m["message"].(string); msg != "" {
				return msg
			}
		}
	}
	return "waiting for Kueue admission"
}

func condTime(conds []metav1.Condition, t string) *metav1.Time {
	if c := meta.FindStatusCondition(conds, t); c != nil {
		return &c.LastTransitionTime
	}
	return nil
}

func condMessage(conds []metav1.Condition, t string) string {
	if c := meta.FindStatusCondition(conds, t); c != nil {
		return c.Message
	}
	return ""
}

func firstNonNil(ts ...*metav1.Time) *metav1.Time {
	for _, t := range ts {
		if t != nil {
			return t
		}
	}
	return nil
}

// mapByLabel maps pods / batch Jobs back to their TrainingJob.
func mapByLabel(_ context.Context, obj client.Object) []reconcile.Request {
	name := obj.GetLabels()[naming.LabelTrainingJob]
	if name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: obj.GetNamespace(), Name: name}}}
}

// SetupWithManager sets up the controller with the Manager.
func (r *TrainingJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	tj := &unstructured.Unstructured{}
	tj.SetGroupVersionKind(kf.TrainJobGVK)
	pipe := &unstructured.Unstructured{}
	pipe.SetGroupVersionKind(kf.PipeGVK)
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.TrainingJob{}).
		Owns(tj).
		Owns(pipe).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(mapByLabel)).
		Watches(&batchv1.Job{}, handler.EnqueueRequestsFromMapFunc(mapByLabel)).
		Named("trainingjob").
		Complete(r)
}

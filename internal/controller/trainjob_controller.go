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
	"fmt"
	"maps"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/recorder"

	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/training"
)

// TrainJobReconciler adds what Trainer and Kueue lack for project jobs:
// stopping and resuming (through the Kueue Workload), failure detection
// (including pods stuck on image pulls or crash loops) and retries.
type TrainJobReconciler struct {
	client.Client
	Config   *platform.Loader
	Recorder recorder.EventRecorder
	Now      func() time.Time
}

// +kubebuilder:rbac:groups=trainer.kubeflow.org,resources=trainjobs,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=nodes;pods;namespaces,verbs=get;list;watch

func (r *TrainJobReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *TrainJobReconciler) event(tj *kf.TrainJob, typ, reason, msg string, args ...any) {
	if r.Recorder == nil {
		return
	}
	ref := &corev1.ObjectReference{
		APIVersion: kf.TrainJobGVK.GroupVersion().String(), Kind: kf.TrainJobGVK.Kind,
		Namespace: tj.Namespace, Name: tj.Name, UID: tj.UID, ResourceVersion: tj.ResourceVersion,
	}
	r.Recorder.Eventf(ref, nil, typ, reason, reason, msg, args...)
}

func (r *TrainJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(kf.TrainJobGVK)
	if err := r.Get(ctx, req.NamespacedName, u); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !u.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}
	// Only jobs in project namespaces.
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: req.Namespace}, ns); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if ns.Labels[naming.LabelProject] == "" {
		return ctrl.Result{}, nil
	}
	cfg, err := r.Config.Get(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	tj := &kf.TrainJob{}
	if err := kf.FromUnstructured(u.Object, tj); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.syncActive(ctx, tj); err != nil {
		return ctrl.Result{}, err
	}
	if training.Finished(tj) || training.Failed(tj) {
		if !training.Failed(tj) {
			return ctrl.Result{}, nil
		}
		if tj.Annotations[naming.AnnotationFailureReason] == "" {
			if err := r.classify(ctx, tj); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, r.retry(ctx, tj, cfg)
	}
	if tj.Annotations[training.AnnotationStopped] == naming.LiteralTrue {
		return ctrl.Result{}, nil
	}
	pods, err := r.pods(ctx, tj)
	if err != nil {
		return ctrl.Result{}, err
	}
	f, next := training.Stuck(pods, r.now(), cfg.Training.StuckTimeout.Duration)
	if f == nil {
		if next > 0 {
			return ctrl.Result{RequeueAfter: next + time.Second}, nil
		}
		return ctrl.Result{}, nil
	}
	// Fail the stuck job: record the failure and release its pods.
	logf.FromContext(ctx).Info("Stopped stuck TrainJob", "reason", f.Reason, "message", f.Message)
	r.event(tj, corev1.EventTypeWarning, f.Reason, "%s", f.Message)
	if err := r.annotate(ctx, tj, map[string]string{
		naming.AnnotationFailureReason:  f.Reason,
		naming.AnnotationFailureMessage: f.Message,
		training.AnnotationStopped:      naming.LiteralTrue,
	}); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: time.Second}, nil
}

func (r *TrainJobReconciler) pods(ctx context.Context, tj *kf.TrainJob) ([]corev1.Pod, error) {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(tj.Namespace), client.MatchingLabels{kf.LabelJobSetName: tj.Name}); err != nil {
		return nil, err
	}
	return pods.Items, nil
}

func (r *TrainJobReconciler) annotate(ctx context.Context, tj *kf.TrainJob, ann map[string]string) error {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(kf.TrainJobGVK)
	u.SetNamespace(tj.Namespace)
	u.SetName(tj.Name)
	patch := map[string]any{naming.KeyMetadata: map[string]any{"annotations": toAny(ann)}}
	data, err := jsonMarshal(patch)
	if err != nil {
		return err
	}
	if err := r.Patch(ctx, u, client.RawPatch(types.MergePatchType, data)); err != nil {
		return err
	}
	if tj.Annotations == nil {
		tj.Annotations = map[string]string{}
	}
	maps.Copy(tj.Annotations, ann)
	return nil
}

// syncActive deactivates the Kueue Workload of a stopped job (Kueue then
// suspends it and deletes its pods) and reactivates it on resume.
func (r *TrainJobReconciler) syncActive(ctx context.Context, tj *kf.TrainJob) error {
	want := tj.Annotations[training.AnnotationStopped] != naming.LiteralTrue
	wls := &unstructured.UnstructuredList{}
	wls.SetGroupVersionKind(kf.WorkloadGVK.GroupVersion().WithKind("WorkloadList"))
	if err := r.List(ctx, wls, client.InNamespace(tj.Namespace),
		client.MatchingLabels{kf.LabelKueueJobUID: string(tj.UID)}); err != nil {
		if meta.IsNoMatchError(err) {
			return nil
		}
		return err
	}
	for i := range wls.Items {
		wl := &wls.Items[i]
		active, found, _ := unstructured.NestedBool(wl.Object, naming.KeySpec, "active")
		if !found {
			active = true
		}
		if active == want {
			continue
		}
		data, _ := jsonMarshal(map[string]any{naming.KeySpec: map[string]any{"active": want}})
		if err := r.Patch(ctx, wl, client.RawPatch(types.MergePatchType, data)); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		if want {
			r.event(tj, corev1.EventTypeNormal, "Resumed", "Reactivated Workload %s", wl.GetName())
		} else {
			r.event(tj, corev1.EventTypeNormal, "Stopped", "Deactivated Workload %s", wl.GetName())
		}
	}
	return nil
}

func (r *TrainJobReconciler) classify(ctx context.Context, tj *kf.TrainJob) error {
	pods, err := r.pods(ctx, tj)
	if err != nil {
		return err
	}
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes); err != nil {
		return err
	}
	notReady := map[string]bool{}
	exists := map[string]bool{}
	for i := range nodes.Items {
		exists[nodes.Items[i].Name] = true
		if !nodeReady(&nodes.Items[i]) {
			notReady[nodes.Items[i].Name] = true
		}
	}
	for _, p := range pods {
		if p.Spec.NodeName != "" && !exists[p.Spec.NodeName] {
			notReady[p.Spec.NodeName] = true
		}
	}
	f := training.Classify(pods, notReady)
	if f.Message == "" {
		if c := meta.FindStatusCondition(tj.Status.Conditions, kf.ConditionFailed); c != nil {
			f.Message = c.Message
		}
	}
	r.event(tj, corev1.EventTypeWarning, f.Reason, "TrainJob failed: %s", f.Message)
	return r.annotate(ctx, tj, map[string]string{
		naming.AnnotationFailureReason: f.Reason, naming.AnnotationFailureMessage: f.Message,
	})
}

// retry resubmits a failed job as <group>-r<n> when its policy allows.
func (r *TrainJobReconciler) retry(ctx context.Context, tj *kf.TrainJob, cfg *platform.Config) error {
	if tj.Annotations[naming.AnnotationRetriedAs] != "" {
		return nil
	}
	maxRetries := int(cfg.Training.MaxRetries)
	if s, ok := tj.Annotations[naming.AnnotationMaxRetries]; ok {
		maxRetries, _ = strconv.Atoi(s)
	}
	attempt, _ := strconv.Atoi(tj.Annotations[naming.AnnotationRetry])
	on := training.RetryOn(tj.Annotations[naming.AnnotationRetryOn])
	reason := tj.Annotations[naming.AnnotationFailureReason]
	if attempt >= maxRetries || !training.Retryable(reason, on) {
		return nil
	}
	group := tj.Labels[naming.LabelJobGroup]
	if group == "" {
		group = tj.Name
	}
	name := training.RetryName(group, attempt+1)
	if len(name) > 63 {
		r.event(tj, corev1.EventTypeWarning, "RetrySkipped", "Name %s is too long to retry", name)
		return nil
	}

	next := &kf.TrainJob{
		TypeMeta: metav1.TypeMeta{APIVersion: kf.TrainJobGVK.GroupVersion().String(), Kind: kf.TrainJobGVK.Kind},
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: tj.Namespace, Labels: map[string]string{}, Annotations: map[string]string{},
		},
		Spec: kf.TrainJobSpec{RuntimeRef: tj.Spec.RuntimeRef, Trainer: tj.Spec.Trainer},
	}
	maps.Copy(next.Labels, tj.Labels)
	next.Labels[naming.LabelJobGroup] = group
	for _, k := range []string{naming.AnnotationMaxRetries, naming.AnnotationRetryOn} {
		if v, ok := tj.Annotations[k]; ok {
			next.Annotations[k] = v
		}
	}
	next.Annotations[naming.AnnotationRetry] = fmt.Sprint(attempt + 1)
	// Keep only forgeai's runtime patch; Kueue adds its own on admission.
	for _, p := range tj.Spec.RuntimePatches {
		if p.Manager == kf.PatchManager {
			next.Spec.RuntimePatches = append(next.Spec.RuntimePatches, p)
		}
	}
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(next)
	if err != nil {
		return err
	}
	u := &unstructured.Unstructured{Object: m}
	unstructured.RemoveNestedField(u.Object, naming.KeyMetadata, "creationTimestamp")
	if err := r.Create(ctx, u); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	logf.FromContext(ctx).Info("Retried failed TrainJob", "retry", name, "attempt", attempt+1, "reason", reason)
	r.event(tj, corev1.EventTypeNormal, "Retried", "Retried as %s (attempt %d of %d) after %s", name, attempt+1, maxRetries, reason)
	return r.annotate(ctx, tj, map[string]string{naming.AnnotationRetriedAs: name})
}

// SetupWithManager sets up the controller with the Manager.
func (r *TrainJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	tj := &unstructured.Unstructured{}
	tj.SetGroupVersionKind(kf.TrainJobGVK)
	wl := &unstructured.Unstructured{}
	wl.SetGroupVersionKind(kf.WorkloadGVK)
	byJobSet := handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []reconcile.Request {
		if n := o.GetLabels()[kf.LabelJobSetName]; n != "" {
			return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: o.GetNamespace(), Name: n}}}
		}
		return nil
	})
	byOwner := handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []reconcile.Request {
		for _, ref := range o.GetOwnerReferences() {
			if ref.Kind == kf.TrainJobGVK.Kind && ref.Controller != nil && *ref.Controller {
				return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: o.GetNamespace(), Name: ref.Name}}}
			}
		}
		return nil
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(tj).
		Watches(&corev1.Pod{}, byJobSet).
		Watches(wl, byOwner).
		Named("trainjob").
		Complete(r)
}

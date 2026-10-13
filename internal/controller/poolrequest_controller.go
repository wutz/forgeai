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
	"sort"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
	"github.com/scclabs/forgeai/pkg/pools"
)

// PoolRequestReconciler moves nodes between pools. It holds nnodes idle nodes
// of srcPool with placeholder pods (one per node, requesting the full resource
// model), relabels those nodes to dstPool, then releases the placeholders.
type PoolRequestReconciler struct {
	client.Client
	// APIReader reads the platform config and nodes without caching them.
	APIReader       client.Reader
	Scheme          *runtime.Scheme
	ConfigNamespace string
}

// +kubebuilder:rbac:groups=forgeai.scclabs.dev,resources=poolrequests,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=forgeai.scclabs.dev,resources=poolrequests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=forgeai.scclabs.dev,resources=poolrequests/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;patch

func (r *PoolRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	pr := &v1.PoolRequest{}
	if err := r.Get(ctx, req.NamespacedName, pr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !pr.DeletionTimestamp.IsZero() {
		// The placeholder Job is garbage collected through its owner reference.
		return ctrl.Result{}, nil
	}
	orig := pr.DeepCopy()

	job := &batchv1.Job{}
	err := r.Get(ctx, req.NamespacedName, job)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	hasJob := err == nil

	if pr.Status.Phase == v1.PoolRequestSuccess || pr.Status.Phase == v1.PoolRequestFailed {
		if hasJob && job.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, r.deleteJob(ctx, job)
		}
		return ctrl.Result{}, nil
	}

	cfg, err := platform.Load(ctx, r.APIReader, r.ConfigNamespace)
	if err != nil {
		return ctrl.Result{}, err
	}
	res, err := cfg.Resource(pr.Spec.ResourceID)
	if err != nil {
		return r.finish(ctx, orig, pr, v1.PoolRequestFailed, err.Error(), nil)
	}

	if !hasJob {
		job = placeholderJob(pr, res, cfg)
		if err := controllerutil.SetControllerReference(pr, job, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, job, fieldOwner); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, fmt.Errorf("create placeholder job: %w", err)
		}
		log.Info("created pool placeholder", "nodes", pr.Spec.NNodes, "from", pr.Spec.SrcPool, "to", pr.Spec.DstPool)
	}
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return r.finish(ctx, orig, pr, v1.PoolRequestFailed, "placeholder failed: "+c.Message, nil)
		}
	}

	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(pr.Namespace),
		client.MatchingLabels{naming.LabelPoolRequestID: pr.Spec.RequestID, naming.LabelUserID: pr.Spec.UserID}); err != nil {
		return ctrl.Result{}, err
	}
	nodes := map[string]bool{}
	msg := ""
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.Status.Phase == corev1.PodRunning && p.Spec.NodeName != "" && p.DeletionTimestamp.IsZero() {
			nodes[p.Spec.NodeName] = true
		} else if m := podWaitingMessage(p); m != "" && msg == "" {
			msg = m
		}
	}

	if int32(len(nodes)) < pr.Spec.NNodes {
		if msg == "" {
			msg = fmt.Sprintf("waiting for %d idle node(s) of %s in pool %s", pr.Spec.NNodes, pr.Spec.ResourceID, pr.Spec.SrcPool)
		}
		pr.Status.Phase, pr.Status.Message = v1.PoolRequestPending, fmt.Sprintf("%d/%d nodes held: %s", len(nodes), pr.Spec.NNodes, msg)
		if err := r.patchStatus(ctx, orig, pr); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	// Relabel while the placeholders still hold the nodes, so no src-pool
	// workload can be scheduled there in between.
	names := make([]string, 0, len(nodes))
	for n := range nodes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		n := &corev1.Node{}
		if err := r.APIReader.Get(ctx, types.NamespacedName{Name: name}, n); err != nil {
			return ctrl.Result{}, err
		}
		if err := pools.SetNodePool(ctx, r.Client, n, pr.Spec.DstPool); err != nil {
			return ctrl.Result{}, fmt.Errorf("label node %s: %w", name, err)
		}
	}
	log.Info("moved nodes", "nodes", names, "to", pr.Spec.DstPool)
	return r.finish(ctx, orig, pr, v1.PoolRequestSuccess, "", names)
}

func (r *PoolRequestReconciler) finish(ctx context.Context, orig, pr *v1.PoolRequest, phase v1.PoolRequestPhase, msg string, nodes []string) (ctrl.Result, error) {
	pr.Status.Phase, pr.Status.Message = phase, msg
	if nodes != nil {
		pr.Status.Nodes = nodes
	}
	now := metav1.Now()
	pr.Status.CompletionTime = &now
	if err := r.patchStatus(ctx, orig, pr); err != nil {
		return ctrl.Result{}, err
	}
	// The next pass deletes the placeholder Job.
	return ctrl.Result{Requeue: true}, nil
}

func (r *PoolRequestReconciler) patchStatus(ctx context.Context, orig, pr *v1.PoolRequest) error {
	return client.IgnoreNotFound(r.Status().Patch(ctx, pr, client.MergeFrom(orig)))
}

func (r *PoolRequestReconciler) deleteJob(ctx context.Context, job *batchv1.Job) error {
	return client.IgnoreNotFound(r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)))
}

// placeholderJob renders an Indexed Job with one sleeping pod per node. Each
// pod requests the full resource model so it only fits an idle node.
func placeholderJob(pr *v1.PoolRequest, res platform.Resource, cfg *platform.Config) *batchv1.Job {
	labels := map[string]string{
		naming.LabelAppName:        naming.AppNamePoolPlaceholder,
		naming.LabelPodName:        pr.Name,
		naming.LabelUserID:         pr.Spec.UserID,
		naming.LabelPodID:          pr.Spec.RequestID,
		naming.LabelAppInstance:    pr.Name,
		naming.LabelPoolRequestID:  pr.Spec.RequestID,
		naming.LabelPoolRequestSrc: pr.Spec.SrcPool,
		naming.LabelPoolRequestDst: pr.Spec.DstPool,
		naming.LabelAppVersion:     cfg.ReleaseVersion,
		naming.LabelResourceID:     pr.Spec.ResourceID,
		naming.LabelManagedBy:      naming.ManagedBy,
		naming.LabelComponent:      naming.ComponentPoolPlaceholder,
	}
	selector := map[string]string{}
	for k, v := range res.Labels {
		selector[k] = v
	}
	selector[naming.LabelNodePool] = pr.Spec.SrcPool

	rl := corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewMilliQuantity(int64(res.CPU.Cores*1000), resource.DecimalSI),
		corev1.ResourceMemory: *resource.NewQuantity(int64(res.Memory.SizeGB*(1<<30)), resource.BinarySI),
	}
	if g := res.GPUCount(); g > 0 {
		rl["nvidia.com/gpu"] = *resource.NewQuantity(int64(g), resource.DecimalSI)
	}
	n := pr.Spec.NNodes
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: pr.Name, Namespace: pr.Namespace, Labels: labels},
		Spec: batchv1.JobSpec{
			Completions:    ptr.To(n),
			Parallelism:    ptr.To(n),
			CompletionMode: ptr.To(batchv1.IndexedCompletion),
			BackoffLimit:   ptr.To[int32](0),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:      "placeholder",
						Image:     cfg.PoolPlaceholderImage,
						Resources: corev1.ResourceRequirements{Requests: rl, Limits: rl.DeepCopy()},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr.To(false),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
					}},
					// One placeholder per node.
					Affinity: &corev1.Affinity{PodAntiAffinity: &corev1.PodAntiAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
							TopologyKey: corev1.LabelHostname,
							LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
								naming.LabelPoolRequestID: pr.Spec.RequestID, naming.LabelUserID: pr.Spec.UserID,
							}},
						}},
					}},
					NodeSelector:                  selector,
					Tolerations:                   []corev1.Toleration{{Key: "nvidia.com/gpu", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule}},
					ServiceAccountName:            cfg.ServiceAccount,
					AutomountServiceAccountToken:  ptr.To(false),
					EnableServiceLinks:            ptr.To(false),
					TerminationGracePeriodSeconds: ptr.To[int64](0),
					RestartPolicy:                 corev1.RestartPolicyNever,
				},
			},
		},
	}
}

// mapPoolPod maps placeholder pods to their PoolRequest (the Job has the same name).
func mapPoolPod(_ context.Context, obj client.Object) []reconcile.Request {
	if obj.GetLabels()[naming.LabelComponent] != naming.ComponentPoolPlaceholder {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{
		Namespace: obj.GetNamespace(), Name: obj.GetLabels()[naming.LabelAppInstance],
	}}}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PoolRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.PoolRequest{}).
		Owns(&batchv1.Job{}).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(mapPoolPod)).
		Named("poolrequest").
		Complete(r)
}

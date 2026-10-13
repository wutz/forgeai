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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/scclabs/forgeai/pkg/naming"
)

// ImageWarmupReconciler removes image warmup DaemonSets (created by the compat
// API) once every selected node pulled the image, or when they expire.
type ImageWarmupReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;delete

func (r *ImageWarmupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	ds := &appsv1.DaemonSet{}
	if err := r.Get(ctx, req.NamespacedName, ds); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !ds.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	image := ds.Annotations[naming.AnnotationImage]

	expires, err := time.Parse(time.RFC3339, ds.Annotations[naming.AnnotationExpires])
	if err != nil || !time.Now().Before(expires) {
		log.Info("image warmup expired", "image", image)
		return ctrl.Result{}, r.remove(ctx, ds)
	}

	done, err := r.pulled(ctx, ds)
	if err != nil {
		return ctrl.Result{}, err
	}
	if done {
		log.Info("image warmup finished", "image", image, "nodes", ds.Status.DesiredNumberScheduled)
		return ctrl.Result{}, r.remove(ctx, ds)
	}
	return ctrl.Result{RequeueAfter: min(time.Until(expires), time.Minute)}, nil
}

// pulled reports whether every node that should run the DaemonSet has the image.
func (r *ImageWarmupReconciler) pulled(ctx context.Context, ds *appsv1.DaemonSet) (bool, error) {
	if ds.Status.ObservedGeneration < ds.Generation {
		return false, nil
	}
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(ds.Namespace),
		client.MatchingLabels{naming.LabelImageWarmup: ds.Labels[naming.LabelImageWarmup]}); err != nil {
		return false, err
	}
	nodes := map[string]bool{}
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.Spec.NodeName != "" && imagePulled(p) {
			nodes[p.Spec.NodeName] = true
		}
	}
	return int32(len(nodes)) >= ds.Status.DesiredNumberScheduled, nil
}

// imagePulled is true once the container got past image pulling, even if the
// image cannot run `sleep` (the image is cached on the node either way).
func imagePulled(p *corev1.Pod) bool {
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Running != nil || cs.State.Terminated != nil || cs.LastTerminationState.Terminated != nil {
			return true
		}
		if w := cs.State.Waiting; w != nil {
			switch w.Reason {
			case "", "ContainerCreating", "PodInitializing", "ErrImagePull", "ImagePullBackOff", "InvalidImageName", "ErrImageNeverPull":
				return false
			}
			return true
		}
	}
	return false
}

func (r *ImageWarmupReconciler) remove(ctx context.Context, ds *appsv1.DaemonSet) error {
	return client.IgnoreNotFound(r.Delete(ctx, ds, client.PropagationPolicy(metav1.DeletePropagationBackground)))
}

// SetupWithManager sets up the controller with the Manager.
func (r *ImageWarmupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	isWarmup := predicate.NewPredicateFuncs(func(o client.Object) bool {
		return o.GetLabels()[naming.LabelComponent] == naming.ComponentImageWarmup
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.DaemonSet{}, builder.WithPredicates(isWarmup)).
		Owns(&corev1.Pod{}, builder.WithPredicates(isWarmup)).
		Named("imagewarmup").
		Complete(r)
}

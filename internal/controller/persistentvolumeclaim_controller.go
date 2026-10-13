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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
	"github.com/scclabs/forgeai/pkg/volumes"
)

// PersistentVolumeClaimReconciler purges volumes from the trash once they
// expire. Volumes still mounted by an active pod are kept until it ends.
type PersistentVolumeClaimReconciler struct {
	client.Client
	Config *platform.Loader
	Now    func() time.Time
}

// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

func (r *PersistentVolumeClaimReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// retention returns the trash retention of the project owning namespace ns.
func (r *PersistentVolumeClaimReconciler) retention(ctx context.Context, ns string) (time.Duration, error) {
	cfg, err := r.Config.Get(ctx)
	if err != nil {
		return 0, err
	}
	d := cfg.TrashRetention.Duration
	nsObj := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: ns}, nsObj); err != nil {
		return d, client.IgnoreNotFound(err)
	}
	p := &v1.Project{}
	if err := r.Get(ctx, types.NamespacedName{Name: nsObj.Labels[naming.LabelProject]}, p); err != nil {
		return d, client.IgnoreNotFound(err)
	}
	if p.Spec.TrashRetention != nil {
		d = p.Spec.TrashRetention.Duration
	}
	return d, nil
}

func (r *PersistentVolumeClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, req.NamespacedName, pvc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !volumes.InTrash(pvc) || !pvc.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	now := r.now()
	expire, ok := volumes.ExpireAt(pvc)
	if !ok {
		// Trashed without an expiry (e.g. labelled with kubectl): start the clock now.
		d, err := r.retention(ctx, pvc.Namespace)
		if err != nil {
			return ctrl.Result{}, err
		}
		orig := pvc.DeepCopy()
		if pvc.Annotations == nil {
			pvc.Annotations = map[string]string{}
		}
		if pvc.Annotations[naming.AnnotationTrashedAt] == "" {
			pvc.Annotations[naming.AnnotationTrashedAt] = now.UTC().Format(time.RFC3339)
		}
		expire = now.Add(d)
		pvc.Annotations[naming.AnnotationExpireAt] = expire.UTC().Format(time.RFC3339)
		if err := r.Patch(ctx, pvc, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, err
		}
	}
	if wait := expire.Sub(now); wait > 0 {
		return ctrl.Result{RequeueAfter: wait}, nil
	}

	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(pvc.Namespace)); err != nil {
		return ctrl.Result{}, err
	}
	if users := volumes.Users(pods.Items)[pvc.Name]; len(users) > 0 {
		log.Info("Postponed purging PersistentVolumeClaim in use", "name", pvc.Name, "pods", strings.Join(users, ","))
		return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
	}
	if err := r.Delete(ctx, pvc, client.Preconditions{UID: &pvc.UID, ResourceVersion: &pvc.ResourceVersion}); err != nil {
		if apierrors.IsNotFound(err) || apierrors.IsConflict(err) {
			// Restored or changed meanwhile; the update triggers another pass.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	log.Info("Purged expired PersistentVolumeClaim", "name", pvc.Name, "namespace", pvc.Namespace)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PersistentVolumeClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	trashed := predicate.NewPredicateFuncs(func(o client.Object) bool {
		return o.GetLabels()[naming.LabelTrash] == naming.LiteralTrue
	})
	// A finished pod may unblock a postponed purge.
	byClaims := handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []reconcile.Request {
		p, ok := o.(*corev1.Pod)
		if !ok || (p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed && p.DeletionTimestamp.IsZero()) {
			return nil
		}
		var out []reconcile.Request
		for _, v := range p.Spec.Volumes {
			if v.PersistentVolumeClaim != nil {
				out = append(out, reconcile.Request{NamespacedName: types.NamespacedName{
					Namespace: p.Namespace, Name: v.PersistentVolumeClaim.ClaimName}})
			}
		}
		return out
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.PersistentVolumeClaim{}, builder.WithPredicates(trashed)).
		Watches(&corev1.Pod{}, byClaims).
		Named("volume-trash").
		Complete(r)
}

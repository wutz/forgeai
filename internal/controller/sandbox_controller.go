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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/sandbox"
)

// SandboxReconciler adds what the agent-sandbox controller lacks for forgeai
// projects: the stop/start annotation shared with TrainJobs and the idle
// policy of the platform. A stopped or idle Sandbox is suspended: its pod is
// deleted but the object and its volumes are kept, so it can be resumed.
type SandboxReconciler struct {
	client.Client
	Config *platform.Loader
	Now    func() time.Time
}

func (r *SandboxReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// +kubebuilder:rbac:groups=agents.x-k8s.io,resources=sandboxes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=agents.x-k8s.io,resources=sandboxes/status,verbs=get
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

// Reconcile applies the stop annotation and the idle policy. It never
// resumes a sandbox: a suspended sandbox stays suspended until its user
// starts it.
func (r *SandboxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(sandbox.SandboxGVK)
	if err := r.Get(ctx, req.NamespacedName, u); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if u.GetLabels()[naming.LabelProject] == "" || !u.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}
	sb := &sandbox.Sandbox{}
	if err := sandbox.FromUnstructured(u.Object, sb); err != nil {
		return ctrl.Result{}, err
	}
	orig := u.DeepCopy()

	if sb.Labels[naming.LabelSSH] == naming.LiteralTrue {
		if err := r.ensureSSH(ctx, u); err != nil {
			return ctrl.Result{}, err
		}
	}

	// The stop annotation is what the GUI, the CLI and the docs all use; it
	// maps onto spec.operatingMode, which is what the sandbox controller reads.
	wantStopped := sb.Annotations[naming.AnnotationStopped] == naming.LiteralTrue
	if wantStopped != (sb.Spec.OperatingMode == sandbox.OperatingSuspended) {
		if wantStopped {
			sb.Spec.OperatingMode = sandbox.OperatingSuspended
		} else {
			sb.Spec.OperatingMode = sandbox.OperatingRunning
			delete(sb.Annotations, naming.AnnotationIdleSince)
		}
		if wantStopped {
			log.Info("Suspending sandbox", "sandbox", sb.Name)
			delete(sb.Annotations, naming.AnnotationIdleSince)
		} else {
			log.Info("Resuming sandbox", "sandbox", sb.Name)
			setLastActive(sb, r.now())
		}
		return ctrl.Result{}, r.apply(ctx, orig, sb)
	}
	if sb.Spec.OperatingMode == sandbox.OperatingSuspended {
		return ctrl.Result{}, nil
	}

	// Idle policy: suspend a sandbox nothing has used for its idle timeout, the
	// one it was created with or else sandbox.idleTimeout. Activity is the
	// client heartbeat (GUI interactions, `kubectl annotate`) and the pod start.
	cfg, err := r.Config.Get(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	timeout := sandbox.IdleTimeoutOf(sb, cfg.Sandbox.IdleTimeout.Duration)
	if timeout <= 0 {
		return ctrl.Result{}, nil
	}
	if phase, _ := sandbox.PhaseOf(sb); phase != sandbox.PhaseRunning {
		return ctrl.Result{}, nil
	}
	last := sandboxLastActive(sb)
	if last.IsZero() {
		// Start the idle clock when the sandbox first reports Ready.
		setLastActive(sb, r.now())
		return ctrl.Result{RequeueAfter: timeout}, r.apply(ctx, orig, sb)
	}
	idle := r.now().Sub(last)
	if idle >= timeout {
		log.Info("Suspending idle sandbox", "sandbox", sb.Name, "idle", idle.Round(time.Second).String())
		sb.Spec.OperatingMode = sandbox.OperatingSuspended
		sb.Annotations[naming.AnnotationStopped] = naming.LiteralTrue
		sb.Annotations[naming.AnnotationIdleSince] = r.now().UTC().Format(time.RFC3339)
		return ctrl.Result{}, r.apply(ctx, orig, sb)
	}
	return ctrl.Result{RequeueAfter: max(timeout-idle, time.Second)}, nil
}

// lastActive is the client heartbeat of a sandbox.
func sandboxLastActive(sb *sandbox.Sandbox) time.Time {
	t, err := time.Parse(time.RFC3339, sb.Annotations[naming.AnnotationLastActive])
	if err != nil {
		return time.Time{}
	}
	return t
}

// setLastActive records a heartbeat; a later idle check counts from it.
func setLastActive(sb *sandbox.Sandbox, now time.Time) {
	if sb.Annotations == nil {
		sb.Annotations = map[string]string{}
	}
	sb.Annotations[naming.AnnotationLastActive] = now.UTC().Format(time.RFC3339)
}

// apply patches the sandbox.
func (r *SandboxReconciler) apply(ctx context.Context, orig *unstructured.Unstructured, sb *sandbox.Sandbox) error {
	u, err := sandbox.ToUnstructured(sb)
	if err != nil {
		return err
	}
	u.SetResourceVersion(sb.ResourceVersion)
	return r.Patch(ctx, u, client.MergeFrom(orig))
}

// SetupWithManager watches project sandboxes, their pods (a restarted pod
// resets the idle clock) and the SSH Service and Secret they own.
func (r *SandboxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(sandbox.SandboxGVK)
	return ctrl.NewControllerManagedBy(mgr).
		Named("sandbox").
		For(u, builder.WithPredicates(projectSandbox())).
		Owns(&corev1.Service{}, builder.WithPredicates(projectSandbox())).
		Owns(&corev1.Secret{}, builder.WithPredicates(projectSandbox())).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []reconcile.Request {
			return []reconcile.Request{{NamespacedName: types.NamespacedName{
				Namespace: o.GetNamespace(), Name: sandboxOfPod(o),
			}}}
		}), builder.WithPredicates(predicate.NewPredicateFuncs(func(o client.Object) bool {
			return o.GetLabels()[naming.LabelProject] != ""
		}))).
		Complete(r)
}

// sandboxOfPod is the sandbox a pod belongs to: its controller reference, or
// the pod name (the sandbox controller names pods after their sandbox).
func sandboxOfPod(o client.Object) string {
	for _, ref := range o.GetOwnerReferences() {
		if ref.Kind == "Sandbox" && ref.APIVersion == sandbox.SandboxGVK.GroupVersion().String() {
			return ref.Name
		}
	}
	return o.GetName()
}

// projectSandbox keeps only sandboxes of forgeai projects; the controller
// never touches anything else in the cluster.
func projectSandbox() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(o client.Object) bool {
		return o.GetLabels()[naming.LabelProject] != ""
	})
}

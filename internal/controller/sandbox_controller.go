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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
// projects: the stop/start annotation shared with TrainJobs, the idle policy of
// the platform and the publication of sandbox ports through the Gateway API. A
// stopped or idle Sandbox is suspended: its pod is deleted but the object and
// its volumes are kept, so it can be resumed.
type SandboxReconciler struct {
	client.Client
	// APIReader reads past the cache, to see Services the cache does not hold.
	APIReader client.Reader
	Config    *platform.Loader
	// Gateway is set when the Gateway API HTTPRoute CRD is installed; without
	// it sandboxes are not published through the gateway.
	Gateway bool
	Now     func() time.Time
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
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete

// Reconcile publishes the sandbox through the gateway if it asks for it, then
// applies the stop annotation and the idle policy. It never resumes a
// sandbox: a suspended sandbox stays suspended until its user starts it.
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
	if r.Gateway {
		if err := r.reconcileGateway(ctx, sb); err != nil {
			return ctrl.Result{}, err
		}
	}
	orig := u.DeepCopy()

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

// reconcileGateway publishes a sandbox with a gateway port: a Service selecting
// its pod on that port and an HTTPRoute for its host name. Clearing the port
// removes both. The objects follow the pod, so a sandbox without one is left as
// it is until the pod starts.
func (r *SandboxReconciler) reconcileGateway(ctx context.Context, sb *sandbox.Sandbox) error {
	log := logf.FromContext(ctx)
	port := sandbox.GatewayPortOf(sb)
	if port == 0 {
		return r.unpublish(ctx, sb)
	}
	cfg, err := r.Config.Get(ctx)
	if err != nil {
		return err
	}
	if err := sandbox.CheckGateway(sb.Name, sb.Namespace, cfg); err != nil {
		// A spec error does not heal by retrying; the next edit reconciles again.
		log.Error(err, "Could not publish sandbox", "sandbox", sb.Name)
		return nil
	}
	pod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: sb.Namespace, Name: sandbox.PodName(sb)}, pod); err != nil {
		return client.IgnoreNotFound(err)
	}
	// The Service selects the sandbox's pods by its name hash. Without it the
	// selector would match every pod in the namespace.
	hash := pod.Labels[sandbox.LabelSandboxNameHash]
	if hash == "" {
		log.Info("Pod has no sandbox name hash; sandbox not published", "sandbox", sb.Name)
		return nil
	}
	svc := sandbox.GatewayService(sb, map[string]string{sandbox.LabelSandboxNameHash: hash}, port)
	if err := r.publish(ctx, sb, svc); err != nil {
		return err
	}
	route := sandbox.GatewayRoute(sb, sandbox.GatewayHost(sb.Name, sb.Namespace, cfg.Ingress), port, cfg.Gateway)
	return r.publish(ctx, sb, route)
}

// publish applies obj owned by the sandbox. It refuses to take over an object
// of the same name that belongs to someone else, such as the Service of another
// sandbox.
func (r *SandboxReconciler) publish(ctx context.Context, sb *sandbox.Sandbox, obj client.Object) error {
	cur := obj.DeepCopyObject().(client.Object)
	switch err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(obj), cur); {
	case err == nil:
		if !metav1.IsControlledBy(cur, sb) {
			return fmt.Errorf("%T %s exists and is not owned by sandbox %s", obj, obj.GetName(), sb.Name)
		}
	case !apierrors.IsNotFound(err):
		return err
	}
	obj.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(sb, sandbox.SandboxGVK)})
	return apply(ctx, r.Client, obj)
}

// unpublish removes the gateway objects the sandbox owns. It reads through the
// cache, so a sandbox that was never published costs no API call.
func (r *SandboxReconciler) unpublish(ctx context.Context, sb *sandbox.Sandbox) error {
	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(sandbox.HTTPRouteGVK)
	if err := r.deleteOwned(ctx, sb, sb.Name, route); err != nil {
		return err
	}
	return r.deleteOwned(ctx, sb, sandbox.GatewayServiceName(sb.Name), &corev1.Service{})
}

// deleteOwned deletes the object of the given name if the sandbox owns it.
func (r *SandboxReconciler) deleteOwned(ctx context.Context, sb *sandbox.Sandbox, name string, obj client.Object) error {
	err := r.Get(ctx, types.NamespacedName{Namespace: sb.Namespace, Name: name}, obj)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !metav1.IsControlledBy(obj, sb) {
		return nil
	}
	return client.IgnoreNotFound(r.Delete(ctx, obj))
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

// SetupWithManager watches project sandboxes and their pods (a restarted pod
// resets the idle clock).
func (r *SandboxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(sandbox.SandboxGVK)
	b := ctrl.NewControllerManagedBy(mgr).
		Named("sandbox").
		For(u, builder.WithPredicates(projectSandbox())).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []reconcile.Request {
			return []reconcile.Request{{NamespacedName: types.NamespacedName{
				Namespace: o.GetNamespace(), Name: sandboxOfPod(o),
			}}}
		}), builder.WithPredicates(predicate.NewPredicateFuncs(func(o client.Object) bool {
			return o.GetLabels()[naming.LabelProject] != ""
		})))
	if r.Gateway {
		route := &unstructured.Unstructured{}
		route.SetGroupVersionKind(sandbox.HTTPRouteGVK)
		b = b.Owns(&corev1.Service{}).Owns(route)
	}
	return b.Complete(r)
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

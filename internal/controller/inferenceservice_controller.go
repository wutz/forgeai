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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/inference"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
)

// Reasons of the Ready condition of an InferenceService.
const (
	inferenceReady           = "Ready"
	inferenceStarting        = "Starting"
	inferenceStopped         = "Stopped"
	inferenceInstanceMissing = "InstanceTypeNotFound"
	inferenceNoGPU           = "InstanceTypeWithoutGPU"
	inferenceApplyFailed     = "ApplyFailed"
	// inferenceRetry is how often a missing InstanceType is looked up again.
	inferenceRetry = time.Minute
)

// InferenceServiceReconciler serves a model with vLLM behind the llm-d router
// in the namespace of the InferenceService.
type InferenceServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config *platform.Loader
}

// +kubebuilder:rbac:groups=forgeai.wutz.dev,resources=inferenceservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=forgeai.wutz.dev,resources=inferenceservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=forgeai.wutz.dev,resources=inferenceservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=forgeai.wutz.dev,resources=instancetypes,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;serviceaccounts;configmaps;secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=inference.networking.k8s.io,resources=inferencepools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=llm-d.ai,resources=inferenceobjectives;inferencemodelrewrites,verbs=get;list;watch

// Reconcile renders the router and the model servers of an InferenceService
// and applies them. Stopping the service (annotation forgeai.wutz.dev/stopped)
// scales both to zero.
func (r *InferenceServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	svc := &v1.InferenceService{}
	if err := r.Get(ctx, req.NamespacedName, svc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !svc.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	cfg, err := r.Config.Get(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	orig := svc.DeepCopy()

	it := &v1.InstanceType{}
	if err := r.Get(ctx, types.NamespacedName{Name: svc.Spec.InstanceType}, it); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		if err := r.setReady(ctx, svc, orig, metav1.ConditionFalse, inferenceInstanceMissing,
			fmt.Sprintf("InstanceType %q does not exist", svc.Spec.InstanceType)); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: inferenceRetry}, nil
	}
	// The GUI refuses a CPU-only InstanceType; a manifest can still name one.
	if it.Spec.GPU.Count < 1 {
		if err := r.setReady(ctx, svc, orig, metav1.ConditionFalse, inferenceNoGPU,
			fmt.Sprintf("InstanceType %q has no GPU", svc.Spec.InstanceType)); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: inferenceRetry}, nil
	}

	replicas := max(svc.Spec.Replicas, 1)
	stopped := svc.Annotations[naming.AnnotationStopped] == "true"
	if stopped {
		replicas = 0
	}
	if _, err := r.ensureAPIKey(ctx, svc); err != nil {
		return ctrl.Result{}, err
	}
	objs, err := inference.Objects(svc, cfg, it, replicas)
	if err != nil {
		return ctrl.Result{}, err
	}
	for _, o := range objs {
		if err := ctrl.SetControllerReference(svc, o, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := apply(ctx, r.Client, o); err != nil {
			if serr := r.setReady(ctx, svc, orig, metav1.ConditionFalse, inferenceApplyFailed,
				fmt.Sprintf("Could not apply %s %s: %v", o.GetObjectKind().GroupVersionKind().Kind, o.GetName(), err)); serr != nil {
				return ctrl.Result{}, serr
			}
			return ctrl.Result{}, fmt.Errorf("apply %s: %w", o.GetName(), err)
		}
	}

	model := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: svc.Namespace, Name: inference.ModelName(svc)}, model); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	epp := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: svc.Namespace, Name: inference.EPPName(svc.Name)}, epp); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	svc.Status.Endpoint = inference.Endpoint(svc, cfg)
	svc.Status.APIKeySecret = naming.InferenceAPIKeySecret(svc.Name)
	svc.Status.ReadyReplicas = model.Status.ReadyReplicas
	status, reason, msg := metav1.ConditionFalse, inferenceStarting,
		fmt.Sprintf("%d of %d model servers ready", model.Status.ReadyReplicas, replicas)
	switch {
	case stopped:
		reason, msg = inferenceStopped, "Stopped by annotation "+naming.AnnotationStopped
	case replicas > 0 && model.Status.ReadyReplicas >= replicas && epp.Status.ReadyReplicas > 0:
		status, reason, msg = metav1.ConditionTrue, inferenceReady, fmt.Sprintf("%d model servers ready", replicas)
	}
	// A Deployment that cannot create its pods, such as one the project quota
	// refuses, stays Starting; the refusal is the reason the user needs to see.
	if reason == inferenceStarting {
		for _, d := range []*appsv1.Deployment{model, epp} {
			if f := replicaFailure(d); f != "" {
				msg += "; " + f
			}
		}
	}
	meta.SetStatusCondition(&svc.Status.Conditions, metav1.Condition{
		Type: inferenceReady, Status: status, Reason: reason, Message: msg, ObservedGeneration: svc.Generation,
	})
	if prev := meta.FindStatusCondition(orig.Status.Conditions, inferenceReady); prev == nil || prev.Reason != reason {
		log.Info("InferenceService changed state", "reason", reason, "readyReplicas", svc.Status.ReadyReplicas)
	}
	return ctrl.Result{}, r.Status().Patch(ctx, svc, client.MergeFrom(orig))
}

// replicaFailure is the message of a Deployment's ReplicaFailure condition, the
// reason its pods cannot be created. It is empty when the Deployment has none.
func replicaFailure(d *appsv1.Deployment) string {
	for _, c := range d.Status.Conditions {
		if c.Type == appsv1.DeploymentReplicaFailure && c.Status == corev1.ConditionTrue {
			return c.Message
		}
	}
	return ""
}

// setReady records a failed Ready condition and patches the status.
func (r *InferenceServiceReconciler) setReady(ctx context.Context, svc, orig *v1.InferenceService, status metav1.ConditionStatus, reason, msg string) error {
	meta.SetStatusCondition(&svc.Status.Conditions, metav1.Condition{
		Type: inferenceReady, Status: status, Reason: reason, Message: msg, ObservedGeneration: svc.Generation,
	})
	return r.Status().Patch(ctx, svc, client.MergeFrom(orig))
}

// ensureAPIKey creates the API key of svc once. The key is kept across
// reconciles, so clients keep working when the endpoint is re-rendered.
func (r *InferenceServiceReconciler) ensureAPIKey(ctx context.Context, svc *v1.InferenceService) (*corev1.Secret, error) {
	name := naming.InferenceAPIKeySecret(svc.Name)
	s := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Namespace: svc.Namespace, Name: name}, s)
	if err == nil && len(s.Data[naming.InferenceAPIKey]) > 0 {
		return s, nil
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	s = &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String(), Kind: kindSecret},
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: svc.Namespace,
			Labels: map[string]string{naming.LabelManagedBy: naming.ManagedBy, naming.LabelComponent: naming.ComponentInference, naming.LabelInference: svc.Name},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{naming.InferenceAPIKey: []byte(hex.EncodeToString(b))},
	}
	if err := ctrl.SetControllerReference(svc, s, r.Scheme); err != nil {
		return nil, err
	}
	return s, apply(ctx, r.Client, s)
}

// SetupWithManager sets up the controller with the Manager.
func (r *InferenceServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.InferenceService{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{}, predicate.AnnotationChangedPredicate{}))).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&networkingv1.Ingress{}).
		Named("inferenceservice").
		Complete(r)
}

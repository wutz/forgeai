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
	"slices"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/volumes"
)

const (
	fileServerPort     = 5000
	fileServerPortName = "http"
	fileServerUser     = "forgeai"
	// labelInstance identifies the file server pods and service.
	labelInstance = "app.kubernetes.io/instance"
	kindSecret    = "Secret"
	// annotationServedVolumes on the pod template restarts dufs when volumes change.
	annotationServedVolumes = "forgeai.wutz.dev/volumes"
	// rootVolumeName is the scratch emptyDir at /data; the volumes are mounted under it.
	rootVolumeName = "root"
	// fileServerContainer is the dufs container, also the name of its binary.
	fileServerContainer = "dufs"
	// activityProbeInterval is how often a running server's access log is read.
	activityProbeInterval = time.Minute
)

// ActivityFunc returns the time of the latest request served by a running
// file server pod (zero when unknown). It reads the dufs access log.
type ActivityFunc func(ctx context.Context, namespace, pod string, since time.Time) (time.Time, error)

// FileServerReconciler runs dufs for the volumes of a namespace while it is
// in use and scales it to zero after idleTimeout.
type FileServerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Config   *platform.Loader
	Activity ActivityFunc
	Now      func() time.Time
}

// +kubebuilder:rbac:groups=forgeai.wutz.dev,resources=fileservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=forgeai.wutz.dev,resources=fileservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=forgeai.wutz.dev,resources=fileservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

func (r *FileServerReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func lastActive(fs *v1.FileServer) time.Time {
	var t time.Time
	if s := fs.Annotations[naming.AnnotationLastActive]; s != "" {
		t, _ = time.Parse(time.RFC3339, s)
	}
	if st := fs.Status.LastActiveTime; st != nil && st.After(t) {
		t = st.Time
	}
	return t
}

func (r *FileServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	fs := &v1.FileServer{}
	if err := r.Get(ctx, req.NamespacedName, fs); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !fs.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	cfg, err := r.Config.Get(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	orig := fs.DeepCopy()
	name := "forgeai-files-" + fs.Name
	idle := cfg.FileServer.IdleTimeout.Duration
	if fs.Spec.IdleTimeout != nil {
		idle = fs.Spec.IdleTimeout.Duration
	}

	vols, err := r.volumes(ctx, fs)
	if err != nil {
		return ctrl.Result{}, err
	}
	secret, err := r.ensureSecret(ctx, fs)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Activity from the access log of the running pod.
	now := r.now()
	active := lastActive(fs)
	dep := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Namespace: fs.Namespace, Name: name}, dep)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	running := err == nil && dep.Status.ReadyReplicas > 0
	if running && r.Activity != nil {
		if t, err := r.podActivity(ctx, fs.Namespace, name, active); err != nil {
			log.V(1).Info("Could not read file server activity", "error", err.Error())
		} else if t.After(active) {
			active = t
		}
	}
	if !active.IsZero() {
		fs.Status.LastActiveTime = &metav1.Time{Time: active}
	}
	wantUp := !active.IsZero() && now.Sub(active) < idle && len(vols) > 0

	host := cfg.Ingress.Host(cfg.FileServer.HostPrefix + fs.Namespace)
	if fs.Name != naming.FileServerName {
		host = cfg.Ingress.Host(cfg.FileServer.HostPrefix + fs.Name + "-" + fs.Namespace)
	}
	labels := map[string]string{
		naming.LabelManagedBy: naming.ManagedBy, naming.LabelComponent: naming.ComponentFileServer,
		labelInstance: name,
	}
	replicas := int32(0)
	if wantUp {
		replicas = 1
	}
	objs := []client.Object{
		fileServerDeployment(fs, cfg, name, labels, vols, secret, replicas),
		fileServerService(fs, name, labels),
		fileServerIngress(fs, cfg, name, host, labels),
	}
	for _, o := range objs {
		if err := ctrl.SetControllerReference(fs, o, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := apply(ctx, r.Client, o); err != nil {
			return ctrl.Result{}, fmt.Errorf("apply %T: %w", o, err)
		}
	}

	fs.Status.URL = cfg.Ingress.URL(strings.TrimSuffix(host, "."+cfg.Ingress.Domain))
	fs.Status.Secret = naming.FileServerSecretName
	fs.Status.Volumes = vols
	switch {
	case !wantUp:
		fs.Status.Phase = v1.FileServerIdle
	case running && dep.Status.UpdatedReplicas == dep.Status.Replicas && dep.Generation == dep.Status.ObservedGeneration:
		fs.Status.Phase = v1.FileServerReady
	default:
		fs.Status.Phase = v1.FileServerStarting
	}
	msg := ""
	if len(vols) == 0 {
		msg = "no ReadWriteMany volumes to serve"
	}
	meta.SetStatusCondition(&fs.Status.Conditions, metav1.Condition{
		Type: "Ready", Status: conditionStatus(fs.Status.Phase == v1.FileServerReady),
		Reason: string(fs.Status.Phase), Message: msg, ObservedGeneration: fs.Generation,
	})
	if fs.Status.Phase != orig.Status.Phase {
		log.Info("FileServer changed phase", "phase", fs.Status.Phase, "volumes", len(vols))
	}
	if err := r.Status().Patch(ctx, fs, client.MergeFrom(orig)); err != nil {
		return ctrl.Result{}, err
	}
	if !wantUp {
		return ctrl.Result{}, nil
	}
	// Check activity again before the idle deadline.
	return ctrl.Result{RequeueAfter: min(idle-now.Sub(active)+time.Second, activityProbeInterval)}, nil
}

func conditionStatus(b bool) metav1.ConditionStatus {
	if b {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

// volumes returns the PVCs to serve: the listed ones or every live
// ReadWriteMany volume, skipping trashed and missing claims.
func (r *FileServerReconciler) volumes(ctx context.Context, fs *v1.FileServer) ([]string, error) {
	list := &corev1.PersistentVolumeClaimList{}
	if err := r.List(ctx, list, client.InNamespace(fs.Namespace)); err != nil {
		return nil, err
	}
	out := []string{}
	for i := range list.Items {
		pvc := &list.Items[i]
		if volumes.InTrash(pvc) || !pvc.DeletionTimestamp.IsZero() {
			continue
		}
		if len(fs.Spec.Volumes) > 0 {
			if slices.Contains(fs.Spec.Volumes, pvc.Name) {
				out = append(out, pvc.Name)
			}
			continue
		}
		if slices.Contains(pvc.Spec.AccessModes, corev1.ReadWriteMany) {
			out = append(out, pvc.Name)
		}
	}
	slices.Sort(out)
	return out, nil
}

func (r *FileServerReconciler) ensureSecret(ctx context.Context, fs *v1.FileServer) (*corev1.Secret, error) {
	s := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Namespace: fs.Namespace, Name: naming.FileServerSecretName}, s)
	if err == nil && len(s.Data["password"]) > 0 {
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
			Name: naming.FileServerSecretName, Namespace: fs.Namespace,
			Labels: map[string]string{naming.LabelManagedBy: naming.ManagedBy, naming.LabelComponent: naming.ComponentFileServer},
		},
		Type: corev1.SecretTypeBasicAuth,
		Data: map[string][]byte{
			corev1.BasicAuthUsernameKey: []byte(fileServerUser),
			corev1.BasicAuthPasswordKey: []byte(hex.EncodeToString(b)),
		},
	}
	if err := ctrl.SetControllerReference(fs, s, r.Scheme); err != nil {
		return nil, err
	}
	return s, apply(ctx, r.Client, s)
}

func fileServerDeployment(fs *v1.FileServer, cfg *platform.Config, name string, labels map[string]string,
	vols []string, secret *corev1.Secret, replicas int32) *appsv1.Deployment {
	var podVols []corev1.Volume
	var mounts []corev1.VolumeMount
	for _, v := range vols {
		podVols = append(podVols, corev1.Volume{Name: "v-" + v, VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: v},
		}})
		mounts = append(mounts, corev1.VolumeMount{Name: "v-" + v, MountPath: "/data/" + v})
	}
	// The password is part of the auth rule; it is passed through the env from
	// the Secret so it never shows in the pod spec.
	env := []corev1.EnvVar{
		{Name: "DUFS_PASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name}, Key: corev1.BasicAuthPasswordKey,
		}}},
	}
	// dufs serves /data, but only the volumes are writable: every other path is
	// read-only, so uploads to the root are refused rather than landing in the
	// scratch emptyDir. The image has no shell, so dufs gets its arguments
	// directly; the kubelet expands $(DUFS_PASSWORD) from the Secret env.
	rules := make([]string, 0, 1+len(vols))
	rules = append(rules, "/:ro")
	for _, v := range vols {
		rules = append(rules, "/"+v+":rw")
	}
	auth := fileServerUser + ":$(DUFS_PASSWORD)@" + strings.Join(rules, ",")
	args := []string{
		"/data", "--bind", "0.0.0.0", "--port", strconv.Itoa(fileServerPort),
		"--allow-all", "--enable-cors", "--auth", auth,
		"--log-format", `$time_iso8601 $remote_user "$request" $status`,
	}
	res := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1Gi")}
	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: fs.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{labelInstance: name}},
			// Only one pod may hold the volumes at a time.
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: map[string]string{
					annotationServedVolumes: strings.Join(vols, ","),
				}},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: new(false),
					EnableServiceLinks:           new(false),
					SecurityContext: &corev1.PodSecurityContext{
						// Files keep the ownership training jobs use (fsGroup 1000 in the past).
						RunAsUser: new(int64(1000)), RunAsGroup: new(int64(1000)), FSGroup: new(int64(1000)),
						RunAsNonRoot: new(true), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
						FSGroupChangePolicy: new(corev1.FSGroupChangeOnRootMismatch),
					},
					Containers: []corev1.Container{{
						Name: fileServerContainer, Image: cfg.FileServer.Image,
						Command: []string{fileServerContainer},
						Args:    args,
						Env:     env,
						Ports:   []corev1.ContainerPort{{Name: fileServerPortName, ContainerPort: fileServerPort}},
						ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
							Path: "/__dufs__/health", Port: intstr.FromString(fileServerPortName),
						}}, PeriodSeconds: 5},
						Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi"),
						}, Limits: res},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: new(false), ReadOnlyRootFilesystem: new(true),
							Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
						// The volumes nest under root, so runc creates their mountpoints
						// in it; root is writable at the mount level and dufs' rules
						// keep it read-only.
						VolumeMounts: append(mounts, corev1.VolumeMount{Name: rootVolumeName, MountPath: "/data"}),
					}},
					Volumes: append(podVols, corev1.Volume{Name: rootVolumeName, VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: new(resource.MustParse("1Mi"))},
					}}),
				},
			},
		},
	}
}

func fileServerService(fs *v1.FileServer, name string, labels map[string]string) *corev1.Service {
	return &corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: fs.Namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{labelInstance: name},
			Ports:    []corev1.ServicePort{{Name: fileServerPortName, Port: 80, TargetPort: intstr.FromString(fileServerPortName)}},
		},
	}
}

func fileServerIngress(fs *v1.FileServer, cfg *platform.Config, name, host string, labels map[string]string) *networkingv1.Ingress {
	pt := networkingv1.PathTypePrefix
	ing := &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "Ingress"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: fs.Namespace, Labels: labels, Annotations: map[string]string{
			// Large uploads: no body limit, stream instead of buffering, long timeouts.
			"nginx.ingress.kubernetes.io/proxy-body-size":         "0",
			"nginx.ingress.kubernetes.io/proxy-request-buffering": "off",
			"nginx.ingress.kubernetes.io/proxy-buffering":         "off",
			"nginx.ingress.kubernetes.io/proxy-read-timeout":      "3600",
			"nginx.ingress.kubernetes.io/proxy-send-timeout":      "3600",
		}},
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{{Hosts: []string{host}, SecretName: cfg.Ingress.TLSSecret}},
			Rules: []networkingv1.IngressRule{{Host: host, IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
					Path: "/", PathType: &pt,
					Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
						Name: name, Port: networkingv1.ServiceBackendPort{Name: fileServerPortName},
					}},
				}}},
			}}},
		},
	}
	if cfg.Ingress.ClassName != "" {
		ing.Spec.IngressClassName = &cfg.Ingress.ClassName
	}
	return ing
}

func (r *FileServerReconciler) podActivity(ctx context.Context, ns, name string, since time.Time) (time.Time, error) {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(ns), client.MatchingLabels{labelInstance: name}); err != nil {
		return time.Time{}, err
	}
	var latest time.Time
	for _, p := range pods.Items {
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		t, err := r.Activity(ctx, ns, p.Name, since)
		if err != nil {
			return latest, err
		}
		if t.After(latest) {
			latest = t
		}
	}
	return latest, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FileServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Volume changes in a namespace update its file servers.
	byNamespace := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		list := &v1.FileServerList{}
		if err := mgr.GetClient().List(ctx, list, client.InNamespace(o.GetNamespace())); err != nil {
			return nil
		}
		out := make([]reconcile.Request, 0, len(list.Items))
		for _, fs := range list.Items {
			out = append(out, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&fs)})
		}
		return out
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.FileServer{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{}, predicate.AnnotationChangedPredicate{}))).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Watches(&corev1.PersistentVolumeClaim{}, byNamespace, builder.WithPredicates(predicate.Or(
			predicate.LabelChangedPredicate{}, predicate.GenerationChangedPredicate{}))).
		Named("fileserver").
		Complete(r)
}

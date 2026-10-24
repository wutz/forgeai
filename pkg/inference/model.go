package inference

import (
	"fmt"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/instancetype"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
)

const (
	// ModelPort is the port of the vLLM server in a model server pod.
	ModelPort = 8000
	// routerPort is the Envoy port of the llm-d router; its Service exposes it
	// as port 80.
	routerPort = 8081
	portName   = "http"
	// hfCacheDir is the Hugging Face cache: a claim when the platform sets a
	// cache storage class, so a restart loads the weights again, and an
	// emptyDir otherwise.
	hfCacheDir = "/root/.cache/huggingface"
	// shutdownTimeout is how long vLLM drains requests on shutdown, in seconds.
	shutdownTimeout = 45
	// startupFailures allows the model to start for up to an hour: a cold start
	// downloads the weights (tens of GB, over a mirror at a few MB/s) before it
	// loads them into the GPUs.
	startupFailures = 360
)

// Objects returns the objects of svc: the vLLM model servers on it, the llm-d
// router in front of them and the Ingress of the endpoint. The model servers
// run replicas pods, each with the GPUs of it; the router runs while replicas
// is above zero. The API key Secret is not among them: it is created once and
// kept (see naming.InferenceAPIKeySecret).
func Objects(
	svc *v1.InferenceService, cfg *platform.Config, it *v1.InstanceType, replicas int32,
) ([]client.Object, error) {
	if it.Spec.GPU.Count < 1 {
		return nil, fmt.Errorf("InstanceType %s has no GPU", it.Name)
	}
	router, err := Router(svc.Name, svc.Namespace, cfg.Inference.RouterImage, min(replicas, 1))
	if err != nil {
		return nil, err
	}
	objs := make([]client.Object, 0, len(router)+3)
	for _, o := range router {
		objs = append(objs, o)
	}
	if claim := CacheClaim(svc, cfg); claim != nil {
		objs = append(objs, claim)
	}
	return append(objs, modelDeployment(svc, cfg, it, replicas), ingress(svc, cfg)), nil
}

// CacheClaimName is the name of the claim holding the weights of svc.
func CacheClaimName(svc *v1.InferenceService) string { return svc.Name + "-hf-cache" }

// CacheClaim is the claim the model servers keep the downloaded weights on, or
// nil when the platform configures no cache.
func CacheClaim(svc *v1.InferenceService, cfg *platform.Config) *corev1.PersistentVolumeClaim {
	cache := cfg.Inference.Cache
	if !cache.Enabled() {
		return nil
	}
	cls := cache.StorageClass
	return &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"},
		ObjectMeta: metav1.ObjectMeta{Name: CacheClaimName(svc), Namespace: svc.Namespace,
			Labels: map[string]string{
				naming.LabelManagedBy: naming.ManagedBy, naming.LabelComponent: naming.ComponentInference,
				naming.LabelInference: svc.Name,
			}},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			StorageClassName: &cls,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: cache.Size},
			},
		},
	}
}

// ModelName is the name of the model server Deployment of svc.
func ModelName(svc *v1.InferenceService) string { return svc.Name + "-model" }

// EPPName is the name of the router's Endpoint Picker, which is also the name
// of its Service and ServiceAccount.
func EPPName(name string) string { return name + "-epp" }

// Host is the host name of the endpoint of svc.
func Host(svc *v1.InferenceService, cfg *platform.Config) string {
	return cfg.Ingress.Host(endpointSub(svc))
}

// Endpoint is the base URL of the OpenAI-compatible API of svc.
func Endpoint(svc *v1.InferenceService, cfg *platform.Config) string {
	return cfg.Ingress.URL(endpointSub(svc)) + "/v1"
}

func endpointSub(svc *v1.InferenceService) string { return svc.Name + "-" + svc.Namespace }

// ServedName is the model name clients send: the served model name, or the
// repository name of the model.
func ServedName(svc *v1.InferenceService) string {
	if svc.Spec.ServedModelName != "" {
		return svc.Spec.ServedModelName
	}
	_, repo, _ := strings.Cut(svc.Spec.Model, "/")
	return repo
}

// modelDeployment runs vllm serve with the GPUs of it. The tensor parallel size
// is the GPU count, so one replica shards the model over its GPUs.
func modelDeployment(
	svc *v1.InferenceService, cfg *platform.Config, it *v1.InstanceType, replicas int32,
) *appsv1.Deployment {
	name := ModelName(svc)
	labels := map[string]string{
		naming.LabelManagedBy: naming.ManagedBy, naming.LabelComponent: naming.ComponentInference,
		naming.LabelInference: svc.Name,
	}
	selector := map[string]string{
		naming.LabelInference: svc.Name, naming.LabelComponent: naming.ComponentInference,
	}
	image := cfg.Inference.Image
	if svc.Spec.Image != "" {
		image = svc.Spec.Image
	}
	gpus := it.Spec.GPU.Count
	args := []string{
		svc.Spec.Model, "--served-model-name", ServedName(svc),
		"--host", "0.0.0.0", "--port", strconv.Itoa(ModelPort),
		"--tensor-parallel-size", strconv.Itoa(int(gpus)),
		"--shutdown-timeout", strconv.Itoa(shutdownTimeout),
	}
	// Qwen3 answers with reasoning and tool calls in its own format; the
	// parsers turn them into the OpenAI fields.
	if strings.Contains(strings.ToLower(svc.Spec.Model), "qwen3") {
		args = append(args, "--reasoning-parser", "qwen3", "--tool-call-parser", "qwen3_coder", "--enable-auto-tool-choice")
	}
	args = append(args, svc.Spec.Args...)

	keySecret := secretKey(naming.InferenceAPIKeySecret(svc.Name), naming.InferenceAPIKey, false)
	env := []corev1.EnvVar{{Name: "VLLM_API_KEY", ValueFrom: keySecret}}
	if svc.Spec.HuggingFaceSecret != "" {
		env = append(env, corev1.EnvVar{Name: "HF_TOKEN", ValueFrom: secretKey(svc.Spec.HuggingFaceSecret, "HF_TOKEN", true)})
	}
	// A cluster without access to huggingface.co downloads through a mirror. The
	// Xet transfer protocol goes to the Hugging Face CAS servers directly, which
	// neither the mirror nor the cluster reaches, so weights come over plain HTTP.
	if cfg.Inference.HFEndpoint != "" {
		env = append(env,
			corev1.EnvVar{Name: "HF_ENDPOINT", Value: cfg.Inference.HFEndpoint},
			corev1.EnvVar{Name: "HF_HUB_DISABLE_XET", Value: "1"})
	}
	// Requests equal limits: the GPUs are extended resources, which need it,
	// and the CPU and memory are the share of the InstanceType.
	res := corev1.ResourceList{
		corev1.ResourceCPU:       it.Spec.CPU,
		corev1.ResourceMemory:    it.Spec.Memory,
		instancetype.ResourceGPU: *resource.NewQuantity(int64(gpus), resource.DecimalSI),
	}
	health := corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt32(ModelPort)}}
	shm := cfg.Inference.ShmSize
	volumes := []corev1.Volume{
		{Name: "dshm", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{
			Medium: corev1.StorageMediumMemory, SizeLimit: &shm,
		}}},
		{Name: "hf-cache", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	if cfg.Inference.Cache.Enabled() {
		volumes[1].VolumeSource = corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: CacheClaimName(svc),
		}}
	}
	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: svc.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selector},
			// A replica holds its GPUs until it is gone, so the old one stops first.
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "vllm", Image: image,
						Command: []string{"vllm", "serve"},
						Args:    args,
						Env:     env,
						Ports:   []corev1.ContainerPort{{Name: portName, ContainerPort: ModelPort}},
						// The weights take minutes to load, so the readiness check
						// waits for a startup probe to pass first.
						StartupProbe:   &corev1.Probe{ProbeHandler: health, PeriodSeconds: 10, FailureThreshold: startupFailures},
						ReadinessProbe: &corev1.Probe{ProbeHandler: health, PeriodSeconds: 10},
						Resources:      corev1.ResourceRequirements{Requests: res, Limits: res},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "dshm", MountPath: "/dev/shm"},
							{Name: "hf-cache", MountPath: hfCacheDir},
						},
					}},
					Volumes:      volumes,
					NodeSelector: it.Spec.NodeSelector,
					Tolerations: []corev1.Toleration{{
						Key: string(instancetype.ResourceGPU), Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule,
					}},
					TerminationGracePeriodSeconds: new(int64(shutdownTimeout + 15)),
				},
			},
		},
	}
}

func secretKey(name, key string, optional bool) *corev1.EnvVarSource {
	return &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: name}, Key: key, Optional: new(optional),
	}}
}

// ingress exposes the router's Service on the endpoint host. Completions stream,
// so buffering is off and the timeouts are long.
func ingress(svc *v1.InferenceService, cfg *platform.Config) *networkingv1.Ingress {
	pt := networkingv1.PathTypePrefix
	host := Host(svc, cfg)
	ing := &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "Ingress"},
		ObjectMeta: metav1.ObjectMeta{Name: svc.Name, Namespace: svc.Namespace,
			Labels: map[string]string{
				naming.LabelManagedBy: naming.ManagedBy, naming.LabelComponent: naming.ComponentInference,
				naming.LabelInference: svc.Name,
			},
			Annotations: map[string]string{
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
						Name: EPPName(svc.Name), Port: networkingv1.ServiceBackendPort{Name: portName},
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

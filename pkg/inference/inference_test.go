package inference

import (
	"maps"
	"slices"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
)

const testImage = "ghcr.io/llm-d/llm-d-router-endpoint-picker:v1.0.0"

func testService() *v1.InferenceService {
	return &v1.InferenceService{
		ObjectMeta: metav1.ObjectMeta{Name: "qwen", Namespace: "forge-demo"},
		Spec: v1.InferenceServiceSpec{
			Model:        "Qwen/Qwen3.8-27B-FP8",
			InstanceType: "rtx-5090.x2",
			Replicas:     1,
		},
	}
}

func testInstanceType() *v1.InstanceType {
	return &v1.InstanceType{
		ObjectMeta: metav1.ObjectMeta{Name: "rtx-5090.x2"},
		Spec: v1.InstanceTypeSpec{
			GPU:          v1.InstanceTypeGPU{Class: "rtx-5090", Count: 2},
			CPU:          resource.MustParse("28"),
			Memory:       resource.MustParse("200Gi"),
			NodeSelector: map[string]string{naming.LabelGPUProduct: "NVIDIA-GeForce-RTX-5090"},
		},
	}
}

func testConfig() *platform.Config {
	cfg := &platform.Config{}
	cfg.Ingress.Domain = "dev1.bz1.paratera.com"
	cfg.Ingress.Port = 7443
	cfg.Inference.Image = "vllm/vllm-openai:v0.32.0"
	cfg.Inference.RouterImage = testImage
	cfg.Inference.ShmSize = resource.MustParse("16Gi")
	cfg.Inference.HFEndpoint = "https://hf-mirror.com"
	cfg.Inference.StartupTimeout = metav1.Duration{Duration: 2 * time.Hour}
	return cfg
}

func objectsOf[T client.Object](t *testing.T, objs []client.Object, kind string) []T {
	t.Helper()
	var out []T
	for _, o := range objs {
		if v, ok := o.(T); ok {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		t.Fatalf("no %s in the objects", kind)
	}
	return out
}

func unstructuredNamed(t *testing.T, objs []client.Object, kind, name string) *unstructured.Unstructured {
	t.Helper()
	for _, o := range objs {
		if u, ok := o.(*unstructured.Unstructured); ok && u.GetKind() == kind && u.GetName() == name {
			return u
		}
	}
	t.Fatalf("no %s %s in the objects", kind, name)
	return nil
}

func TestRouterRendersEndpointPickerForModelServers(t *testing.T) {
	objs, err := Router("qwen", "forge-demo", testImage, 1)
	if err != nil {
		t.Fatal(err)
	}
	find := func(kind, name string) *unstructured.Unstructured {
		t.Helper()
		for _, u := range objs {
			if u.GetKind() == kind && u.GetName() == name {
				return u
			}
		}
		t.Fatalf("no %s %s rendered", kind, name)
		return nil
	}

	pool := find("InferencePool", "qwen")
	selector, _, _ := unstructured.NestedStringMap(pool.Object, "spec", "selector", "matchLabels")
	if selector[naming.LabelInference] != "qwen" {
		t.Errorf("InferencePool selects %v, want %s=qwen", selector, naming.LabelInference)
	}
	epp := find("Deployment", "qwen-epp")
	if got, _, _ := unstructured.NestedFieldNoCopy(epp.Object, "spec", "replicas"); got != int64(1) {
		t.Errorf("EPP replicas %v, want 1", got)
	}
	if image := findContainerImage(t, epp); image != testImage {
		t.Errorf("EPP image %q, want %q", image, testImage)
	}
	// The Envoy ConfigMap is named per router: the chart's default would clash.
	find("ConfigMap", "qwen-envoy")
	for _, u := range objs {
		if u.GetKind() == "ClusterRole" {
			t.Errorf("rendered cluster-scoped %s %s; the router must stay namespaced", u.GetKind(), u.GetName())
		}
		if u.GetNamespace() != "forge-demo" {
			t.Errorf("%s %s in namespace %q, want forge-demo", u.GetKind(), u.GetName(), u.GetNamespace())
		}
		// The manager's cache keeps only objects labelled managed-by, so an
		// unlabelled router Deployment or Service reads as absent.
		if got := u.GetLabels()[naming.LabelManagedBy]; got != naming.ManagedBy {
			t.Errorf("%s %s label %s=%q, want %q", u.GetKind(), u.GetName(), naming.LabelManagedBy, got, naming.ManagedBy)
		}
	}
}

func TestRouterScalesEndpointPickerToZero(t *testing.T) {
	objs, err := Router("qwen", "forge-demo", testImage, 0)
	if err != nil {
		t.Fatal(err)
	}
	epp := unstructuredNamed(t, asObjects(objs), "Deployment", "qwen-epp")
	if got, _, _ := unstructured.NestedFieldNoCopy(epp.Object, "spec", "replicas"); got != int64(0) {
		t.Errorf("EPP replicas %v, want 0 when the service is stopped", got)
	}
}

func TestObjectsServeModelWithTensorParallelGPUs(t *testing.T) {
	svc := testService()
	svc.Spec.Args = []string{"--max-model-len", "65536"}
	svc.Spec.HuggingFaceSecret = "hf"
	objs, err := Objects(svc, testConfig(), testInstanceType(), 1)
	if err != nil {
		t.Fatal(err)
	}

	dep := objectsOf[*appsv1.Deployment](t, objs, "Deployment")
	var model *appsv1.Deployment
	for _, d := range dep {
		if d.Name == "qwen-model" {
			model = d
		}
	}
	if model == nil {
		t.Fatal("no qwen-model Deployment")
	}
	if *model.Spec.Replicas != 1 {
		t.Errorf("replicas %d, want 1", *model.Spec.Replicas)
	}
	c := model.Spec.Template.Spec.Containers[0]
	if !slices.Equal(c.Command, []string{"vllm", "serve"}) {
		t.Errorf("command %v, want vllm serve", c.Command)
	}
	for _, want := range [][]string{
		{"Qwen/Qwen3.8-27B-FP8"},
		{"--served-model-name", "Qwen3.8-27B-FP8"},
		{"--tensor-parallel-size", "2"},
		{"--reasoning-parser", "qwen3"},
		{"--tool-call-parser", "qwen3_coder"},
		{"--enable-auto-tool-choice"},
		{"--max-model-len", "65536"},
	} {
		if !containsSeq(c.Args, want) {
			t.Errorf("args %v miss %v", c.Args, want)
		}
	}
	gpu := c.Resources.Limits["nvidia.com/gpu"]
	if gpu.Value() != 2 {
		t.Errorf("GPU limit %s, want 2", gpu.String())
	}
	req := c.Resources.Requests["nvidia.com/gpu"]
	if req.Cmp(gpu) != 0 {
		t.Errorf("GPU requests %s differ from limits %s", req.String(), gpu.String())
	}
	if env := findEnv(c.Env, "VLLM_API_KEY"); env == nil || env.ValueFrom.SecretKeyRef.Name != "qwen-api-key" {
		t.Errorf("VLLM_API_KEY %+v, want the qwen-api-key Secret", env)
	}
	if env := findEnv(c.Env, "HF_TOKEN"); env == nil || env.ValueFrom.SecretKeyRef.Name != "hf" || !*env.ValueFrom.SecretKeyRef.Optional {
		t.Errorf("HF_TOKEN %+v, want the optional key of the hf Secret", env)
	}
	if env := findEnv(c.Env, "HF_ENDPOINT"); env == nil || env.Value != "https://hf-mirror.com" {
		t.Errorf("HF_ENDPOINT %+v, want the platform mirror", env)
	}
	if env := findEnv(c.Env, "HF_HUB_DISABLE_XET"); env == nil || env.Value != "1" {
		t.Errorf("HF_HUB_DISABLE_XET %+v, want Xet off when downloading through a mirror", env)
	}
	spec := model.Spec.Template.Spec
	if p := spec.Containers[0].StartupProbe; p == nil || p.FailureThreshold*p.PeriodSeconds < 7200 {
		t.Errorf("startup probe %+v, want the configured two hours for a cold start", p)
	}
	if spec.NodeSelector[naming.LabelGPUProduct] != "NVIDIA-GeForce-RTX-5090" {
		t.Errorf("node selector %v misses the GPU product", spec.NodeSelector)
	}
	if len(spec.Tolerations) != 1 || spec.Tolerations[0].Key != "nvidia.com/gpu" {
		t.Errorf("tolerations %+v, want the GPU taint", spec.Tolerations)
	}

	ing := objectsOf[*networkingv1.Ingress](t, objs, "Ingress")[0]
	if host := ing.Spec.Rules[0].Host; host != "qwen-forge-demo.dev1.bz1.paratera.com" {
		t.Errorf("ingress host %q", host)
	}
	backend := ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service
	if backend.Name != "qwen-epp" || backend.Port.Name != "http" {
		t.Errorf("ingress backend %+v, want the router Service", backend)
	}
}

func TestObjectsMergeTheNodeSelector(t *testing.T) {
	svc := testService()
	svc.Spec.NodeSelector = map[string]string{
		"kubernetes.io/hostname": "gn-10-243-145-105",
		// The service can override what the InstanceType sets.
		naming.LabelGPUProduct: "other",
	}
	it := testInstanceType()
	objs, err := Objects(svc, testConfig(), it, 1)
	if err != nil {
		t.Fatal(err)
	}
	var model *appsv1.Deployment
	for _, d := range objectsOf[*appsv1.Deployment](t, objs, "Deployment") {
		if d.Name == "qwen-model" {
			model = d
		}
	}
	if model == nil {
		t.Fatal("no qwen-model Deployment")
	}
	want := map[string]string{"kubernetes.io/hostname": "gn-10-243-145-105", naming.LabelGPUProduct: "other"}
	if !maps.Equal(model.Spec.Template.Spec.NodeSelector, want) {
		t.Errorf("node selector %v, want %v", model.Spec.Template.Spec.NodeSelector, want)
	}
	// The InstanceType's own map is not mutated by the merge.
	if it.Spec.NodeSelector[naming.LabelGPUProduct] != "NVIDIA-GeForce-RTX-5090" {
		t.Errorf("InstanceType node selector changed to %v", it.Spec.NodeSelector)
	}
}

func TestObjectsMountWeightsCache(t *testing.T) {
	cfg := testConfig()
	cfg.Inference.Cache.StorageClass = "shared-vast-cs"
	cfg.Inference.Cache.Size = resource.MustParse("200Gi")
	objs, err := Objects(testService(), cfg, testInstanceType(), 1)
	if err != nil {
		t.Fatal(err)
	}
	claim := objectsOf[*corev1.PersistentVolumeClaim](t, objs, "PersistentVolumeClaim")[0]
	if claim.Name != "qwen-hf-cache" || *claim.Spec.StorageClassName != "shared-vast-cs" {
		t.Errorf("claim %s class %v", claim.Name, claim.Spec.StorageClassName)
	}
	var model *appsv1.Deployment
	for _, d := range objectsOf[*appsv1.Deployment](t, objs, "Deployment") {
		if d.Name == "qwen-model" {
			model = d
		}
	}
	if model == nil {
		t.Fatal("no qwen-model Deployment")
	}
	pvc := model.Spec.Template.Spec.Volumes[1]
	if pvc.PersistentVolumeClaim == nil || pvc.PersistentVolumeClaim.ClaimName != "qwen-hf-cache" {
		t.Errorf("hf-cache volume %+v, want the cache claim", pvc.VolumeSource)
	}
}

func TestCacheClaimWithoutStorageClassUsesDefault(t *testing.T) {
	cfg := testConfig()
	cfg.Inference.Cache.Size = resource.MustParse("200Gi")
	claim := CacheClaim(testService(), cfg)
	if claim == nil {
		t.Fatal("no claim for a cache with a size")
	}
	// A set but empty class would never bind; nil leaves it to the default.
	if claim.Spec.StorageClassName != nil {
		t.Errorf("storage class %q, want the default (nil)", *claim.Spec.StorageClassName)
	}
	if got := claim.Spec.Resources.Requests.Storage().String(); got != "200Gi" {
		t.Errorf("size %s, want 200Gi", got)
	}
}

func TestStartupFailureThreshold(t *testing.T) {
	for _, tc := range []struct {
		timeout time.Duration
		want    int32
	}{
		{2 * time.Hour, 720},
		{startupPeriodSeconds * time.Second, 1},
		{time.Second, 1},
		{0, 1},
	} {
		cfg := testConfig()
		cfg.Inference.StartupTimeout = metav1.Duration{Duration: tc.timeout}
		if got := startupFailureThreshold(cfg); got != tc.want {
			t.Errorf("timeout %s: threshold %d, want %d", tc.timeout, got, tc.want)
		}
	}
}

func TestObjectsUseEmptyDirWithoutCache(t *testing.T) {
	objs, err := Objects(testService(), testConfig(), testInstanceType(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objs {
		if _, ok := o.(*corev1.PersistentVolumeClaim); ok {
			t.Fatalf("rendered %s without a configured cache", o.GetName())
		}
	}
}

func TestObjectsStoppedScalesModelServersToZero(t *testing.T) {
	objs, err := Objects(testService(), testConfig(), testInstanceType(), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range objectsOf[*appsv1.Deployment](t, objs, "Deployment") {
		if *d.Spec.Replicas != 0 {
			t.Errorf("%s replicas %d, want 0", d.Name, *d.Spec.Replicas)
		}
	}
}

func TestObjectsRejectInstanceTypeWithoutGPU(t *testing.T) {
	it := testInstanceType()
	it.Spec.GPU.Count = 0
	if _, err := Objects(testService(), testConfig(), it, 1); err == nil {
		t.Error("got no error for an InstanceType without GPUs")
	}
}

func TestEndpointAndServedName(t *testing.T) {
	cfg := testConfig()
	svc := testService()
	if got, want := Endpoint(svc, cfg), "https://qwen-forge-demo.dev1.bz1.paratera.com:7443/v1"; got != want {
		t.Errorf("endpoint %q, want %q", got, want)
	}
	if got := ServedName(svc); got != "Qwen3.8-27B-FP8" {
		t.Errorf("served name %q", got)
	}
	svc.Spec.ServedModelName = "qwen"
	if got := ServedName(svc); got != "qwen" {
		t.Errorf("served name %q, want the explicit one", got)
	}
}

func TestSplitImage(t *testing.T) {
	reg, repo, tag := splitImage(testImage)
	if reg != "ghcr.io/llm-d" || repo != "llm-d-router-endpoint-picker" || tag != "v1.0.0" {
		t.Errorf("split %q %q %q", reg, repo, tag)
	}
	reg, repo, tag = splitImage("localhost:5000/router")
	if reg != "localhost:5000" || repo != "router" || tag != "" {
		t.Errorf("split %q %q %q", reg, repo, tag)
	}
}

func asObjects(us []*unstructured.Unstructured) []client.Object {
	out := make([]client.Object, 0, len(us))
	for _, u := range us {
		out = append(out, u)
	}
	return out
}

func findContainerImage(t *testing.T, u *unstructured.Unstructured) string {
	t.Helper()
	containers, _, _ := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
	for _, c := range containers {
		m, _ := c.(map[string]any)
		if m["name"] == "epp" {
			image, _ := m["image"].(string)
			return image
		}
	}
	return ""
}

func findEnv(env []corev1.EnvVar, name string) *corev1.EnvVar {
	for i := range env {
		if env[i].Name == name {
			return &env[i]
		}
	}
	return nil
}

// containsSeq reports whether seq appears in args as consecutive elements.
func containsSeq(args, seq []string) bool {
	for i := range args {
		if i+len(seq) <= len(args) && slices.Equal(args[i:i+len(seq)], seq) {
			return true
		}
	}
	return false
}

func TestModelNameAndImageWithoutRegistryOrTag(t *testing.T) {
	svc := testService()
	if got, want := ModelName(svc), svc.Name+"-model"; got != want {
		t.Errorf("model deployment %q, want %q", got, want)
	}
	reg, repo, tag := splitImage("ghcr.io/llm-d/router")
	if reg != "ghcr.io/llm-d" || repo != "router" || tag != "" {
		t.Errorf("split without a tag %q %q %q", reg, repo, tag)
	}
	reg, repo, tag = splitImage("router:v2")
	if reg != "" || repo != "router" || tag != "v2" {
		t.Errorf("split of a bare name %q %q %q", reg, repo, tag)
	}
}

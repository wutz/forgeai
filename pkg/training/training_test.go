package training

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	instancetypev1 "github.com/wutz/forgeai-instancetype-operator/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
)

func instance() *instancetypev1.InstanceType {
	return &instancetypev1.InstanceType{
		ObjectMeta: metav1.ObjectMeta{Name: "rtx-5090.x2"},
		Spec: instancetypev1.InstanceTypeSpec{
			GPU: instancetypev1.InstanceTypeGPU{Class: "rtx-5090", Product: "NVIDIA-GeForce-RTX-5090", Count: 2},
			CPU: resource.MustParse("28"), Memory: resource.MustParse("234Gi"),
			NodeSelector: map[string]string{naming.LabelGPUProduct: "NVIDIA-GeForce-RTX-5090"},
		},
	}
}

func form() *Form {
	return &Form{
		Name: "demo", Image: "pytorch/pytorch:2", Command: "torchrun train.py",
		Env: []EnvVar{{Name: "A", Value: "1"}}, InstanceType: "rtx-5090.x2", Workers: 2,
		Volumes:    []VolumeMount{{Volume: "data", MountPath: "/data"}, {Volume: "data", MountPath: "/ckpt", SubPath: "ckpt"}},
		MaxRetries: 3,
	}
}

func TestTrainJob(t *testing.T) {
	cfg, _ := platform.Parse(nil)
	tj, err := TrainJob(form(), Context{Namespace: "forge-a", InstanceType: instance(), Config: cfg, RegistrySecret: "forgeai-registry"})
	if err != nil {
		t.Fatal(err)
	}
	if tj.Spec.RuntimeRef.Kind != "ClusterTrainingRuntime" || tj.Spec.RuntimeRef.Name != "torch-distributed" {
		t.Errorf("runtimeRef = %+v", tj.Spec.RuntimeRef)
	}
	tr := tj.Spec.Trainer
	if *tr.NumNodes != 2 || *tr.NumProcPerNode != 2 || tr.Command[2] != "torchrun train.py" {
		t.Errorf("trainer = %+v", tr)
	}
	if g := tr.ResourcesPerNode.Limits["nvidia.com/gpu"]; g.Value() != 2 {
		t.Errorf("gpus = %v", g)
	}
	if tj.Labels[kf.LabelKueueQueue] != "default" || tj.Annotations[naming.AnnotationMaxRetries] != "3" ||
		tj.Annotations[naming.AnnotationRetryOn] != "Infra" {
		t.Errorf("meta = %+v %+v", tj.Labels, tj.Annotations)
	}
	pod := tj.Spec.RuntimePatches[0].TrainingRuntimeSpec.Template.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	if len(pod.Volumes) != 2 || len(pod.Containers[0].VolumeMounts) != 3 || pod.ImagePullSecrets[0].Name != "forgeai-registry" {
		t.Errorf("pod patch = %+v", pod)
	}
	// The patch must serialise without empty objects the CRD would reject.
	b, _ := json.Marshal(tj)
	if strings.Contains(b2s(b), `"metadata":{"creationTimestamp"`) {
		t.Errorf("unexpected metadata: %s", b)
	}

	f := form()
	f.Template = "tmpl"
	tj, _ = TrainJob(f, Context{Namespace: "forge-a", InstanceType: instance(), Config: cfg})
	if tj.Spec.RuntimeRef.Kind != "TrainingRuntime" || tj.Spec.RuntimeRef.Name != "tmpl" || tj.Labels[naming.LabelTemplate] != "tmpl" {
		t.Errorf("template ref = %+v", tj.Spec.RuntimeRef)
	}
	if _, err := TrainJob(f, Context{Namespace: "forge-a", Config: cfg}); err == nil {
		t.Error("expected missing instance type error")
	}
}

func b2s(b []byte) string { return string(b) }

func TestValidate(t *testing.T) {
	for _, mut := range []func(*Form){
		func(f *Form) { f.Name = "Bad" },
		func(f *Form) { f.Name = "1abc" },
		func(f *Form) { f.Name = strings.Repeat("a", 51) },
		func(f *Form) { f.Image = " " },
		func(f *Form) { f.Workers = 0 },
		func(f *Form) { f.RetryOn = "Always" },
		func(f *Form) { f.Env = append(f.Env, EnvVar{Name: "A"}) },
		func(f *Form) { f.Volumes = append(f.Volumes, VolumeMount{Volume: "x", MountPath: "/data"}) },
		func(f *Form) { f.Volumes = []VolumeMount{{Volume: "x", MountPath: "rel"}} },
		func(f *Form) { f.Volumes = []VolumeMount{{Volume: "x", MountPath: "/dev/shm"}} },
		func(f *Form) { f.Volumes = []VolumeMount{{Volume: "x", MountPath: "/data", SubPath: "../up"}} },
		func(f *Form) { f.Volumes = []VolumeMount{{Volume: "x", MountPath: "/data", SubPath: "/abs"}} },
		func(f *Form) { f.Env = []EnvVar{{Name: "BAD NAME", Value: "1"}} },
		func(f *Form) { f.Env = []EnvVar{{Name: "1A", Value: "1"}} },
	} {
		f := form()
		mut(f)
		if f.Validate() == nil {
			t.Errorf("expected error for %+v", f)
		}
	}
}

func TestTemplateRoundTrip(t *testing.T) {
	f := form()
	f.Description = "baseline"
	rt, err := Template(f, "forge-a")
	if err != nil {
		t.Fatal(err)
	}
	if rt.Labels[naming.LabelTemplate] != "true" || *rt.Spec.MLPolicy.NumNodes != 2 ||
		rt.Spec.Template.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.Containers[0].Image != "pytorch/pytorch:2" {
		t.Errorf("runtime = %+v", rt)
	}
	got, err := TemplateForm(rt)
	if err != nil {
		t.Fatal(err)
	}
	if got.Template != "demo" || got.Name != "" || got.InstanceType != "rtx-5090.x2" || got.MaxRetries != 3 || got.Description != "baseline" {
		t.Errorf("form = %+v", got)
	}

	// A runtime created outside forgeai.
	plain := &kf.TrainingRuntime{ObjectMeta: metav1.ObjectMeta{Name: "ext"}}
	plain.Spec.Template.Spec.ReplicatedJobs = []kf.ReplicatedJob{{Name: "node"}}
	plain.Spec.Template.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.Containers = []corev1.Container{{Name: "node", Image: "img"}}
	if got, _ := TemplateForm(plain); got.Image != "img" || got.Workers != 1 {
		t.Errorf("plain form = %+v", got)
	}
}

func terminated(node string, idx string, code int32, reason string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p" + idx, Labels: map[string]string{kf.LabelCompletion: idx}},
		Spec:       corev1.PodSpec{NodeName: node},
		Status: corev1.PodStatus{Phase: corev1.PodFailed, ContainerStatuses: []corev1.ContainerStatus{{
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: code, Reason: reason}},
		}}},
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name     string
		pods     []corev1.Pod
		notReady map[string]bool
		want     string
	}{
		{"exit code", []corev1.Pod{terminated("n1", "0", 1, "Error")}, nil, ReasonExitCode},
		{"oom wins over exit", []corev1.Pod{terminated("n1", "0", 1, "Error"), terminated("n2", "1", 137, "OOMKilled")}, nil, ReasonOOMKilled},
		{"node down", []corev1.Pod{terminated("n1", "0", 1, "Error"), terminated("n2", "1", 137, "Error")}, map[string]bool{"n2": true}, ReasonNodeFailure},
		{"none", nil, nil, ReasonUnknown},
	}
	for _, c := range cases {
		if got := Classify(c.pods, c.notReady); got.Reason != c.want {
			t.Errorf("%s: got %+v", c.name, got)
		}
	}
	ev := terminated("n1", "0", 143, "Error")
	ev.Status.Conditions = []corev1.PodCondition{{Type: corev1.DisruptionTarget, Status: corev1.ConditionTrue, Reason: "PreemptionByScheduler"}}
	if got := Classify([]corev1.Pod{ev}, nil); got.Reason != ReasonEvicted || !Retryable(got.Reason, RetryInfra) {
		t.Errorf("eviction: %+v", got)
	}
	if Retryable(ReasonExitCode, RetryInfra) || !Retryable(ReasonExitCode, RetryAny) || Retryable(ReasonImagePullFailed, RetryAny) {
		t.Error("retry policy")
	}
}

func TestStuck(t *testing.T) {
	now := time.Now()
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", CreationTimestamp: metav1.NewTime(now.Add(-20 * time.Minute)),
			Labels: map[string]string{kf.LabelCompletion: "0"}},
		Status: corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "not found"}},
		}}},
	}
	f, _ := Stuck([]corev1.Pod{p}, now, 15*time.Minute)
	if f == nil || f.Reason != ReasonImagePullFailed || !strings.Contains(f.Message, "worker-0") {
		t.Fatalf("stuck = %+v", f)
	}
	f, next := Stuck([]corev1.Pod{p}, now, 30*time.Minute)
	if f != nil || next != 10*time.Minute {
		t.Fatalf("not stuck yet: %+v %s", f, next)
	}
}

func TestJobPhase(t *testing.T) {
	tj := &kf.TrainJob{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}, Status: &kf.TrainJobStatus{}}
	if ph, _ := JobPhase(tj, nil); ph != PhasePending {
		t.Errorf("phase = %s", ph)
	}
	tj.Status.Conditions = []metav1.Condition{{Type: "Suspended", Status: "True"}}
	if ph, _ := JobPhase(tj, nil); ph != PhaseQueued {
		t.Errorf("phase = %s", ph)
	}
	tj.Annotations[AnnotationStopped] = "true"
	if ph, _ := JobPhase(tj, nil); ph != PhaseStopped {
		t.Errorf("phase = %s", ph)
	}
	tj.Annotations[naming.AnnotationFailureReason] = ReasonImagePullFailed
	tj.Annotations[naming.AnnotationFailureMessage] = "bad image"
	if ph, msg := JobPhase(tj, nil); ph != PhaseFailed || msg != "bad image" {
		t.Errorf("phase = %s %s", ph, msg)
	}
	tj.Annotations[naming.AnnotationRetriedAs] = "demo-r1"
	if ph, _ := JobPhase(tj, nil); ph != PhaseRetried {
		t.Errorf("phase = %s", ph)
	}
}

// A runtime error can be longer than a message keeps; the cause it names last
// must survive the cut.
func TestClassifyKeepsErrorEnd(t *testing.T) {
	cause := `exec: "/bin/bash": stat /bin/bash: no such file or directory`
	pod := corev1.Pod{Status: corev1.PodStatus{
		Phase: corev1.PodFailed,
		ContainerStatuses: []corev1.ContainerStatus{{
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				ExitCode: 128, Message: strings.Repeat("runc create failed: ", 20) + cause,
			}},
		}},
	}}
	f := Classify([]corev1.Pod{pod}, nil)
	if f.Reason != ReasonExitCode || !strings.HasSuffix(f.Message, cause) {
		t.Errorf("failure = %+v, want an exit code failure ending with the cause", f)
	}
}

// A crash-looping worker keeps the Running pod phase between restarts; the job
// must still read as not running and say why.
func TestJobPhaseCrashLoop(t *testing.T) {
	tj := &kf.TrainJob{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}, Status: &kf.TrainJobStatus{}}
	crash := corev1.Pod{Status: corev1.PodStatus{
		Phase: corev1.PodRunning,
		ContainerStatuses: []corev1.ContainerStatus{{
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
				Reason: "CrashLoopBackOff", Message: "back-off 40s restarting failed container",
			}},
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				Reason: "StartError", ExitCode: 128,
				Message: "failed to create containerd task: failed to create shim task: OCI runtime create failed: " +
					"runc create failed: unable to start container process: error during container init: " +
					"exec: \"/bin/bash\": stat /bin/bash: no such file or directory\n",
			}},
		}},
	}}
	ph, msg := JobPhase(tj, []corev1.Pod{crash})
	if ph != PhasePending {
		t.Fatalf("phase = %s, want Pending", ph)
	}
	// The runtime error names the cause last; the message keeps that end.
	if want := "CrashLoopBackOff: last run ended with StartError (exit code 128): …"; !strings.HasPrefix(msg, want) ||
		!strings.HasSuffix(msg, `exec: "/bin/bash": stat /bin/bash: no such file or directory`) {
		t.Errorf("message = %q, want prefix %q and the exec error", msg, want)
	}
	if Running(&crash) {
		t.Errorf("crash-looping pod reads as running")
	}

	healthy := corev1.Pod{Status: corev1.PodStatus{
		Phase:             corev1.PodRunning,
		ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
	}}
	if ph, _ := JobPhase(tj, []corev1.Pod{crash, healthy}); ph != PhaseRunning {
		t.Errorf("phase with one healthy worker = %s, want Running", ph)
	}

	runErr := corev1.Pod{Status: corev1.PodStatus{
		Phase: corev1.PodRunning,
		ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
			Reason: "RunContainerError", Message: "failed to start",
		}}}},
	}}
	if Running(&runErr) {
		t.Errorf("RunContainerError pod reads as running")
	}
}

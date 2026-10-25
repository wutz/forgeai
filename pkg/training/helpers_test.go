package training

import (
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
)

func TestFinishedAndFailed(t *testing.T) {
	cond := func(typ string) *kf.TrainJob {
		return &kf.TrainJob{Status: &kf.TrainJobStatus{Conditions: []metav1.Condition{
			{Type: typ, Status: metav1.ConditionTrue},
		}}}
	}
	if Finished(&kf.TrainJob{}) || Failed(&kf.TrainJob{}) {
		t.Error("a new job reads as finished")
	}
	if !Finished(cond(kf.ConditionComplete)) || Failed(cond(kf.ConditionComplete)) {
		t.Error("a completed job is not finished but not failed")
	}
	if !Finished(cond(kf.ConditionFailed)) || !Failed(cond(kf.ConditionFailed)) {
		t.Error("a failed condition is not both finished and failed")
	}
	// A job the operator stopped for being stuck is failed, though it has no
	// Failed condition.
	stuck := &kf.TrainJob{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		naming.AnnotationFailureReason: ReasonImagePullFailed,
		AnnotationStopped:              naming.LiteralTrue,
	}}}
	if !Failed(stuck) {
		t.Error("a job stopped with a failure reason is not failed")
	}
	stuck.Annotations[AnnotationStopped] = ""
	if Failed(stuck) {
		t.Error("a failure reason without the stop is failed")
	}
}

func TestRetryName(t *testing.T) {
	if got := RetryName("demo", 2); got != "demo-r2" {
		t.Errorf("RetryName = %q, want demo-r2", got)
	}
}

func TestSortedEnvByName(t *testing.T) {
	got := SortedEnv(map[string]string{"B": "2", "A": "1", "C": ""})
	want := []EnvVar{{Name: "A", Value: "1"}, {Name: "B", Value: "2"}, {Name: "C"}}
	if !slices.Equal(got, want) {
		t.Errorf("SortedEnv = %+v, want %+v", got, want)
	}
	if len(SortedEnv(nil)) != 0 {
		t.Error("SortedEnv of nil is not empty")
	}
}

// TrainJobForm reads back the form a TrainJob was rendered from, so editing a
// job starts from what the user submitted.
func TestTrainJobFormRoundTrips(t *testing.T) {
	cfg, _ := platform.Parse(nil)
	tj, err := TrainJob(form(), Context{Namespace: "forge-a", InstanceType: instance(), Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	got, err := TrainJobForm(tj)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" || got.Image != "pytorch/pytorch:2" || got.Command != "torchrun train.py" {
		t.Errorf("name, image, command = %q, %q, %q", got.Name, got.Image, got.Command)
	}
	if got.InstanceType != "rtx-5090.x2" || got.Workers != 2 || got.MaxRetries != 3 {
		t.Errorf("instance type %q workers %d retries %d", got.InstanceType, got.Workers, got.MaxRetries)
	}
	if !slices.Equal(got.Env, []EnvVar{{Name: "A", Value: "1"}}) {
		t.Errorf("env %+v", got.Env)
	}
	// The form has no retry policy, so the job carries the Infra default.
	if got.RetryOn != RetryInfra {
		t.Errorf("retry policy %q, want Infra", got.RetryOn)
	}
	want := []VolumeMount{{Volume: "data", MountPath: "/data"}, {Volume: "data", MountPath: "/ckpt", SubPath: "ckpt"}}
	if !slices.Equal(got.Volumes, want) {
		t.Errorf("volumes %+v, want %+v", got.Volumes, want)
	}
}

func TestTrainJobFormKeepsOnlyTheScriptOfBashCommand(t *testing.T) {
	tj := &kf.TrainJob{Spec: kf.TrainJobSpec{Trainer: &kf.Trainer{
		Command: []string{"/bin/bash", "-c", "python", "train.py"},
	}}}
	got, err := TrainJobForm(tj)
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != "python train.py" {
		t.Errorf("command %q, want the script joined", got.Command)
	}
	// Without a trainer the form still has one worker and no command.
	got, err = TrainJobForm(&kf.TrainJob{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Workers != 1 || got.Command != "" || got.Image != "" {
		t.Errorf("form without a trainer = %+v", got)
	}
}

func TestPodMessage(t *testing.T) {
	unscheduled := corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
		Type: corev1.PodScheduled, Status: corev1.ConditionFalse,
		Reason: "Unschedulable", Message: "0/3 nodes are available",
	}}}}
	if got := PodMessage(&unscheduled); got != "Unschedulable: 0/3 nodes are available" {
		t.Errorf("unscheduled message %q", got)
	}

	crash := corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
		LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			Reason: "Error", ExitCode: 1, Message: "boom",
		}},
	}}}}
	if got := PodMessage(&crash); got != "CrashLoopBackOff: last run ended with Error (exit code 1): boom" {
		t.Errorf("crash loop message %q", got)
	}

	creating := corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}},
	}}}}
	if got := PodMessage(&creating); got != "" {
		t.Errorf("a container being created reports %q", got)
	}

	waiting := corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ErrImagePull", Message: "not found"}},
	}}}}
	if got := PodMessage(&waiting); got != "ErrImagePull: not found" {
		t.Errorf("waiting message %q", got)
	}
}

func TestRunningIgnoresStuckAndDeletingPods(t *testing.T) {
	running := corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	if !Running(&running) {
		t.Error("a running pod is not running")
	}
	deleting := running.DeepCopy()
	deleting.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	if Running(deleting) {
		t.Error("a pod being deleted is running")
	}
	crashing := running.DeepCopy()
	crashing.Status.ContainerStatuses = []corev1.ContainerStatus{{
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
	}}
	if Running(crashing) {
		t.Error("a crash-looping pod is running")
	}
	pending := corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending}}
	if Running(&pending) {
		t.Error("a pending pod is running")
	}
}

func TestWorkerIndexAndPodName(t *testing.T) {
	worker := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "demo-node-abc",
		Labels: map[string]string{kf.LabelCompletion: "3"}}}
	if WorkerIndex(&worker) != 3 || podName(&worker) != "worker-3" {
		t.Errorf("index %d name %q", WorkerIndex(&worker), podName(&worker))
	}
	other := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "demo-launcher-x",
		Labels: map[string]string{kf.LabelCompletion: "not-a-number"}}}
	if WorkerIndex(&other) != -1 || podName(&other) != "demo-launcher-x" {
		t.Errorf("index %d name %q", WorkerIndex(&other), podName(&other))
	}
}

func TestByPriorityPutsPlatformFailuresFirst(t *testing.T) {
	got := byPriority([]Failure{
		{ReasonExitCode, "exit"},
		{"Something else", "unknown"},
		{ReasonOOMKilled, "oom"},
		{ReasonNodeFailure, "node"},
	})
	if got.Reason != ReasonNodeFailure {
		t.Errorf("first failure %+v, want the node failure", got)
	}
	got = byPriority([]Failure{{"Something else", "unknown"}, {ReasonExitCode, "exit"}})
	if got.Reason != ReasonExitCode {
		t.Errorf("first failure %+v, want the exit code over an unknown reason", got)
	}
}

func TestStuckCountsFromWhenScheduled(t *testing.T) {
	now := time.Now()
	// Created long ago but scheduled only five minutes ago: the wait is counted
	// from the schedule, so a 15 minute timeout has ten minutes left.
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
			Labels: map[string]string{kf.LabelCompletion: "0"}},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{{
				Type: corev1.PodScheduled, Status: corev1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(now.Add(-5 * time.Minute)),
			}},
			ContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}},
			}},
		},
	}
	f, next := Stuck([]corev1.Pod{p}, now, 15*time.Minute)
	if f != nil {
		t.Fatalf("stuck too early: %+v", f)
	}
	if next != 10*time.Minute {
		t.Errorf("next check in %s, want 10m", next)
	}
	// Ten minutes later the wait since the schedule is the full timeout.
	if f, _ := Stuck([]corev1.Pod{p}, now.Add(10*time.Minute), 15*time.Minute); f == nil || !strings.Contains(f.Message, "stuck for") {
		t.Errorf("not reported stuck once the timeout passed: %+v", f)
	}
}

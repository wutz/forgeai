package app

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/wutz/forgeai/pkg/sandbox"
)

// TestSandboxContainerState checks that the terminal opens only onto a running
// sandbox container: a pod can read Running while its container crash-loops.
func TestSandboxContainerState(t *testing.T) {
	podWith := func(name string, st corev1.ContainerState) *corev1.Pod {
		return &corev1.Pod{Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{Name: name, State: st}},
		}}
	}
	tests := []struct {
		name        string
		pod         *corev1.Pod
		wantRunning bool
		wantReason  string
	}{
		{"running", podWith(sandbox.ContainerName, corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}), true, ""},
		{"crash loop", podWith(sandbox.ContainerName, corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}), false, "CrashLoopBackOff"},
		{"exited", podWith(sandbox.ContainerName, corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{Reason: "Completed"}}), false, "Completed"},
		{"only another container runs", podWith("istio-proxy", corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}), false, ""},
		{"no status yet", &corev1.Pod{}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			running, reason := sandboxContainerState(tt.pod)
			if running != tt.wantRunning || reason != tt.wantReason {
				t.Errorf("sandboxContainerState() = %v, %q; want %v, %q", running, reason, tt.wantRunning, tt.wantReason)
			}
		})
	}
}

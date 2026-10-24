package app

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wutz/forgeai/pkg/sandbox"
)

// TestCreateSandboxRefusesWhilePodLingers checks that a create does not reuse
// the name of a deleted sandbox whose pod is still shutting down.
func TestCreateSandboxRefusesWhilePodLingers(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "box", Namespace: testNamespace}}
	a := testApp(t, pod)
	_, err := a.CreateSandbox(sandbox.Form{Name: "box", Image: "img"})
	if err == nil || !strings.Contains(err.Error(), "still shutting down") {
		t.Fatalf("CreateSandbox with a lingering pod = %v; want a shutting-down error", err)
	}
}

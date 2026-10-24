package app

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/wutz/forgeai/pkg/sandbox"
)

// TestToSandboxSkipsPodOfSuspendedSandbox checks that a sandbox the controller
// reports as suspended does not look for a pod: it has none, so the lookup would
// be a wasted request on every poll of its detail view. A suspend that is only
// requested still has its pod until the controller reports, so it keeps the lookup.
func TestToSandboxSkipsPodOfSuspendedSandbox(t *testing.T) {
	suspended := []metav1.Condition{{Type: sandbox.ConditionSuspended}}
	for _, tt := range []struct {
		name       string
		conditions []metav1.Condition
		wantLookup bool
		wantPhase  string
	}{
		{"running", nil, true, "Pending"},
		{"suspend requested, not yet reported", nil, true, "Pending"},
		{"suspended", suspended, false, "Stopped"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sb := &sandbox.Sandbox{ObjectMeta: metav1.ObjectMeta{Name: "box", Namespace: testNamespace}}
			if tt.name == "suspend requested, not yet reported" {
				sb.Spec.OperatingMode = sandbox.OperatingSuspended
			}
			u, err := sandbox.ToUnstructured(sb)
			if err != nil {
				t.Fatal(err)
			}
			// The create payload drops status; the server returns it, so the test sets it back.
			if tt.conditions != nil {
				if err := unstructured.SetNestedSlice(u.Object, []any{map[string]any{
					"type": sandbox.ConditionSuspended, "status": "True", "reason": sandbox.ReasonSuspended,
				}}, "status", "conditions"); err != nil {
					t.Fatal(err)
				}
			}
			lookups := 0
			out := toSandbox(u, func(string) (*corev1.Pod, bool) {
				lookups++
				return nil, false
			})
			if (lookups == 1) != tt.wantLookup {
				t.Errorf("pod lookups = %d, want lookup %v", lookups, tt.wantLookup)
			}
			if out.Phase != tt.wantPhase {
				t.Errorf("phase = %q, want %q", out.Phase, tt.wantPhase)
			}
		})
	}
}

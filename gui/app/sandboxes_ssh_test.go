package app

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/sandbox"
)

// TestToSandboxReportsSSHEndpoint checks that a sandbox with SSH shows the
// address of its SSH Service, and that one without shows none.
func TestToSandboxReportsSSHEndpoint(t *testing.T) {
	for _, tt := range []struct {
		name     string
		labels   map[string]string
		wantSSH  bool
		wantAddr string
	}{
		{"without SSH", nil, false, ""},
		{"with SSH", map[string]string{naming.LabelSSH: naming.LiteralTrue}, true,
			"agent-ssh.forge-demo.svc:22"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sb := &sandbox.Sandbox{}
			sb.Name, sb.Namespace, sb.Labels = "agent", "forge-demo", tt.labels
			u, err := sandbox.ToUnstructured(sb)
			if err != nil {
				t.Fatal(err)
			}
			unstructured.RemoveNestedField(u.Object, "status")
			out := toSandbox(u, func(string) (*corev1.Pod, bool) { return nil, false })
			if out.SSH != tt.wantSSH || out.SSHEndpoint != tt.wantAddr {
				t.Errorf("ssh = %v %q, want %v %q", out.SSH, out.SSHEndpoint, tt.wantSSH, tt.wantAddr)
			}
		})
	}
}

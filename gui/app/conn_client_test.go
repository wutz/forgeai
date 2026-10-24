package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestClientConfigRaisesRequestBudget checks that a connection is not held to
// client-go's default of 5 requests a second, which made the page reads queue.
func TestClientConfigRaisesRequestBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	kubeconfig := `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: ctx
  context:
    cluster: c
    user: u
current-context: ctx
users:
- name: u
  user:
    token: test
`
	if err := os.WriteFile(path, []byte(kubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", path)

	cfg, _, err := clientConfig("ctx")
	if err != nil {
		t.Fatalf("clientConfig: %v", err)
	}
	if cfg.QPS < 50 || cfg.Burst < 100 {
		t.Errorf("QPS %v, Burst %d; want at least 50 and 100", cfg.QPS, cfg.Burst)
	}
}

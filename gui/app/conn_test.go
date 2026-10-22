package app

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wutz/forgeai/pkg/platform"
)

// TestConfigExposesNamespacePrefix checks that the GUI receives the prefix the
// project controller uses, defaulted when the platform config leaves it unset.
func TestConfigExposesNamespacePrefix(t *testing.T) {
	tests := []struct {
		name, data, want string
	}{
		{"default", "registry:\n  domain: cr.example.com:7443\n", "forge-"},
		{"custom", "namespacePrefix: team-\n", "team-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: platform.ConfigMapName, Namespace: testNamespace},
				Data:       map[string]string{platform.ConfigKey: tt.data},
			}
			a := testApp(t, cm)

			cfg, err := a.Config()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.NamespacePrefix != tt.want {
				t.Fatalf("NamespacePrefix = %q, want %q", cfg.NamespacePrefix, tt.want)
			}
		})
	}
}

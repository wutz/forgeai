package app

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/sandbox"
)

// TestGatewayURL checks the address of a published sandbox: its host under the
// ingress domain, on the gateway's scheme and port, with the default port left out.
func TestGatewayURL(t *testing.T) {
	gateway := platform.Gateway{Name: "gw", Namespace: "gateway-system", Scheme: "https"}
	ingress := platform.Ingress{Domain: "dev1.example.com", Port: 443}
	published := func(port string) *sandbox.Sandbox {
		return &sandbox.Sandbox{ObjectMeta: metav1.ObjectMeta{
			Name: "box", Namespace: "forge-p",
			Annotations: map[string]string{naming.AnnotationGatewayPort: port},
		}}
	}
	tests := []struct {
		name string
		sb   *sandbox.Sandbox
		cfg  *platform.Config
		want string
	}{
		{"not published", &sandbox.Sandbox{ObjectMeta: metav1.ObjectMeta{Name: "box", Namespace: "forge-p"}},
			&platform.Config{Ingress: ingress, Gateway: gateway}, ""},
		{"default port", published("8080"), &platform.Config{Ingress: ingress, Gateway: gateway},
			"https://box-forge-p.dev1.example.com"},
		{"listener port", published("8080"), &platform.Config{Ingress: ingress,
			Gateway: platform.Gateway{Name: "gw", Namespace: "gateway-system", Scheme: "https", Port: 7443}},
			"https://box-forge-p.dev1.example.com:7443"},
		{"http on port 80", published("8080"), &platform.Config{Ingress: ingress,
			Gateway: platform.Gateway{Name: "gw", Namespace: "gateway-system", Scheme: "http", Port: 80}},
			"http://box-forge-p.dev1.example.com"},
		{"no gateway", published("8080"), &platform.Config{Ingress: ingress}, ""},
		{"no ingress domain", published("8080"), &platform.Config{Gateway: gateway}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gatewayURL(tt.sb, tt.cfg); got != tt.want {
				t.Errorf("gatewayURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

package kubeconfig

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
)

func TestFromTokenSecret(t *testing.T) {
	s := &corev1.Secret{Type: corev1.SecretTypeServiceAccountToken, Data: map[string][]byte{
		"token": []byte("tok"), "ca.crt": []byte("CA"),
	}}
	b, err := FromTokenSecret(s, Params{Name: "bz1-forge-demo-alice", Server: "https://api:6443", Namespace: "forge-demo"})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := clientcmd.Load(b)
	if err != nil {
		t.Fatal(err)
	}
	ctx := cfg.Contexts[cfg.CurrentContext]
	if ctx.Namespace != "forge-demo" || cfg.AuthInfos[ctx.AuthInfo].Token != "tok" ||
		string(cfg.Clusters[ctx.Cluster].CertificateAuthorityData) != "CA" || cfg.Clusters[ctx.Cluster].Server != "https://api:6443" {
		t.Fatalf("config %+v", cfg)
	}
	s.Data["token"] = nil
	if _, err := FromTokenSecret(s, Params{Server: "x"}); err == nil {
		t.Error("expected missing token error")
	}
}

// Package kubeconfig renders member kubeconfigs from ServiceAccount token
// Secrets.
package kubeconfig

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Params describe a member kubeconfig.
type Params struct {
	// Name is used for the cluster, user and context entries.
	Name string
	// Server is the API server URL.
	Server string
	// Namespace is the default namespace of the context (the project namespace).
	Namespace string
	// InsecureSkipTLSVerify is set when the token secret has no CA.
	InsecureSkipTLSVerify bool
}

// FromTokenSecret renders a kubeconfig from a kubernetes.io/service-account-token Secret.
func FromTokenSecret(s *corev1.Secret, p Params) ([]byte, error) {
	if s.Type != corev1.SecretTypeServiceAccountToken {
		return nil, fmt.Errorf("secret %s/%s is not a service account token", s.Namespace, s.Name)
	}
	token := s.Data[corev1.ServiceAccountTokenKey]
	if len(token) == 0 {
		return nil, fmt.Errorf("token of %s/%s is not issued yet", s.Namespace, s.Name)
	}
	if p.Server == "" {
		return nil, fmt.Errorf("platform apiServer is not configured")
	}
	cluster := &clientcmdapi.Cluster{Server: p.Server}
	if ca := s.Data[corev1.ServiceAccountRootCAKey]; len(ca) > 0 && !p.InsecureSkipTLSVerify {
		cluster.CertificateAuthorityData = ca
	} else {
		cluster.InsecureSkipTLSVerify = true
	}
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters[p.Name] = cluster
	cfg.AuthInfos[p.Name] = &clientcmdapi.AuthInfo{Token: string(token)}
	cfg.Contexts[p.Name] = &clientcmdapi.Context{Cluster: p.Name, AuthInfo: p.Name, Namespace: p.Namespace}
	cfg.CurrentContext = p.Name
	return clientcmd.Write(*cfg)
}

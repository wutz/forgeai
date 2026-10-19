package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/wutz/forgeai/api/v1alpha1"

	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
)

// loadingRules reads $KUBECONFIG or ~/.kube/config.
func loadingRules() *clientcmd.ClientConfigLoadingRules {
	return clientcmd.NewDefaultClientConfigLoadingRules()
}

// Contexts lists the contexts of the default kubeconfig.
func (a *App) Contexts() ([]KubeContext, error) {
	raw, err := loadingRules().Load()
	if err != nil {
		return nil, err
	}
	out := make([]KubeContext, 0, len(raw.Contexts))
	for name, c := range raw.Contexts {
		out = append(out, KubeContext{
			Name: name, Cluster: c.Cluster, Namespace: c.Namespace, Current: name == raw.CurrentContext,
		})
	}
	slices.SortFunc(out, func(a, b KubeContext) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// Connect switches to a kubeconfig context and a forgeai project namespace.
// The namespace is only accepted when it carries the forgeai project label,
// so the GUI cannot be pointed at unrelated parts of the cluster. It returns
// the resolved connection info, including whether the user may administer
// Projects (used to show the admin area).
func (a *App) Connect(contextName, namespace string) (ConnInfo, error) {
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: contextName})
	rest, err := cc.ClientConfig()
	if err != nil {
		return ConnInfo{}, err
	}
	if namespace == "" {
		if namespace, _, err = cc.Namespace(); err != nil {
			return ConnInfo{}, err
		}
	}
	rest.Timeout = 30 * time.Second

	k8s, err := client.New(rest, client.Options{Scheme: scheme()})
	if err != nil {
		return ConnInfo{}, err
	}
	core, err := kubernetes.NewForConfig(rest)
	if err != nil {
		return ConnInfo{}, err
	}

	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	ns := &corev1.Namespace{}

	if err := k8s.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
		return ConnInfo{}, fmt.Errorf("namespace %q: %w", namespace, err)
	}
	project := ns.Labels[naming.LabelProject]
	if project == "" || ns.Labels[naming.LabelManagedBy] != naming.ManagedBy {
		return ConnInfo{}, fmt.Errorf("namespace %q is not a forgeai project", namespace)
	}

	// Cluster-wide reads need the forgeai catalogues; probe they work.
	if err := a.isolate(ctx, k8s); err != nil {
		return ConnInfo{}, err
	}
	info := ConnInfo{
		Context: contextName, Server: rest.Host, Namespace: namespace, Project: project,
		Admin: mustBeAdmin(ctx, k8s),
	}
	conn := &Connection{Info: info, k8s: k8s, rest: rest, core: core}
	if cfg, err := platform.Load(ctx, k8s, systemNamespace()); err == nil {
		conn.insecure = cfg.Registry.Insecure
	}
	a.mu.Lock()
	a.conn = conn
	a.mu.Unlock()
	return info, nil
}

// isolate verifies the connection reaches the resources the GUI needs, so
// connection errors surface at connect time rather than as broken views.
func (a *App) isolate(ctx context.Context, k8s client.Client) error {
	list := &v1.InstanceTypeList{}
	if err := k8s.List(ctx, list); err != nil {
		return fmt.Errorf("list InstanceTypes: %w", err)
	}
	return nil
}

// mustBeAdmin reports whether the user may create Projects; the admin area
// is hidden when this is false.
func mustBeAdmin(ctx context.Context, k8s client.Client) bool {
	sar := &authv1.SelfSubjectAccessReview{Spec: authv1.SelfSubjectAccessReviewSpec{
		ResourceAttributes: &authv1.ResourceAttributes{
			Group: "forgeai.wutz.dev", Resource: "projects", Verb: "create",
		},
	}}
	if err := k8s.Create(ctx, sar); err != nil {
		return false
	}
	return sar.Status.Allowed
}

// Logout forgets the connection.
func (a *App) Logout() {
	a.mu.Lock()
	a.conn = nil
	a.mu.Unlock()
}

// PlatformConfig is the platform configuration relevant to the GUI.
type PlatformConfig struct {
	APIServer    string                     `json:"apiServer"`
	Ingress      platform.Ingress           `json:"ingress"`
	Registry     RegistryInfo               `json:"registry"`
	StorageTypes map[string]StorageTypeInfo `json:"storageTypes"`
	FileServer   FileServerInfo             `json:"fileServer"`
	Trash        metav1.Duration            `json:"trash"`
	Training     TrainingInfo               `json:"training"`
}

// RegistryInfo is the Harbor registry of the platform.
type RegistryInfo struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

// StorageTypeInfo describes one volume type offered to users.
type StorageTypeInfo struct {
	StorageClass string `json:"storageClass"`
	AccessMode   string `json:"accessMode"`
	Description  string `json:"description"`
}

// FileServerInfo configures the on-demand file browser.
type FileServerInfo struct {
	IdleTimeout metav1.Duration `json:"idleTimeout"`
}

// TrainingInfo carries job defaults from the platform config.
type TrainingInfo struct {
	DefaultRuntime string          `json:"defaultRuntime"`
	ShmSize        string          `json:"shmSize"`
	StuckTimeout   metav1.Duration `json:"stuckTimeout"`
	MaxRetries     int32           `json:"maxRetries"`
}

// Config returns the platform configuration.
func (a *App) Config() (PlatformConfig, error) {
	c, err := a.current()
	if err != nil {
		return PlatformConfig{}, err
	}
	ctx, cancel := a.call()
	defer cancel()
	cfg, err := platform.Load(ctx, c.k8s, c.namespace())
	if err != nil {
		return PlatformConfig{}, err
	}
	out := PlatformConfig{
		APIServer:    cfg.APIServer,
		Ingress:      cfg.Ingress,
		Registry:     RegistryInfo{Enabled: cfg.Registry.Enabled(), URL: cfg.Registry.Domain},
		StorageTypes: map[string]StorageTypeInfo{},
		FileServer:   FileServerInfo{IdleTimeout: cfg.FileServer.IdleTimeout},
		Trash:        cfg.TrashRetention,
		Training: TrainingInfo{
			DefaultRuntime: cfg.Training.DefaultRuntime,
			ShmSize:        cfg.Training.ShmSize.String(),
			StuckTimeout:   cfg.Training.StuckTimeout,
			MaxRetries:     cfg.Training.MaxRetries,
		},
	}
	for name, st := range cfg.StorageTypes {
		mode := string(st.AccessMode)
		if mode == "" {
			mode = "ReadWriteMany"
		}
		out.StorageTypes[name] = StorageTypeInfo{
			StorageClass: st.StorageClass, AccessMode: mode, Description: st.Description,
		}
	}
	return out, nil
}

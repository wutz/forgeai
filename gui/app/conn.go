package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/wutz/forgeai/api/v1alpha1"

	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/sandbox"
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

// clientConfig resolves a kubeconfig context to its REST config and default
// namespace.
func clientConfig(contextName string) (*rest.Config, string, error) {
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: contextName})
	cfg, err := cc.ClientConfig()
	if err != nil {
		return nil, "", err
	}
	namespace, _, err := cc.Namespace()
	if err != nil {
		return nil, "", err
	}
	cfg.Timeout = 30 * time.Second
	// client-go allows 5 requests a second by default. The pages read several objects
	// at once (the dependency probes, the list polls), so that limit made the reads
	// queue behind each other on the client; a person's desktop client gets a larger budget.
	cfg.QPS = 50
	cfg.Burst = 100
	return cfg, namespace, nil
}

// Projects lists the forgeai project namespaces visible to a kubeconfig
// context, for the project picker in the sidebar. Users that may not list
// namespaces (plain project members) get an empty list.
func (a *App) Projects(contextName string) ([]ProjectNamespace, error) {
	cfg, _, err := clientConfig(contextName)
	if err != nil {
		return nil, err
	}
	k8s, err := client.New(cfg, client.Options{Scheme: scheme()})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	out, err := listProjects(ctx, k8s)
	if apierrors.IsForbidden(err) {
		return []ProjectNamespace{}, nil
	}
	return out, err
}

// listProjects returns the forgeai project namespaces sorted by name.
func listProjects(ctx context.Context, k8s client.Client) ([]ProjectNamespace, error) {
	list := &corev1.NamespaceList{}
	if err := k8s.List(ctx, list, client.MatchingLabels{naming.LabelManagedBy: naming.ManagedBy},
		client.HasLabels{naming.LabelProject}); err != nil {
		return nil, err
	}
	out := make([]ProjectNamespace, 0, len(list.Items))
	for _, ns := range list.Items {
		out = append(out, ProjectNamespace{Namespace: ns.Name, Project: ns.Labels[naming.LabelProject]})
	}
	slices.SortFunc(out, func(a, b ProjectNamespace) int { return strings.Compare(a.Namespace, b.Namespace) })
	return out, nil
}

// projectOf returns the forgeai project of a namespace, or "" when the
// namespace is not a project namespace.
func projectOf(ctx context.Context, k8s client.Client, namespace string) (string, error) {
	ns := &corev1.Namespace{}
	if err := k8s.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
		return "", fmt.Errorf("namespace %q: %w", namespace, err)
	}
	if ns.Labels[naming.LabelManagedBy] != naming.ManagedBy {
		return "", nil
	}
	return ns.Labels[naming.LabelProject], nil
}

// resolveProject returns the project namespace and project a connection opens.
// An explicit namespace must be a project the user can read. Otherwise the
// context default is used when it is a project, else the first project the
// user can see. When the user can see no project, the namespace is "": the
// connection opens nothing, which an admin reads as the admin area and anyone
// else as a connection the App explains.
func resolveProject(ctx context.Context, k8s client.Client, namespace string, explicit bool) (string, string, error) {
	project, err := projectOf(ctx, k8s, namespace)
	if err == nil && project == "" {
		err = fmt.Errorf("namespace %q is not a forgeai project", namespace)
	}
	if err == nil || explicit {
		return namespace, project, err
	}
	projects, lerr := listProjects(ctx, k8s)
	switch {
	case lerr == nil && len(projects) > 0:
		return projects[0].Namespace, projects[0].Project, nil
	case lerr == nil || apierrors.IsForbidden(lerr):
		// Listing namespaces is itself forbidden to a user with no project role.
		return "", "", nil
	}
	return "", "", lerr
}

// Connect switches to a kubeconfig context and a forgeai project namespace.
// The namespace is only accepted when it carries the forgeai project label,
// so the GUI cannot be pointed at unrelated parts of the cluster. When no
// namespace is given and the context default (often "default") is not a
// project, the first project namespace the user can see is used instead; an
// admin of a cluster without projects connects to the admin area only. It
// returns the resolved connection info, including whether the user may
// administer Projects (used to show the admin area).
func (a *App) Connect(contextName, namespace string) (ConnInfo, error) {
	a.mu.Lock()
	a.connGen++
	gen := a.connGen
	a.mu.Unlock()

	rest, defaultNamespace, err := clientConfig(contextName)
	if err != nil {
		return ConnInfo{}, err
	}
	explicit := namespace != ""
	if !explicit {
		namespace = defaultNamespace
	}

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

	admin := mustBeAdmin(ctx, k8s)
	clusterAdmin := mustBeClusterAdmin(ctx, k8s)
	namespace, project, err := resolveProject(ctx, k8s, namespace, explicit)
	if err != nil {
		return ConnInfo{}, err
	}

	// Cluster-wide reads need the forgeai catalogues; probe they work. Someone
	// with nothing open does not read them, and may not be allowed to.
	forgeai := false
	if namespace != "" || admin {
		if forgeai, err = isolate(ctx, k8s, clusterAdmin); err != nil {
			return ConnInfo{}, err
		}
	}
	info := ConnInfo{
		Context: contextName, Server: rest.Host, Namespace: namespace, Project: project,
		Admin: admin, ClusterAdmin: clusterAdmin, Forgeai: forgeai,
		Sandboxes: namespace != "" && sandboxesAvailable(ctx, k8s, namespace),
	}
	conn := &Connection{Info: info, k8s: k8s, rest: rest, core: core}
	if cfg, err := platform.Load(ctx, k8s, conn.configNamespace()); err == nil {
		conn.insecure = cfg.Registry.Insecure
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.connGen {
		return ConnInfo{}, fmt.Errorf("connection to %s was superseded by a newer connect", contextName)
	}
	a.closeConnectionState()
	a.conn = conn
	return info, nil
}

// isolate verifies the connection reaches the resources the GUI needs, so
// connection errors surface at connect time rather than as broken views. A
// cluster admin may connect to a cluster without the forgeai CRDs, to install
// them from the Dependencies view; it reports whether they are installed.
func isolate(ctx context.Context, k8s client.Client, clusterAdmin bool) (bool, error) {
	list := &v1.InstanceTypeList{}
	if err := k8s.List(ctx, list); err != nil {
		if clusterAdmin && meta.IsNoMatchError(err) {
			return false, nil
		}
		return false, fmt.Errorf("list InstanceTypes: %w", err)
	}
	return true, nil
}

// sandboxesAvailable reports whether agent-sandbox is installed and the user
// may use it in the project namespace; the GUI hides the Sandboxes view when
// it is not.
func sandboxesAvailable(ctx context.Context, k8s client.Client, namespace string) bool {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(sandbox.SandboxGVK.GroupVersion().WithKind("SandboxList"))
	if err := k8s.List(ctx, list, client.InNamespace(namespace), client.Limit(1)); err != nil {
		return false
	}
	return true
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

// mustBeClusterAdmin reports whether the user may install cluster components:
// it asks for every verb on every resource, which only cluster-admin grants.
func mustBeClusterAdmin(ctx context.Context, k8s client.Client) bool {
	sar := &authv1.SelfSubjectAccessReview{Spec: authv1.SelfSubjectAccessReviewSpec{
		ResourceAttributes: &authv1.ResourceAttributes{
			Group: "*", Resource: "*", Verb: "*",
		},
	}}
	if err := k8s.Create(ctx, sar); err != nil {
		return false
	}
	return sar.Status.Allowed
}

// closeConnectionState ends the sandbox shells and the file server target of
// the connection being replaced: they belong to its cluster, not the next one.
// The caller holds a.mu.
func (a *App) closeConnectionState() {
	a.terminals.Range(func(id, _ any) bool {
		a.CloseSandboxTerminal(id.(string))
		return true
	})
	a.files.SetTarget(nil)
}

// PlatformConfig is the platform configuration relevant to the GUI.
type PlatformConfig struct {
	APIServer string `json:"apiServer"`
	// NamespacePrefix is prepended to a project name to form its namespace.
	NamespacePrefix string                     `json:"namespacePrefix"`
	Ingress         platform.Ingress           `json:"ingress"`
	Registry        RegistryInfo               `json:"registry"`
	StorageTypes    map[string]StorageTypeInfo `json:"storageTypes"`
	// DefaultStorageType is the type a volume gets when none is named.
	DefaultStorageType string          `json:"defaultStorageType"`
	FileServer         FileServerInfo  `json:"fileServer"`
	Trash              metav1.Duration `json:"trash"`
	Training           TrainingInfo    `json:"training"`
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
	cfg, err := platform.Load(ctx, c.k8s, c.configNamespace())
	if err != nil {
		return PlatformConfig{}, err
	}
	out := PlatformConfig{
		APIServer:          cfg.APIServer,
		NamespacePrefix:    cfg.NamespacePrefix,
		Ingress:            cfg.Ingress,
		Registry:           RegistryInfo{Enabled: cfg.Registry.Enabled(), URL: cfg.Registry.Domain},
		StorageTypes:       map[string]StorageTypeInfo{},
		DefaultStorageType: cfg.DefaultStorageType,
		FileServer:         FileServerInfo{IdleTimeout: cfg.FileServer.IdleTimeout},
		Trash:              cfg.TrashRetention,
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

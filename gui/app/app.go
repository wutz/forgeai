// Package app implements the Wails backend of the forgeai desktop client.
// It is split by area: connection (conn.go), jobs and templates (jobs.go,
// templates.go), volumes and the file server (volumes.go, files.go),
// registry images (images.go), instance types and quota (cluster.go), the
// public template source (catalog.go) and project administration (admin.go).
// Every call goes straight to the Kubernetes API of the connected kubeconfig
// context; the forgeai operator does the rest.
package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/wutz/forgeai/pkg/platform"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	fsv1 "github.com/wutz/fileserver/api/v1alpha1"
	v1 "github.com/wutz/forgeai/api/v1alpha1"
	projectv1 "github.com/wutz/workspace/api/v1alpha1"
	imagebuildv1 "github.com/wutz/imagebuild/api/v1alpha1"
)

// ErrNotConnected is returned when no kubeconfig context is selected.
var ErrNotConnected = errors.New("not connected: select a kubeconfig context first")

// App is the Wails-bound application. Every bound method operates on the
// current connection and is safe to call from the UI goroutine.
type App struct {
	ctx context.Context

	mu   sync.RWMutex
	conn *Connection
	// connGen counts the Connect calls. A Connect that finishes
	// after a newer call started must not install its connection.
	connGen uint64
	// terminals are the open sandbox shells, by session id.
	terminals sync.Map
	// files is the loopback proxy the file browser is embedded through.
	files fileProxy
	// filesMu serializes Files, so a slow read cannot publish its target after a newer one.
	filesMu sync.Mutex
	// installs tracks the cluster component installs in progress.
	installs depInstalls
	// emit replaces the Wails runtime as the event sink when set; see emitEvent.
	emit func(ctx context.Context, name string, data ...interface{})
}

// New creates the bound application.
func New() *App {
	// controller-runtime prints a stack trace and drops its logs until a logger
	// is set, so set one before the first Kubernetes client is created.
	logf.SetLogger(logr.FromSlogHandler(slog.Default().Handler()))
	return &App{}
}

// Startup stores the Wails context.
func (a *App) Startup(ctx context.Context) { a.ctx = ctx }

// Connection is the active Kubernetes connection and its per-project
// clients. The GUI only ever views one project namespace at a time.
type Connection struct {
	Info ConnInfo
	k8s  client.Client
	rest *rest.Config
	core kubernetes.Interface
	// insecure tracks whether the registry connection skips TLS verify.
	insecure bool
}

// KubeContext is a kubeconfig context.
type KubeContext struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Current   bool   `json:"current"`
}

// ProjectNamespace is a forgeai project namespace offered in the sidebar
// project picker.
type ProjectNamespace struct {
	Namespace string `json:"namespace"`
	Project   string `json:"project"`
}

// ConnInfo describes the active connection. Namespace and Project are empty
// for an admin-only connection to a cluster without projects.
type ConnInfo struct {
	Context   string `json:"context"`
	Server    string `json:"server"`
	Namespace string `json:"namespace"`
	Project   string `json:"project"`
	// Admin is set when the user may manage Projects.
	Admin bool `json:"admin"`
	// ClusterAdmin is set when the user may install cluster components.
	ClusterAdmin bool `json:"clusterAdmin"`
	// Forgeai is set when the forgeai CRDs are installed; without them only
	// the Dependencies view is useful.
	Forgeai bool `json:"forgeai"`
	// Sandboxes is set when the cluster runs agent-sandbox and the user may
	// use it; the Sandboxes view is hidden otherwise.
	Sandboxes bool `json:"sandboxes"`
	// Inference is set when the InferenceService CRD is installed; the
	// Inference view is hidden otherwise.
	Inference bool `json:"inference"`
	// ImageBuilds is set when the ImageBuild CRD is installed; the
	// ImageBuilds view is hidden otherwise.
	ImageBuilds bool `json:"imageBuilds"`
	// Create says which kinds the user may create in the project namespace.
	Create CreateAccess `json:"create"`
	// Change says which kinds the user may change and remove in the project namespace.
	Change ChangeAccess `json:"change"`
}

// CreateAccess says which kinds the user may create in the project namespace.
// The GUI shows a create button only when its kind is set, so a member is not
// offered an action the cluster would refuse.
type CreateAccess struct {
	Jobs             bool `json:"jobs"`
	JobTemplates     bool `json:"jobTemplates"`
	Volumes          bool `json:"volumes"`
	Inference        bool `json:"inference"`
	Sandboxes        bool `json:"sandboxes"`
	SandboxTemplates bool `json:"sandboxTemplates"`
	// SandboxTerminal says whether the user may open a shell in a sandbox: that
	// is a create on the pods/exec subresource, not a kind of its own.
	SandboxTerminal bool `json:"sandboxTerminal"`
}

// ChangeAccess says which kinds the user may change and remove in the project
// namespace. A row action (edit, grow, trash, delete) is shown only when its
// kind is set, for the same reason as CreateAccess.
type ChangeAccess struct {
	Jobs             bool `json:"jobs"`
	JobTemplates     bool `json:"jobTemplates"`
	Volumes          bool `json:"volumes"`
	Inference        bool `json:"inference"`
	Sandboxes        bool `json:"sandboxes"`
	SandboxTemplates bool `json:"sandboxTemplates"`
}

func scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v1.AddToScheme(s)
	_ = projectv1.AddToScheme(s)
	_ = fsv1.AddToScheme(s)
	_ = imagebuildv1.AddToScheme(s)
	return s
}

// current returns the active connection.
func (a *App) current() (*Connection, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.conn == nil {
		return nil, ErrNotConnected
	}
	return a.conn, nil
}

func (a *App) call() (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.ctx, 60*time.Second)
}

func (c *Connection) namespace() string { return c.Info.Namespace }

// configNamespace is where the connection reads the platform config: the
// operator's copy in the project namespace (members cannot read the platform
// namespace), or the platform namespace for an admin-only connection.
func (c *Connection) configNamespace() string {
	if c.Info.Namespace == "" {
		return platform.Namespace()
	}
	return c.Info.Namespace
}

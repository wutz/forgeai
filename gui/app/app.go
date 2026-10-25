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
	"sync"
	"time"

	"github.com/wutz/forgeai/pkg/platform"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
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
func New() *App { return &App{} }

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
}

func scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v1.AddToScheme(s)
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

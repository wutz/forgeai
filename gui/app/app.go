// Package app implements the Wails backend of the forgeai desktop client.
// It is split by area: connection (conn.go), jobs and templates (jobs.go,
// templates.go), volumes and the file server (volumes.go, files.go),
// registry images (images.go), instance types and quota (cluster.go) and
// project administration (admin.go). Every call goes straight to the
// Kubernetes API of the connected kubeconfig context; the forgeai operator
// does the rest.
package app

import (
	"context"
	"errors"
	"sync"
	"time"

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

// ConnInfo describes the active connection.
type ConnInfo struct {
	Context   string `json:"context"`
	Server    string `json:"server"`
	Namespace string `json:"namespace"`
	Project   string `json:"project"`
	Admin     bool   `json:"admin"`
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

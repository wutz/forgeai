package app

import (
	"context"
	"net/url"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/naming"
)

// Files is the state of the on-demand file browser of the project.
type Files struct {
	Phase string `json:"phase"` // Idle, Starting, Ready
	// Address is the loopback URL of the file browser. It is empty until the
	// server is ready and the user may read its password.
	Address string   `json:"address,omitempty"`
	Volumes []string `json:"volumes"`
}

// Files reports the file server: it wakes or keeps it alive (heartbeat) and
// points the loopback proxy at it. The basic-auth credentials from the file
// server Secret stay in this process; the webview only gets the proxy
// address. The server scales to zero when nobody calls this for a while.
func (a *App) Files() (*Files, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()

	// Wake the server (or keep it awake): the operator treats a fresh
	// last-active annotation as activity and scales to one replica.
	key := client.ObjectKey{Namespace: c.namespace(), Name: naming.FileServerName}
	fs := &v1.FileServer{}
	err = c.k8s.Get(ctx, key, fs)
	if err != nil {
		if !isNotFound(err) {
			return nil, err
		}
		fs = &v1.FileServer{
			ObjectMeta: metav1.ObjectMeta{Name: naming.FileServerName, Namespace: c.namespace(),
				Labels: map[string]string{naming.LabelManagedBy: naming.ManagedBy}},
		}
		if err := c.k8s.Create(ctx, fs); err != nil && !isAlreadyExists(err) {
			return nil, err
		}
	}
	if err := a.heartbeat(ctx, c); err != nil {
		return nil, err
	}
	out := &Files{Phase: "Starting"}
	fs = &v1.FileServer{}
	if err := c.k8s.Get(ctx, key, fs); err == nil {
		out.Phase = string(fs.Status.Phase)
		out.Volumes = fs.Status.Volumes
	}
	// View-only members cannot read the Secret: they get no address.
	var target *fileTarget
	sec := &corev1.Secret{}
	secKey := client.ObjectKey{Namespace: c.namespace(), Name: naming.FileServerSecretName}
	if out.Phase == string(v1.FileServerReady) && fs.Status.URL != "" && c.k8s.Get(ctx, secKey, sec) == nil {
		base, err := url.Parse(fs.Status.URL)
		if err != nil {
			return nil, err
		}
		target = &fileTarget{
			base: base,
			user: string(sec.Data[corev1.BasicAuthUsernameKey]),
			pass: string(sec.Data[corev1.BasicAuthPasswordKey]),
		}
	}
	a.files.SetTarget(target)
	if target != nil {
		origin, err := a.files.Origin()
		if err != nil {
			return nil, err
		}
		out.Address = origin + "/"
	}
	return out, nil
}

// FileServerPhase reports the phase of the file server (Idle, Starting or
// Ready) without waking it or refreshing its heartbeat. It is empty when the
// project has no file server yet.
func (a *App) FileServerPhase() (string, error) {
	c, err := a.current()
	if err != nil {
		return "", err
	}
	ctx, cancel := a.call()
	defer cancel()
	fs := &v1.FileServer{}
	if err := c.k8s.Get(ctx, client.ObjectKey{Namespace: c.namespace(), Name: naming.FileServerName}, fs); err != nil {
		if isNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return string(fs.Status.Phase), nil
}

// Heartbeat touches the last-active annotation without returning anything.
// Writes are throttled to heartbeatEvery, so calling it often is cheap.
func (a *App) Heartbeat() error {
	c, err := a.current()
	if err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()
	return a.heartbeat(ctx, c)
}

// heartbeatEvery bounds how often a view's heartbeat writes the FileServer.
// Files is polled every few seconds; each write would reconcile the object.
const heartbeatEvery = 30 * time.Second

func (a *App) heartbeat(ctx context.Context, c *Connection) error {
	fs := &v1.FileServer{}
	key := client.ObjectKey{Namespace: c.namespace(), Name: naming.FileServerName}
	if err := c.k8s.Get(ctx, key, fs); err != nil {
		return err
	}
	now := time.Now().UTC()
	if last, err := time.Parse(time.RFC3339, fs.Annotations[naming.AnnotationLastActive]); err == nil && now.Sub(last) < heartbeatEvery {
		return nil
	}
	// The patch base must be copied before the annotation changes; otherwise
	// the merge patch is empty and the operator never sees the activity.
	patch := client.MergeFrom(fs.DeepCopy())
	if fs.Annotations == nil {
		fs.Annotations = map[string]string{}
	}
	fs.Annotations[naming.AnnotationLastActive] = now.Format(time.RFC3339)
	return c.k8s.Patch(ctx, fs, patch)
}

func isNotFound(err error) bool { return apierrors.IsNotFound(err) }

func isAlreadyExists(err error) bool { return apierrors.IsAlreadyExists(err) }

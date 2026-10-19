package app

import (
	"context"
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
	Phase    string   `json:"phase"` // Idle, Starting, Ready
	URL      string   `json:"url,omitempty"`
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"`
	Volumes  []string `json:"volumes"`
	Secret   string   `json:"secret"`
}

// Files reports the file server: it wakes or keeps it alive (heartbeat) and
// returns the URL with the basic-auth credentials from the file server
// Secret. The server scales to zero when nobody calls this for a while.
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
	// Read the credentials for the webview; view-only members cannot read
	// the Secret and get the URL without a password.
	out := &Files{Phase: "Starting", Secret: naming.FileServerSecretName}
	sec := &corev1.Secret{}
	secKey := client.ObjectKey{Namespace: c.namespace(), Name: naming.FileServerSecretName}
	if err := c.k8s.Get(ctx, secKey, sec); err == nil {
		out.Username = string(sec.Data[corev1.BasicAuthUsernameKey])
		out.Password = string(sec.Data[corev1.BasicAuthPasswordKey])
	}
	fs = &v1.FileServer{}
	if err := c.k8s.Get(ctx, key, fs); err == nil {
		out.Phase = string(fs.Status.Phase)
		out.URL = fs.Status.URL
		out.Volumes = fs.Status.Volumes
	}
	return out, nil
}

// Heartbeat touches the last-active annotation without returning anything;
// the open-online-files view calls it once a minute while the user browses.
func (a *App) Heartbeat() error {
	c, err := a.current()
	if err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()
	return a.heartbeat(ctx, c)
}

func (a *App) heartbeat(ctx context.Context, c *Connection) error {
	fs := &v1.FileServer{}
	key := client.ObjectKey{Namespace: c.namespace(), Name: naming.FileServerName}
	if err := c.k8s.Get(ctx, key, fs); err != nil {
		return err
	}
	ann := fs.Annotations
	if ann == nil {
		ann = map[string]string{}
	}
	ann[naming.AnnotationLastActive] = time.Now().UTC().Format(time.RFC3339)
	patch := client.MergeFrom(fs.DeepCopy())
	metaAnnotation(fs, naming.AnnotationLastActive, ann[naming.AnnotationLastActive])
	return c.k8s.Patch(ctx, fs, patch)
}

func metaAnnotation(fs *v1.FileServer, key, value string) {
	if fs.Annotations == nil {
		fs.Annotations = map[string]string{}
	}
	fs.Annotations[key] = value
}

func isNotFound(err error) bool { return apierrors.IsNotFound(err) }

func isAlreadyExists(err error) bool { return apierrors.IsAlreadyExists(err) }

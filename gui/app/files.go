package app

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/url"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fsv1 "github.com/wutz/fileserver-operator/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/naming"
)

// Files is the state of the on-demand file browser of the project.
type Files struct {
	Phase string `json:"phase"` // Idle, Starting, Ready
	// Address is the loopback URL of the file browser. It is empty until the
	// server is ready and the user may read its password.
	Address string   `json:"address,omitempty"`
	Volumes []string `json:"volumes"`
	// ReadOnly is set when the caller cannot wake the server or read its
	// credentials: a view member sees the state and no files.
	ReadOnly bool `json:"readOnly"`
}

// errConnectionChanged is returned when the connection moved on while Files read the server.
var errConnectionChanged = errors.New("the connection changed while the file server was read; try again")

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

	a.filesMu.Lock()
	defer a.filesMu.Unlock()

	// Wake the server (or keep it awake): the operator treats a fresh
	// last-active annotation as activity and scales to one replica.
	key := client.ObjectKey{Namespace: c.namespace(), Name: fsv1.DefaultName}
	fs := &fsv1.FileServer{}
	err = c.k8s.Get(ctx, key, fs)
	if err != nil {
		if !isNotFound(err) {
			return nil, err
		}
		fs = &fsv1.FileServer{
			ObjectMeta: metav1.ObjectMeta{Name: fsv1.DefaultName, Namespace: c.namespace(),
				Labels: map[string]string{naming.LabelManagedBy: naming.ManagedBy}},
		}
		if err := c.k8s.Create(ctx, fs); err != nil && !isAlreadyExists(err) {
			// A view member cannot create the server, so there is none to show yet.
			if apierrors.IsForbidden(err) {
				return &Files{Phase: string(fsv1.FileServerIdle), ReadOnly: true}, nil
			}
			return nil, err
		}
	}
	// A view member cannot patch the heartbeat: it sees the server without waking it.
	readOnly := false
	if err := a.heartbeat(ctx, c); err != nil {
		if !apierrors.IsForbidden(err) {
			return nil, err
		}
		readOnly = true
	}
	out := &Files{Phase: "Starting"}
	fs = &fsv1.FileServer{}
	if err := c.k8s.Get(ctx, key, fs); err == nil {
		// A server the operator has not reconciled yet has no phase; it is still starting.
		if p := string(fs.Status.Phase); p != "" {
			out.Phase = p
		}
		out.Volumes = fs.Status.Volumes
	}
	// View-only members cannot read the Secret: they get no address.
	var target *fileTarget
	sec := &corev1.Secret{}
	secKey := client.ObjectKey{Namespace: c.namespace(), Name: fsv1.SecretName}
	if out.Phase == string(fsv1.FileServerReady) && fs.Status.URL != "" {
		switch err := c.k8s.Get(ctx, secKey, sec); {
		case apierrors.IsForbidden(err):
			readOnly = true
		case err == nil:
			base, err := url.Parse(fs.Status.URL)
			if err != nil {
				return nil, err
			}
			// The pod can be Ready before the ingress routes to it; until then the
			// browser gets 503 and the frame would never recover. Report Starting
			// until the server answers through the ingress.
			if !fileServerServing(ctx, base, c.insecure) {
				out.Phase = string(fsv1.FileServerStarting)
			} else {
				target = &fileTarget{
					base:     base,
					user:     string(sec.Data[corev1.BasicAuthUsernameKey]),
					pass:     string(sec.Data[corev1.BasicAuthPasswordKey]),
					insecure: c.insecure,
				}
			}
		}
	}
	out.ReadOnly = readOnly
	// The target is published only while this connection is still the current one:
	// a Connect or a disconnect during the reads has already reset the proxy.
	a.mu.RLock()
	current := a.conn == c
	if current {
		a.files.SetTarget(target)
	}
	a.mu.RUnlock()
	if !current {
		return nil, errConnectionChanged
	}
	if target != nil {
		origin, err := a.files.Origin()
		if err != nil {
			return nil, err
		}
		out.Address = origin + "/"
	}
	return out, nil
}

// fileServerTransport is the transport to the file server. The ingress serves
// the same private CA as the registry, so insecure skips verification the way
// the registry setting does.
func fileServerTransport(insecure bool) *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // private CA, opt-in
	}
	return tr
}

var (
	fileTransport         = fileServerTransport(false)
	fileTransportInsecure = fileServerTransport(true)
)

// fileServerServing reports whether the file server answers at base. dufs
// serves its health check before authentication, so no credentials are needed.
// Any failure, including a 503 from the ingress, counts as not serving.
func fileServerServing(ctx context.Context, base *url.URL, insecure bool) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.JoinPath("__dufs__", "health").String(), nil)
	if err != nil {
		return false
	}
	tr := fileTransport
	if insecure {
		tr = fileTransportInsecure
	}
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
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
	fs := &fsv1.FileServer{}
	if err := c.k8s.Get(ctx, client.ObjectKey{Namespace: c.namespace(), Name: fsv1.DefaultName}, fs); err != nil {
		if isNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return string(fs.Status.Phase), nil
}

// heartbeatEvery bounds how often the Files poll's heartbeat writes the FileServer.
// Files is polled every few seconds; each write would reconcile the object.
const heartbeatEvery = 30 * time.Second

// heartbeat touches the last-active annotation. Files calls it on every poll,
// so the view keeps the server awake without a separate binding.

func (a *App) heartbeat(ctx context.Context, c *Connection) error {
	fs := &fsv1.FileServer{}
	key := client.ObjectKey{Namespace: c.namespace(), Name: fsv1.DefaultName}
	if err := c.k8s.Get(ctx, key, fs); err != nil {
		return err
	}
	now := time.Now().UTC()
	if last, err := time.Parse(time.RFC3339, fs.Annotations[fsv1.AnnotationLastActive]); err == nil && now.Sub(last) < heartbeatEvery {
		return nil
	}
	// The patch base must be copied before the annotation changes; otherwise
	// the merge patch is empty and the operator never sees the activity.
	patch := client.MergeFrom(fs.DeepCopy())
	if fs.Annotations == nil {
		fs.Annotations = map[string]string{}
	}
	fs.Annotations[fsv1.AnnotationLastActive] = now.Format(time.RFC3339)
	return c.k8s.Patch(ctx, fs, patch)
}

func isNotFound(err error) bool { return apierrors.IsNotFound(err) }

func isAlreadyExists(err error) bool { return apierrors.IsAlreadyExists(err) }

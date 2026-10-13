// Package images lists user images in Harbor and pre-pulls images onto
// training nodes.
package images

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
)

// Image is one Harbor artifact (scc-k8s-v2 image list item).
type Image struct {
	Registry     string   `json:"registry"`
	Project      string   `json:"project"`
	Repository   string   `json:"repository"`
	Tags         []string `json:"tags"`
	Digest       string   `json:"digest"`
	Created      string   `json:"created"`
	Size         int64    `json:"size"`
	PullImageURL string   `json:"pull_image_url"`
}

// Registry lists a user's Harbor project p-{userHash}.
type Registry struct {
	Domain   string
	Username string
	Password string
	HTTP     *http.Client
}

// NewRegistry builds a Registry from the platform config and credentials.
func NewRegistry(cfg platform.Registry, username, password string) *Registry {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // Harbor with a private CA, as in scc-k8s-v2.
	}
	return &Registry{
		Domain: cfg.Domain, Username: username, Password: password,
		HTTP: &http.Client{Transport: tr, Timeout: 30 * time.Second},
	}
}

func (r *Registry) get(ctx context.Context, path string, query url.Values, out any) error {
	u := "https://" + r.Domain + "/api/v2.0" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(r.Username, r.Password)
	req.Header.Set("Accept", "application/json")
	resp, err := r.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("harbor %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// getAll pages through a Harbor list endpoint.
func getAll[T any](ctx context.Context, r *Registry, path string) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		var items []T
		q := url.Values{"page": {fmt.Sprint(page)}, "page_size": {"100"}}
		if err := r.get(ctx, path, q, &items); err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(items) < 100 {
			return all, nil
		}
	}
}

// List returns all artifacts in the user's project.
func (r *Registry) List(ctx context.Context, namespace, userID string) ([]Image, error) {
	if r.Domain == "" {
		return nil, fmt.Errorf("registry is not configured")
	}
	project := naming.UserHash(namespace, userID)
	type repo struct {
		Name string `json:"name"`
	}
	type artifact struct {
		Digest   string `json:"digest"`
		PushTime string `json:"push_time"`
		Size     int64  `json:"size"`
		Tags     []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}
	repos, err := getAll[repo](ctx, r, "/projects/"+url.PathEscape(project)+"/repositories")
	if err != nil {
		return nil, err
	}
	out := []Image{}
	for _, rp := range repos {
		// Repository names are "{project}/{repo...}"; Harbor wants "/" in the
		// repo part double-escaped.
		name := strings.TrimPrefix(rp.Name, project+"/")
		escaped := url.PathEscape(strings.ReplaceAll(name, "/", "%2F"))
		arts, err := getAll[artifact](ctx, r, "/projects/"+url.PathEscape(project)+"/repositories/"+escaped+"/artifacts")
		if err != nil {
			return nil, err
		}
		for _, a := range arts {
			tags := []string{}
			for _, t := range a.Tags {
				tags = append(tags, t.Name)
			}
			out = append(out, Image{
				Registry: r.Domain, Project: project, Repository: name, Tags: tags,
				Digest: a.Digest, Created: a.PushTime, Size: a.Size,
				PullImageURL: fmt.Sprintf("%s/%s/%s@%s", r.Domain, project, name, a.Digest),
			})
		}
	}
	return out, nil
}

// Warmer pre-pulls images with one short-lived DaemonSet per image. The
// operator deletes it once every node pulled the image or it expires.
type Warmer struct {
	K8s       client.Client
	Namespace string
}

// DaemonSet renders the warmup DaemonSet of an image. Its only container
// sleeps without privileges, a service account token or a network endpoint.
func DaemonSet(namespace, image string, cfg *platform.Config, expires time.Time) *appsv1.DaemonSet {
	labels := map[string]string{
		naming.LabelAppName:     naming.AppNameImageWarmup,
		naming.LabelImageWarmup: naming.ImageWarmupID(image),
		naming.LabelComponent:   naming.ComponentImageWarmup,
		naming.LabelManagedBy:   naming.ManagedBy,
	}
	res := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1m"),
		corev1.ResourceMemory: resource.MustParse("16Mi"),
	}
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: naming.ImageWarmupName(image), Namespace: namespace, Labels: labels,
			Annotations: map[string]string{
				naming.AnnotationImage:   image,
				naming.AnnotationExpires: expires.UTC().Format(time.RFC3339),
			},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{naming.LabelImageWarmup: naming.ImageWarmupID(image)}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					NodeSelector:                  cfg.ImageWarmup.NodeSelector,
					ServiceAccountName:            cfg.ServiceAccount,
					AutomountServiceAccountToken:  ptr.To(false),
					EnableServiceLinks:            ptr.To(false),
					TerminationGracePeriodSeconds: ptr.To[int64](0),
					Tolerations:                   []corev1.Toleration{{Key: "nvidia.com/gpu", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule}},
					Containers: []corev1.Container{{
						Name:    "pull",
						Image:   image,
						Command: []string{"sleep", "infinity"},
						// Images are pulled once; the kubelet's IfNotPresent keeps them cached.
						ImagePullPolicy: corev1.PullIfNotPresent,
						Resources:       corev1.ResourceRequirements{Requests: res, Limits: res},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr.To(false),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
					}},
				},
			},
		},
	}
}

// Warmup starts (or extends) a pre-pull of each image on the training nodes.
func (w *Warmer) Warmup(ctx context.Context, cfg *platform.Config, imgs []string) error {
	ttl, err := time.ParseDuration(cfg.ImageWarmup.TTL)
	if err != nil {
		return fmt.Errorf("imageWarmup.ttl: %w", err)
	}
	expires := time.Now().Add(ttl)
	for _, img := range imgs {
		img = strings.TrimSpace(img)
		if img == "" {
			continue
		}
		ds := DaemonSet(w.Namespace, img, cfg, expires)
		err := w.K8s.Create(ctx, ds)
		if apierrors.IsAlreadyExists(err) {
			cur := &appsv1.DaemonSet{}
			if err := w.K8s.Get(ctx, types.NamespacedName{Namespace: w.Namespace, Name: ds.Name}, cur); err != nil {
				return err
			}
			patch := client.MergeFrom(cur.DeepCopy())
			cur.Annotations[naming.AnnotationExpires] = ds.Annotations[naming.AnnotationExpires]
			err = w.K8s.Patch(ctx, cur, patch)
		}
		if err != nil {
			return fmt.Errorf("warmup %s: %w", img, err)
		}
	}
	return nil
}

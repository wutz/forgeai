package images

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
)

const testImage = "img:1"

func TestList(t *testing.T) {
	project := naming.UserHash("pod-prod", "u1")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "user" || p != "pass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("page") != "1" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		switch r.URL.EscapedPath() {
		case "/api/v2.0/projects/" + project + "/repositories":
			_ = json.NewEncoder(w).Encode([]map[string]string{{"name": project + "/team/train"}})
		case "/api/v2.0/projects/" + project + "/repositories/team%252Ftrain/artifacts":
			_, _ = w.Write([]byte(`[{"digest":"sha256:abc","push_time":"2026-01-01T00:00:00Z",` +
				`"size":42,"tags":[{"name":"v1"}]}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	domain := strings.TrimPrefix(srv.URL, "https://")
	r := NewRegistry(platform.Registry{Domain: domain, Insecure: true}, "user", "pass")
	imgs, err := r.List(context.Background(), "pod-prod", "u1")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%s/%s/team/train@sha256:abc", domain, project)
	if len(imgs) != 1 || imgs[0].Repository != "team/train" || imgs[0].Tags[0] != "v1" || imgs[0].PullImageURL != want {
		t.Fatalf("imgs=%+v", imgs)
	}

	r.Password = "wrong"
	if _, err := r.List(context.Background(), "pod-prod", "u1"); err == nil {
		t.Fatal("bad credentials accepted")
	}
}

func TestWarmup(t *testing.T) {
	ctx := context.Background()
	cfg, err := platform.Parse([]byte(`
releaseVersion: test
serviceAccount: pod-default
imageWarmup: {nodeSelector: {scclabs.dev/gpu: rtx-5090}, ttl: 1h}
`))
	if err != nil {
		t.Fatal(err)
	}
	w := &Warmer{K8s: fake.NewClientBuilder().Build(), Namespace: "pod-prod"}
	if err := w.Warmup(ctx, cfg, []string{testImage, " ", testImage}); err != nil {
		t.Fatal(err)
	}
	ds := &appsv1.DaemonSet{}
	key := types.NamespacedName{Namespace: "pod-prod", Name: naming.ImageWarmupName(testImage)}
	if err := w.K8s.Get(ctx, key, ds); err != nil {
		t.Fatal(err)
	}
	spec := ds.Spec.Template.Spec
	c := spec.Containers[0]
	if c.Image != testImage || spec.NodeSelector["scclabs.dev/gpu"] != "rtx-5090" || *spec.AutomountServiceAccountToken ||
		*c.SecurityContext.AllowPrivilegeEscalation || len(c.Ports) != 0 {
		t.Fatalf("spec=%+v", spec)
	}
	if ds.Labels[naming.LabelComponent] != naming.ComponentImageWarmup {
		t.Fatalf("labels=%v", ds.Labels)
	}
	exp, err := time.Parse(time.RFC3339, ds.Annotations[naming.AnnotationExpires])
	if err != nil || time.Until(exp) < 59*time.Minute {
		t.Fatalf("expires=%v err=%v", exp, err)
	}
}

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wutz/forgeai/pkg/harbor"
)

// TestRepoImagesKeepsOtherRepositoriesWhenOneFails checks a repository whose
// artifacts cannot be listed reports why and keeps its counts, while the
// repository next to it lists normally.
func TestRepoImagesKeepsOtherRepositoriesWhenOneFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "broken") {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	hc := harbor.New(srv.URL, "u", "p", false)
	ctx := context.Background()

	good := repoImages(ctx, hc, "forge", harbor.Repository{Name: "forge/good", ArtifactCount: 2})
	if good.Error != "" {
		t.Fatalf("healthy repository reported error %q", good.Error)
	}
	bad := repoImages(ctx, hc, "forge", harbor.Repository{Name: "forge/broken", ArtifactCount: 3, PullCount: 7})
	if bad.Error == "" {
		t.Fatal("failing repository reported no error")
	}
	if bad.Name != "forge/broken" || bad.Count != 3 || bad.Pulls != 7 {
		t.Fatalf("failing repository lost its counts: %+v", bad)
	}
}

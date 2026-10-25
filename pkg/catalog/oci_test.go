package catalog

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
)

// newTestRegistry starts an in-memory OCI registry and returns its host:port.
// The Docker configuration is emptied, so no credential helper of the machine
// runs during the test.
func newTestRegistry(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

// loadOCI loads the published source at location over plain HTTP, as the
// tests' registry requires.
func loadOCI(t *testing.T, location string) *Catalog {
	t.Helper()
	src, err := newOCISource(location, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Load(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// testRepo returns the repository of a location for pushing test artifacts.
func testRepo(t *testing.T, location string) *remote.Repository {
	t.Helper()
	ref, err := parseOCI(location)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := newRepository(ref, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestPublishAndLoadOCI(t *testing.T) {
	host := newTestRegistry(t)
	fixture := fixtureTemplates(t)

	pinned, err := publish(context.Background(), fixture, "oci://"+host+"/forgeai/templates:v1", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pinned, "oci://"+host+"/forgeai/templates@sha256:") {
		t.Fatalf("pinned reference = %q, want a digest reference", pinned)
	}

	want := load(t, fixture)
	for _, location := range []string{"oci://" + host + "/forgeai/templates:v1", pinned} {
		c := loadOCI(t, location)
		checkSource(t, "", c)
		if len(c.Templates) != len(want.Templates) || c.DisplayName != want.DisplayName {
			t.Errorf("%s: got %d templates named %q, want %d named %q",
				location, len(c.Templates), c.DisplayName, len(want.Templates), want.DisplayName)
		}
	}
}

func TestPublishIsReproducible(t *testing.T) {
	host := newTestRegistry(t)
	ctx := context.Background()
	first, err := publish(ctx, fixtureTemplates(t), "oci://"+host+"/forgeai/templates:v1", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := publish(ctx, fixtureTemplates(t), "oci://"+host+"/forgeai/templates:v2", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("the same source pushed twice gave %s and %s", first, second)
	}
}

func TestPublishRefusesBrokenSource(t *testing.T) {
	host := newTestRegistry(t)
	broken := writeTree(t, map[string]string{
		"index.yaml":               goodIndex,
		"jobs/job-a/template.yaml": strings.Replace(goodJob, "name: job-a", "name: job-b", 1),
	})
	_, err := publish(context.Background(), broken, "oci://"+host+"/forgeai/broken:v1", nil, true)
	if err == nil || !strings.Contains(err.Error(), "template job-a") {
		t.Fatalf("publish of a broken source: err = %v, want the template error", err)
	}
}

func TestPublishDigestMustMatch(t *testing.T) {
	host := newTestRegistry(t)
	repo := testRepo(t, "oci://"+host+"/forgeai/templates:v1")
	wrong := "sha256:" + strings.Repeat("0", 64)
	if _, err := pushSource(context.Background(), repo, wrong, tarGz(t, []tar.Header{{Name: IndexPath, Typeflag: tar.TypeReg}})); err == nil {
		t.Fatal("pushSource accepted a digest that is not the manifest's")
	}
}

func TestOCILocations(t *testing.T) {
	for _, loc := range []string{"oci://", "oci:///templates:v1", "oci://registry.example.com/templates", "oci://registry.example.com/templates@"} {
		if _, err := Open(loc, nil); err == nil {
			t.Errorf("Open(%q) succeeded, want an error", loc)
		}
	}
	if _, err := Open("oci://registry.example.com/forgeai/templates:v1", nil); err != nil {
		t.Errorf("Open of a tagged oci location: %v", err)
	}
}

// TestOCISourceRejectsUnsafeArchive pushes sources that a published artifact
// could carry, and checks that reading them fails.
func TestOCISourceRejectsUnsafeArchive(t *testing.T) {
	cases := []struct {
		name    string
		entries []tar.Header
		want    string
	}{
		{name: "parent directory", entries: []tar.Header{{Name: "../escape.yaml", Typeflag: tar.TypeReg}}, want: "clean relative path"},
		{name: "absolute path", entries: []tar.Header{{Name: "/etc/passwd", Typeflag: tar.TypeReg}}, want: "clean relative path"},
		{name: "unclean path", entries: []tar.Header{{Name: "jobs//job-a/../x.yaml", Typeflag: tar.TypeReg}}, want: "clean relative path"},
		{name: "symlink", entries: []tar.Header{{Name: IndexPath, Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}}, want: "not a regular file"},
		{name: "duplicate", entries: []tar.Header{{Name: IndexPath, Typeflag: tar.TypeReg}, {Name: IndexPath, Typeflag: tar.TypeReg}}, want: "listed twice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host := newTestRegistry(t)
			location := "oci://" + host + "/forgeai/unsafe:v1"
			repo := testRepo(t, location)
			if _, err := pushSource(context.Background(), repo, "v1", tarGz(t, tc.entries)); err != nil {
				t.Fatal(err)
			}
			src, err := newOCISource(location, nil, true)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Load(context.Background(), src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load: err = %v, want %q", err, tc.want)
			}
		})
	}
}

// TestOCISourceNeedsSourceArtifact checks that an artifact of another type is
// not read as a template source, even when its layer looks like one.
func TestOCISourceNeedsSourceArtifact(t *testing.T) {
	host := newTestRegistry(t)
	location := "oci://" + host + "/forgeai/other:v1"
	repo := testRepo(t, location)
	ctx := context.Background()
	archive := tarGz(t, []tar.Header{{Name: IndexPath, Typeflag: tar.TypeReg}})
	layer := content.NewDescriptorFromBytes(LayerMediaType, archive)
	if err := repo.Push(ctx, layer, bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	desc, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, "application/vnd.example.other", oras.PackManifestOptions{
		Layers: []ocispec.Descriptor{layer},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Tag(ctx, desc, "v1"); err != nil {
		t.Fatal(err)
	}
	src, err := newOCISource(location, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(ctx, src); err == nil || !strings.Contains(err.Error(), "not a template source") {
		t.Fatalf("Load: err = %v, want a not-a-template-source error", err)
	}
}

// TestPublishNamesTheLayer checks that the layer has a title, so oras pull
// saves it instead of skipping it.
func TestPublishNamesTheLayer(t *testing.T) {
	host := newTestRegistry(t)
	location := "oci://" + host + "/forgeai/named:v1"
	if _, err := publish(context.Background(), fixtureTemplates(t), location, nil, true); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo := testRepo(t, location)
	desc, err := repo.Resolve(ctx, "v1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := fetchBlob(ctx, repo, desc)
	if err != nil {
		t.Fatal(err)
	}
	var m ocispec.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Layers) != 1 || m.Layers[0].Annotations[ocispec.AnnotationTitle] != LayerTitle {
		t.Fatalf("layers = %+v, want one layer titled %s", m.Layers, LayerTitle)
	}
}

// TestOCISourceMissingTag checks the message for a tag the registry does not have.
func TestOCISourceMissingTag(t *testing.T) {
	host := newTestRegistry(t)
	if _, err := publish(context.Background(), fixtureTemplates(t), "oci://"+host+"/forgeai/templates:v1", nil, true); err != nil {
		t.Fatal(err)
	}
	src, err := newOCISource("oci://"+host+"/forgeai/templates:v2", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Load(context.Background(), src)
	if err == nil || !strings.Contains(err.Error(), "no such tag or digest") {
		t.Fatalf("Load of a missing tag: err = %v, want a no-such-tag error", err)
	}
	if strings.Count(err.Error(), host) != 1 {
		t.Errorf("error repeats the reference: %v", err)
	}
}

// tarGz returns a gzipped tar of entries. Regular files get a one-byte body.
func tarGz(t *testing.T, entries []tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		var body []byte
		if h.Typeflag == tar.TypeReg {
			body = []byte("x")
		}
		h.Mode = 0o644
		h.Size = int64(len(body))
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

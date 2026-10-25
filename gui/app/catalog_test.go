package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wutz/forgeai/pkg/catalog"
)

const testIndex = `apiVersion: templates.forgeai.wutz.dev/v1alpha1
kind: TemplateIndex
metadata:
  name: test
spec:
  displayName: Test source
  templates:
    - name: job-a
      kind: Job
      displayName: Job A
      description: d
      version: 1.0.0
      path: jobs/job-a/template.yaml
`

const testJob = `apiVersion: templates.forgeai.wutz.dev/v1alpha1
kind: JobTemplate
metadata:
  name: job-a
spec:
  displayName: Job A
  description: d
  version: 1.0.0
  image: example.com/img:1
  command: echo hi
  workers: 2
`

// testSource writes a template source with one job template under a new
// temporary directory and returns its file:// location.
func testSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{
		"index.yaml":               testIndex,
		"jobs/job-a/template.yaml": testJob,
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return "file://" + root
}

func TestCatalogSourcesListsEachSourceOnce(t *testing.T) {
	platform := testSource(t)
	user := testSource(t)
	missing := "file://" + filepath.Join(t.TempDir(), "gone")

	got := catalogSources(context.Background(), platform, []string{
		user,
		user + "/", // the same source, with a trailing slash
		"",         // an empty entry is ignored
		platform,   // the platform source is not listed again
		missing,    // a source that fails is listed with its error
	})
	if len(got) != 3 {
		t.Fatalf("want 3 sources (platform, user, failing), got %d: %+v", len(got), got)
	}
	if !got[0].Platform || got[0].Location != platform {
		t.Errorf("first source must be the platform source, got %+v", got[0])
	}
	if got[1].Platform || got[1].Location != user {
		t.Errorf("user source = %+v, want location %s", got[1], user)
	}
	if got[1].DisplayName != "Test source" || len(got[1].Templates) != 1 || got[1].Templates[0].Kind != "Job" {
		t.Errorf("user source templates = %+v", got[1])
	}
	if got[2].Error == "" || got[2].Location != missing || len(got[2].Templates) != 0 {
		t.Errorf("failing source must keep its error and no templates, got %+v", got[2])
	}
	if got[0].Error != "" || got[1].Error != "" {
		t.Errorf("healthy sources report errors: %+v, %+v", got[0], got[1])
	}
}

func TestCatalogTemplateFormFromSource(t *testing.T) {
	src := testSource(t)
	f, err := catalogTemplate(context.Background(), src+"/", "job-a", catalog.KindJob)
	if err != nil {
		t.Fatalf("catalogTemplate: %v", err)
	}
	if f.Job == nil || f.Job.Image != "example.com/img:1" || f.Job.Workers != 2 {
		t.Errorf("job template = %+v", f.Job)
	}
	if _, err := catalogTemplate(context.Background(), src, "job-a", catalog.KindSandbox); err == nil {
		t.Error("a job template must not be found as a sandbox template")
	}
	_, err = catalogTemplate(context.Background(), src, "missing", catalog.KindJob)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("unknown template error = %v", err)
	}
}

func TestNormalizeSource(t *testing.T) {
	if got := normalizeSource(""); got != catalog.DefaultSource {
		t.Errorf("empty location = %q, want the default source", got)
	}
	if got := normalizeSource(" https://example.com/t/ "); got != "https://example.com/t" {
		t.Errorf("normalized = %q", got)
	}
}

func TestCheckCatalogSourceNeedsLocation(t *testing.T) {
	var a App
	if _, err := a.CheckCatalogSource("  "); err == nil {
		t.Error("an empty location must be refused before any read")
	}
}

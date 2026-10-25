package catalog

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureTemplates is a copy of the public source, wutz/forgeai-templates, so
// the tests run without the network. Update it with the public source.
func fixtureTemplates(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("testdata/templates")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// writeTree writes files (path to content) under a new temporary directory.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func load(t *testing.T, root string) *Catalog {
	t.Helper()
	src, err := Open("file://"+root, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Load(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestFixtureTemplates checks the public templates: every template loads, and
// every template file is listed in the index.
func TestFixtureTemplates(t *testing.T) {
	root := fixtureTemplates(t)
	checkSource(t, root, load(t, root))
}

// TestTemplateSource runs the same checks on the source named by
// FORGEAI_TEMPLATE_SOURCE, such as a checkout of wutz/forgeai-templates:
//
//	FORGEAI_TEMPLATE_SOURCE=file:///path/to/forgeai-templates GOWORK=off go test ./pkg/catalog -run TestTemplateSource
//
// It is skipped when the variable is unset.
func TestTemplateSource(t *testing.T) {
	loc := os.Getenv("FORGEAI_TEMPLATE_SOURCE")
	if loc == "" {
		t.Skip("FORGEAI_TEMPLATE_SOURCE is not set")
	}
	src, err := Open(loc, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Load(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	// Only a directory can be walked for unlisted files; an https source is checked by its templates alone.
	root := ""
	if after, ok := strings.CutPrefix(loc, "file://"); ok {
		root = after
	}
	checkSource(t, root, c)
}

// checkSource checks a loaded source: every template loads, both kinds are
// present, and, when root is a directory, every template file is listed in the index.
func checkSource(t *testing.T, root string, c *Catalog) {
	t.Helper()
	if c.DisplayName == "" {
		t.Error("index has no displayName")
	}
	listed := map[string]bool{}
	var jobs, sandboxes int
	for _, tpl := range c.Templates {
		listed[tpl.Path] = true
		if tpl.Error != "" {
			t.Errorf("template %s: %s", tpl.Name, tpl.Error)
		}
		switch tpl.Kind {
		case KindJob:
			jobs++
		case KindSandbox:
			sandboxes++
		}
	}
	if jobs == 0 || sandboxes == 0 {
		t.Errorf("want job and sandbox templates, got %d jobs and %d sandboxes", jobs, sandboxes)
	}
	if root == "" {
		return
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(p) != "template.yaml" {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if !listed[filepath.ToSlash(rel)] {
			t.Errorf("%s is not listed in index.yaml", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestNCCLTemplateForm checks the job form the nccl-tests template prefills:
// the instance type and name stay with the user.
func TestNCCLTemplateForm(t *testing.T) {
	c := load(t, fixtureTemplates(t))
	var found *Template
	for i := range c.Templates {
		if c.Templates[i].Name == "nccl-tests" {
			found = &c.Templates[i]
		}
	}
	if found == nil || found.Job == nil {
		t.Fatal("nccl-tests job template not found")
	}
	f := found.Job.Form("")
	if f.InstanceType != "" || f.Name != "" {
		t.Errorf("prefill must not choose a name or instance type: %+v", f)
	}
	if f.Image == "" || f.Command == "" || f.Workers != 1 {
		t.Errorf("unexpected prefill: %+v", f)
	}
}

const (
	goodIndex = `apiVersion: templates.forgeai.wutz.dev/v1alpha1
kind: TemplateIndex
metadata:
  name: test
spec:
  displayName: Test
  templates:
    - name: job-a
      kind: Job
      displayName: Job A
      description: d
      version: 1.0.0
      path: jobs/job-a/template.yaml
`
	goodJob = `apiVersion: templates.forgeai.wutz.dev/v1alpha1
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
)

func TestLoadAcceptsGoodTree(t *testing.T) {
	root := writeTree(t, map[string]string{"index.yaml": goodIndex, "jobs/job-a/template.yaml": goodJob})
	c := load(t, root)
	if len(c.Templates) != 1 || c.Templates[0].Error != "" || c.Templates[0].Job == nil {
		t.Fatalf("want one valid job template, got %+v", c.Templates)
	}
	if c.Templates[0].Job.Workers != 2 {
		t.Errorf("workers = %d, want 2", c.Templates[0].Job.Workers)
	}
}

// TestLoadRejects covers the rules a template must meet to be listed as usable.
// A bad template is listed with an error; a bad index fails the whole load.
func TestLoadRejects(t *testing.T) {
	cases := []struct {
		name  string
		index string
		job   string
		// wantIndexErr is set when Load itself must fail.
		wantIndexErr bool
		wantErr      string
	}{
		{name: "wrong index kind", index: strings.Replace(goodIndex, "kind: TemplateIndex", "kind: Catalog", 1), wantIndexErr: true},
		{name: "unknown index kind", index: strings.Replace(goodIndex, "kind: Job", "kind: Pod", 1), wantIndexErr: true},
		{name: "absolute path", index: strings.Replace(goodIndex, "path: jobs/job-a/template.yaml", "path: /etc/passwd", 1), wantIndexErr: true},
		{name: "path traversal", index: strings.Replace(goodIndex, "path: jobs/job-a/template.yaml", "path: ../secrets.yaml", 1), wantIndexErr: true},
		{name: "unclean path", index: strings.Replace(goodIndex, "path: jobs/job-a/template.yaml", "path: jobs//job-a/template.yaml", 1), wantIndexErr: true},
		{name: "unknown field in index", index: strings.Replace(goodIndex, "version: 1.0.0", "version: 1.0.0\n      tags: [x]", 1), wantIndexErr: true},
		{name: "duplicate entry", index: goodIndex + "    - name: job-a\n      kind: Job\n      displayName: Job A\n      description: d\n      version: 1.0.0\n      path: jobs/job-a/template.yaml\n", wantIndexErr: true},
		{name: "empty index", index: "apiVersion: templates.forgeai.wutz.dev/v1alpha1\nkind: TemplateIndex\nmetadata:\n  name: test\nspec:\n  displayName: Test\n  templates: []\n", wantIndexErr: true},
		{name: "name mismatch", job: strings.Replace(goodJob, "name: job-a", "name: job-b", 1), wantErr: "does not match"},
		{name: "summary mismatch", job: strings.Replace(goodJob, "displayName: Job A", "displayName: Other", 1), wantErr: "must match the index entry"},
		{name: "wrong file kind", job: strings.Replace(goodJob, "kind: JobTemplate", "kind: SandboxTemplate", 1), wantErr: "must be"},
		{name: "unknown spec field", job: strings.Replace(goodJob, "workers: 2", "workers: 2\n  replicas: 3", 1), wantErr: "unknown field"},
		{name: "missing image", job: strings.Replace(goodJob, "  image: example.com/img:1\n", "", 1), wantErr: "image is required"},
		{name: "invalid name", index: strings.ReplaceAll(goodIndex, "name: job-a", "name: Job_A"), job: strings.ReplaceAll(goodJob, "name: job-a", "name: Job_A"), wantErr: "invalid name"},
		{name: "zero workers", job: strings.Replace(goodJob, "workers: 2", "workers: 0", 1), wantErr: "workers must be at least 1"},
		{name: "bad env name", job: strings.Replace(goodJob, "  workers: 2", "  workers: 2\n  env:\n    - name: 1X\n      value: y", 1), wantErr: "environment variable"},
		{name: "missing file", job: "", wantErr: "no such file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			index := tc.index
			if index == "" {
				index = goodIndex
			}
			files := map[string]string{"index.yaml": index}
			if tc.job != "" || tc.wantErr != "no such file" {
				job := tc.job
				if job == "" {
					job = goodJob
				}
				files["jobs/job-a/template.yaml"] = job
			}
			root := writeTree(t, files)
			src, err := Open("file://"+root, nil)
			if err != nil {
				t.Fatal(err)
			}
			c, err := Load(context.Background(), src)
			if tc.wantIndexErr {
				if err == nil {
					t.Fatalf("Load succeeded, want an index error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if len(c.Templates) != 1 {
				t.Fatalf("want 1 template, got %d", len(c.Templates))
			}
			got := c.Templates[0]
			if got.Error == "" || !strings.Contains(got.Error, tc.wantErr) {
				t.Errorf("Error = %q, want it to contain %q", got.Error, tc.wantErr)
			}
			if got.Job != nil || got.Sandbox != nil {
				t.Errorf("a failed template must not carry a spec")
			}
		})
	}
}

func TestSandboxTemplateRules(t *testing.T) {
	index := `apiVersion: templates.forgeai.wutz.dev/v1alpha1
kind: TemplateIndex
metadata:
  name: test
spec:
  displayName: Test
  templates:
    - name: box
      kind: Sandbox
      displayName: Box
      description: d
      version: 1.0.0
      path: sandboxes/box/template.yaml
`
	box := `apiVersion: templates.forgeai.wutz.dev/v1alpha1
kind: SandboxTemplate
metadata:
  name: box
spec:
  displayName: Box
  description: d
  version: 1.0.0
  image: ""
`
	c := load(t, writeTree(t, map[string]string{"index.yaml": index, "sandboxes/box/template.yaml": box}))
	if got := c.Templates[0].Error; !strings.Contains(got, "image is required") {
		t.Errorf("Error = %q, want the image rule", got)
	}
	// The image is required: a template is explicit about what it runs.
	box = strings.Replace(box, `image: ""`, "image: example.com/box:1\n  command: sleep infinity", 1)
	c = load(t, writeTree(t, map[string]string{"index.yaml": index, "sandboxes/box/template.yaml": box}))
	if c.Templates[0].Error != "" || c.Templates[0].Sandbox == nil {
		t.Errorf("want a valid sandbox template, got error %q", c.Templates[0].Error)
	}
}

func TestOpenHTTPSource(t *testing.T) {
	srv := httptest.NewServer(http.FileServer(http.Dir(fixtureTemplates(t))))
	defer srv.Close()
	src, err := Open(srv.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Load(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Templates) == 0 {
		t.Fatal("no templates over HTTP")
	}
	for _, tpl := range c.Templates {
		if tpl.Error != "" {
			t.Errorf("template %s over HTTP: %s", tpl.Name, tpl.Error)
		}
	}
}

func TestHTTPSourceMissingIndex(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	src, err := Open(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(context.Background(), src); err == nil {
		t.Fatal("Load succeeded without an index")
	}
}

func TestOpenRejectsLocations(t *testing.T) {
	for _, loc := range []string{"ftp://example.com/templates", "file://relative/path", "file://", "https://", "templates/"} {
		if _, err := Open(loc, nil); err == nil {
			t.Errorf("Open(%q) succeeded, want an error", loc)
		}
	}
	if _, err := Open("", nil); err != nil {
		t.Errorf("empty location must use the default source: %v", err)
	}
}

// TestSandboxFileRejects checks the sandbox file against its index entry: the
// kind, the name, the summary and the fields it may carry.
func TestSandboxFileRejects(t *testing.T) {
	index := `apiVersion: templates.forgeai.wutz.dev/v1alpha1
kind: TemplateIndex
metadata:
  name: test
spec:
  displayName: Test
  templates:
    - name: box
      kind: Sandbox
      displayName: Box
      description: d
      version: 1.0.0
      path: sandboxes/box/template.yaml
`
	box := `apiVersion: templates.forgeai.wutz.dev/v1alpha1
kind: SandboxTemplate
metadata:
  name: box
spec:
  displayName: Box
  description: d
  version: 1.0.0
  image: example.com/box:1
`
	cases := []struct {
		name    string
		index   string
		box     string
		wantErr string
	}{
		{name: "wrong file kind", box: strings.Replace(box, "kind: SandboxTemplate", "kind: JobTemplate", 1), wantErr: "must be"},
		{name: "name mismatch", box: strings.Replace(box, "name: box", "name: other", 1), wantErr: "does not match"},
		{name: "summary mismatch", box: strings.Replace(box, "displayName: Box", "displayName: Other", 1), wantErr: "must match the index entry"},
		{name: "unknown spec field", box: strings.Replace(box, "image: example.com/box:1", "image: example.com/box:1\n  replicas: 3", 1), wantErr: "unknown field"},
		{name: "unparseable file", box: "spec: [", wantErr: "parse sandboxes/box/template.yaml"},
		{
			name:    "empty summary",
			index:   strings.ReplaceAll(index, "displayName: Box", `displayName: " "`),
			box:     strings.ReplaceAll(box, "displayName: Box", `displayName: " "`),
			wantErr: "required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx := tc.index
			if idx == "" {
				idx = index
			}
			c := load(t, writeTree(t, map[string]string{"index.yaml": idx, "sandboxes/box/template.yaml": tc.box}))
			got := c.Templates[0]
			if got.Error == "" || !strings.Contains(got.Error, tc.wantErr) {
				t.Errorf("Error = %q, want it to contain %q", got.Error, tc.wantErr)
			}
			if got.Sandbox != nil {
				t.Error("a failed sandbox template must not carry a spec")
			}
		})
	}
}

func TestLoadRejectsEmptyNameAndSummary(t *testing.T) {
	cases := map[string]struct{ index, job, wantErr string }{
		"empty entry name": {
			index:   strings.Replace(goodIndex, "name: job-a", `name: ""`, 1),
			wantErr: "name is required",
		},
		"blank summary": {
			index:   strings.ReplaceAll(goodIndex, "displayName: Job A", `displayName: " "`),
			job:     strings.ReplaceAll(goodJob, "displayName: Job A", `displayName: " "`),
			wantErr: "displayName and version are required",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			job := tc.job
			if job == "" {
				job = goodJob
			}
			c := load(t, writeTree(t, map[string]string{"index.yaml": tc.index, "jobs/job-a/template.yaml": job}))
			if got := c.Templates[0].Error; !strings.Contains(got, tc.wantErr) {
				t.Errorf("Error = %q, want it to contain %q", got, tc.wantErr)
			}
		})
	}
}

func TestHTTPSourceUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	location := srv.URL
	srv.Close()
	src, err := Open(location, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(context.Background(), src); err == nil || !strings.Contains(err.Error(), "fetch index.yaml") {
		t.Errorf("Load from a closed server = %v, want a fetch error", err)
	}
}

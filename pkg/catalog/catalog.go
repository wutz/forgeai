// Package catalog reads the public template source: an index of job and
// sandbox templates and the template files it lists. A template is a form
// prefill for the GUI's create dialogs (image, command, env, workers and
// retries for jobs; agent blueprint for sandboxes). Nothing is applied to a
// cluster from the source: the user reviews the prefilled form and submits it.
//
// The source layout is documented in the README of wutz/forgeai-templates.
package catalog

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/wutz/forgeai/pkg/sandbox"
	"github.com/wutz/forgeai/pkg/training"
)

const (
	// APIVersion is the apiVersion of the index and of every template file.
	APIVersion = "templates.forgeai.wutz.dev/v1alpha1"
	// IndexKind is the kind of the index file.
	IndexKind = "TemplateIndex"
	// IndexPath is the index file, relative to the source root.
	IndexPath = "index.yaml"
	// DefaultSource is the public template source: the wutz/forgeai-templates
	// repository on its default branch.
	DefaultSource = "https://raw.githubusercontent.com/wutz/forgeai-templates/main"

	// maxFileBytes bounds one fetched file; templates are a few kilobytes.
	maxFileBytes = 1 << 20
	// placeholderInstanceType stands in for the instance type while a job
	// template is validated: a template does not choose one.
	placeholderInstanceType = "template"
)

// Kind is the kind of a template in the index.
type Kind string

const (
	// KindJob is a job template: the form of a new job.
	KindJob Kind = "Job"
	// KindSandbox is a sandbox template: the agent a new sandbox starts from.
	KindSandbox Kind = "Sandbox"
)

// nameRE is the name rule shared with the job and sandbox forms.
var nameRE = regexp.MustCompile(`^[a-z]([-a-z0-9]*[a-z0-9])?$`)

// Index is index.yaml: the templates of the source, with the summary fields
// the GUI lists. Each summary must match the template file it points to.
type Index struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   Meta   `json:"metadata"`
	Spec       struct {
		DisplayName string  `json:"displayName"`
		Homepage    string  `json:"homepage,omitempty"`
		Templates   []Entry `json:"templates"`
	} `json:"spec"`
}

// Meta is the metadata of an index or a template file.
type Meta struct {
	Name string `json:"name"`
}

// Entry is one template in the index.
type Entry struct {
	Name        string `json:"name"`
	Kind        Kind   `json:"kind"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Version     string `json:"version"`
	// Path of the template file, relative to the source root.
	Path string `json:"path"`
}

// JobSpec is the spec of a JobTemplate file.
type JobSpec struct {
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Image       string `json:"image"`
	// Command is a bash script run on every worker.
	Command string            `json:"command,omitempty"`
	Env     []training.EnvVar `json:"env,omitempty"`
	// Workers is the default number of workers; the form may change it.
	Workers    int32            `json:"workers"`
	MaxRetries int32            `json:"maxRetries,omitempty"`
	RetryOn    training.RetryOn `json:"retryOn,omitempty"`
}

// SandboxSpec is the spec of a SandboxTemplate file.
type SandboxSpec struct {
	DisplayName  string           `json:"displayName"`
	Description  string           `json:"description"`
	Version      string           `json:"version"`
	Image        string           `json:"image"`
	Command      string           `json:"command,omitempty"`
	Env          []sandbox.EnvVar `json:"env,omitempty"`
	RuntimeClass string           `json:"runtimeClass,omitempty"`
}

// jobFile and sandboxFile are the template files of the two kinds.
type jobFile struct {
	APIVersion string  `json:"apiVersion"`
	Kind       string  `json:"kind"`
	Metadata   Meta    `json:"metadata"`
	Spec       JobSpec `json:"spec"`
}

type sandboxFile struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   Meta        `json:"metadata"`
	Spec       SandboxSpec `json:"spec"`
}

// Template is a validated template of the source. Error is set instead of the
// spec when the file cannot be read or is invalid: the template is listed but
// cannot be used, so one broken file does not hide the others.
type Template struct {
	Name        string
	Kind        Kind
	DisplayName string
	Description string
	Version     string
	Path        string
	Job         *JobSpec
	Sandbox     *SandboxSpec
	Error       string
}

// Catalog is a loaded template source.
type Catalog struct {
	DisplayName string
	Homepage    string
	Templates   []Template
}

// Source reads files of a template source by path relative to its root.
type Source interface {
	Read(ctx context.Context, rel string) ([]byte, error)
}

// Open returns the source at location: a file:// directory, or an http(s)
// URL the files are appended to. An empty location is the DefaultSource.
func Open(location string, client *http.Client) (Source, error) {
	if location == "" {
		location = DefaultSource
	}
	switch {
	case strings.HasPrefix(location, "file://"):
		root := strings.TrimPrefix(location, "file://")
		if root == "" || !filepath.IsAbs(root) {
			return nil, fmt.Errorf("template source %q: file sources need an absolute path", location)
		}
		return dirSource(root), nil
	case strings.HasPrefix(location, "https://"), strings.HasPrefix(location, "http://"):
		u, err := url.Parse(location)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("template source %q is not a valid URL", location)
		}
		if client == nil {
			client = http.DefaultClient
		}
		return &httpSource{base: strings.TrimSuffix(location, "/"), client: client}, nil
	}
	return nil, fmt.Errorf("template source %q must be an https:// URL or a file:// path", location)
}

type dirSource string

func (d dirSource) Read(_ context.Context, rel string) ([]byte, error) {
	return readLimited(os.Open(filepath.Join(string(d), filepath.FromSlash(rel))))
}

type httpSource struct {
	base   string
	client *http.Client
}

func (h *httpSource) Read(ctx context.Context, rel string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.base+"/"+rel, nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rel, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: %s", rel, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxFileBytes))
}

func readLimited(f *os.File, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxFileBytes))
}

// Load reads the index and every template it lists. An index that cannot be
// read or parsed is an error; a template that fails is reported in its Error.
func Load(ctx context.Context, src Source) (*Catalog, error) {
	data, err := src.Read(ctx, IndexPath)
	if err != nil {
		return nil, fmt.Errorf("read template index: %w", err)
	}
	var idx Index
	if err := yaml.UnmarshalStrict(data, &idx); err != nil {
		return nil, fmt.Errorf("parse template index: %w", err)
	}
	if err := checkIndex(&idx); err != nil {
		return nil, err
	}
	c := &Catalog{DisplayName: idx.Spec.DisplayName, Homepage: idx.Spec.Homepage}
	for _, e := range idx.Spec.Templates {
		t := Template{
			Name: e.Name, Kind: e.Kind, DisplayName: e.DisplayName,
			Description: e.Description, Version: e.Version, Path: e.Path,
		}
		if err := loadTemplate(ctx, src, e, &t); err != nil {
			t.Error = err.Error()
			t.Job, t.Sandbox = nil, nil
		}
		c.Templates = append(c.Templates, t)
	}
	return c, nil
}

// checkIndex validates what can be checked without reading the template files.
func checkIndex(idx *Index) error {
	if idx.APIVersion != APIVersion || idx.Kind != IndexKind {
		return fmt.Errorf("template index must be %s %s, got %s %s", APIVersion, IndexKind, idx.APIVersion, idx.Kind)
	}
	if len(idx.Spec.Templates) == 0 {
		return fmt.Errorf("template index lists no templates")
	}
	seen := map[string]bool{}
	for _, e := range idx.Spec.Templates {
		if e.Kind != KindJob && e.Kind != KindSandbox {
			return fmt.Errorf("template %s: kind must be Job or Sandbox, got %q", e.Name, e.Kind)
		}
		key := string(e.Kind) + "/" + e.Name
		if seen[key] {
			return fmt.Errorf("template %s is listed twice", key)
		}
		seen[key] = true
		if clean := path.Clean(e.Path); e.Path == "" || path.IsAbs(e.Path) || clean != e.Path || strings.HasPrefix(clean, "..") {
			return fmt.Errorf("template %s: path %q must be a clean path inside the source", e.Name, e.Path)
		}
	}
	return nil
}

// loadTemplate reads and validates the file of one index entry and copies its
// spec into t.
func loadTemplate(ctx context.Context, src Source, e Entry, t *Template) error {
	data, err := src.Read(ctx, e.Path)
	if err != nil {
		return err
	}
	if e.Name == "" {
		return fmt.Errorf("name is required")
	}
	switch e.Kind {
	case KindJob:
		var f jobFile
		if err := yaml.UnmarshalStrict(data, &f); err != nil {
			return fmt.Errorf("parse %s: %w", e.Path, err)
		}
		if err := checkFile(f.APIVersion, f.Kind, "JobTemplate", f.Metadata.Name, e); err != nil {
			return err
		}
		if err := checkSummary(e, f.Spec.DisplayName, f.Spec.Description, f.Spec.Version); err != nil {
			return err
		}
		// The form is checked with a placeholder instance type: the user picks the real one.
		check := f.Spec.Form(e.Name)
		check.InstanceType = placeholderInstanceType
		if err := check.Validate(); err != nil {
			return err
		}
		t.Job = &f.Spec
	case KindSandbox:
		var f sandboxFile
		if err := yaml.UnmarshalStrict(data, &f); err != nil {
			return fmt.Errorf("parse %s: %w", e.Path, err)
		}
		if err := checkFile(f.APIVersion, f.Kind, "SandboxTemplate", f.Metadata.Name, e); err != nil {
			return err
		}
		if err := checkSummary(e, f.Spec.DisplayName, f.Spec.Description, f.Spec.Version); err != nil {
			return err
		}
		// RenderTemplate runs the same checks the GUI applies to a sandbox template.
		if _, err := sandbox.RenderTemplate(f.Spec.Form(e.Name), sandbox.Context{Namespace: "catalog"}); err != nil {
			return err
		}
		t.Sandbox = &f.Spec
	}
	return nil
}

func checkFile(apiVersion, kind, wantKind, name string, e Entry) error {
	if apiVersion != APIVersion || kind != wantKind {
		return fmt.Errorf("%s must be %s %s, got %s %s", e.Path, APIVersion, wantKind, apiVersion, kind)
	}
	if name != e.Name {
		return fmt.Errorf("%s: metadata.name %q does not match the index name %q", e.Path, name, e.Name)
	}
	return nil
}

func checkSummary(e Entry, displayName, description, version string) error {
	if displayName != e.DisplayName || description != e.Description || version != e.Version {
		return fmt.Errorf("%s: displayName, description and version must match the index entry %s", e.Path, e.Name)
	}
	if strings.TrimSpace(displayName) == "" || strings.TrimSpace(version) == "" {
		return fmt.Errorf("%s: displayName and version are required", e.Path)
	}
	return nil
}

// Form is the job form the template prefills. The instance type and the job
// name are chosen by the user, so they are left empty.
func (s JobSpec) Form(name string) *training.Form {
	return &training.Form{
		Name: name, Description: s.Description, Image: s.Image, Command: s.Command, Env: s.Env,
		Workers: s.Workers, MaxRetries: s.MaxRetries, RetryOn: s.RetryOn,
	}
}

// Form is the sandbox template form the template prefills.
func (s SandboxSpec) Form(name string) *sandbox.TemplateForm {
	return &sandbox.TemplateForm{
		Name: name, Description: s.Description, Image: s.Image, Command: s.Command, Env: s.Env,
		RuntimeClass: s.RuntimeClass,
	}
}

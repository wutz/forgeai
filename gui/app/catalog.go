package app

import (
	"fmt"

	"github.com/wutz/forgeai/pkg/catalog"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/sandbox"
	"github.com/wutz/forgeai/pkg/training"
)

// CatalogTemplate is a template of the public template source, as the create
// dialogs list it.
type CatalogTemplate struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
	Image       string `json:"image,omitempty"`
	// Error is set for a template the source lists but the app cannot use.
	Error string `json:"error,omitempty"`
}

// CatalogTemplates lists the templates of the template source the platform
// configures (platform config templates.source, the forgeai default when
// unset). Job and sandbox templates are both listed; the dialogs pick by kind.
func (a *App) CatalogTemplates() ([]CatalogTemplate, error) {
	cat, err := a.loadCatalog()
	if err != nil {
		return nil, err
	}
	out := make([]CatalogTemplate, 0, len(cat.Templates))
	for _, t := range cat.Templates {
		v := CatalogTemplate{
			Name: t.Name, Kind: string(t.Kind), DisplayName: t.DisplayName,
			Description: t.Description, Version: t.Version, Error: t.Error,
		}
		switch {
		case t.Job != nil:
			v.Image = t.Job.Image
		case t.Sandbox != nil:
			v.Image = t.Sandbox.Image
		}
		out = append(out, v)
	}
	return out, nil
}

// CatalogJobForm returns the job form a job template prefills. The name and
// the instance type stay empty for the user to choose.
func (a *App) CatalogJobForm(name string) (training.Form, error) {
	t, err := a.catalogTemplate(name, catalog.KindJob)
	if err != nil {
		return training.Form{}, err
	}
	return *t.Job.Form(""), nil
}

// CatalogSandboxForm returns the sandbox template form a sandbox template
// prefills, the same shape as a project SandboxTemplate.
func (a *App) CatalogSandboxForm(name string) (sandbox.TemplateForm, error) {
	t, err := a.catalogTemplate(name, catalog.KindSandbox)
	if err != nil {
		return sandbox.TemplateForm{}, err
	}
	return *t.Sandbox.Form(""), nil
}

// catalogTemplate finds a usable template of the given kind.
func (a *App) catalogTemplate(name string, kind catalog.Kind) (*catalog.Template, error) {
	cat, err := a.loadCatalog()
	if err != nil {
		return nil, err
	}
	for i := range cat.Templates {
		t := &cat.Templates[i]
		if t.Name != name || t.Kind != kind {
			continue
		}
		if t.Error != "" {
			return nil, fmt.Errorf("template %s cannot be used: %s", name, t.Error)
		}
		return t, nil
	}
	return nil, fmt.Errorf("no %s template named %q in the template source", kind, name)
}

// loadCatalog reads the template source named by the platform config. The
// source is read on every call: the dialogs call it when they open, and a
// fix to a template shows up without restarting the app.
func (a *App) loadCatalog() (*catalog.Catalog, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	cfg, err := platform.Load(ctx, c.k8s, c.configNamespace())
	if err != nil {
		return nil, err
	}
	src, err := catalog.Open(cfg.Templates.Source, nil)
	if err != nil {
		return nil, err
	}
	return catalog.Load(ctx, src)
}

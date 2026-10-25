package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/wutz/forgeai/pkg/catalog"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/sandbox"
	"github.com/wutz/forgeai/pkg/training"
)

// CatalogSource is one template source the create dialogs read: the source the
// platform config names, and each source the user added on this machine.
type CatalogSource struct {
	// Location is the source's https:// URL or file:// directory. The dialogs
	// name the templates they pick by it.
	Location    string `json:"location"`
	DisplayName string `json:"displayName,omitempty"`
	// Platform is set for the source the platform config names. It is not
	// published by the user, so the dialogs show it without a trust notice.
	Platform bool `json:"platform"`
	// Error is set when the source cannot be read; its templates are then empty.
	Error     string            `json:"error,omitempty"`
	Templates []CatalogTemplate `json:"templates"`
}

// CatalogTemplate is a template of a template source, as the create dialogs
// list it.
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

// CatalogSources reads the template source the platform config names (the
// forgeai default when unset) and each of userSources, the sources the user
// added on this machine. A source that fails is listed with its error; the
// others still load.
func (a *App) CatalogSources(userSources []string) ([]CatalogSource, error) {
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
	return catalogSources(ctx, cfg.Templates.Source, userSources), nil
}

// CheckCatalogSource reads a source the user is about to add, so the dialog
// shows its name and templates, or its error, before the source is saved.
func (a *App) CheckCatalogSource(location string) (CatalogSource, error) {
	if strings.TrimSpace(location) == "" {
		return CatalogSource{}, fmt.Errorf("enter the location of a template source")
	}
	ctx, cancel := a.call()
	defer cancel()
	return readCatalogSource(ctx, normalizeSource(location), false), nil
}

// CatalogJobForm returns the job form a job template prefills from the source
// at location. The name and the instance type stay empty for the user to choose.
func (a *App) CatalogJobForm(location, name string) (training.Form, error) {
	ctx, cancel := a.call()
	defer cancel()
	t, err := catalogTemplate(ctx, location, name, catalog.KindJob)
	if err != nil {
		return training.Form{}, err
	}
	return *t.Job.Form(""), nil
}

// CatalogSandboxForm returns the sandbox template form a sandbox template
// prefills from the source at location, the same shape as a project
// SandboxTemplate.
func (a *App) CatalogSandboxForm(location, name string) (sandbox.TemplateForm, error) {
	ctx, cancel := a.call()
	defer cancel()
	t, err := catalogTemplate(ctx, location, name, catalog.KindSandbox)
	if err != nil {
		return sandbox.TemplateForm{}, err
	}
	return *t.Sandbox.Form(""), nil
}

// catalogSources reads the platform source, then each user source that is not
// the platform source and not listed twice.
func catalogSources(ctx context.Context, platformLocation string, userSources []string) []CatalogSource {
	platformKey := normalizeSource(platformLocation)
	out := []CatalogSource{readCatalogSource(ctx, platformKey, true)}
	seen := map[string]bool{platformKey: true}
	for _, loc := range userSources {
		if strings.TrimSpace(loc) == "" {
			continue
		}
		key := normalizeSource(loc)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, readCatalogSource(ctx, key, false))
	}
	return out
}

// readCatalogSource loads one source and turns it into the dialog view. A
// failure is kept in Error rather than returned, so one source does not hide
// the others.
func readCatalogSource(ctx context.Context, location string, platform bool) CatalogSource {
	s := CatalogSource{Location: location, Platform: platform, Templates: []CatalogTemplate{}}
	cat, err := loadSource(ctx, location)
	if err != nil {
		s.Error = err.Error()
		return s
	}
	s.DisplayName = cat.DisplayName
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
		s.Templates = append(s.Templates, v)
	}
	return s
}

// catalogTemplate finds a usable template of the given kind in the source at
// location.
func catalogTemplate(ctx context.Context, location, name string, kind catalog.Kind) (*catalog.Template, error) {
	key := normalizeSource(location)
	cat, err := loadSource(ctx, key)
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
	return nil, fmt.Errorf("no %s template named %q in the template source %s", kind, name, key)
}

// loadSource reads a template source. It is read on every call: the dialogs
// read the sources when they open, and a fix to a template shows up without
// restarting the app.
func loadSource(ctx context.Context, location string) (*catalog.Catalog, error) {
	src, err := catalog.Open(location, nil)
	if err != nil {
		return nil, err
	}
	return catalog.Load(ctx, src)
}

// normalizeSource is the key a template source is known by: the forgeai default
// when unset, and without trailing slashes, so one source is not listed twice.
func normalizeSource(location string) string {
	location = strings.TrimRight(strings.TrimSpace(location), "/")
	if location == "" {
		return catalog.DefaultSource
	}
	return location
}

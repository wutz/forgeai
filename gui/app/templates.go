package app

import (
	"time"

	"github.com/scclabs/forgeai/pkg/kf"
	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/training"
)

// Template is the view model of a job template (a TrainingRuntime).
type Template struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Form        *training.Form `json:"form,omitempty"`
	Created     string         `json:"created"`
}

// ListTemplates lists the job templates of the project. Cluster runtimes
// (platform templates) are marked Template=true names but live cluster-wide;
// the project templates are namespaced TrainingRuntimes.
func (a *App) ListTemplates() ([]Template, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	items, err := listRuntimes(ctx, c, false)
	if err != nil {
		return nil, err
	}
	out := make([]Template, 0, len(items))
	for i := range items {
		rt := &items[i]
		f, err := training.TemplateForm(rt)
		if err != nil {
			return nil, err
		}
		t := Template{
			Name:        rt.Name,
			Description: rt.Annotations[naming.AnnotationDescription],
			Created:     rt.CreationTimestamp.UTC().Format(time.RFC3339),
			Form:        f,
		}
		out = append(out, t)
	}
	return out, nil
}

// CreateTemplate saves a job template from a form.
func (a *App) CreateTemplate(f training.Form) (*Template, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	rt, err := training.Template(&f, c.namespace())
	if err != nil {
		return nil, err
	}
	u, err := runtimeObject(rt)
	if err != nil {
		return nil, err
	}
	if err := c.k8s.Create(ctx, u); err != nil {
		return nil, err
	}
	form, err := training.TemplateForm(rt)
	if err != nil {
		return nil, err
	}
	return &Template{Name: rt.Name, Description: f.Description, Form: form}, nil
}

// DeleteTemplate deletes a job template.
func (a *App) DeleteTemplate(name string) error {
	c, err := a.current()
	if err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()
	return c.k8s.Delete(ctx, runtimeStub(kf.TrainingRuntimeGVK, c.namespace(), name))
}

// TemplateNames returns the templates selectable when creating a job
// (the names only, for a dropdown).
func (a *App) TemplateNames() ([]string, error) {
	ts, err := a.ListTemplates()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Name)
	}
	return out, nil
}

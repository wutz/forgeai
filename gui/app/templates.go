package app

import (
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/training"
)

// Template is the view model of a job template (a TrainingRuntime).
type Template struct {
	Name            string         `json:"name"`
	Description     string         `json:"description,omitempty"`
	Form            *training.Form `json:"form,omitempty"`
	Created         string         `json:"created"`
	ResourceVersion string         `json:"resourceVersion,omitempty"`
	// Error is set for a template whose spec the app cannot read; it has no form,
	// so it cannot be edited or used, only deleted.
	Error string `json:"error,omitempty"`
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
		// A template whose spec annotation does not parse is still listed, with its
		// name and error, so the user can see it and delete it; one bad annotation
		// does not hide the other templates.
		f, err := training.TemplateForm(rt)
		t := Template{
			Name:            rt.Name,
			Description:     rt.Annotations[naming.AnnotationDescription],
			Created:         rt.CreationTimestamp.UTC().Format(time.RFC3339),
			ResourceVersion: rt.ResourceVersion,
		}
		if err != nil {
			t.Error = err.Error()
		} else {
			t.Form = f
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
	return &Template{Name: rt.Name, Description: f.Description, Form: form, ResourceVersion: u.GetResourceVersion()}, nil
}

// UpdateTemplate replaces the form of an existing job template. The name is
// the template's identity and does not change. Jobs created from the template
// keep their settings. resourceVersion is the version the form was loaded at;
// the update fails if the template changed since.
func (a *App) UpdateTemplate(name, resourceVersion string, f training.Form) (*Template, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	cur := runtimeStub(kf.TrainingRuntimeGVK, c.namespace(), name)
	if err := c.k8s.Get(ctx, client.ObjectKeyFromObject(cur), cur); err != nil {
		return nil, err
	}
	if cur.GetLabels()[naming.LabelManagedBy] != naming.ManagedBy {
		return nil, fmt.Errorf("template %s was not created by forgeai and cannot be edited here", name)
	}
	f.Name = name
	rt, err := training.Template(&f, c.namespace())
	if err != nil {
		return nil, err
	}
	u, err := runtimeObject(rt)
	if err != nil {
		return nil, err
	}
	// Updating against the version the form was loaded at makes a concurrent
	// change fail with a conflict, instead of being overwritten.
	if resourceVersion == "" {
		return nil, fmt.Errorf("template %s has no loaded version to update from; reload it and try again", name)
	}
	u.SetResourceVersion(resourceVersion)
	if err := c.k8s.Update(ctx, u); err != nil {
		return nil, err
	}
	form, err := training.TemplateForm(rt)
	if err != nil {
		return nil, err
	}
	return &Template{Name: rt.Name, Description: f.Description, Form: form, ResourceVersion: u.GetResourceVersion()}, nil
}

// DeleteTemplate deletes a job template. Like UpdateTemplate, it only deletes
// the templates forgeai created.
func (a *App) DeleteTemplate(name string) error {
	c, err := a.current()
	if err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()
	cur := runtimeStub(kf.TrainingRuntimeGVK, c.namespace(), name)
	if err := c.k8s.Get(ctx, client.ObjectKeyFromObject(cur), cur); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if cur.GetLabels()[naming.LabelManagedBy] != naming.ManagedBy {
		return fmt.Errorf("template %s was not created by forgeai and cannot be deleted here", name)
	}
	if err := c.k8s.Delete(ctx, cur); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
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
		// An unreadable template has no form to prefill a job from, so it is not offered.
		if t.Error != "" {
			continue
		}
		out = append(out, t.Name)
	}
	return out, nil
}

package app

import (
	"errors"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scclabs/forgeai/pkg/harbor"
	"github.com/scclabs/forgeai/pkg/naming"
)

// Image is one pushed image tag.
type Image struct {
	Repository string   `json:"repository"`
	Digest     string   `json:"digest"`
	Tags       []string `json:"tags"`
	SizeMiB    int64    `json:"sizeMiB"`
	PushedAt   string   `json:"pushedAt"`
	PulledAt   string   `json:"pulledAt,omitempty"`
}

// ImageRepo groups the images of one repository.
type ImageRepo struct {
	Name   string  `json:"name"`
	Count  int64   `json:"count"`
	Pulls  int64   `json:"pulls"`
	Images []Image `json:"images"`
}

// Registry is the project registry: the robot credentials and how to push.
type Registry struct {
	Enabled  bool   `json:"enabled"`
	URL      string `json:"url"` // cr.example.com:7443/forge-foo
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Secret   string `json:"secret"`
}

// Registry returns the push target of the project. The robot credentials come
// from the project Secret; view-only members get the URL without them.
func (a *App) Registry() (*Registry, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	out := &Registry{Secret: naming.RegistrySecretName}
	sec := &corev1.Secret{}
	secKey := client.ObjectKey{Namespace: c.namespace(), Name: naming.RegistrySecretName}
	if err := c.k8s.Get(ctx, secKey, sec); err != nil {
		if !isNotFound(err) {
			return nil, err
		}
		return out, nil // registry not set up for this project
	}
	out.Enabled = true
	out.URL = string(sec.Data["server"]) + "/" + c.namespace()
	out.Username = string(sec.Data["username"])
	out.Password = string(sec.Data["password"])
	return out, nil
}

// ListImages lists the image repositories of the project and their tags,
// using the robot credentials from the project Secret (they carry list
// rights on the project).
func (a *App) ListImages() ([]ImageRepo, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	reg, err := a.Registry()
	if err != nil {
		return nil, err
	}
	if !reg.Enabled {
		return []ImageRepo{}, nil
	}
	ctx, cancel := a.call()
	defer cancel()
	hc := harbor.New(harborAPIURL(reg), reg.Username, reg.Password, a.insecureRegistry())
	repos, err := hc.ListRepositories(ctx, harborProject(c))
	if err != nil {
		return nil, err
	}
	out := make([]ImageRepo, 0, len(repos))
	for _, r := range repos {
		arts, err := hc.ListArtifacts(ctx, harborProject(c), r.Name)
		if err != nil {
			return nil, err
		}
		ir := ImageRepo{Name: r.Name, Count: r.ArtifactCount, Pulls: r.PullCount}
		for _, art := range arts {
			ir.Images = append(ir.Images, Image{
				Repository: r.Name, Digest: art.Digest, Tags: art.TagNames(),
				SizeMiB: art.Size >> 20, PushedAt: art.PushTime, PulledAt: art.PullTime,
			})
		}
		out = append(out, ir)
	}
	return out, nil
}

// DeleteImage deletes one image by digest or tag.
func (a *App) DeleteImage(repository, reference string) error {
	c, err := a.current()
	if err != nil {
		return err
	}
	reg, err := a.Registry()
	if err != nil {
		return err
	}
	if !reg.Enabled {
		return errNoRegistry
	}
	ctx, cancel := a.call()
	defer cancel()
	hc := harbor.New(harborAPIURL(reg), reg.Username, reg.Password, a.insecureRegistry())
	return hc.DeleteArtifact(ctx, harborProject(c), repository, reference)
}

// DeleteImageRepo deletes a whole repository with all its tags.
func (a *App) DeleteImageRepo(repository string) error {
	c, err := a.current()
	if err != nil {
		return err
	}
	reg, err := a.Registry()
	if err != nil {
		return err
	}
	if !reg.Enabled {
		return errNoRegistry
	}
	ctx, cancel := a.call()
	defer cancel()
	hc := harbor.New(harborAPIURL(reg), reg.Username, reg.Password, a.insecureRegistry())
	return hc.DeleteRepository(ctx, harborProject(c), repository)
}

var errNoRegistry = errors.New("registry is not configured for this project")

// harborProject is the Harbor project backing the namespace: equal to the
// namespace (the operator keeps them in sync).
func harborProject(c *Connection) string { return c.namespace() }

// harborAPIURL strips the project suffix from the push URL.
func harborAPIURL(reg *Registry) string {
	if i := strings.IndexByte(reg.URL, '/'); i >= 0 {
		return "https://" + reg.URL[:i]
	}
	return "https://" + reg.URL
}

// insecureRegistry reports whether TLS verification is off (private CA).
func (a *App) insecureRegistry() bool {
	c, err := a.current()
	return err == nil && c.insecure
}

package app

import (
	"context"
	"errors"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wutz/forgeai/pkg/harbor"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
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

// ImageRepo groups the images of one repository. Error is set when its images
// could not be listed; the repository is still returned, with its counts.
type ImageRepo struct {
	Name   string  `json:"name"`
	Count  int64   `json:"count"`
	Pulls  int64   `json:"pulls"`
	Images []Image `json:"images"`
	Error  string  `json:"error,omitempty"`
}

// imageListConcurrency bounds the artifact requests sent to the registry at
// once.
const imageListConcurrency = 4

// Registry is the project registry: the robot credentials and how to push.
type Registry struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"` // cr.example.com:7443/forge-foo
	// Credentials is false for view-only members, who cannot read the robot
	// Secret. They get the push address but cannot list or push images.
	Credentials bool   `json:"credentials"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	Secret      string `json:"secret"`
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
	err = c.k8s.Get(ctx, secKey, sec)
	switch {
	case isNotFound(err):
		return out, nil // registry not set up for this project
	case apierrors.IsForbidden(err):
		// The push address comes from the platform config, which members can read.
		cfg, err := platform.Load(ctx, c.k8s, c.namespace())
		if err != nil {
			return nil, err
		}
		if !cfg.Registry.Enabled() {
			return out, nil
		}
		out.Enabled = true
		out.URL = cfg.Registry.Domain + "/" + c.namespace()
		return out, nil
	case err != nil:
		return nil, err
	}
	out.Enabled = true
	out.Credentials = true
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
	if !reg.Enabled || !reg.Credentials {
		return []ImageRepo{}, nil
	}
	ctx, cancel := a.call()
	defer cancel()
	hc := harbor.New(harborAPIURL(reg), reg.Username, reg.Password, a.insecureRegistry())
	repos, err := hc.ListRepositories(ctx, harborProject(c))
	if err != nil {
		return nil, err
	}
	out := make([]ImageRepo, len(repos))
	var wg sync.WaitGroup
	sem := make(chan struct{}, imageListConcurrency)
	for i, r := range repos {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = repoImages(ctx, hc, harborProject(c), r)
		}()
	}
	wg.Wait()
	return out, nil
}

// repoImages lists the images of one repository. A repository whose artifacts
// cannot be listed keeps its counts and says why, so one failure does not hide
// the other repositories.
func repoImages(ctx context.Context, hc *harbor.Client, project string, r harbor.Repository) ImageRepo {
	ir := ImageRepo{Name: r.Name, Count: r.ArtifactCount, Pulls: r.PullCount}
	arts, err := hc.ListArtifacts(ctx, project, r.Name)
	if err != nil {
		ir.Error = err.Error()
		return ir
	}
	for _, art := range arts {
		ir.Images = append(ir.Images, Image{
			Repository: r.Name, Digest: art.Digest, Tags: art.TagNames(),
			SizeMiB: art.Size >> 20, PushedAt: art.PushTime, PulledAt: art.PullTime,
		})
	}
	return ir
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
	if err := registryWritable(reg); err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()
	hc := harbor.New(harborAPIURL(reg), reg.Username, reg.Password, a.insecureRegistry())
	return hc.DeleteArtifact(ctx, harborProject(c), repository, reference)
}

// DeleteImageTag removes one tag from an image. The image and its other tags
// stay; only DeleteImage removes the image.
func (a *App) DeleteImageTag(repository, digest, tag string) error {
	c, err := a.current()
	if err != nil {
		return err
	}
	reg, err := a.Registry()
	if err != nil {
		return err
	}
	if err := registryWritable(reg); err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()
	hc := harbor.New(harborAPIURL(reg), reg.Username, reg.Password, a.insecureRegistry())
	return hc.DeleteTag(ctx, harborProject(c), repository, digest, tag)
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
	if err := registryWritable(reg); err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()
	hc := harbor.New(harborAPIURL(reg), reg.Username, reg.Password, a.insecureRegistry())
	return hc.DeleteRepository(ctx, harborProject(c), repository)
}

var (
	errNoRegistry       = errors.New("registry is not configured for this project")
	errNoRegistryAccess = errors.New("your account cannot change images in this project")
)

// registryWritable reports why the registry cannot be changed with this account.
// Delete calls need the robot credentials as much as listing does.
func registryWritable(reg *Registry) error {
	if !reg.Enabled {
		return errNoRegistry
	}
	if !reg.Credentials {
		return errNoRegistryAccess
	}
	return nil
}

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

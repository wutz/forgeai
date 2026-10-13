package app

import (
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/kubeconfig"
	"github.com/scclabs/forgeai/pkg/platform"
)

// Project is the admin view of a Project.
type Project struct {
	Name           string          `json:"name"`
	DisplayName    string          `json:"displayName"`
	Namespace      string          `json:"namespace"`
	Quota          ProjectQuota    `json:"quota"`
	Network        ProjectNetwork  `json:"network"`
	Members        []ProjectMember `json:"members"`
	TrashRetention string          `json:"trashRetention,omitempty"`
	DeletionPolicy string          `json:"deletionPolicy"`
	Ready          bool            `json:"ready"`
	Registry       string          `json:"registry"`
	Created        string          `json:"created"`
}

// ProjectQuota is the quota form of a project. GPU classes map class->count.
type ProjectQuota struct {
	GPUs            map[string]int32 `json:"gpus,omitempty"`
	Storage         string           `json:"storage,omitempty"`
	Volumes         int32            `json:"volumes"`
	Pods            int32            `json:"pods"`
	RegistryStorage string           `json:"registryStorage,omitempty"`
}

// ProjectNetwork is the isolation form.
type ProjectNetwork struct {
	Isolation    string `json:"isolation"`
	DenyInternet bool   `json:"denyInternet"`
}

// ProjectMember is a member form entry.
type ProjectMember struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// ListProjects lists all projects (admin only; members get one).
func (a *App) ListProjects() ([]Project, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	list := &v1.ProjectList{}
	if err := c.k8s.List(ctx, list); err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, toProject(&list.Items[i]))
	}
	return out, nil
}

// GetProject returns one project.
func (a *App) GetProject(name string) (*Project, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	p := &v1.Project{}
	if err := c.k8s.Get(ctx, client.ObjectKey{Name: name}, p); err != nil {
		return nil, err
	}
	out := toProject(p)
	return &out, nil
}

// SaveProject creates or updates a project from the admin form.
func (a *App) SaveProject(f Project) error {
	c, err := a.current()
	if err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()

	p := &v1.Project{}
	create := false
	if err := c.k8s.Get(ctx, client.ObjectKey{Name: f.Name}, p); err != nil {
		p = &v1.Project{ObjectMeta: metav1.ObjectMeta{Name: f.Name}}
		create = true
	}
	p.Spec.DisplayName = f.DisplayName
	p.Spec.Network = v1.ProjectNetwork{
		Isolation: v1.NetworkIsolation(f.Network.Isolation), DenyInternet: f.Network.DenyInternet,
	}
	if f.DeletionPolicy != "" {
		p.Spec.DeletionPolicy = v1.DeletionPolicy(f.DeletionPolicy)
	}
	if f.TrashRetention != "" {
		if d, err := time.ParseDuration(f.TrashRetention); err == nil {
			p.Spec.TrashRetention = &metav1.Duration{Duration: d}
		}
	}
	if err := applyProjectQuota(&p.Spec.Quota, f.Quota); err != nil {
		return err
	}
	p.Spec.Members = p.Spec.Members[:0]
	for _, m := range f.Members {
		name := strings.TrimSpace(m.Name)
		if name == "" {
			continue
		}
		p.Spec.Members = append(p.Spec.Members, v1.Member{Name: name, Role: v1.MemberRole(m.Role)})
	}
	if create {
		return c.k8s.Create(ctx, p)
	}
	return c.k8s.Update(ctx, p)
}

func applyProjectQuota(q *v1.ProjectQuota, f ProjectQuota) error {
	q.GPUs = q.GPUs[:0]
	for class, count := range f.GPUs {
		q.GPUs = append(q.GPUs, v1.GPUQuota{Class: class, Count: count})
	}
	for _, name := range []struct {
		s   string
		set func(resource.Quantity)
	}{
		{f.Storage, func(v resource.Quantity) { q.Storage = &v }},
		{f.RegistryStorage, func(v resource.Quantity) { q.RegistryStorage = &v }},
	} {
		if name.s != "" {
			v, err := resource.ParseQuantity(name.s)
			if err != nil {
				return err
			}
			name.set(v)
		}
	}
	if f.Volumes > 0 {
		q.Volumes = &f.Volumes
	}
	if f.Pods > 0 {
		q.Pods = &f.Pods
	}
	return nil
}

// DeleteProject deletes a project (with its data on deletionPolicy Delete).
func (a *App) DeleteProject(name string) error {
	c, err := a.current()
	if err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()
	return c.k8s.Delete(ctx, &v1.Project{ObjectMeta: metav1.ObjectMeta{Name: name}})
}

// MemberKubeconfig exports the kubeconfig of a project member as YAML. The
// admin hands it to the user, who loads it in the client.
func (a *App) MemberKubeconfig(projectName, member string) (string, error) {
	c, err := a.current()
	if err != nil {
		return "", err
	}
	ctx, cancel := a.call()
	defer cancel()
	cfg, err := platform.Load(ctx, c.k8s, c.namespace())
	if err != nil {
		return "", err
	}
	// The token secret lives in the members namespace, out of reach of
	// project members; read it by name from the project status.
	p := &v1.Project{}
	if err := c.k8s.Get(ctx, client.ObjectKey{Name: projectName}, p); err != nil {
		return "", err
	}
	var st *v1.MemberStatus
	for i := range p.Status.Members {
		if p.Status.Members[i].Name == member {
			st = &p.Status.Members[i]
		}
	}
	if st == nil {
		return "", errMemberNotFound(member)
	}
	sec := &corev1.Secret{}
	if err := c.k8s.Get(ctx, types.NamespacedName{Namespace: st.Namespace, Name: st.TokenSecret}, sec); err != nil {
		return "", err
	}
	data, err := kubeconfig.FromTokenSecret(sec, kubeconfig.Params{
		Name:      projectName + "." + member,
		Server:    cfg.APIServer,
		Namespace: p.Status.Namespace,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func errMemberNotFound(name string) error {
	return &memberNotFoundErr{name}
}

type memberNotFoundErr struct{ name string }

func (e *memberNotFoundErr) Error() string { return "member " + e.name + " has no credentials yet" }

func toProject(p *v1.Project) Project {
	out := Project{
		Name: p.Name, DisplayName: p.Spec.DisplayName, Namespace: p.Status.Namespace,
		Network: ProjectNetwork{
			Isolation: string(p.Spec.Network.Isolation), DenyInternet: p.Spec.Network.DenyInternet,
		},
		DeletionPolicy: string(p.Spec.DeletionPolicy),
		Ready:          false, Registry: p.Status.Registry.URL,
		Created: p.CreationTimestamp.UTC().Format(time.RFC3339),
		Quota:   ProjectQuota{GPUs: map[string]int32{}},
	}
	for _, g := range p.Spec.Quota.GPUs {
		out.Quota.GPUs[g.Class] = g.Count
	}
	if p.Spec.Quota.Storage != nil {
		out.Quota.Storage = p.Spec.Quota.Storage.String()
	}
	if p.Spec.Quota.Volumes != nil {
		out.Quota.Volumes = *p.Spec.Quota.Volumes
	}
	if p.Spec.Quota.Pods != nil {
		out.Quota.Pods = *p.Spec.Quota.Pods
	}
	if y := p.Status.Registry.URL; y != "" {
		out.Registry = y
	}
	if p.Spec.TrashRetention != nil {
		out.TrashRetention = p.Spec.TrashRetention.Duration.String()
	}
	for _, m := range p.Spec.Members {
		out.Members = append(out.Members, ProjectMember{Name: m.Name, Role: string(m.Role)})
	}
	return out
}

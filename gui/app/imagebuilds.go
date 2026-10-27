package app

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/naming"
)

// Containers of the build pods the operator creates: the source container
// clones the repository, the builder runs BuildKit.
const (
	imageBuildSourceContainer  = "source"
	imageBuildBuilderContainer = "buildkit"
)

// Limits and defaults of an image build, the same as the ImageBuild CRD.
const (
	imageBuildMaxRepository = 200
	imageBuildMaxTags       = 8
	imageBuildMaxDockerfile = 65536
	imageBuildMaxName       = 40
	imageBuildMaxPath       = 255
	imageBuildMaxURL        = 512
	imageBuildDefaultRev    = "HEAD"
	imageBuildDefaultCtx    = "."
	imageBuildDefaultDocker = "Dockerfile"
)

var (
	imageRepositoryPattern = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$`)
	imageTagPattern        = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	gitURLPattern          = regexp.MustCompile(`^https://\S+$`)
	nonDNSLabelChars       = regexp.MustCompile(`[^a-z0-9]+`)
)

// ImageBuildRequest is a build the GUI asks for. Name is optional; a Dockerfile
// given inline overrides the one of the repository.
type ImageBuildRequest struct {
	Name       string         `json:"name"`
	Repository string         `json:"repository"`
	Tags       []string       `json:"tags"`
	Dockerfile string         `json:"dockerfile"`
	Git        *ImageBuildGit `json:"git,omitempty"`
}

// ImageBuildGit is the repository the build context is cloned from.
type ImageBuildGit struct {
	URL            string `json:"url"`
	Revision       string `json:"revision"`
	ContextPath    string `json:"contextPath"`
	DockerfilePath string `json:"dockerfilePath"`
}

// ImageBuild is one image build of the project, for the GUI list.
type ImageBuild struct {
	Name        string   `json:"name"`
	Repository  string   `json:"repository"`
	Tags        []string `json:"tags"`
	Source      string   `json:"source"`
	Phase       string   `json:"phase"`
	Message     string   `json:"message"`
	Images      []string `json:"images"`
	Digest      string   `json:"digest"`
	CreatedAt   string   `json:"createdAt"`
	StartedAt   string   `json:"startedAt"`
	CompletedAt string   `json:"completedAt"`
}

// ListImageBuilds lists the image builds of the project, newest first. A
// cluster without the ImageBuild CRD has none.
func (a *App) ListImageBuilds() ([]ImageBuild, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	list := &v1.ImageBuildList{}
	if err := c.k8s.List(ctx, list, client.InNamespace(c.namespace())); err != nil {
		if meta.IsNoMatchError(err) {
			return []ImageBuild{}, nil
		}
		return nil, err
	}
	items := list.Items
	sort.SliceStable(items, func(i, j int) bool {
		ti, tj := items[i].CreationTimestamp, items[j].CreationTimestamp
		if !ti.Equal(&tj) {
			return tj.Before(&ti)
		}
		return items[i].Name < items[j].Name
	})
	out := make([]ImageBuild, 0, len(items))
	for i := range items {
		out = append(out, imageBuildOf(&items[i]))
	}
	return out, nil
}

// CreateImageBuild creates an ImageBuild in the project namespace, for the
// operator to build and push. It returns the name of the build.
func (a *App) CreateImageBuild(req ImageBuildRequest) (string, error) {
	c, err := a.current()
	if err != nil {
		return "", err
	}
	req = normalizeImageBuildRequest(req)
	if err := validateImageBuildRequest(req); err != nil {
		return "", err
	}
	ctx, cancel := a.call()
	defer cancel()
	reg, err := a.Registry()
	if err != nil {
		return "", err
	}
	if err := registryWritable(reg); err != nil {
		return "", err
	}
	name := req.Name
	if name == "" {
		name, err = buildNameFor(req.Repository)
		if err != nil {
			return "", err
		}
	}
	ib := &v1.ImageBuild{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace()},
		Spec: v1.ImageBuildSpec{
			Source: imageBuildSource(req),
			Destination: v1.ImageBuildDestination{
				Repository: req.Repository,
				Tags:       imageBuildTags(req.Tags),
			},
		},
	}
	if err := c.k8s.Create(ctx, ib); err != nil {
		return "", err
	}
	return name, nil
}

// DeleteImageBuild deletes an ImageBuild. The operator removes its build pods
// and the Dockerfile ConfigMap with it; the pushed images stay in the registry.
func (a *App) DeleteImageBuild(name string) error {
	c, err := a.current()
	if err != nil {
		return err
	}
	if name == "" {
		return errors.New("image build name is required")
	}
	ctx, cancel := a.call()
	defer cancel()
	return c.k8s.Delete(ctx, &v1.ImageBuild{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace()}})
}

// ImageBuildLogs returns the last tailLines of the newest build pod of an
// image build. Its log is empty until a container of the pod has started.
func (a *App) ImageBuildLogs(name string, tailLines int64) (string, error) {
	c, err := a.current()
	if err != nil {
		return "", err
	}
	ctx, cancel := a.call()
	defer cancel()
	ns := naming.BuildNamespace(c.namespace())
	pods := &corev1.PodList{}
	if err := c.k8s.List(ctx, pods, client.InNamespace(ns), client.MatchingLabels{naming.LabelImageBuild: name}); err != nil {
		return "", err
	}
	if len(pods.Items) == 0 {
		return "", nil
	}
	pod := &pods.Items[0]
	for i := range pods.Items {
		if pods.Items[i].CreationTimestamp.After(pod.CreationTimestamp.Time) {
			pod = &pods.Items[i]
		}
	}
	container := imageBuildLogContainer(pod)
	if container == "" {
		return "", nil
	}
	if tailLines <= 0 {
		tailLines = 500
	}
	opts := &corev1.PodLogOptions{TailLines: &tailLines, Container: container}
	rc, err := c.core.CoreV1().Pods(ns).GetLogs(pod.Name, opts).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()
	return readTail(rc, logTailLimit)
}

// imageBuildLogContainer picks the container whose log shows the build now: the
// source container while it clones or after it failed, else the builder. It
// returns "" while the container to read has not started.
func imageBuildLogContainer(pod *corev1.Pod) string {
	for _, ic := range pod.Spec.InitContainers {
		if ic.Name != imageBuildSourceContainer {
			continue
		}
		st := containerStatus(pod.Status.InitContainerStatuses, ic.Name)
		if st == nil || st.State.Waiting != nil {
			return ""
		}
		if st.State.Terminated == nil || st.State.Terminated.ExitCode != 0 {
			return imageBuildSourceContainer
		}
	}
	st := containerStatus(pod.Status.ContainerStatuses, imageBuildBuilderContainer)
	if st == nil || st.State.Waiting != nil {
		return ""
	}
	return imageBuildBuilderContainer
}

func containerStatus(statuses []corev1.ContainerStatus, name string) *corev1.ContainerStatus {
	for i := range statuses {
		if statuses[i].Name == name {
			return &statuses[i]
		}
	}
	return nil
}

// imageBuildOf maps an ImageBuild to the GUI entry. The phase of a build the
// operator has not reconciled yet is Pending.
func imageBuildOf(ib *v1.ImageBuild) ImageBuild {
	phase := string(ib.Status.Phase)
	if phase == "" {
		phase = string(v1.ImageBuildPending)
	}
	source := "dockerfile"
	if ib.Spec.Source.Git != nil {
		source = "git"
	}
	tags := make([]string, 0, len(ib.Spec.Destination.Tags))
	for _, t := range ib.Spec.Destination.Tags {
		tags = append(tags, string(t))
	}
	images := append([]string{}, ib.Status.Images...)
	return ImageBuild{
		Name:        ib.Name,
		Repository:  ib.Spec.Destination.Repository,
		Tags:        tags,
		Source:      source,
		Phase:       phase,
		Message:     ib.Status.Message,
		Images:      images,
		Digest:      ib.Status.Digest,
		CreatedAt:   rfc3339(&ib.CreationTimestamp),
		StartedAt:   rfc3339(ib.Status.StartTime),
		CompletedAt: rfc3339(ib.Status.CompletionTime),
	}
}

func rfc3339(t *metav1.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// normalizeImageBuildRequest trims the input and fills the defaults the CRD
// sets, so validation and the spec see the same values.
func normalizeImageBuildRequest(req ImageBuildRequest) ImageBuildRequest {
	req.Name = strings.TrimSpace(req.Name)
	req.Repository = strings.Trim(strings.TrimSpace(req.Repository), "/")
	tags := make([]string, 0, len(req.Tags))
	for _, t := range req.Tags {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	req.Tags = tags
	if strings.TrimSpace(req.Dockerfile) == "" {
		req.Dockerfile = ""
	}
	if req.Git != nil {
		g := *req.Git
		g.URL = strings.TrimSpace(g.URL)
		g.Revision = strings.TrimSpace(g.Revision)
		g.ContextPath = strings.TrimSpace(g.ContextPath)
		g.DockerfilePath = strings.TrimSpace(g.DockerfilePath)
		if g.Revision == "" {
			g.Revision = imageBuildDefaultRev
		}
		if g.ContextPath == "" {
			g.ContextPath = imageBuildDefaultCtx
		}
		if g.DockerfilePath == "" {
			g.DockerfilePath = imageBuildDefaultDocker
		}
		req.Git = &g
	}
	return req
}

// validateImageBuildRequest checks a normalized request. Each error names the
// field it is about, so the GUI can show it next to the input.
func validateImageBuildRequest(req ImageBuildRequest) error {
	if req.Name != "" {
		if len(req.Name) > imageBuildMaxName {
			return fmt.Errorf("build name %q is longer than %d characters", req.Name, imageBuildMaxName)
		}
		if errs := validation.IsDNS1123Label(req.Name); len(errs) > 0 {
			return fmt.Errorf("build name %q: %s", req.Name, errs[0])
		}
	}
	if req.Repository == "" {
		return errors.New("repository is required")
	}
	if len(req.Repository) > imageBuildMaxRepository || !imageRepositoryPattern.MatchString(req.Repository) {
		return fmt.Errorf("repository %q must be lowercase path segments of letters, digits, '.', '_' or '-' (at most %d characters)", req.Repository, imageBuildMaxRepository)
	}
	if len(req.Tags) == 0 {
		return errors.New("at least one tag is required")
	}
	if len(req.Tags) > imageBuildMaxTags {
		return fmt.Errorf("at most %d tags can be pushed by one build", imageBuildMaxTags)
	}
	seen := map[string]bool{}
	for _, t := range req.Tags {
		if !imageTagPattern.MatchString(t) {
			return fmt.Errorf("tag %q must start with a letter, digit or '_' and contain only letters, digits, '_', '.' or '-' (at most 128 characters)", t)
		}
		if seen[t] {
			return fmt.Errorf("tag %q is listed twice", t)
		}
		seen[t] = true
	}
	if req.Git == nil && req.Dockerfile == "" {
		return errors.New("a Git repository or a Dockerfile is required")
	}
	if len(req.Dockerfile) > imageBuildMaxDockerfile {
		return fmt.Errorf("the Dockerfile is larger than %d bytes", imageBuildMaxDockerfile)
	}
	if g := req.Git; g != nil {
		if len(g.URL) > imageBuildMaxURL || !gitURLPattern.MatchString(g.URL) {
			return fmt.Errorf("git URL %q must be an https:// address without spaces", g.URL)
		}
		if len(g.Revision) > imageBuildMaxPath || strings.HasPrefix(g.Revision, "-") || strings.ContainsAny(g.Revision, " \t\r\n\x00") {
			return fmt.Errorf("git revision %q must be a branch, a tag or a commit", g.Revision)
		}
		if err := checkRelativePath("git context path", g.ContextPath); err != nil {
			return err
		}
		if err := checkRelativePath("Dockerfile path", g.DockerfilePath); err != nil {
			return err
		}
	}
	return nil
}

// checkRelativePath accepts a path inside the repository: relative, without
// ".." segments and without control characters.
func checkRelativePath(field, p string) error {
	if len(p) > imageBuildMaxPath {
		return fmt.Errorf("%s is longer than %d characters", field, imageBuildMaxPath)
	}
	if strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\x00\r\n") {
		return fmt.Errorf("%s %q must be a relative path inside the repository", field, p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("%s %q must not contain '..'", field, p)
		}
	}
	return nil
}

// imageBuildSource is the spec source of a normalized request.
func imageBuildSource(req ImageBuildRequest) v1.ImageBuildSource {
	src := v1.ImageBuildSource{Dockerfile: req.Dockerfile}
	if g := req.Git; g != nil {
		src.Git = &v1.GitSource{
			URL:            g.URL,
			Revision:       g.Revision,
			ContextPath:    g.ContextPath,
			DockerfilePath: g.DockerfilePath,
		}
	}
	return src
}

func imageBuildTags(tags []string) []v1.ImageBuildTag {
	out := make([]v1.ImageBuildTag, 0, len(tags))
	for _, t := range tags {
		out = append(out, v1.ImageBuildTag(t))
	}
	return out
}

// buildNameFor makes a name for a build that has none: the last segment of the
// repository, sanitized to a DNS label, and five random hex characters.
func buildNameFor(repository string) (string, error) {
	base := repository
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	base = strings.Trim(nonDNSLabelChars.ReplaceAllString(strings.ToLower(base), "-"), "-")
	if len(base) > 30 {
		base = strings.TrimRight(base[:30], "-")
	}
	if base == "" {
		base = "build"
	}
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate build name: %w", err)
	}
	return base + "-" + hex.EncodeToString(b[:])[:5], nil
}

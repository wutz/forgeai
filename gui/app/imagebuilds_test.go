package app

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/naming"
)

// validBuild is a request that passes validation, for tests that change one field.
func validBuild() ImageBuildRequest {
	return ImageBuildRequest{
		Repository: "team/app",
		Tags:       []string{"latest", "v1.2.3"},
		Dockerfile: "FROM alpine\n",
	}
}

func TestValidateImageBuildRequest(t *testing.T) {
	gitBuild := func(mut func(*ImageBuildGit)) ImageBuildRequest {
		g := &ImageBuildGit{URL: "https://github.com/example/app.git", Revision: "main", ContextPath: ".", DockerfilePath: "Dockerfile"}
		mut(g)
		r := validBuild()
		r.Dockerfile = ""
		r.Git = g
		return r
	}
	tests := []struct {
		name    string
		req     func() ImageBuildRequest
		wantErr string
	}{
		{"inline dockerfile", validBuild, ""},
		{"git only", func() ImageBuildRequest { r := gitBuild(func(*ImageBuildGit) {}); return r }, ""},
		{"no source", func() ImageBuildRequest { r := validBuild(); r.Dockerfile = ""; return r }, "a Git repository or a Dockerfile is required"},
		{"no repository", func() ImageBuildRequest { r := validBuild(); r.Repository = ""; return r }, "repository is required"},
		{"uppercase repository", func() ImageBuildRequest { r := validBuild(); r.Repository = "Team/App"; return r }, "repository"},
		{"repository with double slash", func() ImageBuildRequest { r := validBuild(); r.Repository = "a//b"; return r }, "repository"},
		{"no tags", func() ImageBuildRequest { r := validBuild(); r.Tags = nil; return r }, "at least one tag"},
		{"too many tags", func() ImageBuildRequest {
			r := validBuild()
			r.Tags = []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}
			return r
		}, "at most 8 tags"},
		{"duplicate tag", func() ImageBuildRequest { r := validBuild(); r.Tags = []string{"a", "a"}; return r }, "listed twice"},
		{"tag starting with dash", func() ImageBuildRequest { r := validBuild(); r.Tags = []string{"-a"}; return r }, "tag \"-a\""},
		{"tag with slash", func() ImageBuildRequest { r := validBuild(); r.Tags = []string{"a/b"}; return r }, "tag \"a/b\""},
		{"name too long", func() ImageBuildRequest { r := validBuild(); r.Name = strings.Repeat("a", 41); return r }, "longer than 40"},
		{"name not a DNS label", func() ImageBuildRequest { r := validBuild(); r.Name = "Bad_Name"; return r }, "build name"},
		{"valid name", func() ImageBuildRequest { r := validBuild(); r.Name = "app-build-1"; return r }, ""},
		{"dockerfile too large", func() ImageBuildRequest {
			r := validBuild()
			r.Dockerfile = strings.Repeat("a", 65537)
			return r
		}, "larger than 65536"},
		{"http git url", func() ImageBuildRequest {
			return gitBuild(func(g *ImageBuildGit) { g.URL = "http://example.com/app.git" })
		}, "git URL"},
		{"git url with space", func() ImageBuildRequest {
			return gitBuild(func(g *ImageBuildGit) { g.URL = "https://example.com/a b.git" })
		}, "git URL"},
		{"revision starting with dash", func() ImageBuildRequest {
			return gitBuild(func(g *ImageBuildGit) { g.Revision = "--upload-pack=x" })
		}, "git revision"},
		{"absolute context path", func() ImageBuildRequest {
			return gitBuild(func(g *ImageBuildGit) { g.ContextPath = "/etc" })
		}, "relative path"},
		{"dockerfile path escapes repo", func() ImageBuildRequest {
			return gitBuild(func(g *ImageBuildGit) { g.DockerfilePath = "docker/../../x/Dockerfile" })
		}, "'..'"},
		{"newline in dockerfile path", func() ImageBuildRequest {
			return gitBuild(func(g *ImageBuildGit) { g.DockerfilePath = "a\nb" })
		}, "relative path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateImageBuildRequest(normalizeImageBuildRequest(tt.req()))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestNormalizeImageBuildRequestDefaults checks the defaults the CRD also sets.
func TestNormalizeImageBuildRequestDefaults(t *testing.T) {
	r := normalizeImageBuildRequest(ImageBuildRequest{
		Repository: " /team/app/ ",
		Tags:       []string{" latest ", "", "  "},
		Git:        &ImageBuildGit{URL: "https://example.com/app.git"},
	})
	if r.Repository != "team/app" {
		t.Errorf("repository = %q", r.Repository)
	}
	if len(r.Tags) != 1 || r.Tags[0] != "latest" {
		t.Errorf("tags = %q", r.Tags)
	}
	if r.Git.Revision != "HEAD" || r.Git.ContextPath != "." || r.Git.DockerfilePath != "Dockerfile" {
		t.Errorf("git defaults = %+v", r.Git)
	}
}

var suffixPattern = regexp.MustCompile(`-[0-9a-f]{5}$`)

// TestBuildNameFor checks that generated names are DNS labels of at most 40
// characters and keep the readable part of the repository.
func TestBuildNameFor(t *testing.T) {
	for _, repo := range []string{
		"team/app",
		"team/My_App.v2",
		"a/" + strings.Repeat("x", 80),
		"!!!",
		"",
	} {
		name, err := buildNameFor(repo)
		if err != nil {
			t.Fatal(err)
		}
		if len(name) > imageBuildMaxName {
			t.Errorf("%q: name %q is longer than %d", repo, name, imageBuildMaxName)
		}
		if errs := validation.IsDNS1123Label(name); len(errs) > 0 {
			t.Errorf("%q: name %q is not a DNS label: %v", repo, name, errs)
		}
		if !suffixPattern.MatchString(name) {
			t.Errorf("%q: name %q lacks the random suffix", repo, name)
		}
	}
	name, _ := buildNameFor("team/My_App.v2")
	if !strings.HasPrefix(name, "my-app-v2-") {
		t.Errorf("name = %q, want the sanitized repository as prefix", name)
	}
	a, _ := buildNameFor("team/app")
	b, _ := buildNameFor("team/app")
	if a == b {
		t.Errorf("names %q and %q are equal; the suffix must be random", a, b)
	}
}

// registrySecret is the project robot Secret, which makes the registry writable.
func registrySecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: naming.RegistrySecretName, Namespace: testNamespace},
		Data: map[string][]byte{
			"server":   []byte("cr.example.com:7443"),
			"username": []byte("robot$forge"),
			"password": []byte("secret"),
		},
	}
}

func buildOf(name string, created time.Time, phase v1.ImageBuildPhase) *v1.ImageBuild {
	ts := metav1.NewTime(created)
	return &v1.ImageBuild{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace, CreationTimestamp: ts},
		Spec: v1.ImageBuildSpec{
			Source:      v1.ImageBuildSource{Dockerfile: "FROM alpine\n"},
			Destination: v1.ImageBuildDestination{Repository: "team/" + name, Tags: []v1.ImageBuildTag{"latest"}},
		},
		Status: v1.ImageBuildStatus{Phase: phase},
	}
}

// TestListImageBuildsNewestFirst checks the mapping of each build and the order.
func TestListImageBuildsNewestFirst(t *testing.T) {
	base := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	old := buildOf("old", base, v1.ImageBuildSucceeded)
	old.Status.Images = []string{"cr.example.com:7443/forge-team/team/old:latest"}
	old.Status.Digest = "sha256:abc"
	started := metav1.NewTime(base.Add(time.Minute))
	done := metav1.NewTime(base.Add(2 * time.Minute))
	old.Status.StartTime = &started
	old.Status.CompletionTime = &done
	newer := buildOf("new", base.Add(time.Hour), v1.ImageBuildFailed)
	newer.Status.Message = "push failed"
	newer.Spec.Source = v1.ImageBuildSource{Git: &v1.GitSource{URL: "https://example.com/app.git"}}
	pending := buildOf("pending", base.Add(-time.Hour), "")
	pending.Spec.Destination.Tags = []v1.ImageBuildTag{"a", "b"}

	k8s := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(old, newer, pending).Build()
	a := &App{ctx: context.Background(), conn: &Connection{Info: ConnInfo{Namespace: testNamespace}, k8s: k8s}}

	got, err := a.ListImageBuilds()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Name != "new" || got[1].Name != "old" || got[2].Name != "pending" {
		t.Fatalf("order = %+v, want new, old, pending", got)
	}
	if n := got[0]; n.Source != "git" || n.Phase != "Failed" || n.Message != "push failed" || n.CreatedAt != "2026-10-27T09:00:00Z" {
		t.Errorf("failed git build = %+v", n)
	}
	if o := got[1]; o.Source != "dockerfile" || o.Phase != "Succeeded" || o.Digest != "sha256:abc" ||
		o.StartedAt != "2026-10-27T08:01:00Z" || o.CompletedAt != "2026-10-27T08:02:00Z" ||
		len(o.Images) != 1 || o.Repository != "team/old" || len(o.Tags) != 1 {
		t.Errorf("succeeded build = %+v", o)
	}
	if p := got[2]; p.Phase != "Pending" || len(p.Tags) != 2 || p.StartedAt != "" || p.CompletedAt != "" || p.Images == nil {
		t.Errorf("unreconciled build = %+v, want Pending with empty times and a non-nil images list", p)
	}
}

// TestListImageBuildsEmpty checks that no builds is an empty, non-nil list.
func TestListImageBuildsEmpty(t *testing.T) {
	k8s := fake.NewClientBuilder().WithScheme(scheme()).Build()
	a := &App{ctx: context.Background(), conn: &Connection{Info: ConnInfo{Namespace: testNamespace}, k8s: k8s}}
	got, err := a.ListImageBuilds()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want an empty list", got)
	}
}

// TestImageBuildsAvailable checks that the Builds view knows whether the
// ImageBuild CRD is installed. A user without the grant still sees the view.
func TestImageBuildsAvailable(t *testing.T) {
	gk := v1.GroupVersion.WithKind("ImageBuild").GroupKind()
	cases := map[string]struct {
		err  error
		want bool
	}{
		"installed":     {err: nil, want: true},
		"crd missing":   {err: &meta.NoKindMatchError{GroupKind: gk, SearchedVersions: []string{v1.GroupVersion.Version}}, want: false},
		"not permitted": {err: apierrors.NewForbidden(schema.GroupResource{Group: v1.GroupVersion.Group, Resource: "imagebuilds"}, "", nil), want: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			k8s := fake.NewClientBuilder().WithScheme(scheme()).WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*v1.ImageBuildList); ok && tc.err != nil {
						return tc.err
					}
					return c.List(ctx, list, opts...)
				},
			}).Build()
			if got := imageBuildsAvailable(context.Background(), k8s, testNamespace); got != tc.want {
				t.Fatalf("imageBuildsAvailable() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCreateImageBuildWritesSpec checks the object the GUI creates.
func TestCreateImageBuildWritesSpec(t *testing.T) {
	k8s := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(registrySecret()).Build()
	a := &App{ctx: context.Background(), conn: &Connection{Info: ConnInfo{Namespace: testNamespace}, k8s: k8s}}

	req := ImageBuildRequest{
		Name:       "app-1",
		Repository: "team/app",
		Tags:       []string{"latest", "v1"},
		Git:        &ImageBuildGit{URL: "https://github.com/example/app.git"},
	}
	name, err := a.CreateImageBuild(req)
	if err != nil {
		t.Fatal(err)
	}
	if name != "app-1" {
		t.Fatalf("name = %q", name)
	}
	ib := &v1.ImageBuild{}
	if err := k8s.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: name}, ib); err != nil {
		t.Fatal(err)
	}
	g := ib.Spec.Source.Git
	if g == nil || g.URL != "https://github.com/example/app.git" || g.Revision != "HEAD" || g.ContextPath != "." || g.DockerfilePath != "Dockerfile" {
		t.Errorf("git source = %+v", g)
	}
	if ib.Spec.Source.Dockerfile != "" {
		t.Errorf("dockerfile = %q, want empty for a repository build", ib.Spec.Source.Dockerfile)
	}
	if ib.Spec.Destination.Repository != "team/app" || len(ib.Spec.Destination.Tags) != 2 || ib.Spec.Destination.Tags[1] != "v1" {
		t.Errorf("destination = %+v", ib.Spec.Destination)
	}
}

// TestCreateImageBuildGeneratesName checks the name when the request has none.
func TestCreateImageBuildGeneratesName(t *testing.T) {
	k8s := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(registrySecret()).Build()
	a := &App{ctx: context.Background(), conn: &Connection{Info: ConnInfo{Namespace: testNamespace}, k8s: k8s}}
	name, err := a.CreateImageBuild(validBuild())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "app-") || len(name) > imageBuildMaxName {
		t.Fatalf("name = %q", name)
	}
	ib := &v1.ImageBuild{}
	if err := k8s.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: name}, ib); err != nil {
		t.Fatal(err)
	}
	if ib.Spec.Source.Dockerfile != "FROM alpine\n" {
		t.Errorf("dockerfile = %q", ib.Spec.Source.Dockerfile)
	}
}

// TestCreateImageBuildNeedsRegistry checks that a project without a registry
// cannot create builds, and that a bad request is refused before the registry is read.
func TestCreateImageBuildNeedsRegistry(t *testing.T) {
	k8s := fake.NewClientBuilder().WithScheme(scheme()).Build()
	a := &App{ctx: context.Background(), conn: &Connection{Info: ConnInfo{Namespace: testNamespace}, k8s: k8s}}
	if _, err := a.CreateImageBuild(validBuild()); err != errNoRegistry {
		t.Fatalf("error = %v, want %v", err, errNoRegistry)
	}
	bad := validBuild()
	bad.Tags = nil
	if _, err := a.CreateImageBuild(bad); err == nil || !strings.Contains(err.Error(), "at least one tag") {
		t.Fatalf("error = %v, want the tag error", err)
	}
}

// TestDeleteImageBuild checks that the build object is removed.
func TestDeleteImageBuild(t *testing.T) {
	k8s := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(buildOf("app-1", time.Now(), v1.ImageBuildSucceeded)).Build()
	a := &App{ctx: context.Background(), conn: &Connection{Info: ConnInfo{Namespace: testNamespace}, k8s: k8s}}
	if err := a.DeleteImageBuild("app-1"); err != nil {
		t.Fatal(err)
	}
	list := &v1.ImageBuildList{}
	if err := k8s.List(context.Background(), list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("items after delete = %d", len(list.Items))
	}
	if err := a.DeleteImageBuild(""); err == nil {
		t.Fatal("empty name must be refused")
	}
}

// TestImageBuildLogs reads the builder log of the newest build pod in the build namespace.
func TestImageBuildLogs(t *testing.T) {
	buildNS := naming.BuildNamespace(testNamespace)
	labels := map[string]string{naming.LabelImageBuild: "app-1"}
	old := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "app-1-old", Namespace: buildNS, Labels: labels,
		CreationTimestamp: metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))}}
	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "app-1-new", Namespace: buildNS, Labels: labels,
			CreationTimestamp: metav1.NewTime(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))},
		Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: imageBuildSourceContainer}}, Containers: []corev1.Container{{Name: imageBuildBuilderContainer}}},
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{{Name: imageBuildSourceContainer, State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}}},
			ContainerStatuses:     []corev1.ContainerStatus{{Name: imageBuildBuilderContainer, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
		},
	}
	k8s := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(old, newPod).Build()
	core := kubefake.NewSimpleClientset(old, newPod)
	a := &App{ctx: context.Background(), conn: &Connection{Info: ConnInfo{Namespace: testNamespace}, k8s: k8s, core: core}}

	got, err := a.ImageBuildLogs("app-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != "fake logs" {
		t.Fatalf("logs = %q", got)
	}
	none, err := a.ImageBuildLogs("missing", 0)
	if err != nil || none != "" {
		t.Fatalf("logs of a build without pods = %q, %v; want empty", none, err)
	}
}

// TestImageBuildLogContainer checks which container's log is read at each stage.
func TestImageBuildLogContainer(t *testing.T) {
	src := func(st corev1.ContainerState) corev1.ContainerStatus {
		return corev1.ContainerStatus{Name: imageBuildSourceContainer, State: st}
	}
	builder := func(st corev1.ContainerState) corev1.ContainerStatus {
		return corev1.ContainerStatus{Name: imageBuildBuilderContainer, State: st}
	}
	running := corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	waiting := corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"}}
	ok := corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}
	failed := corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}
	withSource := corev1.PodSpec{InitContainers: []corev1.Container{{Name: imageBuildSourceContainer}}}

	tests := []struct {
		name string
		pod  corev1.Pod
		want string
	}{
		{"cloning", corev1.Pod{Spec: withSource, Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{src(running)}}}, imageBuildSourceContainer},
		{"clone failed", corev1.Pod{Spec: withSource, Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{src(failed)}}}, imageBuildSourceContainer},
		{"source not started", corev1.Pod{Spec: withSource, Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{src(waiting)}}}, ""},
		{"no status yet", corev1.Pod{Spec: withSource}, ""},
		{"builder running", corev1.Pod{Spec: withSource, Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{src(ok)},
			ContainerStatuses:     []corev1.ContainerStatus{builder(running)}}}, imageBuildBuilderContainer},
		{"builder pulling", corev1.Pod{Spec: withSource, Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{src(ok)},
			ContainerStatuses:     []corev1.ContainerStatus{builder(waiting)}}}, ""},
		{"dockerfile only", corev1.Pod{Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{builder(running)}}}, imageBuildBuilderContainer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := imageBuildLogContainer(&tt.pod); got != tt.want {
				t.Fatalf("container = %q, want %q", got, tt.want)
			}
		})
	}
}

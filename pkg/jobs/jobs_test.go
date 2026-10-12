package jobs

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
)

func newClient(t *testing.T, objs ...runtime.Object) *Client {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v1.AddToScheme(s)
	k := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).WithStatusSubresource(&v1.TrainingJob{}).Build()
	return &Client{K8s: k, Namespace: "pod-prod", ConfigNamespace: "forgeai-system"}
}

func spec() v1.TrainingJobSpec {
	return v1.TrainingJobSpec{UserID: "u1", JobID: "ackjob-1", Image: "img:1", ResourceID: "g1", Workers: 1}
}

func TestApply(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	job, err := c.Apply(ctx, spec())
	if err != nil {
		t.Fatal(err)
	}
	if job.Labels[naming.LabelUserID] != "u1" || job.Name != naming.JobName("pod-prod", "u1", "ackjob-1") {
		t.Fatalf("job=%s labels=%v", job.Name, job.Labels)
	}

	// Same spec: no restart.
	if job, err = c.Apply(ctx, spec()); err != nil || job.Spec.RestartGeneration != 0 {
		t.Fatalf("restartGeneration=%d err=%v", job.Spec.RestartGeneration, err)
	}
	// Stopped job re-created with same spec: resumed, not restarted.
	if _, err = c.Stop(ctx, job.Name); err != nil {
		t.Fatal(err)
	}
	if job, _ = c.Apply(ctx, spec()); job.Spec.Suspend || job.Spec.RestartGeneration != 0 {
		t.Fatalf("suspend=%v gen=%d", job.Spec.Suspend, job.Spec.RestartGeneration)
	}
	// Changed spec: restart.
	s := spec()
	s.Image = "img:2"
	if job, _ = c.Apply(ctx, s); job.Spec.RestartGeneration != 1 || job.Spec.Image != "img:2" {
		t.Fatalf("gen=%d image=%s", job.Spec.RestartGeneration, job.Spec.Image)
	}

	list, err := c.List(ctx, "u1", "")
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%d err=%v", len(list), err)
	}
	if list, _ = c.List(ctx, "u2", ""); len(list) != 0 {
		t.Fatalf("other user sees %d jobs", len(list))
	}
}

func TestStartFinished(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	job, _ := c.Apply(ctx, spec())
	job.Status.Phase = v1.PhaseSucceeded
	if err := c.K8s.Status().Update(ctx, job); err != nil {
		t.Fatal(err)
	}
	if job, _ = c.Start(ctx, job.Name); job.Spec.RestartGeneration != 1 {
		t.Fatalf("finished job not re-run, gen=%d", job.Spec.RestartGeneration)
	}
	if _, err := c.Delete(ctx, job.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stop(ctx, job.Name); err != ErrNotFound {
		t.Fatalf("err=%v", err)
	}
}

func TestSSH(t *testing.T) {
	ctx := context.Background()
	name := naming.JobName("pod-prod", "u1", "ackjob-1")
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "sshpiper", Namespace: "pod-prod"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 2222}}},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}},
		}},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: naming.SecretName(name), Namespace: "pod-prod"},
		Data:       map[string][]byte{"SSH_PASSWORD": []byte("pw")},
	}
	c := newClient(t, svc, secret)
	cfg := &platform.Config{
		RootDomain: "dev1.example.com", ExtraDomains: []string{"dev1.example.cn"},
		SSH: platform.SSHConfig{PiperService: "pod-prod/sshpiper", Domains: map[string][]string{
			"10.0.0.1": {"ssh.dev1.example.com", "ssh.dev1.example.cn"},
		}},
	}
	job := &v1.TrainingJob{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "pod-prod"}}
	job.Status.Workers = []v1.WorkerStatus{
		{Index: 0, Phase: "Running", SSHUser: "root@ackjob-1-0"},
		{Index: 1, Phase: "Pending", SSHUser: "root@ackjob-1-1"},
	}
	conns, err := c.SSH(ctx, job, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := SSHConnection{
		ID: "worker-0", URL: "ssh://root@ackjob-1-0@ssh.dev1.example.com:2222",
		RefactorURL: "ssh://root@ackjob-1-0@ssh.dev1.example.cn:2222", Password: "pw",
	}
	if len(conns) != 1 || conns[0] != want {
		t.Fatalf("conns=%+v", conns)
	}
}

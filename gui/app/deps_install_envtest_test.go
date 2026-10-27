package app

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/wutz/forgeai/pkg/platform"
)

// Opt-in: needs a kube-apiserver and etcd in bin/k8s (FORGEAI_ENVTEST=1).
func TestInstallDependencyEnvtest(t *testing.T) {
	if os.Getenv("FORGEAI_ENVTEST") == "" {
		t.Skip("set FORGEAI_ENVTEST=1")
	}
	env := &envtest.Environment{
		BinaryAssetsDirectory: "../../bin/k8s/1.37.0-darwin-arm64",
		CRDDirectoryPaths:     crdDirs(t),
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("envtest start: %v", err)
	}
	defer func() { _ = env.Stop() }()

	ctx := context.Background()
	c, err := client.New(cfg, client.Options{Scheme: scheme()})
	if err != nil {
		t.Fatal(err)
	}
	for _, ns := range []string{platform.Namespace(), "forge-demo"} {
		if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
			t.Fatal(err)
		}
		if err := c.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "forgeai-platform"},
			Data:       map[string]string{"config.yaml": testPlatformConfig},
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Stand-in for the Deployment controller: envtest runs no kubelet, so
	// Deployments are marked Available once they exist.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(200 * time.Millisecond):
			}
			ds := &appsv1.DeploymentList{}
			if err := c.List(ctx, ds); err != nil {
				continue
			}
			for i := range ds.Items {
				d := &ds.Items[i]
				if deploymentAvailable(d) {
					continue
				}
				d.Status.ObservedGeneration = d.Generation
				d.Status.Replicas = 1
				d.Status.ReadyReplicas = 1
				d.Status.AvailableReplicas = 1
				d.Status.Conditions = []appsv1.DeploymentCondition{{
					Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue, Reason: "MinimumReplicasAvailable",
				}}
				_ = c.Status().Update(ctx, d)
			}
		}
	}()

	events := make(chan DependencyProgress, 256)
	a := &App{
		ctx:  ctx,
		conn: &Connection{Info: ConnInfo{Namespace: "forge-demo", ClusterAdmin: true, Context: "envtest"}, k8s: c, rest: cfg},
		emit: func(_ context.Context, name string, data ...interface{}) {
			if p, ok := data[0].(DependencyProgress); ok && name == depProgressEvent {
				events <- p
			}
		},
	}
	if err := a.InstallDependency("kueue"); err != nil {
		t.Fatalf("InstallDependency: %v", err)
	}
	if err := waitInstallDone(events, 10*time.Minute, t); err != nil {
		t.Fatal(err)
	}
	recs, err := readRecords(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := recs["kueue"]
	if !ok || rec.AppliedAt == "" {
		t.Fatalf("no install record for kueue: %+v", recs)
	}
	t.Logf("record: version=%s namespace=%s appliedBy=%q appliedAt=%s", rec.Version, rec.Namespace, rec.AppliedBy, rec.AppliedAt)

	// A repeat install is refused only while one is running, so it must succeed.
	if err := a.InstallDependency("kueue"); err != nil {
		t.Fatalf("second InstallDependency: %v", err)
	}
	if err := waitInstallDone(events, 10*time.Minute, t); err != nil {
		t.Fatalf("second install: %v", err)
	}
}

// waitInstallDone reads progress until the install reports its outcome.
func waitInstallDone(events <-chan DependencyProgress, limit time.Duration, t *testing.T) error {
	deadline := time.After(limit)
	for {
		select {
		case p := <-events:
			t.Logf("progress step=%q done=%v error=%q", p.Step, p.Done, p.Error)
			if p.Done {
				if p.Error != "" {
					return fmt.Errorf("install failed: %s", p.Error)
				}
				return nil
			}
		case <-deadline:
			return fmt.Errorf("timed out waiting for the install outcome")
		}
	}
}

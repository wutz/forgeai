package app

import (
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/sandbox"
	"github.com/wutz/forgeai/pkg/training"
	"github.com/wutz/forgeai/pkg/volumes"
	projectv1 "github.com/wutz/project/api/v1alpha1"
)

// mountFixtures returns a platform config, a bound volume that is shared, a
// bound ReadWriteOnce volume and a volume in the recycle bin.
func mountFixtures() []client.Object {
	pvc := func(name string, mode corev1.PersistentVolumeAccessMode, trashed bool) *corev1.PersistentVolumeClaim {
		p := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec:       corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{mode}},
			Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
		}
		if trashed {
			p.Labels = map[string]string{naming.LabelFileServerIgnore: "true"}
		}
		return p
	}
	return []client.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: platform.ConfigMapName, Namespace: testNamespace},
			Data:       map[string]string{platform.ConfigKey: ""},
		},
		&projectv1.InstanceType{ObjectMeta: metav1.ObjectMeta{Name: "cpu.small"}},
		pvc("shared", corev1.ReadWriteMany, false),
		pvc("local", corev1.ReadWriteOnce, false),
		pvc("old", corev1.ReadWriteMany, true),
	}
}

func TestCreateSandboxChecksMountedVolumes(t *testing.T) {
	a := testApp(t, mountFixtures()...)

	mount := func(name string) []sandbox.VolumeMount {
		return []sandbox.VolumeMount{{Volume: name, MountPath: "/data"}}
	}
	if _, err := a.CreateSandbox(sandbox.Form{Name: "agent", Image: "img", Volumes: mount("old")}); !errors.Is(err, volumes.ErrInTrash) {
		t.Fatalf("trashed volume: got %v, want ErrInTrash", err)
	}
	if _, err := a.CreateSandbox(sandbox.Form{Name: "agent", Image: "img", Volumes: mount("nope")}); !errors.Is(err, volumes.ErrNotFound) {
		t.Fatalf("missing volume: got %v, want ErrNotFound", err)
	}
}

func TestCreateJobChecksMountedVolumes(t *testing.T) {
	a := testApp(t, mountFixtures()...)

	job := func(workers int32, name string) training.Form {
		return training.Form{
			Name: "train", Image: "img", InstanceType: "cpu.small", Workers: workers,
			Volumes: []training.VolumeMount{{Volume: name, MountPath: "/data"}},
		}
	}
	if _, err := a.CreateJob(job(1, "old")); !errors.Is(err, volumes.ErrInTrash) {
		t.Fatalf("trashed volume: got %v, want ErrInTrash", err)
	}
	if _, err := a.CreateJob(job(2, "local")); !errors.Is(err, volumes.ErrNotShared) {
		t.Fatalf("ReadWriteOnce volume on two workers: got %v, want ErrNotShared", err)
	}
}

// A job keeps its mount paths, so saving it as a template does not drop them.
func TestJobMountsKeepPathsForTemplates(t *testing.T) {
	a := testApp(t, mountFixtures()...)
	want := []training.VolumeMount{
		{Volume: "shared", MountPath: "/data", SubPath: "run1", ReadOnly: true},
		{Volume: "shared", MountPath: "/cache"},
	}
	if _, err := a.CreateJob(training.Form{
		Name: "train", Image: "img", InstanceType: "cpu.small", Workers: 2, Volumes: want,
	}); err != nil {
		t.Fatal(err)
	}
	j, err := a.GetJob("train")
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Mounts) != len(want) {
		t.Fatalf("mounts = %+v, want %+v", j.Mounts, want)
	}
	for i := range want {
		if j.Mounts[i] != want[i] {
			t.Errorf("mount %d = %+v, want %+v", i, j.Mounts[i], want[i])
		}
	}
}

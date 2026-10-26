package app

import (
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
)

// The admin list shows Ready only once the controller reports the Ready condition.
func TestToProjectReadyFollowsCondition(t *testing.T) {
	p := &v1.Project{}
	if toProject(p).Ready {
		t.Error("Ready = true without a Ready condition")
	}
	p.Status.Conditions = []metav1.Condition{{Type: v1.ConditionReady, Status: metav1.ConditionFalse, Reason: "ReconcileError"}}
	if toProject(p).Ready {
		t.Error("Ready = true with Ready=False")
	}
	p.Status.Conditions[0].Status = metav1.ConditionTrue
	if !toProject(p).Ready {
		t.Error("Ready = false with Ready=True")
	}
}

// A Project whose finalizer is still running stays listed, and the list must say so.
func TestToProjectReportsDeleting(t *testing.T) {
	p := &v1.Project{}
	if toProject(p).Deleting {
		t.Error("Deleting = true without a deletion timestamp")
	}
	now := metav1.Now()
	p.DeletionTimestamp = &now
	if !toProject(p).Deleting {
		t.Error("Deleting = false with a deletion timestamp")
	}
}

// Clearing a limit in the form must clear it on the Project, so the platform
// default applies again instead of the old value silently staying in place.
func TestApplyProjectQuotaClearsEmptyLimits(t *testing.T) {
	storage := resource.MustParse("1Ti")
	volumes, pods := int32(10), int32(20)
	q := &v1.ProjectQuota{Storage: &storage, RegistryStorage: &storage, Volumes: &volumes, Pods: &pods}

	if err := applyProjectQuota(q, ProjectQuota{GPUs: map[string]int32{"rtx-5090": 4}}); err != nil {
		t.Fatal(err)
	}
	if q.Storage != nil || q.RegistryStorage != nil || q.Volumes != nil || q.Pods != nil {
		t.Errorf("empty limits kept old values: %+v", q)
	}
	if len(q.GPUs) != 1 || q.GPUs[0].Class != "rtx-5090" || q.GPUs[0].Count != 4 {
		t.Errorf("GPUs = %+v", q.GPUs)
	}
}

func TestApplyProjectQuotaRejectsBadStorage(t *testing.T) {
	q := &v1.ProjectQuota{}
	if err := applyProjectQuota(q, ProjectQuota{Storage: "lots"}); err == nil {
		t.Error("expected parse error for storage")
	}
}

// The edit form shows what the Project holds, and saving it unchanged keeps
// every limit. Registry storage was dropped on the way to the form, so saving
// an untouched project cleared it.
func TestToProjectKeepsRegistryStorageThroughSave(t *testing.T) {
	registry := resource.MustParse("200Gi")
	storage := resource.MustParse("1Ti")
	p := &v1.Project{}
	p.Spec.Quota = v1.ProjectQuota{Storage: &storage, RegistryStorage: &registry}

	form := toProject(p).Quota
	if form.RegistryStorage != "200Gi" {
		t.Fatalf("form RegistryStorage = %q, want 200Gi", form.RegistryStorage)
	}
	if err := applyProjectQuota(&p.Spec.Quota, form); err != nil {
		t.Fatal(err)
	}
	if p.Spec.Quota.RegistryStorage == nil || p.Spec.Quota.RegistryStorage.Cmp(registry) != 0 {
		t.Errorf("saving the form changed RegistryStorage to %v", p.Spec.Quota.RegistryStorage)
	}
}

// The GPU list is written in class order, so saving the same form twice leaves
// the Project unchanged instead of reordering its list.
func TestApplyProjectQuotaSortsGPUClasses(t *testing.T) {
	gpus := map[string]int32{"rtx-5090": 1, "a100": 2, "h100": 3, "l4": 4}
	for range 20 {
		q := &v1.ProjectQuota{}
		if err := applyProjectQuota(q, ProjectQuota{GPUs: gpus}); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, g := range q.GPUs {
			got = append(got, g.Class)
		}
		if want := []string{"a100", "h100", "l4", "rtx-5090"}; !slices.Equal(got, want) {
			t.Fatalf("GPU classes = %v, want %v", got, want)
		}
	}
}

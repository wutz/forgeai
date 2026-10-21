package app

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
)

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

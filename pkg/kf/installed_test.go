package kf

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestInstalled(t *testing.T) {
	known := meta.NewDefaultRESTMapper([]schema.GroupVersion{KueueGroupVersion})
	known.Add(WorkloadGVK, meta.RESTScopeNamespace)

	tests := []struct {
		name   string
		mapper meta.RESTMapper
		want   bool
	}{
		{"served", known, true},
		{"not served", meta.NewDefaultRESTMapper(nil), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Installed(tt.mapper, WorkloadGVK)
			if err != nil {
				t.Fatalf("Installed() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Installed() = %v, want %v", got, tt.want)
			}
		})
	}
}

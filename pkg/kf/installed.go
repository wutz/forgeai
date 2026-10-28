package kf

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Installed reports whether the cluster serves the kind of gvk, asking the
// RESTMapper that knows its resources. Kueue is optional, so the TrainJob
// controller checks it before it depends on Kueue's Workloads.
func Installed(mapper meta.RESTMapper, gvk schema.GroupVersionKind) (bool, error) {
	_, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if meta.IsNoMatchError(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

package app

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scclabs/forgeai/pkg/kf"
	"github.com/scclabs/forgeai/pkg/naming"
)

// Trainer v2 Alpha1 objects are handled the same way as in the operator:
// unstructured on the wire, typed (kf.*) in memory. This file keeps the
// conversion in one place.

// listTrainJobs lists the TrainJobs of a namespace, newest first is applied
// by callers.
func listTrainJobs(ctx context.Context, c *Connection) ([]kf.TrainJob, error) {
	u := &unstructured.UnstructuredList{}
	u.SetGroupVersionKind(kf.TrainJobGVK.GroupVersion().WithKind("TrainJobList"))
	if err := c.k8s.List(ctx, u, client.InNamespace(c.namespace())); err != nil {
		return nil, err
	}
	return decodeTrainJobs(u.Items)
}

func decodeTrainJobs(items []unstructured.Unstructured) ([]kf.TrainJob, error) {
	out := make([]kf.TrainJob, 0, len(items))
	for i := range items {
		tj, err := decodeTrainJob(&items[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *tj)
	}
	return out, nil
}

func decodeTrainJob(u *unstructured.Unstructured) (*kf.TrainJob, error) {
	tj := &kf.TrainJob{}
	if err := kf.FromUnstructured(u.Object, tj); err != nil {
		return nil, err
	}
	return tj, nil
}

// trainJobObject turns a rendered TrainJob into an unstructured create call.
func trainJobObject(tj *kf.TrainJob) (*unstructured.Unstructured, error) {
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tj)
	if err != nil {
		return nil, err
	}
	u := &unstructured.Unstructured{Object: m}
	u.SetGroupVersionKind(kf.TrainJobGVK)
	unstructured.RemoveNestedField(u.Object, naming.KeyMetadata, "creationTimestamp")
	// creationTimestamp and status are never set on create.
	unstructured.RemoveNestedField(u.Object, "status")
	return u, nil
}

// encodeTrainJob converts a typed TrainJob back for update calls.
func encodeTrainJob(tj *kf.TrainJob) (*unstructured.Unstructured, error) {
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tj)
	if err != nil {
		return nil, err
	}
	u := &unstructured.Unstructured{Object: m}
	u.SetGroupVersionKind(kf.TrainJobGVK)
	return u, nil
}

// getTrainJob fetches one TrainJob by name.
func getTrainJob(ctx context.Context, c *Connection, name string) (*kf.TrainJob, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(kf.TrainJobGVK)
	if err := c.k8s.Get(ctx, client.ObjectKey{Namespace: c.namespace(), Name: name}, u); err != nil {
		return nil, err
	}
	return decodeTrainJob(u)
}

// listTrainingRuntimes lists the job templates (TrainingRuntimes) of a
// namespace, or the ClusterTrainingRuntimes when clusterScope is set.
func listRuntimes(ctx context.Context, c *Connection, clusterScope bool) ([]kf.TrainingRuntime, error) {
	gvk := kf.TrainingRuntimeGVK
	if clusterScope {
		gvk = kf.ClusterTrainingRuntimeGVK
	}
	u := &unstructured.UnstructuredList{}
	u.SetGroupVersionKind(gvk.GroupVersion().WithKind(listKindFor(gvk.Kind)))
	if err := c.k8s.List(ctx, u, client.InNamespace(c.namespace())); err != nil {
		return nil, err
	}
	out := make([]kf.TrainingRuntime, 0, len(u.Items))
	for i := range u.Items {
		rt := &kf.TrainingRuntime{}
		if err := kf.FromUnstructured(u.Items[i].Object, rt); err != nil {
			return nil, err
		}
		out = append(out, *rt)
	}
	return out, nil
}

func listKindFor(kind string) string {
	if kind == "TrainingRuntime" {
		return "TrainingRuntimeList"
	}
	return "ClusterTrainingRuntimeList"
}

// trainJobStub addresses a TrainJob for delete calls.
func trainJobStub(c *Connection, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(kf.TrainJobGVK)
	u.SetNamespace(c.namespace())
	u.SetName(name)
	return u
}

// runtimeObject converts a rendered TrainingRuntime template into the
// unstructured create payload.
func runtimeObject(rt *kf.TrainingRuntime) (*unstructured.Unstructured, error) {
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(rt)
	if err != nil {
		return nil, err
	}
	u := &unstructured.Unstructured{Object: m}
	u.SetGroupVersionKind(kf.TrainingRuntimeGVK)
	unstructured.RemoveNestedField(u.Object, naming.KeyMetadata, "creationTimestamp")
	unstructured.RemoveNestedField(u.Object, naming.KeyStatus)
	return u, nil
}

// runtimeStub addresses a runtime object for delete calls.
func runtimeStub(gvk schema.GroupVersionKind, namespace, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	u.SetNamespace(namespace)
	u.SetName(name)
	return u
}

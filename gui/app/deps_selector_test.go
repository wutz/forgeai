package app

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCheckSelectors(t *testing.T) {
	s := scheme()
	deploy := func(selector map[string]string, labels, annotations map[string]string) *appsv1.Deployment {
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Namespace: "kueue-system", Name: "kueue-controller-manager", Labels: labels, Annotations: annotations},
			Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: selector}},
		}
	}
	// The bundle's Deployment, as the embedded manifest decodes it.
	bundleDeploy := deploy(map[string]string{"control-plane": "controller-manager"}, nil, nil)
	bundleDeploy.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"}
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(bundleDeploy)
	if err != nil {
		t.Fatal(err)
	}
	b := &depBundle{Name: "kueue", objects: []*unstructured.Unstructured{{Object: raw}}}

	helm := map[string]string{"app.kubernetes.io/managed-by": "Helm"}
	helmAnn := map[string]string{"meta.helm.sh/release-name": "kueue"}
	tests := []struct {
		name    string
		k8s     client.Client
		wantErr string
	}{
		{"nothing installed", fake.NewClientBuilder().WithScheme(s).Build(), ""},
		{"same selector", fake.NewClientBuilder().WithScheme(s).WithObjects(
			deploy(map[string]string{"control-plane": "controller-manager"}, nil, nil)).Build(), ""},
		{"Helm release with another selector", fake.NewClientBuilder().WithScheme(s).WithObjects(
			deploy(map[string]string{"app.kubernetes.io/name": "kueue", "control-plane": "controller-manager"}, helm, helmAnn)).Build(),
			"installed by Helm release kueue"},
		{"plain Deployment with another selector", fake.NewClientBuilder().WithScheme(s).WithObjects(
			deploy(map[string]string{"app": "kueue"}, nil, nil)).Build(),
			"delete the Deployment before installing kueue"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkSelectors(context.Background(), tt.k8s, b)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("checkSelectors = %v; want no error", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("checkSelectors = %v; want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

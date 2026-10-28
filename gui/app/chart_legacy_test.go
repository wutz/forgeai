package app

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/wutz/forgeai/pkg/naming"
)

func TestMigrateTrashLabels(t *testing.T) {
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	pvc := func(name string, labels map[string]string) *corev1.PersistentVolumeClaim {
		return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "forge-a", Labels: labels}}
	}
	k8s := fake.NewClientBuilder().WithScheme(s).WithObjects(
		pvc("legacy", map[string]string{naming.LabelTrashLegacy: naming.LiteralTrue}),
		pvc("live", map[string]string{"app": "data"}),
	).Build()
	ctx := context.Background()

	if err := migrateTrashLabels(ctx, k8s); err != nil {
		t.Fatalf("migrateTrashLabels: %v", err)
	}
	legacy := &corev1.PersistentVolumeClaim{}
	if err := k8s.Get(ctx, client.ObjectKey{Namespace: "forge-a", Name: "legacy"}, legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Labels[naming.LabelFileServerIgnore] != naming.LiteralTrue || legacy.Labels[naming.LabelTrashLegacy] != "" {
		t.Errorf("legacy volume labels = %v, want the ignore label and no legacy label", legacy.Labels)
	}
	live := &corev1.PersistentVolumeClaim{}
	if err := k8s.Get(ctx, client.ObjectKey{Namespace: "forge-a", Name: "live"}, live); err != nil {
		t.Fatal(err)
	}
	if _, ok := live.Labels[naming.LabelFileServerIgnore]; ok {
		t.Errorf("live volume labels = %v, want no ignore label", live.Labels)
	}
}

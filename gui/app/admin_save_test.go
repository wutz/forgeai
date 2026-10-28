package app

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	projectv1 "github.com/wutz/workspace/api/v1alpha1"
)

// TestSaveProjectRefusesStaleVersion checks that a form read before another
// save cannot overwrite that save: the stale form is refused, the current one
// is written.
func TestSaveProjectRefusesStaleVersion(t *testing.T) {
	a := testApp(t, &projectv1.Project{ObjectMeta: metav1.ObjectMeta{Name: "demo"}})
	ctx := context.Background()

	stored := &projectv1.Project{}
	if err := a.conn.k8s.Get(ctx, client.ObjectKey{Name: "demo"}, stored); err != nil {
		t.Fatal(err)
	}
	opened := toProject(stored)

	// Another admin saves first, moving the version on.
	first := opened
	first.DisplayName = "Changed elsewhere"
	if err := a.SaveProject(first); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// The form opened before that save must not overwrite it.
	stale := opened
	stale.DisplayName = "Lost update"
	if err := a.SaveProject(stale); err == nil {
		t.Fatal("stale form saved over a newer project")
	}

	if err := a.conn.k8s.Get(ctx, client.ObjectKey{Name: "demo"}, stored); err != nil {
		t.Fatal(err)
	}
	if stored.Spec.DisplayName != "Changed elsewhere" {
		t.Fatalf("display name = %q, want the first save", stored.Spec.DisplayName)
	}
}

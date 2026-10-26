package app

import (
	"context"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
)

// TestSaveProjectCreatesProjectFromForm checks a new project from the admin
// form: blank member rows are dropped, names are trimmed, and the retention
// and deletion policy reach the Project. Deleting it then removes it.
func TestSaveProjectCreatesProjectFromForm(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()

	err := a.SaveProject(Project{
		Name:           "qa-new",
		DisplayName:    "QA new",
		Network:        ProjectNetwork{Isolation: string(v1.NetworkIsolated)},
		Members:        []ProjectMember{{Name: " alice ", Role: "edit"}, {Name: "  ", Role: "view"}, {Name: "bob", Role: "view"}},
		TrashRetention: "72h",
		DeletionPolicy: string(v1.DeletionDelete),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	stored := &v1.Project{}
	if err := a.conn.k8s.Get(ctx, client.ObjectKey{Name: "qa-new"}, stored); err != nil {
		t.Fatal(err)
	}
	if got := len(stored.Spec.Members); got != 2 {
		t.Fatalf("members = %+v, want 2 (blank row dropped)", stored.Spec.Members)
	}
	if stored.Spec.Members[0].Name != "alice" || stored.Spec.Members[1].Name != "bob" {
		t.Errorf("member names = %q, %q, want trimmed alice, bob", stored.Spec.Members[0].Name, stored.Spec.Members[1].Name)
	}
	if stored.Spec.TrashRetention == nil || stored.Spec.TrashRetention.Duration != 72*time.Hour {
		t.Errorf("trash retention = %v, want 72h", stored.Spec.TrashRetention)
	}
	if stored.Spec.DeletionPolicy != v1.DeletionDelete {
		t.Errorf("deletion policy = %q, want Delete", stored.Spec.DeletionPolicy)
	}

	if err := a.DeleteProject("qa-new"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	err = a.conn.k8s.Get(ctx, client.ObjectKey{Name: "qa-new"}, stored)
	if !apierrors.IsNotFound(err) {
		t.Errorf("after delete, get err = %v, want NotFound", err)
	}
}

// A malformed trash retention is refused and nothing is written.
func TestSaveProjectRefusesBadTrashRetention(t *testing.T) {
	a := testApp(t)
	err := a.SaveProject(Project{Name: "qa-bad", TrashRetention: "soon"})
	if err == nil {
		t.Fatal("bad trash retention saved")
	}
	err = a.conn.k8s.Get(context.Background(), client.ObjectKey{Name: "qa-bad"}, &v1.Project{})
	if !apierrors.IsNotFound(err) {
		t.Errorf("project written despite bad retention, get err = %v", err)
	}
}

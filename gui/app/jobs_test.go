package app

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/naming"
)

// testTrainJob returns a TrainJob; group is its retry group label, empty for
// a group leader.
func testTrainJob(name, group string) *kf.TrainJob {
	tj := &kf.TrainJob{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace}}
	if group != "" {
		tj.Labels = map[string]string{naming.LabelJobGroup: group}
	}
	return tj
}

func TestGetJobDrillsIntoRetries(t *testing.T) {
	a := testApp(t)
	c := a.conn
	ctx := context.Background()
	// Retries are created before their leader is listed, to cover ordering.
	for _, tj := range []*kf.TrainJob{
		testTrainJob("train-r2", "train"),
		testTrainJob("train-r1", "train"),
		testTrainJob("train", ""),
		testTrainJob("orphan-r1", "gone"),
	} {
		obj, err := trainJobObject(tj)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.k8s.Create(ctx, obj); err != nil {
			t.Fatal(err)
		}
	}

	leader, err := a.GetJob("train")
	if err != nil {
		t.Fatal(err)
	}
	if len(leader.Retries) != 2 || leader.Retries[0].Name != "train-r1" || leader.Retries[1].Name != "train-r2" {
		t.Fatalf("leader retries = %+v", leader.Retries)
	}

	// The list links a retry to its detail view; that must resolve on its own.
	retry, err := a.GetJob("train-r1")
	if err != nil {
		t.Fatalf("GetJob(retry): %v", err)
	}
	if retry.Name != "train-r1" || len(retry.Retries) != 0 {
		t.Errorf("GetJob(retry) = %+v", retry)
	}

	if _, err := a.GetJob("orphan-r1"); err != nil {
		t.Errorf("GetJob(orphan retry): %v", err)
	}
	if _, err := a.GetJob("missing"); err == nil {
		t.Error("expected not found")
	}
}

package app

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/training"
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

// The list shows each job once: retries are nested under their leader and do
// not appear as rows of their own.
func TestListJobsNestsRetriesOnce(t *testing.T) {
	a := testApp(t)
	c := a.conn
	ctx := context.Background()
	for _, tj := range []*kf.TrainJob{
		testTrainJob("train-r1", "train"),
		testTrainJob("train", ""),
		testTrainJob("solo", ""),
		testTrainJob("train-r2", "train"),
	} {
		obj, err := trainJobObject(tj)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.k8s.Create(ctx, obj); err != nil {
			t.Fatal(err)
		}
	}

	jobs, err := a.ListJobs()
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]int{}
	var leader *Job
	for _, j := range jobs {
		rows[j.Name]++
		if j.Name == "train" {
			leader = j
		}
	}
	if len(jobs) != 2 || rows["train"] != 1 || rows["solo"] != 1 {
		t.Fatalf("ListJobs rows = %v, want train and solo once each", rows)
	}
	if leader == nil || len(leader.Retries) != 2 {
		t.Fatalf("leader = %+v, want 2 nested retries", leader)
	}
}

func TestRerunFormKeepsTemplate(t *testing.T) {
	src := testTrainJob("train", "")
	src.Labels = map[string]string{naming.LabelTemplate: "torch", naming.LabelInstanceType: "cpu.small"}
	f, err := rerunForm(src, "train-again")
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "train-again" || f.Template != "torch" || f.InstanceType != "cpu.small" {
		t.Errorf("rerun form = %+v, want the new name with template torch on cpu.small", f)
	}
}

// seedTrainJobs creates the TrainJobs in the test namespace.
func seedTrainJobs(t *testing.T, a *App, tjs ...*kf.TrainJob) {
	t.Helper()
	for _, tj := range tjs {
		obj, err := trainJobObject(tj)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.conn.k8s.Create(context.Background(), obj); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFreeRerunNameSkipsTakenNames(t *testing.T) {
	a := testApp(t)
	seedTrainJobs(t, a, testTrainJob("job", ""), testTrainJob("job-r2", "job"))
	got, err := freeRerunName(context.Background(), a.conn, "job")
	if err != nil {
		t.Fatal(err)
	}
	if got != "job-r3" {
		t.Fatalf("freeRerunName = %q, want job-r3", got)
	}
}

func TestFreeRerunNameKeepsSuffixWithinLimit(t *testing.T) {
	long := strings.Repeat("a", training.MaxNameLength)
	a := testApp(t)
	got, err := freeRerunName(context.Background(), a.conn, long)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > training.MaxNameLength+len("-r9") || !strings.HasSuffix(got, "-r2") {
		t.Fatalf("freeRerunName = %q, want a -r2 suffix within the name limit", got)
	}
}

func TestDeleteJobRemovesWholeGroup(t *testing.T) {
	a := testApp(t)
	seedTrainJobs(t, a,
		testTrainJob("lead", ""),
		testTrainJob("lead-r2", "lead"),
		testTrainJob("other", ""),
	)
	ctx := context.Background()
	if err := a.DeleteJob("lead"); err != nil {
		t.Fatal(err)
	}
	items, err := listTrainJobs(ctx, a.conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "other" {
		names := make([]string, 0, len(items))
		for i := range items {
			names = append(names, items[i].Name)
		}
		t.Fatalf("remaining jobs = %v, want [other]", names)
	}
}

func TestCreateJobRefusesMissingTemplate(t *testing.T) {
	a := testApp(t)
	f := testTemplateForm("train", "img:1")
	f.Template = "gone"
	_, err := a.renderTrainJob(context.Background(), a.conn, f)
	if err == nil || !strings.Contains(err.Error(), `template "gone" not found`) {
		t.Fatalf("renderTrainJob with a missing template: err = %v, want template not found", err)
	}
}

func TestDeletedRetryDoesNotSupersedeJob(t *testing.T) {
	a := testApp(t)
	leader := testTrainJob("train", "")
	leader.Annotations = map[string]string{naming.AnnotationRetriedAs: "train-r1"}
	seedTrainJobs(t, a, leader)

	// The retry the leader points at is gone, so the leader is not superseded.
	j, err := a.GetJob("train")
	if err != nil {
		t.Fatal(err)
	}
	if j.Superseded || j.RetriedAs != "" {
		t.Errorf("job with a deleted retry: superseded = %v, retriedAs = %q; want neither", j.Superseded, j.RetriedAs)
	}

	// Once the retry exists, the link is shown again.
	seedTrainJobs(t, a, testTrainJob("train-r1", "train"))
	j, err = a.GetJob("train")
	if err != nil {
		t.Fatal(err)
	}
	if !j.Superseded || j.RetriedAs != "train-r1" {
		t.Errorf("job with a live retry: superseded = %v, retriedAs = %q; want superseded by train-r1", j.Superseded, j.RetriedAs)
	}
}

// Package jobs implements TrainingJob operations shared by the compat API and
// the GUI. It only talks to the Kubernetes API; the operator does the rest.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
)

// ErrNotFound is returned when a job does not exist.
var ErrNotFound = errors.New("job not found")

// Client operates on TrainingJobs in one namespace.
type Client struct {
	K8s client.Client
	// Namespace holds the jobs.
	Namespace string
	// ConfigNamespace holds the platform ConfigMap.
	ConfigNamespace string
}

// Config loads the platform config.
func (c *Client) Config(ctx context.Context) (*platform.Config, error) {
	return platform.Load(ctx, c.K8s, c.ConfigNamespace)
}

// Name returns the TrainingJob name of a user job.
func (c *Client) Name(userID, jobID string) string {
	return naming.JobName(c.Namespace, userID, jobID)
}

// Get returns a job by user and job ID.
func (c *Client) Get(ctx context.Context, userID, jobID string) (*v1.TrainingJob, error) {
	return c.GetByName(ctx, c.Name(userID, jobID))
}

// GetByName returns a job by TrainingJob name.
func (c *Client) GetByName(ctx context.Context, name string) (*v1.TrainingJob, error) {
	job := &v1.TrainingJob{}
	if err := c.K8s.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: name}, job); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return job, nil
}

// List returns jobs; empty userID lists all users, empty jobID lists all jobs of the user.
func (c *Client) List(ctx context.Context, userID, jobID string) ([]v1.TrainingJob, error) {
	sel := client.MatchingLabels{}
	if userID != "" {
		sel[naming.LabelUserID] = userID
	}
	if jobID != "" {
		sel[naming.LabelPodID] = jobID
	}
	list := &v1.TrainingJobList{}
	if err := c.K8s.List(ctx, list, client.InNamespace(c.Namespace), sel); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// Validate checks a spec against the platform config before it is submitted.
func Validate(cfg *platform.Config, spec *v1.TrainingJobSpec) error {
	if spec.UserID == "" || spec.JobID == "" {
		return fmt.Errorf("userID and jobID are required")
	}
	if spec.Image == "" {
		return fmt.Errorf("image is required")
	}
	if _, err := cfg.Resource(spec.ResourceID); err != nil {
		return err
	}
	if len(spec.UserID) > 63 {
		return fmt.Errorf("userID longer than 63 characters")
	}
	if spec.JobID != strings.ToLower(spec.JobID) || strings.ContainsAny(spec.JobID, "_. /") {
		return fmt.Errorf("jobID %q must be lowercase alphanumeric or '-'", spec.JobID)
	}
	return nil
}

// Apply creates a job, or updates an existing one. Updating a job with a changed
// spec restarts it so the change takes effect; an identical spec is a no-op
// apart from un-suspending it.
func (c *Client) Apply(ctx context.Context, spec v1.TrainingJobSpec) (*v1.TrainingJob, error) {
	if spec.Workers < 1 {
		spec.Workers = 1
	}
	name := c.Name(spec.UserID, spec.JobID)
	var out *v1.TrainingJob
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur, err := c.GetByName(ctx, name)
		if errors.Is(err, ErrNotFound) {
			job := &v1.TrainingJob{Spec: spec}
			job.Name, job.Namespace = name, c.Namespace
			job.Labels = map[string]string{
				naming.LabelUserID: spec.UserID,
				naming.LabelPodID:  spec.JobID,
			}
			job.Spec.Suspend = false
			if err := c.K8s.Create(ctx, job); err != nil {
				return err
			}
			out = job
			return nil
		}
		if err != nil {
			return err
		}
		next := spec
		next.Suspend = false
		next.RestartGeneration = cur.Spec.RestartGeneration
		prev := cur.Spec
		prev.Suspend = false
		if !reflect.DeepEqual(normalize(prev), normalize(next)) {
			next.RestartGeneration++
		}
		cur.Spec = next
		if err := c.K8s.Update(ctx, cur); err != nil {
			return err
		}
		out = cur
		return nil
	})
	return out, err
}

// normalize clears API server defaults so specs submitted by clients compare equal.
func normalize(s v1.TrainingJobSpec) v1.TrainingJobSpec {
	if s.Workers < 1 {
		s.Workers = 1
	}
	if len(s.Env) == 0 {
		s.Env = nil
	}
	if len(s.VolumeMounts) == 0 {
		s.VolumeMounts = nil
	}
	return s
}

func (c *Client) mutate(ctx context.Context, name string, fn func(*v1.TrainingJob)) (*v1.TrainingJob, error) {
	var out *v1.TrainingJob
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		job, err := c.GetByName(ctx, name)
		if err != nil {
			return err
		}
		fn(job)
		if err := c.K8s.Update(ctx, job); err != nil {
			return err
		}
		out = job
		return nil
	})
	return out, err
}

// Stop releases the job's pods but keeps its spec and volumes.
func (c *Client) Stop(ctx context.Context, name string) (*v1.TrainingJob, error) {
	return c.mutate(ctx, name, func(j *v1.TrainingJob) { j.Spec.Suspend = true })
}

// Start resumes a stopped job, or re-runs a finished one.
func (c *Client) Start(ctx context.Context, name string) (*v1.TrainingJob, error) {
	return c.mutate(ctx, name, func(j *v1.TrainingJob) {
		if !j.Spec.Suspend && (j.Status.Phase == v1.PhaseSucceeded || j.Status.Phase == v1.PhaseFailed) {
			j.Spec.RestartGeneration++
		}
		j.Spec.Suspend = false
	})
}

// Restart deletes the current run and starts a new one.
func (c *Client) Restart(ctx context.Context, name string) (*v1.TrainingJob, error) {
	return c.mutate(ctx, name, func(j *v1.TrainingJob) {
		j.Spec.Suspend = false
		j.Spec.RestartGeneration++
	})
}

// Delete removes the job and everything it owns.
func (c *Client) Delete(ctx context.Context, name string) (*v1.TrainingJob, error) {
	job, err := c.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if err := c.K8s.Delete(ctx, job); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return job, nil
}

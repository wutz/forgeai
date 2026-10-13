package server

import (
	"fmt"
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/util/duration"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
)

// Request and response models mirror scc-k8s-v2 schemas/job.py and
// schemas/volume_mount.py so existing clients keep working.

type VolumeMountIn struct {
	_         struct{} `additionalProperties:"true"`
	VolumeID  string   `json:"volumeID" minLength:"1"`
	UserID    *string  `json:"userID,omitempty" nullable:"true"`
	MountPath string   `json:"mountPath" minLength:"1"`
	SubPath   *string  `json:"subPath,omitempty" nullable:"true"`
	ReadOnly  *bool    `json:"readOnly,omitempty" nullable:"true"`
}

type JobIn struct {
	_            struct{}        `additionalProperties:"true"`
	Image        string          `json:"image" minLength:"1" example:"cr.zw1.paratera.com/pod/pytorch:latest"`
	ResourceID   string          `json:"resourceID" minLength:"1" example:"rtx5090.xeon6530.xlarge"`
	PoolID       string          `json:"pool_id,omitempty" default:"default"`
	UserGroup    string          `json:"userGroup,omitempty" default:"bj8"`
	Workers      int32           `json:"workers,omitempty" default:"1" minimum:"1"`
	VolumeMounts []VolumeMountIn `json:"volumeMounts,omitempty" nullable:"false"`
	Cmd          *string         `json:"cmd,omitempty" nullable:"true" example:"python train.py"`
	// Envs is injected into every worker (accepted but ignored by scc-k8s-v2).
	Envs     map[string]any `json:"envs,omitempty"`
	UserID   *string        `json:"userID,omitempty" nullable:"true"`
	DataFrom *string        `json:"data_from,omitempty" nullable:"true"`
}

type VolumeMountOut struct {
	VolumeID  string  `json:"volumeID"`
	UserID    *string `json:"userID"`
	MountPath string  `json:"mountPath"`
	SubPath   *string `json:"subPath"`
	ReadOnly  *bool   `json:"readOnly"`
}

type WorkerPodInfo struct {
	PodName     string  `json:"pod_name"`
	NodeName    *string `json:"node_name"`
	PodIP       *string `json:"pod_ip"`
	NodeIP      *string `json:"node_ip"`
	Status      *string `json:"status"`
	WorkerIndex *string `json:"worker_index"`
}

type JobOut struct {
	Image          string           `json:"image"`
	ResourceID     string           `json:"resourceID"`
	PoolID         string           `json:"pool_id"`
	UserGroup      string           `json:"userGroup"`
	Workers        int32            `json:"workers"`
	VolumeMounts   []VolumeMountOut `json:"volumeMounts"`
	Cmd            string           `json:"cmd"`
	ID             string           `json:"id"`
	Status         *string          `json:"status"`
	CreateTime     string           `json:"createTime"`
	UserID         string           `json:"userID"`
	Completions    *string          `json:"completions"`
	Duration       *string          `json:"duration"`
	Age            *string          `json:"age"`
	Active         *string          `json:"active"`
	Failed         *string          `json:"failed"`
	Succeeded      *string          `json:"succeeded"`
	StartTime      *string          `json:"startTime"`
	CompletionTime *string          `json:"completionTime"`
	Ready          *string          `json:"ready"`
	WorkersInfo    []WorkerPodInfo  `json:"workers_info"`
	// Message explains the current status (forgeai extension, e.g. Kueue queueing reason).
	Message string `json:"message,omitempty"`
}

type JobList struct {
	Jobs []JobOut `json:"jobs"`
}

// legacyStatus maps the TrainingJob phase to the batch Job STATUS values that
// scc-k8s-v2 returned from `kubectl get jobs -o wide`.
func legacyStatus(p v1.TrainingJobPhase) string {
	switch p {
	case v1.PhaseSucceeded:
		return "Complete"
	case v1.PhaseFailed:
		return "Failed"
	case v1.PhaseRunning:
		return "Running"
	case v1.PhaseQueued:
		return "Suspended"
	case v1.PhaseStopped, v1.PhaseStopping:
		return string(p)
	default:
		return "Pending"
	}
}

func strp(s string) *string { return &s }

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func spec(userID, jobID string, in *JobIn) v1.TrainingJobSpec {
	s := v1.TrainingJobSpec{
		UserID: userID, JobID: jobID, UserGroup: in.UserGroup, PoolID: in.PoolID,
		Image: in.Image, ResourceID: in.ResourceID, Workers: in.Workers,
	}
	if in.Cmd != nil {
		s.Command = *in.Cmd
	}
	keys := make([]string, 0, len(in.Envs))
	for k := range in.Envs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s.Env = append(s.Env, v1.EnvVar{Name: k, Value: fmt.Sprint(in.Envs[k])})
	}
	for _, m := range in.VolumeMounts {
		vm := v1.VolumeMount{VolumeID: m.VolumeID, MountPath: m.MountPath}
		if m.UserID != nil {
			vm.UserID = *m.UserID
		}
		if m.SubPath != nil {
			vm.SubPath = *m.SubPath
		}
		if m.ReadOnly != nil {
			vm.ReadOnly = *m.ReadOnly
		}
		s.VolumeMounts = append(s.VolumeMounts, vm)
	}
	return s
}

// toJobOut converts a TrainingJob to the scc-k8s-v2 JobOut shape. cmd is always blanked.
func toJobOut(j *v1.TrainingJob, now time.Time) JobOut {
	st := j.Status
	out := JobOut{
		Image: j.Spec.Image, ResourceID: j.Spec.ResourceID, PoolID: j.Spec.PoolID,
		UserGroup: j.Spec.UserGroup, Workers: j.Spec.Workers, VolumeMounts: []VolumeMountOut{},
		ID: j.Spec.JobID, UserID: j.Spec.UserID, Message: st.Message,
		Status:         strp(legacyStatus(st.Phase)),
		CreateTime:     j.CreationTimestamp.Local().Format("2006-01-02 15:04:05"),
		Active:         strp(fmt.Sprint(st.Active)),
		Ready:          strp(fmt.Sprint(st.Ready)),
		Succeeded:      strp(fmt.Sprint(st.Succeeded)),
		Failed:         strp(fmt.Sprint(st.Failed)),
		Completions:    strp(fmt.Sprintf("%d/%d", st.Succeeded, max(j.Spec.Workers, 1))),
		Age:            strp(duration.HumanDuration(now.Sub(j.CreationTimestamp.Time))),
		StartTime:      strp(""),
		CompletionTime: strp(""),
		Duration:       strp(""),
	}
	for _, m := range j.Spec.VolumeMounts {
		out.VolumeMounts = append(out.VolumeMounts, VolumeMountOut{
			VolumeID: m.VolumeID, UserID: optStr(m.UserID), MountPath: m.MountPath,
			SubPath: optStr(m.SubPath), ReadOnly: &m.ReadOnly,
		})
	}
	if st.StartTime != nil {
		out.StartTime = strp(st.StartTime.UTC().Format(time.RFC3339))
		end := now
		if st.CompletionTime != nil {
			end = st.CompletionTime.Time
		}
		out.Duration = strp(duration.HumanDuration(end.Sub(st.StartTime.Time)))
	}
	if st.CompletionTime != nil {
		out.CompletionTime = strp(st.CompletionTime.UTC().Format(time.RFC3339))
	}
	for _, w := range st.Workers {
		if w.PodName == "" {
			continue
		}
		out.WorkersInfo = append(out.WorkersInfo, WorkerPodInfo{
			PodName: w.PodName, NodeName: optStr(w.NodeName), PodIP: optStr(w.PodIP),
			NodeIP: optStr(w.NodeIP), Status: optStr(w.Phase), WorkerIndex: strp(fmt.Sprint(w.Index)),
		})
	}
	return out
}

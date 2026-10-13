/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// VolumeMount mounts a user volume (PVC) into every worker.
type VolumeMount struct {
	// volumeID is the platform volume ID. The PVC name is derived as p-{userHash}-{volumeID}
	// unless claimName is set.
	// +required
	VolumeID string `json:"volumeID"`

	// userID owns the volume; defaults to the job's userID (shared volumes may belong to another user).
	// +optional
	UserID string `json:"userID,omitempty"`

	// claimName overrides the derived PVC name.
	// +optional
	ClaimName string `json:"claimName,omitempty"`

	// +required
	MountPath string `json:"mountPath"`

	// +optional
	SubPath string `json:"subPath,omitempty"`

	// +optional
	ReadOnly bool `json:"readOnly,omitempty"`
}

// EnvVar is a plain name/value environment variable.
type EnvVar struct {
	// +required
	Name string `json:"name"`
	// +optional
	Value string `json:"value"`
}

// TrainingJobSpec defines the desired state of TrainingJob.
type TrainingJobSpec struct {
	// userID is the platform user that owns this job.
	// +required
	// +kubebuilder:validation:MinLength=1
	UserID string `json:"userID"`

	// jobID is the user-facing job ID, unique per user.
	// +required
	// +kubebuilder:validation:MaxLength=40
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	JobID string `json:"jobID"`

	// userGroup is propagated as the user.scclabs.dev/group label.
	// +optional
	UserGroup string `json:"userGroup,omitempty"`

	// image is the container image for every worker.
	// +required
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// command is a bash script run in every worker. When empty the image entrypoint is used.
	// +optional
	Command string `json:"command,omitempty"`

	// env is injected into every worker.
	// +optional
	Env []EnvVar `json:"env,omitempty"`

	// resourceID references a resource model in the platform config.
	// +required
	// +kubebuilder:validation:MinLength=1
	ResourceID string `json:"resourceID"`

	// workers is the number of nodes (one pod per node).
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Workers int32 `json:"workers,omitempty"`

	// volumeMounts mounts user PVCs into every worker.
	// +optional
	VolumeMounts []VolumeMount `json:"volumeMounts,omitempty"`

	// queueName is the Kueue LocalQueue; defaults to the platform default queue.
	// +optional
	QueueName string `json:"queueName,omitempty"`

	// suspend stops the job: the underlying TrainJob and its pods are deleted,
	// the spec is kept so it can be started again.
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// restartGeneration triggers a restart when incremented.
	// +optional
	RestartGeneration int64 `json:"restartGeneration,omitempty"`
}

// TrainingJobPhase is a high level summary of the job lifecycle.
// +kubebuilder:validation:Enum=Pending;Queued;Running;Succeeded;Failed;Stopping;Stopped;Restarting
type TrainingJobPhase string

const (
	PhasePending    TrainingJobPhase = "Pending"
	PhaseQueued     TrainingJobPhase = "Queued"
	PhaseRunning    TrainingJobPhase = "Running"
	PhaseSucceeded  TrainingJobPhase = "Succeeded"
	PhaseFailed     TrainingJobPhase = "Failed"
	PhaseStopping   TrainingJobPhase = "Stopping"
	PhaseStopped    TrainingJobPhase = "Stopped"
	PhaseRestarting TrainingJobPhase = "Restarting"
)

// WorkerStatus describes one worker pod.
type WorkerStatus struct {
	Index    int32  `json:"index"`
	PodName  string `json:"podName,omitempty"`
	NodeName string `json:"nodeName,omitempty"`
	PodIP    string `json:"podIP,omitempty"`
	NodeIP   string `json:"nodeIP,omitempty"`
	// phase is the pod phase.
	Phase string `json:"phase,omitempty"`
}

// TrainingJobStatus defines the observed state of TrainingJob.
type TrainingJobStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	Phase TrainingJobPhase `json:"phase,omitempty"`

	// message gives detail on the current phase.
	// +optional
	Message string `json:"message,omitempty"`

	// trainJobName is the Kubeflow TrainJob backing this job.
	// +optional
	TrainJobName string `json:"trainJobName,omitempty"`

	// observedRestartGeneration is the last restartGeneration that was acted upon.
	// +optional
	ObservedRestartGeneration int64 `json:"observedRestartGeneration,omitempty"`

	// Aggregated counts over the batch Jobs created by the JobSet.
	// +optional
	Active int32 `json:"active,omitempty"`
	// +optional
	Ready int32 `json:"ready,omitempty"`
	// +optional
	Succeeded int32 `json:"succeeded,omitempty"`
	// +optional
	Failed int32 `json:"failed,omitempty"`

	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// +optional
	Workers []WorkerStatus `json:"workers,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ftj,categories=forgeai
// +kubebuilder:printcolumn:name="User",type=string,JSONPath=`.spec.userID`,priority=1
// +kubebuilder:printcolumn:name="Job",type=string,JSONPath=`.spec.jobID`
// +kubebuilder:printcolumn:name="Resource",type=string,JSONPath=`.spec.resourceID`
// +kubebuilder:printcolumn:name="Workers",type=integer,JSONPath=`.spec.workers`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TrainingJob is a distributed training job built on Kubeflow Trainer + Kueue.
// Workers are not reachable interactively: the pod network is not isolated
// between users, so no SSH / Jupyter / exec endpoints are exposed.
type TrainingJob struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of TrainingJob
	// +required
	Spec TrainingJobSpec `json:"spec"`

	// status defines the observed state of TrainingJob
	// +optional
	Status TrainingJobStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TrainingJobList contains a list of TrainingJob
type TrainingJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TrainingJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &TrainingJob{}, &TrainingJobList{})
		return nil
	})
}

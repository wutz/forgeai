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

// PoolRequestSpec moves nnodes idle nodes that fit resourceID from srcPool to
// dstPool (the scclabs.dev/pool node label). It mirrors scc-k8s-v2 pool requests.
type PoolRequestSpec struct {
	// userID is the platform user that requested the transfer.
	// +required
	// +kubebuilder:validation:MinLength=1
	UserID string `json:"userID"`

	// requestID is the user-facing request ID.
	// +required
	// +kubebuilder:validation:MaxLength=40
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	RequestID string `json:"requestID"`

	// srcPool is the pool nodes are taken from.
	// +optional
	// +kubebuilder:default=default
	SrcPool string `json:"srcPool,omitempty"`

	// dstPool is the pool nodes are moved to.
	// +optional
	// +kubebuilder:default=default
	DstPool string `json:"dstPool,omitempty"`

	// resourceID selects node type (resource model labels) and the capacity
	// that must be free on each node.
	// +required
	// +kubebuilder:validation:MinLength=1
	ResourceID string `json:"resourceID"`

	// nnodes is the number of nodes to move.
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	NNodes int32 `json:"nnodes,omitempty"`
}

// PoolRequestPhase is Pending until all nodes are moved.
// +kubebuilder:validation:Enum=Pending;Success;Failed
type PoolRequestPhase string

const (
	PoolRequestPending PoolRequestPhase = "Pending"
	PoolRequestSuccess PoolRequestPhase = "Success"
	PoolRequestFailed  PoolRequestPhase = "Failed"
)

// PoolRequestStatus defines the observed state of PoolRequest.
type PoolRequestStatus struct {
	// +optional
	Phase PoolRequestPhase `json:"phase,omitempty"`

	// message explains the current phase (e.g. why placeholders are not scheduled).
	// +optional
	Message string `json:"message,omitempty"`

	// nodes moved to dstPool.
	// +optional
	Nodes []string `json:"nodes,omitempty"`

	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=fpr,categories=forgeai
// +kubebuilder:printcolumn:name="Request",type=string,JSONPath=`.spec.requestID`
// +kubebuilder:printcolumn:name="From",type=string,JSONPath=`.spec.srcPool`
// +kubebuilder:printcolumn:name="To",type=string,JSONPath=`.spec.dstPool`
// +kubebuilder:printcolumn:name="Resource",type=string,JSONPath=`.spec.resourceID`
// +kubebuilder:printcolumn:name="Nodes",type=integer,JSONPath=`.spec.nnodes`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PoolRequest moves idle nodes between node pools. The operator holds matching
// nodes with placeholder pods, then relabels them to dstPool.
type PoolRequest struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of PoolRequest
	// +required
	Spec PoolRequestSpec `json:"spec"`

	// status defines the observed state of PoolRequest
	// +optional
	Status PoolRequestStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// PoolRequestList contains a list of PoolRequest
type PoolRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []PoolRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &PoolRequest{}, &PoolRequestList{})
		return nil
	})
}

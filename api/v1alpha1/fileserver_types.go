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

// FileServerSpec defines the desired state of FileServer.
type FileServerSpec struct {
	// volumes lists the PVCs to serve. Empty serves every ReadWriteMany volume
	// of the namespace that is not in the trash.
	// +optional
	Volumes []string `json:"volumes,omitempty"`
	// idleTimeout stops the server after no activity for this long. Defaults
	// to the platform setting.
	// +optional
	IdleTimeout *metav1.Duration `json:"idleTimeout,omitempty"`
}

// FileServerPhase summarises the server state.
// +kubebuilder:validation:Enum=Idle;Starting;Ready
type FileServerPhase string

const (
	FileServerIdle     FileServerPhase = "Idle"
	FileServerStarting FileServerPhase = "Starting"
	FileServerReady    FileServerPhase = "Ready"
)

// FileServerStatus defines the observed state of FileServer.
type FileServerStatus struct {
	// +optional
	Phase FileServerPhase `json:"phase,omitempty"`
	// url is the external URL of the server.
	// +optional
	URL string `json:"url,omitempty"`
	// secret holds the basic auth username and password.
	// +optional
	Secret string `json:"secret,omitempty"`
	// volumes are the PVCs currently served, under /<name>.
	// +optional
	Volumes []string `json:"volumes,omitempty"`
	// lastActiveTime is the latest activity seen (client heartbeat or request).
	// +optional
	LastActiveTime *metav1.Time `json:"lastActiveTime,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ffs,categories=forgeai
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.status.url`
// +kubebuilder:printcolumn:name="Last Active",type=date,JSONPath=`.status.lastActiveTime`

// FileServer is an on-demand file transfer service (dufs) for the volumes of
// a project. Clients wake it by updating the forgeai.scclabs.dev/last-active
// annotation; it scales to zero after idleTimeout.
type FileServer struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of FileServer
	// +optional
	Spec FileServerSpec `json:"spec"`

	// status defines the observed state of FileServer
	// +optional
	Status FileServerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// FileServerList contains a list of FileServer
type FileServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []FileServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &FileServer{}, &FileServerList{})
		return nil
	})
}

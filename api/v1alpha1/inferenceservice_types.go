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

// InferenceServiceSpec defines the desired state of InferenceService.
type InferenceServiceSpec struct {
	// model is the Hugging Face repository of the model to serve, e.g. Qwen/Qwen3.8-27B-FP8.
	// +kubebuilder:validation:MinLength=3
	// +kubebuilder:validation:MaxLength=200
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`
	Model string `json:"model"`
	// servedModelName is the name clients send as the model of a request.
	// Defaults to the repository name of model.
	// +optional
	// +kubebuilder:validation:MaxLength=200
	ServedModelName string `json:"servedModelName,omitempty"`
	// instanceType is the InstanceType of each model server replica. Its GPUs
	// are the tensor parallel size of the model.
	// +kubebuilder:validation:MinLength=1
	InstanceType string `json:"instanceType"`
	// replicas is the number of model server replicas, each on instanceType.
	// Requests are balanced across the replicas by the llm-d router.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`
	// image overrides the platform inference image (vLLM).
	// +optional
	Image string `json:"image,omitempty"`
	// args are extra arguments of vllm serve, e.g. --max-model-len 65536.
	// +optional
	Args []string `json:"args,omitempty"`
	// huggingFaceSecret names a Secret of the namespace with an HF_TOKEN key,
	// needed for gated models.
	// +optional
	HuggingFaceSecret string `json:"huggingFaceSecret,omitempty"`
	// nodeSelector pins the model server pods to nodes with these labels, on top
	// of the GPU product of instanceType. It is the way to avoid a node whose
	// GPU is free but whose runtime is broken.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

// InferenceServiceStatus defines the observed state of InferenceService.
type InferenceServiceStatus struct {
	// endpoint is the external URL of the OpenAI-compatible API.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`
	// readyReplicas is the number of model server replicas that are ready.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`
	// apiKeySecret names the Secret holding the API key (key api-key) that
	// clients send as a bearer token.
	// +optional
	APIKeySecret string `json:"apiKeySecret,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=fisvc,categories=forgeai
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=`.spec.model`
// +kubebuilder:printcolumn:name="Instance",type=string,JSONPath=`.spec.instanceType`
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.status.endpoint`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].reason`

// InferenceService serves a model with vLLM behind the llm-d router (the
// Endpoint Picker of the Gateway API Inference Extension), which balances
// requests by prefix-cache affinity and load. The API is OpenAI-compatible and
// requires the API key of the status. Stopping it (annotation
// forgeai.wutz.dev/stopped: "true") scales the replicas to zero.
type InferenceService struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of InferenceService
	// +required
	Spec InferenceServiceSpec `json:"spec"`

	// status defines the observed state of InferenceService
	// +optional
	Status InferenceServiceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// InferenceServiceList contains a list of InferenceService
type InferenceServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []InferenceService `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &InferenceService{}, &InferenceServiceList{})
		return nil
	})
}

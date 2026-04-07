/*
Copyright 2025.

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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ComponentTemplateSpec defines the desired state of ComponentTemplate
type ComponentTemplateSpec struct {
	ResourceSelector ResourceSelector                `json:"resourceSelector"`
	Resources        map[string]apiextensionsv1.JSON `json:"resources"`
	// SyncInterval defines the interval for reconciliation (e.g., "30s", "5m"). Defaults to 10s.
	// +optional
	// +kubebuilder:default="10s"
	SyncInterval string `json:"syncInterval,omitempty"`

	// Protected prevents deletion of external resources when this Kubernetes resource is deleted
	// +optional
	Protected bool `json:"protected,omitempty"`
}

// ComponentTemplateStatus defines the observed state of ComponentTemplate.
type ComponentTemplateStatus struct {
	// Phase represents the current phase of the ComponentTemplate
	// Possible values: Pending, Syncing, Ready, Error
	// +optional
	Phase string `json:"phase,omitempty"`

	// Message provides additional information about the current phase
	// +optional
	Message string `json:"message,omitempty"`

	// TargetCluster is the namespace/name of the target Elasticsearch cluster
	// Format: "namespace/name"
	// +optional
	TargetCluster string `json:"targetCluster,omitempty"`

	// AppliedResources is a list of resource names that have been successfully applied to Elasticsearch
	// +optional
	AppliedResources []string `json:"appliedResources,omitempty"`

	// LastSyncTime is the timestamp of the last successful synchronization with Elasticsearch
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// ConsecutiveErrors tracks the number of consecutive sync failures for exponential backoff
	// +optional
	ConsecutiveErrors int32 `json:"consecutiveErrors,omitempty"`

	// conditions represent the current state of the ComponentTemplate resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.status.targetCluster`
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=`.status.message`,priority=1
// +kubebuilder:printcolumn:name="Last Sync",type=date,JSONPath=`.status.lastSyncTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ComponentTemplate is the Schema for the componenttemplates API
type ComponentTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ComponentTemplate
	// +required
	Spec ComponentTemplateSpec `json:"spec"`

	// status defines the observed state of ComponentTemplate
	// +optional
	Status ComponentTemplateStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ComponentTemplateList contains a list of ComponentTemplate
type ComponentTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ComponentTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ComponentTemplate{}, &ComponentTemplateList{})
}

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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// IndexTemplateSpec defines the desired state of IndexTemplate
type IndexTemplateSpec struct {
	ResourceSelector ResourceSelector                `json:"resourceSelector"`
	Resources        map[string]apiextensionsv1.JSON `json:"resources"`
	// SyncInterval defines the interval for reconciliation (e.g., "30s", "5m"). Defaults to 10s.
	// +optional
	// +kubebuilder:default="10s"
	SyncInterval string `json:"syncInterval,omitempty"`
}

// IndexTemplateStatus defines the observed state of IndexTemplate.
type IndexTemplateStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// Phase represents the current phase of the IndexTemplate
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

	// conditions represent the current state of the IndexTemplate resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
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

// IndexTemplate is the Schema for the indextemplates API
type IndexTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of IndexTemplate
	// +required
	Spec IndexTemplateSpec `json:"spec"`

	// status defines the observed state of IndexTemplate
	// +optional
	Status IndexTemplateStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// IndexTemplateList contains a list of IndexTemplate
type IndexTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []IndexTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IndexTemplate{}, &IndexTemplateList{})
}

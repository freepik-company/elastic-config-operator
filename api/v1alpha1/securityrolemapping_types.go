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

// SecurityRoleMappingSpec defines the desired state of SecurityRoleMapping
// SecurityRoleMapping manages OpenSearch Security Plugin role mappings via the _plugins/_security/api/rolesmapping API
type SecurityRoleMappingSpec struct {
	// SyncInterval defines how often the operator will reconcile this resource (default: 10s)
	// Examples: "30s", "5m", "1h"
	// +optional
	SyncInterval string `json:"syncInterval,omitempty"`

	// ResourceSelector specifies the target OpenSearch cluster for security role mappings
	ResourceSelector ResourceSelector `json:"resourceSelector"`

	// Resources contains the security role mappings to apply, keyed by role name
	// Each key represents a role name, the value is the role mapping definition
	Resources map[string]apiextensionsv1.JSON `json:"resources"`
}

// SecurityRoleMappingStatus defines the observed state of SecurityRoleMapping.
type SecurityRoleMappingStatus struct {
	// Phase indicates the current phase of the SecurityRoleMapping.
	// It can be "Pending", "Syncing", "Ready", or "Error".
	// +optional
	Phase string `json:"phase,omitempty"`

	// Message provides a human-readable message about the current status.
	// +optional
	Message string `json:"message,omitempty"`

	// TargetCluster is the namespace/name of the target OpenSearch cluster
	// Format: "namespace/name"
	// +optional
	TargetCluster string `json:"targetCluster,omitempty"`

	// AppliedResources lists the names of the role mappings that were successfully applied to OpenSearch.
	// This is used to track which role mappings need to be deleted if they are removed from the spec.
	// +optional
	AppliedResources []string `json:"appliedResources,omitempty"`

	// LastSyncTime records the last time the resource was successfully synchronized with OpenSearch.
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// conditions represent the current state of the SecurityRoleMapping resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Current phase of the SecurityRoleMapping"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".status.targetCluster",description="Target cluster"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Detailed status message",priority=1
// +kubebuilder:printcolumn:name="Last Sync",type="date",JSONPath=".status.lastSyncTime",description="Last successful synchronization time"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// SecurityRoleMapping is the Schema for the securityrolemappings API
// This resource is specifically for OpenSearch clusters (Security Plugin API)
type SecurityRoleMapping struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of SecurityRoleMapping
	// +required
	Spec SecurityRoleMappingSpec `json:"spec"`

	// status defines the observed state of SecurityRoleMapping
	// +optional
	Status SecurityRoleMappingStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SecurityRoleMappingList contains a list of SecurityRoleMapping
type SecurityRoleMappingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SecurityRoleMapping `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SecurityRoleMapping{}, &SecurityRoleMappingList{})
}

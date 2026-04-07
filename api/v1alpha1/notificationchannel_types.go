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

// NotificationChannelSpec defines the desired state of NotificationChannel
// NotificationChannel is OpenSearch's notification channels (Notifications API), for configuring alert destinations
type NotificationChannelSpec struct {
	// SyncInterval defines how often the operator will reconcile this resource (default: 10s)
	// Examples: "30s", "5m", "1h"
	// +optional
	SyncInterval string `json:"syncInterval,omitempty"`

	// ResourceSelector specifies the target OpenSearch cluster for notification channels
	ResourceSelector ResourceSelector `json:"resourceSelector"`

	// Resources contains the notification channels to apply, keyed by channel name
	// Each key represents a channel name, the value is the channel configuration
	Resources map[string]apiextensionsv1.JSON `json:"resources"`

	// Protected prevents deletion of external resources when this Kubernetes resource is deleted
	// +optional
	Protected bool `json:"protected,omitempty"`
}

// NotificationChannelStatus defines the observed state of NotificationChannel.
type NotificationChannelStatus struct {
	// Phase indicates the current phase of the NotificationChannel.
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

	// AppliedResources lists the names of the notification channels that were successfully applied to OpenSearch.
	// +optional
	AppliedResources []string `json:"appliedResources,omitempty"`

	// LastSyncTime records the last time the resource was successfully synchronized with OpenSearch.
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// ConsecutiveErrors tracks the number of consecutive sync failures for exponential backoff
	// +optional
	ConsecutiveErrors int32 `json:"consecutiveErrors,omitempty"`

	// conditions represent the current state of the NotificationChannel resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Current phase of the NotificationChannel"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".status.targetCluster",description="Target cluster"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Detailed status message",priority=1
// +kubebuilder:printcolumn:name="Last Sync",type="date",JSONPath=".status.lastSyncTime",description="Last successful synchronization time"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// NotificationChannel is the Schema for the notificationchannels API
// This resource is specifically for OpenSearch clusters (Notifications API)
type NotificationChannel struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of NotificationChannel
	// +required
	Spec NotificationChannelSpec `json:"spec"`

	// status defines the observed state of NotificationChannel
	// +optional
	Status NotificationChannelStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// NotificationChannelList contains a list of NotificationChannel
type NotificationChannelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []NotificationChannel `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NotificationChannel{}, &NotificationChannelList{})
}

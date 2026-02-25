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

package securityrolemapping

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"elastic-config-operator.freepik.com/elastic-config-operator/api/v1alpha1"
	"elastic-config-operator.freepik.com/elastic-config-operator/internal/controller"
	"elastic-config-operator.freepik.com/elastic-config-operator/internal/globals"
)

// UpdateConditionSuccess updates the status of the SecurityRoleMapping resource with a success condition
func (r *SecurityRoleMappingReconciler) UpdateConditionSuccess(securityRoleMapping *v1alpha1.SecurityRoleMapping) {

	// Create the new condition with the success status
	condition := globals.NewCondition(globals.ConditionTypeResourceSynced, metav1.ConditionTrue,
		globals.ConditionReasonTargetSynced, globals.ConditionReasonTargetSyncedMessage)

	// Update the status of the SecurityRoleMapping resource
	globals.UpdateCondition(&securityRoleMapping.Status.Conditions, condition)
}

// UpdateConditionKubernetesApiCallFailure updates the status of the SecurityRoleMapping resource with a failure condition
func (r *SecurityRoleMappingReconciler) UpdateConditionKubernetesApiCallFailure(securityRoleMapping *v1alpha1.SecurityRoleMapping) {

	// Create the new condition with the failure status
	condition := globals.NewCondition(globals.ConditionTypeResourceSynced, metav1.ConditionTrue,
		globals.ConditionReasonKubernetesApiCallErrorType, globals.ConditionReasonKubernetesApiCallErrorMessage)

	// Update the status of the SecurityRoleMapping resource
	globals.UpdateCondition(&securityRoleMapping.Status.Conditions, condition)
}

// SetSyncing updates the status to Syncing phase
func (r *SecurityRoleMappingReconciler) SetSyncing(ctx context.Context, resource *v1alpha1.SecurityRoleMapping) {
	logger := log.FromContext(ctx)
	resource.Status.Phase = controller.PhaseSyncing
	resource.Status.Message = "Synchronizing with OpenSearch"
	if err := r.Status().Update(ctx, resource); err != nil {
		logger.Error(err, "Failed to update status to Syncing")
	}
}

// SetReady updates the status to Ready phase with applied resources
func (r *SecurityRoleMappingReconciler) SetReady(ctx context.Context, resource *v1alpha1.SecurityRoleMapping, targetCluster string, appliedResources []string) error {
	now := metav1.Now()
	resource.Status.Phase = controller.PhaseReady
	resource.Status.Message = fmt.Sprintf("Successfully synced %d role mappings", len(appliedResources))
	resource.Status.TargetCluster = targetCluster
	resource.Status.AppliedResources = appliedResources
	resource.Status.LastSyncTime = &now
	return r.Status().Update(ctx, resource)
}

// SetError updates the status to Error phase with error message
func (r *SecurityRoleMappingReconciler) SetError(ctx context.Context, resource *v1alpha1.SecurityRoleMapping, err error) {
	resource.Status.Phase = controller.PhaseError
	resource.Status.Message = err.Error()
	_ = r.Status().Update(ctx, resource)
}

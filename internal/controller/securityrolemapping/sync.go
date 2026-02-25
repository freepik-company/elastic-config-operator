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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/elastic/go-elasticsearch/v8"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"elastic-config-operator.freepik.com/elastic-config-operator/api/v1alpha1"
	"elastic-config-operator.freepik.com/elastic-config-operator/internal/globals"
)

// Sync executes the synchronization of security role mappings with OpenSearch
func (r *SecurityRoleMappingReconciler) Sync(ctx context.Context, eventType watch.EventType, resource *v1alpha1.SecurityRoleMapping) (err error) {

	logger := log.FromContext(ctx)

	// Get the cluster associated to the resource
	if resource.Spec.ResourceSelector.Namespace == "" {
		resource.Spec.ResourceSelector.Namespace = resource.Namespace
	}

	// Build the cluster key for the pools
	clusterKey := fmt.Sprintf("%s_%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)

	if eventType == watch.Deleted {
		logger.Info(fmt.Sprintf("Deleting SecurityRoleMapping %s/%s", resource.Namespace, resource.Name))

		// Get OpenSearch connection to delete the role mappings
		esConnection, err := globals.GetOrCreateElasticsearchConnection(ctx, clusterKey, &resource.Spec.ResourceSelector, resource.Namespace, r.ElasticsearchConnectionsPool)
		if err != nil {
			logger.Error(err, "Failed to get OpenSearch connection for deletion")
			return err
		}

		// Delete each security role mapping from OpenSearch
		for roleMappingName := range resource.Spec.Resources {
			logger.Info(fmt.Sprintf("Deleting security role mapping %s from OpenSearch", roleMappingName))
			if err := r.deleteSecurityRoleMapping(ctx, esConnection.Client, roleMappingName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete security role mapping %s", roleMappingName))
				return err
			}
			logger.Info(fmt.Sprintf("Security role mapping %s deleted successfully", roleMappingName))
		}

		return nil
	}

	logger.Info(fmt.Sprintf("Syncing SecurityRoleMapping %s/%s", resource.Namespace, resource.Name))

	// Set status to Syncing at the beginning
	r.SetSyncing(ctx, resource)

	// Step 1: Get or create OpenSearch connection
	esConnection, err := globals.GetOrCreateElasticsearchConnection(ctx, clusterKey, &resource.Spec.ResourceSelector, resource.Namespace, r.ElasticsearchConnectionsPool)
	if err != nil {
		logger.Error(err, "Failed to get or create OpenSearch connection")
		r.SetError(ctx, resource, fmt.Errorf("failed to connect to OpenSearch: %w", err))
		return err
	}

	logger.Info(fmt.Sprintf("OpenSearch connection established for cluster %s (type: %s, version: %s)", clusterKey, esConnection.ClusterType, esConnection.Version))

	// Validate cluster type - Security Plugin is only available in OpenSearch
	if esConnection.ClusterType == "elasticsearch" {
		err := fmt.Errorf("Security Plugin role mappings API is only available in OpenSearch. Please use appropriate Elasticsearch security mechanisms for Elasticsearch clusters")
		logger.Error(err, "Incompatible cluster type for SecurityRoleMapping")
		r.SetError(ctx, resource, err)
		return err
	}

	// Step 2: Get the list of role mappings currently applied (from Status)
	appliedRoleMappings := make(map[string]bool)
	for _, roleMappingName := range resource.Status.AppliedResources {
		appliedRoleMappings[roleMappingName] = true
	}

	// Step 3: Get the list of desired role mappings (from Spec)
	desiredRoleMappings := make(map[string]bool)
	for roleMappingName := range resource.Spec.Resources {
		desiredRoleMappings[roleMappingName] = true
	}

	// Step 4: Delete role mappings that are no longer desired
	for roleMappingName := range appliedRoleMappings {
		if !desiredRoleMappings[roleMappingName] {
			logger.Info(fmt.Sprintf("Role mapping %s is no longer desired, deleting from OpenSearch", roleMappingName))
			if err := r.deleteSecurityRoleMapping(ctx, esConnection.Client, roleMappingName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete security role mapping %s", roleMappingName))
				r.SetError(ctx, resource, fmt.Errorf("failed to delete security role mapping %s: %w", roleMappingName, err))
				return err
			}
			logger.Info(fmt.Sprintf("Security role mapping %s deleted successfully", roleMappingName))
		}
	}

	// Step 5: Apply all desired role mappings (idempotent)
	newAppliedRoleMappings := make([]string, 0, len(resource.Spec.Resources))
	for roleMappingName, roleMappingResource := range resource.Spec.Resources {
		logger.Info(fmt.Sprintf("Processing security role mapping: %s", roleMappingName))

		// Parse the desired role mapping from the resource
		var desiredRoleMapping map[string]interface{}
		roleMappingJSON, err := roleMappingResource.MarshalJSON()
		if err != nil {
			logger.Error(err, fmt.Sprintf("Failed to marshal role mapping %s", roleMappingName))
			r.SetError(ctx, resource, fmt.Errorf("failed to marshal role mapping %s: %w", roleMappingName, err))
			return err
		}
		if err := json.Unmarshal(roleMappingJSON, &desiredRoleMapping); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to unmarshal role mapping %s", roleMappingName))
			r.SetError(ctx, resource, fmt.Errorf("failed to unmarshal role mapping %s: %w", roleMappingName, err))
			return err
		}

		// Apply the role mapping (OpenSearch Security PUT is idempotent - creates or updates)
		if err := r.applySecurityRoleMapping(ctx, esConnection.Client, roleMappingName, desiredRoleMapping); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to apply security role mapping %s", roleMappingName))
			r.SetError(ctx, resource, fmt.Errorf("failed to apply security role mapping %s: %w", roleMappingName, err))
			return err
		}
		logger.Info(fmt.Sprintf("Security role mapping %s applied successfully", roleMappingName))
		newAppliedRoleMappings = append(newAppliedRoleMappings, roleMappingName)
	}

	// Step 6: Update the Status with the new list of applied role mappings
	targetCluster := fmt.Sprintf("%s/%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)
	if err := r.SetReady(ctx, resource, targetCluster, newAppliedRoleMappings); err != nil {
		logger.Error(err, "Failed to update SecurityRoleMapping status")
		r.SetError(ctx, resource, fmt.Errorf("failed to update SecurityRoleMapping status: %w", err))
		return err
	}

	logger.Info(fmt.Sprintf("SecurityRoleMapping %s/%s synced successfully", resource.Namespace, resource.Name))

	return nil
}

// applySecurityRoleMapping creates or updates a security role mapping in OpenSearch
func (r *SecurityRoleMappingReconciler) applySecurityRoleMapping(ctx context.Context, esClient *elasticsearch.Client, roleMappingName string, roleMapping map[string]interface{}) error {
	logger := log.FromContext(ctx)

	// Marshal the role mapping to JSON (sent as-is, no wrapping)
	roleMappingJSON, err := json.Marshal(roleMapping)
	if err != nil {
		return fmt.Errorf("failed to marshal role mapping: %w", err)
	}

	logger.Info(fmt.Sprintf("Applying security role mapping %s to OpenSearch", roleMappingName))

	// Apply the security role mapping using OpenSearch Security Plugin API
	// PUT /_plugins/_security/api/rolesmapping/{role_name}
	req, err := http.NewRequestWithContext(ctx, "PUT",
		fmt.Sprintf("/_plugins/_security/api/rolesmapping/%s", roleMappingName),
		bytes.NewReader(roleMappingJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := esClient.Perform(req)
	if err != nil {
		return fmt.Errorf("failed to apply security role mapping: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("OpenSearch API error: %s - %s", res.Status, string(bodyBytes))
	}

	return nil
}

// deleteSecurityRoleMapping deletes a security role mapping from OpenSearch
func (r *SecurityRoleMappingReconciler) deleteSecurityRoleMapping(ctx context.Context, esClient *elasticsearch.Client, roleMappingName string) error {
	logger := log.FromContext(ctx)

	logger.Info(fmt.Sprintf("Deleting security role mapping %s from OpenSearch", roleMappingName))

	// Delete the security role mapping using OpenSearch Security Plugin API
	// DELETE /_plugins/_security/api/rolesmapping/{role_name}
	req, err := http.NewRequestWithContext(ctx, "DELETE",
		fmt.Sprintf("/_plugins/_security/api/rolesmapping/%s", roleMappingName),
		nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	res, err := esClient.Perform(req)
	if err != nil {
		return fmt.Errorf("failed to delete security role mapping: %w", err)
	}
	defer res.Body.Close()

	// If the role mapping doesn't exist (404), consider it already deleted
	if res.StatusCode == http.StatusNotFound {
		logger.Info(fmt.Sprintf("Security role mapping %s not found in OpenSearch (already deleted)", roleMappingName))
		return nil
	}

	if res.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("OpenSearch API error: %s - %s", res.Status, string(bodyBytes))
	}

	return nil
}

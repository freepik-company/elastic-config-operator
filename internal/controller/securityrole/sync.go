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

package securityrole

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
	"elastic-config-operator.freepik.com/elastic-config-operator/internal/controller"
	"elastic-config-operator.freepik.com/elastic-config-operator/internal/globals"
)

// Sync executes the synchronization of security roles with OpenSearch
func (r *SecurityRoleReconciler) Sync(ctx context.Context, eventType watch.EventType, resource *v1alpha1.SecurityRole) (err error) {

	logger := log.FromContext(ctx)

	// Get the cluster associated to the resource
	if resource.Spec.ResourceSelector.Namespace == "" {
		resource.Spec.ResourceSelector.Namespace = resource.Namespace
	}

	// Build the cluster key for the pools
	clusterKey := fmt.Sprintf("%s_%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)

	if eventType == watch.Deleted {
		logger.Info(fmt.Sprintf("Deleting SecurityRole %s/%s", resource.Namespace, resource.Name))

		// Get OpenSearch connection to delete the roles
		esConnection, err := globals.GetOrCreateElasticsearchConnection(ctx, clusterKey, &resource.Spec.ResourceSelector, resource.Namespace, r.ElasticsearchConnectionsPool)
		if err != nil {
			logger.Error(err, "Failed to get OpenSearch connection for deletion")
			return err
		}

		// Delete each security role from OpenSearch
		for roleName := range resource.Spec.Resources {
			logger.Info(fmt.Sprintf("Deleting security role %s from OpenSearch", roleName))
			if err := r.deleteSecurityRole(ctx, esConnection.Client, roleName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete security role %s", roleName))
				return err
			}
			logger.Info(fmt.Sprintf("Security role %s deleted successfully", roleName))
		}

		return nil
	}

	logger.Info(fmt.Sprintf("Syncing SecurityRole %s/%s", resource.Namespace, resource.Name))

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
		err := fmt.Errorf("Security Plugin roles API is only available in OpenSearch. Please use appropriate Elasticsearch security mechanisms for Elasticsearch clusters")
		logger.Error(err, "Incompatible cluster type for SecurityRole")
		r.SetError(ctx, resource, err)
		return err
	}

	// Step 2: Get the list of roles currently applied (from Status)
	appliedRoles := make(map[string]bool)
	for _, roleName := range resource.Status.AppliedResources {
		appliedRoles[roleName] = true
	}

	// Step 3: Get the list of desired roles (from Spec)
	desiredRoles := make(map[string]bool)
	for roleName := range resource.Spec.Resources {
		desiredRoles[roleName] = true
	}

	// Step 4: Delete roles that are no longer desired
	for roleName := range appliedRoles {
		if !desiredRoles[roleName] {
			logger.Info(fmt.Sprintf("Role %s is no longer desired, deleting from OpenSearch", roleName))
			if err := r.deleteSecurityRole(ctx, esConnection.Client, roleName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete security role %s", roleName))
				r.SetError(ctx, resource, fmt.Errorf("failed to delete security role %s: %w", roleName, err))
				return err
			}
			logger.Info(fmt.Sprintf("Security role %s deleted successfully", roleName))
		}
	}

	// Step 5: Apply all desired roles (idempotent)
	newAppliedRoles := make([]string, 0, len(resource.Spec.Resources))
	for roleName, roleResource := range resource.Spec.Resources {
		logger.Info(fmt.Sprintf("Processing security role: %s", roleName))

		// Parse the desired role from the resource
		var desiredRole map[string]interface{}
		roleJSON, err := roleResource.MarshalJSON()
		if err != nil {
			logger.Error(err, fmt.Sprintf("Failed to marshal role %s", roleName))
			r.SetError(ctx, resource, fmt.Errorf("failed to marshal role %s: %w", roleName, err))
			return err
		}
		if err := json.Unmarshal(roleJSON, &desiredRole); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to unmarshal role %s", roleName))
			r.SetError(ctx, resource, fmt.Errorf("failed to unmarshal role %s: %w", roleName, err))
			return err
		}

		// Apply the role (OpenSearch Security PUT is idempotent - creates or updates)
		if err := r.applySecurityRole(ctx, esConnection.Client, roleName, desiredRole); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to apply security role %s", roleName))
			r.SetError(ctx, resource, fmt.Errorf("failed to apply security role %s: %w", roleName, err))
			return err
		}
		logger.Info(fmt.Sprintf("Security role %s applied successfully", roleName))
		newAppliedRoles = append(newAppliedRoles, roleName)
	}

	// Step 6: Update the Status with the new list of applied roles
	targetCluster := fmt.Sprintf("%s/%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)
	if err := r.SetReady(ctx, resource, targetCluster, newAppliedRoles); err != nil {
		logger.Error(err, "Failed to update SecurityRole status")
		r.SetError(ctx, resource, fmt.Errorf("failed to update SecurityRole status: %w", err))
		return err
	}

	logger.Info(fmt.Sprintf("SecurityRole %s/%s synced successfully", resource.Namespace, resource.Name))

	return nil
}

// applySecurityRole creates or updates a security role in OpenSearch
func (r *SecurityRoleReconciler) applySecurityRole(ctx context.Context, esClient *elasticsearch.Client, roleName string, role map[string]interface{}) error {
	logger := log.FromContext(ctx)

	// Marshal the role to JSON (sent as-is, no wrapping)
	roleJSON, err := json.Marshal(role)
	if err != nil {
		return fmt.Errorf("failed to marshal role: %w", err)
	}

	logger.Info(fmt.Sprintf("Applying security role %s to OpenSearch", roleName))

	// Check if role exists and compare for drift
	getReq, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("/_plugins/_security/api/roles/%s", roleName), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	getRes, err := esClient.Perform(getReq)
	if err != nil {
		logger.Info(fmt.Sprintf("Failed to check security role %s existence, proceeding with apply: %v", roleName, err))
	} else {
		if getRes.StatusCode == http.StatusOK {
			var getBody map[string]interface{}
			if err := json.NewDecoder(getRes.Body).Decode(&getBody); err == nil {
				if currentRole, ok := getBody[roleName]; ok {
					if controller.IsSubsetMatch(role, currentRole) {
						logger.Info(fmt.Sprintf("No drift detected for security role %s, skipping apply", roleName))
						getRes.Body.Close()
						return nil
					}
				}
			}
			getRes.Body.Close()
		} else {
			getRes.Body.Close()
		}
	}

	// Apply the security role using OpenSearch Security Plugin API
	// PUT /_plugins/_security/api/roles/{role_name}
	req, err := http.NewRequestWithContext(ctx, "PUT",
		fmt.Sprintf("/_plugins/_security/api/roles/%s", roleName),
		bytes.NewReader(roleJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := esClient.Perform(req)
	if err != nil {
		return fmt.Errorf("failed to apply security role: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("OpenSearch API error: %s - %s", res.Status, string(bodyBytes))
	}

	return nil
}

// deleteSecurityRole deletes a security role from OpenSearch
func (r *SecurityRoleReconciler) deleteSecurityRole(ctx context.Context, esClient *elasticsearch.Client, roleName string) error {
	logger := log.FromContext(ctx)

	logger.Info(fmt.Sprintf("Deleting security role %s from OpenSearch", roleName))

	// Delete the security role using OpenSearch Security Plugin API
	// DELETE /_plugins/_security/api/roles/{role_name}
	req, err := http.NewRequestWithContext(ctx, "DELETE",
		fmt.Sprintf("/_plugins/_security/api/roles/%s", roleName),
		nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	res, err := esClient.Perform(req)
	if err != nil {
		return fmt.Errorf("failed to delete security role: %w", err)
	}
	defer res.Body.Close()

	// If the role doesn't exist (404), consider it already deleted
	if res.StatusCode == http.StatusNotFound {
		logger.Info(fmt.Sprintf("Security role %s not found in OpenSearch (already deleted)", roleName))
		return nil
	}

	if res.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("OpenSearch API error: %s - %s", res.Status, string(bodyBytes))
	}

	return nil
}

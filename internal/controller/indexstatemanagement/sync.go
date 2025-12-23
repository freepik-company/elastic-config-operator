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

package indexstatemanagement

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

// Sync executes the synchronization of ISM policies with OpenSearch
func (r *IndexStateManagementReconciler) Sync(ctx context.Context, eventType watch.EventType, resource *v1alpha1.IndexStateManagement) (err error) {

	logger := log.FromContext(ctx)

	// Get the cluster associated to the resource
	if resource.Spec.ResourceSelector.Namespace == "" {
		resource.Spec.ResourceSelector.Namespace = resource.Namespace
	}

	// Build the cluster key for the pools
	clusterKey := fmt.Sprintf("%s_%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)

	if eventType == watch.Deleted {
		logger.Info(fmt.Sprintf("Deleting IndexStateManagement %s/%s", resource.Namespace, resource.Name))

		// Get OpenSearch connection to delete the policies
		esConnection, err := globals.GetOrCreateElasticsearchConnection(ctx, clusterKey, &resource.Spec.ResourceSelector, r.ElasticsearchConnectionsPool)
		if err != nil {
			logger.Error(err, "Failed to get OpenSearch connection for deletion")
			return err
		}

		// Delete each ISM policy from OpenSearch
		for policyName := range resource.Spec.Resources {
			logger.Info(fmt.Sprintf("Deleting ISM policy %s from OpenSearch", policyName))
			if err := r.deleteISMPolicy(ctx, esConnection.Client, policyName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete ISM policy %s", policyName))
				return err
			}
			logger.Info(fmt.Sprintf("ISM policy %s deleted successfully", policyName))
		}

		return nil
	}

	logger.Info(fmt.Sprintf("Syncing IndexStateManagement %s/%s", resource.Namespace, resource.Name))

	// Set status to Syncing at the beginning
	r.SetSyncing(ctx, resource)

	// Step 1: Get or create OpenSearch connection
	esConnection, err := globals.GetOrCreateElasticsearchConnection(ctx, clusterKey, &resource.Spec.ResourceSelector, r.ElasticsearchConnectionsPool)
	if err != nil {
		logger.Error(err, "Failed to get or create OpenSearch connection")
		r.SetError(ctx, resource, fmt.Errorf("failed to connect to OpenSearch: %w", err))
		return err
	}

	logger.Info(fmt.Sprintf("OpenSearch connection established for cluster %s (type: %s, version: %s)", clusterKey, esConnection.ClusterType, esConnection.Version))

	// Validate cluster type - ISM is only available in OpenSearch
	if esConnection.ClusterType == "elasticsearch" {
		err := fmt.Errorf("ISM (Index State Management) is only available in OpenSearch. Elasticsearch uses ILM (Index Lifecycle Management) instead. Please use the IndexLifecyclePolicy CRD for Elasticsearch clusters")
		logger.Error(err, "Incompatible cluster type for IndexStateManagement")
		r.SetError(ctx, resource, err)
		return err
	}

	// Step 2: Get the list of policies currently applied (from Status)
	appliedPolicies := make(map[string]bool)
	for _, policyName := range resource.Status.AppliedResources {
		appliedPolicies[policyName] = true
	}

	// Step 3: Get the list of desired policies (from Spec)
	desiredPolicies := make(map[string]bool)
	for policyName := range resource.Spec.Resources {
		desiredPolicies[policyName] = true
	}

	// Step 4: Delete policies that are no longer desired
	for policyName := range appliedPolicies {
		if !desiredPolicies[policyName] {
			logger.Info(fmt.Sprintf("Policy %s is no longer desired, deleting from OpenSearch", policyName))
			if err := r.deleteISMPolicy(ctx, esConnection.Client, policyName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete ISM policy %s", policyName))
				return err
			}
			logger.Info(fmt.Sprintf("ISM policy %s deleted successfully", policyName))
		}
	}

	// Step 5: Apply all desired policies (idempotent)
	newAppliedPolicies := make([]string, 0, len(resource.Spec.Resources))
	for policyName, policyResource := range resource.Spec.Resources {
		logger.Info(fmt.Sprintf("Processing ISM policy: %s", policyName))

		// Parse the desired policy from the resource
		var desiredPolicy map[string]interface{}
		policyJSON, err := policyResource.MarshalJSON()
		if err != nil {
			logger.Error(err, fmt.Sprintf("Failed to marshal policy %s", policyName))
			return err
		}
		if err := json.Unmarshal(policyJSON, &desiredPolicy); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to unmarshal policy %s", policyName))
			return err
		}

		// Apply the policy (OpenSearch ISM PUT is idempotent - creates or updates)
		if err := r.applyISMPolicy(ctx, esConnection.Client, policyName, desiredPolicy); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to apply ISM policy %s", policyName))
			return err
		}
		logger.Info(fmt.Sprintf("ISM policy %s applied successfully", policyName))
		newAppliedPolicies = append(newAppliedPolicies, policyName)
	}

	// Step 6: Update the Status with the new list of applied policies
	targetCluster := fmt.Sprintf("%s/%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)
	if err := r.SetReady(ctx, resource, targetCluster, newAppliedPolicies); err != nil {
		logger.Error(err, "Failed to update IndexStateManagement status")
		return err
	}

	logger.Info(fmt.Sprintf("IndexStateManagement %s/%s synced successfully", resource.Namespace, resource.Name))

	return nil
}

// applyISMPolicy creates or updates an ISM policy in OpenSearch
func (r *IndexStateManagementReconciler) applyISMPolicy(ctx context.Context, esClient *elasticsearch.Client, policyName string, policy map[string]interface{}) error {
	logger := log.FromContext(ctx)

	// Wrap the policy in the expected OpenSearch ISM format
	ismRequest := map[string]interface{}{
		"policy": policy,
	}

	// Marshal the policy to JSON
	policyJSON, err := json.Marshal(ismRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal policy: %w", err)
	}

	logger.Info(fmt.Sprintf("Applying ISM policy %s to OpenSearch", policyName))

	// Apply the ISM policy using OpenSearch ISM API
	// PUT /_plugins/_ism/policies/{policy_name}
	req, err := http.NewRequestWithContext(ctx, "PUT",
		fmt.Sprintf("/_plugins/_ism/policies/%s", policyName),
		bytes.NewReader(policyJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := esClient.Perform(req)
	if err != nil {
		return fmt.Errorf("failed to apply ISM policy: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("OpenSearch API error: %s - %s", res.Status, string(bodyBytes))
	}

	return nil
}

// deleteISMPolicy deletes an ISM policy from OpenSearch
func (r *IndexStateManagementReconciler) deleteISMPolicy(ctx context.Context, esClient *elasticsearch.Client, policyName string) error {
	logger := log.FromContext(ctx)

	logger.Info(fmt.Sprintf("Deleting ISM policy %s from OpenSearch", policyName))

	// Delete the ISM policy using OpenSearch ISM API
	// DELETE /_plugins/_ism/policies/{policy_name}
	req, err := http.NewRequestWithContext(ctx, "DELETE",
		fmt.Sprintf("/_plugins/_ism/policies/%s", policyName),
		nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	res, err := esClient.Perform(req)
	if err != nil {
		return fmt.Errorf("failed to delete ISM policy: %w", err)
	}
	defer res.Body.Close()

	// If the policy doesn't exist (404), consider it already deleted
	if res.StatusCode == http.StatusNotFound {
		logger.Info(fmt.Sprintf("ISM policy %s not found in OpenSearch (already deleted)", policyName))
		return nil
	}

	if res.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("OpenSearch API error: %s - %s", res.Status, string(bodyBytes))
	}

	return nil
}

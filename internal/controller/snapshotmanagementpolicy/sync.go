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

package snapshotmanagementpolicy

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

// Sync executes the synchronization of SM policies with OpenSearch
func (r *SnapshotManagementPolicyReconciler) Sync(ctx context.Context, eventType watch.EventType, resource *v1alpha1.SnapshotManagementPolicy) (err error) {

	logger := log.FromContext(ctx)

	// Get the cluster associated to the resource
	if resource.Spec.ResourceSelector.Namespace == "" {
		resource.Spec.ResourceSelector.Namespace = resource.Namespace
	}

	// Build the cluster key for the pools
	clusterKey := fmt.Sprintf("%s_%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)

	if eventType == watch.Deleted {
		logger.Info(fmt.Sprintf("Deleting SnapshotManagementPolicy %s/%s", resource.Namespace, resource.Name))

		// Get OpenSearch connection to delete the policies
		esConnection, err := globals.GetOrCreateElasticsearchConnection(ctx, clusterKey, &resource.Spec.ResourceSelector, resource.Namespace, r.ElasticsearchConnectionsPool)
		if err != nil {
			logger.Error(err, "Failed to get OpenSearch connection for deletion")
			return err
		}

		// Delete each SM policy from OpenSearch
		for policyName := range resource.Spec.Resources {
			logger.Info(fmt.Sprintf("Deleting SM policy %s from OpenSearch", policyName))
			if err := r.deleteSMPolicy(ctx, esConnection.Client, policyName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete SM policy %s", policyName))
				return err
			}
			logger.Info(fmt.Sprintf("SM policy %s deleted successfully", policyName))
		}

		return nil
	}

	logger.Info(fmt.Sprintf("Syncing SnapshotManagementPolicy %s/%s", resource.Namespace, resource.Name))

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

	// Validate cluster type - SM is only available in OpenSearch
	if esConnection.ClusterType == "elasticsearch" {
		err := fmt.Errorf("SM (Snapshot Management) is only available in OpenSearch. Elasticsearch uses SLM (Snapshot Lifecycle Management) instead. Please use the SnapshotLifecyclePolicy CRD for Elasticsearch clusters")
		logger.Error(err, "Incompatible cluster type for SnapshotManagementPolicy")
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
			logger.Info(fmt.Sprintf("SM policy %s is no longer desired, deleting from OpenSearch", policyName))
			if err := r.deleteSMPolicy(ctx, esConnection.Client, policyName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete SM policy %s", policyName))
				r.SetError(ctx, resource, fmt.Errorf("failed to delete SM policy %s: %w", policyName, err))
				return err
			}
			logger.Info(fmt.Sprintf("SM policy %s deleted successfully", policyName))
		}
	}

	// Step 5: Apply all desired policies (idempotent)
	newAppliedPolicies := make([]string, 0, len(resource.Spec.Resources))
	for policyName, policyResource := range resource.Spec.Resources {
		logger.Info(fmt.Sprintf("Processing SM policy: %s", policyName))

		// Parse the desired policy from the resource
		var desiredPolicy map[string]interface{}
		policyJSON, err := policyResource.MarshalJSON()
		if err != nil {
			logger.Error(err, fmt.Sprintf("Failed to marshal policy %s", policyName))
			r.SetError(ctx, resource, fmt.Errorf("failed to marshal policy %s: %w", policyName, err))
			return err
		}
		if err := json.Unmarshal(policyJSON, &desiredPolicy); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to unmarshal policy %s", policyName))
			r.SetError(ctx, resource, fmt.Errorf("failed to unmarshal policy %s: %w", policyName, err))
			return err
		}

		// Apply the policy using OpenSearch SM API
		if err := r.applySMPolicy(ctx, esConnection.Client, policyName, desiredPolicy); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to apply SM policy %s", policyName))
			r.SetError(ctx, resource, fmt.Errorf("failed to apply SM policy %s: %w", policyName, err))
			return err
		}
		logger.Info(fmt.Sprintf("SM policy %s applied successfully", policyName))
		newAppliedPolicies = append(newAppliedPolicies, policyName)
	}

	// Step 6: Update the Status with the new list of applied policies
	targetCluster := fmt.Sprintf("%s/%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)
	if err := r.SetReady(ctx, resource, targetCluster, newAppliedPolicies); err != nil {
		logger.Error(err, "Failed to update SnapshotManagementPolicy status")
		r.SetError(ctx, resource, fmt.Errorf("failed to update SnapshotManagementPolicy status: %w", err))
		return err
	}

	logger.Info(fmt.Sprintf("SnapshotManagementPolicy %s/%s synced successfully", resource.Namespace, resource.Name))

	return nil
}

// applySMPolicy creates or updates a Snapshot Management policy in OpenSearch
func (r *SnapshotManagementPolicyReconciler) applySMPolicy(ctx context.Context, esClient *elasticsearch.Client, policyName string, policy map[string]interface{}) error {
	logger := log.FromContext(ctx)

	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("failed to marshal policy: %w", err)
	}

	logger.Info(fmt.Sprintf("Applying SM policy %s to OpenSearch", policyName))

	// Check if policy exists first
	// GET _plugins/_sm/policies/{name}
	getReq, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("/_plugins/_sm/policies/%s", policyName), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	getRes, err := esClient.Perform(getReq)
	if err != nil {
		return fmt.Errorf("failed to check SM policy existence: %w", err)
	}

	// Use POST for creation, PUT for update
	var method, url string
	if getRes.StatusCode == http.StatusOK {
		// Read and parse the full GET response
		bodyBytes, err := io.ReadAll(getRes.Body)
		getRes.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to read SM policy response: %w", err)
		}

		var getResponse map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &getResponse); err != nil {
			return fmt.Errorf("failed to parse SM policy response: %w", err)
		}

		// Check for drift using subset comparison
		if currentSMPolicy, ok := getResponse["sm_policy"]; ok {
			if controller.IsSubsetMatch(policy, currentSMPolicy) {
				logger.Info(fmt.Sprintf("No drift detected for SM policy %s, skipping apply", policyName))
				return nil
			}
		}

		// Extract seq_no and primary_term for update
		seqNo, _ := getResponse["_seq_no"].(float64)
		primaryTerm, _ := getResponse["_primary_term"].(float64)

		method = "PUT"
		url = fmt.Sprintf("/_plugins/_sm/policies/%s?if_seq_no=%.0f&if_primary_term=%.0f",
			policyName, seqNo, primaryTerm)
		logger.Info(fmt.Sprintf("Updating existing SM policy %s (seq_no: %.0f, primary_term: %.0f)", policyName, seqNo, primaryTerm))
	} else {
		getRes.Body.Close()
		method = "POST"
		url = fmt.Sprintf("/_plugins/_sm/policies/%s", policyName)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(policyJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := esClient.Perform(req)
	if err != nil {
		return fmt.Errorf("failed to apply SM policy: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("OpenSearch API error: %s - %s", res.Status, string(bodyBytes))
	}

	return nil
}

// deleteSMPolicy deletes a Snapshot Management policy from OpenSearch
func (r *SnapshotManagementPolicyReconciler) deleteSMPolicy(ctx context.Context, esClient *elasticsearch.Client, policyName string) error {
	logger := log.FromContext(ctx)

	logger.Info(fmt.Sprintf("Deleting SM policy %s from OpenSearch", policyName))

	// DELETE _plugins/_sm/policies/{name}
	req, err := http.NewRequestWithContext(ctx, "DELETE",
		fmt.Sprintf("/_plugins/_sm/policies/%s", policyName),
		nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	res, err := esClient.Perform(req)
	if err != nil {
		return fmt.Errorf("failed to delete SM policy: %w", err)
	}
	defer res.Body.Close()

	// If the policy doesn't exist (404), consider it already deleted
	if res.StatusCode == http.StatusNotFound {
		logger.Info(fmt.Sprintf("SM policy %s not found in OpenSearch (already deleted)", policyName))
		return nil
	}

	if res.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("OpenSearch API error: %s - %s", res.Status, string(bodyBytes))
	}

	return nil
}

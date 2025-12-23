/*
Copyright 2024.

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

package snapshotlifecyclepolicy

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

	//
	"eck-config-operator.freepik.com/eck-config-operator/api/v1alpha1"
	"eck-config-operator.freepik.com/eck-config-operator/internal/globals"
)

// Sync execute the query to the elasticsearch and evaluate the condition. Then trigger the action adding the alert to the pool
// and sending an event to the Kubernetes API
func (r *SnapshotLifecyclePolicyReconciler) Sync(ctx context.Context, eventType watch.EventType, resource *v1alpha1.SnapshotLifecyclePolicy) (err error) {

	logger := log.FromContext(ctx)

	// Get the ECK cluster associated to the resource
	if resource.Spec.ResourceSelector.Namespace == "" {
		resource.Spec.ResourceSelector.Namespace = resource.Namespace
	}

	// Build the cluster key for the pools
	clusterKey := fmt.Sprintf("%s_%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)

	if eventType == watch.Deleted {
		logger.Info(fmt.Sprintf("Deleting SnapshotLifecyclePolicy %s/%s", resource.Namespace, resource.Name))

		// Get Elasticsearch connection to delete the policies
		esConnection, err := globals.GetOrCreateElasticsearchConnection(ctx, clusterKey, &resource.Spec.ResourceSelector, r.ElasticsearchConnectionsPool)
		if err != nil {
			logger.Error(err, "Failed to get Elasticsearch connection for deletion")
			return err
		}

		// Delete each snapshot lifecycle policy from Elasticsearch
		for policyName := range resource.Spec.Resources {
			logger.Info(fmt.Sprintf("Deleting snapshot lifecycle policy %s from Elasticsearch", policyName))
			if err := r.deleteSnapshotLifecyclePolicy(ctx, esConnection.Client, policyName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete snapshot lifecycle policy %s", policyName))
				return err
			}
			logger.Info(fmt.Sprintf("Snapshot lifecycle policy %s deleted successfully", policyName))
		}

		return nil
	}

	logger.Info(fmt.Sprintf("Syncing SnapshotLifecyclePolicy %s/%s", resource.Namespace, resource.Name))

	// Set status to Syncing at the beginning
	r.SetSyncing(ctx, resource)

	// Step 1: Get or create Elasticsearch connection
	esConnection, err := globals.GetOrCreateElasticsearchConnection(ctx, clusterKey, &resource.Spec.ResourceSelector, r.ElasticsearchConnectionsPool)
	if err != nil {
		logger.Error(err, "Failed to get or create Elasticsearch connection")
		r.SetError(ctx, resource, fmt.Errorf("failed to connect to Elasticsearch: %w", err))
		return err
	}

	logger.Info(fmt.Sprintf("Elasticsearch connection established for cluster %s", clusterKey))

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
			logger.Info(fmt.Sprintf("Policy %s is no longer desired, deleting from Elasticsearch", policyName))
			if err := r.deleteSnapshotLifecyclePolicy(ctx, esConnection.Client, policyName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete snapshot lifecycle policy %s", policyName))
				return err
			}
			logger.Info(fmt.Sprintf("Snapshot lifecycle policy %s deleted successfully", policyName))
		}
	}

	// Step 5: Apply all desired policies (idempotent)
	newAppliedPolicies := make([]string, 0, len(resource.Spec.Resources))
	for policyName, policyResource := range resource.Spec.Resources {
		logger.Info(fmt.Sprintf("Processing snapshot lifecycle policy: %s", policyName))

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

		// Apply the policy (PutLifecycle is idempotent - creates or updates)
		if err := r.applySnapshotLifecyclePolicy(ctx, esConnection.Client, policyName, desiredPolicy); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to apply snapshot lifecycle policy %s", policyName))
			return err
		}
		logger.Info(fmt.Sprintf("Snapshot lifecycle policy %s applied successfully", policyName))
		newAppliedPolicies = append(newAppliedPolicies, policyName)
	}

	// Step 6: Update the Status with the new list of applied policies
	if err := r.SetReady(ctx, resource, newAppliedPolicies); err != nil {
		logger.Error(err, "Failed to update SnapshotLifecyclePolicy status")
		return err
	}

	logger.Info(fmt.Sprintf("SnapshotLifecyclePolicy %s/%s synced successfully", resource.Namespace, resource.Name))

	return nil
}

// applySnapshotLifecyclePolicy creates or updates a snapshot lifecycle policy in Elasticsearch
func (r *SnapshotLifecyclePolicyReconciler) applySnapshotLifecyclePolicy(ctx context.Context, esClient *elasticsearch.Client, policyName string, policy map[string]interface{}) error {
	logger := log.FromContext(ctx)

	// Marshal the policy to JSON
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("failed to marshal policy: %w", err)
	}

	logger.Info(fmt.Sprintf("Applying snapshot lifecycle policy %s", policyName))

	// Apply the snapshot lifecycle policy using the SLM API
	res, err := esClient.SlmPutLifecycle(
		policyName,
		esClient.SlmPutLifecycle.WithBody(bytes.NewReader(policyJSON)),
		esClient.SlmPutLifecycle.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to apply snapshot lifecycle policy: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("elasticsearch API error: %s - %s", res.Status(), string(bodyBytes))
	}

	return nil
}

// deleteSnapshotLifecyclePolicy deletes a snapshot lifecycle policy from Elasticsearch
func (r *SnapshotLifecyclePolicyReconciler) deleteSnapshotLifecyclePolicy(ctx context.Context, esClient *elasticsearch.Client, policyName string) error {
	logger := log.FromContext(ctx)

	logger.Info(fmt.Sprintf("Deleting snapshot lifecycle policy %s from Elasticsearch", policyName))

	// Delete the snapshot lifecycle policy using the SLM API
	res, err := esClient.SlmDeleteLifecycle(
		policyName,
		esClient.SlmDeleteLifecycle.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to delete snapshot lifecycle policy: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		// If the policy doesn't exist (404), consider it already deleted
		if res.StatusCode == http.StatusNotFound {
			logger.Info(fmt.Sprintf("Snapshot lifecycle policy %s not found in Elasticsearch (already deleted)", policyName))
			return nil
		}
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("elasticsearch API error: %s - %s", res.Status(), string(bodyBytes))
	}

	return nil
}

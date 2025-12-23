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

package snapshotrepository

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
func (r *SnapshotRepositoryReconciler) Sync(ctx context.Context, eventType watch.EventType, resource *v1alpha1.SnapshotRepository) (err error) {

	logger := log.FromContext(ctx)

	// Get the ECK cluster associated to the resource
	if resource.Spec.ResourceSelector.Namespace == "" {
		resource.Spec.ResourceSelector.Namespace = resource.Namespace
	}

	// Build the cluster key for the pools
	clusterKey := fmt.Sprintf("%s_%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)

	if eventType == watch.Deleted {
		logger.Info(fmt.Sprintf("Deleting SnapshotRepository %s/%s", resource.Namespace, resource.Name))

		// Get Elasticsearch connection to delete the repositories
		esConnection, err := globals.GetOrCreateElasticsearchConnection(ctx, clusterKey, &resource.Spec.ResourceSelector, r.ElasticsearchConnectionsPool)
		if err != nil {
			logger.Error(err, "Failed to get Elasticsearch connection for deletion")
			return err
		}

		// Delete each snapshot repository from Elasticsearch
		for repoName := range resource.Spec.Resources {
			logger.Info(fmt.Sprintf("Deleting snapshot repository %s from Elasticsearch", repoName))
			if err := r.deleteSnapshotRepository(ctx, esConnection.Client, repoName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete snapshot repository %s", repoName))
				return err
			}
			logger.Info(fmt.Sprintf("Snapshot repository %s deleted successfully", repoName))
		}

		return nil
	}

	logger.Info(fmt.Sprintf("Syncing SnapshotRepository %s/%s", resource.Namespace, resource.Name))

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

	// Step 2: Get the list of repositories currently applied (from Status)
	appliedRepositories := make(map[string]bool)
	for _, repoName := range resource.Status.AppliedResources {
		appliedRepositories[repoName] = true
	}

	// Step 3: Get the list of desired repositories (from Spec)
	desiredRepositories := make(map[string]bool)
	for repoName := range resource.Spec.Resources {
		desiredRepositories[repoName] = true
	}

	// Step 4: Delete repositories that are no longer desired
	for repoName := range appliedRepositories {
		if !desiredRepositories[repoName] {
			logger.Info(fmt.Sprintf("Repository %s is no longer desired, deleting from Elasticsearch", repoName))
			if err := r.deleteSnapshotRepository(ctx, esConnection.Client, repoName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete snapshot repository %s", repoName))
				return err
			}
			logger.Info(fmt.Sprintf("Snapshot repository %s deleted successfully", repoName))
		}
	}

	// Step 5: Apply all desired repositories (idempotent)
	newAppliedRepositories := make([]string, 0, len(resource.Spec.Resources))
	for repoName, repoResource := range resource.Spec.Resources {
		logger.Info(fmt.Sprintf("Processing snapshot repository: %s", repoName))

		// Parse the desired repository from the resource
		var desiredRepository map[string]interface{}
		repoJSON, err := repoResource.MarshalJSON()
		if err != nil {
			logger.Error(err, fmt.Sprintf("Failed to marshal repository %s", repoName))
			return err
		}
		if err := json.Unmarshal(repoJSON, &desiredRepository); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to unmarshal repository %s", repoName))
			return err
		}

		// Apply the repository (CreateRepository is idempotent - creates or updates)
		if err := r.applySnapshotRepository(ctx, esConnection.Client, repoName, desiredRepository); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to apply snapshot repository %s", repoName))
			return err
		}
		logger.Info(fmt.Sprintf("Snapshot repository %s applied successfully", repoName))
		newAppliedRepositories = append(newAppliedRepositories, repoName)
	}

	// Step 6: Update the Status with the new list of applied repositories
	targetCluster := fmt.Sprintf("%s/%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)
	if err := r.SetReady(ctx, resource, targetCluster, newAppliedRepositories); err != nil {
		logger.Error(err, "Failed to update SnapshotRepository status")
		return err
	}

	logger.Info(fmt.Sprintf("SnapshotRepository %s/%s synced successfully", resource.Namespace, resource.Name))

	return nil
}

// applySnapshotRepository creates or updates a snapshot repository in Elasticsearch
func (r *SnapshotRepositoryReconciler) applySnapshotRepository(ctx context.Context, esClient *elasticsearch.Client, repoName string, repository map[string]interface{}) error {
	logger := log.FromContext(ctx)

	// Marshal the repository to JSON
	repoJSON, err := json.Marshal(repository)
	if err != nil {
		return fmt.Errorf("failed to marshal repository: %w", err)
	}

	logger.Info(fmt.Sprintf("Applying snapshot repository %s", repoName))

	// Apply the snapshot repository (CreateRepository is idempotent - creates or updates)
	res, err := esClient.Snapshot.CreateRepository(
		repoName,
		bytes.NewReader(repoJSON),
		esClient.Snapshot.CreateRepository.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to apply snapshot repository: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("elasticsearch API error: %s - %s", res.Status(), string(bodyBytes))
	}

	return nil
}

// deleteSnapshotRepository deletes a snapshot repository from Elasticsearch
func (r *SnapshotRepositoryReconciler) deleteSnapshotRepository(ctx context.Context, esClient *elasticsearch.Client, repoName string) error {
	logger := log.FromContext(ctx)

	logger.Info(fmt.Sprintf("Deleting snapshot repository %s from Elasticsearch", repoName))

	// Delete the snapshot repository
	res, err := esClient.Snapshot.DeleteRepository(
		[]string{repoName},
		esClient.Snapshot.DeleteRepository.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to delete snapshot repository: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		// If the repository doesn't exist (404), consider it already deleted
		if res.StatusCode == http.StatusNotFound {
			logger.Info(fmt.Sprintf("Snapshot repository %s not found in Elasticsearch (already deleted)", repoName))
			return nil
		}
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("elasticsearch API error: %s - %s", res.Status(), string(bodyBytes))
	}

	return nil
}

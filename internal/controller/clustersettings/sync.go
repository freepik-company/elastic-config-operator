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

package clustersettings

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

// Sync executes the synchronization of cluster settings with Elasticsearch
func (r *ClusterSettingsReconciler) Sync(ctx context.Context, eventType watch.EventType, resource *v1alpha1.ClusterSettings) (err error) {

	logger := log.FromContext(ctx)

	// Get the ECK cluster associated to the resource
	if resource.Spec.ResourceSelector.Namespace == "" {
		resource.Spec.ResourceSelector.Namespace = resource.Namespace
	}

	// Build the cluster key for the pools
	clusterKey := fmt.Sprintf("%s_%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)

	if eventType == watch.Deleted {
		logger.Info(fmt.Sprintf("Deleting ClusterSettings %s/%s", resource.Namespace, resource.Name))

		// Get Elasticsearch connection to delete the settings
		esConnection, err := globals.GetOrCreateElasticsearchConnection(ctx, clusterKey, &resource.Spec.ResourceSelector, r.ElasticsearchConnectionsPool)
		if err != nil {
			logger.Error(err, "Failed to get Elasticsearch connection for deletion")
			return err
		}

		// Reset individual cluster settings that were applied (from Status.AppliedResources)
		// Format: "category.setting.path"
		settingsToResetByCategory := make(map[string][]string)
		for _, fullKey := range resource.Status.AppliedResources {
			// Parse category and setting key from "category.setting.path"
			dotIndex := 0
			for i, c := range fullKey {
				if c == '.' {
					dotIndex = i
					break
				}
			}
			if dotIndex > 0 {
				category := fullKey[:dotIndex]
				settingKey := fullKey[dotIndex+1:]
				settingsToResetByCategory[category] = append(settingsToResetByCategory[category], settingKey)
			}
		}

		// Reset settings by category
		for category, settingKeys := range settingsToResetByCategory {
			logger.Info(fmt.Sprintf("Resetting %d cluster settings for category %s", len(settingKeys), category))
			if err := r.resetClusterSettings(ctx, esConnection.Client, category, settingKeys); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to reset cluster settings for category %s", category))
				return err
			}
			logger.Info(fmt.Sprintf("Cluster settings for category %s reset successfully", category))
		}

		return nil
	}

	logger.Info(fmt.Sprintf("Syncing ClusterSettings %s/%s", resource.Namespace, resource.Name))

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

	// Step 2: Get the list of individual settings currently applied (from Status)
	// Format: "category.setting.path" (e.g., "persistent.cluster.routing.allocation.enable")
	appliedSettings := make(map[string]bool)
	for _, settingKey := range resource.Status.AppliedResources {
		appliedSettings[settingKey] = true
	}

	// Step 3: Build the list of desired settings from Spec
	desiredSettings := make(map[string]bool)
	desiredSettingsByCategory := make(map[string]map[string]interface{})

	for category, settingsResource := range resource.Spec.Resources {
		var settings map[string]interface{}
		settingsJSON, err := settingsResource.MarshalJSON()
		if err != nil {
			logger.Error(err, fmt.Sprintf("Failed to marshal settings for category %s", category))
			r.SetError(ctx, resource, fmt.Errorf("failed to marshal settings for category %s: %w", category, err))
			return err
		}
		if err := json.Unmarshal(settingsJSON, &settings); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to unmarshal settings for category %s", category))
			r.SetError(ctx, resource, fmt.Errorf("failed to unmarshal settings for category %s: %w", category, err))
			return err
		}

		desiredSettingsByCategory[category] = settings

		// Build the list of desired setting keys
		for settingKey := range settings {
			fullKey := fmt.Sprintf("%s.%s", category, settingKey)
			desiredSettings[fullKey] = true
		}
	}

	// Step 4: Reset individual settings that are no longer desired
	settingsToReset := make(map[string][]string) // category -> []settingKeys
	for appliedKey := range appliedSettings {
		if !desiredSettings[appliedKey] {
			// Parse category and setting key from "category.setting.path"
			// Split by first dot to get category
			dotIndex := 0
			for i, c := range appliedKey {
				if c == '.' {
					dotIndex = i
					break
				}
			}
			if dotIndex > 0 {
				category := appliedKey[:dotIndex]
				settingKey := appliedKey[dotIndex+1:]
				logger.Info(fmt.Sprintf("Setting %s is no longer desired, will reset it", appliedKey))
				settingsToReset[category] = append(settingsToReset[category], settingKey)
			}
		}
	}

	// Reset settings by category
	for category, settingKeys := range settingsToReset {
		if err := r.resetClusterSettings(ctx, esConnection.Client, category, settingKeys); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to reset cluster settings for category %s", category))
			r.SetError(ctx, resource, fmt.Errorf("failed to reset cluster settings: %w", err))
			return err
		}
		logger.Info(fmt.Sprintf("Reset %d settings in category %s", len(settingKeys), category))
	}

	// Step 5: Apply all desired cluster settings (idempotent)
	newAppliedSettings := make([]string, 0)
	for category, settings := range desiredSettingsByCategory {
		logger.Info(fmt.Sprintf("Processing cluster settings for category: %s", category))

		// Apply the cluster settings (PUT /_cluster/settings is idempotent)
		if err := r.applyClusterSettings(ctx, esConnection.Client, category, settings); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to apply cluster settings for category %s", category))
			r.SetError(ctx, resource, fmt.Errorf("failed to apply cluster settings for category %s: %w", category, err))
			return err
		}

		// Track each individual setting applied
		for settingKey := range settings {
			fullKey := fmt.Sprintf("%s.%s", category, settingKey)
			newAppliedSettings = append(newAppliedSettings, fullKey)
		}

		logger.Info(fmt.Sprintf("Cluster settings for category %s applied successfully (%d settings)", category, len(settings)))
	}

	// Step 6: Update the Status with the new list of applied settings
	targetCluster := fmt.Sprintf("%s/%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)
	if err := r.SetReady(ctx, resource, targetCluster, newAppliedSettings); err != nil {
		logger.Error(err, "Failed to update ClusterSettings status")
		return err
	}

	logger.Info(fmt.Sprintf("ClusterSettings %s/%s synced successfully", resource.Namespace, resource.Name))

	return nil
}

// applyClusterSettings creates or updates cluster settings in Elasticsearch
func (r *ClusterSettingsReconciler) applyClusterSettings(ctx context.Context, esClient *elasticsearch.Client, category string, settings map[string]interface{}) error {
	logger := log.FromContext(ctx)

	// Build the request body: { "category": { ... settings ... } }
	requestBody := map[string]interface{}{
		category: settings,
	}

	// Marshal the request to JSON
	requestJSON, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal cluster settings: %w", err)
	}

	logger.Info(fmt.Sprintf("Applying cluster settings for category %s", category))

	// Apply the cluster settings
	res, err := esClient.Cluster.PutSettings(
		bytes.NewReader(requestJSON),
		esClient.Cluster.PutSettings.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to apply cluster settings: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("elasticsearch API error: %s - %s", res.Status(), string(bodyBytes))
	}

	return nil
}

// resetClusterSettings resets specific cluster settings in Elasticsearch by setting them to null
func (r *ClusterSettingsReconciler) resetClusterSettings(ctx context.Context, esClient *elasticsearch.Client, category string, settingKeys []string) error {
	logger := log.FromContext(ctx)

	logger.Info(fmt.Sprintf("Resetting %d cluster settings in category %s", len(settingKeys), category))

	// To reset cluster settings, we set each individual setting to null
	// This ensures we only reset settings managed by this operator, not all settings in the category
	settingsToReset := make(map[string]interface{})
	for _, settingKey := range settingKeys {
		settingsToReset[settingKey] = nil
		logger.Info(fmt.Sprintf("Will reset setting: %s.%s", category, settingKey))
	}

	// Build the request body: { "category": { "setting1": null, "setting2": null } }
	requestBody := map[string]interface{}{
		category: settingsToReset,
	}

	// Marshal the request to JSON
	requestJSON, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal reset request: %w", err)
	}

	logger.Info(fmt.Sprintf("Resetting cluster settings: %s", string(requestJSON)))

	// Apply the reset (setting individual keys to null)
	res, err := esClient.Cluster.PutSettings(
		bytes.NewReader(requestJSON),
		esClient.Cluster.PutSettings.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to reset cluster settings: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		// If we get an error, but it's because settings don't exist, that's fine
		if res.StatusCode == http.StatusNotFound {
			logger.Info(fmt.Sprintf("Cluster settings for category %s not found (already reset)", category))
			return nil
		}
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("elasticsearch API error: %s - %s", res.Status(), string(bodyBytes))
	}

	logger.Info(fmt.Sprintf("Successfully reset %d settings in category %s", len(settingKeys), category))

	return nil
}

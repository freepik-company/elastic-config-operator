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

package notificationchannel

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

// Sync executes the synchronization of notification channels with OpenSearch
func (r *NotificationChannelReconciler) Sync(ctx context.Context, eventType watch.EventType, resource *v1alpha1.NotificationChannel) (err error) {

	logger := log.FromContext(ctx)

	// Get the cluster associated to the resource
	if resource.Spec.ResourceSelector.Namespace == "" {
		resource.Spec.ResourceSelector.Namespace = resource.Namespace
	}

	// Build the cluster key for the pools
	clusterKey := fmt.Sprintf("%s_%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)

	if eventType == watch.Deleted {
		logger.Info(fmt.Sprintf("Deleting NotificationChannel %s/%s", resource.Namespace, resource.Name))

		// Get OpenSearch connection to delete the channels
		esConnection, err := globals.GetOrCreateElasticsearchConnection(ctx, clusterKey, &resource.Spec.ResourceSelector, resource.Namespace, r.ElasticsearchConnectionsPool)
		if err != nil {
			logger.Error(err, "Failed to get OpenSearch connection for deletion")
			return err
		}

		// Delete each notification channel from OpenSearch
		for channelName := range resource.Spec.Resources {
			logger.Info(fmt.Sprintf("Deleting notification channel %s from OpenSearch", channelName))
			if err := r.deleteNotificationChannel(ctx, esConnection.Client, channelName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete notification channel %s", channelName))
				return err
			}
			logger.Info(fmt.Sprintf("Notification channel %s deleted successfully", channelName))
		}

		return nil
	}

	logger.Info(fmt.Sprintf("Syncing NotificationChannel %s/%s", resource.Namespace, resource.Name))

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

	// Validate cluster type - Notifications API is only available in OpenSearch
	if esConnection.ClusterType == "elasticsearch" {
		err := fmt.Errorf("Notifications API is only available in OpenSearch. Elasticsearch does not support the Notifications plugin. Please use an OpenSearch cluster")
		logger.Error(err, "Incompatible cluster type for NotificationChannel")
		r.SetError(ctx, resource, err)
		return err
	}

	// Step 2: Get the list of channels currently applied (from Status)
	appliedChannels := make(map[string]bool)
	for _, channelName := range resource.Status.AppliedResources {
		appliedChannels[channelName] = true
	}

	// Step 3: Get the list of desired channels (from Spec)
	desiredChannels := make(map[string]bool)
	for channelName := range resource.Spec.Resources {
		desiredChannels[channelName] = true
	}

	// Step 4: Delete channels that are no longer desired
	for channelName := range appliedChannels {
		if !desiredChannels[channelName] {
			logger.Info(fmt.Sprintf("Notification channel %s is no longer desired, deleting from OpenSearch", channelName))
			if err := r.deleteNotificationChannel(ctx, esConnection.Client, channelName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete notification channel %s", channelName))
				r.SetError(ctx, resource, fmt.Errorf("failed to delete notification channel %s: %w", channelName, err))
				return err
			}
			logger.Info(fmt.Sprintf("Notification channel %s deleted successfully", channelName))
		}
	}

	// Step 5: Apply all desired channels (idempotent)
	newAppliedChannels := make([]string, 0, len(resource.Spec.Resources))
	for channelName, channelResource := range resource.Spec.Resources {
		logger.Info(fmt.Sprintf("Processing notification channel: %s", channelName))

		// Parse the desired channel config from the resource
		var desiredConfig map[string]interface{}
		configJSON, err := channelResource.MarshalJSON()
		if err != nil {
			logger.Error(err, fmt.Sprintf("Failed to marshal channel %s", channelName))
			r.SetError(ctx, resource, fmt.Errorf("failed to marshal channel %s: %w", channelName, err))
			return err
		}
		if err := json.Unmarshal(configJSON, &desiredConfig); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to unmarshal channel %s", channelName))
			r.SetError(ctx, resource, fmt.Errorf("failed to unmarshal channel %s: %w", channelName, err))
			return err
		}

		// Apply the channel using OpenSearch Notifications API
		if err := r.applyNotificationChannel(ctx, esConnection.Client, channelName, desiredConfig); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to apply notification channel %s", channelName))
			r.SetError(ctx, resource, fmt.Errorf("failed to apply notification channel %s: %w", channelName, err))
			return err
		}
		logger.Info(fmt.Sprintf("Notification channel %s applied successfully", channelName))
		newAppliedChannels = append(newAppliedChannels, channelName)
	}

	// Step 6: Update the Status with the new list of applied channels
	targetCluster := fmt.Sprintf("%s/%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)
	if err := r.SetReady(ctx, resource, targetCluster, newAppliedChannels); err != nil {
		logger.Error(err, "Failed to update NotificationChannel status")
		r.SetError(ctx, resource, fmt.Errorf("failed to update NotificationChannel status: %w", err))
		return err
	}

	logger.Info(fmt.Sprintf("NotificationChannel %s/%s synced successfully", resource.Namespace, resource.Name))

	return nil
}

// applyNotificationChannel creates or updates a notification channel in OpenSearch
func (r *NotificationChannelReconciler) applyNotificationChannel(ctx context.Context, esClient *elasticsearch.Client, channelID string, config map[string]interface{}) error {
	logger := log.FromContext(ctx)

	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal channel config: %w", err)
	}

	logger.Info(fmt.Sprintf("Applying notification channel %s to OpenSearch", channelID))

	// Check if channel exists first
	// GET /_plugins/_notifications/configs/<config_id>
	getReq, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("/_plugins/_notifications/configs/%s", channelID), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	getRes, err := esClient.Perform(getReq)
	if err != nil {
		return fmt.Errorf("failed to check notification channel existence: %w", err)
	}

	// Use POST for creation, PUT for update
	var method, url string
	if getRes.StatusCode == http.StatusOK {
		getRes.Body.Close()
		method = "PUT"
		url = fmt.Sprintf("/_plugins/_notifications/configs/%s", channelID)
		logger.Info(fmt.Sprintf("Updating existing notification channel %s", channelID))
	} else {
		getRes.Body.Close()
		method = "POST"
		url = "/_plugins/_notifications/configs/"
		// For creation, we need to include the config_id in the body
		// Wrap the config with the ID
		var configWithID map[string]interface{}
		if err := json.Unmarshal(configJSON, &configWithID); err != nil {
			return fmt.Errorf("failed to unmarshal config for ID injection: %w", err)
		}
		configWithID["config_id"] = channelID
		configJSON, err = json.Marshal(configWithID)
		if err != nil {
			return fmt.Errorf("failed to marshal config with ID: %w", err)
		}
		logger.Info(fmt.Sprintf("Creating new notification channel %s", channelID))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(configJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := esClient.Perform(req)
	if err != nil {
		return fmt.Errorf("failed to apply notification channel: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("OpenSearch API error: %s - %s", res.Status, string(bodyBytes))
	}

	return nil
}

// deleteNotificationChannel deletes a notification channel from OpenSearch
func (r *NotificationChannelReconciler) deleteNotificationChannel(ctx context.Context, esClient *elasticsearch.Client, channelID string) error {
	logger := log.FromContext(ctx)

	logger.Info(fmt.Sprintf("Deleting notification channel %s from OpenSearch", channelID))

	// DELETE /_plugins/_notifications/configs/<config_id>
	req, err := http.NewRequestWithContext(ctx, "DELETE",
		fmt.Sprintf("/_plugins/_notifications/configs/%s", channelID),
		nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	res, err := esClient.Perform(req)
	if err != nil {
		return fmt.Errorf("failed to delete notification channel: %w", err)
	}
	defer res.Body.Close()

	// If the channel doesn't exist (404), consider it already deleted
	if res.StatusCode == http.StatusNotFound {
		logger.Info(fmt.Sprintf("Notification channel %s not found in OpenSearch (already deleted)", channelID))
		return nil
	}

	if res.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("OpenSearch API error: %s - %s", res.Status, string(bodyBytes))
	}

	return nil
}

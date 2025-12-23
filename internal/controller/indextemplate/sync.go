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

package indextemplate

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
	"elastic-config-operator.freepik.com/elastic-config-operator/api/v1alpha1"
	"elastic-config-operator.freepik.com/elastic-config-operator/internal/globals"
)

// Sync execute the query to the elasticsearch and evaluate the condition. Then trigger the action adding the alert to the pool
// and sending an event to the Kubernetes API
func (r *IndexTemplateReconciler) Sync(ctx context.Context, eventType watch.EventType, resource *v1alpha1.IndexTemplate) (err error) {

	logger := log.FromContext(ctx)

	// Get the ECK cluster associated to the resource
	if resource.Spec.ResourceSelector.Namespace == "" {
		resource.Spec.ResourceSelector.Namespace = resource.Namespace
	}

	// Build the cluster key for the pools
	clusterKey := fmt.Sprintf("%s_%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)

	if eventType == watch.Deleted {
		logger.Info(fmt.Sprintf("Deleting IndexTemplate %s/%s", resource.Namespace, resource.Name))

		// Get Elasticsearch connection to delete the templates
		esConnection, err := globals.GetOrCreateElasticsearchConnection(ctx, clusterKey, &resource.Spec.ResourceSelector, r.ElasticsearchConnectionsPool)
		if err != nil {
			logger.Error(err, "Failed to get Elasticsearch connection for deletion")
			return err
		}

		// Delete each index template from Elasticsearch
		for templateName := range resource.Spec.Resources {
			logger.Info(fmt.Sprintf("Deleting index template %s from Elasticsearch", templateName))
			if err := r.deleteIndexTemplate(ctx, esConnection.Client, templateName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete index template %s", templateName))
				return err
			}
			logger.Info(fmt.Sprintf("Index template %s deleted successfully", templateName))
		}

		return nil
	}

	logger.Info(fmt.Sprintf("Syncing IndexTemplate %s/%s", resource.Namespace, resource.Name))

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

	// Step 2: Get the list of templates currently applied (from Status)
	appliedTemplates := make(map[string]bool)
	for _, templateName := range resource.Status.AppliedResources {
		appliedTemplates[templateName] = true
	}

	// Step 3: Get the list of desired templates (from Spec)
	desiredTemplates := make(map[string]bool)
	for templateName := range resource.Spec.Resources {
		desiredTemplates[templateName] = true
	}

	// Step 4: Delete templates that are no longer desired
	for templateName := range appliedTemplates {
		if !desiredTemplates[templateName] {
			logger.Info(fmt.Sprintf("Template %s is no longer desired, deleting from Elasticsearch", templateName))
			if err := r.deleteIndexTemplate(ctx, esConnection.Client, templateName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete index template %s", templateName))
				return err
			}
			logger.Info(fmt.Sprintf("Index template %s deleted successfully", templateName))
		}
	}

	// Step 5: Apply all desired templates (idempotent)
	newAppliedTemplates := make([]string, 0, len(resource.Spec.Resources))
	for templateName, templateResource := range resource.Spec.Resources {
		logger.Info(fmt.Sprintf("Processing index template: %s", templateName))

		// Parse the desired template from the resource
		var desiredTemplate map[string]interface{}
		templateJSON, err := templateResource.MarshalJSON()
		if err != nil {
			logger.Error(err, fmt.Sprintf("Failed to marshal template %s", templateName))
			return err
		}
		if err := json.Unmarshal(templateJSON, &desiredTemplate); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to unmarshal template %s", templateName))
			return err
		}

		// Apply the template (PutIndexTemplate is idempotent - creates or updates)
		if err := r.applyIndexTemplate(ctx, esConnection.Client, templateName, desiredTemplate); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to apply index template %s", templateName))
			return err
		}
		logger.Info(fmt.Sprintf("Index template %s applied successfully", templateName))
		newAppliedTemplates = append(newAppliedTemplates, templateName)
	}

	// Step 6: Update the Status with the new list of applied templates
	targetCluster := fmt.Sprintf("%s/%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)
	if err := r.SetReady(ctx, resource, targetCluster, newAppliedTemplates); err != nil {
		logger.Error(err, "Failed to update IndexTemplate status")
		return err
	}

	logger.Info(fmt.Sprintf("IndexTemplate %s/%s synced successfully", resource.Namespace, resource.Name))

	return nil
}

// applyIndexTemplate creates or updates an index template in Elasticsearch
func (r *IndexTemplateReconciler) applyIndexTemplate(ctx context.Context, esClient *elasticsearch.Client, templateName string, template map[string]interface{}) error {
	logger := log.FromContext(ctx)

	// Marshal the template to JSON
	templateJSON, err := json.Marshal(template)
	if err != nil {
		return fmt.Errorf("failed to marshal template: %w", err)
	}

	logger.Info(fmt.Sprintf("Applying index template %s", templateName))

	// Apply the index template (PutIndexTemplate is idempotent - creates or updates)
	res, err := esClient.Indices.PutIndexTemplate(
		templateName,
		bytes.NewReader(templateJSON),
		esClient.Indices.PutIndexTemplate.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to apply index template: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("elasticsearch API error: %s - %s", res.Status(), string(bodyBytes))
	}

	return nil
}

// deleteIndexTemplate deletes an index template from Elasticsearch
func (r *IndexTemplateReconciler) deleteIndexTemplate(ctx context.Context, esClient *elasticsearch.Client, templateName string) error {
	logger := log.FromContext(ctx)

	logger.Info(fmt.Sprintf("Deleting index template %s from Elasticsearch", templateName))

	// Delete the index template
	res, err := esClient.Indices.DeleteIndexTemplate(
		templateName,
		esClient.Indices.DeleteIndexTemplate.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to delete index template: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		// If the template doesn't exist (404), consider it already deleted
		if res.StatusCode == http.StatusNotFound {
			logger.Info(fmt.Sprintf("Index template %s not found in Elasticsearch (already deleted)", templateName))
			return nil
		}
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("elasticsearch API error: %s - %s", res.Status(), string(bodyBytes))
	}

	return nil
}

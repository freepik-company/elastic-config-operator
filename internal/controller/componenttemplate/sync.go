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

package componenttemplate

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

// Sync synchronizes ComponentTemplate resources with Elasticsearch/OpenSearch
func (r *ComponentTemplateReconciler) Sync(ctx context.Context, eventType watch.EventType, resource *v1alpha1.ComponentTemplate) (err error) {

	logger := log.FromContext(ctx)

	// Get the cluster associated to the resource
	if resource.Spec.ResourceSelector.Namespace == "" {
		resource.Spec.ResourceSelector.Namespace = resource.Namespace
	}

	// Build the cluster key for the pools
	clusterKey := fmt.Sprintf("%s_%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)

	if eventType == watch.Deleted {
		logger.Info(fmt.Sprintf("Deleting ComponentTemplate %s/%s", resource.Namespace, resource.Name))

		// Get Elasticsearch connection to delete the templates
		esConnection, err := globals.GetOrCreateElasticsearchConnection(ctx, clusterKey, &resource.Spec.ResourceSelector, resource.Namespace, r.ElasticsearchConnectionsPool)
		if err != nil {
			logger.Error(err, "Failed to get Elasticsearch connection for deletion")
			return err
		}

		// Delete each component template
		for templateName := range resource.Spec.Resources {
			logger.Info(fmt.Sprintf("Deleting component template %s", templateName))
			if err := r.deleteComponentTemplate(ctx, esConnection.Client, templateName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete component template %s", templateName))
				return err
			}
			logger.Info(fmt.Sprintf("Component template %s deleted successfully", templateName))
		}

		return nil
	}

	logger.Info(fmt.Sprintf("Syncing ComponentTemplate %s/%s", resource.Namespace, resource.Name))

	// Set status to Syncing at the beginning
	r.SetSyncing(ctx, resource)

	// Step 1: Get or create Elasticsearch connection
	esConnection, err := globals.GetOrCreateElasticsearchConnection(ctx, clusterKey, &resource.Spec.ResourceSelector, resource.Namespace, r.ElasticsearchConnectionsPool)
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
			logger.Info(fmt.Sprintf("Component template %s is no longer desired, deleting", templateName))
			if err := r.deleteComponentTemplate(ctx, esConnection.Client, templateName); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to delete component template %s", templateName))
				r.SetError(ctx, resource, fmt.Errorf("failed to delete component template %s: %w", templateName, err))
				return err
			}
			logger.Info(fmt.Sprintf("Component template %s deleted successfully", templateName))
		}
	}

	// Step 5: Apply all desired templates (idempotent)
	newAppliedTemplates := make([]string, 0, len(resource.Spec.Resources))
	for templateName, templateResource := range resource.Spec.Resources {
		logger.Info(fmt.Sprintf("Processing component template: %s", templateName))

		// Parse the desired template from the resource
		var desiredTemplate map[string]interface{}
		templateJSON, err := templateResource.MarshalJSON()
		if err != nil {
			logger.Error(err, fmt.Sprintf("Failed to marshal template %s", templateName))
			r.SetError(ctx, resource, fmt.Errorf("failed to marshal template %s: %w", templateName, err))
			return err
		}
		if err := json.Unmarshal(templateJSON, &desiredTemplate); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to unmarshal template %s", templateName))
			r.SetError(ctx, resource, fmt.Errorf("failed to unmarshal template %s: %w", templateName, err))
			return err
		}

		// Apply the component template
		if err := r.applyComponentTemplate(ctx, esConnection.Client, templateName, desiredTemplate); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to apply component template %s", templateName))
			r.SetError(ctx, resource, fmt.Errorf("failed to apply component template %s: %w", templateName, err))
			return err
		}
		logger.Info(fmt.Sprintf("Component template %s applied successfully", templateName))
		newAppliedTemplates = append(newAppliedTemplates, templateName)
	}

	// Step 6: Update the Status with the new list of applied templates
	targetCluster := fmt.Sprintf("%s/%s", resource.Spec.ResourceSelector.Namespace, resource.Spec.ResourceSelector.Name)
	if err := r.SetReady(ctx, resource, targetCluster, newAppliedTemplates); err != nil {
		logger.Error(err, "Failed to update ComponentTemplate status")
		r.SetError(ctx, resource, fmt.Errorf("failed to update ComponentTemplate status: %w", err))
		return err
	}

	logger.Info(fmt.Sprintf("ComponentTemplate %s/%s synced successfully", resource.Namespace, resource.Name))

	return nil
}

// applyComponentTemplate creates or updates a component template using the _component_template API
func (r *ComponentTemplateReconciler) applyComponentTemplate(ctx context.Context, esClient *elasticsearch.Client, templateName string, template map[string]interface{}) error {
	logger := log.FromContext(ctx)

	// GET current state and compare to detect drift
	getRes, getErr := esClient.Cluster.GetComponentTemplate(esClient.Cluster.GetComponentTemplate.WithName(templateName), esClient.Cluster.GetComponentTemplate.WithContext(ctx))
	if getErr == nil && !getRes.IsError() {
		defer getRes.Body.Close()
		bodyBytes, readErr := io.ReadAll(getRes.Body)
		if readErr == nil {
			var getBody map[string]interface{}
			if json.Unmarshal(bodyBytes, &getBody) == nil {
				if templates, ok := getBody["component_templates"].([]interface{}); ok && len(templates) > 0 {
					if first, ok := templates[0].(map[string]interface{}); ok {
						if currentTemplate, ok := first["component_template"].(map[string]interface{}); ok {
							if controller.IsSubsetMatch(template, currentTemplate) {
								logger.Info(fmt.Sprintf("No drift detected for component template %s, skipping apply", templateName))
								return nil
							}
						}
					}
				}
			}
		}
	} else if getErr == nil {
		getRes.Body.Close()
	}

	templateJSON, err := json.Marshal(template)
	if err != nil {
		return fmt.Errorf("failed to marshal template: %w", err)
	}

	logger.Info(fmt.Sprintf("Applying component template %s", templateName))

	// PUT _component_template/{name}
	req, err := http.NewRequestWithContext(ctx, "PUT",
		fmt.Sprintf("/_component_template/%s", templateName),
		bytes.NewReader(templateJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := esClient.Perform(req)
	if err != nil {
		return fmt.Errorf("failed to apply component template: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("API error: %s - %s", res.Status, string(bodyBytes))
	}

	return nil
}

// deleteComponentTemplate deletes a component template
func (r *ComponentTemplateReconciler) deleteComponentTemplate(ctx context.Context, esClient *elasticsearch.Client, templateName string) error {
	logger := log.FromContext(ctx)

	logger.Info(fmt.Sprintf("Deleting component template %s", templateName))

	// DELETE _component_template/{name}
	req, err := http.NewRequestWithContext(ctx, "DELETE",
		fmt.Sprintf("/_component_template/%s", templateName),
		nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	res, err := esClient.Perform(req)
	if err != nil {
		return fmt.Errorf("failed to delete component template: %w", err)
	}
	defer res.Body.Close()

	// If the template doesn't exist (404), consider it already deleted
	if res.StatusCode == http.StatusNotFound {
		logger.Info(fmt.Sprintf("Component template %s not found (already deleted)", templateName))
		return nil
	}

	if res.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("API error: %s - %s", res.Status, string(bodyBytes))
	}

	return nil
}

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
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"elastic-config-operator.freepik.com/elastic-config-operator/api/v1alpha1"
	"elastic-config-operator.freepik.com/elastic-config-operator/internal/controller"
	"elastic-config-operator.freepik.com/elastic-config-operator/internal/pools"
)

// ComponentTemplateReconciler reconciles a ComponentTemplate object
type ComponentTemplateReconciler struct {
	client.Client
	Scheme                       *runtime.Scheme
	ElasticsearchConnectionsPool *pools.ElasticsearchConnectionsStore
}

// +kubebuilder:rbac:groups=elastic-config-operator.freepik.com,resources=componenttemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=elastic-config-operator.freepik.com,resources=componenttemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=elastic-config-operator.freepik.com,resources=componenttemplates/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=elasticsearch.k8s.elastic.co,resources=elasticsearches,verbs=get;list;watch

func (r *ComponentTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	logger := logf.FromContext(ctx)

	// 1. Get the content of the resource
	componentTemplateResource := &v1alpha1.ComponentTemplate{}
	err = r.Get(ctx, req.NamespacedName, componentTemplateResource)

	// 2. Check existence on the cluster
	if err != nil {

		// 2.1 It does NOT exist: manage removal
		if err = client.IgnoreNotFound(err); err == nil {
			logger.Info(fmt.Sprintf(controller.ResourceNotFoundError, controller.ComponentTemplateResourceType, req.NamespacedName))
			return result, err
		}

		// 2.2 Failed to get the resource, requeue the request
		logger.Info(fmt.Sprintf(controller.ResourceSyncTimeRetrievalError, controller.ComponentTemplateResourceType, req.NamespacedName, err.Error()))
		return result, err
	}

	// 3. Check if the ComponentTemplate instance is marked to be deleted
	if !componentTemplateResource.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(componentTemplateResource, controller.ResourceFinalizer) {

			// 3.1 Delete the resources associated with the ComponentTemplate
			err = r.Sync(ctx, watch.Deleted, componentTemplateResource)

			// Remove the finalizers on ComponentTemplate CR
			controllerutil.RemoveFinalizer(componentTemplateResource, controller.ResourceFinalizer)
			err = r.Update(ctx, componentTemplateResource)
			if err != nil {
				logger.Info(fmt.Sprintf(controller.ResourceFinalizersUpdateError, controller.ComponentTemplateResourceType, req.NamespacedName, err.Error()))
			}
		}

		result = ctrl.Result{}
		err = nil
		return result, err
	}

	// 4. Add finalizer to the ComponentTemplate CR
	if !controllerutil.ContainsFinalizer(componentTemplateResource, controller.ResourceFinalizer) {
		controllerutil.AddFinalizer(componentTemplateResource, controller.ResourceFinalizer)
		err = r.Update(ctx, componentTemplateResource)
		if err != nil {
			return result, err
		}
	}

	// 5. Update the status before the requeue
	defer func() {
		err = r.Status().Update(ctx, componentTemplateResource)
		if err != nil {
			logger.Info(fmt.Sprintf(controller.ResourceConditionUpdateError, controller.ComponentTemplateResourceType, req.NamespacedName, err.Error()))
		}
	}()

	// 6. Schedule periodical request
	syncInterval := componentTemplateResource.Spec.SyncInterval
	if syncInterval == "" {
		syncInterval = controller.DefaultSyncInterval
	}
	RequeueTime, err := time.ParseDuration(syncInterval)
	if err != nil {
		logger.Info(fmt.Sprintf(controller.ResourceSyncTimeRetrievalError, controller.ComponentTemplateResourceType, req.NamespacedName, err.Error()))
		return result, err
	}
	result = ctrl.Result{
		RequeueAfter: RequeueTime,
	}

	// 7. Sync the component templates
	err = r.Sync(ctx, watch.Modified, componentTemplateResource)
	if err != nil {
		r.UpdateConditionKubernetesApiCallFailure(componentTemplateResource)
		logger.Info(fmt.Sprintf(controller.SyncTargetError, controller.ComponentTemplateResourceType, req.NamespacedName, err.Error()))
		return result, err
	}

	// 8. Success, update the status
	r.UpdateConditionSuccess(componentTemplateResource)

	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *ComponentTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ComponentTemplate{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("componenttemplate").
		Complete(r)
}

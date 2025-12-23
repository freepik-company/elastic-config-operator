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

	"eck-config-operator.freepik.com/eck-config-operator/api/v1alpha1"
	"eck-config-operator.freepik.com/eck-config-operator/internal/controller"
	"eck-config-operator.freepik.com/eck-config-operator/internal/pools"
)

// IndexStateManagementReconciler reconciles an IndexStateManagement object
type IndexStateManagementReconciler struct {
	client.Client
	Scheme                       *runtime.Scheme
	ElasticsearchConnectionsPool *pools.ElasticsearchConnectionsStore
}

// +kubebuilder:rbac:groups=eck-config-operator.freepik.com,resources=indexstatemanagements,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=eck-config-operator.freepik.com,resources=indexstatemanagements/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=eck-config-operator.freepik.com,resources=indexstatemanagements/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=elasticsearch.k8s.elastic.co,resources=elasticsearches,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *IndexStateManagementReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	logger := logf.FromContext(ctx)

	// 1. Get the content of the resource
	indexStateManagementResource := &v1alpha1.IndexStateManagement{}
	err = r.Get(ctx, req.NamespacedName, indexStateManagementResource)

	// 2. Check existence on the cluster
	if err != nil {

		// 2.1 It does NOT exist: manage removal
		if err = client.IgnoreNotFound(err); err == nil {
			logger.Info(fmt.Sprintf(controller.ResourceNotFoundError, controller.IndexStateManagementResourceType, req.NamespacedName))
			return result, err
		}

		// 2.2 Failed to get the resource, requeue the request
		logger.Info(fmt.Sprintf(controller.ResourceSyncTimeRetrievalError, controller.IndexStateManagementResourceType, req.NamespacedName, err.Error()))
		return result, err
	}

	// 3. Check if the IndexStateManagement instance is marked to be deleted
	if !indexStateManagementResource.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(indexStateManagementResource, controller.ResourceFinalizer) {

			// 3.1 Delete the resources associated with the IndexStateManagement
			err = r.Sync(ctx, watch.Deleted, indexStateManagementResource)

			// Remove the finalizers on IndexStateManagement CR
			controllerutil.RemoveFinalizer(indexStateManagementResource, controller.ResourceFinalizer)
			err = r.Update(ctx, indexStateManagementResource)
			if err != nil {
				logger.Info(fmt.Sprintf(controller.ResourceFinalizersUpdateError, controller.IndexStateManagementResourceType, req.NamespacedName, err.Error()))
			}
		}

		result = ctrl.Result{}
		err = nil
		return result, err
	}

	// 4. Add finalizer to the IndexStateManagement CR
	if !controllerutil.ContainsFinalizer(indexStateManagementResource, controller.ResourceFinalizer) {
		controllerutil.AddFinalizer(indexStateManagementResource, controller.ResourceFinalizer)
		err = r.Update(ctx, indexStateManagementResource)
		if err != nil {
			return result, err
		}
	}

	// 5. Update the status before the requeue
	defer func() {
		err = r.Status().Update(ctx, indexStateManagementResource)
		if err != nil {
			logger.Info(fmt.Sprintf(controller.ResourceConditionUpdateError, controller.IndexStateManagementResourceType, req.NamespacedName, err.Error()))
		}
	}()

	// 6. Schedule periodical request
	syncInterval := indexStateManagementResource.Spec.SyncInterval
	if syncInterval == "" {
		syncInterval = controller.DefaultSyncInterval
	}
	RequeueTime, err := time.ParseDuration(syncInterval)
	if err != nil {
		logger.Info(fmt.Sprintf(controller.ResourceSyncTimeRetrievalError, controller.IndexStateManagementResourceType, req.NamespacedName, err.Error()))
		return result, err
	}
	result = ctrl.Result{
		RequeueAfter: RequeueTime,
	}

	// 7. Sync the ISM policies
	err = r.Sync(ctx, watch.Modified, indexStateManagementResource)
	if err != nil {
		r.UpdateConditionKubernetesApiCallFailure(indexStateManagementResource)
		logger.Info(fmt.Sprintf(controller.SyncTargetError, controller.IndexStateManagementResourceType, req.NamespacedName, err.Error()))
		return result, err
	}

	// 8. Success, update the status
	r.UpdateConditionSuccess(indexStateManagementResource)

	return result, err

}

// SetupWithManager sets up the controller with the Manager.
func (r *IndexStateManagementReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.IndexStateManagement{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("indexstatemanagement").
		Complete(r)
}

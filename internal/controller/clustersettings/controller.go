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

// ClusterSettingsReconciler reconciles a ClusterSettings object
type ClusterSettingsReconciler struct {
	client.Client
	Scheme                       *runtime.Scheme
	ElasticsearchConnectionsPool *pools.ElasticsearchConnectionsStore
}

// +kubebuilder:rbac:groups=eck-config-operator.freepik.com,resources=clustersettings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=eck-config-operator.freepik.com,resources=clustersettings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=eck-config-operator.freepik.com,resources=clustersettings/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=elasticsearch.k8s.elastic.co,resources=elasticsearches,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ClusterSettingsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	logger := logf.FromContext(ctx)

	// 1. Get the content of the resource
	clusterSettingsResource := &v1alpha1.ClusterSettings{}
	err = r.Get(ctx, req.NamespacedName, clusterSettingsResource)

	// 2. Check existence on the cluster
	if err != nil {

		// 2.1 It does NOT exist: manage removal
		if err = client.IgnoreNotFound(err); err == nil {
			logger.Info(fmt.Sprintf(controller.ResourceNotFoundError, controller.ClusterSettingsResourceType, req.NamespacedName))
			return result, err
		}

		// 2.2 Failed to get the resource, requeue the request
		logger.Info(fmt.Sprintf(controller.ResourceSyncTimeRetrievalError, controller.ClusterSettingsResourceType, req.NamespacedName, err.Error()))
		return result, err
	}

	// 3. Check if the ClusterSettings instance is marked to be deleted: indicated by the deletion timestamp being set
	if !clusterSettingsResource.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(clusterSettingsResource, controller.ResourceFinalizer) {

			// 3.1 Delete the resources associated with the ClusterSettings
			err = r.Sync(ctx, watch.Deleted, clusterSettingsResource)

			// Remove the finalizers on ClusterSettings CR
			controllerutil.RemoveFinalizer(clusterSettingsResource, controller.ResourceFinalizer)
			err = r.Update(ctx, clusterSettingsResource)
			if err != nil {
				logger.Info(fmt.Sprintf(controller.ResourceFinalizersUpdateError, controller.ClusterSettingsResourceType, req.NamespacedName, err.Error()))
			}
		}

		result = ctrl.Result{}
		err = nil
		return result, err
	}

	// 4. Add finalizer to the ClusterSettings CR
	if !controllerutil.ContainsFinalizer(clusterSettingsResource, controller.ResourceFinalizer) {
		controllerutil.AddFinalizer(clusterSettingsResource, controller.ResourceFinalizer)
		err = r.Update(ctx, clusterSettingsResource)
		if err != nil {
			return result, err
		}
	}

	// 5. Update the status before the requeue
	defer func() {
		err = r.Status().Update(ctx, clusterSettingsResource)
		if err != nil {
			logger.Info(fmt.Sprintf(controller.ResourceConditionUpdateError, controller.ClusterSettingsResourceType, req.NamespacedName, err.Error()))
		}
	}()

	// 6. Schedule periodical request
	syncInterval := clusterSettingsResource.Spec.SyncInterval
	if syncInterval == "" {
		syncInterval = controller.DefaultSyncInterval
	}
	RequeueTime, err := time.ParseDuration(syncInterval)
	if err != nil {
		logger.Info(fmt.Sprintf(controller.ResourceSyncTimeRetrievalError, controller.ClusterSettingsResourceType, req.NamespacedName, err.Error()))
		return result, err
	}
	result = ctrl.Result{
		RequeueAfter: RequeueTime,
	}

	// 7. Sync the cluster settings
	err = r.Sync(ctx, watch.Modified, clusterSettingsResource)
	if err != nil {
		r.UpdateConditionKubernetesApiCallFailure(clusterSettingsResource)
		logger.Info(fmt.Sprintf(controller.SyncTargetError, controller.ClusterSettingsResourceType, req.NamespacedName, err.Error()))
		return result, err
	}

	// 8. Success, update the status
	r.UpdateConditionSuccess(clusterSettingsResource)

	return result, err

}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterSettingsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ClusterSettings{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("clustersettings").
		Complete(r)
}

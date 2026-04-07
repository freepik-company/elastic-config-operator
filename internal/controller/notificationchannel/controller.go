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

// NotificationChannelReconciler reconciles a NotificationChannel object
type NotificationChannelReconciler struct {
	client.Client
	Scheme                       *runtime.Scheme
	ElasticsearchConnectionsPool *pools.ElasticsearchConnectionsStore
}

// +kubebuilder:rbac:groups=elastic-config-operator.freepik.com,resources=notificationchannels,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=elastic-config-operator.freepik.com,resources=notificationchannels/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=elastic-config-operator.freepik.com,resources=notificationchannels/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=elasticsearch.k8s.elastic.co,resources=elasticsearches,verbs=get;list;watch

func (r *NotificationChannelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	logger := logf.FromContext(ctx)

	// 1. Get the content of the resource
	notificationChannelResource := &v1alpha1.NotificationChannel{}
	err = r.Get(ctx, req.NamespacedName, notificationChannelResource)

	// 2. Check existence on the cluster
	if err != nil {

		// 2.1 It does NOT exist: manage removal
		if err = client.IgnoreNotFound(err); err == nil {
			logger.Info(fmt.Sprintf(controller.ResourceNotFoundError, controller.NotificationChannelResourceType, req.NamespacedName))
			return result, err
		}

		// 2.2 Failed to get the resource, requeue the request
		logger.Info(fmt.Sprintf(controller.ResourceSyncTimeRetrievalError, controller.NotificationChannelResourceType, req.NamespacedName, err.Error()))
		return result, err
	}

	// 3. Check if the NotificationChannel instance is marked to be deleted
	if !notificationChannelResource.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(notificationChannelResource, controller.ResourceFinalizer) {

			// 3.1 Delete the resources associated with the NotificationChannel
			if !notificationChannelResource.Spec.Protected {
				err = r.Sync(ctx, watch.Deleted, notificationChannelResource)
			} else {
				logger.Info(fmt.Sprintf("Protected resource %s/%s, skipping deletion of external resources", notificationChannelResource.Namespace, notificationChannelResource.Name))
			}

			// Remove the finalizers on NotificationChannel CR
			controllerutil.RemoveFinalizer(notificationChannelResource, controller.ResourceFinalizer)
			err = r.Update(ctx, notificationChannelResource)
			if err != nil {
				logger.Info(fmt.Sprintf(controller.ResourceFinalizersUpdateError, controller.NotificationChannelResourceType, req.NamespacedName, err.Error()))
			}
		}

		result = ctrl.Result{}
		err = nil
		return result, err
	}

	// 4. Add finalizer to the NotificationChannel CR
	if !controllerutil.ContainsFinalizer(notificationChannelResource, controller.ResourceFinalizer) {
		controllerutil.AddFinalizer(notificationChannelResource, controller.ResourceFinalizer)
		err = r.Update(ctx, notificationChannelResource)
		if err != nil {
			return result, err
		}
	}

	// 5. Update the status before the requeue
	defer func() {
		err = r.Status().Update(ctx, notificationChannelResource)
		if err != nil {
			logger.Info(fmt.Sprintf(controller.ResourceConditionUpdateError, controller.NotificationChannelResourceType, req.NamespacedName, err.Error()))
		}
	}()

	// 6. Schedule periodical request
	syncInterval := notificationChannelResource.Spec.SyncInterval
	if syncInterval == "" {
		syncInterval = controller.DefaultSyncInterval
	}
	RequeueTime, err := time.ParseDuration(syncInterval)
	if err != nil {
		logger.Info(fmt.Sprintf(controller.ResourceSyncTimeRetrievalError, controller.NotificationChannelResourceType, req.NamespacedName, err.Error()))
		return result, err
	}
	result = ctrl.Result{
		RequeueAfter: RequeueTime,
	}

	// 7. Sync the notification channels
	err = r.Sync(ctx, watch.Modified, notificationChannelResource)
	if err != nil {
		notificationChannelResource.Status.ConsecutiveErrors++
		backoff := controller.CalculateBackoff(notificationChannelResource.Status.ConsecutiveErrors)
		result = ctrl.Result{RequeueAfter: backoff}
		r.UpdateConditionKubernetesApiCallFailure(notificationChannelResource)
		logger.Info(fmt.Sprintf(controller.SyncTargetError, controller.NotificationChannelResourceType, req.NamespacedName, err.Error()))
		return result, err
	}

	// 8. Success, update the status
	notificationChannelResource.Status.ConsecutiveErrors = 0
	r.UpdateConditionSuccess(notificationChannelResource)

	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *NotificationChannelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.NotificationChannel{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("notificationchannel").
		Complete(r)
}

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

package securityrole

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

// SecurityRoleReconciler reconciles a SecurityRole object
type SecurityRoleReconciler struct {
	client.Client
	Scheme                       *runtime.Scheme
	ElasticsearchConnectionsPool *pools.ElasticsearchConnectionsStore
}

// +kubebuilder:rbac:groups=elastic-config-operator.freepik.com,resources=securityroles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=elastic-config-operator.freepik.com,resources=securityroles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=elastic-config-operator.freepik.com,resources=securityroles/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=elasticsearch.k8s.elastic.co,resources=elasticsearches,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *SecurityRoleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	logger := logf.FromContext(ctx)

	// 1. Get the content of the resource
	securityRoleResource := &v1alpha1.SecurityRole{}
	err = r.Get(ctx, req.NamespacedName, securityRoleResource)

	// 2. Check existence on the cluster
	if err != nil {

		// 2.1 It does NOT exist: manage removal
		if err = client.IgnoreNotFound(err); err == nil {
			logger.Info(fmt.Sprintf(controller.ResourceNotFoundError, controller.SecurityRoleResourceType, req.NamespacedName))
			return result, err
		}

		// 2.2 Failed to get the resource, requeue the request
		logger.Info(fmt.Sprintf(controller.ResourceSyncTimeRetrievalError, controller.SecurityRoleResourceType, req.NamespacedName, err.Error()))
		return result, err
	}

	// 3. Check if the SecurityRole instance is marked to be deleted
	if !securityRoleResource.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(securityRoleResource, controller.ResourceFinalizer) {

			// 3.1 Delete the resources associated with the SecurityRole
			if !securityRoleResource.Spec.Protected {
				err = r.Sync(ctx, watch.Deleted, securityRoleResource)
			} else {
				logger.Info(fmt.Sprintf("Protected resource %s/%s, skipping deletion of external resources", securityRoleResource.Namespace, securityRoleResource.Name))
			}

			// Remove the finalizers on SecurityRole CR
			controllerutil.RemoveFinalizer(securityRoleResource, controller.ResourceFinalizer)
			err = r.Update(ctx, securityRoleResource)
			if err != nil {
				logger.Info(fmt.Sprintf(controller.ResourceFinalizersUpdateError, controller.SecurityRoleResourceType, req.NamespacedName, err.Error()))
			}
		}

		result = ctrl.Result{}
		err = nil
		return result, err
	}

	// 4. Add finalizer to the SecurityRole CR
	if !controllerutil.ContainsFinalizer(securityRoleResource, controller.ResourceFinalizer) {
		controllerutil.AddFinalizer(securityRoleResource, controller.ResourceFinalizer)
		err = r.Update(ctx, securityRoleResource)
		if err != nil {
			return result, err
		}
	}

	// 5. Update the status before the requeue
	defer func() {
		err = r.Status().Update(ctx, securityRoleResource)
		if err != nil {
			logger.Info(fmt.Sprintf(controller.ResourceConditionUpdateError, controller.SecurityRoleResourceType, req.NamespacedName, err.Error()))
		}
	}()

	// 6. Schedule periodical request
	syncInterval := securityRoleResource.Spec.SyncInterval
	if syncInterval == "" {
		syncInterval = controller.DefaultSyncInterval
	}
	RequeueTime, err := time.ParseDuration(syncInterval)
	if err != nil {
		logger.Info(fmt.Sprintf(controller.ResourceSyncTimeRetrievalError, controller.SecurityRoleResourceType, req.NamespacedName, err.Error()))
		return result, err
	}
	result = ctrl.Result{
		RequeueAfter: RequeueTime,
	}

	// 7. Sync the security roles
	err = r.Sync(ctx, watch.Modified, securityRoleResource)
	if err != nil {
		securityRoleResource.Status.ConsecutiveErrors++
		backoff := controller.CalculateBackoff(securityRoleResource.Status.ConsecutiveErrors)
		result = ctrl.Result{RequeueAfter: backoff}
		r.UpdateConditionKubernetesApiCallFailure(securityRoleResource)
		logger.Info(fmt.Sprintf(controller.SyncTargetError, controller.SecurityRoleResourceType, req.NamespacedName, err.Error()))
		return result, err
	}

	// 8. Success, update the status
	securityRoleResource.Status.ConsecutiveErrors = 0
	r.UpdateConditionSuccess(securityRoleResource)

	return result, err

}

// SetupWithManager sets up the controller with the Manager.
func (r *SecurityRoleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.SecurityRole{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("securityrole").
		Complete(r)
}

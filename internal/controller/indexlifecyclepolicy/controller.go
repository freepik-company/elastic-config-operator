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

package indexlifecyclepolicy

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

// IndexLifecyclePolicyReconciler reconciles a IndexLifecyclePolicy object
type IndexLifecyclePolicyReconciler struct {
	client.Client
	Scheme                       *runtime.Scheme
	ElasticsearchConnectionsPool *pools.ElasticsearchConnectionsStore
}

// +kubebuilder:rbac:groups=eck-config-operator.freepik.com,resources=indexlifecyclepolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=eck-config-operator.freepik.com,resources=indexlifecyclepolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=eck-config-operator.freepik.com,resources=indexlifecyclepolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=elasticsearch.k8s.elastic.co,resources=elasticsearches,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the IndexLifecyclePolicy object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/reconcile
func (r *IndexLifecyclePolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	logger := logf.FromContext(ctx)

	// 1. Get the content of the Patch
	indexLifecyclePolicyResource := &v1alpha1.IndexLifecyclePolicy{}
	err = r.Get(ctx, req.NamespacedName, indexLifecyclePolicyResource)

	// 2. Check existence on the cluster
	if err != nil {

		// 2.1 It does NOT exist: manage removal
		if err = client.IgnoreNotFound(err); err == nil {
			logger.Info(fmt.Sprintf(controller.ResourceNotFoundError, controller.IndexLifecyclePolicyResourceType, req.NamespacedName))
			return result, err
		}

		// 2.2 Failed to get the resource, requeue the request
		logger.Info(fmt.Sprintf(controller.ResourceSyncTimeRetrievalError, controller.IndexLifecyclePolicyResourceType, req.NamespacedName, err.Error()))
		return result, err
	}

	// 3. Check if the IndexLifecyclePolicy instance is marked to be deleted: indicated by the deletion timestamp being set
	if !indexLifecyclePolicyResource.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(indexLifecyclePolicyResource, controller.ResourceFinalizer) {

			// 3.1 Delete the resources associated with the SearchRule
			err = r.Sync(ctx, watch.Deleted, indexLifecyclePolicyResource)

			// Remove the finalizers on Patch CR
			controllerutil.RemoveFinalizer(indexLifecyclePolicyResource, controller.ResourceFinalizer)
			err = r.Update(ctx, indexLifecyclePolicyResource)
			if err != nil {
				logger.Info(fmt.Sprintf(controller.ResourceFinalizersUpdateError, controller.IndexLifecyclePolicyResourceType, req.NamespacedName, err.Error()))
			}
		}

		result = ctrl.Result{}
		err = nil
		return result, err
	}

	// 4. Add finalizer to the SearchRule CR
	if !controllerutil.ContainsFinalizer(indexLifecyclePolicyResource, controller.ResourceFinalizer) {
		controllerutil.AddFinalizer(indexLifecyclePolicyResource, controller.ResourceFinalizer)
		err = r.Update(ctx, indexLifecyclePolicyResource)
		if err != nil {
			return result, err
		}
	}

	// 5. Update the status before the requeue
	defer func() {
		err = r.Status().Update(ctx, indexLifecyclePolicyResource)
		if err != nil {
			logger.Info(fmt.Sprintf(controller.ResourceConditionUpdateError, controller.IndexLifecyclePolicyResourceType, req.NamespacedName, err.Error()))
		}
	}()

	// 6. Schedule periodical request
	syncInterval := indexLifecyclePolicyResource.Spec.SyncInterval
	if syncInterval == "" {
		syncInterval = controller.DefaultSyncInterval
	}
	RequeueTime, err := time.ParseDuration(syncInterval)
	if err != nil {
		logger.Info(fmt.Sprintf(controller.ResourceSyncTimeRetrievalError, controller.IndexLifecyclePolicyResourceType, req.NamespacedName, err.Error()))
		return result, err
	}
	result = ctrl.Result{
		RequeueAfter: RequeueTime,
	}

	// 7. Check the rule
	err = r.Sync(ctx, watch.Modified, indexLifecyclePolicyResource)
	if err != nil {
		r.UpdateConditionKubernetesApiCallFailure(indexLifecyclePolicyResource)
		logger.Info(fmt.Sprintf(controller.SyncTargetError, controller.IndexLifecyclePolicyResourceType, req.NamespacedName, err.Error()))
		return result, err
	}

	// 8. Success, update the status
	r.UpdateConditionSuccess(indexLifecyclePolicyResource)

	return result, err

}

// SetupWithManager sets up the controller with the Manager.
func (r *IndexLifecyclePolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.IndexLifecyclePolicy{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("indexlifecyclepolicy").
		Complete(r)
}

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

package controllers

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/paused"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	infrav1 "github.com/yandex-cloud/cluster-api-provider-yandex/api/v1alpha1"
	yandex "github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/client"
	"github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/cloud/scope"
	loadbalancer "github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/cloud/services/loadbalancers"
	"github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/cloud/ycerrors"
	"github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/options"
)

// YandexClusterReconciler reconciles a YandexCluster object.
type YandexClusterReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	YandexClientGetter yandex.YandexClientGetter
	Config             options.Config
}

//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=yandexclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=yandexclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=yandexclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *YandexClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	logger := log.FromContext(ctx)

	ctx, cancel := context.WithTimeout(ctx, r.Config.ReconcileTimeout)
	defer cancel()

	yandexCluster := &infrav1.YandexCluster{}
	if err := r.Get(ctx, req.NamespacedName, yandexCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Get the Cluster.
	cluster, err := util.GetOwnerCluster(ctx, r.Client, yandexCluster.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if cluster == nil {
		logger.Info("Waiting for Cluster Controller to set OwnerRef on YandexCluster")
		return ctrl.Result{}, nil
	}

	// Surface the paused state in the Paused condition (v1beta2 contract) and stop here if the
	// YandexCluster or the owning Cluster is paused: a paused object must not be reconciled.
	isPaused, _, err := paused.EnsurePausedCondition(ctx, r.Client, cluster, yandexCluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	if isPaused {
		logger.Info("YandexCluster or owning Cluster is marked as paused, not reconciling")
		return ctrl.Result{}, nil
	}

	clusterScope, err := scope.NewClusterScope(ctx, scope.ClusterScopeParams{
		Client:             r.Client,
		Cluster:            cluster,
		YandexCluster:      yandexCluster,
		YandexClientGetter: r.YandexClientGetter,
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	// Always close the scope when exiting this function so we can persist any YandexCluster changes.
	defer func() {
		if err := clusterScope.Close(ctx); err != nil && rerr == nil {
			rerr = err
		}
	}()

	// Handle deleted clusters.
	if !yandexCluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, clusterScope)
	}

	return r.reconcile(ctx, clusterScope)
}

// reconcile it is a part of reconciliation loop in case of YandexCluster update/create.
func (r *YandexClusterReconciler) reconcile(ctx context.Context, clusterScope *scope.ClusterScope) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(clusterScope.YandexCluster, infrav1.ClusterFinalizer) {
		controllerutil.AddFinalizer(clusterScope.YandexCluster, infrav1.ClusterFinalizer)

		// The finalizer is persisted when the scope is closed. That update triggers the next
		// reconcile by itself, so an explicit requeue is not needed.
		logger.Info("Finalizer added to YandexCluster")
		return ctrl.Result{}, nil
	}

	// Get loadbalancer service and reconcile load balancer.
	lb := loadbalancer.New(clusterScope)
	if err := lb.Reconcile(ctx); err != nil {
		setLoadBalancerCondition(clusterScope.YandexCluster, metav1.ConditionFalse,
			infrav1.LoadBalancerReconcileFailedReason, ycerrors.Message(err))
		return ctrl.Result{}, fmt.Errorf("error reconciling load balancer: %w", err)
	}

	active, err := lb.IsActive(ctx)
	if err != nil {
		setLoadBalancerCondition(clusterScope.YandexCluster, metav1.ConditionFalse,
			infrav1.LoadBalancerReconcileFailedReason, ycerrors.Message(err))
		return ctrl.Result{}, fmt.Errorf("error reconciling load balancer: %w", err)
	}
	if !active {
		setLoadBalancerCondition(clusterScope.YandexCluster, metav1.ConditionFalse,
			infrav1.LoadBalancerProvisioningReason, "Load balancer is not active yet")
		logger.Info("load balancer instance not active, requeueing")
		return ctrl.Result{RequeueAfter: RequeueDuration}, nil
	}

	// When load balancer is active, fetch his status from YandexCloud, then set API address
	// and YandexCluster status.
	state, err := lb.Describe(ctx)
	if err != nil {
		setLoadBalancerCondition(clusterScope.YandexCluster, metav1.ConditionFalse,
			infrav1.LoadBalancerReconcileFailedReason, ycerrors.Message(err))
		return ctrl.Result{}, fmt.Errorf("error reconciling load balancer: %w", err)
	}

	switch {
	// The load balancer has been created.
	case clusterScope.YandexCluster.Spec.ControlPlaneEndpoint.Host == "" && clusterScope.YandexCluster.Spec.ControlPlaneEndpoint.Port == 0:
		clusterScope.YandexCluster.Spec.ControlPlaneEndpoint = infrav1.APIEndpoint{
			Host: state.ListenerAddress,
			Port: state.ListenerPort,
		}
		clusterScope.YandexCluster.Status.LoadBalancer = infrav1.LoadBalancerStatus{
			ListenerAddress: state.ListenerAddress,
			ListenerPort:    state.ListenerPort,
		}
		setLoadBalancerCondition(clusterScope.YandexCluster, metav1.ConditionTrue, infrav1.LoadBalancerActiveReason, "")
		clusterScope.SetProvisioned()
		return ctrl.Result{}, nil

	// The load balancer has been recreated.
	case clusterScope.YandexCluster.Spec.ControlPlaneEndpoint.Host == state.ListenerAddress && clusterScope.YandexCluster.Spec.ControlPlaneEndpoint.Port == state.ListenerPort:
		clusterScope.YandexCluster.Status.LoadBalancer = infrav1.LoadBalancerStatus{
			ListenerAddress: state.ListenerAddress,
			ListenerPort:    state.ListenerPort,
		}
		setLoadBalancerCondition(clusterScope.YandexCluster, metav1.ConditionTrue, infrav1.LoadBalancerActiveReason, "")
		clusterScope.SetProvisioned()
		return ctrl.Result{}, nil

	default:
		err := fmt.Errorf("YandexCluster controlPlaneEndpoint differs from the load balancer listener values. The cluster has become unrecoverable and should be manually deleted")
		setLoadBalancerCondition(clusterScope.YandexCluster, metav1.ConditionFalse,
			infrav1.LoadBalancerEndpointMismatchReason, err.Error())
		return ctrl.Result{}, err
	}
}

// reconcileDelete it is a part of reconciliation loop in case of YandexCluster delete.
func (r *YandexClusterReconciler) reconcileDelete(ctx context.Context, clusterScope *scope.ClusterScope) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(clusterScope.YandexCluster, infrav1.ClusterFinalizer) {
		logger.Info("no finalizer found on YandexCluster, skipping deletion reconciliation")
		return ctrl.Result{}, nil
	}

	// Delete load balancer and remove finalizer from YandexCluster.
	lb := loadbalancer.New(clusterScope)
	deleted, err := lb.Delete(ctx)
	if err != nil {
		setLoadBalancerCondition(clusterScope.YandexCluster, metav1.ConditionFalse,
			infrav1.LoadBalancerDeletionFailedReason, ycerrors.Message(err))
		return ctrl.Result{}, fmt.Errorf("error deleting load balancer  %w", err)
	}

	if deleted {
		logger.Info("load balancer has been deleted")
		controllerutil.RemoveFinalizer(clusterScope.YandexCluster, infrav1.ClusterFinalizer)
		return ctrl.Result{}, nil
	}

	logger.V(1).Info("load balancer is being deleted, requeueing")
	setLoadBalancerCondition(clusterScope.YandexCluster, metav1.ConditionFalse,
		infrav1.LoadBalancerDeletingReason, "Load balancer is being deleted")
	return ctrl.Result{RequeueAfter: RequeueDuration}, nil
}

// setLoadBalancerCondition sets the LoadBalancerReady condition of the YandexCluster.
func setLoadBalancerCondition(yandexCluster *infrav1.YandexCluster, status metav1.ConditionStatus, reason, message string) {
	conditions.Set(yandexCluster, metav1.Condition{
		Type:    infrav1.LoadBalancerReadyCondition,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *YandexClusterReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.YandexCluster{}).
		// Paused objects are not filtered out here: the controller has to see them to keep the
		// Paused condition up to date. Reconcile returns early for paused objects instead.
		WithEventFilter(predicates.ResourceIsChanged(mgr.GetScheme(), ctrl.LoggerFrom(ctx))).
		Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(
				util.ClusterToInfrastructureMapFunc(
					ctx,
					infrav1.GroupVersion.WithKind("YandexCluster"),
					mgr.GetClient(),
					&infrav1.YandexCluster{},
				),
			),
			builder.WithPredicates(predicates.ClusterPausedTransitions(mgr.GetScheme(), ctrl.LoggerFrom(ctx))),
		).
		Complete(r)
}

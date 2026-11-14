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
	"time"

	"github.com/pkg/errors"
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
	"github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/cloud/services/compute"
	loadbalancer "github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/cloud/services/loadbalancers"
	"github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/cloud/ycerrors"
)

const (
	// RequeueDuration is pause before reconcile will be repeated.
	RequeueDuration time.Duration = 10 * time.Second
)

// YandexMachineReconciler reconciles a YandexMachine object.
type YandexMachineReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	YandexClientGetter yandex.YandexClientGetter
}

//+kubebuilder:rbac:groups="",resources=secrets;,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=yandexmachines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=yandexmachines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=yandexmachines/finalizers,verbs=update

// Reconcile brings YandexMachine into desired state.
func (r *YandexMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	logger := log.FromContext(ctx)

	yandexMachine := &infrav1.YandexMachine{}
	err := r.Get(ctx, req.NamespacedName, yandexMachine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("error occurred while fetching YandexMachine resource: %w", err)
	}

	logger.V(1).Info("machine found")
	machine, err := util.GetOwnerMachine(ctx, r.Client, yandexMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if machine == nil {
		logger.Info("machine controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	logger = logger.WithValues("machine", machine.Name)
	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, machine.ObjectMeta)
	if err != nil {
		logger.Info("machine is missing cluster label or cluster does not exist")
		return ctrl.Result{}, nil
	}

	logger = logger.WithValues("cluster", cluster.Name)
	// Surface the paused state in the Paused condition (v1beta2 contract) and stop here if the
	// YandexMachine or the linked Cluster is paused: a paused object must not be reconciled.
	isPaused, _, err := paused.EnsurePausedCondition(ctx, r.Client, cluster, yandexMachine)
	if err != nil {
		return ctrl.Result{}, err
	}
	if isPaused {
		logger.Info("YandexMachine or linked Cluster is marked as paused. Won't reconcile")
		return ctrl.Result{}, nil
	}

	yandexCluster := &infrav1.YandexCluster{}
	yandexClusterKey := client.ObjectKey{
		Namespace: yandexMachine.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	if err = r.Get(ctx, yandexClusterKey, yandexCluster); err != nil {
		logger.Info("YandexCluster is not available yet")
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

	// The Yandex Cloud client is built for this reconcile only and has to be closed, otherwise
	// every reconcile of a YandexMachine leaks the GRPC connections of the SDK.
	// (The cluster scope itself is not closed here: that would also patch the YandexCluster.)
	defer func() {
		if err := clusterScope.GetClient().Close(ctx); err != nil {
			logger.Error(err, "failed to close YandexCloud client")
		}
	}()

	machineScope, err := scope.NewMachineScope(scope.MachineScopeParams{
		Client:        r.Client,
		Machine:       machine,
		YandexMachine: yandexMachine,
		LoadBalancer:  loadbalancer.New(clusterScope),
		ClusterGetter: clusterScope,
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	// always close the scope when exiting this function so we can persist any YandexMachine changes.
	defer func() {
		if err := machineScope.Close(ctx); err != nil && reterr == nil {
			reterr = err
		}
	}()

	if !yandexMachine.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, machineScope)
	}

	return r.reconcile(ctx, machineScope)
}

// reconcile it is a part of reconciliation loop in case of yandexmachine update/create.
func (r *YandexMachineReconciler) reconcile(ctx context.Context, machineScope *scope.MachineScope) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("reconciling YandexMachine")

	controllerutil.AddFinalizer(machineScope.YandexMachine, infrav1.MachineFinalizer)

	if err := machineScope.PatchObject(ctx); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
	}

	if machineScope.Machine.Spec.Bootstrap.DataSecretName == nil {
		conditions.Set(machineScope.YandexMachine, metav1.Condition{
			Type:    infrav1.InstanceReadyCondition,
			Status:  metav1.ConditionFalse,
			Reason:  infrav1.WaitingForBootstrapDataReason,
			Message: "Waiting for the bootstrap data secret of the Machine",
		})
		logger.Info("bootstrap data is not ready yet: linked Machine's bootstrap.dataSecretName is nil. Skipping reconciliation")
		return ctrl.Result{}, nil
	}

	if err := compute.New(machineScope).Reconcile(ctx); err != nil {
		return ctrl.Result{}, fmt.Errorf("error reconciling instance resources: %w", err)
	}

	instanceState := *machineScope.GetInstanceStatus()
	switch instanceState {
	case infrav1.InstanceStatusStarting, infrav1.InstanceStatusProvisioning:
		logger.Info("YandexMachine instance is provisioning", "instance-id", machineScope.GetInstanceID())
		return ctrl.Result{RequeueAfter: RequeueDuration}, nil
	case infrav1.InstanceStatusRunning:
		logger.Info("YandexMachine instance is running", "instance-id", machineScope.GetInstanceID())
		machineScope.SetProvisioned()
		return ctrl.Result{}, nil
	default:
		// The instance is neither running nor starting (for example it was stopped or deleted
		// outside of Cluster API). The provider does not restart or recreate it: the state is
		// reported in status.instanceState and in the InstanceReady condition (set by the compute
		// service) and the instance is polled until it runs again.
		// The v1beta2 contract has no terminal failures, so failureReason/failureMessage are not set.
		logger.Info("YandexMachine instance is not running", "instance-id", machineScope.GetInstanceID(), "state", instanceState)
		return ctrl.Result{RequeueAfter: RequeueDuration}, nil
	}
}

// reconcileDelete it is a part of reconciliation loop in case of yandexmachine delete.
func (r *YandexMachineReconciler) reconcileDelete(ctx context.Context, machineScope *scope.MachineScope) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("reconciling YandexMachine delete")

	deleted, err := compute.New(machineScope).Delete(ctx)
	if err != nil {
		conditions.Set(machineScope.YandexMachine, metav1.Condition{
			Type:    infrav1.InstanceReadyCondition,
			Status:  metav1.ConditionFalse,
			Reason:  infrav1.InstanceDeletionFailedReason,
			Message: ycerrors.Message(err),
		})
		return ctrl.Result{}, fmt.Errorf("error deleting instance resources: %w", err)
	}

	if deleted {
		logger.Info("YandexMachine instance is deleted", "instance-id", machineScope.GetInstanceID())
		controllerutil.RemoveFinalizer(machineScope.YandexMachine, infrav1.MachineFinalizer)
		return ctrl.Result{}, nil
	}

	conditions.Set(machineScope.YandexMachine, metav1.Condition{
		Type:    infrav1.InstanceReadyCondition,
		Status:  metav1.ConditionFalse,
		Reason:  infrav1.InstanceDeletingReason,
		Message: "Instance is being deleted",
	})
	logger.Info("YandexMachine instance is deleting", "instance-id", machineScope.GetInstanceID())
	return ctrl.Result{RequeueAfter: RequeueDuration}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *YandexMachineReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	clusterToObjectFunc, err := util.ClusterToTypedObjectsMapper(r.Client, &infrav1.YandexMachineList{}, mgr.GetScheme())
	if err != nil {
		return errors.Wrapf(err, "failed to create mapper for Cluster to YandexMachines")
	}

	err = ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.YandexMachine{}).
		// Paused objects are not filtered out here: the controller has to see them to keep the
		// Paused condition up to date. Reconcile returns early for paused objects instead.
		WithEventFilter(predicates.ResourceIsChanged(mgr.GetScheme(), ctrl.LoggerFrom(ctx))).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(util.MachineToInfrastructureMapFunc(infrav1.GroupVersion.WithKind("YandexMachine"))),
		).
		Watches(
			&infrav1.YandexCluster{},
			handler.EnqueueRequestsFromMapFunc(r.YandexClusterToYandexMachines(ctx)),
		).
		// watch the clusterv1.Cluster object for pause/unpause & infrastructure provisioned notifications.
		Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(clusterToObjectFunc),
			builder.WithPredicates(predicates.ClusterPausedTransitionsOrInfrastructureProvisioned(mgr.GetScheme(), ctrl.LoggerFrom(ctx))),
		).
		Complete(r)
	if err != nil {
		return errors.Wrapf(err, "error creating controller")
	}

	return nil
}

// YandexClusterToYandexMachines is a handler.ToRequestsFunc to be used to enqeue requests for reconciliation
// of YandexMachines.
func (r *YandexMachineReconciler) YandexClusterToYandexMachines(ctx context.Context) handler.MapFunc {
	logger := ctrl.LoggerFrom(ctx)
	return func(mapCtx context.Context, o client.Object) []ctrl.Request {
		result := []ctrl.Request{}

		c, ok := o.(*infrav1.YandexCluster)
		if !ok {
			logger.Error(errors.Errorf("expected a YandexCluster but got a %T", o), "failed to get YandexMachine for YandexCluster")
			return nil
		}

		cluster, err := util.GetOwnerCluster(mapCtx, r.Client, c.ObjectMeta)
		switch {
		case apierrors.IsNotFound(err) || cluster == nil:
			return result
		case err != nil:
			logger.Error(err, "failed to get owning cluster")
			return result
		}

		labels := map[string]string{clusterv1.ClusterNameLabel: cluster.Name}
		machineList := &clusterv1.MachineList{}
		if err := r.List(mapCtx, machineList, client.InNamespace(c.Namespace), client.MatchingLabels(labels)); err != nil {
			logger.Error(err, "failed to list Machines")
			return nil
		}
		for _, m := range machineList.Items {
			if m.Spec.InfrastructureRef.Name == "" {
				continue
			}
			name := client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.InfrastructureRef.Name}
			result = append(result, ctrl.Request{NamespacedName: name})
		}

		return result
	}
}

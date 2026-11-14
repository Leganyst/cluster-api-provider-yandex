package compute

import (
	"context"
	"fmt"

	infrav1 "github.com/yandex-cloud/cluster-api-provider-yandex/api/v1alpha1"
	yandex "github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/client"
	"github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/cloud/ycerrors"
	yandex_compute "github.com/yandex-cloud/go-genproto/yandex/cloud/compute/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	instanceDeleted    bool = true
	instanceNotDeleted bool = false
)

// setInstanceCondition sets the InstanceReady condition of the YandexMachine.
func (s *Service) setInstanceCondition(status metav1.ConditionStatus, reason, message string) {
	conditions.Set(s.scope.YandexMachine, metav1.Condition{
		Type:    infrav1.InstanceReadyCondition,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

// Reconcile reconciles compute instance.
func (s *Service) Reconcile(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Info("reconciling compute instance")

	client := s.scope.GetClient()

	// If InstanceID does not set up, we have to create new instance.
	instanceID := s.scope.GetInstanceID()
	if instanceID == "" {
		newInstanceID, err := s.createComputeInstance(ctx, client)
		if err != nil {
			s.setInstanceCondition(metav1.ConditionFalse, infrav1.InstanceCreateFailedReason, ycerrors.Message(err))
			return err
		}

		s.scope.SetProviderID(newInstanceID)
		logger.Info("compute instance creating")
	}

	// Find compute instance and set status.
	vm, err := client.ComputeGet(ctx, s.scope.GetInstanceID())
	if err != nil {
		if ycerrors.IsNotFound(err) {
			logger.Info("unable to find compute instance in yandex cloud")
			s.scope.SetInstanceStatus(infrav1.InstanceStatusDeleted)
			s.setInstanceCondition(metav1.ConditionFalse, infrav1.InstanceNotFoundReason, ycerrors.Message(err))
			return nil
		}

		s.setInstanceCondition(metav1.ConditionUnknown, infrav1.InstanceReconcileFailedReason, ycerrors.Message(err))
		return fmt.Errorf("unable to find compute instance %v: %w", s.scope.GetInstanceID(), err)
	}

	instanceState := infrav1.InstanceStatus(vm.GetStatus().String())
	switch instanceState {
	case infrav1.InstanceStatusRunning:
		instanceAddress, err := s.getInstanceAddress(vm)
		if err != nil {
			s.setInstanceCondition(metav1.ConditionFalse, infrav1.InstanceReconcileFailedReason, err.Error())
			return err
		}

		s.scope.SetAddresses(instanceAddress)
		if s.scope.IsControlPlane() {
			logger.V(1).Info("registering controlplane compute instance in load balancer")
			if err := s.registerControlPlane(ctx); err != nil {
				s.setInstanceCondition(metav1.ConditionFalse, infrav1.InstanceReconcileFailedReason,
					fmt.Sprintf("failed to register the instance in the control plane load balancer: %s", ycerrors.Message(err)))
				return fmt.Errorf("failed to register controlplane compute instance in load balancer: %w", err)
			}
		}

		s.setInstanceCondition(metav1.ConditionTrue, infrav1.InstanceRunningReason, "")
	case infrav1.InstanceStatusProvisioning, infrav1.InstanceStatusStarting:
		s.setInstanceCondition(metav1.ConditionFalse, infrav1.InstanceProvisioningReason,
			fmt.Sprintf("Instance is in the %s state", instanceState))
	default:
		// The instance exists but does not run (for example it was stopped by the user).
		// The provider intentionally does not restart or recreate it, it only reports the state.
		s.setInstanceCondition(metav1.ConditionFalse, infrav1.InstanceNotRunningReason,
			fmt.Sprintf("Instance is in the %s state", instanceState))
	}

	s.scope.SetInstanceStatus(instanceState)

	return nil
}

// Delete deletes a compute instance.
func (s *Service) Delete(ctx context.Context) (bool, error) {
	logger := log.FromContext(ctx)
	logger.Info("deleting YandexMachine compute instance")
	client := s.scope.GetClient()

	// TODO: bootstap secret deletion handling.
	instanceID := s.scope.GetInstanceID()
	// instance never been created, mark deleted and return.
	if instanceID == "" {
		logger.Info("YandexMachine does not have providerID, instance deleted")
		return instanceDeleted, nil
	}

	// Find compute instance for deletion. If the instance has already been deleted,
	// return with instanceDeleted status.
	vm, err := client.ComputeGet(ctx, s.scope.GetInstanceID())
	if err != nil {
		if !ycerrors.IsNotFound(err) {
			return instanceNotDeleted, fmt.Errorf("failed to get compute instance with id: %s for delete: %w",
				s.scope.GetInstanceID(), err)
		}
		// instance already deleted or deleted by someone else.
		if s.scope.IsControlPlane() {
			// Try to deregister deleted VM to prevent zombie targets in load balancer.
			logger.V(1).Info("deregistering controlplane compute instance from load balancer")
			if err := s.deregisterControlPlane(ctx); err != nil {
				return instanceNotDeleted, fmt.Errorf("failed to deregister controlplane compute instance from load balancer: %w", err)
			}
		}
		return instanceDeleted, nil
	}

	// Check the instance status. If instance already shutting down or terminated,
	// do nothing. Otherwise attempt to delete compute instance.
	instanceStatus := infrav1.InstanceStatus(vm.GetStatus().String())
	s.scope.SetInstanceStatus(instanceStatus)

	if instanceStatus == infrav1.InstanceStatusDeleting {
		return instanceNotDeleted, nil
	}

	if s.scope.IsControlPlane() {
		logger.V(1).Info("deregistering controlplane compute instance form load balancer")
		if err := s.deregisterControlPlane(ctx); err != nil {
			return instanceNotDeleted, fmt.Errorf("failed to deregister controlplane compute instance from load balancer: %w", err)
		}
	}

	return instanceNotDeleted, client.ComputeDelete(ctx, instanceID)
}

// createComputeInstance creates a virtual machine from YandexCompute specification.
func (s *Service) createComputeInstance(ctx context.Context, client yandex.Client) (string, error) {
	request, err := s.scope.GetInstanceReq(ctx)
	if err != nil {
		return "", err
	}
	return client.ComputeCreate(ctx, request)
}

// getInstanceAddress returns the internal IP address of the instance.
func (s *Service) getInstanceAddress(instance *yandex_compute.Instance) ([]corev1.NodeAddress, error) {
	intfList := instance.GetNetworkInterfaces()
	// TODO: MVP support only one interface with IPv4 Address.
	if len(intfList) != 1 {
		return nil, fmt.Errorf("only one interface supported")
	}

	address := intfList[0].GetPrimaryV4Address().GetAddress()
	if address == "" {
		return nil, fmt.Errorf("only IPv4 address supported")
	}

	return []corev1.NodeAddress{{
		Type:    corev1.NodeInternalIP,
		Address: address,
	}}, nil
}

// registerControlPlane adds controlplane instance address to controlplane loadbalancer target group.
func (s *Service) registerControlPlane(ctx context.Context) error {
	addresses := s.scope.GetAddresses()
	if len(addresses) == 0 {
		return fmt.Errorf("no addresses registered for YandexMachne %s", s.scope.Name())
	}

	address := addresses[0].Address
	subnetID := s.scope.YandexMachine.Spec.NetworkInterfaces[0].SubnetID
	return s.scope.LoadBalancer.AddTarget(ctx, address, subnetID)
}

// deregisterControlPlane removes controlplane instance address from controlplane loadbalancer target group.
func (s *Service) deregisterControlPlane(ctx context.Context) error {
	logger := log.FromContext(ctx)
	addresses := s.scope.GetAddresses()
	// if instance have no addresses, skip deregistration.
	if len(addresses) == 0 {
		logger.V(1).Info("no addresses registered for YandexMachne", "name", s.scope.Name())
		return nil
	}

	address := addresses[0].Address
	subnetID := s.scope.YandexMachine.Spec.NetworkInterfaces[0].SubnetID
	return s.scope.LoadBalancer.RemoveTarget(ctx, address, subnetID)
}

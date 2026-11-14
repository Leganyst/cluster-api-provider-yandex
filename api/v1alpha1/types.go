package v1alpha1

// InstanceStatus describes the status of a Yandex Cloud Compute instance.
type InstanceStatus string

var (
	// InstanceStatusProvisioning is the string representing an instance in a provisioning state.
	InstanceStatusProvisioning = InstanceStatus("PROVISIONING")
	// InstanceStatusRunning is the string representing an instance in a running state.
	InstanceStatusRunning = InstanceStatus("RUNNING")
	// InstanceStatusError is the string representing an instance in a error state.
	InstanceStatusError = InstanceStatus("ERROR")
	// InstanceStatusStopped is the string representing an instance in a stopped state.
	InstanceStatusStopped = InstanceStatus("STOPPED")
	// InstanceStatusStarting is the string representing an instance in a starting state.
	InstanceStatusStarting = InstanceStatus("STARTING")
	// InstanceStatusStopping is the string representing an instance in a stopping state.
	InstanceStatusStopping = InstanceStatus("STOPPING")
	// InstanceStatusRestarting is the string representing an instance in a restarting state.
	InstanceStatusRestarting = InstanceStatus("RESTARTING")
	// InstanceStatusUpdating is the string representing an instance in a updating state.
	InstanceStatusUpdating = InstanceStatus("UPDATING")
	// InstanceStatusCrashed is the string representing an instance in a crashed state.
	InstanceStatusCrashed = InstanceStatus("CRASHED")
	// InstanceStatusDeleting is the string representing an instance in a deleting state.
	InstanceStatusDeleting = InstanceStatus("DELETING")
	// InstanceStatusDeleted is the string representing an instance in a deleted state.
	InstanceStatusDeleted = InstanceStatus("DELETED")
	// InstanceStatusUnspecified is the string representing an instance in a unknown state.
	InstanceStatusUnspecified = InstanceStatus("UNSPECIFIED")
)

// Conditions shared by YandexCluster and YandexMachine.
//
// All conditions use the standard metav1.Condition type, as required by the
// v1beta2 version of the Cluster API contract. Condition types and reasons are
// CamelCase, as required by the Kubernetes API conventions.
const (
	// ReadyCondition is the summary condition of a resource. Cluster API mirrors it into the
	// InfrastructureReady condition of the owning Cluster or Machine.
	// It reflects the state of the resource during its entire lifecycle: provisioning,
	// normal operation and deletion.
	ReadyCondition = "Ready"

	// ReadyReason surfaces that the resource is ready.
	ReadyReason = "Ready"
	// NotReadyReason surfaces that the resource is not ready.
	NotReadyReason = "NotReady"
	// ReadyUnknownReason surfaces that the readiness of the resource could not be determined.
	ReadyUnknownReason = "ReadyUnknown"

	// PausedCondition reports whether reconciliation of the resource is paused, either by
	// spec.paused of the owning Cluster or by the cluster.x-k8s.io/paused annotation.
	// The condition is managed by the Cluster API paused utility.
	PausedCondition = "Paused"
)

// Conditions and reasons of YandexCluster.
const (
	// LoadBalancerReadyCondition reports on whether the control plane load balancer is provisioned and active.
	LoadBalancerReadyCondition = "LoadBalancerReady"

	// LoadBalancerActiveReason surfaces that the load balancer is active and its listener
	// matches the control plane endpoint.
	LoadBalancerActiveReason = "Active"
	// LoadBalancerProvisioningReason surfaces that the load balancer exists but is not active yet.
	LoadBalancerProvisioningReason = "Provisioning"
	// LoadBalancerReconcileFailedReason surfaces an error occurred during load balancer reconciliation.
	LoadBalancerReconcileFailedReason = "ReconcileFailed"
	// LoadBalancerEndpointMismatchReason surfaces that the load balancer listener differs from the
	// control plane endpoint of the YandexCluster. The cluster can not be recovered automatically.
	LoadBalancerEndpointMismatchReason = "EndpointMismatch"
	// LoadBalancerDeletingReason surfaces that the load balancer is being deleted.
	LoadBalancerDeletingReason = "Deleting"
	// LoadBalancerDeletionFailedReason surfaces an error occurred during load balancer deletion.
	LoadBalancerDeletionFailedReason = "DeletionFailed"
)

// Conditions and reasons of YandexMachine.
const (
	// InstanceReadyCondition reports on the state of the Yandex Compute instance backing the YandexMachine.
	InstanceReadyCondition = "InstanceReady"

	// InstanceRunningReason surfaces that the instance is in the RUNNING state.
	InstanceRunningReason = "Running"
	// InstanceProvisioningReason surfaces that the instance is being created or started.
	InstanceProvisioningReason = "Provisioning"
	// InstanceNotRunningReason surfaces that the instance exists but is in a state other than RUNNING,
	// PROVISIONING or STARTING (for example STOPPED). The provider does not restart or recreate such
	// an instance; the condition becomes true again as soon as the instance is running.
	InstanceNotRunningReason = "NotRunning"
	// InstanceNotFoundReason surfaces that the instance referenced by spec.providerID does not exist anymore.
	InstanceNotFoundReason = "NotFound"
	// InstanceCreateFailedReason surfaces an error occurred while creating the instance.
	InstanceCreateFailedReason = "CreateFailed"
	// InstanceReconcileFailedReason surfaces an error occurred while reconciling an existing instance.
	InstanceReconcileFailedReason = "ReconcileFailed"
	// WaitingForBootstrapDataReason surfaces that the instance can not be created yet, because
	// the bootstrap data secret of the owning Machine is not available.
	WaitingForBootstrapDataReason = "WaitingForBootstrapData"
	// InstanceDeletingReason surfaces that the instance is being deleted.
	InstanceDeletingReason = "Deleting"
	// InstanceDeletionFailedReason surfaces an error occurred during instance deletion.
	InstanceDeletionFailedReason = "DeletionFailed"
)

// Conditions and reasons of YandexIdentity.
const (
	// IdentityReadyCondition reports on whether a Yandex Identity was successfully reconciled.
	IdentityReadyCondition = "IdentityReady"
	// IdentityValidCondition reports on whether a Yandex Identity is valid.
	IdentityValidCondition = "IdentityValid"
	// IdentitySecretUpdatedCondition reports on whether a Yandex Identity secret was updated.
	IdentitySecretUpdatedCondition = "IdentitySecretUpdated"
	// IdentityLinkedClustersUpdatedCondition reports on whether a Yandex Identity linked clusters were updated.
	IdentityLinkedClustersUpdatedCondition = "IdentityLinkedClustersUpdated"

	// IdentitySucceededReason is used for all YandexIdentity conditions with status True.
	IdentitySucceededReason = "Succeeded"
	// IdentityReconciliationErrorReason surfaces an error occurred during YandexIdentity reconciliation.
	IdentityReconciliationErrorReason = "ReconciliationError"
	// IdentitySecretValidationFailedReason surfaces that the identity secret could not be read or validated.
	IdentitySecretValidationFailedReason = "SecretValidationFailed"
	// IdentityConnectionCheckFailedReason surfaces that a Yandex Cloud client could not be built from the identity key.
	IdentityConnectionCheckFailedReason = "ConnectionCheckFailed"
	// IdentityKeyHashUpdateFailedReason surfaces that the hash of the identity key could not be computed.
	IdentityKeyHashUpdateFailedReason = "KeyHashUpdateFailed"
	// IdentitySecretUpdateFailedReason surfaces that the finalizer or owner reference could not be set on the identity secret.
	IdentitySecretUpdateFailedReason = "SecretUpdateFailed"
	// IdentityLinkedClustersUpdateFailedReason surfaces that the list of linked clusters could not be updated.
	IdentityLinkedClustersUpdateFailedReason = "LinkedClustersUpdateFailed"
)

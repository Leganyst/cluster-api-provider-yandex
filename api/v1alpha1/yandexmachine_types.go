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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// MachineFinalizer allows cleaning up resources associated with
	// YandexMachine before removing it from the apiserver.
	MachineFinalizer = "yandexmachine.infrastructure.cluster.x-k8s.io"
)

//+kubebuilder:validation:Required

// YandexMachineSpec defines the desired state of YandexMachine.
type YandexMachineSpec struct {
	// providerID must match the provider ID as seen on the node object corresponding to this machine.
	// It has the format yandex://<instance-id> and is set by the provider once the instance is created.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	ProviderID string `json:"providerID,omitempty"`

	// ZoneID is the identifier of YandexCloud availability zone.
	// +optional
	// +kubebuilder:default=ru-central1-d
	ZoneID *string `json:"zoneID,omitempty"`

	// PlatformID is the identifier of YandexCloud current CPU model.
	// For example: standard-v1, standard-v2, standard-v3, highfreq-v3
	// With GPU: gpu-standard-v1, gpu-standard-v2, gpu-standard-v3, standard-v3-t4
	// More information https://cloud.yandex.ru/ru/docs/compute/concepts/vm-platforms .
	// +optional
	// +kubebuilder:default=standard-v3
	PlatformID *string `json:"platformID,omitempty"`

	// Disk is boot storage configuration for YandexCloud VM.
	BootDisk *Disk `json:"bootDisk"`

	// Resources contains computing resources of YandexCloud VM.
	Resources Resources `json:"resources"`

	// NetworkInterfaces is a network interfaces configurations for YandexCloud VM
	NetworkInterfaces []NetworkInterface `json:"networkInterfaces"`
}

// NetworkInterface defines the network interface configuration of YandexCloud VM.
type NetworkInterface struct {
	// SubnetID is the identifier of subnetwork to use for this instance.
	SubnetID string `json:"subnetID"`

	// HasPublicIP is set to true if public IP for YandexCloud VM is needed.
	// +optional
	// +kubebuilder:default=false
	HasPublicIP *bool `json:"hasPublicIP,omitempty"`
}

// Resources defines the YandexCloud VM resources, like cores, memory etc.
type Resources struct {
	// Memory is the RAM memory size for YandexCloud VM in bytes
	// Allows to specify k,M,G... or Ki,Mi,Gi... suffixes
	// For more information see https://pkg.go.dev/k8s.io/apimachinery/pkg/api/resource#Quantity .
	Memory resource.Quantity `json:"memory"`

	// Cores is the number of cpu cores for YandexCloud VM.
	Cores int64 `json:"cores"`

	// CoreFraction is baseline level of CPU performance with the ability to burst performance above that baseline level.
	// This field sets baseline performance for each core.
	// For more information see https://yandex.cloud/en/docs/compute/concepts/performance-levels
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=100
	CoreFraction *int64 `json:"coreFraction,omitempty"`

	// GPUs is the number of GPUs available for YandexCloud VM.
	// +optional
	GPUs *int64 `json:"gpus,omitempty"`
}

// Disk defines YandexCloud VM disk configuration.
type Disk struct {
	// TypeID is the disk storage type for YandexCloud VM
	// Possible values: network-ssd, network-hdd, network-ssd-nonreplicated, network-ssd-io-m3
	// More information https://cloud.yandex.ru/ru/docs/compute/concepts/disk .
	// +optional
	// +kubebuilder:default=network-ssd
	TypeID *string `json:"typeID,omitempty"`

	// Size is the disk size in bytes
	// Allows to specify k,M,G... or Ki,Mi,Gi... suffixes
	// For more information see https://pkg.go.dev/k8s.io/apimachinery/pkg/api/resource#Quantity .
	Size resource.Quantity `json:"size"`

	// ImageID is the identifier for OS image of YandexCloud VM.
	ImageID string `json:"imageID"`
}

// YandexMachineStatus defines the observed state of YandexMachine.
type YandexMachineStatus struct {
	// conditions represents the observations of a YandexMachine's current state.
	// Known condition types are Ready, InstanceReady and Paused.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=32
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// initialization provides observations of the YandexMachine initialization process.
	// NOTE: Fields in this struct are part of the Cluster API contract and are used to orchestrate initial Machine provisioning.
	// +optional
	Initialization YandexMachineInitializationStatus `json:"initialization,omitempty,omitzero"`

	// Addresses contains the YandexCloud instance associated addresses.
	// +optional
	Addresses []corev1.NodeAddress `json:"addresses,omitempty"`

	// InstanceStatus is the status of the Yandex instance for this machine.
	// +optional
	InstanceStatus *InstanceStatus `json:"instanceState,omitempty"`
}

// YandexMachineInitializationStatus provides observations of the YandexMachine initialization process.
// +kubebuilder:validation:MinProperties=1
type YandexMachineInitializationStatus struct {
	// provisioned is true when the infrastructure provider reports that the Machine's infrastructure is fully provisioned.
	// NOTE: this field is part of the Cluster API contract, and it is used to orchestrate initial Machine provisioning.
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=yandexmachines,scope=Namespaced,categories=cluster-api
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels.cluster\\.x-k8s\\.io/cluster-name",description="Cluster to which this YandexMachine belongs"
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.instanceState",description="Yandex Compute instance state"
// +kubebuilder:printcolumn:name="Provisioned",type="string",JSONPath=".status.initialization.provisioned",description="Machine infrastructure is provisioned"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Machine infrastructure is ready"
// +kubebuilder:printcolumn:name="Paused",type="string",JSONPath=`.status.conditions[?(@.type=="Paused")].status`,description="Reconciliation paused",priority=10
// +kubebuilder:printcolumn:name="Provider ID",type="string",JSONPath=".spec.providerID",description="Provider ID",priority=10
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time duration since creation of YandexMachine"

// YandexMachine is the Schema for the yandexmachines API.
type YandexMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   YandexMachineSpec   `json:"spec,omitempty"`
	Status YandexMachineStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// YandexMachineList contains a list of YandexMachine.
type YandexMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []YandexMachine `json:"items"`
}

// GetConditions returns the set of conditions for this object.
func (ym *YandexMachine) GetConditions() []metav1.Condition {
	return ym.Status.Conditions
}

// SetConditions sets conditions for an API object.
func (ym *YandexMachine) SetConditions(conditions []metav1.Condition) {
	ym.Status.Conditions = conditions
}

func init() {
	objectTypes = append(objectTypes, &YandexMachine{}, &YandexMachineList{})
}

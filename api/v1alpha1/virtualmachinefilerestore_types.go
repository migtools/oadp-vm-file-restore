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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VirtualMachineFileRestorePhase is a high-level summary of the lifecycle of a VirtualMachineFileRestore.
// It describes the current state of the file-serving workflow — not actual restore in the VM.
// +kubebuilder:validation:Enum=New;BackingOff;Created;Deleting
type VirtualMachineFileRestorePhase string

const (
	// VirtualMachineFileRestorePhaseNew indicates the request was accepted by the cluster,
	// but the controller has not yet started processing it.
	VirtualMachineFileRestorePhaseNew VirtualMachineFileRestorePhase = "New"

	// VirtualMachineFileRestorePhaseBackingOff indicates that processing failed temporarily,
	// for example due to configuration issues or resource unavailability.
	// The controller may retry automatically.
	VirtualMachineFileRestorePhaseBackingOff VirtualMachineFileRestorePhase = "BackingOff"

	// VirtualMachineFileRestorePhaseCreated indicates that the necessary resources (e.g., pods)
	// have been created and the requested files are now accessible to the user.
	VirtualMachineFileRestorePhaseCreated VirtualMachineFileRestorePhase = "Created"

	// VirtualMachineFileRestorePhaseDeleting indicates that the VirtualMachineFileRestore
	// resource is pending deletion and cleanup of associated resources is in progress.
	VirtualMachineFileRestorePhaseDeleting VirtualMachineFileRestorePhase = "Deleting"
)

// VirtualMachineFileRestoreCondition represents detailed, granular states
// of a VirtualMachineFileRestore. A single object may have multiple conditions
// simultaneously, providing more insight than the high-level Phase alone.
// +kubebuilder:validation:Enum=Accepted;BackupsDiscovered;FileRestoreRequested;FilesReady;Queued;Deleting
type VirtualMachineFileRestoreCondition string

const (
	// VirtualMachineFileRestoreConditionAccepted indicates the controller has acknowledged
	// the request and it is valid for processing. This is typically the first condition set.
	VirtualMachineFileRestoreConditionAccepted VirtualMachineFileRestoreCondition = "Accepted"

	// VirtualMachineFileRestoreConditionBackupsDiscovered indicates that the controller
	// has successfully discovered one or more Velero backups matching the selection criteria
	// (time range or explicit list).
	VirtualMachineFileRestoreConditionBackupsDiscovered VirtualMachineFileRestoreCondition = "BackupsDiscovered"

	// VirtualMachineFileRestoreConditionFileRestoreRequested indicates that the user
	// or system has requested access to files from the discovered backups.
	VirtualMachineFileRestoreConditionFileRestoreRequested VirtualMachineFileRestoreCondition = "FileRestoreRequested"

	// VirtualMachineFileRestoreConditionFilesReady indicates that the required files
	// have been prepared and are now accessible to the user via the serving pod.
	VirtualMachineFileRestoreConditionFilesReady VirtualMachineFileRestoreCondition = "FilesReady"

	// VirtualMachineFileRestoreConditionQueued indicates that the request is waiting
	// in a queue before processing, e.g., due to concurrency limits or other resources constraints.
	VirtualMachineFileRestoreConditionQueued VirtualMachineFileRestoreCondition = "Queued"

	// VirtualMachineFileRestoreConditionDeleting indicates that the object is being deleted
	// and the controller is cleaning up associated resources.
	VirtualMachineFileRestoreConditionDeleting VirtualMachineFileRestoreCondition = "Deleting"
)

// SelectionMode defines how backups should be selected for file restore.
// +kubebuilder:validation:Enum=AllInRange;ExplicitList
type SelectionMode string

const (
	// SelectionModeAllInRange means all backups discovered between StartTime and EndTime
	// are to be restored.
	SelectionModeAllInRange SelectionMode = "AllInRange"

	// SelectionModeExplicitList means exactly the backups in RequestedBackups should be restored.
	SelectionModeExplicitList SelectionMode = "ExplicitList"
)

// VeleroBackupInfo contains information about a discovered backup
type VeleroBackupInfo struct {
	Name      string      `json:"name"`
	Namespace string      `json:"namespace"`
	CreatedAt metav1.Time `json:"createdAt"`
}

// VirtualMachineFileRestoreSpec defines the desired state of VirtualMachineFileRestore
type VirtualMachineFileRestoreSpec struct {
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// VirtualMachineName specifies the name of the VirtualMachine whose backups are to be listed or restored.
	// +kubebuilder:validation:MinLength=1
	// +required
	VirtualMachineName string `json:"virtualMachineName"`

	// VirtualMachineNamespace specifies the namespace of the VirtualMachine.
	// +kubebuilder:validation:MinLength=1
	// +required
	VirtualMachineNamespace string `json:"virtualMachineNamespace"`

	// SelectionMode defines how backups are chosen for file restore.
	// Valid values: "AllInRange", "ExplicitList".
	// +kubebuilder:validation:Enum=AllInRange;ExplicitList
	// +kubebuilder:default=AllInRange
	// +required
	SelectionMode SelectionMode `json:"selectionMode"`

	// StartTime is an optional field to filter backups created after this time.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// EndTime is an optional field to filter backups created before this time.
	// +optional
	EndTime *metav1.Time `json:"endTime,omitempty"`

	// RequestedBackups lists specific backups to restore.
	// Used only when SelectionMode is "ExplicitList".
	// +optional
	RequestedBackups []string `json:"requestedBackups,omitempty"`
}

// VirtualMachineFileRestoreStatus defines the observed state of VirtualMachineFileRestore.
type VirtualMachineFileRestoreStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the VirtualMachineFileRestore resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase indicates the overall phase of the file-serving operation
	// +optional
	Phase VirtualMachineFileRestorePhase `json:"phase,omitempty"`

	// DiscoveredBackups lists all backups found matching the selection criteria
	// +optional
	DiscoveredBackups []VeleroBackupInfo `json:"discoveredBackups,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VirtualMachineFileRestore is the Schema for the virtualmachinefilerestores API
type VirtualMachineFileRestore struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of VirtualMachineFileRestore
	// +required
	Spec VirtualMachineFileRestoreSpec `json:"spec"`

	// status defines the observed state of VirtualMachineFileRestore
	// +optional
	Status VirtualMachineFileRestoreStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// VirtualMachineFileRestoreList contains a list of VirtualMachineFileRestore
type VirtualMachineFileRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VirtualMachineFileRestore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VirtualMachineFileRestore{}, &VirtualMachineFileRestoreList{})
}

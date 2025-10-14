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
	"github.com/migtools/oadp-vm-file-restore/api/v1alpha1/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// DiscoveryStatistics provides summary information about backup discovery
type DiscoveryStatistics struct {
	// +kubebuilder:validation:Minimum=0
	TotalCandidates int          `json:"totalCandidates"`
	StartTime       *metav1.Time `json:"startTime,omitempty"`
	CompletionTime  *metav1.Time `json:"completionTime,omitempty"`
	// +kubebuilder:validation:Minimum=0
	Pending int `json:"pending"`
	// +kubebuilder:validation:Minimum=0
	InProgress int `json:"inProgress"`
	// +kubebuilder:validation:Minimum=0
	Completed int `json:"completed"`
	// +kubebuilder:validation:Minimum=0
	Skipped int `json:"skipped"`
	// +kubebuilder:validation:Minimum=0
	Failed int `json:"failed"`
}

// VirtualMachineBackupsDiscoverySpec defines the desired state of VirtualMachineBackupsDiscovery
type VirtualMachineBackupsDiscoverySpec struct {
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// Name of the VirtualMachine to discover backups for.
	// +kubebuilder:validation:MinLength=1
	// +required
	VirtualMachineName string `json:"virtualMachineName"`

	// Namespace where the VirtualMachine is located.
	// +kubebuilder:validation:MinLength=1
	// +required
	VirtualMachineNamespace string `json:"virtualMachineNamespace"`

	// Only include backups created after this time (optional time range filtering).
	// Supports both date-only (YYYY-MM-DD) and full RFC3339 (YYYY-MM-DDTHH:MM:SSZ) formats.
	// Date-only format defaults to start of day (00:00:00Z).
	// +optional
	// +kubebuilder:validation:Type=string
	StartTime *types.FlexibleTime `json:"startTime,omitempty"`

	// Only include backups created before this time (optional time range filtering).
	// Supports both date-only (YYYY-MM-DD) and full RFC3339 (YYYY-MM-DDTHH:MM:SSZ) formats.
	// Date-only format defaults to end of day (23:59:59Z).
	// +optional
	// +kubebuilder:validation:Type=string
	EndTime *types.FlexibleTime `json:"endTime,omitempty"`

	// Specific backup names to include in addition to any time-based filtering.
	// If specified, these backups will be included even if they fall outside the time range.
	// +optional
	RequestedBackups []string `json:"requestedBackups,omitempty"`
}

// VirtualMachineBackupsDiscoveryPhase represents the high-level state of backup discovery.
// These phases match Velero's phase model for consistency with OADP's Velero foundation.
// +kubebuilder:validation:Enum=New;InProgress;Completed;PartiallyFailed;Failed
type VirtualMachineBackupsDiscoveryPhase string

const (
	// VirtualMachineBackupsDiscoveryPhaseNew indicates the discovery request has been
	// accepted but the controller has not yet started processing it.
	VirtualMachineBackupsDiscoveryPhaseNew VirtualMachineBackupsDiscoveryPhase = "New"

	// VirtualMachineBackupsDiscoveryPhaseInProgress indicates discovery is actively
	// scanning backups to identify those containing the specified VM.
	VirtualMachineBackupsDiscoveryPhaseInProgress VirtualMachineBackupsDiscoveryPhase = "InProgress"

	// VirtualMachineBackupsDiscoveryPhaseCompleted indicates discovery finished successfully
	// and all candidate backups were scanned without errors. Valid backups were found.
	VirtualMachineBackupsDiscoveryPhaseCompleted VirtualMachineBackupsDiscoveryPhase = "Completed"

	// VirtualMachineBackupsDiscoveryPhasePartiallyFailed indicates discovery finished
	// but some backups failed to scan or were invalid. At least one valid backup was found,
	// making the discovery result usable despite partial failures.
	VirtualMachineBackupsDiscoveryPhasePartiallyFailed VirtualMachineBackupsDiscoveryPhase = "PartiallyFailed"

	// VirtualMachineBackupsDiscoveryPhaseFailed indicates discovery failed completely
	// due to unrecoverable errors (e.g., no backup storage locations, invalid spec,
	// or no valid backups found).
	VirtualMachineBackupsDiscoveryPhaseFailed VirtualMachineBackupsDiscoveryPhase = "Failed"
)

// VirtualMachineBackupsDiscoveryStatus defines the observed state of VirtualMachineBackupsDiscovery.
type VirtualMachineBackupsDiscoveryStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// Phase indicates the overall phase of the backup discovery operation.
	// Derived from conditions for human readability. Matches Velero's phase model.
	// Automation should rely on conditions, not phase.
	// +optional
	Phase VirtualMachineBackupsDiscoveryPhase `json:"phase,omitempty"`

	// ObservedGeneration represents the .metadata.generation that the status was set based upon.
	// For instance, if .metadata.generation is currently 12, but the .status.observedGeneration is 9,
	// the status is out of date with respect to the current state of the instance.
	//
	// IMPORTANT: Controllers must set this at the START of reconciliation, not at the end.
	// This prevents race conditions where clients see updated conditions but stale observedGeneration.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the current state of the VirtualMachineBackupsDiscovery resource.
	// This is the PRIMARY source of truth for resource state.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types for this resource (defined in types package):
	// - types.ConditionTypeProgressing: Discovery is actively running
	// - types.ConditionTypeAvailable: Valid backups are available for use
	// - types.ConditionTypeDegraded: Partial failures occurred (may still be usable)
	// - types.ConditionTypeReady: Summary condition (resource is usable)
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Backups that contain the specified virtual machine and are ready for file serving.
	// +optional
	ValidBackups []types.VeleroBackupInfo `json:"validBackups,omitempty"`

	// Requested backups that don't contain the VM (only populated when RequestedBackups is used).
	// +optional
	InvalidBackups []types.InvalidBackupInfo `json:"invalidBackups,omitempty"`

	// Detailed discovery progress for each candidate backup.
	// +optional
	BackupDiscoveryProgress []types.BackupDiscoveryProgress `json:"backupDiscoveryProgress,omitempty"`

	// Summary statistics about the backup discovery process.
	// +optional
	DiscoveryStats *DiscoveryStatistics `json:"discoveryStats,omitempty"`
}

// +kubebuilder:storageversion
// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=vmbd,scope=Namespaced
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="VM",type=string,JSONPath=`.spec.virtualMachineName`
// +kubebuilder:printcolumn:name="VMNS",type=string,JSONPath=`.spec.virtualMachineNamespace`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Valid",type=integer,JSONPath=`.status.discoveryStats.completed`
// +kubebuilder:printcolumn:name="Invalid",type=integer,JSONPath=`.status.discoveryStats.skipped`

// VirtualMachineBackupsDiscovery is the Schema for the virtualmachinebackupsdiscoveries API
type VirtualMachineBackupsDiscovery struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of VirtualMachineBackupsDiscovery
	// +required
	Spec VirtualMachineBackupsDiscoverySpec `json:"spec"`

	// status defines the observed state of VirtualMachineBackupsDiscovery
	// +optional
	Status VirtualMachineBackupsDiscoveryStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// VirtualMachineBackupsDiscoveryList contains a list of VirtualMachineBackupsDiscovery
type VirtualMachineBackupsDiscoveryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VirtualMachineBackupsDiscovery `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VirtualMachineBackupsDiscovery{}, &VirtualMachineBackupsDiscoveryList{})
}

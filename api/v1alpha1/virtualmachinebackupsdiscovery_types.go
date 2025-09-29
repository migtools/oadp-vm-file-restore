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
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// BackupPVCInfo contains minimal PVC information from a backup
type BackupPVCInfo struct {
	// Name is the name of the PersistentVolumeClaim
	Name string `json:"name"`

	// Namespace is the namespace of the PersistentVolumeClaim
	Namespace string `json:"namespace"`

	// UID is the UID of the PersistentVolumeClaim resource
	UID string `json:"uid"`

	// Size of the PVC storage request from the backup contents in human-readable format (e.g., "5Gi", "30Gi")
	// +optional
	Size string `json:"size,omitempty"`
}

// VeleroBackupInfo contains information about a discovered backup
type VeleroBackupInfo struct {
	// Name of the backup resource.
	Name string `json:"name"`
	// When the backup was created.
	CreatedAt *metav1.Time `json:"createdAt,omitempty"`
	// PVCs contains the list of PVCs available in this backup
	// This field is populated during file restore processing
	// +optional
	PVCs []BackupPVCInfo `json:"pvcs,omitempty"`
}

// InvalidBackupInfo contains information about a backup that doesn't contain the VM
type InvalidBackupInfo struct {
	VeleroBackupInfo `json:",inline"`
	// Reason why this backup doesn't contain the VM or couldn't be processed.
	// +kubebuilder:validation:MaxLength=1024
	Reason string `json:"reason,omitempty"`
}

// BackupDiscoveryStatus represents the status of backup discovery for a specific backup
// +kubebuilder:validation:Enum=Pending;InProgress;Completed;Skipped;Failed
type BackupDiscoveryStatus string

const (
	// BackupDiscoveryStatusPending indicates the backup discovery has not started yet
	BackupDiscoveryStatusPending BackupDiscoveryStatus = "Pending"

	// BackupDiscoveryStatusInProgress indicates the backup is currently being analyzed
	BackupDiscoveryStatusInProgress BackupDiscoveryStatus = "InProgress"

	// BackupDiscoveryStatusCompleted indicates the backup analysis is complete and VM was found
	BackupDiscoveryStatusCompleted BackupDiscoveryStatus = "Completed"

	// BackupDiscoveryStatusSkipped indicates the backup was skipped (e.g., doesn't contain the VM)
	BackupDiscoveryStatusSkipped BackupDiscoveryStatus = "Skipped"

	// BackupDiscoveryStatusFailed indicates the backup analysis failed
	BackupDiscoveryStatusFailed BackupDiscoveryStatus = "Failed"
)

// BackupDiscoveryInfo contains detailed information about backup discovery progress
type BackupDiscoveryInfo struct {
	VeleroBackupInfo `json:",inline"`
	// Current status of backup discovery for this backup.
	Status BackupDiscoveryStatus `json:"status"`
	// Human-readable message about the discovery status.
	// +kubebuilder:validation:MaxLength=1024
	Message string `json:"message,omitempty"`
	// When this backup's discovery status was last updated.
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`
}

// FlexibleTime supports both date-only (YYYY-MM-DD) and full RFC3339 (YYYY-MM-DDTHH:MM:SSZ) formats
// +kubebuilder:validation:Type=string
type FlexibleTime string

// Time returns the parsed time.Time value
func (ft FlexibleTime) Time() (time.Time, error) {
	str := string(ft)
	if str == "" {
		return time.Time{}, nil
	}

	// Try RFC3339 format first
	if t, err := time.Parse(time.RFC3339, str); err == nil {
		return t, nil
	}

	// Try date-only format (YYYY-MM-DD)
	if t, err := time.Parse("2006-01-02", str); err == nil {
		return t.UTC(), nil
	}

	return time.Time{}, fmt.Errorf("unable to parse time %q: expected RFC3339 (2006-01-02T15:04:05Z) or date-only (2006-01-02) format", str)
}

// GetEndOfDay returns a FlexibleTime set to the end of the day (23:59:59.999999999Z)
// for the same date as the receiver
func (ft FlexibleTime) GetEndOfDay() (FlexibleTime, error) {
	t, err := ft.Time()
	if err != nil {
		return "", err
	}
	year, month, day := t.Date()
	endOfDay := time.Date(year, month, day, 23, 59, 59, 999999999, time.UTC)
	return FlexibleTime(endOfDay.Format(time.RFC3339)), nil
}

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
	StartTime *FlexibleTime `json:"startTime,omitempty"`

	// Only include backups created before this time (optional time range filtering).
	// Supports both date-only (YYYY-MM-DD) and full RFC3339 (YYYY-MM-DDTHH:MM:SSZ) formats.
	// Date-only format defaults to end of day (23:59:59Z).
	// +optional
	// +kubebuilder:validation:Type=string
	EndTime *FlexibleTime `json:"endTime,omitempty"`

	// Specific backup names to include in addition to any time-based filtering.
	// If specified, these backups will be included even if they fall outside the time range.
	// +optional
	RequestedBackups []string `json:"requestedBackups,omitempty"`
}

// VirtualMachineBackupsDiscoveryPhase represents the high-level state of backup discovery.
// +kubebuilder:validation:Enum=New;InProgress;Completed;Failed
type VirtualMachineBackupsDiscoveryPhase string

const (
	// VirtualMachineBackupsDiscoveryPhaseNew indicates discovery has not started yet.
	VirtualMachineBackupsDiscoveryPhaseNew VirtualMachineBackupsDiscoveryPhase = "New"

	// VirtualMachineBackupsDiscoveryPhaseInProgress indicates discovery is actively running.
	VirtualMachineBackupsDiscoveryPhaseInProgress VirtualMachineBackupsDiscoveryPhase = "InProgress"

	// VirtualMachineBackupsDiscoveryPhaseCompleted indicates discovery has finished successfully.
	VirtualMachineBackupsDiscoveryPhaseCompleted VirtualMachineBackupsDiscoveryPhase = "Completed"

	// VirtualMachineBackupsDiscoveryPhaseFailed indicates discovery failed due to errors.
	VirtualMachineBackupsDiscoveryPhaseFailed VirtualMachineBackupsDiscoveryPhase = "Failed"
)

// VirtualMachineBackupsDiscoveryCondition represents the state of a VirtualMachineBackupsDiscovery.
// +kubebuilder:validation:Enum=Complete
type VirtualMachineBackupsDiscoveryCondition string

const (
	// VirtualMachineBackupsDiscoveryConditionComplete indicates that backup
	// discovery has completed successfully and valid backups have been identified.
	VirtualMachineBackupsDiscoveryConditionComplete VirtualMachineBackupsDiscoveryCondition = "Complete"
)

// VirtualMachineBackupsDiscoveryStatus defines the observed state of VirtualMachineBackupsDiscovery.
type VirtualMachineBackupsDiscoveryStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// Phase indicates the overall phase of the backup discovery operation.
	// +optional
	Phase VirtualMachineBackupsDiscoveryPhase `json:"phase,omitempty"`

	// observedGeneration represents the .metadata.generation that the status was set based upon.
	// For instance, if .metadata.generation is currently 12, but the .status.observedGeneration is 9,
	// the status is out of date with respect to the current state of the instance.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the VirtualMachineBackupsDiscovery resource.
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

	// Backups that contain the specified virtual machine and are ready for file serving.
	// +optional
	ValidBackups []VeleroBackupInfo `json:"validBackups,omitempty"`

	// Requested backups that don't contain the VM (only populated when RequestedBackups is used).
	// +optional
	InvalidBackups []InvalidBackupInfo `json:"invalidBackups,omitempty"`

	// Detailed discovery progress for each candidate backup.
	// +optional
	BackupDiscoveryProgress []BackupDiscoveryInfo `json:"backupDiscoveryProgress,omitempty"`

	// Summary statistics about the backup discovery process.
	// +optional
	DiscoveryStats *DiscoveryStatistics `json:"discoveryStats,omitempty"`
}

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

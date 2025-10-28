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

package types

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VeleroBackupInfo contains information about a discovered backup
// +k8s:deepcopy-gen=true
type VeleroBackupInfo struct {
	// Name of the backup resource.
	Name string `json:"name"`

	// Namespace is the namespace of the backup resource
	Namespace string `json:"namespace"`

	// When the backup was taken (from backup.status.completionTimestamp).
	// For synced backups, this reflects when the backup actually completed, not when it was imported.
	// +optional
	CreatedAt *metav1.Time `json:"createdAt,omitempty"`

	// PVCs contains the list of PVCs available in this backup
	// For a given VM
	// This field is populated during file restore processing
	// +optional
	PVCs []PVCInfo `json:"pvcs,omitempty"`
}

// InvalidBackupInfo contains information about a backup that doesn't contain the VM
// +k8s:deepcopy-gen=true
type InvalidBackupInfo struct {
	VeleroBackupInfo `json:",inline"`

	// Reason why this backup doesn't contain the VM or couldn't be processed.
	// +kubebuilder:validation:MaxLength=1024
	Reason string `json:"reason,omitempty"`
}

// BackupDiscoveryStatus represents the status of backup discovery for a specific backup.
// These statuses match Velero's phase model for consistency.
// +k8s:deepcopy-gen=true
// +kubebuilder:validation:Enum=New;InProgress;Completed;Skipped;Failed
type BackupDiscoveryStatus string

const (
	// BackupDiscoveryStatusNew indicates the backup discovery has not started yet
	BackupDiscoveryStatusNew BackupDiscoveryStatus = "New"

	// BackupDiscoveryStatusInProgress indicates the backup is currently being analyzed
	BackupDiscoveryStatusInProgress BackupDiscoveryStatus = "InProgress"

	// BackupDiscoveryStatusCompleted indicates the backup analysis is complete and VM was found
	BackupDiscoveryStatusCompleted BackupDiscoveryStatus = "Completed"

	// BackupDiscoveryStatusSkipped indicates the backup was skipped (e.g., doesn't contain the VM)
	BackupDiscoveryStatusSkipped BackupDiscoveryStatus = "Skipped"

	// BackupDiscoveryStatusFailed indicates the backup analysis failed
	BackupDiscoveryStatusFailed BackupDiscoveryStatus = "Failed"
)

// BackupDiscoveryState provides a more granular state tracking for backup-level file restore and extraction outcomes.
// +k8s:deepcopy-gen=true
type BackupDiscoveryState string

const (
	BackupDiscoveryStateAvailable         BackupDiscoveryState = "available"
	BackupDiscoveryStateBackupDeleted     BackupDiscoveryState = "backup-deleted"
	BackupDiscoveryStateBackupMissing     BackupDiscoveryState = "backup-missing"
	BackupDiscoveryStateUnsupportedPlugin BackupDiscoveryState = "unsupported-plugin"
	BackupDiscoveryStateExtractionFailed  BackupDiscoveryState = "extraction-failed"
)

// BackupDiscoveryProgress contains detailed information about backup discovery progress
// +k8s:deepcopy-gen=true
type BackupDiscoveryProgress struct {
	VeleroBackupInfo `json:",inline"`
	// Current status of backup discovery for this backup.
	Status BackupDiscoveryStatus `json:"status"`
	// Human-readable message about the discovery status.
	// +kubebuilder:validation:MaxLength=1024
	Message string `json:"message,omitempty"`
	// When this backup's discovery status was last updated.
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`
}

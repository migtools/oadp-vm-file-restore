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

// VirtualMachineFileRestoreCondition represents the state of a VirtualMachineFileRestore.
// +kubebuilder:validation:Enum=Ready
type VirtualMachineFileRestoreCondition string

const (
	// VirtualMachineFileRestoreConditionReady indicates that file serving resources
	// have been created and files are accessible to the user.
	VirtualMachineFileRestoreConditionReady VirtualMachineFileRestoreCondition = "Ready"
)

// VirtualMachineFileRestoreSpec defines the desired state of VirtualMachineFileRestore
type VirtualMachineFileRestoreSpec struct {
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// Reference to the VirtualMachineBackupsDiscovery resource in the same namespace
	// that contains the discovered backups to serve files from.
	// +kubebuilder:validation:MinLength=1
	// +required
	BackupsDiscoveryRef string `json:"backupsDiscoveryRef"`

	// Specific backup names to serve files from, selected from the discovery results.
	// If not specified, all valid backups from the discovery will be used for file serving.
	// All specified backup names must exist in the ValidBackups list of the referenced discovery.
	// +optional
	SelectedBackups []string `json:"selectedBackups,omitempty"`
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

	// Information about the file serving resources that have been created.
	// +optional
	FileServingInfo *FileServingInfo `json:"fileServingInfo,omitempty"`
}

// FileServingInfo contains information about file serving resources
// TODO: Define file serving information structure based on chosen implementation
// Structure will be determined when file serving mechanism is implemented
type FileServingInfo struct {
	// TODO: Add fields based on file serving implementation
	// Potential fields may include:
	// - Resource names (pods, services, jobs, etc.)
	// - Access endpoints and credentials
	// - Service ports and network configuration
	// - User instructions for file access
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=vmfr,scope=Namespaced
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Discovery",type=string,JSONPath=`.spec.backupsDiscoveryRef`
// +kubebuilder:printcolumn:name="Pod",type=string,JSONPath=`.status.fileServingInfo.podName`

// VirtualMachineFileRestore is the Schema for the virtualmachinefilerestores API
type VirtualMachineFileRestore struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of VirtualMachineFileRestore
	// +required
	Spec VirtualMachineFileRestoreSpec `json:"spec"`

	// status defines the observed state of VirtualMachineFileRestore
	// +optional
	Status VirtualMachineFileRestoreStatus `json:"status,omitempty"`
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

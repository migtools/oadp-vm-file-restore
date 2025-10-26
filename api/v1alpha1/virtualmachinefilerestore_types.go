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
	veleroapi "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VirtualMachineFileRestorePhase is a high-level summary of the lifecycle of a VirtualMachineFileRestore.
// These phases match Velero's phase model for consistency with OADP's Velero foundation.
// +kubebuilder:validation:Enum=New;InProgress;Completed;PartiallyFailed;Failed;Deleting
type VirtualMachineFileRestorePhase string

const (
	// VirtualMachineFileRestorePhaseNew indicates the request was accepted by the cluster,
	// but the controller has not yet started processing it.
	VirtualMachineFileRestorePhaseNew VirtualMachineFileRestorePhase = "New"

	// VirtualMachineFileRestorePhaseInProgress indicates the controller is actively
	// working on creating file serving resources (validating, creating restores, etc.).
	VirtualMachineFileRestorePhaseInProgress VirtualMachineFileRestorePhase = "InProgress"

	// VirtualMachineFileRestorePhaseCompleted indicates the restore finished successfully
	// and all file serving resources are ready. All Velero Restores succeeded.
	VirtualMachineFileRestorePhaseCompleted VirtualMachineFileRestorePhase = "Completed"

	// VirtualMachineFileRestorePhasePartiallyFailed indicates the restore finished
	// but some Velero Restores failed. File serving is available for successful restores,
	// making the resource usable despite partial failures.
	VirtualMachineFileRestorePhasePartiallyFailed VirtualMachineFileRestorePhase = "PartiallyFailed"

	// VirtualMachineFileRestorePhaseFailed indicates the file restore failed completely
	// due to unrecoverable errors (e.g., validation failure, all restores failed).
	// The controller will not retry automatically.
	VirtualMachineFileRestorePhaseFailed VirtualMachineFileRestorePhase = "Failed"

	// VirtualMachineFileRestorePhaseDeleting indicates that the VirtualMachineFileRestore
	// resource is pending deletion and cleanup of associated resources is in progress.
	VirtualMachineFileRestorePhaseDeleting VirtualMachineFileRestorePhase = "Deleting"
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

	// RestoreNamespace specifies an existing namespace where file serving resources will be created.
	// If not specified, a temporary namespace will be created automatically.
	// The namespace must exist and be accessible to the controller.
	// +optional
	RestoreNamespace string `json:"restoreNamespace,omitempty"`

	// NamespacePrefix specifies a prefix for automatically generated temporary namespaces.
	// Only used when RestoreNamespace is not specified.
	// If not specified, the generated namespace name will use the VM's namespace-name format.
	// The final namespace name will be: <prefix>-<vm-namespace>-<vm-name>-<suffix>
	// +optional
	NamespacePrefix string `json:"namespacePrefix,omitempty"`

	// FileAccess defines which file access methods are enabled for this restore.
	// If not specified, defaults to HTTP file browser only.
	// +optional
	FileAccess *FileAccessSpec `json:"fileAccess,omitempty"`
}

// FileAccessSpec defines the file access methods available for the restored files
// +kubebuilder:validation:XValidation:rule="has(self.ssh) || has(self.fileBrowser)",message="At least one of ssh or fileBrowser must be specified"
type FileAccessSpec struct {
	// SSH provides read-only access to restored files via chrooted OpenSSH.
	// Supports SFTP, SCP, and rsync for file transfer only (no interactive shell access).
	// The SSH server runs in a chroot environment for security isolation.
	// +optional
	SSH *SSHAccessSpec `json:"ssh,omitempty"`

	// FileBrowser enables HTTPS web-based file browser access
	// If present (non-nil), FileBrowser access is enabled
	// +optional
	FileBrowser *FileBrowserAccessSpec `json:"fileBrowser,omitempty"`
}

// SSHAccessSpec configures read-only SSH file access to restored files.
// Only SFTP, SCP, and rsync protocols are supported in a chrooted environment.
// Interactive shell access is disabled for security.
type SSHAccessSpec struct {
	// Username for SSH access
	// Defaults to "oadp" if not specified
	// +optional
	Username string `json:"username,omitempty"`

	// PublicKey for SSH key-based authentication
	// Public keys are not sensitive and can be specified inline
	// If both PublicKey and CredentialsSecretRef are empty, controller generates keypair
	// +optional
	PublicKey string `json:"publicKey,omitempty"`

	// CredentialsSecretRef references a Secret containing SSH authentication credentials.
	// The Secret must have an "authorized_keys" key for SSH key-based authentication.
	// The "username" key is optional and defaults to "oadp" if not provided.
	// Note: Only SSH key-based authentication is supported; password authentication is not available.
	// Takes precedence over inline Username and PublicKey fields.
	// +optional
	CredentialsSecretRef *SecretReference `json:"credentialsSecretRef,omitempty"`
}

// FileBrowserAccessSpec configures HTTPS file browser access
type FileBrowserAccessSpec struct {
	// CredentialsSecretRef references a Secret containing FileBrowser credentials.
	// The Secret must have a "password" key and optionally a "username" key.
	// If "username" is not provided in the Secret, it defaults to "oadp".
	// If CredentialsSecretRef is not specified, the controller generates both
	// username (defaults to "oadp") and password, storing them in a Secret
	// in the temporary restore namespace.
	// +optional
	CredentialsSecretRef *SecretReference `json:"credentialsSecretRef,omitempty"`

	// ExposeExternally enables creation of an OpenShift Route for the FileBrowser service.
	// When enabled, creates a Route with reencrypt TLS termination for external HTTPS access.
	// Only effective on OpenShift clusters.
	// +optional
	ExposeExternally bool `json:"exposeExternally,omitempty"`
}

// SecretReference identifies a Secret by name and optional namespace
type SecretReference struct {
	// Name of the Secret
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`

	// Namespace where the Secret is located.
	// Defaults to the OADP namespace when not specified.
	// Note: Secrets outside TemporaryRestoreNamespace are automatically
	// copied to that namespace for mounting in the serving pod.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// VirtualMachineFileRestoreStatus defines the observed state of VirtualMachineFileRestore.
type VirtualMachineFileRestoreStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// Phase indicates the overall phase of the file restore operation.
	// Derived from conditions for human readability. Matches Velero's phase model.
	// Automation should rely on conditions, not phase.
	// +optional
	Phase VirtualMachineFileRestorePhase `json:"phase,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// IMPORTANT: Controllers must set this at the START of reconciliation, not at the end.
	// This prevents race conditions where clients see updated conditions but stale observedGeneration.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the current state of the VirtualMachineFileRestore resource.
	// This is the PRIMARY source of truth for resource state.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types for this resource (defined in types package):
	// - types.ConditionTypeProgressing: Restore is actively running
	// - types.ConditionTypeAvailable: File serving resources are ready and accessible
	// - types.ConditionTypeDegraded: Partial failures occurred (may still be usable)
	// - types.ConditionTypeReady: Summary condition (resource is usable)
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Information about the file serving resources that have been created.
	// +optional
	FileServingInfo *FileServingInfo `json:"fileServingInfo,omitempty"`

	// PVCRestores contains PVC-grouped restore information showing which backups each PVC was restored from.
	// This provides a user-friendly view of the restoration data organized by PVC.
	// +optional
	PVCRestores []PVCRestoreInfo `json:"pvcRestores,omitempty"`

	// CreatedNamespace contains information about the namespace used for file serving.
	// This will be set to the specified RestoreNamespace or the name of the auto-generated temporary namespace.
	// +optional
	CreatedNamespace string `json:"createdNamespace,omitempty"`
}

// PVCRestoreInfo combines PVC metadata with restores.
// PVC metadata is inlined for simplicity in JSON output.
type PVCRestoreInfo struct {
	types.PVCInfo `json:",inline"`

	// Restores contains all backup restores for this PVC
	// +optional
	Restores []RestoreInfo `json:"restores,omitempty"`
}

// RestoreInfo contains information about a specific restore of a PVC from a backup.
type RestoreInfo struct {
	// Name of the backup this restore came from
	VeleroBackupName string `json:"veleroBackupName"`

	// Namespace of the backup this restore came from
	VeleroBackupNamespace string `json:"veleroBackupNamespace"`

	// Timestamp indicates when the backup was created
	// +optional
	Timestamp *metav1.Time `json:"timestamp,omitempty"`

	// State indicates the compatibility and processing state of this backup
	// Values: "available", "backup-deleted", "backup-missing", "unsupported-plugin", "extraction-failed", "processing", "failed"
	// +optional
	State string `json:"state,omitempty"`

	// Name of the Velero Restore object
	// +optional
	VeleroRestoreName string `json:"veleroRestoreName,omitempty"`

	// Namespace of the Velero Restore object
	// +optional
	VeleroRestoreNamespace string `json:"veleroRestoreNamespace,omitempty"`

	// Phase of the Velero Restore object
	// +optional
	Phase veleroapi.RestorePhase `json:"phase,omitempty"`

	// When the Velero Restore object was created
	// +optional
	CreatedAt *metav1.Time `json:"createdAt,omitempty"`

	// When the Velero Restore completed
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// Reason for failure if the restore failed
	// +optional
	FailureReason string `json:"failureReason,omitempty"`
}

// FileServingInfo summarizes how restored files can be accessed
type FileServingInfo struct {
	// SSH contains SSH/SFTP/SCP/rsync access information, if enabled.
	// +optional
	SSH *SSHServingInfo `json:"ssh,omitempty"`

	// FileBrowser contains HTTPS file browser access information, if enabled.
	// +optional
	FileBrowser *FileBrowserServingInfo `json:"fileBrowser,omitempty"`
}

// SSHServingInfo provides details for accessing restored files over SSH/SFTP/SCP/rsync
type SSHServingInfo struct {
	// ClusterAccess provides the internal SSH endpoint, accessible within the cluster
	// or from environments connected to the cluster network (e.g. via VPN, oc port-forward).
	// SSH is only exposed within the cluster for security reasons.
	// Use 'oc port-forward' or 'kubectl port-forward' for external access.
	// Example: "ssh://vmfr-ssh.restore-tmp.svc.cluster.local:22"
	// +optional
	ClusterAccess string `json:"clusterAccess,omitempty"`

	// CredentialsSecretRef references a Secret containing SSH connection details:
	// - "username"
	// - "authorized_keys"
	// - optionally "privateKey"
	// The Secret is created or referenced by the controller.
	// +optional
	CredentialsSecretRef *SecretReference `json:"credentialsSecretRef,omitempty"`
}

// FileBrowserServingInfo provides details for accessing restored files via HTTPS web interface
type FileBrowserServingInfo struct {
	// ClusterAccess provides the internal HTTPS URL, usable from within the cluster network.
	// Example: "https://vmfr-browser.restore-tmp.svc.cluster.local"
	// +optional
	ClusterAccess string `json:"clusterAccess,omitempty"`

	// PublicAccess provides the external HTTPS URL, if exposed via Route or Ingress.
	// Example: "https://restore-files.apps.example.com"
	// +optional
	PublicAccess string `json:"publicAccess,omitempty"`

	// CredentialsSecretRef references a Secret containing login credentials for the file browser:
	// - "username"
	// - "password"
	// If not specified, the controller creates and manages this Secret automatically.
	// +optional
	CredentialsSecretRef *SecretReference `json:"credentialsSecretRef,omitempty"`
}

// +kubebuilder:storageversion
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

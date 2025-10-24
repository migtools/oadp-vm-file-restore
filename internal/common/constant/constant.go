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

// Package constant contains all common constants used in the VM File Restore project
package constant

// Common labels for objects manipulated by the VM File Restore Controller
const (
	OadpLabel              = "oadp.openshift.io/oadp-vm-file-restore"
	OadpLabelValue         = TrueString
	ManagedByLabel         = "app.kubernetes.io/managed-by"
	ManagedByLabelValue    = "oadp-vm-file-restore-controller"
	VMFROriginUUIDLabel    = "oadp.openshift.io/vmfr-origin-uuid"
	VMFRTempNamespaceLabel = "oadp.openshift.io/vmfr-temp-namespace"
	VMFRManagedCopyLabel   = "oadp.openshift.io/vmfr-managed-copy"
	CredentialTypeLabel    = "oadp.openshift.io/credential-type"

	// Resource UID labeling constants for selective restore implemented
	// by the https://github.com/kubevirt/kubevirt-velero-plugin/pull/396
	PVCUIDLabel = "velero.kubevirt.io/pvc-uid"
)

// Credential type values for CredentialTypeLabel
const (
	CredentialTypeSSH         = "ssh"
	CredentialTypeFileBrowser = "filebrowser"
)

// Common annotations for tracking ownership and origin
const (
	VMFROriginNameAnnotation          = "oadp.openshift.io/vmfr-origin-name"
	VMFROriginNamespaceAnnotation     = "oadp.openshift.io/vmfr-origin-namespace"
	BackupNameAnnotation              = "oadp.openshift.io/backup-name"
	VirtualMachineNameAnnotation      = "oadp.openshift.io/vm-name"
	VirtualMachineNamespaceAnnotation = "oadp.openshift.io/vm-namespace"

	// VMFROriginalPVCNameAnnotation tracks the original PVC name before restore
	// This is set by kubevirt-velero-plugin when restoring PVCs with generated names
	VMFROriginalPVCNameAnnotation = "oadp.openshift.io/vmfr-original-name"
)

// Common finalizers for VirtualMachineFileRestore resources
const (
	// VMFileRestoreFinalizer is the main finalizer used for VirtualMachineFileRestore resources
	VMFileRestoreFinalizer = "oadp.openshift.io/vm-file-restore-finalizer"

	// VeleroRestoreCleanupFinalizer ensures Velero Restore objects are cleaned up before other resources
	VeleroRestoreCleanupFinalizer = "oadp.openshift.io/velero-restore-cleanup-finalizer"
)

// Common environment variables for the VM File Restore Controller
const (
	WatchNamespaceEnvVar = "WATCH_NAMESPACE"
	// LogLevelEnvVar: Numeric log level corresponding to logrus levels (matching velero).
	// 0 = panic, 1 = fatal, 2 = error, 3 = warn, 4 = info, 5 = debug, 6 = trace
	LogLevelEnvVar = "LOG_LEVEL"
	// LogFormatEnvVar: Log format, either "json" for structured logging or "text" for human-readable
	LogFormatEnvVar = "LOG_FORMAT"
)

// Common string constants
const (
	EmptyString     = ""
	TrueString      = "True"
	FalseString     = "False"
	NameDelimiter   = "-"
	NamespaceString = "Namespace"
	NameString      = "name"
	JSONTagString   = "json"
	CommaString     = ","
)

// Log level constants (corresponding to logrus levels)
const (
	LogLevelPanic = "0"
	LogLevelFatal = "1"
	LogLevelError = "2"
	LogLevelWarn  = "3"
	LogLevelInfo  = "4"
	LogLevelDebug = "5"
	LogLevelTrace = "6"
)

// Log format constants
const (
	LogFormatJSON = "json"
	LogFormatText = "text"
)

// Discovery and restore constants
const (
	DefaultDiscoveryTimeoutMinutes = 30
	DefaultRetryIntervalSeconds    = 30
	DiscoveryBatchRequeueSeconds   = 2
	MaxBackupDiscoveryParallel     = 10
	BackupDiscoveryBatchSize       = 3
	MaxRetryAttempts               = 5

	// BackupLevelFailurePVCName is a synthetic PVC name used in status to track
	// backup-level failures (e.g., backup deleted, backup files missing) where
	// no PVC metadata is available
	BackupLevelFailurePVCName = "backup-level-failure"
)

// Backup discovery phases and reasons
const (
	DiscoveryPhaseInitializing = "Initializing"
	DiscoveryPhaseDiscovering  = "Discovering"
	DiscoveryPhaseCompleted    = "Completed"
	DiscoveryPhaseFailed       = "Failed"

	ReasonBackupsFound          = "BackupsFound"
	ReasonNoValidBackupsFound   = "NoValidBackupsFound"
	ReasonBackupSelectionFailed = "BackupSelectionFailed"
	ReasonDeletionInProgress    = "DeletionInProgress"
	ReasonDiscoveryTimeout      = "DiscoveryTimeout"
)

// File serving constants
const (
	DefaultFileServerPort    = 8080
	DefaultFileServerTimeout = "10m"
	FileServerPodPrefix      = "vmfr-fileserver"
	FileServerServicePrefix  = "vmfr-service"

	// Container images for file serving
	VMFileServerImage       = "quay.io/migi/oadp-vm-file-server:latest"       // Main VM file server for mounting VM disk images
	SSHSidecarImage         = "quay.io/migtools/oadp-vmfr-sshd:latest"        // SSH sidecar for SSH/SFTP/SCP/rsync access
	FileBrowserSidecarImage = "quay.io/migtools/oadp-vmfr-filebrowser:latest" // FileBrowser sidecar for HTTPS web-based file browser

	// Default ports for file access methods
	// Using unprivileged ports (>1024) since sidecars run as non-root
	DefaultSSHPort         = 2222 // SSH on unprivileged port (standard 22 requires root)
	DefaultFileBrowserPort = 8443 // HTTPS on unprivileged port (standard 443 requires root)

	// Default usernames for file access methods
	DefaultSSHUsername         = "oadp"
	DefaultFileBrowserUsername = "oadp"

	// Minimum password length for FileBrowser credentials
	DefaultMinimumPasswordLength = 12
)

// Magic numbers
const (
	Base10                  = 10
	Bits32                  = 32
	DefaultRequeueMinutes   = 10
	ErrorRequeueMinutes     = 5
	DiscoveryRequeueMinutes = 2
)

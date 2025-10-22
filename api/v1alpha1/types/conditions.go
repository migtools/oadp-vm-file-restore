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

// Standard Kubernetes condition types used across OADP VM File Restore CRDs.
// These follow Kubernetes API conventions and are the primary source of truth for resource state.
// Both VirtualMachineBackupsDiscovery and VirtualMachineFileRestore use these identical condition types.
const (
	// ConditionTypeProgressing indicates whether an operation is actively running.
	// Status=True: Operation is in progress (Phase: InProgress)
	// Status=False: Operation is not running (Phase: New, Completed, PartiallyFailed, Failed, Deleting)
	//
	// Common reasons for VirtualMachineBackupsDiscovery:
	//   - "DiscoveryStarted", "ScanningBackups", "DiscoveryCompleted", "DiscoveryFailed"
	//
	// Common reasons for VirtualMachineFileRestore:
	//   - "Validating", "CreatingRestores", "WaitingForRestores", "SettingUpFileServing",
	//     "RestoreCompleted", "ValidationFailed"
	ConditionTypeProgressing = "Progressing"

	// ConditionTypeAvailable indicates whether a resource has usable data/functionality.
	// Status=True: Resource is usable (Phase: Completed or PartiallyFailed)
	// Status=False: Resource is not usable (Phase: New, InProgress, Failed, Deleting)
	//
	// For VirtualMachineBackupsDiscovery:
	//   - Available=True means "at least one valid backup was found"
	//   - Available=False means "no valid backups found yet" or "discovery failed"
	//
	// For VirtualMachineFileRestore:
	//   - Available=True means "files are accessible via file serving resources"
	//   - Available=False means "files not yet available" or "all restores failed"
	//
	// Common reasons:
	//   - "ValidBackupsFound", "FileServingReady" (when True)
	//   - "InProgress", "WaitingForRestores", "Failed", "NoValidBackupsFound" (when False)
	//
	// KEY DISTINCTION:
	//   - PartiallyFailed phase → Available=True (some data is usable)
	//   - Failed phase → Available=False (nothing usable)
	ConditionTypeAvailable = "Available"

	// ConditionTypeDegraded indicates partial failures or suboptimal conditions occurred.
	// Status=True: Some operations failed (Phase: PartiallyFailed or Failed)
	// Status=False: All operations succeeded (Phase: Completed)
	//
	// IMPORTANT: Degraded=True does NOT always mean unusable!
	//
	// When Available=True AND Degraded=True (Phase: PartiallyFailed):
	//   - Resource is FUNCTIONAL and USABLE
	//   - Some operations failed, but others succeeded
	//   - Users should investigate but can still use the resource
	//
	// Example scenarios:
	//   - VirtualMachineBackupsDiscovery: Found 3 valid backups, but 2 failed to scan
	//   - VirtualMachineFileRestore: 2 of 3 Velero Restores succeeded
	//
	// Common reasons:
	//   - "PartialFailure", "SomeBackupsInvalid", "SomeRestoresFailed" (when True)
	//   - "AllOperationsSucceeded", "NoFailures", "AllBackupsValid", "AllRestoresSucceeded" (when False)
	ConditionTypeDegraded = "Degraded"

	// ConditionTypeReady is a summary condition indicating overall usability.
	// Status=True: Resource is usable for its purpose (Phase: Completed or PartiallyFailed)
	// Status=False: Resource is not usable yet or failed (Phase: New, InProgress, Failed, Deleting)
	//
	// Calculation: Ready = Available (True if resource has usable data)
	//
	// This means:
	//   - Ready=True for Phase Completed (perfect, no issues)
	//   - Ready=True for Phase PartiallyFailed (degraded but usable)
	//   - Ready=False for Phase Failed (not usable)
	//
	// Common reasons:
	//   - "DiscoveryComplete", "RestoreComplete" (when True)
	//   - "InProgress", "Failed", "Deleting" (when False)
	//
	// Use Case: Simple readiness check for automation
	ConditionTypeReady = "Ready"
)

// Common condition reasons used across both VirtualMachineBackupsDiscovery and VirtualMachineFileRestore
const (
	// Shared Progressing reasons
	ReasonInProgress = "InProgress"
	ReasonFailed     = "Failed"

	// Shared Available reasons
	ReasonNotStarted = "NotStarted"

	// Shared Degraded reasons
	ReasonNoFailures      = "NoFailures"
	ReasonCriticalFailure = "CriticalFailure"
	ReasonPartialFailure  = "PartialFailure"

	// Shared Ready reasons
	ReasonNotReady        = "NotReady"
	ReasonCompleted       = "Completed"
	ReasonPartiallyFailed = "PartiallyFailed"
)

// VirtualMachineFileRestore specific condition reasons
const (
	// Progressing condition reasons
	ReasonInitialized                   = "Initialized"
	ReasonValidating                    = "Validating"
	ReasonValidationCompleted           = "ValidationCompleted"
	ReasonValidationFailed              = "ValidationFailed"
	ReasonPartialValidationFailed       = "PartialValidationFailed"
	ReasonDiscoveringPVCs               = "DiscoveringPVCs"
	ReasonWaitingForDiscovery           = "WaitingForDiscovery"
	ReasonNamespaceReady                = "NamespaceReady"
	ReasonWaitingForRestores            = "WaitingForRestores"
	ReasonRestoresCompleted             = "RestoresCompleted"
	ReasonRestoresCompletedWithFailures = "RestoresCompletedWithFailures"
	ReasonRestoresFailed                = "RestoresFailed"
	ReasonFileServerCreated             = "FileServerCreated"

	// Available condition reasons
	ReasonDiscoveryInProgress   = "DiscoveryInProgress"
	ReasonPreparingRestores     = "PreparingRestores"
	ReasonRestoresInProgress    = "RestoresInProgress"
	ReasonFileServerPending     = "FileServerPending"
	ReasonFileServerAvailable   = "FileServerAvailable"
	ReasonPartialRestoreSuccess = "PartialRestoreSuccess"
	ReasonAllRestoresFailed     = "AllRestoresFailed"

	// Degraded condition reasons
	ReasonSomeRestoresFailed = "SomeRestoresFailed"
)

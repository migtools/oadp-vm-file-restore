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

// Package velerohelpers provides utilities for reading and discovering Velero backup contents
// using legitimate Velero APIs and mechanisms for VM backup validation.
package velerohelpers

import (
	veleroapi "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
)

// ValidateVMInBackupSpec checks if VM namespace is included in backup spec
// This is the canonical implementation used by both legacy and current code paths
func ValidateVMInBackupSpec(backup *veleroapi.Backup, vm *kubevirtv1.VirtualMachine) bool {
	// Check exclusions first - they always take precedence
	for _, excludedNS := range backup.Spec.ExcludedNamespaces {
		if excludedNS == vm.Namespace {
			return false // VM namespace is explicitly excluded
		}
	}

	// Check inclusions
	if len(backup.Spec.IncludedNamespaces) == 0 {
		// No included namespaces specified = all namespaces included (except excluded)
		return true
	}

	// Check if VM namespace is in included list
	for _, includedNS := range backup.Spec.IncludedNamespaces {
		if includedNS == "*" || includedNS == vm.Namespace {
			return true
		}
	}

	// VM namespace not in included list
	return false
}

const (
	virtualMachineKind          = "VirtualMachine"
	virtualMachineKindLowercase = "virtualmachine"
)

// hasVMInBackupResources is a generic function to check VM presence in any backup resource collection
func hasVMInBackupResources(vmName, vmNamespace string, iterateResources func(yield func(kind, namespace, name string) bool)) bool {
	found := false
	iterateResources(func(kind, namespace, name string) bool {
		if (kind == virtualMachineKind || kind == virtualMachineKindLowercase) &&
			namespace == vmNamespace &&
			name == vmName {
			found = true
			return false // stop iteration
		}
		return true // continue iteration
	})
	return found
}

// String parsing utilities for Velero resource formats

// parseNamespacedName parses "namespace/name" format, returns ("", name) for cluster-scoped resources
func parseNamespacedName(resource string) (namespace, name string) {
	for i, char := range resource {
		if char == '/' {
			return resource[:i], resource[i+1:]
		}
	}
	// No namespace separator found, this is a cluster-scoped resource
	return "", resource
}

// extractKindFromGVK extracts the Kind from a Velero GVK string (e.g., "v1/VirtualMachine.kubevirt.io" -> "VirtualMachine")
func extractKindFromGVK(gvk string) string {
	// GVK format is typically "version/Kind.group" or just "Kind"
	parts := []rune(gvk)
	start := 0
	end := len(parts)

	// Find the last '/' to skip version
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == '/' {
			start = i + 1
			break
		}
	}

	// Find the first '.' to skip group
	for i := start; i < len(parts); i++ {
		if parts[i] == '.' {
			end = i
			break
		}
	}

	return string(parts[start:end])
}

// BackupAsyncOperationsIncomplete checks if a backup has incomplete async operations.
// This is used to validate that backups with data mover operations (e.g., snapshotMoveData: true)
// have completed all their async operations before being considered valid for restore.
//
// Returns true if the backup has async operations that haven't completed (backup is NOT ready for restore).
// Returns false if:
//   - No async operations were attempted (BackupItemOperationsAttempted == 0)
//   - All async operations completed successfully (Completed == Attempted)
//
// This prevents attempting restores from backups where the data mover upload completed
// after the backup was marked as Completed, leaving BackupItemOperationsCompleted unset.
func BackupAsyncOperationsIncomplete(backup *veleroapi.Backup) bool {
	if backup == nil || backup.Status.BackupItemOperationsAttempted == 0 {
		// No async operations attempted, backup is ready
		return false
	}

	// If attempted > 0, completed must equal attempted for the backup to be ready
	return backup.Status.BackupItemOperationsCompleted != backup.Status.BackupItemOperationsAttempted
}

// BackupAsyncOperationsReason returns a human-readable reason for why a backup's async operations are incomplete.
// Returns empty string if async operations are complete or not applicable.
func BackupAsyncOperationsReason(backup *veleroapi.Backup) string {
	if backup == nil || backup.Status.BackupItemOperationsAttempted == 0 {
		return ""
	}

	if backup.Status.BackupItemOperationsCompleted != backup.Status.BackupItemOperationsAttempted {
		return "backup has incomplete async operations (data mover upload may not have finished); " +
			"BackupItemOperationsAttempted=" + itoa(backup.Status.BackupItemOperationsAttempted) +
			", BackupItemOperationsCompleted=" + itoa(backup.Status.BackupItemOperationsCompleted) +
			", BackupItemOperationsFailed=" + itoa(backup.Status.BackupItemOperationsFailed)
	}

	return ""
}

// itoa converts an integer to a string without importing strconv
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	negative := i < 0
	if negative {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

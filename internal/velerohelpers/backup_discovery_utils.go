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

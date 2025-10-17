// Package function provides common utility functions for the OADP VM file restore controller,
// including metadata validation and logging helpers.
package function

import (
	"context"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
	"github.com/migtools/oadp-vm-file-restore/internal/common/constant"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// CheckVeleroRestoreMetadata return true if Velero Restore object has required VMFR origin labels
func CheckVeleroRestoreMetadata(obj client.Object) bool {
	objLabels := obj.GetLabels()
	_, exists := objLabels[constant.VMFROriginUUIDLabel]
	return exists
}

// GetLogger return a logger from input ctx, with additional key/value pairs being
// input key and input obj name and namespace
func GetLogger(ctx context.Context, obj client.Object, key string) logr.Logger {
	return log.FromContext(ctx).WithValues(key, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()})
}

// FormatSizeHumanReadable converts a resource.Quantity to human-readable storage format.
// It ensures consistent formatting using binary units (Ki, Mi, Gi, Ti) which are standard for storage
func FormatSizeHumanReadable(quantity resource.Quantity) string {
	// Convert to bytes to ensure we start with a consistent base
	bytes := quantity.Value()

	// Create a new quantity from the byte value and format it with binary units
	newQuantity := resource.NewQuantity(bytes, resource.BinarySI)

	// The String() method will automatically choose the appropriate unit
	// e.g., 5368709120 bytes -> 5Gi, 32212254720 bytes -> 30Gi
	return newQuantity.String()
}

// GenerateTemporaryVMFRNamespaceName generates a unique temporary namespace name.
// Format: [prefix-]<vm-namespace>-<vm-name>-<suffix>
// - prefix: optional string to prepend (can be empty)
// - vmNamespace, vmName: names of the VM
// - uid: UID string for uniqueness
func GenerateTemporaryVMFRNamespaceName(prefix, vmNamespace, vmName, uid string, logger logr.Logger) string {
	var nameParts []string

	// Optional prefix
	if prefix != "" {
		nameParts = append(nameParts, prefix)
	}

	// Add VM namespace and name
	nameParts = append(nameParts, vmNamespace, vmName)

	// Ensure suffix is always 8 chars (pad or truncate)
	var suffix string
	if len(uid) >= 8 {
		suffix = uid[:8]
	} else {
		// Pad short UID with zeros for stability
		suffix = uid + strings.Repeat("0", 8-len(uid))
	}
	nameParts = append(nameParts, suffix)

	// Join with hyphens
	namespaceName := strings.Join(nameParts, "-")

	// Ensure max 63 chars (DNS-1123)
	if len(namespaceName) > 63 {
		maxPrefixLen := 63 - len(suffix) - 1 // -1 for hyphen
		if maxPrefixLen > 0 && maxPrefixLen < len(namespaceName) {
			namespaceName = namespaceName[:maxPrefixLen] + "-" + suffix
		} else {
			namespaceName = suffix
		}
	}

	// Convert to lowercase
	namespaceName = strings.ToLower(namespaceName)

	// Replace invalid DNS-1123 chars (anything not a-z, 0-9, or '-')
	re := regexp.MustCompile(`[^a-z0-9-]`)
	namespaceName = re.ReplaceAllString(namespaceName, "-")

	// Trim leading/trailing hyphens (illegal in DNS-1123)
	namespaceName = strings.Trim(namespaceName, "-")

	logger.V(1).Info("Generated temporary namespace name",
		"namespace", namespaceName,
		"vmNamespace", vmNamespace,
		"vmName", vmName,
		"prefix", prefix)

	return namespaceName
}

// GenerateVeleroRestorePrefix generates a prefix for Velero Restore generateName field.
// Kubernetes will automatically append a random suffix (5 chars) to ensure uniqueness.
// Format: vmfr-<vmfr-name>-<backup-name>-
// - vmfrName: Name of the VirtualMachineFileRestore resource
// - backupName: Name of the Velero backup
func GenerateVeleroRestorePrefix(vmfrName, backupName string, logger logr.Logger) string {
	// Build restore prefix: vmfr-<name>-<backup>-
	// K8s will append random 5-char suffix like "x7k2j"
	restorePrefix := "vmfr-" + vmfrName + "-" + backupName + "-"

	// Convert to lowercase (DNS-1123 requirement)
	restorePrefix = strings.ToLower(restorePrefix)

	logger.V(1).Info("Generated Velero Restore prefix for generateName",
		"restorePrefix", restorePrefix,
		"vmfrName", vmfrName,
		"backupName", backupName)

	return restorePrefix
}

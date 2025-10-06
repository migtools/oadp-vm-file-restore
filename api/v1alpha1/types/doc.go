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

// Package types contains shared types used across OADP VM File Restore CRDs.
//
// This package provides reusable API types for VirtualMachineBackupsDiscovery
// and VirtualMachineFileRestore resources, ensuring consistency in backup
// discovery and file restoration workflows.
//
// # Shared Types
//
// Condition Constants (conditions.go):
//   - ConditionTypeProgressing, ConditionTypeAvailable, ConditionTypeDegraded, ConditionTypeReady
//   - Standard Kubernetes conditions used by both CRDs
//   - Follow Kubernetes API conventions for status representation
//
// Backup Discovery (backup_discovery.go):
//   - VeleroBackupInfo: Metadata about discovered Velero backups
//   - InvalidBackupInfo: Information about backups that don't contain the VM
//   - BackupDiscoveryStatus: Status enum for backup discovery progress
//   - BackupDiscoveryProgress: Detailed per-backup discovery tracking
//
// PVC Information (pvc.go):
//   - PVCInfo: PVC metadata from backups (UID, name, namespace, size)
//   - Used to track persistent volume claims across backup discovery and restoration
//
// Time Handling (time.go):
//   - FlexibleTime: Custom time type supporting both RFC3339 and date-only formats
//   - Enables user-friendly time range filtering in backup discovery
//
// # Versioning
//
// All types in this package are versioned as part of the v1alpha1 API.
// When creating new API versions (v1beta1, v1), these types may be:
//   - Reused across versions if the schema remains unchanged
//   - Copied and modified in the new version's types package
//   - Converted between versions using conversion functions
//
// # Design Principles
//
// Types in this package follow these design principles:
//   - DRY (Don't Repeat Yourself): Shared types are defined once and reused
//   - Velero Alignment: Status enums and patterns match Velero's phase model
//   - Kubernetes Conventions: Follows standard K8s API patterns and condition types
//   - API Compatibility: All types are designed for JSON serialization in CRD schemas
//
// For more information about the overall status lifecycle and phase/condition
// relationships, see docs/design/crd_status_lifecycle.md in the project root.
package types

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

package velerohelpers

import (
	"context"

	veleroapi "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
)

// BackupContentsInterface defines the interface for reading backup contents
// This interface allows for easy mocking in tests and provides a clean abstraction
// for backup storage operations
type BackupContentsInterface interface {
	// BackupContainsVM validates whether a backup contains the specified virtual machine
	BackupContainsVM(ctx context.Context, backup *veleroapi.Backup, vmName, vmNamespace string) (bool, error)

	// FetchBackupMetadata downloads and parses backup metadata from object storage
	FetchBackupMetadata(ctx context.Context, backup *veleroapi.Backup) (*BackupMetadata, error)

	// ExtractVMFromBackupMetadata extracts VM definition from backup metadata
	ExtractVMFromBackupMetadata(ctx context.Context, backup *veleroapi.Backup, vmName, vmNamespace string) (*kubevirtv1.VirtualMachine, error)

	// FetchPVCFromBackup fetches a specific PVC from backup contents
	FetchPVCFromBackup(ctx context.Context, backup *veleroapi.Backup, pvcName, pvcNamespace string) (*corev1.PersistentVolumeClaim, error)
}

// Ensure VeleroBackupContentsReader implements the interface
var _ BackupContentsInterface = (*VeleroBackupContentsReader)(nil)

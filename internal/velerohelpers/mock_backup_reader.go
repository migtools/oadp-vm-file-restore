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
	"fmt"

	veleroapi "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubevirtv1 "kubevirt.io/api/core/v1"
)

// MockBackupContentsReader implements BackupContentsInterface for testing
type MockBackupContentsReader struct {
	// BackupMetadata maps backup names to their metadata
	BackupMetadata map[string]*BackupMetadata

	// VMs maps backup+vm combination to VM definitions
	VMs map[string]*kubevirtv1.VirtualMachine

	// PVCs maps backup+pvc combination to PVC definitions
	PVCs map[string]*corev1.PersistentVolumeClaim

	// Errors allows simulating specific error conditions
	Errors map[string]error

	// ShouldFailAll makes all operations fail (for testing general error handling)
	ShouldFailAll bool
}

// NewMockBackupContentsReader creates a new mock backup contents reader
func NewMockBackupContentsReader() *MockBackupContentsReader {
	return &MockBackupContentsReader{
		BackupMetadata: make(map[string]*BackupMetadata),
		VMs:            make(map[string]*kubevirtv1.VirtualMachine),
		PVCs:           make(map[string]*corev1.PersistentVolumeClaim),
		Errors:         make(map[string]error),
	}
}

// BackupContainsVM validates whether a backup contains the specified virtual machine
func (m *MockBackupContentsReader) BackupContainsVM(ctx context.Context, backup *veleroapi.Backup, vmName, vmNamespace string) (bool, error) {
	if m.ShouldFailAll {
		return false, fmt.Errorf("mock configured to fail all operations")
	}

	key := fmt.Sprintf("%s-contains-vm-%s-%s", backup.Name, vmNamespace, vmName)
	if err, exists := m.Errors[key]; exists {
		return false, err
	}

	// Check if VM exists in mock data
	vmKey := fmt.Sprintf("%s-%s-%s", backup.Name, vmNamespace, vmName)
	_, exists := m.VMs[vmKey]
	return exists, nil
}

// FetchBackupMetadata downloads and parses backup metadata from object storage
func (m *MockBackupContentsReader) FetchBackupMetadata(ctx context.Context, backup *veleroapi.Backup) (*BackupMetadata, error) {
	if m.ShouldFailAll {
		return nil, fmt.Errorf("mock configured to fail all operations")
	}

	key := fmt.Sprintf("%s-metadata", backup.Name)
	if err, exists := m.Errors[key]; exists {
		return nil, err
	}

	metadata, exists := m.BackupMetadata[backup.Name]
	if !exists {
		return nil, fmt.Errorf("backup metadata not found for %s", backup.Name)
	}

	return metadata, nil
}

// ExtractVMFromBackupMetadata extracts VM definition from backup metadata
func (m *MockBackupContentsReader) ExtractVMFromBackupMetadata(ctx context.Context, backup *veleroapi.Backup, vmName, vmNamespace string) (*kubevirtv1.VirtualMachine, error) {
	if m.ShouldFailAll {
		return nil, fmt.Errorf("mock configured to fail all operations")
	}

	key := fmt.Sprintf("%s-vm-%s-%s", backup.Name, vmNamespace, vmName)
	if err, exists := m.Errors[key]; exists {
		return nil, err
	}

	vmKey := fmt.Sprintf("%s-%s-%s", backup.Name, vmNamespace, vmName)
	vm, exists := m.VMs[vmKey]
	if !exists {
		return nil, fmt.Errorf("VM %s/%s not found in backup %s", vmNamespace, vmName, backup.Name)
	}

	return vm.DeepCopy(), nil
}

// FetchPVCFromBackup fetches a specific PVC from backup contents
func (m *MockBackupContentsReader) FetchPVCFromBackup(ctx context.Context, backup *veleroapi.Backup, pvcName, pvcNamespace string) (*corev1.PersistentVolumeClaim, error) {
	if m.ShouldFailAll {
		return nil, fmt.Errorf("mock configured to fail all operations")
	}

	key := fmt.Sprintf("%s-pvc-%s-%s", backup.Name, pvcNamespace, pvcName)
	if err, exists := m.Errors[key]; exists {
		return nil, err
	}

	pvcKey := fmt.Sprintf("%s-%s-%s", backup.Name, pvcNamespace, pvcName)
	pvc, exists := m.PVCs[pvcKey]
	if !exists {
		return nil, fmt.Errorf("PVC %s/%s not found in backup %s", pvcNamespace, pvcName, backup.Name)
	}

	return pvc.DeepCopy(), nil
}

// SetupTestBackup configures mock data for a test backup scenario
func (m *MockBackupContentsReader) SetupTestBackup(backupName string, vmName, vmNamespace string, pvcNames []string) {
	// Create mock VM
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vmName,
			Namespace: vmNamespace,
		},
		Spec: kubevirtv1.VirtualMachineSpec{
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				Spec: kubevirtv1.VirtualMachineInstanceSpec{
					Volumes: make([]kubevirtv1.Volume, 0, len(pvcNames)),
				},
			},
		},
	}

	// Add volumes referencing the PVCs
	for i, pvcName := range pvcNames {
		volume := kubevirtv1.Volume{
			Name: fmt.Sprintf("volume-%d", i),
			VolumeSource: kubevirtv1.VolumeSource{
				PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
					PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvcName,
					},
				},
			},
		}
		vm.Spec.Template.Spec.Volumes = append(vm.Spec.Template.Spec.Volumes, volume)
	}

	// Store VM
	vmKey := fmt.Sprintf("%s-%s-%s", backupName, vmNamespace, vmName)
	m.VMs[vmKey] = vm

	// Create mock PVCs
	metadataItems := make([]BackupResourceItem, 0)
	metadataItems = append(metadataItems, BackupResourceItem{
		APIVersion: "kubevirt.io/v1",
		Kind:       "VirtualMachine",
		Namespace:  vmNamespace,
		Name:       vmName,
	})

	for _, pvcName := range pvcNames {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: vmNamespace,
				UID:       types.UID(fmt.Sprintf("mock-uid-%s", pvcName)),
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("5Gi"),
					},
				},
			},
		}

		pvcKey := fmt.Sprintf("%s-%s-%s", backupName, vmNamespace, pvcName)
		m.PVCs[pvcKey] = pvc

		// Add to metadata
		metadataItems = append(metadataItems, BackupResourceItem{
			APIVersion: "v1",
			Kind:       "PersistentVolumeClaim",
			Namespace:  vmNamespace,
			Name:       pvcName,
		})
	}

	// Store metadata
	m.BackupMetadata[backupName] = &BackupMetadata{
		Items: metadataItems,
	}
}

// SetError configures the mock to return a specific error for a given operation
func (m *MockBackupContentsReader) SetError(operation, backupName, namespace, name string, err error) {
	var key string
	switch operation {
	case "contains-vm":
		key = fmt.Sprintf("%s-contains-vm-%s-%s", backupName, namespace, name)
	case "metadata":
		key = fmt.Sprintf("%s-metadata", backupName)
	case "vm":
		key = fmt.Sprintf("%s-vm-%s-%s", backupName, namespace, name)
	case "pvc":
		key = fmt.Sprintf("%s-pvc-%s-%s", backupName, namespace, name)
	}
	m.Errors[key] = err
}

// ClearErrors removes all configured errors
func (m *MockBackupContentsReader) ClearErrors() {
	m.Errors = make(map[string]error)
}

// Ensure MockBackupContentsReader implements the interface
var _ BackupContentsInterface = (*MockBackupContentsReader)(nil)

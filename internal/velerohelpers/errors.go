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
	"fmt"
)

// BackupDiscoveryError represents errors that occur during backup discovery operations
type BackupDiscoveryError struct {
	BackupName string
	Operation  string
	Cause      error
}

func (e *BackupDiscoveryError) Error() string {
	return fmt.Sprintf("backup discovery failed for %s during %s: %v",
		e.BackupName, e.Operation, e.Cause)
}

func (e *BackupDiscoveryError) Unwrap() error {
	return e.Cause
}

// NewBackupDiscoveryError creates a new BackupDiscoveryError
func NewBackupDiscoveryError(backupName, operation string, cause error) *BackupDiscoveryError {
	return &BackupDiscoveryError{
		BackupName: backupName,
		Operation:  operation,
		Cause:      cause,
	}
}

// PVCDiscoveryError represents errors that occur during PVC discovery operations
type PVCDiscoveryError struct {
	BackupName   string
	PVCName      string
	PVCNamespace string
	Operation    string
	Cause        error
}

func (e *PVCDiscoveryError) Error() string {
	return fmt.Sprintf("PVC discovery failed for %s/%s in backup %s during %s: %v",
		e.PVCNamespace, e.PVCName, e.BackupName, e.Operation, e.Cause)
}

func (e *PVCDiscoveryError) Unwrap() error {
	return e.Cause
}

// NewPVCDiscoveryError creates a new PVCDiscoveryError
func NewPVCDiscoveryError(backupName, pvcName, pvcNamespace, operation string, cause error) *PVCDiscoveryError {
	return &PVCDiscoveryError{
		BackupName:   backupName,
		PVCName:      pvcName,
		PVCNamespace: pvcNamespace,
		Operation:    operation,
		Cause:        cause,
	}
}

// VMDiscoveryError represents errors that occur during VM discovery operations
type VMDiscoveryError struct {
	BackupName  string
	VMName      string
	VMNamespace string
	Operation   string
	Cause       error
}

func (e *VMDiscoveryError) Error() string {
	return fmt.Sprintf("VM discovery failed for %s/%s in backup %s during %s: %v",
		e.VMNamespace, e.VMName, e.BackupName, e.Operation, e.Cause)
}

func (e *VMDiscoveryError) Unwrap() error {
	return e.Cause
}

// NewVMDiscoveryError creates a new VMDiscoveryError
func NewVMDiscoveryError(backupName, vmName, vmNamespace, operation string, cause error) *VMDiscoveryError {
	return &VMDiscoveryError{
		BackupName:  backupName,
		VMName:      vmName,
		VMNamespace: vmNamespace,
		Operation:   operation,
		Cause:       cause,
	}
}

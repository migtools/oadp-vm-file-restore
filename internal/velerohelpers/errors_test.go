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
	"errors"
	"testing"
)

func TestBackupDiscoveryError(t *testing.T) {
	tests := []struct {
		name           string
		backupName     string
		operation      string
		cause          error
		expectedError  string
		expectedUnwrap error
	}{
		{
			name:           "basic error",
			backupName:     "test-backup",
			operation:      "fetch",
			cause:          errors.New("connection failed"),
			expectedError:  "backup discovery failed for test-backup during fetch: connection failed",
			expectedUnwrap: errors.New("connection failed"),
		},
		{
			name:           "empty backup name",
			backupName:     "",
			operation:      "validate",
			cause:          errors.New("validation error"),
			expectedError:  "backup discovery failed for  during validate: validation error",
			expectedUnwrap: errors.New("validation error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewBackupDiscoveryError(tt.backupName, tt.operation, tt.cause)

			// Test Error() method
			if err.Error() != tt.expectedError {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.expectedError)
			}

			// Test Unwrap() method
			unwrapped := err.Unwrap()
			if unwrapped == nil || unwrapped.Error() != tt.expectedUnwrap.Error() {
				t.Errorf("Unwrap() = %v, want %v", unwrapped, tt.expectedUnwrap)
			}

			// Verify struct fields
			if err.BackupName != tt.backupName {
				t.Errorf("BackupName = %q, want %q", err.BackupName, tt.backupName)
			}
			if err.Operation != tt.operation {
				t.Errorf("Operation = %q, want %q", err.Operation, tt.operation)
			}
			if err.Cause.Error() != tt.cause.Error() {
				t.Errorf("Cause = %v, want %v", err.Cause, tt.cause)
			}
		})
	}
}

func TestPVCDiscoveryError(t *testing.T) {
	tests := []struct {
		name           string
		backupName     string
		pvcName        string
		pvcNamespace   string
		operation      string
		cause          error
		expectedError  string
		expectedUnwrap error
	}{
		{
			name:           "basic error",
			backupName:     "test-backup",
			pvcName:        "test-pvc",
			pvcNamespace:   "default",
			operation:      "extract",
			cause:          errors.New("tar extraction failed"),
			expectedError:  "PVC discovery failed for default/test-pvc in backup test-backup during extract: tar extraction failed",
			expectedUnwrap: errors.New("tar extraction failed"),
		},
		{
			name:           "missing namespace",
			backupName:     "backup-2",
			pvcName:        "pvc-2",
			pvcNamespace:   "",
			operation:      "read",
			cause:          errors.New("read error"),
			expectedError:  "PVC discovery failed for /pvc-2 in backup backup-2 during read: read error",
			expectedUnwrap: errors.New("read error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewPVCDiscoveryError(tt.backupName, tt.pvcName, tt.pvcNamespace, tt.operation, tt.cause)

			// Test Error() method
			if err.Error() != tt.expectedError {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.expectedError)
			}

			// Test Unwrap() method
			unwrapped := err.Unwrap()
			if unwrapped == nil || unwrapped.Error() != tt.expectedUnwrap.Error() {
				t.Errorf("Unwrap() = %v, want %v", unwrapped, tt.expectedUnwrap)
			}

			// Verify struct fields
			if err.BackupName != tt.backupName {
				t.Errorf("BackupName = %q, want %q", err.BackupName, tt.backupName)
			}
			if err.PVCName != tt.pvcName {
				t.Errorf("PVCName = %q, want %q", err.PVCName, tt.pvcName)
			}
			if err.PVCNamespace != tt.pvcNamespace {
				t.Errorf("PVCNamespace = %q, want %q", err.PVCNamespace, tt.pvcNamespace)
			}
			if err.Operation != tt.operation {
				t.Errorf("Operation = %q, want %q", err.Operation, tt.operation)
			}
		})
	}
}

func TestVMDiscoveryError(t *testing.T) {
	tests := []struct {
		name           string
		backupName     string
		vmName         string
		vmNamespace    string
		operation      string
		cause          error
		expectedError  string
		expectedUnwrap error
	}{
		{
			name:           "basic error",
			backupName:     "vm-backup",
			vmName:         "test-vm",
			vmNamespace:    "vms",
			operation:      "download",
			cause:          errors.New("network timeout"),
			expectedError:  "VM discovery failed for vms/test-vm in backup vm-backup during download: network timeout",
			expectedUnwrap: errors.New("network timeout"),
		},
		{
			name:           "parsing error",
			backupName:     "backup-3",
			vmName:         "vm-3",
			vmNamespace:    "prod",
			operation:      "parse",
			cause:          errors.New("invalid YAML"),
			expectedError:  "VM discovery failed for prod/vm-3 in backup backup-3 during parse: invalid YAML",
			expectedUnwrap: errors.New("invalid YAML"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewVMDiscoveryError(tt.backupName, tt.vmName, tt.vmNamespace, tt.operation, tt.cause)

			// Test Error() method
			if err.Error() != tt.expectedError {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.expectedError)
			}

			// Test Unwrap() method
			unwrapped := err.Unwrap()
			if unwrapped == nil || unwrapped.Error() != tt.expectedUnwrap.Error() {
				t.Errorf("Unwrap() = %v, want %v", unwrapped, tt.expectedUnwrap)
			}

			// Verify struct fields
			if err.BackupName != tt.backupName {
				t.Errorf("BackupName = %q, want %q", err.BackupName, tt.backupName)
			}
			if err.VMName != tt.vmName {
				t.Errorf("VMName = %q, want %q", err.VMName, tt.vmName)
			}
			if err.VMNamespace != tt.vmNamespace {
				t.Errorf("VMNamespace = %q, want %q", err.VMNamespace, tt.vmNamespace)
			}
			if err.Operation != tt.operation {
				t.Errorf("Operation = %q, want %q", err.Operation, tt.operation)
			}
		})
	}
}

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

package controller

import (
	"encoding/json"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	oadpv1alpha1 "github.com/migtools/oadp-vm-file-restore/api/v1alpha1"
	oadptypes "github.com/migtools/oadp-vm-file-restore/api/v1alpha1/types"
	"github.com/migtools/oadp-vm-file-restore/internal/common/constant"
)

func getTestPVCMounts() []PVCMountInfo {
	return []PVCMountInfo{
		{
			PVCName:         "test-pvc-1",
			PVCNamespace:    "test-ns",
			PVCUID:          "uid-1",
			BackupName:      "backup-1",
			BackupTimestamp: &metav1.Time{},
		},
	}
}

func TestBuildFileServerPodSpec_DefaultMainContainer(t *testing.T) {
	pvcMounts := getTestPVCMounts()

	t.Run("builds pod with default main container", func(t *testing.T) {
		config := FileServerPodConfig{
			PodName:              "test-pod",
			PodNamespace:         "test-ns",
			VMFRName:             "test-vmfr",
			VMFRNamespace:        "vmfr-ns",
			VMFRUID:              "test-uid",
			PVCMounts:            pvcMounts,
			EnableDualPathAccess: true,
			UseInternalMounts:    false,
		}

		pod, err := buildFileServerPodSpec(config)
		if err != nil {
			t.Fatalf("buildFileServerPodSpec() error = %v", err)
		}

		// Verify pod metadata
		if pod.Name != "test-pod" {
			t.Errorf("Expected pod name 'test-pod', got '%s'", pod.Name)
		}
		if pod.Namespace != "test-ns" {
			t.Errorf("Expected namespace 'test-ns', got '%s'", pod.Namespace)
		}

		// Verify labels
		if pod.Labels[constant.VMFROriginUUIDLabel] != "test-uid" {
			t.Errorf("Expected UUID label '%s', got '%s'", "test-uid", pod.Labels[constant.VMFROriginUUIDLabel])
		}

		// Verify annotations
		if pod.Annotations[constant.VMFROriginNameAnnotation] != "test-vmfr" {
			t.Errorf("Expected name annotation 'test-vmfr', got '%s'", pod.Annotations[constant.VMFROriginNameAnnotation])
		}
		if pod.Annotations[constant.VMFROriginNamespaceAnnotation] != "vmfr-ns" {
			t.Errorf("Expected namespace annotation 'vmfr-ns', got '%s'", pod.Annotations[constant.VMFROriginNamespaceAnnotation])
		}

		// Verify main container exists
		if len(pod.Spec.Containers) == 0 {
			t.Fatal("Pod has no containers")
		}
		mainContainer := pod.Spec.Containers[0]
		if mainContainer.Name != "file-server" {
			t.Errorf("Expected main container name 'file-server', got '%s'", mainContainer.Name)
		}

		// Verify PVC volumes are created
		if len(pod.Spec.Volumes) == 0 {
			t.Fatal("Pod has no volumes")
		}

		// Note: Dual-path init containers have been removed in the new architecture
		// The new implementation uses internal mounts with mount propagation instead
	})
}

func TestBuildFileServerPodSpec_SSHSidecar(t *testing.T) {
	pvcMounts := getTestPVCMounts()

	t.Run("builds pod with SSH sidecar", func(t *testing.T) {
		sshConfig := &SSHAccessConfig{
			Username:                   "testuser",
			CredentialsSecretName:      "test-ssh-secret",
			CredentialsSecretNamespace: "test-ns",
			Port:                       constant.DefaultSSHPort,
		}

		config := FileServerPodConfig{
			PodName:              "test-pod",
			PodNamespace:         "test-ns",
			VMFRName:             "test-vmfr",
			VMFRNamespace:        "vmfr-ns",
			VMFRUID:              "test-uid",
			PVCMounts:            pvcMounts,
			SSHAccess:            sshConfig,
			EnableDualPathAccess: false,
			UseInternalMounts:    false,
		}

		pod, err := buildFileServerPodSpec(config)
		if err != nil {
			t.Fatalf("buildFileServerPodSpec() error = %v", err)
		}

		// Should have main container + SSH sidecar
		if len(pod.Spec.Containers) != 2 {
			t.Errorf("Expected 2 containers (main + SSH), got %d", len(pod.Spec.Containers))
		}

		// Find SSH container
		var sshContainer *corev1.Container
		for i := range pod.Spec.Containers {
			if pod.Spec.Containers[i].Name == "sshd" {
				sshContainer = &pod.Spec.Containers[i]
				break
			}
		}

		if sshContainer == nil {
			t.Fatal("SSH container not found")
		}

		// Verify SSH container image
		if sshContainer.Image != constant.SSHSidecarImage {
			t.Errorf("Unexpected SSH image: %s (expected %s)", sshContainer.Image, constant.SSHSidecarImage)
		}

		// Note: SSH init containers have been removed in the new architecture
		// The new implementation uses inline initialization scripts instead
	})
}

func TestBuildFileServerPodSpec_FileBrowserSidecar(t *testing.T) {
	pvcMounts := getTestPVCMounts()

	t.Run("builds pod with FileBrowser sidecar", func(t *testing.T) {
		fbConfig := &FileBrowserAccessConfig{
			CredentialsSecretName:      "fb-secret",
			CredentialsSecretNamespace: "test-ns",
			Port:                       constant.DefaultFileBrowserPort,
		}

		config := FileServerPodConfig{
			PodName:              "test-pod",
			PodNamespace:         "test-ns",
			VMFRName:             "test-vmfr",
			VMFRNamespace:        "vmfr-ns",
			VMFRUID:              "test-uid",
			PVCMounts:            pvcMounts,
			FileBrowserAccess:    fbConfig,
			EnableDualPathAccess: false,
			UseInternalMounts:    false,
		}

		pod, err := buildFileServerPodSpec(config)
		if err != nil {
			t.Fatalf("buildFileServerPodSpec() error = %v", err)
		}

		// Should have main container + FileBrowser sidecar
		if len(pod.Spec.Containers) != 2 {
			t.Errorf("Expected 2 containers (main + FileBrowser), got %d", len(pod.Spec.Containers))
		}

		// Find FileBrowser container
		var fbContainer *corev1.Container
		for i := range pod.Spec.Containers {
			if pod.Spec.Containers[i].Name == "filebrowser" {
				fbContainer = &pod.Spec.Containers[i]
				break
			}
		}

		if fbContainer == nil {
			t.Fatal("FileBrowser container not found")
		}

		// Verify FileBrowser container image
		if fbContainer.Image != constant.FileBrowserSidecarImage {
			t.Errorf("Unexpected FileBrowser image: %s (expected %s)", fbContainer.Image, constant.FileBrowserSidecarImage)
		}

		// Note: FileBrowser init containers have been removed in the new architecture
		// The new implementation uses inline initialization scripts instead
	})
}

func TestBuildFileServerPodSpec_BothSidecars(t *testing.T) {
	pvcMounts := getTestPVCMounts()

	t.Run("builds pod with both SSH and FileBrowser sidecars", func(t *testing.T) {
		sshConfig := &SSHAccessConfig{
			Username:                   "testuser",
			CredentialsSecretName:      "test-ssh-secret",
			CredentialsSecretNamespace: "test-ns",
			Port:                       constant.DefaultSSHPort,
		}

		fbConfig := &FileBrowserAccessConfig{
			CredentialsSecretName:      "fb-secret",
			CredentialsSecretNamespace: "test-ns",
			Port:                       constant.DefaultFileBrowserPort,
		}

		config := FileServerPodConfig{
			PodName:              "test-pod",
			PodNamespace:         "test-ns",
			VMFRName:             "test-vmfr",
			VMFRNamespace:        "vmfr-ns",
			VMFRUID:              "test-uid",
			PVCMounts:            pvcMounts,
			SSHAccess:            sshConfig,
			FileBrowserAccess:    fbConfig,
			EnableDualPathAccess: true,
			UseInternalMounts:    false,
		}

		pod, err := buildFileServerPodSpec(config)
		if err != nil {
			t.Fatalf("buildFileServerPodSpec() error = %v", err)
		}

		// Should have main container + SSH + FileBrowser
		if len(pod.Spec.Containers) != 3 {
			t.Errorf("Expected 3 containers (main + SSH + FileBrowser), got %d", len(pod.Spec.Containers))
		}

		// Verify all three containers exist
		containerNames := make(map[string]bool)
		for _, container := range pod.Spec.Containers {
			containerNames[container.Name] = true
		}

		expectedContainers := []string{"file-server", "sshd", "filebrowser"}
		for _, name := range expectedContainers {
			if !containerNames[name] {
				t.Errorf("Missing container: %s", name)
			}
		}
	})
}

func TestBuildFileServerPodSpec_Validation(t *testing.T) {
	pvcMounts := getTestPVCMounts()

	t.Run("validates required config fields", func(t *testing.T) {
		testCases := []struct {
			name        string
			config      FileServerPodConfig
			expectedErr string
		}{
			{
				name: "missing pod name",
				config: FileServerPodConfig{
					PodNamespace: "test-ns",
					VMFRName:     "test-vmfr",
					PVCMounts:    pvcMounts,
				},
				expectedErr: "pod name is required",
			},
			{
				name: "missing pod namespace",
				config: FileServerPodConfig{
					PodName:   "test-pod",
					VMFRName:  "test-vmfr",
					PVCMounts: pvcMounts,
				},
				expectedErr: "pod namespace is required",
			},
			{
				name: "missing VMFR name",
				config: FileServerPodConfig{
					PodName:      "test-pod",
					PodNamespace: "test-ns",
					PVCMounts:    pvcMounts,
				},
				expectedErr: "VMFR name is required for labels",
			},
			{
				name: "missing PVC mounts",
				config: FileServerPodConfig{
					PodName:      "test-pod",
					PodNamespace: "test-ns",
					VMFRName:     "test-vmfr",
				},
				expectedErr: "at least one PVC mount is required",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := buildFileServerPodSpec(tc.config)
				if err == nil {
					t.Errorf("Expected error '%s', got nil", tc.expectedErr)
				} else if err.Error() != tc.expectedErr {
					t.Errorf("Expected error '%s', got '%s'", tc.expectedErr, err.Error())
				}
			})
		}
	})
}

func TestBuildFileServerService(t *testing.T) {
	t.Run("builds basic service", func(t *testing.T) {
		config := ServiceConfig{
			ServiceName:      "test-service",
			ServiceNamespace: "test-ns",
			VMFRName:         "test-vmfr",
			VMFRNamespace:    "vmfr-ns",
			VMFRUID:          "test-uid",
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP},
			},
		}

		service, err := buildFileServerService(config)
		if err != nil {
			t.Fatalf("buildFileServerService() error = %v", err)
		}

		if service.Name != "test-service" {
			t.Errorf("Expected service name 'test-service', got '%s'", service.Name)
		}
		if service.Namespace != "test-ns" {
			t.Errorf("Expected namespace 'test-ns', got '%s'", service.Namespace)
		}

		// Verify selector
		if service.Spec.Selector[constant.VMFROriginUUIDLabel] != "test-uid" {
			t.Errorf("Expected UUID selector '%s', got '%s'", "test-uid", service.Spec.Selector[constant.VMFROriginUUIDLabel])
		}

		// Verify annotations
		if service.Annotations[constant.VMFROriginNameAnnotation] != "test-vmfr" {
			t.Errorf("Expected name annotation 'test-vmfr', got '%s'", service.Annotations[constant.VMFROriginNameAnnotation])
		}
		if service.Annotations[constant.VMFROriginNamespaceAnnotation] != "vmfr-ns" {
			t.Errorf("Expected namespace annotation 'vmfr-ns', got '%s'", service.Annotations[constant.VMFROriginNamespaceAnnotation])
		}

		// Verify ports
		if len(service.Spec.Ports) != 1 {
			t.Errorf("Expected 1 port, got %d", len(service.Spec.Ports))
		}
	})

	t.Run("builds service with multiple ports", func(t *testing.T) {
		config := ServiceConfig{
			ServiceName:      "test-service",
			ServiceNamespace: "test-ns",
			VMFRName:         "test-vmfr",
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 8080},
				{Name: "ssh", Port: 22},
				{Name: "https", Port: 443},
			},
		}

		service, err := buildFileServerService(config)
		if err != nil {
			t.Fatalf("buildFileServerService() error = %v", err)
		}

		if len(service.Spec.Ports) != 3 {
			t.Errorf("Expected 3 ports, got %d", len(service.Spec.Ports))
		}

		// Verify all ports are present
		portNames := make(map[string]bool)
		for _, port := range service.Spec.Ports {
			portNames[port.Name] = true
		}

		expectedPorts := []string{"http", "ssh", "https"}
		for _, name := range expectedPorts {
			if !portNames[name] {
				t.Errorf("Missing port: %s", name)
			}
		}
	})

	t.Run("validates required config fields", func(t *testing.T) {
		testCases := []struct {
			name        string
			config      ServiceConfig
			expectedErr string
		}{
			{
				name: "missing service name",
				config: ServiceConfig{
					ServiceNamespace: "test-ns",
					VMFRName:         "test-vmfr",
					Ports:            []corev1.ServicePort{{Name: "http", Port: 8080}},
				},
				expectedErr: "service name is required",
			},
			{
				name: "missing service namespace",
				config: ServiceConfig{
					ServiceName: "test-service",
					VMFRName:    "test-vmfr",
					Ports:       []corev1.ServicePort{{Name: "http", Port: 8080}},
				},
				expectedErr: "service namespace is required",
			},
			{
				name: "missing VMFR name",
				config: ServiceConfig{
					ServiceName:      "test-service",
					ServiceNamespace: "test-ns",
					Ports:            []corev1.ServicePort{{Name: "http", Port: 8080}},
				},
				expectedErr: "VMFR name is required for labels",
			},
			{
				name: "missing ports",
				config: ServiceConfig{
					ServiceName:      "test-service",
					ServiceNamespace: "test-ns",
					VMFRName:         "test-vmfr",
				},
				expectedErr: "at least one port is required",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := buildFileServerService(tc.config)
				if err == nil {
					t.Errorf("Expected error '%s', got nil", tc.expectedErr)
				} else if err.Error() != tc.expectedErr {
					t.Errorf("Expected error '%s', got '%s'", tc.expectedErr, err.Error())
				}
			})
		}
	})
}

func TestGatherServicePorts(t *testing.T) {
	t.Run("default HTTP only", func(t *testing.T) {
		ports := gatherServicePorts(nil, nil)

		if len(ports) != 1 {
			t.Errorf("Expected 1 port (HTTP), got %d", len(ports))
		}
		if ports[0].Name != "http" {
			t.Errorf("Expected HTTP port, got '%s'", ports[0].Name)
		}
	})

	t.Run("HTTP + SSH", func(t *testing.T) {
		sshConfig := &SSHAccessConfig{Port: constant.DefaultSSHPort}
		ports := gatherServicePorts(sshConfig, nil)

		if len(ports) != 2 {
			t.Errorf("Expected 2 ports (HTTP + SSH), got %d", len(ports))
		}

		portNames := make(map[string]bool)
		for _, port := range ports {
			portNames[port.Name] = true
		}

		if !portNames["http"] {
			t.Error("Missing HTTP port")
		}
		if !portNames["ssh"] {
			t.Error("Missing SSH port")
		}
	})

	t.Run("HTTP + FileBrowser", func(t *testing.T) {
		fbConfig := &FileBrowserAccessConfig{Port: constant.DefaultFileBrowserPort}
		ports := gatherServicePorts(nil, fbConfig)

		if len(ports) != 2 {
			t.Errorf("Expected 2 ports (HTTP + HTTPS), got %d", len(ports))
		}

		portNames := make(map[string]bool)
		for _, port := range ports {
			portNames[port.Name] = true
		}

		if !portNames["http"] {
			t.Error("Missing HTTP port")
		}
		if !portNames["https"] {
			t.Error("Missing HTTPS port")
		}
	})

	t.Run("HTTP + SSH + FileBrowser", func(t *testing.T) {
		sshConfig := &SSHAccessConfig{Port: constant.DefaultSSHPort}
		fbConfig := &FileBrowserAccessConfig{Port: constant.DefaultFileBrowserPort}
		ports := gatherServicePorts(sshConfig, fbConfig)

		if len(ports) != 3 {
			t.Errorf("Expected 3 ports (HTTP + SSH + HTTPS), got %d", len(ports))
		}

		portNames := make(map[string]bool)
		for _, port := range ports {
			portNames[port.Name] = true
		}

		expectedPorts := []string{"http", "ssh", "https"}
		for _, name := range expectedPorts {
			if !portNames[name] {
				t.Errorf("Missing port: %s", name)
			}
		}
	})
}

func TestExtractPVCMountsFromVMFR(t *testing.T) {
	time1 := metav1.NewTime(time.Date(2025, 1, 27, 17, 21, 24, 0, time.UTC))
	time2 := metav1.NewTime(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC))

	t.Run("single PVC with single backup version", func(t *testing.T) {
		vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
			Status: oadpv1alpha1.VirtualMachineFileRestoreStatus{
				PVCRestores: []oadpv1alpha1.PVCRestoreInfo{
					{
						PVCInfo: oadptypes.PVCInfo{
							PVCName:      "test-vm-disk-1",
							PVCNamespace: "vm-test-ns",
							PVCUID:       "pvc-uid-1",
						},
						Restores: []oadpv1alpha1.RestoreInfo{
							{
								VeleroBackupName: "test-vm-backup-2025-01-27",
								Phase:            "Completed",
								State:            "available",
								Timestamp:        &time1,
							},
						},
					},
				},
			},
		}

		result := extractPVCMountsFromVMFR(vmfr)

		if len(result) != 1 {
			t.Fatalf("Expected 1 PVC mount, got %d", len(result))
		}

		mount := result[0]
		if mount.PVCName != "test-vm-disk-1" {
			t.Errorf("Expected PVC name 'test-vm-disk-1', got '%s'", mount.PVCName)
		}
		if mount.BackupName != "test-vm-backup-2025-01-27" {
			t.Errorf("Expected backup name 'test-vm-backup-2025-01-27', got '%s'", mount.BackupName)
		}
	})

	t.Run("single PVC with multiple backup versions - should mount all", func(t *testing.T) {
		vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
			Status: oadpv1alpha1.VirtualMachineFileRestoreStatus{
				PVCRestores: []oadpv1alpha1.PVCRestoreInfo{
					{
						PVCInfo: oadptypes.PVCInfo{
							PVCName:      "test-vm-disk-1",
							PVCNamespace: "vm-test-ns",
							PVCUID:       "pvc-uid-1",
						},
						Restores: []oadpv1alpha1.RestoreInfo{
							{
								VeleroBackupName:       "test-vm-backup-2025-01-27",
								VeleroRestoreName:      "restore-1",
								VeleroRestoreNamespace: "openshift-adp",
								Phase:                  "Completed",
								State:                  "available",
								Timestamp:              &time1,
							},
							{
								VeleroBackupName:       "test-vm-backup-2025-01-15",
								VeleroRestoreName:      "restore-2",
								VeleroRestoreNamespace: "openshift-adp",
								Phase:                  "Completed",
								State:                  "available",
								Timestamp:              &time2,
							},
						},
					},
				},
			},
		}

		result := extractPVCMountsFromVMFR(vmfr)

		// CRITICAL: Should return 2 mounts (one for each backup version), not just 1
		if len(result) != 2 {
			t.Fatalf("Expected 2 PVC mounts (one per backup version), got %d", len(result))
		}

		// Verify both backup versions are included
		backupNames := make(map[string]bool)
		for _, mount := range result {
			if mount.PVCName != "test-vm-disk-1" {
				t.Errorf("Expected PVC name 'test-vm-disk-1', got '%s'", mount.PVCName)
			}
			backupNames[mount.BackupName] = true
		}

		if !backupNames["test-vm-backup-2025-01-27"] {
			t.Error("Expected backup 'test-vm-backup-2025-01-27' in mounts")
		}
		if !backupNames["test-vm-backup-2025-01-15"] {
			t.Error("Expected backup 'test-vm-backup-2025-01-15' in mounts")
		}
	})

	t.Run("skips non-completed restores", func(t *testing.T) {
		vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
			Status: oadpv1alpha1.VirtualMachineFileRestoreStatus{
				PVCRestores: []oadpv1alpha1.PVCRestoreInfo{
					{
						PVCInfo: oadptypes.PVCInfo{
							PVCName:      "test-vm-disk-1",
							PVCNamespace: "vm-test-ns",
							PVCUID:       "pvc-uid-1",
						},
						Restores: []oadpv1alpha1.RestoreInfo{
							{
								VeleroBackupName: "test-vm-backup-completed",
								Phase:            "Completed",
								State:            "available",
								Timestamp:        &time1,
							},
							{
								VeleroBackupName: "test-vm-backup-failed",
								Phase:            "Failed",
								State:            "available",
								Timestamp:        &time2,
							},
							{
								VeleroBackupName: "test-vm-backup-in-progress",
								Phase:            "InProgress",
								State:            "available",
								Timestamp:        &time2,
							},
						},
					},
				},
			},
		}

		result := extractPVCMountsFromVMFR(vmfr)

		// Should only include the completed restore
		if len(result) != 1 {
			t.Fatalf("Expected 1 PVC mount (only completed), got %d", len(result))
		}

		if result[0].BackupName != "test-vm-backup-completed" {
			t.Errorf("Expected backup 'test-vm-backup-completed', got '%s'", result[0].BackupName)
		}
	})

	t.Run("includes Finalizing phase restores", func(t *testing.T) {
		vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
			Status: oadpv1alpha1.VirtualMachineFileRestoreStatus{
				PVCRestores: []oadpv1alpha1.PVCRestoreInfo{
					{
						PVCInfo: oadptypes.PVCInfo{
							PVCName:      "test-vm-disk-1",
							PVCNamespace: "vm-test-ns",
							PVCUID:       "pvc-uid-1",
						},
						Restores: []oadpv1alpha1.RestoreInfo{
							{
								VeleroBackupName: "test-vm-backup-finalizing",
								Phase:            "Finalizing",
								State:            "available",
								Timestamp:        &time1,
							},
						},
					},
				},
			},
		}

		result := extractPVCMountsFromVMFR(vmfr)

		// Should include Finalizing phase
		if len(result) != 1 {
			t.Fatalf("Expected 1 PVC mount (Finalizing phase), got %d", len(result))
		}

		if result[0].BackupName != "test-vm-backup-finalizing" {
			t.Errorf("Expected backup 'test-vm-backup-finalizing', got '%s'", result[0].BackupName)
		}
	})

	t.Run("skips synthetic backup-level failure entries", func(t *testing.T) {
		vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
			Status: oadpv1alpha1.VirtualMachineFileRestoreStatus{
				PVCRestores: []oadpv1alpha1.PVCRestoreInfo{
					{
						PVCInfo: oadptypes.PVCInfo{
							PVCName: constant.BackupLevelFailurePVCName, // Synthetic entry
						},
						Restores: []oadpv1alpha1.RestoreInfo{
							{
								VeleroBackupName: "failed-backup",
								Phase:            "Completed",
								State:            "available",
							},
						},
					},
					{
						PVCInfo: oadptypes.PVCInfo{
							PVCName:      "real-pvc",
							PVCNamespace: "vm-test-ns",
							PVCUID:       "pvc-uid-1",
						},
						Restores: []oadpv1alpha1.RestoreInfo{
							{
								VeleroBackupName: "real-backup",
								Phase:            "Completed",
								State:            "available",
								Timestamp:        &time1,
							},
						},
					},
				},
			},
		}

		result := extractPVCMountsFromVMFR(vmfr)

		// Should only include real PVC, not synthetic entry
		if len(result) != 1 {
			t.Fatalf("Expected 1 PVC mount (skipping synthetic), got %d", len(result))
		}

		if result[0].PVCName != "real-pvc" {
			t.Errorf("Expected PVC name 'real-pvc', got '%s'", result[0].PVCName)
		}
	})

	t.Run("multiple PVCs each with multiple backup versions", func(t *testing.T) {
		vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
			Status: oadpv1alpha1.VirtualMachineFileRestoreStatus{
				PVCRestores: []oadpv1alpha1.PVCRestoreInfo{
					{
						PVCInfo: oadptypes.PVCInfo{
							PVCName:      "disk-1",
							PVCNamespace: "vm-test-ns",
							PVCUID:       "uid-1",
						},
						Restores: []oadpv1alpha1.RestoreInfo{
							{
								VeleroBackupName: "backup-new",
								Phase:            "Completed",
								State:            "available",
								Timestamp:        &time1,
							},
							{
								VeleroBackupName: "backup-old",
								Phase:            "Completed",
								State:            "available",
								Timestamp:        &time2,
							},
						},
					},
					{
						PVCInfo: oadptypes.PVCInfo{
							PVCName:      "disk-2",
							PVCNamespace: "vm-test-ns",
							PVCUID:       "uid-2",
						},
						Restores: []oadpv1alpha1.RestoreInfo{
							{
								VeleroBackupName: "backup-new",
								Phase:            "Completed",
								State:            "available",
								Timestamp:        &time1,
							},
							{
								VeleroBackupName: "backup-old",
								Phase:            "Completed",
								State:            "available",
								Timestamp:        &time2,
							},
						},
					},
				},
			},
		}

		result := extractPVCMountsFromVMFR(vmfr)

		// Should return 4 mounts: 2 PVCs × 2 backup versions each
		if len(result) != 4 {
			t.Fatalf("Expected 4 PVC mounts (2 PVCs × 2 versions), got %d", len(result))
		}

		// Count mounts per PVC
		pvcCounts := make(map[string]int)
		for _, mount := range result {
			pvcCounts[mount.PVCName]++
		}

		if pvcCounts["disk-1"] != 2 {
			t.Errorf("Expected 2 mounts for disk-1, got %d", pvcCounts["disk-1"])
		}
		if pvcCounts["disk-2"] != 2 {
			t.Errorf("Expected 2 mounts for disk-2, got %d", pvcCounts["disk-2"])
		}
	})
}

func TestBuildBackupPVCMapJSON(t *testing.T) {
	t.Run("single PVC in single backup", func(t *testing.T) {
		timestamp := metav1.Now()
		pvcMounts := []PVCMountInfo{
			{
				PVCName:         "test-pvc-1",
				PVCNamespace:    "test-ns",
				PVCUID:          "uid-123",
				BackupName:      "backup-1",
				BackupTimestamp: &timestamp,
			},
		}

		result := buildBackupPVCMapJSON(pvcMounts)

		// Verify it's valid JSON
		var jsonMap map[string][]map[string]string
		if err := json.Unmarshal([]byte(result), &jsonMap); err != nil {
			t.Fatalf("Result is not valid JSON: %v", err)
		}

		// Verify structure
		if len(jsonMap) != 1 {
			t.Errorf("Expected 1 backup in map, got %d", len(jsonMap))
		}

		backupPVCs, exists := jsonMap["backup-1"]
		if !exists {
			t.Fatal("Expected backup-1 in map")
		}

		if len(backupPVCs) != 1 {
			t.Errorf("Expected 1 PVC in backup-1, got %d", len(backupPVCs))
		}

		pvc := backupPVCs[0]
		if pvc["name"] != "test-pvc-1" {
			t.Errorf("Expected PVC name 'test-pvc-1', got '%s'", pvc["name"])
		}
		if pvc["path"] != "/dev/pvc-uid-123" {
			t.Errorf("Expected path '/dev/pvc-uid-123', got '%s'", pvc["path"])
		}
		if pvc["timestamp"] == "" {
			t.Error("Expected non-empty timestamp")
		}
	})

	t.Run("multiple PVCs in same backup", func(t *testing.T) {
		timestamp := metav1.Now()
		pvcMounts := []PVCMountInfo{
			{
				PVCName:         "pvc-1",
				PVCUID:          "uid-1",
				BackupName:      "backup-1",
				BackupTimestamp: &timestamp,
			},
			{
				PVCName:         "pvc-2",
				PVCUID:          "uid-2",
				BackupName:      "backup-1",
				BackupTimestamp: &timestamp,
			},
		}

		result := buildBackupPVCMapJSON(pvcMounts)

		var jsonMap map[string][]map[string]string
		if err := json.Unmarshal([]byte(result), &jsonMap); err != nil {
			t.Fatalf("Result is not valid JSON: %v", err)
		}

		backupPVCs := jsonMap["backup-1"]
		if len(backupPVCs) != 2 {
			t.Errorf("Expected 2 PVCs in backup-1, got %d", len(backupPVCs))
		}

		// Verify both PVCs are present
		pvcNames := make(map[string]bool)
		for _, pvc := range backupPVCs {
			pvcNames[pvc["name"]] = true
		}

		if !pvcNames["pvc-1"] || !pvcNames["pvc-2"] {
			t.Error("Expected both pvc-1 and pvc-2 in result")
		}
	})

	t.Run("PVCs across multiple backups", func(t *testing.T) {
		timestamp1 := metav1.Now()
		timestamp2 := metav1.NewTime(timestamp1.Add(-24 * time.Hour)) // 1 day earlier

		pvcMounts := []PVCMountInfo{
			{
				PVCName:         "pvc-1",
				PVCUID:          "uid-1",
				BackupName:      "backup-1",
				BackupTimestamp: &timestamp1,
			},
			{
				PVCName:         "pvc-2",
				PVCUID:          "uid-2",
				BackupName:      "backup-2",
				BackupTimestamp: &timestamp2,
			},
			{
				PVCName:         "pvc-3",
				PVCUID:          "uid-3",
				BackupName:      "backup-1",
				BackupTimestamp: &timestamp1,
			},
		}

		result := buildBackupPVCMapJSON(pvcMounts)

		var jsonMap map[string][]map[string]string
		if err := json.Unmarshal([]byte(result), &jsonMap); err != nil {
			t.Fatalf("Result is not valid JSON: %v", err)
		}

		// Should have 2 backups
		if len(jsonMap) != 2 {
			t.Errorf("Expected 2 backups in map, got %d", len(jsonMap))
		}

		// backup-1 should have 2 PVCs
		if len(jsonMap["backup-1"]) != 2 {
			t.Errorf("Expected 2 PVCs in backup-1, got %d", len(jsonMap["backup-1"]))
		}

		// backup-2 should have 1 PVC
		if len(jsonMap["backup-2"]) != 1 {
			t.Errorf("Expected 1 PVC in backup-2, got %d", len(jsonMap["backup-2"]))
		}
	})

	t.Run("PVC with nil timestamp", func(t *testing.T) {
		pvcMounts := []PVCMountInfo{
			{
				PVCName:         "pvc-1",
				PVCUID:          "uid-1",
				BackupName:      "backup-1",
				BackupTimestamp: nil, // nil timestamp
			},
		}

		result := buildBackupPVCMapJSON(pvcMounts)

		var jsonMap map[string][]map[string]string
		if err := json.Unmarshal([]byte(result), &jsonMap); err != nil {
			t.Fatalf("Result is not valid JSON: %v", err)
		}

		pvc := jsonMap["backup-1"][0]
		if pvc["timestamp"] != "" {
			t.Errorf("Expected empty timestamp for nil BackupTimestamp, got '%s'", pvc["timestamp"])
		}
	})

	t.Run("empty PVC mounts list", func(t *testing.T) {
		pvcMounts := []PVCMountInfo{}

		result := buildBackupPVCMapJSON(pvcMounts)

		// Should return valid empty JSON object
		var jsonMap map[string][]map[string]string
		if err := json.Unmarshal([]byte(result), &jsonMap); err != nil {
			t.Fatalf("Result is not valid JSON: %v", err)
		}

		if len(jsonMap) != 0 {
			t.Errorf("Expected empty map, got %d entries", len(jsonMap))
		}
	})

	t.Run("device path format", func(t *testing.T) {
		pvcMounts := []PVCMountInfo{
			{
				PVCName:    "test-pvc",
				PVCUID:     "abc-123-xyz",
				BackupName: "backup-1",
			},
		}

		result := buildBackupPVCMapJSON(pvcMounts)

		var jsonMap map[string][]map[string]string
		if err := json.Unmarshal([]byte(result), &jsonMap); err != nil {
			t.Fatalf("Failed to unmarshal JSON: %v", err)
		}

		expectedPath := "/dev/pvc-abc-123-xyz"
		actualPath := jsonMap["backup-1"][0]["path"]

		if actualPath != expectedPath {
			t.Errorf("Expected device path '%s', got '%s'", expectedPath, actualPath)
		}
	})
}

func TestBuildVMFileServerMainContainer(t *testing.T) {
	t.Run("basic container configuration", func(t *testing.T) {
		pvcMounts := []PVCMountInfo{
			{
				PVCName:    "test-pvc",
				PVCUID:     "uid-123",
				BackupName: "backup-1",
			},
		}

		container := buildVMFileServerMainContainer(pvcMounts)

		// Verify container name
		if container.Name != "vm-file-server" {
			t.Errorf("Expected container name 'vm-file-server', got '%s'", container.Name)
		}

		// Verify image
		if container.Image != constant.VMFileServerImage {
			t.Errorf("Expected image '%s', got '%s'", constant.VMFileServerImage, container.Image)
		}

		// Verify command
		if len(container.Command) != 1 {
			t.Errorf("Expected 1 command, got %d", len(container.Command))
		} else if container.Command[0] != "/usr/local/bin/detect-and-mount.sh" {
			t.Errorf("Expected command '/usr/local/bin/detect-and-mount.sh', got '%s'", container.Command[0])
		}
	})

	t.Run("environment variables", func(t *testing.T) {
		timestamp := metav1.Now()
		pvcMounts := []PVCMountInfo{
			{
				PVCName:         "pvc-1",
				PVCUID:          "uid-1",
				BackupName:      "backup-1",
				BackupTimestamp: &timestamp,
			},
		}

		container := buildVMFileServerMainContainer(pvcMounts)

		// Should have HOME and BACKUP_PVC_MAP env vars
		if len(container.Env) != 2 {
			t.Errorf("Expected 2 environment variables, got %d", len(container.Env))
		}

		// Check HOME env var
		var homeEnv *corev1.EnvVar
		var backupMapEnv *corev1.EnvVar
		for i := range container.Env {
			if container.Env[i].Name == "HOME" {
				homeEnv = &container.Env[i]
			}
			if container.Env[i].Name == "BACKUP_PVC_MAP" {
				backupMapEnv = &container.Env[i]
			}
		}

		if homeEnv == nil {
			t.Error("Missing HOME environment variable")
		} else if homeEnv.Value != "/tmp" {
			t.Errorf("Expected HOME='/tmp', got '%s'", homeEnv.Value)
		}

		if backupMapEnv == nil {
			t.Error("Missing BACKUP_PVC_MAP environment variable")
		} else {
			// Verify it's valid JSON
			var jsonMap map[string][]map[string]string
			if err := json.Unmarshal([]byte(backupMapEnv.Value), &jsonMap); err != nil {
				t.Errorf("BACKUP_PVC_MAP is not valid JSON: %v", err)
			}
		}
	})

	t.Run("security context", func(t *testing.T) {
		pvcMounts := []PVCMountInfo{
			{
				PVCName:    "test-pvc",
				PVCUID:     "uid-123",
				BackupName: "backup-1",
			},
		}

		container := buildVMFileServerMainContainer(pvcMounts)

		if container.SecurityContext == nil {
			t.Fatal("SecurityContext is nil")
		}

		// Should be privileged
		if container.SecurityContext.Privileged == nil || !*container.SecurityContext.Privileged {
			t.Error("Container should be privileged")
		}

		// Should run as qemu user (107)
		if container.SecurityContext.RunAsUser == nil || *container.SecurityContext.RunAsUser != 107 {
			t.Errorf("Expected RunAsUser=107, got %v", container.SecurityContext.RunAsUser)
		}

		// Should run as qemu group (107)
		if container.SecurityContext.RunAsGroup == nil || *container.SecurityContext.RunAsGroup != 107 {
			t.Errorf("Expected RunAsGroup=107, got %v", container.SecurityContext.RunAsGroup)
		}
	})

	t.Run("resource requirements", func(t *testing.T) {
		pvcMounts := []PVCMountInfo{
			{
				PVCName:    "test-pvc",
				PVCUID:     "uid-123",
				BackupName: "backup-1",
			},
		}

		container := buildVMFileServerMainContainer(pvcMounts)

		// Verify requests
		memRequest := container.Resources.Requests[corev1.ResourceMemory]
		cpuRequest := container.Resources.Requests[corev1.ResourceCPU]

		if memRequest.String() != "512Mi" {
			t.Errorf("Expected memory request '512Mi', got '%s'", memRequest.String())
		}
		if cpuRequest.String() != "250m" {
			t.Errorf("Expected CPU request '250m', got '%s'", cpuRequest.String())
		}

		// Verify limits
		memLimit := container.Resources.Limits[corev1.ResourceMemory]
		cpuLimit := container.Resources.Limits[corev1.ResourceCPU]

		if memLimit.String() != "2Gi" {
			t.Errorf("Expected memory limit '2Gi', got '%s'", memLimit.String())
		}
		if cpuLimit.String() != "1" {
			t.Errorf("Expected CPU limit '1', got '%s'", cpuLimit.String())
		}
	})

	t.Run("empty PVC mounts", func(t *testing.T) {
		pvcMounts := []PVCMountInfo{}

		container := buildVMFileServerMainContainer(pvcMounts)

		// Should still create valid container
		if container.Name != "vm-file-server" {
			t.Errorf("Expected container name 'vm-file-server', got '%s'", container.Name)
		}

		// BACKUP_PVC_MAP should be empty JSON object
		var backupMapEnv *corev1.EnvVar
		for i := range container.Env {
			if container.Env[i].Name == "BACKUP_PVC_MAP" {
				backupMapEnv = &container.Env[i]
				break
			}
		}

		if backupMapEnv != nil {
			var jsonMap map[string][]map[string]string
			if err := json.Unmarshal([]byte(backupMapEnv.Value), &jsonMap); err != nil {
				t.Errorf("BACKUP_PVC_MAP is not valid JSON: %v", err)
			}
			if len(jsonMap) != 0 {
				t.Error("Expected empty BACKUP_PVC_MAP for empty PVC mounts")
			}
		}
	})
}

func TestFormatBackupDateForPath(t *testing.T) {
	t.Run("nil timestamp", func(t *testing.T) {
		result := formatBackupDateForPath(nil)
		if result != "unknown-date" {
			t.Errorf("Expected 'unknown-date', got '%s'", result)
		}
	})

	t.Run("valid timestamp", func(t *testing.T) {
		// Create a specific timestamp: 2025-01-15 14:30:00 UTC
		timestamp := metav1.NewTime(metav1.Now().Time.Truncate(0))

		result := formatBackupDateForPath(&timestamp)

		// Verify it's in YYYY-MM-DD format
		if len(result) != 10 {
			t.Errorf("Expected date format YYYY-MM-DD (length 10), got '%s' (length %d)", result, len(result))
		}

		// Verify it matches the expected format
		expectedDate := timestamp.Time.Format("2006-01-02")
		if result != expectedDate {
			t.Errorf("Expected '%s', got '%s'", expectedDate, result)
		}
	})

	t.Run("date formatting consistency", func(t *testing.T) {
		// Test a known date
		knownTime := metav1.NewTime(time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC))

		result := formatBackupDateForPath(&knownTime)

		expected := "2025-01-15"
		if result != expected {
			t.Errorf("Expected '%s', got '%s'", expected, result)
		}
	})
}

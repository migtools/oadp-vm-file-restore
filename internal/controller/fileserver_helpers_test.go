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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	// This test would require importing the API types
	// Skipping for now as it's tested indirectly via integration tests
	t.Skip("Requires full API types setup")
}

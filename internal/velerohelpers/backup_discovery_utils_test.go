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
	"testing"

	veleroapi "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
)

func TestValidateVMInBackupSpec(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vm",
			Namespace: "vm-namespace",
		},
	}

	tests := []struct {
		name     string
		backup   *veleroapi.Backup
		vm       *kubevirtv1.VirtualMachine
		expected bool
	}{
		{
			name: "no inclusions or exclusions - should include",
			backup: &veleroapi.Backup{
				Spec: veleroapi.BackupSpec{},
			},
			vm:       vm,
			expected: true,
		},
		{
			name: "vm namespace explicitly excluded - should not include",
			backup: &veleroapi.Backup{
				Spec: veleroapi.BackupSpec{
					ExcludedNamespaces: []string{"vm-namespace"},
				},
			},
			vm:       vm,
			expected: false,
		},
		{
			name: "vm namespace in exclusions with other namespaces - should not include",
			backup: &veleroapi.Backup{
				Spec: veleroapi.BackupSpec{
					ExcludedNamespaces: []string{"other-ns", "vm-namespace", "another-ns"},
				},
			},
			vm:       vm,
			expected: false,
		},
		{
			name: "vm namespace not in exclusions - should include",
			backup: &veleroapi.Backup{
				Spec: veleroapi.BackupSpec{
					ExcludedNamespaces: []string{"other-ns", "another-ns"},
				},
			},
			vm:       vm,
			expected: true,
		},
		{
			name: "vm namespace explicitly included - should include",
			backup: &veleroapi.Backup{
				Spec: veleroapi.BackupSpec{
					IncludedNamespaces: []string{"vm-namespace"},
				},
			},
			vm:       vm,
			expected: true,
		},
		{
			name: "vm namespace in inclusions with other namespaces - should include",
			backup: &veleroapi.Backup{
				Spec: veleroapi.BackupSpec{
					IncludedNamespaces: []string{"other-ns", "vm-namespace", "another-ns"},
				},
			},
			vm:       vm,
			expected: true,
		},
		{
			name: "vm namespace not in inclusions - should not include",
			backup: &veleroapi.Backup{
				Spec: veleroapi.BackupSpec{
					IncludedNamespaces: []string{"other-ns", "another-ns"},
				},
			},
			vm:       vm,
			expected: false,
		},
		{
			name: "wildcard inclusion - should include",
			backup: &veleroapi.Backup{
				Spec: veleroapi.BackupSpec{
					IncludedNamespaces: []string{"*"},
				},
			},
			vm:       vm,
			expected: true,
		},
		{
			name: "wildcard inclusion with other namespaces - should include",
			backup: &veleroapi.Backup{
				Spec: veleroapi.BackupSpec{
					IncludedNamespaces: []string{"other-ns", "*", "another-ns"},
				},
			},
			vm:       vm,
			expected: true,
		},
		{
			name: "excluded namespace takes precedence over inclusion",
			backup: &veleroapi.Backup{
				Spec: veleroapi.BackupSpec{
					IncludedNamespaces: []string{"vm-namespace"},
					ExcludedNamespaces: []string{"vm-namespace"},
				},
			},
			vm:       vm,
			expected: false,
		},
		{
			name: "excluded namespace takes precedence over wildcard inclusion",
			backup: &veleroapi.Backup{
				Spec: veleroapi.BackupSpec{
					IncludedNamespaces: []string{"*"},
					ExcludedNamespaces: []string{"vm-namespace"},
				},
			},
			vm:       vm,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateVMInBackupSpec(tt.backup, tt.vm)
			if result != tt.expected {
				t.Errorf("ValidateVMInBackupSpec() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestHasVMInBackupResources(t *testing.T) {
	tests := []struct {
		name        string
		vmName      string
		vmNamespace string
		resources   []struct{ kind, namespace, name string }
		expected    bool
	}{
		{
			name:        "vm found with exact match",
			vmName:      "test-vm",
			vmNamespace: "test-ns",
			resources: []struct{ kind, namespace, name string }{
				{"VirtualMachine", "test-ns", "test-vm"},
			},
			expected: true,
		},
		{
			name:        "vm found with lowercase kind",
			vmName:      "test-vm",
			vmNamespace: "test-ns",
			resources: []struct{ kind, namespace, name string }{
				{"virtualmachine", "test-ns", "test-vm"},
			},
			expected: true,
		},
		{
			name:        "vm found among multiple resources",
			vmName:      "test-vm",
			vmNamespace: "test-ns",
			resources: []struct{ kind, namespace, name string }{
				{"Pod", "test-ns", "some-pod"},
				{"VirtualMachine", "test-ns", "test-vm"},
				{"Service", "test-ns", "some-service"},
			},
			expected: true,
		},
		{
			name:        "vm not found - wrong name",
			vmName:      "test-vm",
			vmNamespace: "test-ns",
			resources: []struct{ kind, namespace, name string }{
				{"VirtualMachine", "test-ns", "other-vm"},
			},
			expected: false,
		},
		{
			name:        "vm not found - wrong namespace",
			vmName:      "test-vm",
			vmNamespace: "test-ns",
			resources: []struct{ kind, namespace, name string }{
				{"VirtualMachine", "other-ns", "test-vm"},
			},
			expected: false,
		},
		{
			name:        "vm not found - wrong kind",
			vmName:      "test-vm",
			vmNamespace: "test-ns",
			resources: []struct{ kind, namespace, name string }{
				{"Pod", "test-ns", "test-vm"},
			},
			expected: false,
		},
		{
			name:        "no resources",
			vmName:      "test-vm",
			vmNamespace: "test-ns",
			resources:   []struct{ kind, namespace, name string }{},
			expected:    false,
		},
		{
			name:        "iteration stops when vm found",
			vmName:      "test-vm",
			vmNamespace: "test-ns",
			resources: []struct{ kind, namespace, name string }{
				{"VirtualMachine", "test-ns", "test-vm"},
				{"VirtualMachine", "test-ns", "test-vm"}, // This should not be reached
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iterationCount := 0
			result := hasVMInBackupResources(tt.vmName, tt.vmNamespace, func(yield func(kind, namespace, name string) bool) {
				for _, res := range tt.resources {
					iterationCount++
					if !yield(res.kind, res.namespace, res.name) {
						break
					}
				}
			})

			if result != tt.expected {
				t.Errorf("hasVMInBackupResources() = %v, expected %v", result, tt.expected)
			}

			// Verify that iteration stops when VM is found
			if tt.expected && len(tt.resources) > 1 {
				if iterationCount > 1 {
					// Find the index of the matching resource
					matchIndex := -1
					for i, res := range tt.resources {
						if (res.kind == virtualMachineKind || res.kind == virtualMachineKindLowercase) &&
							res.namespace == tt.vmNamespace &&
							res.name == tt.vmName {
							matchIndex = i
							break
						}
					}
					if matchIndex >= 0 && iterationCount > matchIndex+1 {
						t.Errorf("Expected iteration to stop after finding VM, but continued. Expected <= %d iterations, got %d", matchIndex+1, iterationCount)
					}
				}
			}
		})
	}
}

func TestParseNamespacedName(t *testing.T) {
	tests := []struct {
		name              string
		resource          string
		expectedNamespace string
		expectedName      string
	}{
		{
			name:              "namespaced resource",
			resource:          "test-namespace/resource-name",
			expectedNamespace: "test-namespace",
			expectedName:      "resource-name",
		},
		{
			name:              "cluster-scoped resource",
			resource:          "cluster-resource",
			expectedNamespace: "",
			expectedName:      "cluster-resource",
		},
		{
			name:              "empty string",
			resource:          "",
			expectedNamespace: "",
			expectedName:      "",
		},
		{
			name:              "only namespace with slash",
			resource:          "namespace/",
			expectedNamespace: "namespace",
			expectedName:      "",
		},
		{
			name:              "only slash",
			resource:          "/",
			expectedNamespace: "",
			expectedName:      "",
		},
		{
			name:              "multiple slashes - first one wins",
			resource:          "ns1/name/with/slashes",
			expectedNamespace: "ns1",
			expectedName:      "name/with/slashes",
		},
		{
			name:              "namespace with special characters",
			resource:          "test-ns_123/resource.name",
			expectedNamespace: "test-ns_123",
			expectedName:      "resource.name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace, name := parseNamespacedName(tt.resource)
			if namespace != tt.expectedNamespace {
				t.Errorf("parseNamespacedName() namespace = %q, expected %q", namespace, tt.expectedNamespace)
			}
			if name != tt.expectedName {
				t.Errorf("parseNamespacedName() name = %q, expected %q", name, tt.expectedName)
			}
		})
	}
}

func TestExtractKindFromGVK(t *testing.T) {
	tests := []struct {
		name         string
		gvk          string
		expectedKind string
	}{
		{
			name:         "full GVK with version and group",
			gvk:          "v1/VirtualMachine.kubevirt.io",
			expectedKind: "VirtualMachine",
		},
		{
			name:         "kind with group but no version",
			gvk:          "VirtualMachine.kubevirt.io",
			expectedKind: "VirtualMachine",
		},
		{
			name:         "kind only",
			gvk:          "VirtualMachine",
			expectedKind: "VirtualMachine",
		},
		{
			name:         "core API with version",
			gvk:          "v1/Pod",
			expectedKind: "Pod",
		},
		{
			name:         "empty string",
			gvk:          "",
			expectedKind: "",
		},
		{
			name:         "only version slash",
			gvk:          "v1/",
			expectedKind: "",
		},
		{
			name:         "only group dot",
			gvk:          ".kubevirt.io",
			expectedKind: "",
		},
		{
			name:         "complex version",
			gvk:          "v1alpha1/CustomResource.example.com",
			expectedKind: "CustomResource",
		},
		{
			name:         "multiple dots in group",
			gvk:          "v1/Resource.sub.example.com",
			expectedKind: "Resource",
		},
		{
			name:         "multiple slashes - last one wins for version",
			gvk:          "apps/v1/Deployment.apps",
			expectedKind: "Deployment",
		},
		{
			name:         "kind with numbers",
			gvk:          "v1/VirtualMachine123.kubevirt.io",
			expectedKind: "VirtualMachine123",
		},
		{
			name:         "kind with underscore",
			gvk:          "v1/Virtual_Machine.kubevirt.io",
			expectedKind: "Virtual_Machine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractKindFromGVK(tt.gvk)
			if result != tt.expectedKind {
				t.Errorf("extractKindFromGVK(%q) = %q, expected %q", tt.gvk, result, tt.expectedKind)
			}
		})
	}
}

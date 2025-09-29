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
	"encoding/json"
	"errors"
	"testing"

	"github.com/sirupsen/logrus"
	veleroapi "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNewVeleroBackupContentsReader(t *testing.T) {
	reader := NewVeleroBackupContentsReader()

	if reader == nil {
		t.Fatal("NewVeleroBackupContentsReader() returned nil")
	}

	if reader.logger == nil {
		t.Error("Expected logger to be initialized")
	}

	if reader.insecureSkipTLSVerify != false {
		t.Error("Expected insecureSkipTLSVerify to be false by default")
	}

	if reader.caCertFile != "" {
		t.Error("Expected caCertFile to be empty by default")
	}

	if reader.downloadTimeout.Minutes() != 5 {
		t.Errorf("Expected downloadTimeout to be 5 minutes, got %v", reader.downloadTimeout)
	}
}

func TestSetClient(t *testing.T) {
	reader := NewVeleroBackupContentsReader()

	// Mock client (nil is fine for this test)
	var mockClient client.Client

	reader.SetClient(mockClient)

	if reader.k8sClient != mockClient {
		t.Error("SetClient() did not set the client correctly")
	}
}

func TestBackupContainsVM_SpecValidationFails(t *testing.T) {
	reader := NewVeleroBackupContentsReader()
	reader.logger = logrus.New() // Set to avoid nil pointer

	backup := &veleroapi.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backup",
			Namespace: "velero",
		},
		Spec: veleroapi.BackupSpec{
			ExcludedNamespaces: []string{"vm-namespace"}, // VM namespace is excluded
		},
	}

	ctx := context.Background()
	result, err := reader.BackupContainsVM(ctx, backup, "test-vm", "vm-namespace")

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if result {
		t.Error("Expected false when VM namespace is excluded")
	}
}

func TestBackupContainsVM_SpecValidationPasses_MetadataFetchFails(t *testing.T) {
	reader := NewVeleroBackupContentsReader()
	reader.logger = logrus.New()

	// Set a mock client that will cause metadata fetch to fail
	mockClient := &mockK8sClient{shouldFail: true}
	reader.SetClient(mockClient)

	backup := &veleroapi.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backup",
			Namespace: "velero",
		},
		Spec: veleroapi.BackupSpec{
			IncludedNamespaces: []string{"vm-namespace"}, // VM namespace is included
		},
	}

	ctx := context.Background()
	result, err := reader.BackupContainsVM(ctx, backup, "test-vm", "vm-namespace")

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Should return true as fallback when metadata fetch fails but spec validation passes
	if !result {
		t.Error("Expected true when spec validation passes but metadata fetch fails")
	}
}

func TestHasVMInMetadata(t *testing.T) {
	reader := NewVeleroBackupContentsReader()

	tests := []struct {
		name        string
		metadata    *BackupMetadata
		vmName      string
		vmNamespace string
		expected    bool
	}{
		{
			name: "VM found in metadata",
			metadata: &BackupMetadata{
				Items: []BackupResourceItem{
					{Kind: "VirtualMachine", Namespace: "test-ns", Name: "test-vm"},
					{Kind: "Pod", Namespace: "test-ns", Name: "some-pod"},
				},
			},
			vmName:      "test-vm",
			vmNamespace: "test-ns",
			expected:    true,
		},
		{
			name: "VM not found in metadata",
			metadata: &BackupMetadata{
				Items: []BackupResourceItem{
					{Kind: "Pod", Namespace: "test-ns", Name: "some-pod"},
					{Kind: "VirtualMachine", Namespace: "other-ns", Name: "other-vm"},
				},
			},
			vmName:      "test-vm",
			vmNamespace: "test-ns",
			expected:    false,
		},
		{
			name: "empty metadata",
			metadata: &BackupMetadata{
				Items: []BackupResourceItem{},
			},
			vmName:      "test-vm",
			vmNamespace: "test-ns",
			expected:    false,
		},
		{
			name: "VM found with lowercase kind",
			metadata: &BackupMetadata{
				Items: []BackupResourceItem{
					{Kind: "virtualmachine", Namespace: "test-ns", Name: "test-vm"},
				},
			},
			vmName:      "test-vm",
			vmNamespace: "test-ns",
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reader.hasVMInMetadata(tt.metadata, tt.vmName, tt.vmNamespace)
			if result != tt.expected {
				t.Errorf("hasVMInMetadata() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestBackupMetadata_JSONParsing(t *testing.T) {
	// Test that our BackupMetadata struct can parse JSON correctly
	jsonData := `{
		"items": [
			{
				"apiVersion": "v1/VirtualMachine.kubevirt.io",
				"kind": "VirtualMachine",
				"namespace": "test-ns",
				"name": "test-vm"
			},
			{
				"apiVersion": "v1",
				"kind": "Pod",
				"namespace": "test-ns",
				"name": "test-pod"
			}
		]
	}`

	var metadata BackupMetadata
	err := json.Unmarshal([]byte(jsonData), &metadata)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if len(metadata.Items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(metadata.Items))
	}

	vmItem := metadata.Items[0]
	if vmItem.Kind != virtualMachineKind {
		t.Errorf("Expected Kind to be VirtualMachine, got %s", vmItem.Kind)
	}
	if vmItem.Namespace != "test-ns" {
		t.Errorf("Expected Namespace to be test-ns, got %s", vmItem.Namespace)
	}
	if vmItem.Name != "test-vm" {
		t.Errorf("Expected Name to be test-vm, got %s", vmItem.Name)
	}
}

func TestBackupResourceItem_JSONParsing(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
		expected BackupResourceItem
	}{
		{
			name: "namespaced resource",
			jsonData: `{
				"apiVersion": "v1/VirtualMachine.kubevirt.io",
				"kind": "VirtualMachine",
				"namespace": "test-ns",
				"name": "test-vm"
			}`,
			expected: BackupResourceItem{
				APIVersion: "v1/VirtualMachine.kubevirt.io",
				Kind:       "VirtualMachine",
				Namespace:  "test-ns",
				Name:       "test-vm",
			},
		},
		{
			name: "cluster-scoped resource (no namespace)",
			jsonData: `{
				"apiVersion": "v1",
				"kind": "Node",
				"name": "worker-1"
			}`,
			expected: BackupResourceItem{
				APIVersion: "v1",
				Kind:       "Node",
				Namespace:  "", // Should be empty for cluster-scoped
				Name:       "worker-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var item BackupResourceItem
			err := json.Unmarshal([]byte(tt.jsonData), &item)
			if err != nil {
				t.Fatalf("Failed to unmarshal JSON: %v", err)
			}

			if item.APIVersion != tt.expected.APIVersion {
				t.Errorf("APIVersion = %q, expected %q", item.APIVersion, tt.expected.APIVersion)
			}
			if item.Kind != tt.expected.Kind {
				t.Errorf("Kind = %q, expected %q", item.Kind, tt.expected.Kind)
			}
			if item.Namespace != tt.expected.Namespace {
				t.Errorf("Namespace = %q, expected %q", item.Namespace, tt.expected.Namespace)
			}
			if item.Name != tt.expected.Name {
				t.Errorf("Name = %q, expected %q", item.Name, tt.expected.Name)
			}
		})
	}
}

// Mock client for testing
type mockK8sClient struct {
	client.Client
	shouldFail bool
}

func (m *mockK8sClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if m.shouldFail {
		return errors.New("mock client error")
	}
	return nil
}

func (m *mockK8sClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if m.shouldFail {
		return errors.New("mock client error")
	}
	return nil
}

func (m *mockK8sClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if m.shouldFail {
		return errors.New("mock client error")
	}
	return nil
}

func (m *mockK8sClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if m.shouldFail {
		return errors.New("mock client error")
	}
	return nil
}

func (m *mockK8sClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if m.shouldFail {
		return errors.New("mock client error")
	}
	return nil
}

func (m *mockK8sClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	if m.shouldFail {
		return errors.New("mock client error")
	}
	return nil
}

func (m *mockK8sClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	if m.shouldFail {
		return errors.New("mock client error")
	}
	return nil
}

func (m *mockK8sClient) Status() client.StatusWriter {
	return &mockStatusWriter{shouldFail: m.shouldFail}
}

func (m *mockK8sClient) Scheme() *runtime.Scheme {
	return nil
}

func (m *mockK8sClient) RESTMapper() meta.RESTMapper {
	return nil
}

// Mock status writer for testing
type mockStatusWriter struct {
	shouldFail bool
}

func (m *mockStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	if m.shouldFail {
		return errors.New("mock status writer error")
	}
	return nil
}

func (m *mockStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	if m.shouldFail {
		return errors.New("mock status writer error")
	}
	return nil
}

func (m *mockStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	if m.shouldFail {
		return errors.New("mock status writer error")
	}
	return nil
}

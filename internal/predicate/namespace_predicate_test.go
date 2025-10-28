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

package predicate

import (
	"testing"

	oadpv1alpha1 "github.com/migtools/oadp-vm-file-restore/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestNamespacePredicate_Create(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		vmbd           *oadpv1alpha1.VirtualMachineBackupsDiscovery
		expectedAccept bool
		description    string
	}{
		{
			name:      "create in correct namespace",
			namespace: "openshift-adp",
			vmbd: &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmbd",
					Namespace: "openshift-adp",
				},
			},
			expectedAccept: true,
			description:    "should accept events from the specified namespace",
		},
		{
			name:      "create in wrong namespace",
			namespace: "openshift-adp",
			vmbd: &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmbd",
					Namespace: "other-namespace",
				},
			},
			expectedAccept: false,
			description:    "should reject events from other namespaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := NamespacePredicate{Namespace: tt.namespace}

			evt := event.CreateEvent{
				Object: tt.vmbd,
			}

			result := predicate.Create(evt)
			if result != tt.expectedAccept {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expectedAccept, result)
			}
		})
	}
}

func TestNamespacePredicate_Update(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		oldVMBD        *oadpv1alpha1.VirtualMachineBackupsDiscovery
		newVMBD        *oadpv1alpha1.VirtualMachineBackupsDiscovery
		expectedAccept bool
		description    string
	}{
		{
			name:      "update in correct namespace",
			namespace: "openshift-adp",
			oldVMBD: &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmbd",
					Namespace: "openshift-adp",
				},
			},
			newVMBD: &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmbd",
					Namespace: "openshift-adp",
				},
			},
			expectedAccept: true,
			description:    "should accept update events from the specified namespace",
		},
		{
			name:      "update in wrong namespace",
			namespace: "openshift-adp",
			oldVMBD: &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmbd",
					Namespace: "other-namespace",
				},
			},
			newVMBD: &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmbd",
					Namespace: "other-namespace",
				},
			},
			expectedAccept: false,
			description:    "should reject update events from other namespaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := NamespacePredicate{Namespace: tt.namespace}

			evt := event.TypedUpdateEvent[client.Object]{
				ObjectOld: tt.oldVMBD,
				ObjectNew: tt.newVMBD,
			}

			result := predicate.Update(evt)
			if result != tt.expectedAccept {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expectedAccept, result)
			}
		})
	}
}

func TestNamespacePredicate_Delete(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		vmbd           *oadpv1alpha1.VirtualMachineBackupsDiscovery
		expectedAccept bool
		description    string
	}{
		{
			name:      "delete in correct namespace",
			namespace: "openshift-adp",
			vmbd: &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmbd",
					Namespace: "openshift-adp",
				},
			},
			expectedAccept: true,
			description:    "should accept delete events from the specified namespace",
		},
		{
			name:      "delete in wrong namespace",
			namespace: "openshift-adp",
			vmbd: &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmbd",
					Namespace: "other-namespace",
				},
			},
			expectedAccept: false,
			description:    "should reject delete events from other namespaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := NamespacePredicate{Namespace: tt.namespace}

			evt := event.DeleteEvent{
				Object: tt.vmbd,
			}

			result := predicate.Delete(evt)
			if result != tt.expectedAccept {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expectedAccept, result)
			}
		})
	}
}

func TestNamespacePredicate_Generic(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		vmbd           *oadpv1alpha1.VirtualMachineBackupsDiscovery
		expectedAccept bool
		description    string
	}{
		{
			name:      "generic in correct namespace",
			namespace: "openshift-adp",
			vmbd: &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmbd",
					Namespace: "openshift-adp",
				},
			},
			expectedAccept: true,
			description:    "should accept generic events from the specified namespace",
		},
		{
			name:      "generic in wrong namespace",
			namespace: "openshift-adp",
			vmbd: &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmbd",
					Namespace: "other-namespace",
				},
			},
			expectedAccept: false,
			description:    "should reject generic events from other namespaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := NamespacePredicate{Namespace: tt.namespace}

			evt := event.GenericEvent{
				Object: tt.vmbd,
			}

			result := predicate.Generic(evt)
			if result != tt.expectedAccept {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expectedAccept, result)
			}
		})
	}
}

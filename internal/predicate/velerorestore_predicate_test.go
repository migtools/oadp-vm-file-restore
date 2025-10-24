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

	"github.com/migtools/oadp-vm-file-restore/internal/common/constant"
	veleroapi "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	testOADPNamespace  = "openshift-adp"
	testOtherNamespace = "other-namespace"
)

func TestVeleroRestorePredicate_Create(t *testing.T) {
	predicate := VeleroRestorePredicate{
		OADPNamespace: testOADPNamespace,
	}

	restore := &veleroapi.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-restore",
			Namespace: testOADPNamespace,
		},
	}

	evt := event.CreateEvent{
		Object: restore,
	}

	// Create events should always return false
	result := predicate.Create(evt)
	if result {
		t.Error("Create() should return false for all events")
	}
}

func TestVeleroRestorePredicate_Update(t *testing.T) {
	tests := []struct {
		name           string
		oadpNamespace  string
		oldRestore     *veleroapi.Restore
		newRestore     *veleroapi.Restore
		expectedAccept bool
		description    string
	}{
		{
			name:          "phase change in OADP namespace with metadata",
			oadpNamespace: testOADPNamespace,
			oldRestore: &veleroapi.Restore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore",
					Namespace: testOADPNamespace,
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
					Annotations: map[string]string{
						constant.VMFROriginNameAnnotation:      "test-vmfr",
						constant.VMFROriginNamespaceAnnotation: "test-ns",
					},
				},
				Status: veleroapi.RestoreStatus{
					Phase: veleroapi.RestorePhaseNew,
				},
			},
			newRestore: &veleroapi.Restore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore",
					Namespace: testOADPNamespace,
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
					Annotations: map[string]string{
						constant.VMFROriginNameAnnotation:      "test-vmfr",
						constant.VMFROriginNamespaceAnnotation: "test-ns",
					},
				},
				Status: veleroapi.RestoreStatus{
					Phase: veleroapi.RestorePhaseInProgress,
				},
			},
			expectedAccept: true,
			description:    "should accept when phase changes in OADP namespace with required metadata",
		},
		{
			name:          "no phase change",
			oadpNamespace: testOADPNamespace,
			oldRestore: &veleroapi.Restore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore",
					Namespace: testOADPNamespace,
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
					Annotations: map[string]string{
						constant.VMFROriginNameAnnotation:      "test-vmfr",
						constant.VMFROriginNamespaceAnnotation: "test-ns",
					},
				},
				Status: veleroapi.RestoreStatus{
					Phase: veleroapi.RestorePhaseInProgress,
				},
			},
			newRestore: &veleroapi.Restore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore",
					Namespace: testOADPNamespace,
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
					Annotations: map[string]string{
						constant.VMFROriginNameAnnotation:      "test-vmfr",
						constant.VMFROriginNamespaceAnnotation: "test-ns",
					},
				},
				Status: veleroapi.RestoreStatus{
					Phase: veleroapi.RestorePhaseInProgress,
				},
			},
			expectedAccept: false,
			description:    "should reject when phase doesn't change (avoid unnecessary reconciliations)",
		},
		{
			name:          "phase change outside OADP namespace",
			oadpNamespace: testOADPNamespace,
			oldRestore: &veleroapi.Restore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore",
					Namespace: testOtherNamespace,
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
					Annotations: map[string]string{
						constant.VMFROriginNameAnnotation:      "test-vmfr",
						constant.VMFROriginNamespaceAnnotation: "test-ns",
					},
				},
				Status: veleroapi.RestoreStatus{
					Phase: veleroapi.RestorePhaseNew,
				},
			},
			newRestore: &veleroapi.Restore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore",
					Namespace: testOtherNamespace,
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
					Annotations: map[string]string{
						constant.VMFROriginNameAnnotation:      "test-vmfr",
						constant.VMFROriginNamespaceAnnotation: "test-ns",
					},
				},
				Status: veleroapi.RestoreStatus{
					Phase: veleroapi.RestorePhaseInProgress,
				},
			},
			expectedAccept: false,
			description:    "should reject when not in OADP namespace",
		},
		{
			name:          "phase change with missing metadata",
			oadpNamespace: testOADPNamespace,
			oldRestore: &veleroapi.Restore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore",
					Namespace: testOADPNamespace,
					// Missing required labels and annotations
				},
				Status: veleroapi.RestoreStatus{
					Phase: veleroapi.RestorePhaseNew,
				},
			},
			newRestore: &veleroapi.Restore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore",
					Namespace: testOADPNamespace,
					// Missing required labels and annotations
				},
				Status: veleroapi.RestoreStatus{
					Phase: veleroapi.RestorePhaseInProgress,
				},
			},
			expectedAccept: false,
			description:    "should reject when metadata is missing (not a VMFR-managed restore)",
		},
		{
			name:          "phase change with only labels (no annotations)",
			oadpNamespace: testOADPNamespace,
			oldRestore: &veleroapi.Restore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore",
					Namespace: testOADPNamespace,
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
					// Missing annotations - predicate only checks labels
				},
				Status: veleroapi.RestoreStatus{
					Phase: veleroapi.RestorePhaseNew,
				},
			},
			newRestore: &veleroapi.Restore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore",
					Namespace: testOADPNamespace,
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
					// Missing annotations - predicate only checks labels
				},
				Status: veleroapi.RestoreStatus{
					Phase: veleroapi.RestorePhaseInProgress,
				},
			},
			expectedAccept: true,
			description:    "should accept when labels present (predicate only checks labels, mapper validates annotations)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := VeleroRestorePredicate{
				OADPNamespace: tt.oadpNamespace,
			}

			evt := event.TypedUpdateEvent[client.Object]{
				ObjectOld: tt.oldRestore,
				ObjectNew: tt.newRestore,
			}

			result := predicate.Update(evt)
			if result != tt.expectedAccept {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expectedAccept, result)
			}
		})
	}
}

func TestVeleroRestorePredicate_Delete(t *testing.T) {
	tests := []struct {
		name           string
		oadpNamespace  string
		restore        *veleroapi.Restore
		expectedAccept bool
		description    string
	}{
		{
			name:          "delete in OADP namespace with metadata",
			oadpNamespace: testOADPNamespace,
			restore: &veleroapi.Restore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore",
					Namespace: testOADPNamespace,
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
					Annotations: map[string]string{
						constant.VMFROriginNameAnnotation:      "test-vmfr",
						constant.VMFROriginNamespaceAnnotation: "test-ns",
					},
				},
			},
			expectedAccept: true,
			description:    "should accept delete events in OADP namespace with metadata",
		},
		{
			name:          "delete outside OADP namespace",
			oadpNamespace: testOADPNamespace,
			restore: &veleroapi.Restore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore",
					Namespace: testOtherNamespace,
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
					Annotations: map[string]string{
						constant.VMFROriginNameAnnotation:      "test-vmfr",
						constant.VMFROriginNamespaceAnnotation: "test-ns",
					},
				},
			},
			expectedAccept: false,
			description:    "should reject delete events outside OADP namespace",
		},
		{
			name:          "delete with missing metadata",
			oadpNamespace: testOADPNamespace,
			restore: &veleroapi.Restore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restore",
					Namespace: testOADPNamespace,
					// Missing labels and annotations
				},
			},
			expectedAccept: false,
			description:    "should reject delete events without required metadata",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := VeleroRestorePredicate{
				OADPNamespace: tt.oadpNamespace,
			}

			evt := event.DeleteEvent{
				Object: tt.restore,
			}

			result := predicate.Delete(evt)
			if result != tt.expectedAccept {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expectedAccept, result)
			}
		})
	}
}

func TestVeleroRestorePredicate_Generic(t *testing.T) {
	predicate := VeleroRestorePredicate{
		OADPNamespace: testOADPNamespace,
	}

	restore := &veleroapi.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-restore",
			Namespace: testOADPNamespace,
			Labels: map[string]string{
				constant.VMFROriginUUIDLabel: "test-uid",
			},
			Annotations: map[string]string{
				constant.VMFROriginNameAnnotation:      "test-vmfr",
				constant.VMFROriginNamespaceAnnotation: "test-ns",
			},
		},
	}

	evt := event.GenericEvent{
		Object: restore,
	}

	// Generic events should always return false
	result := predicate.Generic(evt)
	if result {
		t.Error("Generic() should return false for all events")
	}
}

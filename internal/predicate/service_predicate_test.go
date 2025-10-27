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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestServicePredicate_Create(t *testing.T) {
	tests := []struct {
		name           string
		service        *corev1.Service
		expectedAccept bool
		description    string
	}{
		{
			name: "create with VMFR label",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-namespace",
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
				},
			},
			expectedAccept: true,
			description:    "should accept Service create events with VMFR label",
		},
		{
			name: "create without VMFR label",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-namespace",
					// No VMFR label
				},
			},
			expectedAccept: false,
			description:    "should reject Service create events without VMFR label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := ServicePredicate{}

			evt := event.CreateEvent{
				Object: tt.service,
			}

			result := predicate.Create(evt)
			if result != tt.expectedAccept {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expectedAccept, result)
			}
		})
	}
}

func TestServicePredicate_Update(t *testing.T) {
	tests := []struct {
		name           string
		oldService     *corev1.Service
		newService     *corev1.Service
		expectedAccept bool
		description    string
	}{
		{
			name: "update with VMFR label",
			oldService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-namespace",
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
				},
			},
			newService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-namespace",
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
				},
			},
			expectedAccept: true,
			description:    "should accept Service update events with VMFR label",
		},
		{
			name: "update without VMFR label",
			oldService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-namespace",
				},
			},
			newService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-namespace",
				},
			},
			expectedAccept: false,
			description:    "should reject Service update events without VMFR label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := ServicePredicate{}

			evt := event.TypedUpdateEvent[client.Object]{
				ObjectOld: tt.oldService,
				ObjectNew: tt.newService,
			}

			result := predicate.Update(evt)
			if result != tt.expectedAccept {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expectedAccept, result)
			}
		})
	}
}

func TestServicePredicate_Delete(t *testing.T) {
	tests := []struct {
		name           string
		service        *corev1.Service
		expectedAccept bool
		description    string
	}{
		{
			name: "delete with VMFR label",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-namespace",
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
				},
			},
			expectedAccept: true,
			description:    "should accept Service delete events with VMFR label",
		},
		{
			name: "delete without VMFR label",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-namespace",
				},
			},
			expectedAccept: false,
			description:    "should reject Service delete events without VMFR label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := ServicePredicate{}

			evt := event.DeleteEvent{
				Object: tt.service,
			}

			result := predicate.Delete(evt)
			if result != tt.expectedAccept {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expectedAccept, result)
			}
		})
	}
}

func TestServicePredicate_Generic(t *testing.T) {
	predicate := ServicePredicate{}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test-namespace",
			Labels: map[string]string{
				constant.VMFROriginUUIDLabel: "test-uid",
			},
		},
	}

	evt := event.GenericEvent{
		Object: service,
	}

	// Generic events should always return false
	result := predicate.Generic(evt)
	if result {
		t.Error("Generic() should return false for all Service events")
	}
}

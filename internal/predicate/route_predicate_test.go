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
	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestRoutePredicate_Create(t *testing.T) {
	tests := []struct {
		name           string
		route          *routev1.Route
		expectedAccept bool
		description    string
	}{
		{
			name: "create with VMFR label",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
				},
			},
			expectedAccept: true,
			description:    "should accept Route create events with VMFR label",
		},
		{
			name: "create without VMFR label",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
					// No VMFR label
				},
			},
			expectedAccept: false,
			description:    "should reject Route create events without VMFR label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := RoutePredicate{}

			evt := event.CreateEvent{
				Object: tt.route,
			}

			result := predicate.Create(evt)
			if result != tt.expectedAccept {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expectedAccept, result)
			}
		})
	}
}

func TestRoutePredicate_Update(t *testing.T) {
	tests := []struct {
		name           string
		oldRoute       *routev1.Route
		newRoute       *routev1.Route
		expectedAccept bool
		description    string
	}{
		{
			name: "update with VMFR label",
			oldRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
				},
			},
			newRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
				},
			},
			expectedAccept: true,
			description:    "should accept Route update events with VMFR label",
		},
		{
			name: "update without VMFR label",
			oldRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			newRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAccept: false,
			description:    "should reject Route update events without VMFR label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := RoutePredicate{}

			evt := event.TypedUpdateEvent[client.Object]{
				ObjectOld: tt.oldRoute,
				ObjectNew: tt.newRoute,
			}

			result := predicate.Update(evt)
			if result != tt.expectedAccept {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expectedAccept, result)
			}
		})
	}
}

func TestRoutePredicate_Delete(t *testing.T) {
	tests := []struct {
		name           string
		route          *routev1.Route
		expectedAccept bool
		description    string
	}{
		{
			name: "delete with VMFR label",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
					Labels: map[string]string{
						constant.VMFROriginUUIDLabel: "test-uid",
					},
				},
			},
			expectedAccept: true,
			description:    "should accept Route delete events with VMFR label",
		},
		{
			name: "delete without VMFR label",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAccept: false,
			description:    "should reject Route delete events without VMFR label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := RoutePredicate{}

			evt := event.DeleteEvent{
				Object: tt.route,
			}

			result := predicate.Delete(evt)
			if result != tt.expectedAccept {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expectedAccept, result)
			}
		})
	}
}

func TestRoutePredicate_Generic(t *testing.T) {
	predicate := RoutePredicate{}

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
			Labels: map[string]string{
				constant.VMFROriginUUIDLabel: "test-uid",
			},
		},
	}

	evt := event.GenericEvent{
		Object: route,
	}

	// Generic events should always return false
	result := predicate.Generic(evt)
	if result {
		t.Error("Generic() should return false for all Route events")
	}
}

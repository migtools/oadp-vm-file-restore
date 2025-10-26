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

// Package predicate implements event filtering predicates for various Kubernetes resources
// used by the OADP VM file restore controller.
package predicate

import (
	"context"

	"github.com/migtools/oadp-vm-file-restore/internal/common/function"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// RoutePredicate contains event filters for OpenShift Route objects owned by VirtualMachineFileRestore
type RoutePredicate struct{}

// Create event filter only accepts Routes with VMFR labels
func (p RoutePredicate) Create(evt event.CreateEvent) bool {
	logger := function.GetLogger(context.Background(), evt.Object, "RoutePredicate")

	if function.HasVMFRLabel(evt.Object) {
		logger.V(1).Info("Accepted Route Create event")
		return true
	}

	logger.V(1).Info("Rejected Route Create event - no VMFR label")
	return false
}

// Update event filter only accepts Routes with VMFR labels and status changes
func (p RoutePredicate) Update(evt event.TypedUpdateEvent[client.Object]) bool {
	logger := function.GetLogger(context.Background(), evt.ObjectNew, "RoutePredicate")

	if function.HasVMFRLabel(evt.ObjectNew) {
		// Accept updates - we care about Route status changes (e.g., when host is assigned)
		logger.V(1).Info("Accepted Route Update event")
		return true
	}

	logger.V(1).Info("Rejected Route Update event - no VMFR label")
	return false
}

// Delete event filter only accepts Routes with VMFR labels
func (p RoutePredicate) Delete(evt event.DeleteEvent) bool {
	logger := function.GetLogger(context.Background(), evt.Object, "RoutePredicate")

	if function.HasVMFRLabel(evt.Object) {
		logger.V(1).Info("Accepted Route Delete event")
		return true
	}

	logger.V(1).Info("Rejected Route Delete event - no VMFR label")
	return false
}

// Generic determines whether to process generic Route events
func (p RoutePredicate) Generic(evt event.GenericEvent) bool {
	logger := function.GetLogger(context.Background(), evt.Object, "RoutePredicate")

	// We don't need to process generic events for Routes
	logger.V(1).Info("Filtering out Route generic event", "routeName", evt.Object.GetName())
	return false
}

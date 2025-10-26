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

// ServicePredicate contains event filters for Service objects owned by VirtualMachineFileRestore
type ServicePredicate struct{}

// Create event filter only accepts Services with VMFR labels
func (p ServicePredicate) Create(evt event.CreateEvent) bool {
	logger := function.GetLogger(context.Background(), evt.Object, "ServicePredicate")

	if function.HasVMFRLabel(evt.Object) {
		logger.V(1).Info("Accepted Service Create event")
		return true
	}

	logger.V(1).Info("Rejected Service Create event - no VMFR label")
	return false
}

// Update event filter only accepts Services with VMFR labels
func (p ServicePredicate) Update(evt event.TypedUpdateEvent[client.Object]) bool {
	logger := function.GetLogger(context.Background(), evt.ObjectNew, "ServicePredicate")

	if function.HasVMFRLabel(evt.ObjectNew) {
		logger.V(1).Info("Accepted Service Update event")
		return true
	}

	logger.V(1).Info("Rejected Service Update event - no VMFR label")
	return false
}

// Delete event filter only accepts Services with VMFR labels
func (p ServicePredicate) Delete(evt event.DeleteEvent) bool {
	logger := function.GetLogger(context.Background(), evt.Object, "ServicePredicate")

	if function.HasVMFRLabel(evt.Object) {
		logger.V(1).Info("Accepted Service Delete event")
		return true
	}

	logger.V(1).Info("Rejected Service Delete event - no VMFR label")
	return false
}

// Generic determines whether to process generic Service events
func (p ServicePredicate) Generic(evt event.GenericEvent) bool {
	logger := function.GetLogger(context.Background(), evt.Object, "ServicePredicate")

	// We don't need to process generic events for Services
	logger.V(1).Info("Filtering out Service generic event", "serviceName", evt.Object.GetName())
	return false
}

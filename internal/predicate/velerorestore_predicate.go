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
	veleroapi "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// VeleroRestorePredicate contains event filters for Velero Restore objects
type VeleroRestorePredicate struct {
	OADPNamespace string
}

func (p VeleroRestorePredicate) Create(evt event.CreateEvent) bool {
	// We don't need to process create events as they don't affect status initially
	return false
}

// Update event filter only accepts Velero Restore update events from OADP namespace
// and from Velero Restores that have required metadata
func (p VeleroRestorePredicate) Update(evt event.TypedUpdateEvent[client.Object]) bool {
	logger := function.GetLogger(context.Background(), evt.ObjectNew, "VeleroRestorePredicate")

	namespace := evt.ObjectNew.GetNamespace()
	if namespace == p.OADPNamespace {
		// Only accept update events where the Restore Status actually changed
		oldRestore, okOld := evt.ObjectOld.(*veleroapi.Restore)
		// Ignore if the status is veleroapi.RestorePhaseNew or empty
		if oldRestore.Status.Phase == veleroapi.RestorePhaseNew || oldRestore.Status.Phase == "" {
			logger.V(1).Info("Filtered Restore Update event with Status New or empty")
			return false
		}

		newRestore, okNew := evt.ObjectNew.(*veleroapi.Restore)
		if !okOld || !okNew {
			logger.V(1).Info("Rejected Restore Update event due to type assertion failure")
			return false
		}

		// Only accept update events where the Restore Status actually changed
		if function.CheckVeleroRestoreMetadata(evt.ObjectNew) && !equality.Semantic.DeepEqual(oldRestore.Status, newRestore.Status) {
			logger.V(1).Info("Accepted Restore Update event due to Status change")
			return true
		}
	}

	logger.V(1).Info("Rejected Restore Update event")
	return false
}

// Delete determines whether to process Velero Restore deletion events
func (p VeleroRestorePredicate) Delete(evt event.DeleteEvent) bool {
	logger := function.GetLogger(context.Background(), evt.Object, "VeleroRestorePredicate")

	namespace := evt.Object.GetNamespace()
	if namespace == p.OADPNamespace {
		if function.CheckVeleroRestoreMetadata(evt.Object) {
			logger.V(1).Info("Accepted Restore Delete event")
			return true
		}
	}

	logger.V(1).Info("Rejected Restore Delete event")
	return false
}

// Generic determines whether to process generic Velero Restore events
func (p VeleroRestorePredicate) Generic(evt event.GenericEvent) bool {
	logger := function.GetLogger(context.Background(), evt.Object, "VeleroRestorePredicate")

	// We don't need to process generic events for Velero Restores
	logger.V(1).Info("Filtering out Velero Restore generic event", "restoreName", evt.Object.GetName())
	return false
}

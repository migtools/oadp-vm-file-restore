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

// NamespacePredicate filters events to only accept objects from a specific namespace
type NamespacePredicate struct {
	Namespace string
}

// Create event filter only accepts objects from the specified namespace
func (p NamespacePredicate) Create(evt event.CreateEvent) bool {
	logger := function.GetLogger(context.Background(), evt.Object, "NamespacePredicate")

	if evt.Object.GetNamespace() == p.Namespace {
		logger.V(1).Info("Accepted Create event from namespace", "namespace", p.Namespace)
		return true
	}

	logger.V(1).Info("Rejected Create event - wrong namespace",
		"objectNamespace", evt.Object.GetNamespace(),
		"expectedNamespace", p.Namespace)
	return false
}

// Update event filter only accepts objects from the specified namespace
func (p NamespacePredicate) Update(evt event.TypedUpdateEvent[client.Object]) bool {
	logger := function.GetLogger(context.Background(), evt.ObjectNew, "NamespacePredicate")

	if evt.ObjectNew.GetNamespace() == p.Namespace {
		logger.V(1).Info("Accepted Update event from namespace", "namespace", p.Namespace)
		return true
	}

	logger.V(1).Info("Rejected Update event - wrong namespace",
		"objectNamespace", evt.ObjectNew.GetNamespace(),
		"expectedNamespace", p.Namespace)
	return false
}

// Delete event filter only accepts objects from the specified namespace
func (p NamespacePredicate) Delete(evt event.DeleteEvent) bool {
	logger := function.GetLogger(context.Background(), evt.Object, "NamespacePredicate")

	if evt.Object.GetNamespace() == p.Namespace {
		logger.V(1).Info("Accepted Delete event from namespace", "namespace", p.Namespace)
		return true
	}

	logger.V(1).Info("Rejected Delete event - wrong namespace",
		"objectNamespace", evt.Object.GetNamespace(),
		"expectedNamespace", p.Namespace)
	return false
}

// Generic determines whether to process generic events from the specified namespace
func (p NamespacePredicate) Generic(evt event.GenericEvent) bool {
	logger := function.GetLogger(context.Background(), evt.Object, "NamespacePredicate")

	if evt.Object.GetNamespace() == p.Namespace {
		logger.V(1).Info("Accepted Generic event from namespace", "namespace", p.Namespace)
		return true
	}

	logger.V(1).Info("Rejected Generic event - wrong namespace",
		"objectNamespace", evt.Object.GetNamespace(),
		"expectedNamespace", p.Namespace)
	return false
}

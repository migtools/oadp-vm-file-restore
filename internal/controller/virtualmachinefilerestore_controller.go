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

// Package controller implements the VirtualMachineFileRestore controller
package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	oadpv1alpha1 "github.com/migtools/oadp-vm-file-restore/api/v1alpha1"
)

// VirtualMachineFileRestoreReconciler reconciles a VirtualMachineFileRestore object
type VirtualMachineFileRestoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// virtualmachinefilerestoreReconcileStepFunction defines the signature for VMFR reconciliation steps
type virtualmachinefilerestoreReconcileStepFunction func(ctx context.Context, logger logr.Logger, vmfr *oadpv1alpha1.VirtualMachineFileRestore) (bool, error)

// +kubebuilder:rbac:groups=oadp.openshift.io,resources=virtualmachinefilerestores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=oadp.openshift.io,resources=virtualmachinefilerestores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=oadp.openshift.io,resources=virtualmachinefilerestores/finalizers,verbs=update
// +kubebuilder:rbac:groups=oadp.openshift.io,resources=virtualmachinebackupsdiscoveries,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *VirtualMachineFileRestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("VirtualMachineFileRestore Reconcile start")

	// Get the VirtualMachineFileRestore object
	vmfr := &oadpv1alpha1.VirtualMachineFileRestore{}
	err := r.Get(ctx, req.NamespacedName, vmfr)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info("VirtualMachineFileRestore not found, skipping reconciliation")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Unable to fetch VirtualMachineFileRestore")
		return ctrl.Result{}, err
	}

	// Determine which path to take based on current state
	var reconcileSteps []virtualmachinefilerestoreReconcileStepFunction

	switch {
	case vmfr.DeletionTimestamp != nil:
		// Deletion path
		logger.V(0).Info("Executing deletion path")
		reconcileSteps = []virtualmachinefilerestoreReconcileStepFunction{
			r.handleResourceDeletion,
		}

	case vmfr.Status.Phase == "":
		// Initial creation path
		logger.V(0).Info("Executing initial creation path")
		reconcileSteps = []virtualmachinefilerestoreReconcileStepFunction{
			r.initializePhaseForFileRestore,
		}

	default:
		// Standard file restore processing path
		logger.V(0).Info("Executing file restore processing path")
		reconcileSteps = []virtualmachinefilerestoreReconcileStepFunction{
			r.validateDiscoveryReference,
			r.validateSelectedBackups,
			r.setupFileServing,
		}
	}

	// Execute the selected reconciliation steps
	for _, step := range reconcileSteps {
		requeue, err := step(ctx, logger, vmfr)
		if err != nil {
			return ctrl.Result{}, err
		} else if requeue {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	logger.V(1).Info("VirtualMachineFileRestore Reconcile exit")
	return ctrl.Result{}, nil
}

// initializePhaseForFileRestore initializes the phase for a new VirtualMachineFileRestore
func (r *VirtualMachineFileRestoreReconciler) initializePhaseForFileRestore(ctx context.Context, logger logr.Logger, vmfr *oadpv1alpha1.VirtualMachineFileRestore) (bool, error) {
	vmfr.Status.Phase = oadpv1alpha1.VirtualMachineFileRestorePhaseNew
	if err := r.Status().Update(ctx, vmfr); err != nil {
		logger.Error(err, "Failed to update status to New phase")
		return false, err
	}
	logger.V(0).Info("VirtualMachineFileRestore phase initialized to New")
	return true, nil // Requeue to proceed to next step
}

// validateDiscoveryReference validates that the referenced VirtualMachineBackupsDiscovery exists and is ready
func (r *VirtualMachineFileRestoreReconciler) validateDiscoveryReference(ctx context.Context, logger logr.Logger, vmfr *oadpv1alpha1.VirtualMachineFileRestore) (bool, error) {
	// Fetch the referenced VirtualMachineBackupsDiscovery from the same namespace
	vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      vmfr.Spec.BackupsDiscoveryRef,
		Namespace: vmfr.Namespace,
	}, vmbd)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Error(err, "Referenced VirtualMachineBackupsDiscovery not found",
				"backupsDiscoveryRef", vmfr.Spec.BackupsDiscoveryRef,
				"namespace", vmfr.Namespace)

			// Set backing off phase and condition
			vmfr.Status.Phase = oadpv1alpha1.VirtualMachineFileRestorePhaseBackingOff
			if err := r.setReadyCondition(ctx, vmfr,
				metav1.ConditionFalse, "DiscoveryNotFound",
				fmt.Sprintf("Referenced VirtualMachineBackupsDiscovery '%s' not found in namespace '%s'",
					vmfr.Spec.BackupsDiscoveryRef, vmfr.Namespace)); err != nil {
				return false, err
			}

			return true, nil // Requeue after delay
		}
		logger.Error(err, "Failed to get referenced VirtualMachineBackupsDiscovery")
		return false, err
	}

	// Check if discovery is complete
	discoveryCompleteCondition := meta.FindStatusCondition(vmbd.Status.Conditions,
		string(oadpv1alpha1.VirtualMachineBackupsDiscoveryConditionComplete))

	if discoveryCompleteCondition == nil || discoveryCompleteCondition.Status != metav1.ConditionTrue {
		logger.V(0).Info("Discovery not yet complete, waiting",
			"discoveryRef", vmfr.Spec.BackupsDiscoveryRef,
			"discoveryNamespace", vmfr.Namespace)

		// Set backing off phase
		vmfr.Status.Phase = oadpv1alpha1.VirtualMachineFileRestorePhaseBackingOff
		if err := r.setReadyCondition(ctx, vmfr,
			metav1.ConditionFalse, "DiscoveryInProgress",
			"Waiting for backup discovery to complete"); err != nil {
			return false, err
		}

		return true, nil // Requeue after delay
	}

	// Check if any valid backups were found
	if len(vmbd.Status.ValidBackups) == 0 {
		logger.V(0).Info("No valid backups found in discovery",
			"discoveryRef", vmfr.Spec.BackupsDiscoveryRef)

		vmfr.Status.Phase = oadpv1alpha1.VirtualMachineFileRestorePhaseBackingOff
		if err := r.setReadyCondition(ctx, vmfr,
			metav1.ConditionFalse, "NoValidBackups",
			"No valid backups found containing the specified virtual machine"); err != nil {
			return false, err
		}

		return false, nil // No requeue, permanent failure
	}

	logger.V(1).Info("Discovery reference validation passed")
	return false, nil // No requeue, proceed to next step
}

// validateSelectedBackups validates that selectedBackups exist in the discovery results
func (r *VirtualMachineFileRestoreReconciler) validateSelectedBackups(ctx context.Context, logger logr.Logger, vmfr *oadpv1alpha1.VirtualMachineFileRestore) (bool, error) {
	// Get the referenced VirtualMachineBackupsDiscovery
	vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      vmfr.Spec.BackupsDiscoveryRef,
		Namespace: vmfr.Namespace,
	}, vmbd)
	if err != nil {
		logger.Error(err, "Failed to get referenced VirtualMachineBackupsDiscovery")
		return false, err
	}

	// Validate selected backups if specified
	backupsToServe, err := r.getBackupsToServe(vmfr, vmbd)
	if err != nil {
		logger.Error(err, "Invalid selected backups")
		vmfr.Status.Phase = oadpv1alpha1.VirtualMachineFileRestorePhaseBackingOff
		if err := r.setReadyCondition(ctx, vmfr,
			metav1.ConditionFalse, "InvalidSelectedBackups",
			err.Error()); err != nil {
			return false, err
		}
		return false, nil // No requeue, permanent failure
	}

	logger.V(1).Info("Selected backups validation passed",
		"validBackups", len(vmbd.Status.ValidBackups),
		"selectedBackups", len(backupsToServe))

	return false, nil // No requeue, proceed to next step
}

// setupFileServing sets up the file serving infrastructure
func (r *VirtualMachineFileRestoreReconciler) setupFileServing(ctx context.Context, logger logr.Logger, vmfr *oadpv1alpha1.VirtualMachineFileRestore) (bool, error) {
	// Get the referenced VirtualMachineBackupsDiscovery
	vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      vmfr.Spec.BackupsDiscoveryRef,
		Namespace: vmfr.Namespace,
	}, vmbd)
	if err != nil {
		logger.Error(err, "Failed to get referenced VirtualMachineBackupsDiscovery")
		return false, err
	}

	// Get backups to serve
	backupsToServe, err := r.getBackupsToServe(vmfr, vmbd)
	if err != nil {
		logger.Error(err, "Failed to determine backups to serve")
		return false, err
	}

	// TODO: Implement file serving logic here
	// File serving infrastructure creation will be implemented in future phases
	vmfr.Status.Phase = oadpv1alpha1.VirtualMachineFileRestorePhaseCreated

	// TODO: Create and populate FileServingInfo based on chosen implementation
	// vmfr.Status.FileServingInfo = &oadpv1alpha1.FileServingInfo{
	//     // Fields TBD based on file serving implementation
	// }

	if err := r.setReadyCondition(ctx, vmfr,
		metav1.ConditionTrue, "FileServingReady",
		fmt.Sprintf("File serving setup completed for %d backups", len(backupsToServe))); err != nil {
		return false, err
	}

	logger.V(0).Info("File restore process completed successfully")
	return false, nil // No requeue, process complete
}

// handleResourceDeletion handles cleanup when the resource is being deleted
func (r *VirtualMachineFileRestoreReconciler) handleResourceDeletion(ctx context.Context, logger logr.Logger, vmfr *oadpv1alpha1.VirtualMachineFileRestore) (bool, error) {
	// Update phase to indicate deletion
	vmfr.Status.Phase = oadpv1alpha1.VirtualMachineFileRestorePhaseDeleting
	if err := r.Status().Update(ctx, vmfr); err != nil {
		logger.Error(err, "Failed to update phase to Deleting")
		return false, err
	}

	// TODO: Implement cleanup logic here
	// - Delete file serving pods
	// - Clean up any other resources

	logger.V(0).Info("Cleanup completed")
	return false, nil // No requeue, cleanup complete
}

// getBackupsToServe validates that selectedBackups exist in the discovery results
// Returns the list of backups to serve (selected or all valid backups)
func (r *VirtualMachineFileRestoreReconciler) getBackupsToServe(vmfr *oadpv1alpha1.VirtualMachineFileRestore, vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery) ([]oadpv1alpha1.VeleroBackupInfo, error) {
	// If no selection specified, use all valid backups
	if len(vmfr.Spec.SelectedBackups) == 0 {
		return vmbd.Status.ValidBackups, nil
	}

	// Create a map of valid backup names for efficient lookup
	validBackupNames := make(map[string]oadpv1alpha1.VeleroBackupInfo)
	for _, backup := range vmbd.Status.ValidBackups {
		validBackupNames[backup.Name] = backup
	}

	// Validate each selected backup and collect the results
	var backupsToServe []oadpv1alpha1.VeleroBackupInfo
	var invalidSelections []string

	for _, selectedName := range vmfr.Spec.SelectedBackups {
		if backup, exists := validBackupNames[selectedName]; exists {
			backupsToServe = append(backupsToServe, backup)
		} else {
			invalidSelections = append(invalidSelections, selectedName)
		}
	}

	// Return error if any selected backups are invalid
	if len(invalidSelections) > 0 {
		return nil, fmt.Errorf("selected backups not found in discovery results: %v", invalidSelections)
	}

	return backupsToServe, nil
}

// setReadyCondition sets the Ready condition on the VirtualMachineFileRestore status
func (r *VirtualMachineFileRestoreReconciler) setReadyCondition(ctx context.Context, vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	status metav1.ConditionStatus, reason, message string) error {

	condition := metav1.Condition{
		Type:               string(oadpv1alpha1.VirtualMachineFileRestoreConditionReady),
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	meta.SetStatusCondition(&vmfr.Status.Conditions, condition)
	return r.Status().Update(ctx, vmfr)
}

// SetupWithManager sets up the controller with the Manager.
func (r *VirtualMachineFileRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&oadpv1alpha1.VirtualMachineFileRestore{}).
		Watches(&oadpv1alpha1.VirtualMachineBackupsDiscovery{}, handler.EnqueueRequestsFromMapFunc(r.mapVMBDToVMFR)).
		Named("virtualmachinefilerestore").
		Complete(r)
}

// mapVMBDToVMFR maps VirtualMachineBackupsDiscovery changes to VirtualMachineFileRestore reconcile requests
func (r *VirtualMachineFileRestoreReconciler) mapVMBDToVMFR(ctx context.Context, obj client.Object) []ctrl.Request {
	// Find all VirtualMachineFileRestore resources that reference this discovery
	vmfrList := &oadpv1alpha1.VirtualMachineFileRestoreList{}
	if err := r.List(ctx, vmfrList); err != nil {
		return nil
	}

	var requests []ctrl.Request
	for _, vmfr := range vmfrList.Items {
		// Check if this VMFR references the updated VMBD (must be in same namespace)
		if vmfr.Spec.BackupsDiscoveryRef == obj.GetName() && vmfr.Namespace == obj.GetNamespace() {
			requests = append(requests, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      vmfr.Name,
					Namespace: vmfr.Namespace,
				},
			})
		}
	}

	return requests
}

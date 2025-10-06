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
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	veleroapi "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	oadpv1alpha1 "github.com/migtools/oadp-vm-file-restore/api/v1alpha1"
	oadptypes "github.com/migtools/oadp-vm-file-restore/api/v1alpha1/types"
	"github.com/migtools/oadp-vm-file-restore/internal/common/constant"
	"github.com/migtools/oadp-vm-file-restore/internal/common/function"
	"github.com/migtools/oadp-vm-file-restore/internal/predicate"
	"github.com/migtools/oadp-vm-file-restore/internal/velerohelpers"
	"sigs.k8s.io/controller-runtime/pkg/builder"
)

// VirtualMachineFileRestoreReconciler reconciles a VirtualMachineFileRestore object
type VirtualMachineFileRestoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// OADPNamespace is the namespace where OADP and Velero backups are located
	OADPNamespace string

	// BackupContentsReader for reading backup contents
	BackupContentsReader velerohelpers.BackupContentsInterface
}

// virtualmachinefilerestoreReconcileStepFunction defines the signature for VMFR reconciliation steps
type virtualmachinefilerestoreReconcileStepFunction func(ctx context.Context, logger logr.Logger, vmfr *oadpv1alpha1.VirtualMachineFileRestore) (bool, error)

// ErrUnsupportedBackup indicates a backup was created with unsupported kubevirt-velero-plugin
type ErrUnsupportedBackup struct {
	BackupName   string
	PVCName      string
	PVCNamespace string
	PVCUID       string
	PVCSize      string
	Reason       string
}

func (e ErrUnsupportedBackup) Error() string {
	return fmt.Sprintf("backup %s created with unsupported kubevirt-velero-plugin", e.BackupName)
}

// +kubebuilder:rbac:groups=oadp.openshift.io,resources=virtualmachinefilerestores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=oadp.openshift.io,resources=virtualmachinefilerestores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=oadp.openshift.io,resources=virtualmachinefilerestores/finalizers,verbs=update
// +kubebuilder:rbac:groups=oadp.openshift.io,resources=virtualmachinebackupsdiscoveries,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=velero.io,resources=restores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=velero.io,resources=backups,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *VirtualMachineFileRestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("VirtualMachineFileRestore Reconcile start")

	// Get the VirtualMachineFileRestore object
	vmfr := &oadpv1alpha1.VirtualMachineFileRestore{}
	err := r.Get(ctx, req.NamespacedName, vmfr)
	if err != nil {
		if apierrors.IsNotFound(err) {
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
		// Deletion path - handle finalizer-based cleanup
		logger.V(0).Info("Executing deletion path")
		reconcileSteps = []virtualmachinefilerestoreReconcileStepFunction{
			// r.handleResourceDeletion,
			// TBD handle resource deletion
		}

	case vmfr.Status.Phase == "":
		// Initial creation path - add finalizer and initialize to New phase
		// Phase -> New
		logger.V(0).Info("Executing initial creation path")
		reconcileSteps = []virtualmachinefilerestoreReconcileStepFunction{
			r.ensureFinalizer,
			r.initializePhaseForFileRestore,
		}

	case vmfr.Status.Phase == oadpv1alpha1.VirtualMachineFileRestorePhaseNew ||
		vmfr.Status.Phase == oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress:
		progressingCondition := meta.FindStatusCondition(vmfr.Status.Conditions, oadptypes.ConditionTypeProgressing)

		if progressingCondition == nil || progressingCondition.Reason != "ValidationCompleted" {
			// Still validating
			// ensure to give reason in logger
			// Remember there may be few conditions in the status conditions array.
			// log all the conditions
			logger.V(0).Info("Status conditions", "conditions", vmfr.Status.Conditions)
			reconcileSteps = []virtualmachinefilerestoreReconcileStepFunction{
				r.validateAndDiscoverPVCs,
			}
		} else {
			// Validation complete — proceed with restore
			// ensure to give reason in logger
			logger.V(0).Info("Executing file restore workflow (validation completed)")
			reconcileSteps = []virtualmachinefilerestoreReconcileStepFunction{
				r.executeFileRestoreWorkflow,
			}
		}

	default:
		// Handle any unexpected phases - should not normally happen
		logger.V(0).Info("Handling unknown phase, defaulting to execution workflow", "phase", vmfr.Status.Phase)
		reconcileSteps = []virtualmachinefilerestoreReconcileStepFunction{
			// TBD execute file restore workflow?
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

// patchVmfrStatusPhaseConditions updates the Status.Phase, Status.ObservedGeneration, and Status.Conditions fields using a patch operation
func (r *VirtualMachineFileRestoreReconciler) patchVmfrStatusPhaseConditions(
	ctx context.Context,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	newPhase oadpv1alpha1.VirtualMachineFileRestorePhase,
	conditions []metav1.Condition, // can be nil or empty
	updateObservedGen bool,
	logger logr.Logger,
) error {
	patch := client.MergeFrom(vmfr.DeepCopy())
	vmfr.Status.Phase = newPhase
	if updateObservedGen {
		vmfr.Status.ObservedGeneration = vmfr.Generation
	}
	originalStatus := vmfr.Status.DeepCopy()

	// Only set conditions if any are provided
	for _, cond := range conditions {
		meta.SetStatusCondition(&vmfr.Status.Conditions, cond)
	}

	// Compare before patching
	if equality.Semantic.DeepEqual(originalStatus, &vmfr.Status) {
		logger.V(1).Info("No status update required", "phase", newPhase)
		return nil
	}

	if err := r.Status().Patch(ctx, vmfr, patch); err != nil {
		logger.Error(err, "Failed to patch status phase", "newPhase", newPhase)
		return err
	}

	logger.V(1).Info("Successfully updated phase", "newPhase", newPhase, "observedGeneration", vmfr.Generation)
	return nil
}

// failValidation sets a phase + condition for a permanent failure and returns it as an error
func (r *VirtualMachineFileRestoreReconciler) failValidation(
	ctx context.Context,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	reason, message string,
	logger logr.Logger,
) error {
	// Set all 4 conditions for Failed phase with semantically appropriate reasons per design doc
	// - Available gets the root cause (reason parameter)
	// - Other conditions get generic/summary reasons
	conditions := []metav1.Condition{
		{
			Type:               oadptypes.ConditionTypeProgressing,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "ValidationFailed",
			Message:            message,
		},
		{
			Type:               oadptypes.ConditionTypeAvailable,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             reason, // Root cause (e.g., "NoValidBackups", "InvalidSelectedBackups")
			Message:            message,
		},
		{
			Type:               oadptypes.ConditionTypeDegraded,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "CriticalFailure",
			Message:            message,
		},
		{
			Type:               oadptypes.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "Failed",
			Message:            message,
		},
	}

	if err := r.patchVmfrStatusPhaseConditions(ctx, vmfr, oadpv1alpha1.VirtualMachineFileRestorePhaseFailed, conditions, false, logger); err != nil {
		return err
	}
	// Return a sentinel error to stop reconciliation
	return fmt.Errorf("validation failed: %s", message)
}

// ensureFinalizer ensures both finalizers are present on the VirtualMachineFileRestore resource.
func (r *VirtualMachineFileRestoreReconciler) ensureFinalizer(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
) (bool, error) {
	// Keep a copy of the original object before mutation
	original := vmfr.DeepCopy()

	// Always attempt to add the required finalizers
	controllerutil.AddFinalizer(vmfr, constant.VeleroRestoreCleanupFinalizer)
	controllerutil.AddFinalizer(vmfr, constant.VMFileRestoreFinalizer)

	// If nothing actually changed, skip patch
	if equality.Semantic.DeepEqual(original.Finalizers, vmfr.Finalizers) {
		return false, nil
	}

	// Patch only the finalizers field
	if err := r.Patch(ctx, vmfr, client.MergeFrom(original)); err != nil {
		logger.Error(err, "Failed to patch finalizers")
		return false, err
	}

	logger.V(0).Info("Finalizers added to VirtualMachineFileRestore")
	return true, nil
}

// initializePhaseForFileRestore initializes the phase for a new VirtualMachineFileRestore
func (r *VirtualMachineFileRestoreReconciler) initializePhaseForFileRestore(ctx context.Context, logger logr.Logger, vmfr *oadpv1alpha1.VirtualMachineFileRestore) (bool, error) {
	now := metav1.Now()
	conditions := []metav1.Condition{
		{
			Type:               oadptypes.ConditionTypeProgressing,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: now,
			Reason:             "Initialized",
			Message:            "File restore request has been accepted",
		},
		{
			Type:               oadptypes.ConditionTypeAvailable,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: now,
			Reason:             "NotStarted",
			Message:            "File serving resources not yet created",
		},
		{
			Type:               oadptypes.ConditionTypeDegraded,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: now,
			Reason:             "NoFailures",
			Message:            "No errors have occurred",
		},
		{
			Type:               oadptypes.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: now,
			Reason:             "NotReady",
			Message:            "File restore has not started processing",
		},
	}

	if err := r.patchVmfrStatusPhaseConditions(ctx, vmfr, oadpv1alpha1.VirtualMachineFileRestorePhaseNew, conditions, true, logger); err != nil {
		return false, err
	}
	logger.V(0).Info("VirtualMachineFileRestore phase initialized to New")
	return true, nil // Requeue to proceed to validation step
}

// validateAndDiscoverPVCs handles the validation phase - discovers and validates backups, populates PVCRestores
func (r *VirtualMachineFileRestoreReconciler) validateAndDiscoverPVCs(ctx context.Context, logger logr.Logger, vmfr *oadpv1alpha1.VirtualMachineFileRestore) (bool, error) {
	// During first run, set Phase -> InProgress with proper conditions
	if vmfr.Status.Phase == oadpv1alpha1.VirtualMachineFileRestorePhaseNew {
		conditions := []metav1.Condition{
			{
				Type:    oadptypes.ConditionTypeProgressing,
				Status:  metav1.ConditionTrue,
				Reason:  "Validating",
				Message: "Validating discovery reference and discovering PVCs from backups",
			},
			{
				Type:    oadptypes.ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "InProgress",
				Message: "Validation in progress",
			},
			{
				Type:    oadptypes.ConditionTypeDegraded,
				Status:  metav1.ConditionFalse,
				Reason:  "NoFailures",
				Message: "No failures detected yet",
			},
			{
				Type:    oadptypes.ConditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  "InProgress",
				Message: "Validation in progress",
			},
		}

		if err := r.patchVmfrStatusPhaseConditions(ctx, vmfr, oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress, conditions, true, logger); err != nil {
			return false, err
		}

		logger.V(0).Info("VirtualMachineFileRestore phase updated to InProgress")
	}

	// Step 1: Validate and get discovery resource
	vmbd, requeue, err := r.validateReferencedDiscovery(ctx, logger, vmfr)
	if err != nil || requeue {
		return requeue, err
	}

	logger.V(1).Info("Discovery reference validated, proceeding to backup selection",
		"validBackups", len(vmbd.Status.ValidBackups),
		"selectedBackupsRequested", len(vmfr.Spec.SelectedBackups))

	// Step 2: Validate backups and selected backups from discovery
	// backupsToServe contains the list of backups which are valid and selected
	backupsToServe, err := r.validateDiscoveryBackups(ctx, logger, vmfr, vmbd)
	if err != nil {
		return false, err
	}

	logger.V(1).Info("Discovery reference and selected backups validation passed",
		"validBackups", len(vmbd.Status.ValidBackups),
		"selectedBackups", len(backupsToServe))

	// Update status - transitioning from validation to PVC discovery
	if err := r.patchVmfrStatusPhaseConditions(ctx, vmfr,
		oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress,
		[]metav1.Condition{{
			Type:               oadptypes.ConditionTypeProgressing,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "DiscoveringPVCs",
			Message:            fmt.Sprintf("Discovering PVCs from %d backups", len(backupsToServe)),
		}},
		false,
		logger); err != nil {
		return false, err
	}

	// Step 3: Discover PVCs from backups
	// Ad this stage we need to get the PVCs from the backups that belongs to the selected
	// discovery VM. This is the last step before we can transition to the Processing phase.
	pvcDiscoveryResults := r.runConcurrentPVCDiscovery(ctx, logger, vmbd, backupsToServe)

	// Step 4: Process PVC discovery results
	logger.V(0).Info("Processing PVC discovery results", "pvcDiscoveryResults", len(pvcDiscoveryResults))

	err = r.processDiscoveryResults(ctx, logger, vmfr, pvcDiscoveryResults)
	if err != nil {
		return false, r.failValidation(ctx, vmfr, "ProcessDiscoveryFailed", err.Error(), logger)
	}

	// All validation/discovery completed successfully - stay in InProgress, update progress reason
	conditions := []metav1.Condition{
		{
			Type:               oadptypes.ConditionTypeProgressing,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "ValidationCompleted",
			Message:            fmt.Sprintf("PVC discovery completed for %d backups, ready to create restores", len(backupsToServe)),
		},
		{
			Type:               oadptypes.ConditionTypeAvailable,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "InProgress",
			Message:            "Validation completed, restore creation pending",
		},
		{
			Type:               oadptypes.ConditionTypeDegraded,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "NoFailures",
			Message:            "No failures detected during validation",
		},
		{
			Type:               oadptypes.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "InProgress",
			Message:            "File restore in progress",
		},
	}

	if err := r.patchVmfrStatusPhaseConditions(ctx, vmfr, oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress, conditions, false, logger); err != nil {
		return false, err
	}
	logger.V(0).Info("Validation phase completed successfully, ready to create Velero restores")
	return true, nil // Requeue to proceed to restore creation phase
}

// validateReferencedDiscovery validates and retrieves the discovery resource
func (r *VirtualMachineFileRestoreReconciler) validateReferencedDiscovery(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
) (*oadpv1alpha1.VirtualMachineBackupsDiscovery, bool, error) {

	vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      vmfr.Spec.BackupsDiscoveryRef,
		Namespace: vmfr.Namespace,
	}, vmbd)
	if err != nil {
		var reason, msg string
		if apierrors.IsNotFound(err) {
			logger.Error(err, "Referenced VirtualMachineBackupsDiscovery not found",
				"backupsDiscoveryRef", vmfr.Spec.BackupsDiscoveryRef,
				"namespace", vmfr.Namespace)
			reason = "DiscoveryNotFound"
			msg = fmt.Sprintf("Referenced VirtualMachineBackupsDiscovery '%s' not found in namespace '%s'",
				vmfr.Spec.BackupsDiscoveryRef, vmfr.Namespace)
		} else {
			logger.Error(err, "Failed to get referenced VirtualMachineBackupsDiscovery",
				"backupsDiscoveryRef", vmfr.Spec.BackupsDiscoveryRef,
				"namespace", vmfr.Namespace)
			reason = "DiscoveryGetFailed"
			msg = fmt.Sprintf("Failed to get referenced VirtualMachineBackupsDiscovery '%s' in namespace '%s'",
				vmfr.Spec.BackupsDiscoveryRef, vmfr.Namespace)
		}

		if failErr := r.failValidation(ctx, vmfr, reason, msg, logger); failErr != nil {
			return nil, false, failErr
		}
		return nil, false, err
	}

	// Check if discovery is ready
	if vmbd.Status.Phase == oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseInProgress ||
		vmbd.Status.Phase == oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseNew {
		logger.V(1).Info("Discovery not yet ready, waiting",
			"discoveryRef", vmfr.Spec.BackupsDiscoveryRef,
			"discoveryNamespace", vmfr.Namespace)

		// Set all 4 conditions for InProgress state while waiting
		conditions := []metav1.Condition{
			{
				Type:               oadptypes.ConditionTypeProgressing,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "WaitingForDiscovery",
				Message:            "Waiting for backup discovery to complete",
			},
			{
				Type:               oadptypes.ConditionTypeAvailable,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "DiscoveryInProgress",
				Message:            "Discovery not yet completed",
			},
			{
				Type:               oadptypes.ConditionTypeDegraded,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "NoFailures",
				Message:            "No failures detected",
			},
			{
				Type:               oadptypes.ConditionTypeReady,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "DiscoveryInProgress",
				Message:            "Waiting for backup discovery to complete",
			},
		}

		if err := r.patchVmfrStatusPhaseConditions(ctx, vmfr,
			oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress,
			conditions,
			false,
			logger); err != nil {
			return nil, false, err
		}

		return nil, true, nil // Requeue, discovery not ready yet
	}

	return vmbd, false, nil // Success
}

func (r *VirtualMachineFileRestoreReconciler) executeFileRestoreWorkflow(ctx context.Context, logger logr.Logger, vmfr *oadpv1alpha1.VirtualMachineFileRestore) (bool, error) {
	return false, nil
}

// validateDiscoveryBackups validates selected backups and returns the list to serve
func (r *VirtualMachineFileRestoreReconciler) validateDiscoveryBackups(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery,
) ([]oadptypes.VeleroBackupInfo, error) {

	// No valid backups in discovery
	if len(vmbd.Status.ValidBackups) == 0 {
		logger.V(0).Info("No valid backups found in discovery", "discoveryRef", vmfr.Spec.BackupsDiscoveryRef)
		return nil, r.failValidation(ctx, vmfr, "NoValidBackups",
			fmt.Sprintf("Referenced discovery '%s' found no valid backups", vmfr.Spec.BackupsDiscoveryRef),
			logger)
	}

	// Filter selected backups if specified
	backupsToServe, err := r.getFilteredBackupsToServe(vmfr, vmbd)
	if err != nil {
		logger.Error(err, "Invalid selected backups")
		return nil, r.failValidation(ctx, vmfr, "InvalidSelectedBackups", err.Error(), logger)
	}

	// User selected backups but none matched discovery results
	if len(vmfr.Spec.SelectedBackups) > 0 && len(backupsToServe) == 0 {
		logger.V(0).Info("Selected backups not found in discovery results", "discoveryRef", vmfr.Spec.BackupsDiscoveryRef)
		return nil, r.failValidation(ctx, vmfr, "InvalidSelectedBackups",
			fmt.Sprintf("None of the selected backups were found in discovery '%s'", vmfr.Spec.BackupsDiscoveryRef),
			logger)
	}

	// After filtering, ensure we have at least one backup to serve
	if len(backupsToServe) == 0 {
		noBackupsMsg := "No valid backups available for file restore after applying filters"
		logger.V(0).Info(noBackupsMsg, "discoveryRef", vmfr.Spec.BackupsDiscoveryRef)
		return nil, r.failValidation(ctx, vmfr, "NoValidBackups",
			noBackupsMsg,
			logger)
	}
	return backupsToServe, nil
}

// getFilteredBackupsToServe validates that selectedBackups exist in the discovery results
// Returns the list of backups to serve (selected or all valid backups)
func (r *VirtualMachineFileRestoreReconciler) getFilteredBackupsToServe(
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery,
) ([]oadptypes.VeleroBackupInfo, error) {

	// If no selection specified, return all valid backups
	if len(vmfr.Spec.SelectedBackups) == 0 {
		return vmbd.Status.ValidBackups, nil
	}

	// Build lookup map of valid backups
	validBackupNames := make(map[string]oadptypes.VeleroBackupInfo, len(vmbd.Status.ValidBackups))
	for _, backup := range vmbd.Status.ValidBackups {
		validBackupNames[backup.Name] = backup
	}

	// Validate each selected backup
	var backupsToServe []oadptypes.VeleroBackupInfo
	var invalidSelections []string
	for _, selectedName := range vmfr.Spec.SelectedBackups {
		if backup, ok := validBackupNames[selectedName]; ok {
			backupsToServe = append(backupsToServe, backup)
		} else {
			invalidSelections = append(invalidSelections, selectedName)
		}
	}

	if len(invalidSelections) > 0 {
		return nil, fmt.Errorf("selected backups not found in discovery results: %v", invalidSelections)
	}

	return backupsToServe, nil
}

// runConcurrentPVCDiscovery discovers PVCs from multiple backups concurrently.
// Returns a slice of BackupDiscoveryProgress containing results from all backups.
// Errors are encoded in the BackupDiscoveryProgress status field, not returned.
func (r *VirtualMachineFileRestoreReconciler) runConcurrentPVCDiscovery(
	ctx context.Context,
	logger logr.Logger,
	vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery,
	backupsToServe []oadptypes.VeleroBackupInfo,
) []oadptypes.BackupDiscoveryProgress {

	if len(backupsToServe) == 0 {
		logger.V(1).Info("No backups to process for PVC discovery")
		return []oadptypes.BackupDiscoveryProgress{}
	}

	logger.V(0).Info("Starting concurrent PVC discovery", "backupCount", len(backupsToServe))

	// Channel to collect results from concurrent goroutines
	resultsChan := make(chan oadptypes.BackupDiscoveryProgress, len(backupsToServe))

	// WaitGroup to ensure all goroutines complete
	var wg sync.WaitGroup

	// Start concurrent PVC discovery for each backup
	for _, backupInfo := range backupsToServe {
		wg.Add(1)
		go r.discoverPVCsFromSingleBackup(ctx, logger, backupInfo, vmbd, resultsChan, &wg)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(resultsChan)

	// Collect results from channel into slice
	results := make([]oadptypes.BackupDiscoveryProgress, 0, len(backupsToServe))
	for result := range resultsChan {
		results = append(results, result)
	}

	logger.Info("Concurrent PVC discovery completed",
		"totalBackups", len(backupsToServe),
		"resultsCollected", len(results))

	return results
}

// discoverPVCsFromSingleBackup discovers PVCs from a single backup in a goroutine
func (r *VirtualMachineFileRestoreReconciler) discoverPVCsFromSingleBackup(
	ctx context.Context,
	logger logr.Logger,
	backupInfo oadptypes.VeleroBackupInfo,
	vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery,
	resultsChan chan<- oadptypes.BackupDiscoveryProgress,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	backupLogger := logger.WithValues("backup", backupInfo.Name)
	backupLogger.V(1).Info("Starting PVC discovery for backup")

	// Step 1: Get backup metadata (creation timestamp)
	progress, err := r.getBackupMetadata(ctx, backupInfo)
	if err != nil {
		// Failed to get backup CRD - permanent failure
		backupLogger.Error(err, "Failed to get backup CRD")
		resultsChan <- r.buildFailedProgress(backupInfo, "BackupNotFound", err.Error())
		return
	}

	// Step 2: Extract PVCs from backup storage
	pvcs, err := r.extractPVCsForGivenVMFromBackup(ctx, backupLogger, backupInfo.Name, backupInfo.Namespace, vmbd.Spec.VirtualMachineName, vmbd.Spec.VirtualMachineNamespace)
	if err != nil {
		backupLogger.Error(err, "Failed to extract PVCs from backup")
		// Failed to extract PVCs - could be missing files, unsupported format, etc.
		var unsupportedErr ErrUnsupportedBackup
		if errors.As(err, &unsupportedErr) {
			// For unsupported backups, we have PVC metadata but the backup is incompatible
			// Include the PVC in results so it appears in status with failure reason
			progress.VeleroBackupInfo.PVCs = []oadptypes.PVCInfo{
				{
					PVCName:      unsupportedErr.PVCName,
					PVCNamespace: unsupportedErr.PVCNamespace,
					PVCUID:       unsupportedErr.PVCUID,
					Size:         unsupportedErr.PVCSize,
				},
			}
			progress.Status = oadptypes.BackupDiscoveryStatusFailed
			progress.Message = fmt.Sprintf("UnsupportedBackupFormat: %s", unsupportedErr.Reason)
			now := metav1.Now()
			progress.LastUpdated = &now
			backupLogger.V(1).Info("Backup incompatible with vmfr, including PVC metadata in results",
				"pvcUID", unsupportedErr.PVCUID, "reason", unsupportedErr.Reason)
			resultsChan <- progress
		} else if strings.Contains(err.Error(), "file not found") {
			resultsChan <- r.buildFailedProgress(backupInfo, "BackupFilesMissing", "Backup files missing from storage")
		} else {
			resultsChan <- r.buildFailedProgress(backupInfo, "ExtractionFailed", err.Error())
		}
		return
	}

	// Step 3: Build success result
	progress.VeleroBackupInfo.PVCs = pvcs
	progress.Status = oadptypes.BackupDiscoveryStatusCompleted
	progress.Message = fmt.Sprintf("Successfully discovered %d PVCs", len(pvcs))
	now := metav1.Now()
	progress.LastUpdated = &now

	backupLogger.V(1).Info("Successfully discovered PVCs from backup", "pvcCount", len(pvcs))
	resultsChan <- progress
}

// getBackupMetadata retrieves Velero backup metadata and initializes BackupDiscoveryProgress
func (r *VirtualMachineFileRestoreReconciler) getBackupMetadata(
	ctx context.Context,
	backupInfo oadptypes.VeleroBackupInfo,
) (oadptypes.BackupDiscoveryProgress, error) {

	veleroBackup, err := r.getVeleroBackup(ctx, backupInfo.Name, backupInfo.Namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return oadptypes.BackupDiscoveryProgress{}, fmt.Errorf("backup CRD object deleted from cluster")
		}
		return oadptypes.BackupDiscoveryProgress{}, fmt.Errorf("failed to get Velero backup metadata: %w", err)
	}

	// Initialize progress with backup metadata
	now := metav1.Now()
	progress := oadptypes.BackupDiscoveryProgress{
		VeleroBackupInfo: oadptypes.VeleroBackupInfo{
			Name:      backupInfo.Name,
			Namespace: backupInfo.Namespace,
			CreatedAt: &veleroBackup.CreationTimestamp,
		},
		Status:      oadptypes.BackupDiscoveryStatusInProgress,
		Message:     "Extracting PVC information",
		LastUpdated: &now,
	}

	return progress, nil
}

// extractPVCsForGivenVMFromBackup extracts PVC information from backup storage
// This reads PVC metadata from the backup tar files, NOT from the live cluster
func (r *VirtualMachineFileRestoreReconciler) extractPVCsForGivenVMFromBackup(
	ctx context.Context,
	logger logr.Logger,
	backupName string,
	backupNamespace string,
	vmName string,
	vmNamespace string,
) ([]oadptypes.PVCInfo, error) {

	if r.BackupContentsReader == nil {
		return nil, fmt.Errorf("backup contents reader not configured")
	}

	// Get the Velero backup object
	backup, err := r.getVeleroBackup(ctx, backupName, backupNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get Velero backup %s: %w", backupName, err)
	}

	// Step 1: Extract VM resource from backup metadata
	logger.V(1).Info("Extracting VM from backup metadata",
		"backupName", backupName, "vmName", vmName, "vmNamespace", vmNamespace)

	vm, err := r.BackupContentsReader.ExtractVMFromBackupMetadata(ctx, backup, vmName, vmNamespace)
	if err != nil {
		logger.Error(err, "Failed to extract VM from backup", "backupName", backupName)
		return nil, fmt.Errorf("failed to extract VM from backup: %w", err)
	}
	if vm == nil {
		logger.V(1).Info("VM not found in backup", "vmName", vmName, "vmNamespace", vmNamespace)
		return []oadptypes.PVCInfo{}, nil
	}

	// Step 2: Extract PVC names from VM volume references
	vmPVCNames := make(map[string]bool)
	if vm.Spec.Template != nil && vm.Spec.Template.Spec.Volumes != nil {
		for _, volume := range vm.Spec.Template.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil {
				vmPVCNames[volume.PersistentVolumeClaim.ClaimName] = true
				logger.V(1).Info("Found VM PVC reference", "pvcName", volume.PersistentVolumeClaim.ClaimName)
			}
			if volume.DataVolume != nil {
				vmPVCNames[volume.DataVolume.Name] = true
				logger.V(1).Info("Found VM DataVolume reference (maps to PVC)", "pvcName", volume.DataVolume.Name)
			}
		}
	}

	if len(vmPVCNames) == 0 {
		logger.V(1).Info("VM has no PVC volumes", "vmName", vmName)
		return []oadptypes.PVCInfo{}, nil
	}

	logger.V(1).Info("Found VM PVC references", "pvcCount", len(vmPVCNames), "vmName", vmName)

	// Step 3: Fetch PVC manifests from backup and extract metadata
	pvcInfos := make([]oadptypes.PVCInfo, 0, len(vmPVCNames))
	for pvcName := range vmPVCNames {
		logger.V(1).Info("Fetching PVC from backup", "pvcName", pvcName)

		// Fetch the full PVC manifest from backup tar
		pvc, err := r.BackupContentsReader.FetchPVCFromBackup(ctx, backup, pvcName, vmNamespace)
		if err != nil {
			logger.Error(err, "Failed to fetch PVC from backup", "pvcName", pvcName, "pvcNamespace", vmNamespace)
			return nil, fmt.Errorf("failed to fetch PVC %s from backup: %w", pvcName, err)
		}

		// Extract size from PVC spec first (we'll need it for error reporting)
		var size string
		if storageRequest, exists := pvc.Spec.Resources.Requests["storage"]; exists {
			size = function.FormatSizeHumanReadable(storageRequest)
		}

		// Validate that PVC has the required UID label added by supported kubevirt-velero-plugin
		// This label is required for VMFR to work correctly with selective restore
		if pvc.Labels == nil {
			logger.Error(nil, "PVC missing labels in backup metadata - backup not compatible with vmfr",
				"pvcName", pvcName, "backupName", backupName)
			return nil, ErrUnsupportedBackup{
				BackupName:   backupName,
				PVCName:      pvc.Name,
				PVCNamespace: pvc.Namespace,
				PVCUID:       string(pvc.UID),
				PVCSize:      size,
				Reason:       "PVC missing labels - backup created with plugin not compatible with vmfr",
			}
		}

		pvcUIDLabel, exists := pvc.Labels[constant.PVCUIDLabel]
		if !exists {
			logger.Error(nil, "PVC missing required UID label in backup metadata - backup not compatible with vmfr",
				"pvcName", pvcName, "backupName", backupName, "requiredLabel", constant.PVCUIDLabel)
			return nil, ErrUnsupportedBackup{
				BackupName:   backupName,
				PVCName:      pvc.Name,
				PVCNamespace: pvc.Namespace,
				PVCUID:       string(pvc.UID),
				PVCSize:      size,
				Reason:       fmt.Sprintf("PVC missing required label '%s' - backup created with plugin not compatible with vmfr", constant.PVCUIDLabel),
			}
		}

		// Verify label value matches actual PVC UID
		if pvcUIDLabel != string(pvc.UID) {
			logger.Error(nil, "PVC UID label mismatch in backup metadata",
				"pvcName", pvcName, "backupName", backupName,
				"labelValue", pvcUIDLabel, "actualUID", string(pvc.UID))
			return nil, ErrUnsupportedBackup{
				BackupName:   backupName,
				PVCName:      pvc.Name,
				PVCNamespace: pvc.Namespace,
				PVCUID:       string(pvc.UID),
				PVCSize:      size,
				Reason:       fmt.Sprintf("PVC UID label mismatch (label: %s, actual: %s) - backup metadata corrupted or created with incompatible plugin", pvcUIDLabel, string(pvc.UID)),
			}
		}

		logger.V(1).Info("PVC validation passed - backup compatible with vmfr",
			"pvcName", pvcName, "pvcUID", string(pvc.UID))

		// Create PVC info with data from backup (size already extracted above)
		pvcInfo := oadptypes.PVCInfo{
			PVCName:      pvc.Name,
			PVCNamespace: pvc.Namespace,
			PVCUID:       string(pvc.UID),
			Size:         size,
		}
		pvcInfos = append(pvcInfos, pvcInfo)

		logger.V(1).Info("Successfully extracted PVC from backup",
			"pvcName", pvc.Name, "pvcUID", string(pvc.UID), "size", size)
	}

	logger.Info("PVC extraction from backup completed",
		"vmName", vmName, "pvcCount", len(pvcInfos), "backupName", backupName)

	return pvcInfos, nil
}

// buildFailedProgress creates a BackupDiscoveryProgress for failed discovery
func (r *VirtualMachineFileRestoreReconciler) buildFailedProgress(
	backupInfo oadptypes.VeleroBackupInfo,
	reason string,
	message string,
) oadptypes.BackupDiscoveryProgress {

	now := metav1.Now()
	return oadptypes.BackupDiscoveryProgress{
		VeleroBackupInfo: oadptypes.VeleroBackupInfo{
			Name:      backupInfo.Name,
			Namespace: backupInfo.Namespace,
			CreatedAt: backupInfo.CreatedAt,
			PVCs:      []oadptypes.PVCInfo{}, // Empty list for failed discoveries
		},
		Status:      oadptypes.BackupDiscoveryStatusFailed,
		Message:     fmt.Sprintf("%s: %s", reason, message),
		LastUpdated: &now,
	}
}

// processDiscoveryResults transforms backup-centric discovery results into PVC-centric restore information
// and stores it in VMFR status for later use in creating Velero Restore objects
func (r *VirtualMachineFileRestoreReconciler) processDiscoveryResults(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	pvcDiscoveryResults []oadptypes.BackupDiscoveryProgress,
) error {
	// Build PVC-grouped restore information from backup-centric discovery results
	// Key: PVC UID (unique identifier across all backups)
	// Value: PVCRestoreInfo with all backups containing this PVC
	pvcMap := make(map[string]*oadpv1alpha1.PVCRestoreInfo)

	for _, backupProgress := range pvcDiscoveryResults {
		// Determine state based on backup discovery status
		var state string
		switch backupProgress.Status {
		case oadptypes.BackupDiscoveryStatusCompleted:
			state = string(oadptypes.BackupDiscoveryStateAvailable)
		case oadptypes.BackupDiscoveryStatusFailed:
			// Parse message to determine specific failure reason
			if strings.Contains(backupProgress.Message, "BackupNotFound") ||
				strings.Contains(backupProgress.Message, "backup CRD object deleted") {
				state = string(oadptypes.BackupDiscoveryStateBackupDeleted)
			} else if strings.Contains(backupProgress.Message, "BackupFilesMissing") ||
				strings.Contains(backupProgress.Message, "Backup files missing") {
				state = string(oadptypes.BackupDiscoveryStateBackupMissing)
			} else if strings.Contains(backupProgress.Message, "UnsupportedBackupFormat") {
				state = string(oadptypes.BackupDiscoveryStateUnsupportedPlugin)
			} else {
				state = string(oadptypes.BackupDiscoveryStateExtractionFailed)
			}
		case oadptypes.BackupDiscoveryStatusSkipped:
			// Skip backups that were skipped during discovery
			logger.V(1).Info("Skipping backup that was skipped during discovery", "backup", backupProgress.Name)
			continue
		default:
			logger.V(1).Info("Unexpected backup status, skipping", "status", backupProgress.Status, "backup", backupProgress.Name)
			continue
		}

		// Process each PVC from this backup
		// For failed backups with no PVC data (backup-deleted, backup-missing, extraction-failed without PVC info),
		// create synthetic entry to track backup-level failures
		if state != string(oadptypes.BackupDiscoveryStateAvailable) && len(backupProgress.VeleroBackupInfo.PVCs) == 0 {
			logger.V(1).Info("Creating synthetic PVC entry for backup-level failure (no PVC data available)",
				"backup", backupProgress.Name, "state", state)

			// Create synthetic PVC UID for backup-level failures
			syntheticUID := fmt.Sprintf("backup-failure-%s-%s", backupProgress.Namespace, backupProgress.Name)

			// Add synthetic PVC entry for backup-level failures
			pvcMap[syntheticUID] = &oadpv1alpha1.PVCRestoreInfo{
				PVCInfo: oadptypes.PVCInfo{
					PVCName:      "backup-level-failure",
					PVCNamespace: backupProgress.Namespace,
					PVCUID:       syntheticUID,
					Size:         "N/A",
				},
				Restores: []oadpv1alpha1.RestoreInfo{
					{
						VeleroBackupName:      backupProgress.Name,
						VeleroBackupNamespace: backupProgress.Namespace,
						Timestamp:             backupProgress.CreatedAt,
						State:                 state,
						FailureReason:         backupProgress.Message,
					},
				},
			}
			continue // Skip to next backup
		}

		// Process PVCs (either successful or failed with PVC metadata)
		for _, pvc := range backupProgress.VeleroBackupInfo.PVCs {
			// Use UID as unique key for grouping PVCs across backups
			pvcKey := pvc.PVCUID

			// Initialize PVC restore info if this is the first time we see this PVC
			if _, exists := pvcMap[pvcKey]; !exists {
				pvcMap[pvcKey] = &oadpv1alpha1.PVCRestoreInfo{
					PVCInfo: oadptypes.PVCInfo{
						PVCName:      pvc.PVCName,
						PVCNamespace: pvc.PVCNamespace,
						PVCUID:       pvc.PVCUID,
						Size:         pvc.Size,
					},
					Restores: []oadpv1alpha1.RestoreInfo{},
				}
			}

			// Add restore information for this backup to the PVC's restore list
			restoreInfo := oadpv1alpha1.RestoreInfo{
				VeleroBackupName:      backupProgress.Name,
				VeleroBackupNamespace: backupProgress.Namespace,
				Timestamp:             backupProgress.CreatedAt,
				State:                 state,
				// VeleroRestore* fields will be populated later when Velero Restore objects are created
			}

			// For failed backups, include the failure reason
			if state != string(oadptypes.BackupDiscoveryStateAvailable) {
				restoreInfo.FailureReason = backupProgress.Message
			}

			pvcMap[pvcKey].Restores = append(pvcMap[pvcKey].Restores, restoreInfo)
		}
	}

	// Convert map to slice
	pvcRestores := make([]oadpv1alpha1.PVCRestoreInfo, 0, len(pvcMap))
	for _, pvcRestore := range pvcMap {
		// Sort restores by timestamp (newest first) for each PVC
		r.sortRestoresByTimestamp(pvcRestore.Restores)
		pvcRestores = append(pvcRestores, *pvcRestore)
	}

	// Use patch pattern (same as patchVmfrStatusPhaseConditions) to update status
	patch := client.MergeFrom(vmfr.DeepCopy())
	originalStatus := vmfr.Status.DeepCopy()

	// Update PVCRestores field
	vmfr.Status.PVCRestores = pvcRestores

	// Compare before patching to avoid unnecessary API calls
	if equality.Semantic.DeepEqual(originalStatus, &vmfr.Status) {
		logger.V(1).Info("No status update required for PVC restores")
		return nil
	}

	// Patch the status
	if err := r.Status().Patch(ctx, vmfr, patch); err != nil {
		logger.Error(err, "Failed to patch status with PVC restore info")
		return fmt.Errorf("failed to update VMFR status with PVC restore info: %w", err)
	}

	logger.V(0).Info("Successfully stored PVC restore information in status",
		"pvcCount", len(pvcRestores),
		"totalBackups", len(pvcDiscoveryResults))

	return nil
}

// sortRestoresByTimestamp sorts restore info by timestamp (newest first)
func (r *VirtualMachineFileRestoreReconciler) sortRestoresByTimestamp(restores []oadpv1alpha1.RestoreInfo) {
	// Sort newest first using simple bubble sort
	for i := 0; i < len(restores); i++ {
		for j := i + 1; j < len(restores); j++ {
			var iTime, jTime time.Time
			if restores[i].Timestamp != nil {
				iTime = restores[i].Timestamp.Time
			}
			if restores[j].Timestamp != nil {
				jTime = restores[j].Timestamp.Time
			}

			// Sort newest first (jTime > iTime means j should come before i)
			if jTime.After(iTime) {
				restores[i], restores[j] = restores[j], restores[i]
			}
		}
	}
}

// getVeleroBackup retrieves a Velero backup object by name
func (r *VirtualMachineFileRestoreReconciler) getVeleroBackup(ctx context.Context, backupName string, backupNamespace string) (*veleroapi.Backup, error) {
	backup := &veleroapi.Backup{}
	namespacedName := types.NamespacedName{
		Name:      backupName,
		Namespace: backupNamespace,
	}

	err := r.Get(ctx, namespacedName, backup)
	if err != nil {
		return nil, fmt.Errorf("failed to get Velero backup %s in namespace %s: %w", backupName, backupNamespace, err)
	}

	return backup, nil
}

// getDiscoveryResource retrieves the referenced VirtualMachineBackupsDiscovery resource
//
//nolint:unused // Will be used in file restore workflow implementation
func (r *VirtualMachineFileRestoreReconciler) getDiscoveryResource(ctx context.Context, vmfr *oadpv1alpha1.VirtualMachineFileRestore) (*oadpv1alpha1.VirtualMachineBackupsDiscovery, error) {
	discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
	discoveryKey := types.NamespacedName{
		Name:      vmfr.Spec.BackupsDiscoveryRef,
		Namespace: vmfr.Namespace,
	}

	err := r.Get(ctx, discoveryKey, discovery)
	if err != nil {
		return nil, err
	}

	return discovery, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VirtualMachineFileRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&oadpv1alpha1.VirtualMachineFileRestore{}).
		Watches(&oadpv1alpha1.VirtualMachineBackupsDiscovery{}, handler.EnqueueRequestsFromMapFunc(r.mapVMBDToVMFR)).
		Watches(&veleroapi.Restore{}, handler.EnqueueRequestsFromMapFunc(r.mapVeleroRestoreToVMFR),
			builder.WithPredicates(predicate.VeleroRestorePredicate{OADPNamespace: r.OADPNamespace})).
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

// mapVeleroRestoreToVMFR maps Velero Restore changes to VirtualMachineFileRestore reconcile requests
func (r *VirtualMachineFileRestoreReconciler) mapVeleroRestoreToVMFR(ctx context.Context, obj client.Object) []ctrl.Request {
	restore, ok := obj.(*veleroapi.Restore)
	if !ok {
		return nil
	}

	// Check if this Velero Restore is managed by a VMFR
	vmfrName, exists := restore.Labels["oadp.openshift.io/vm-file-restore"]
	if !exists {
		return nil
	}

	vmfrNamespace, exists := restore.Labels["oadp.openshift.io/vm-file-restore-ns"]
	if !exists {
		return nil
	}

	// Return a reconcile request for the VMFR that owns this Velero Restore
	return []ctrl.Request{
		{
			NamespacedName: client.ObjectKey{
				Name:      vmfrName,
				Namespace: vmfrNamespace,
			},
		},
	}
}

// ensureRestoreNamespace ensures that the restore namespace exists, either by using the specified one
// or by creating a new temporary namespace with appropriate naming
//
//nolint:unused // Will be used in file restore workflow implementation
func (r *VirtualMachineFileRestoreReconciler) ensureRestoreNamespace(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
) (string, error) {

	// If a specific namespace is provided, validate it exists
	if vmfr.Spec.RestoreNamespace != "" {
		namespace := &corev1.Namespace{}
		err := r.Get(ctx, types.NamespacedName{Name: vmfr.Spec.RestoreNamespace}, namespace)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return "", fmt.Errorf("specified restore namespace '%s' does not exist", vmfr.Spec.RestoreNamespace)
			}
			return "", fmt.Errorf("failed to validate restore namespace '%s': %w", vmfr.Spec.RestoreNamespace, err)
		}
		logger.V(1).Info("Using existing restore namespace", "namespace", vmfr.Spec.RestoreNamespace)
		return vmfr.Spec.RestoreNamespace, nil
	}

	// Generate a temporary namespace name using information from the vmbd discovery object
	// and eventually optional prefix provided by the user in the vmfr spec
	vmbd, err := r.getDiscoveryResource(ctx, vmfr)
	if err != nil {
		return "", fmt.Errorf("failed to get discovery resource for namespace generation: %w", err)
	}

	namespaceName := function.GenerateTemporaryVMFRNamespaceName(
		vmfr.Spec.NamespacePrefix,         // optional prefix
		vmbd.Spec.VirtualMachineNamespace, // VM namespace
		vmbd.Spec.VirtualMachineName,      // VM name
		string(vmfr.UID),                  // UID suffix
		logger,
	)

	// Create the temporary namespace
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
			Labels: map[string]string{
				constant.VMFROriginUUIDLabel:    string(vmfr.UID),
				constant.VMFRTempNamespaceLabel: "true",
				constant.ManagedByLabel:         constant.ManagedByLabelValue,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         vmfr.APIVersion,
					Kind:               vmfr.Kind,
					Name:               vmfr.Name,
					UID:                vmfr.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
	}

	err = r.Create(ctx, namespace)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.V(1).Info("Temporary namespace already exists", "namespace", namespaceName)
		} else {
			return "", fmt.Errorf("failed to create temporary namespace '%s': %w", namespaceName, err)
		}
	} else {
		logger.V(0).Info("Created temporary restore namespace", "namespace", namespaceName)
	}

	return namespaceName, nil
}

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
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
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

		// Check if we've completed validation and are in workflow execution phase
		// Workflow reasons: ValidationCompleted (transition point), NamespaceReady, WaitingForRestores (monitoring), and future workflow steps
		isInWorkflowPhase := progressingCondition != nil &&
			(progressingCondition.Reason == "ValidationCompleted" ||
				progressingCondition.Reason == "NamespaceReady" ||
				progressingCondition.Reason == "WaitingForRestores")

		if isInWorkflowPhase {
			// Validation complete — proceed with restore workflow
			logger.V(0).Info("Executing file restore workflow", "progressingReason", progressingCondition.Reason)
			reconcileSteps = []virtualmachinefilerestoreReconcileStepFunction{
				r.executeFileRestoreWorkflow,
			}
		} else {
			// Still validating or initializing
			logger.V(0).Info("Running validation phase", "conditions", vmfr.Status.Conditions)
			reconcileSteps = []virtualmachinefilerestoreReconcileStepFunction{
				r.validateAndDiscoverPVCs,
			}
		}

	case vmfr.Status.Phase == oadpv1alpha1.VirtualMachineFileRestorePhaseCompleted ||
		vmfr.Status.Phase == oadpv1alpha1.VirtualMachineFileRestorePhaseFailed ||
		vmfr.Status.Phase == oadpv1alpha1.VirtualMachineFileRestorePhasePartiallyFailed:
		// Terminal states - no further action needed, reconciliation stops here
		logger.V(0).Info("Terminal phase reached, no action needed", "phase", vmfr.Status.Phase)
		reconcileSteps = []virtualmachinefilerestoreReconcileStepFunction{}

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
// IMPORTANT: This function patches the ENTIRE status, including any changes already made to vmfr.Status (like PVCRestores)
func (r *VirtualMachineFileRestoreReconciler) patchVmfrStatusPhaseConditions(
	ctx context.Context,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	newPhase oadpv1alpha1.VirtualMachineFileRestorePhase,
	conditions []metav1.Condition, // can be nil or empty
	updateObservedGen bool,
	logger logr.Logger,
) error {
	// IMPORTANT: Create patch BEFORE modifying vmfr
	// This captures the current state as the baseline for comparison
	patch := client.MergeFrom(vmfr.DeepCopy())

	// Now modify vmfr.Status - the patch will include ALL changes to Status
	// including any changes made BEFORE calling this function (e.g., PVCRestores updates)
	vmfr.Status.Phase = newPhase
	if updateObservedGen {
		vmfr.Status.ObservedGeneration = vmfr.Generation
	}

	// Only set conditions if any are provided
	for _, cond := range conditions {
		meta.SetStatusCondition(&vmfr.Status.Conditions, cond)
	}

	// Patch the entire status (includes Phase, Conditions, ObservedGeneration, AND any other modified fields like PVCRestores)
	if err := r.Status().Patch(ctx, vmfr, patch); err != nil {
		logger.Error(err, "Failed to patch status", "newPhase", newPhase)
		return err
	}

	logger.V(1).Info("Successfully updated status", "newPhase", newPhase, "observedGeneration", vmfr.Generation)
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

// failPartialValidation sets a phase + condition for partial failure and returns it as an error
func (r *VirtualMachineFileRestoreReconciler) failPartialValidation(
	ctx context.Context,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	reason, message string,
	logger logr.Logger,
) error {
	// Set all 4 conditions for PartiallyFailed phase
	// Some PVCs are available, but not all
	conditions := []metav1.Condition{
		{
			Type:               oadptypes.ConditionTypeProgressing,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "PartialValidationFailed",
			Message:            message,
		},
		{
			Type:               oadptypes.ConditionTypeAvailable,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             reason, // Root cause (e.g., "PartialAvailability")
			Message:            "Some PVCs are available but not all backups succeeded",
		},
		{
			Type:               oadptypes.ConditionTypeDegraded,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "PartialFailure",
			Message:            message,
		},
		{
			Type:               oadptypes.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "PartiallyFailed",
			Message:            message,
		},
	}

	if err := r.patchVmfrStatusPhaseConditions(ctx, vmfr, oadpv1alpha1.VirtualMachineFileRestorePhasePartiallyFailed, conditions, false, logger); err != nil {
		return err
	}
	// Return a sentinel error to stop reconciliation
	return fmt.Errorf("partial validation failed: %s", message)
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

	// Step 5: Validate PVC availability and determine if we can proceed
	// Rules:
	// 1. If NO PVCs have available backups → Failed
	// 2. If SOME PVCs have non-available restores → PartiallyFailed
	// 3. If ALL PVCs have ONLY available restores → Continue

	totalRealPVCs := 0
	fullyAvailablePVCs := 0
	partiallyAvailablePVCs := 0
	unavailablePVCs := 0

	for _, pvcRestore := range vmfr.Status.PVCRestores {
		// Skip synthetic PVC entries created for backup-level failures
		if pvcRestore.PVCName == constant.BackupLevelFailurePVCName {
			continue
		}

		totalRealPVCs++
		allAvailable := true
		hasAvailable := false

		for _, restore := range pvcRestore.Restores {
			if restore.State == string(oadptypes.BackupDiscoveryStateAvailable) {
				hasAvailable = true
			} else {
				allAvailable = false
			}
		}

		// Categorize this PVC
		if allAvailable && hasAvailable {
			fullyAvailablePVCs++
		} else if hasAvailable {
			partiallyAvailablePVCs++
		} else {
			unavailablePVCs++
		}
	}

	logger.V(0).Info("PVC availability summary",
		"totalPVCs", totalRealPVCs,
		"fullyAvailable", fullyAvailablePVCs,
		"partiallyAvailable", partiallyAvailablePVCs,
		"unavailable", unavailablePVCs)

	// Rule 1: No PVCs have any available backups → Failed
	if fullyAvailablePVCs == 0 && partiallyAvailablePVCs == 0 {
		failureMsg := fmt.Sprintf("No PVCs with available backups found (total: %d, unavailable: %d)",
			totalRealPVCs, unavailablePVCs)
		logger.V(0).Info("PVC availability validation failed - no available backups")
		return false, r.failValidation(ctx, vmfr, "NoAvailableBackups", failureMsg, logger)
	}

	// Rule 2: Some PVCs have non-available restores → PartiallyFailed
	if partiallyAvailablePVCs > 0 || unavailablePVCs > 0 {
		failureMsg := fmt.Sprintf("Cannot proceed: %d PVC(s) have non-available backups (partially available: %d, unavailable: %d)",
			partiallyAvailablePVCs+unavailablePVCs, partiallyAvailablePVCs, unavailablePVCs)
		logger.V(0).Info("PVC availability validation failed - partial availability detected")
		return false, r.failPartialValidation(ctx, vmfr, "PartialAvailability", failureMsg, logger)
	}

	// Rule 3: All PVCs are fully available → Continue
	logger.V(0).Info("PVC availability validation passed - all PVCs fully available", "totalPVCs", fullyAvailablePVCs)

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
	progressingCondition := meta.FindStatusCondition(vmfr.Status.Conditions, oadptypes.ConditionTypeProgressing)
	if progressingCondition == nil {
		return false, fmt.Errorf("progressing condition not found in workflow execution phase")
	}

	logger.V(0).Info("Executing file restore workflow", "currentReason", progressingCondition.Reason)

	// Determine workflow step based on current Progressing reason
	switch progressingCondition.Reason {
	case "ValidationCompleted":
		// Step 1: Create or validate the restore namespace
		// At this point, we know all PVCs have available backups (validated in validateAndDiscoverPVCs)
		restoreNamespace, err := r.ensureRestoreNamespace(ctx, logger, vmfr)
		if err != nil {
			failureMsg := fmt.Sprintf("Failed to create/validate restore namespace: %s", err.Error())
			logger.Error(err, "Namespace creation/validation failed")
			return false, r.failValidation(ctx, vmfr, "NamespaceCreationFailed", failureMsg, logger)
		}

		logger.V(0).Info("Restore namespace ready", "namespace", restoreNamespace)

		// Update status to reflect successful namespace creation
		// IMPORTANT: Change Progressing reason to avoid re-entering validation loop
		conditions := []metav1.Condition{
			{
				Type:               oadptypes.ConditionTypeProgressing,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "NamespaceReady",
				Message:            fmt.Sprintf("Restore namespace '%s' is ready, proceeding with Velero restore creation", restoreNamespace),
			},
			{
				Type:               oadptypes.ConditionTypeAvailable,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "PreparingRestores",
				Message:            "Namespace created, Velero restore creation pending",
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
				Reason:             "InProgress",
				Message:            "File restore workflow in progress",
			},
		}

		if err := r.patchVmfrStatusPhaseConditions(ctx, vmfr, oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress, conditions, false, logger); err != nil {
			return false, err
		}

		logger.V(0).Info("Namespace creation completed", "namespace", restoreNamespace)
		return true, nil // Requeue to proceed to next step

	case "NamespaceReady":
		// Step 2: Create Velero Restores for available backups
		logger.V(0).Info("Creating Velero Restores")

		// Task: Create Velero Restore objects using generateName
		// K8s will assign unique names, then we update PVCRestores status with actual names
		err := r.createVeleroRestores(ctx, logger, vmfr)
		if err != nil {
			failureMsg := fmt.Sprintf("Failed to create Velero Restores: %s", err.Error())
			logger.Error(err, "Velero Restore creation failed")
			return false, r.failValidation(ctx, vmfr, "RestoreCreationFailed", failureMsg, logger)
		}

		// Update workflow conditions to transition to WaitingForRestores state
		conditions := []metav1.Condition{
			{
				Type:               oadptypes.ConditionTypeProgressing,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "WaitingForRestores",
				Message:            "Waiting for Velero Restore(s) to complete",
			},
			{
				Type:               oadptypes.ConditionTypeAvailable,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "RestoresInProgress",
				Message:            "Velero Restores are in progress",
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
				Reason:             "InProgress",
				Message:            "File restore workflow in progress",
			},
		}

		if err := r.patchVmfrStatusPhaseConditions(ctx, vmfr, oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress, conditions, false, logger); err != nil {
			return false, err
		}

		logger.V(0).Info("Velero Restores created, transitioning to WaitingForRestores")
		return true, nil // Requeue - next reconciliation will monitor restores

	case "WaitingForRestores":
		// Step 3: Monitor Velero Restore progress AND validate PVCs
		// Only transition when BOTH Velero Restores are complete AND PVCs exist
		logger.V(0).Info("Monitoring Velero Restore progress and PVC creation")

		// Task: Monitor Velero Restores and update PVCRestores phases
		// monitorVeleroRestores updates phases in-memory and returns statusUpdated flag
		completed, failed, inProgress, statusUpdated, err := r.monitorVeleroRestores(ctx, logger, vmfr)
		if err != nil {
			logger.Error(err, "Failed to monitor Velero Restores")
			return false, err
		}

		totalRestores := completed + failed + inProgress
		logger.V(0).Info("Velero Restore status summary",
			"total", totalRestores,
			"completed", completed,
			"failed", failed,
			"inProgress", inProgress,
			"statusUpdated", statusUpdated)

		// Persist phase updates if any RestoreInfo phases changed
		if statusUpdated {
			// CRITICAL FIX: Use Status().Update() instead of Status().Patch() for nested array fields
			// The issue with Patch() is that client.MergeFrom() creates a baseline from the CURRENT state,
			// which already includes the in-memory modifications. This means the patch sees no diff and
			// doesn't actually update the nested fields in the cluster.
			// Update() bypasses the diff calculation and directly replaces the status in the cluster.
			//
			// NOTE: This may occasionally fail with optimistic concurrency conflicts if the object was
			// modified between when we fetched it and when we try to update. This is expected and
			// controller-runtime will automatically retry the reconciliation.
			if err := r.Status().Update(ctx, vmfr); err != nil {
				// Check if this is an optimistic concurrency conflict
				if apierrors.IsConflict(err) {
					logger.V(1).Info("Optimistic concurrency conflict updating phases, will retry",
						"error", err.Error())
					// Return error to trigger automatic retry by controller-runtime
					return false, err
				}
				logger.Error(err, "Failed to update PVCRestores with updated phases")
				return false, err
			}
			logger.V(1).Info("Successfully updated PVCRestores with Velero Restore phases")
		}

		// If any restores still in progress, requeue periodically to check status
		// The watcher will also trigger reconciliation on Velero Restore phase changes,
		// providing immediate responsiveness. The requeue ensures eventual consistency
		// even if watcher events are missed or timing issues occur.
		if inProgress > 0 {
			logger.V(1).Info("Velero Restores still in progress, requeuing to check status",
				"requeueAfter", "30s")
			return true, nil // Requeue for periodic status checking (watcher provides immediate updates)
		}

		// All Velero Restore CRs have reached terminal state
		// Now check if PVCs have been created by Velero before transitioning
		logger.V(0).Info("All Velero Restore CRs completed, checking PVC creation")

		// Validate PVCs exist for successful restores
		if completed > 0 {
			allPVCsReady, err := r.validateRestoredPVCs(ctx, logger, vmfr)
			if err != nil {
				// Hard error (e.g., PVC in Lost state)
				failureMsg := fmt.Sprintf("PVC validation failed: %s", err.Error())
				logger.Error(err, "PVC validation encountered error")
				return false, r.failValidation(ctx, vmfr, "PVCValidationFailed", failureMsg, logger)
			}

			if !allPVCsReady {
				logger.V(1).Info("Velero Restores completed but PVCs not yet created, waiting")
				// Requeue to wait for Velero to create the PVCs
				return true, nil
			}

			logger.V(0).Info("All PVCs created and ready for mounting")
		}

		// Both Velero Restores AND PVCs are ready - determine final phase
		var conditions []metav1.Condition
		var finalPhase oadpv1alpha1.VirtualMachineFileRestorePhase
		var progressingReason string

		if failed == 0 {
			// All restores completed successfully and PVCs are ready
			logger.V(0).Info("All Velero Restores completed successfully, ready to create file server")
			finalPhase = oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress // Stay in InProgress
			progressingReason = "RestoresCompleted"
			conditions = []metav1.Condition{
				{
					Type:               oadptypes.ConditionTypeProgressing,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             progressingReason,
					Message:            fmt.Sprintf("All %d Velero Restores and PVCs ready, proceeding to file server creation", totalRestores),
				},
				{
					Type:               oadptypes.ConditionTypeAvailable,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Now(),
					Reason:             "FileServerPending",
					Message:            "PVCs restored, file server creation pending",
				},
				{
					Type:               oadptypes.ConditionTypeDegraded,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Now(),
					Reason:             "NoFailures",
					Message:            "All Velero Restores completed without errors",
				},
				{
					Type:               oadptypes.ConditionTypeReady,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Now(),
					Reason:             "InProgress",
					Message:            "File server creation pending",
				},
			}
		} else if completed > 0 {
			// Some restores succeeded, some failed - partial success
			logger.V(0).Info("Velero Restores completed with failures, transitioning to PartiallyFailed phase",
				"succeeded", completed, "failed", failed)
			finalPhase = oadpv1alpha1.VirtualMachineFileRestorePhasePartiallyFailed
			conditions = []metav1.Condition{
				{
					Type:               oadptypes.ConditionTypeProgressing,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Now(),
					Reason:             "RestoresCompletedWithFailures",
					Message:            fmt.Sprintf("Velero Restores completed: %d succeeded, %d failed", completed, failed),
				},
				{
					Type:               oadptypes.ConditionTypeAvailable,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "PartialRestoreSuccess",
					Message:            fmt.Sprintf("Successfully restored %d PVCs, %d failed", completed, failed),
				},
				{
					Type:               oadptypes.ConditionTypeDegraded,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "SomeRestoresFailed",
					Message:            fmt.Sprintf("%d of %d Velero Restores failed", failed, totalRestores),
				},
				{
					Type:               oadptypes.ConditionTypeReady,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "PartiallyFailed",
					Message:            "File restore completed with partial failures",
				},
			}
		} else {
			// All restores failed
			logger.V(0).Info("All Velero Restores failed, transitioning to Failed phase")
			finalPhase = oadpv1alpha1.VirtualMachineFileRestorePhaseFailed
			conditions = []metav1.Condition{
				{
					Type:               oadptypes.ConditionTypeProgressing,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Now(),
					Reason:             "RestoresFailed",
					Message:            fmt.Sprintf("All %d Velero Restores failed", totalRestores),
				},
				{
					Type:               oadptypes.ConditionTypeAvailable,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Now(),
					Reason:             "AllRestoresFailed",
					Message:            "No PVCs were successfully restored",
				},
				{
					Type:               oadptypes.ConditionTypeDegraded,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "CriticalFailure",
					Message:            "All Velero Restores failed",
				},
				{
					Type:               oadptypes.ConditionTypeReady,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Now(),
					Reason:             "Failed",
					Message:            "File restore failed completely",
				},
			}
		}

		// Update workflow conditions
		if err := r.patchVmfrStatusPhaseConditions(ctx, vmfr, finalPhase, conditions, false, logger); err != nil {
			return false, err
		}

		if finalPhase == oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress {
			logger.V(0).Info("Transitioning to file server creation phase")
			return true, nil // Requeue to enter RestoresCompleted case
		}

		logger.V(0).Info("Successfully transitioned to terminal phase", "phase", finalPhase)
		return false, nil // Terminal state - no requeue

	// Step 4: Deploy file server pod (pure action step - no validation)
	// This case is only reached after WaitingForRestores has verified:
	// - All Velero Restore CRs completed successfully
	// - All PVCs exist and are ready for mounting
	case "RestoresCompleted":
		logger.V(0).Info("Creating file server pod and service (PVCs already validated)")

		// Create file server pod and service
		// PVCs are in Pending state - they will bind when the pod is created
		err := r.createFileServerResources(ctx, logger, vmfr)
		if err != nil {
			failureMsg := fmt.Sprintf("Failed to create file server resources: %s", err.Error())
			logger.Error(err, "File server creation failed")
			return false, r.failValidation(ctx, vmfr, "FileServerCreationFailed", failureMsg, logger)
		}

		logger.V(0).Info("File server resources created successfully")

		// Update to final Completed phase with Available status
		conditions := []metav1.Condition{
			{
				Type:               oadptypes.ConditionTypeProgressing,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "FileServerCreated",
				Message:            "File server pod and service created, PVCs binding in progress",
			},
			{
				Type:               oadptypes.ConditionTypeAvailable,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "FileServerAvailable",
				Message:            "File server is accessible and serving files",
			},
			{
				Type:               oadptypes.ConditionTypeDegraded,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "NoFailures",
				Message:            "All operations completed successfully",
			},
			{
				Type:               oadptypes.ConditionTypeReady,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "Completed",
				Message:            "File restore completed, files accessible via file server",
			},
		}

		if err := r.patchVmfrStatusPhaseConditions(ctx, vmfr, oadpv1alpha1.VirtualMachineFileRestorePhaseCompleted, conditions, false, logger); err != nil {
			return false, err
		}

		logger.V(0).Info("Successfully transitioned to Completed phase with file server available")
		return false, nil // Terminal state - no requeue

	default:
		// Unknown workflow state
		logger.V(0).Info("Unknown workflow state, no action taken", "reason", progressingCondition.Reason)
		return false, nil
	}
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
					PVCName:      constant.BackupLevelFailurePVCName,
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
	_, exists := restore.Labels[constant.VMFROriginUUIDLabel]
	if !exists {
		return nil
	}

	// Get VMFR name and namespace from annotations
	vmfrName, nameExists := restore.Annotations[constant.VMFROriginNameAnnotation]
	vmfrNamespace, nsExists := restore.Annotations[constant.VMFROriginNamespaceAnnotation]
	if !nameExists || !nsExists {
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

		// Update VMFR status with the namespace (same as we do for temporary namespaces)
		patch := client.MergeFrom(vmfr.DeepCopy())
		vmfr.Status.CreatedNamespace = vmfr.Spec.RestoreNamespace
		if err := r.Status().Patch(ctx, vmfr, patch); err != nil {
			logger.Error(err, "Failed to update status with restore namespace")
			return "", fmt.Errorf("failed to update status with restore namespace: %w", err)
		}

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

	// Update VMFR status with the created namespace
	patch := client.MergeFrom(vmfr.DeepCopy())
	vmfr.Status.CreatedNamespace = namespaceName
	if err := r.Status().Patch(ctx, vmfr, patch); err != nil {
		logger.Error(err, "Failed to update status with created namespace")
		return "", fmt.Errorf("failed to update status with created namespace: %w", err)
	}

	return namespaceName, nil
}

// createVeleroRestores creates Velero Restore objects using generateName and updates PVCRestores status with actual names.
// Uses Kubernetes generateName to let K8s assign unique names automatically.
func (r *VirtualMachineFileRestoreReconciler) createVeleroRestores(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
) error {

	// Get the restore namespace from status
	restoreNamespace := vmfr.Status.CreatedNamespace
	if restoreNamespace == "" {
		return fmt.Errorf("restore namespace not found in status - namespace creation may have failed")
	}

	// Get VM name and namespace from discovery resource for annotations
	vmbd, err := r.getDiscoveryResource(ctx, vmfr)
	if err != nil {
		return fmt.Errorf("failed to get discovery resource for VM metadata: %w", err)
	}

	logger.V(0).Info("Creating Velero Restores with generateName", "targetNamespace", restoreNamespace)

	// Group PVCs by backup name to create one Restore per backup
	// Map: backup name -> list of PVC UIDs
	backupToPVCUIDs := make(map[string][]string)

	for _, pvcRestore := range vmfr.Status.PVCRestores {
		// Skip synthetic PVC entries (backup-level failures)
		if pvcRestore.PVCName == constant.BackupLevelFailurePVCName {
			continue
		}

		// Process each restore info for this PVC
		for _, restoreInfo := range pvcRestore.Restores {
			// Only process available backups
			if restoreInfo.State != string(oadptypes.BackupDiscoveryStateAvailable) {
				logger.V(1).Info("Skipping non-available restore",
					"pvcUID", pvcRestore.PVCUID,
					"backup", restoreInfo.VeleroBackupName,
					"state", restoreInfo.State)
				continue
			}

			// Add this PVC UID to the backup's list
			backupKey := restoreInfo.VeleroBackupName
			if _, exists := backupToPVCUIDs[backupKey]; !exists {
				backupToPVCUIDs[backupKey] = []string{}
			}
			backupToPVCUIDs[backupKey] = append(backupToPVCUIDs[backupKey], pvcRestore.PVCUID)
		}
	}

	if len(backupToPVCUIDs) == 0 {
		return fmt.Errorf("no available backups found to restore - this should have been caught in validation")
	}

	logger.V(0).Info("Grouped PVCs by backup for Velero Restore creation",
		"backupCount", len(backupToPVCUIDs),
		"targetNamespace", restoreNamespace)

	// Create Velero Restore objects and track created restore names
	// Map: backup name -> actual restore name (assigned by K8s)
	backupToRestoreName := make(map[string]string)
	createdCount := 0

	for backupName, pvcUIDs := range backupToPVCUIDs {
		// Generate restore name prefix (K8s will append random suffix)
		restoreNamePrefix := function.GenerateVeleroRestorePrefix(vmfr.Name, backupName, logger)

		// Build label selector for PVC UIDs
		labelSelector := &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      constant.PVCUIDLabel,
					Operator: metav1.LabelSelectorOpIn,
					Values:   pvcUIDs,
				},
			},
		}

		// Create Velero Restore object with generateName
		restore := &veleroapi.Restore{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: restoreNamePrefix, // K8s will append random suffix
				Namespace:    r.OADPNamespace,
				Labels: map[string]string{
					constant.VMFROriginUUIDLabel: string(vmfr.UID),
				},
				Annotations: map[string]string{
					constant.VMFROriginNameAnnotation:          vmfr.Name,
					constant.VMFROriginNamespaceAnnotation:     vmfr.Namespace,
					constant.VirtualMachineNameAnnotation:      vmbd.Spec.VirtualMachineName,
					constant.VirtualMachineNamespaceAnnotation: vmbd.Spec.VirtualMachineNamespace,
					constant.BackupNameAnnotation:              backupName,
					"oadp.openshift.io/vmfr-restore":           "true",
				},
			},
			Spec: veleroapi.RestoreSpec{
				BackupName:    backupName,
				LabelSelector: labelSelector,
				// Use original VM namespace for filtering backed-up resources
				IncludedNamespaces: []string{vmbd.Spec.VirtualMachineNamespace},
				// Map original namespace to restore namespace
				NamespaceMapping: map[string]string{
					vmbd.Spec.VirtualMachineNamespace: restoreNamespace,
				},
				// Only restore PVCs and VolumeSnapshots
				IncludedResources: []string{
					"persistentvolumeclaims",
					"volumesnapshots",
				},
			},
		}

		// Create the Velero Restore (K8s assigns actual name)
		if err := r.Create(ctx, restore); err != nil {
			logger.Error(err, "Failed to create Velero Restore", "generateNamePrefix", restoreNamePrefix, "backupName", backupName)
			return fmt.Errorf("failed to create Velero Restore with prefix %s: %w", restoreNamePrefix, err)
		}

		// Get the actual name assigned by K8s (available in restore.Name after Create)
		actualRestoreName := restore.Name
		backupToRestoreName[backupName] = actualRestoreName
		createdCount++

		logger.V(0).Info("Created Velero Restore",
			"restoreName", actualRestoreName,
			"backupName", backupName,
			"targetNamespace", restoreNamespace)
	}

	logger.V(0).Info("Velero Restore creation completed",
		"createdCount", createdCount,
		"totalBackups", len(backupToPVCUIDs))

	// Update PVCRestores status with actual Velero Restore names
	patch := client.MergeFrom(vmfr.DeepCopy())

	for i := range vmfr.Status.PVCRestores {
		pvcRestore := &vmfr.Status.PVCRestores[i]

		// Skip synthetic entries
		if pvcRestore.PVCName == constant.BackupLevelFailurePVCName {
			continue
		}

		// Update each RestoreInfo with actual Velero Restore name
		for j := range pvcRestore.Restores {
			restoreInfo := &pvcRestore.Restores[j]

			if restoreInfo.State == string(oadptypes.BackupDiscoveryStateAvailable) {
				// Get actual restore name for this backup
				if actualRestoreName, exists := backupToRestoreName[restoreInfo.VeleroBackupName]; exists {
					restoreInfo.VeleroRestoreName = actualRestoreName
					restoreInfo.VeleroRestoreNamespace = r.OADPNamespace
					restoreInfo.Phase = veleroapi.RestorePhaseNew

					logger.V(1).Info("Updated RestoreInfo with Velero Restore name",
						"pvcUID", pvcRestore.PVCUID,
						"backupName", restoreInfo.VeleroBackupName,
						"restoreName", actualRestoreName)
				}
			}
		}
	}

	// Patch status with updated PVCRestores
	if err := r.Status().Patch(ctx, vmfr, patch); err != nil {
		logger.Error(err, "Failed to patch status with Velero Restore names")
		return fmt.Errorf("failed to update PVCRestores with Velero Restore names: %w", err)
	}

	logger.V(0).Info("Successfully updated PVCRestores with Velero Restore names",
		"restoreCount", len(backupToRestoreName))

	return nil
}

// monitorVeleroRestores monitors all Velero Restore objects associated with this VMFR
// Returns counts: completed, failed, inProgress, statusUpdated (true if any phases changed)
func (r *VirtualMachineFileRestoreReconciler) monitorVeleroRestores(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
) (int, int, int, bool, error) {

	// List all Velero Restores owned by this VMFR using labels
	restoreList := &veleroapi.RestoreList{}
	listOpts := []client.ListOption{
		client.InNamespace(r.OADPNamespace),
		client.MatchingLabels{
			constant.VMFROriginUUIDLabel: string(vmfr.UID),
		},
	}

	if err := r.List(ctx, restoreList, listOpts...); err != nil {
		logger.Error(err, "Failed to list Velero Restores")
		return 0, 0, 0, false, fmt.Errorf("failed to list Velero Restores: %w", err)
	}

	if len(restoreList.Items) == 0 {
		logger.V(1).Info("No Velero Restores found for this VMFR")
		return 0, 0, 0, false, fmt.Errorf("no Velero Restores found - this should not happen")
	}

	logger.V(1).Info("Found Velero Restores", "count", len(restoreList.Items))

	// Track counts
	var completed, failed, inProgress int

	// Update VMFR status with current Velero Restore phases
	statusUpdated := false
	for _, veleroRestore := range restoreList.Items {
		restoreName := veleroRestore.Name
		restorePhase := veleroRestore.Status.Phase

		logger.V(1).Info("Processing Velero Restore",
			"name", restoreName,
			"phase", restorePhase)

		// Categorize by phase
		switch restorePhase {
		case veleroapi.RestorePhaseCompleted:
			completed++
		case veleroapi.RestorePhaseFailed, veleroapi.RestorePhasePartiallyFailed, veleroapi.RestorePhaseFailedValidation:
			failed++
		case veleroapi.RestorePhaseNew, veleroapi.RestorePhaseInProgress, "":
			inProgress++
		default:
			logger.V(1).Info("Unknown Velero Restore phase, treating as in-progress",
				"phase", restorePhase, "restoreName", restoreName)
			inProgress++
		}

		// Get backup name from Velero Restore annotation for matching
		veleroBackupName := veleroRestore.Annotations[constant.BackupNameAnnotation]

		logger.V(1).Info("Matching Velero Restore to PVC RestoreInfo",
			"restoreName", restoreName,
			"backupNameFromAnnotation", veleroBackupName,
			"annotationsPresent", len(veleroRestore.Annotations))

		// Update corresponding RestoreInfo in VMFR status
		for i := range vmfr.Status.PVCRestores {
			pvcRestore := &vmfr.Status.PVCRestores[i]

			// Skip synthetic entries
			if pvcRestore.PVCName == constant.BackupLevelFailurePVCName {
				continue
			}

			// Find and update the RestoreInfo for this Velero Restore
			for j := range pvcRestore.Restores {
				restoreInfo := &pvcRestore.Restores[j]

				logger.V(1).Info("Checking RestoreInfo for match",
					"pvcUID", pvcRestore.PVCUID,
					"restoreInfoBackupName", restoreInfo.VeleroBackupName,
					"veleroBackupName", veleroBackupName,
					"restoreInfoState", restoreInfo.State,
					"restoreInfoRestoreName", restoreInfo.VeleroRestoreName,
					"currentRestoreName", restoreName,
					"match", restoreInfo.VeleroBackupName == veleroBackupName &&
						restoreInfo.State == string(oadptypes.BackupDiscoveryStateAvailable))

				// Match by backup name and restore name (if already populated)
				// This ensures we're updating the correct RestoreInfo entry
				if restoreInfo.VeleroBackupName == veleroBackupName &&
					restoreInfo.State == string(oadptypes.BackupDiscoveryStateAvailable) &&
					(restoreInfo.VeleroRestoreName == "" || restoreInfo.VeleroRestoreName == restoreName) {

					// Update Velero Restore metadata if not already set
					// This handles the case where createVeleroRestoresAndUpdateStatus already set these fields
					if restoreInfo.VeleroRestoreName == "" {
						restoreInfo.VeleroRestoreName = restoreName
						restoreInfo.VeleroRestoreNamespace = r.OADPNamespace
						statusUpdated = true
						logger.V(1).Info("Populated RestoreInfo with Velero Restore metadata",
							"pvcUID", pvcRestore.PVCUID,
							"restoreName", restoreName)
					}

					// Always update phase if it changed (even if metadata was already set)
					if restoreInfo.Phase != restorePhase {
						logger.V(1).Info("Updating RestoreInfo phase",
							"pvcUID", pvcRestore.PVCUID,
							"restoreName", restoreName,
							"oldPhase", restoreInfo.Phase,
							"newPhase", restorePhase)
						restoreInfo.Phase = restorePhase
						statusUpdated = true
					}
				}
			}
		}
	}

	// RestoreInfo phases have been updated in-place on vmfr.Status
	// Return statusUpdated flag so caller knows whether to patch
	if statusUpdated {
		logger.V(1).Info("Updated RestoreInfo phases in-place, caller should patch")
	}

	return completed, failed, inProgress, statusUpdated, nil
}

// validateRestoredPVCs validates that all restored PVCs exist and are in valid states
// Returns true if all PVCs are ready for mounting, false if any are missing
// Note: PVCs can be in Pending state - they will bind when the pod is created
func (r *VirtualMachineFileRestoreReconciler) validateRestoredPVCs(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
) (bool, error) {

	// Get restore namespace where PVCs were created
	restoreNamespace := vmfr.Status.CreatedNamespace
	if restoreNamespace == "" {
		return false, fmt.Errorf("restore namespace not found in status")
	}

	// Extract PVC names from VMFR status (only completed restores)
	pvcMounts := extractPVCMountsFromVMFR(vmfr)
	if len(pvcMounts) == 0 {
		return false, fmt.Errorf("no completed PVC restores found")
	}

	logger.V(1).Info("Validating restored PVCs", "count", len(pvcMounts), "namespace", restoreNamespace)

	// Check each PVC exists and is in a valid state
	allValid := true
	for _, pvcMount := range pvcMounts {
		pvc := &corev1.PersistentVolumeClaim{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      pvcMount.PVCName,
			Namespace: restoreNamespace,
		}, pvc)

		if err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("PVC not yet created by Velero restore",
					"pvcName", pvcMount.PVCName,
					"namespace", restoreNamespace)
				allValid = false
				continue
			}
			return false, fmt.Errorf("failed to get PVC %s: %w", pvcMount.PVCName, err)
		}

		// Check PVC is in a valid state
		// Pending is OK - PVC will bind when the pod is created and scheduled
		// Bound is OK - PVC is already bound (e.g., from previous pod creation attempt)
		// Lost is NOT OK - indicates permanent failure
		switch pvc.Status.Phase {
		case corev1.ClaimPending:
			logger.V(1).Info("PVC is pending (waiting for pod to trigger binding)",
				"pvcName", pvcMount.PVCName,
				"namespace", restoreNamespace)
		case corev1.ClaimBound:
			logger.V(1).Info("PVC is already bound",
				"pvcName", pvcMount.PVCName,
				"volumeName", pvc.Spec.VolumeName,
				"namespace", restoreNamespace)
		case corev1.ClaimLost:
			logger.Error(nil, "PVC is in Lost state - cannot be used",
				"pvcName", pvcMount.PVCName,
				"namespace", restoreNamespace)
			return false, fmt.Errorf("PVC %s is in Lost state", pvcMount.PVCName)
		default:
			logger.V(1).Info("PVC has unknown phase, will attempt to use",
				"pvcName", pvcMount.PVCName,
				"phase", pvc.Status.Phase,
				"namespace", restoreNamespace)
		}

		logger.V(1).Info("PVC validated successfully",
			"pvcName", pvcMount.PVCName,
			"phase", pvc.Status.Phase)
	}

	if allValid {
		logger.V(0).Info("All restored PVCs exist and are ready for mounting", "count", len(pvcMounts))
	} else {
		logger.V(0).Info("Some PVCs are not yet created, will retry", "totalPVCs", len(pvcMounts))
	}

	return allValid, nil
}

// createFileServerResources creates the file server pod and service
func (r *VirtualMachineFileRestoreReconciler) createFileServerResources(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
) error {

	// Get restore namespace
	restoreNamespace := vmfr.Status.CreatedNamespace
	if restoreNamespace == "" {
		return fmt.Errorf("restore namespace not found in status")
	}

	// Extract PVC mount information from VMFR status
	pvcMounts := extractPVCMountsFromVMFR(vmfr)
	if len(pvcMounts) == 0 {
		return fmt.Errorf("no PVC mounts found in status")
	}

	logger.V(0).Info("Creating file server resources",
		"pvcCount", len(pvcMounts),
		"namespace", restoreNamespace)

	// Parse FileAccess spec and build SSH/FileBrowser configurations
	var sshConfig *SSHAccessConfig
	var fileBrowserConfig *FileBrowserAccessConfig

	// Configure SSH access if enabled
	if vmfr.Spec.FileAccess != nil && vmfr.Spec.FileAccess.SSH != nil {
		sshSpec := vmfr.Spec.FileAccess.SSH

		// Determine username (default: constant.DefaultSSHUsername = "oadp")
		username := sshSpec.Username
		if username == "" {
			username = constant.DefaultSSHUsername
		}

		// Use default SSH port
		port := int32(constant.DefaultSSHPort)

		// Handle three credential scenarios:
		// 1. Secret reference provided
		// 2. Inline publicKey provided
		// 3. Neither (generate credentials)
		if sshSpec.CredentialsSecretRef != nil {
			// Scenario 1: Use existing Secret
			secretName := sshSpec.CredentialsSecretRef.Name
			secretNamespace := sshSpec.CredentialsSecretRef.Namespace
			if secretNamespace == "" {
				secretNamespace = vmfr.Namespace
			}

			// Validate that secret exists
			secret := &corev1.Secret{}
			if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: secretNamespace}, secret); err != nil {
				return fmt.Errorf("failed to get SSH credentials secret %s/%s: %w", secretNamespace, secretName, err)
			}

			logger.V(0).Info("Using existing SSH credentials secret", "secretName", secretName, "secretNamespace", secretNamespace)

			sshConfig = &SSHAccessConfig{
				Username:                   username,
				PublicKey:                  "", // Will be read from secret
				CredentialsSecretName:      secretName,
				CredentialsSecretNamespace: secretNamespace,
				Port:                       port,
			}
		} else if sshSpec.PublicKey != "" {
			// Scenario 2: Inline publicKey
			logger.V(0).Info("Using inline SSH public key", "username", username)

			sshConfig = &SSHAccessConfig{
				Username:  username,
				PublicKey: sshSpec.PublicKey,
				Port:      port,
			}
		} else {
			// Scenario 3: Generate SSH keypair and create secret
			logger.V(0).Info("Generating SSH keypair (no publicKey or secret provided)", "username", username)

			keyPair, err := function.GenerateSSHKeyPair(logger)
			if err != nil {
				return fmt.Errorf("failed to generate SSH keypair: %w", err)
			}

			// Create secret with generated credentials in restore namespace
			secretName := fmt.Sprintf("%s-ssh-creds", vmfr.Name)
			secret := function.CreateSSHCredentialsSecret(
				secretName,
				restoreNamespace,
				username,
				keyPair,
				vmfr.Name,
				vmfr.Namespace,
				vmfr.UID,
				logger,
			)

			if err := r.Create(ctx, secret); err != nil && !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create SSH credentials secret: %w", err)
			}

			logger.V(0).Info("Created SSH credentials secret", "secretName", secretName, "namespace", restoreNamespace)

			sshConfig = &SSHAccessConfig{
				Username:                   username,
				PublicKey:                  "", // Will be read from secret
				CredentialsSecretName:      secretName,
				CredentialsSecretNamespace: restoreNamespace,
				Port:                       port,
			}
		}
	}

	// Configure FileBrowser access if enabled
	if vmfr.Spec.FileAccess != nil && vmfr.Spec.FileAccess.FileBrowser != nil {
		fbSpec := vmfr.Spec.FileAccess.FileBrowser

		// Use default FileBrowser port
		port := int32(constant.DefaultFileBrowserPort)

		// Handle credentials: either from secret or generated
		if fbSpec.CredentialsSecretRef != nil {
			// Use existing Secret
			secretName := fbSpec.CredentialsSecretRef.Name
			secretNamespace := fbSpec.CredentialsSecretRef.Namespace
			if secretNamespace == "" {
				secretNamespace = vmfr.Namespace
			}

			// Validate that secret exists
			secret := &corev1.Secret{}
			if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: secretNamespace}, secret); err != nil {
				return fmt.Errorf("failed to get FileBrowser credentials secret %s/%s: %w", secretNamespace, secretName, err)
			}

			logger.V(0).Info("Using existing FileBrowser credentials secret", "secretName", secretName, "secretNamespace", secretNamespace)

			fileBrowserConfig = &FileBrowserAccessConfig{
				CredentialsSecretName:      secretName,
				CredentialsSecretNamespace: secretNamespace,
				Port:                       port,
			}
		} else {
			// Generate FileBrowser credentials and create secret
			logger.V(0).Info("Generating FileBrowser credentials (no secret provided)")

			credentials, err := function.GenerateFileBrowserCredentials("", logger) // Use default username
			if err != nil {
				return fmt.Errorf("failed to generate FileBrowser credentials: %w", err)
			}

			// Create secret with generated credentials in restore namespace
			secretName := fmt.Sprintf("%s-filebrowser-creds", vmfr.Name)
			secret := function.CreateFileBrowserCredentialsSecret(
				secretName,
				restoreNamespace,
				credentials,
				vmfr.Name,
				vmfr.Namespace,
				vmfr.UID,
				logger,
			)

			if err := r.Create(ctx, secret); err != nil && !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create FileBrowser credentials secret: %w", err)
			}

			logger.V(0).Info("Created FileBrowser credentials secret", "secretName", secretName, "namespace", restoreNamespace, "username", credentials.Username)

			fileBrowserConfig = &FileBrowserAccessConfig{
				CredentialsSecretName:      secretName,
				CredentialsSecretNamespace: restoreNamespace,
				Port:                       port,
			}
		}
	}

	// Build pod configuration with parsed access methods
	podConfig := FileServerPodConfig{
		PodName:              fmt.Sprintf("%s-fileserver", vmfr.Name),
		PodNamespace:         restoreNamespace,
		VMFRName:             vmfr.Name,
		VMFRNamespace:        vmfr.Namespace,
		VMFRUID:              string(vmfr.UID),
		PVCMounts:            pvcMounts,
		MainContainer:        ptr.To(buildVMFileServerMainContainer()), // Use VM file server for disk mounting
		SSHAccess:            sshConfig,                                // Configured SSH access (or nil)
		FileBrowserAccess:    fileBrowserConfig,                        // Configured FileBrowser access (or nil)
		EnableDualPathAccess: true,                                     // Enable dual-path symlinks
		UseInternalMounts:    false,                                    // Use Kubernetes-managed PVC mounts
	}

	logger.V(0).Info("File server configuration prepared",
		"sshEnabled", sshConfig != nil,
		"fileBrowserEnabled", fileBrowserConfig != nil)

	// Build deployment spec (uses VM file server container)
	deployment, err := buildFileServerDeployment(podConfig)
	if err != nil {
		return fmt.Errorf("failed to build deployment spec: %w", err)
	}

	// Create the deployment
	err = r.Create(ctx, deployment)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.V(1).Info("File server deployment already exists", "deploymentName", deployment.Name)
		} else {
			return fmt.Errorf("failed to create file server deployment: %w", err)
		}
	} else {
		logger.V(0).Info("Created file server deployment", "deploymentName", deployment.Name, "namespace", deployment.Namespace)
	}

	// Build service configuration
	// Gather service ports based on enabled access methods
	servicePorts := gatherServicePorts(podConfig.SSHAccess, podConfig.FileBrowserAccess)

	serviceConfig := ServiceConfig{
		ServiceName:      fmt.Sprintf("%s-fileserver-svc", vmfr.Name),
		ServiceNamespace: restoreNamespace,
		VMFRName:         vmfr.Name,
		VMFRNamespace:    vmfr.Namespace,
		VMFRUID:          string(vmfr.UID),
		Ports:            servicePorts,
		ServiceType:      corev1.ServiceTypeClusterIP,
	}

	// Build service spec
	service, err := buildFileServerService(serviceConfig)
	if err != nil {
		return fmt.Errorf("failed to build service spec: %w", err)
	}

	// Create the service
	err = r.Create(ctx, service)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.V(1).Info("File server service already exists", "serviceName", service.Name)
		} else {
			return fmt.Errorf("failed to create file server service: %w", err)
		}
	} else {
		logger.V(0).Info("Created file server service",
			"serviceName", service.Name,
			"namespace", service.Namespace,
			"port", servicePorts[0].Port)
	}

	logger.V(0).Info("File server resources created successfully",
		"deploymentName", deployment.Name,
		"serviceName", service.Name,
		"namespace", restoreNamespace)

	return nil
}

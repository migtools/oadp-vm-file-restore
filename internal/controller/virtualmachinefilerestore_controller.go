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
	routev1 "github.com/openshift/api/route/v1"
	veleroapi "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	veleroapiv2alpha1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=velero.io,resources=restores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=velero.io,resources=backups,verbs=get;list;watch
// +kubebuilder:rbac:groups=velero.io,resources=downloadrequests,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=velero.io,resources=datadownloads,verbs=get;list;watch;patch

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
			r.handleVeleroRestoreCleanup,
			r.handleResourceCleanup,
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
		// Workflow reasons: ValidationCompleted (transition point), NamespaceReady, WaitingForRestores (monitoring), RestoresCompleted (credentials), CredentialsReady (file server creation), and future workflow steps
		isInWorkflowPhase := progressingCondition != nil &&
			(progressingCondition.Reason == oadptypes.ReasonValidationCompleted ||
				progressingCondition.Reason == oadptypes.ReasonNamespaceReady ||
				progressingCondition.Reason == oadptypes.ReasonWaitingForRestores ||
				progressingCondition.Reason == oadptypes.ReasonRestoresCompleted ||
				progressingCondition.Reason == oadptypes.ReasonCredentialsReady)

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
			Reason:             oadptypes.ReasonValidationFailed,
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
			Reason:             oadptypes.ReasonCriticalFailure,
			Message:            message,
		},
		{
			Type:               oadptypes.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             oadptypes.ReasonFailed,
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
			Reason:             oadptypes.ReasonPartialValidationFailed,
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
			Reason:             oadptypes.ReasonPartialFailure,
			Message:            message,
		},
		{
			Type:               oadptypes.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             oadptypes.ReasonPartiallyFailed,
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
			Reason:             oadptypes.ReasonInitialized,
			Message:            "File restore request has been accepted",
		},
		{
			Type:               oadptypes.ConditionTypeAvailable,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: now,
			Reason:             oadptypes.ReasonNotStarted,
			Message:            "File serving resources not yet created",
		},
		{
			Type:               oadptypes.ConditionTypeDegraded,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: now,
			Reason:             oadptypes.ReasonNoFailures,
			Message:            "No errors have occurred",
		},
		{
			Type:               oadptypes.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: now,
			Reason:             oadptypes.ReasonNotReady,
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
				Reason:  oadptypes.ReasonValidating,
				Message: "Validating discovery reference and discovering PVCs from backups",
			},
			{
				Type:    oadptypes.ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  oadptypes.ReasonInProgress,
				Message: "Validation in progress",
			},
			{
				Type:    oadptypes.ConditionTypeDegraded,
				Status:  metav1.ConditionFalse,
				Reason:  oadptypes.ReasonNoFailures,
				Message: "No failures detected yet",
			},
			{
				Type:    oadptypes.ConditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  oadptypes.ReasonInProgress,
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
			Reason:             oadptypes.ReasonDiscoveringPVCs,
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
			Reason:             oadptypes.ReasonValidationCompleted,
			Message:            fmt.Sprintf("PVC discovery completed for %d backups, ready to create restores", len(backupsToServe)),
		},
		{
			Type:               oadptypes.ConditionTypeAvailable,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             oadptypes.ReasonInProgress,
			Message:            "Validation completed, restore creation pending",
		},
		{
			Type:               oadptypes.ConditionTypeDegraded,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             oadptypes.ReasonNoFailures,
			Message:            "No failures detected during validation",
		},
		{
			Type:               oadptypes.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             oadptypes.ReasonInProgress,
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
				Reason:             oadptypes.ReasonWaitingForDiscovery,
				Message:            "Waiting for backup discovery to complete",
			},
			{
				Type:               oadptypes.ConditionTypeAvailable,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonDiscoveryInProgress,
				Message:            "Discovery not yet completed",
			},
			{
				Type:               oadptypes.ConditionTypeDegraded,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonNoFailures,
				Message:            "No failures detected",
			},
			{
				Type:               oadptypes.ConditionTypeReady,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonDiscoveryInProgress,
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

//nolint:gocyclo // TODO: Refactor this function to reduce complexity
func (r *VirtualMachineFileRestoreReconciler) executeFileRestoreWorkflow(ctx context.Context, logger logr.Logger, vmfr *oadpv1alpha1.VirtualMachineFileRestore) (bool, error) {
	progressingCondition := meta.FindStatusCondition(vmfr.Status.Conditions, oadptypes.ConditionTypeProgressing)
	if progressingCondition == nil {
		return false, fmt.Errorf("progressing condition not found in workflow execution phase")
	}

	logger.V(0).Info("Executing file restore workflow", "currentReason", progressingCondition.Reason)

	// Determine workflow step based on current Progressing reason
	switch progressingCondition.Reason {
	case oadptypes.ReasonValidationCompleted:
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
				Reason:             oadptypes.ReasonNamespaceReady,
				Message:            fmt.Sprintf("Restore namespace '%s' is ready, proceeding with Velero restore creation", restoreNamespace),
			},
			{
				Type:               oadptypes.ConditionTypeAvailable,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonPreparingRestores,
				Message:            "Namespace created, Velero restore creation pending",
			},
			{
				Type:               oadptypes.ConditionTypeDegraded,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonNoFailures,
				Message:            "No failures detected",
			},
			{
				Type:               oadptypes.ConditionTypeReady,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonInProgress,
				Message:            "File restore workflow in progress",
			},
		}

		if err := r.patchVmfrStatusPhaseConditions(ctx, vmfr, oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress, conditions, false, logger); err != nil {
			return false, err
		}

		logger.V(0).Info("Namespace creation completed", "namespace", restoreNamespace)
		return true, nil // Requeue to proceed to next step

	case oadptypes.ReasonNamespaceReady:
		// Step 2: Create Velero Restores for available backups
		logger.V(0).Info("Preparing to create Velero Restores")

		// IDEMPOTENCY CHECK: Check if Velero Restores have already been created
		// by examining if VeleroRestoreName is populated in status for all available backups
		alreadyCreated := r.checkVeleroRestoresAlreadyCreated(vmfr)

		if alreadyCreated {
			logger.V(0).Info("Velero Restores already created, skipping creation step")
		} else {
			// Task: Create Velero Restore objects using generateName
			// K8s will assign unique names, then we update PVCRestores status with actual names
			logger.V(0).Info("Creating Velero Restores")
			err := r.createVeleroRestores(ctx, logger, vmfr)
			if err != nil {
				failureMsg := fmt.Sprintf("Failed to create Velero Restores: %s", err.Error())
				logger.Error(err, "Velero Restore creation failed")
				return false, r.failValidation(ctx, vmfr, "RestoreCreationFailed", failureMsg, logger)
			}
		}

		// Update workflow conditions to transition to WaitingForRestores state
		conditions := []metav1.Condition{
			{
				Type:               oadptypes.ConditionTypeProgressing,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonWaitingForRestores,
				Message:            "Waiting for Velero Restore(s) to complete",
			},
			{
				Type:               oadptypes.ConditionTypeAvailable,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonRestoresInProgress,
				Message:            "Velero Restores are in progress",
			},
			{
				Type:               oadptypes.ConditionTypeDegraded,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonNoFailures,
				Message:            "No failures detected",
			},
			{
				Type:               oadptypes.ConditionTypeReady,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonInProgress,
				Message:            "File restore workflow in progress",
			},
		}

		if err := r.patchVmfrStatusPhaseConditions(ctx, vmfr, oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress, conditions, false, logger); err != nil {
			return false, err
		}

		logger.V(0).Info("Velero Restores created, transitioning to WaitingForRestores")
		return true, nil // Requeue - next reconciliation will monitor restores

	case oadptypes.ReasonWaitingForRestores:
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

		// Fix DataDownload PVC name mismatch (if any DataDownloads exist)
		// This automatically patches DataDownload resources with correct PVC names
		fixedCount, err := r.fixDataDownloadPVCNames(ctx, logger, vmfr)
		if err != nil {
			logger.Error(err, "Failed to fix DataDownload PVC names")
			// Don't fail the reconciliation - log and continue
			// The DataDownload will eventually timeout and we can retry
		} else if fixedCount > 0 {
			logger.V(0).Info("Fixed DataDownload PVC name mismatches", "count", fixedCount)
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

		// Defensive check: ensure we have at least one restore
		// This should never happen (validation ensures PVCs exist), but check defensively
		if totalRestores == 0 {
			return false, fmt.Errorf("no Velero Restores found - this indicates a serious workflow bug")
		}

		if failed == 0 && completed > 0 {
			// All restores completed successfully and PVCs are ready
			logger.V(0).Info("All Velero Restores completed successfully, ready to create file server")
			finalPhase = oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress // Stay in InProgress
			progressingReason = oadptypes.ReasonRestoresCompleted
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
					Reason:             oadptypes.ReasonFileServerPending,
					Message:            "PVCs restored, file server creation pending",
				},
				{
					Type:               oadptypes.ConditionTypeDegraded,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Now(),
					Reason:             oadptypes.ReasonNoFailures,
					Message:            "All Velero Restores completed without errors",
				},
				{
					Type:               oadptypes.ConditionTypeReady,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Now(),
					Reason:             oadptypes.ReasonInProgress,
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
					Reason:             oadptypes.ReasonRestoresCompletedWithFailures,
					Message:            fmt.Sprintf("Velero Restores completed: %d succeeded, %d failed", completed, failed),
				},
				{
					Type:               oadptypes.ConditionTypeAvailable,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             oadptypes.ReasonPartialRestoreSuccess,
					Message:            fmt.Sprintf("Successfully restored %d PVCs, %d failed", completed, failed),
				},
				{
					Type:               oadptypes.ConditionTypeDegraded,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             oadptypes.ReasonSomeRestoresFailed,
					Message:            fmt.Sprintf("%d of %d Velero Restores failed", failed, totalRestores),
				},
				{
					Type:               oadptypes.ConditionTypeReady,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             oadptypes.ReasonPartiallyFailed,
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
					Reason:             oadptypes.ReasonRestoresFailed,
					Message:            fmt.Sprintf("All %d Velero Restores failed", totalRestores),
				},
				{
					Type:               oadptypes.ConditionTypeAvailable,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Now(),
					Reason:             oadptypes.ReasonAllRestoresFailed,
					Message:            "No PVCs were successfully restored",
				},
				{
					Type:               oadptypes.ConditionTypeDegraded,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             oadptypes.ReasonCriticalFailure,
					Message:            "All Velero Restores failed",
				},
				{
					Type:               oadptypes.ConditionTypeReady,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Now(),
					Reason:             oadptypes.ReasonFailed,
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

	// Step 4: Prepare credentials for file server access
	// This case is reached after WaitingForRestores has verified:
	// - All Velero Restore CRs completed successfully
	// - All PVCs exist and are ready for mounting
	case oadptypes.ReasonRestoresCompleted:
		logger.V(0).Info("Preparing credentials for file server (PVCs already validated)")

		// Ensure credentials are validated/copied/generated as needed
		// This handles SSH and FileBrowser credentials based on user configuration
		err := r.ensureCredentials(ctx, logger, vmfr)
		if err != nil {
			failureMsg := fmt.Sprintf("Failed to prepare credentials: %s", err.Error())
			logger.Error(err, "Credentials preparation failed")
			return false, r.failValidation(ctx, vmfr, "CredentialsPreparationFailed", failureMsg, logger)
		}

		logger.V(0).Info("Credentials prepared successfully, ready to create file server")

		// Update workflow to indicate credentials are ready
		conditions := []metav1.Condition{
			{
				Type:               oadptypes.ConditionTypeProgressing,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonCredentialsReady,
				Message:            "Credentials validated and ready, proceeding to file server creation",
			},
			{
				Type:               oadptypes.ConditionTypeAvailable,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonFileServerPending,
				Message:            "Credentials ready, file server creation pending",
			},
			{
				Type:               oadptypes.ConditionTypeDegraded,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonNoFailures,
				Message:            "No failures detected",
			},
			{
				Type:               oadptypes.ConditionTypeReady,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonInProgress,
				Message:            "File server creation pending",
			},
		}

		if err := r.patchVmfrStatusPhaseConditions(ctx, vmfr, oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress, conditions, false, logger); err != nil {
			return false, err
		}

		logger.V(0).Info("Credentials preparation completed, transitioning to file server creation")
		return true, nil // Requeue to proceed to CredentialsReady case

	// Step 5: Deploy file server pod with validated credentials
	case oadptypes.ReasonCredentialsReady:
		logger.V(0).Info("Creating file server pod and service with validated credentials")

		// Create file server pod and service
		// PVCs are in Pending state - they will bind when the pod is created
		// Credentials have been validated/copied/generated in previous step
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
				Reason:             oadptypes.ReasonFileServerCreated,
				Message:            "File server pod and service created, PVCs binding in progress",
			},
			{
				Type:               oadptypes.ConditionTypeAvailable,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonFileServerAvailable,
				Message:            "File server is accessible and serving files",
			},
			{
				Type:               oadptypes.ConditionTypeDegraded,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonNoFailures,
				Message:            "All operations completed successfully",
			},
			{
				Type:               oadptypes.ConditionTypeReady,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             oadptypes.ReasonCompleted,
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
	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&oadpv1alpha1.VirtualMachineFileRestore{}).
		Watches(&oadpv1alpha1.VirtualMachineBackupsDiscovery{}, handler.EnqueueRequestsFromMapFunc(r.mapVMBDToVMFR)).
		Watches(&veleroapi.Restore{}, handler.EnqueueRequestsFromMapFunc(r.mapVeleroRestoreToVMFR),
			builder.WithPredicates(predicate.VeleroRestorePredicate{OADPNamespace: r.OADPNamespace})).
		Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(r.mapServiceToVMFR),
			builder.WithPredicates(predicate.ServicePredicate{}))

	// Watch Routes if the Route CRD is available (OpenShift clusters)
	// On non-OpenShift clusters, this watch will be skipped
	if r.isOpenShiftCluster(mgr) {
		controllerBuilder = controllerBuilder.Watches(
			&routev1.Route{},
			handler.EnqueueRequestsFromMapFunc(r.mapRouteToVMFR),
			builder.WithPredicates(predicate.RoutePredicate{}))
	}

	return controllerBuilder.
		Named("virtualmachinefilerestore").
		Complete(r)
}

// isOpenShiftCluster checks if the cluster has OpenShift Route CRD available
func (r *VirtualMachineFileRestoreReconciler) isOpenShiftCluster(mgr ctrl.Manager) bool {
	// Check if Route CRD exists by attempting to list it
	routeList := &routev1.RouteList{}
	err := mgr.GetAPIReader().List(context.Background(), routeList, client.Limit(1))
	return err == nil
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

// mapServiceToVMFR maps Service changes to VirtualMachineFileRestore reconcile requests
func (r *VirtualMachineFileRestoreReconciler) mapServiceToVMFR(ctx context.Context, obj client.Object) []ctrl.Request {
	service, ok := obj.(*corev1.Service)
	if !ok {
		return nil
	}

	// Check if this Service is managed by a VMFR
	_, exists := service.Labels[constant.VMFROriginUUIDLabel]
	if !exists {
		return nil
	}

	// Get VMFR name and namespace from annotations
	vmfrName, nameExists := service.Annotations[constant.VMFROriginNameAnnotation]
	vmfrNamespace, nsExists := service.Annotations[constant.VMFROriginNamespaceAnnotation]
	if !nameExists || !nsExists {
		return nil
	}

	// Return a reconcile request for the VMFR that owns this Service
	return []ctrl.Request{
		{
			NamespacedName: client.ObjectKey{
				Name:      vmfrName,
				Namespace: vmfrNamespace,
			},
		},
	}
}

// mapRouteToVMFR maps Route changes to VirtualMachineFileRestore reconcile requests
func (r *VirtualMachineFileRestoreReconciler) mapRouteToVMFR(ctx context.Context, obj client.Object) []ctrl.Request {
	// Check if this Route is managed by a VMFR
	_, exists := obj.GetLabels()[constant.VMFROriginUUIDLabel]
	if !exists {
		return nil
	}

	// Get VMFR name and namespace from annotations
	vmfrName, nameExists := obj.GetAnnotations()[constant.VMFROriginNameAnnotation]
	vmfrNamespace, nsExists := obj.GetAnnotations()[constant.VMFROriginNamespaceAnnotation]
	if !nameExists || !nsExists {
		return nil
	}

	// Return a reconcile request for the VMFR that owns this Route
	return []ctrl.Request{
		{
			NamespacedName: client.ObjectKey{
				Name:      vmfrName,
				Namespace: vmfrNamespace,
			},
		},
	}
}

// updateFileServingInfo updates the FileServingInfo in the VMFR status with access URLs and credentials.
// This function uses the created Service and discovers Routes to populate both cluster-internal and public access URLs.
func (r *VirtualMachineFileRestoreReconciler) updateFileServingInfo(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	service *corev1.Service,
) error {
	restoreNamespace := vmfr.Status.CreatedNamespace
	if restoreNamespace == "" {
		return fmt.Errorf("restore namespace not set in status")
	}

	serviceName := service.Name
	fileServingInfo := &oadpv1alpha1.FileServingInfo{}

	// Build SSH serving info if SSH is enabled
	if vmfr.Spec.FileAccess != nil && vmfr.Spec.FileAccess.SSH != nil {
		sshInfo := &oadpv1alpha1.SSHServingInfo{}

		// Find SSH port from service
		var sshPort int32
		for _, port := range service.Spec.Ports {
			if port.Name == "ssh" {
				sshPort = port.Port
				break
			}
		}

		// Build cluster access URL
		if sshPort > 0 {
			sshInfo.ClusterAccess = fmt.Sprintf("ssh://%s.%s.svc.cluster.local:%d",
				serviceName, restoreNamespace, sshPort)
		}

		// Find SSH credentials secret
		sshSecretName, err := r.findSecretByLabels(ctx, restoreNamespace, string(vmfr.UID), constant.CredentialTypeSSH)
		if err == nil {
			sshInfo.CredentialsSecretRef = &oadpv1alpha1.SecretReference{
				Name:      sshSecretName,
				Namespace: restoreNamespace,
			}
		}

		// Note: SSH is not exposed externally via Routes for security reasons.
		// Users can access SSH via port-forward: kubectl port-forward svc/<service-name> 2222:22

		fileServingInfo.SSH = sshInfo
	}

	// Build FileBrowser serving info if FileBrowser is enabled
	if vmfr.Spec.FileAccess != nil && vmfr.Spec.FileAccess.FileBrowser != nil {
		fbInfo := &oadpv1alpha1.FileBrowserServingInfo{}

		// Find HTTPS port from service
		var httpsPort int32
		for _, port := range service.Spec.Ports {
			if port.Name == "https" {
				httpsPort = port.Port
				break
			}
		}

		// Build cluster access URL
		if httpsPort > 0 {
			fbInfo.ClusterAccess = fmt.Sprintf("https://%s.%s.svc.cluster.local:%d",
				serviceName, restoreNamespace, httpsPort)
		}

		// Find FileBrowser credentials secret
		fbSecretName, err := r.findSecretByLabels(ctx, restoreNamespace, string(vmfr.UID), constant.CredentialTypeFileBrowser)
		if err == nil {
			fbInfo.CredentialsSecretRef = &oadpv1alpha1.SecretReference{
				Name:      fbSecretName,
				Namespace: restoreNamespace,
			}
		}

		// Check for external Route if enabled
		if vmfr.Spec.FileAccess.FileBrowser.ExposeExternally {
			routeHost, err := r.findRouteHost(ctx, restoreNamespace, serviceName+"-https")
			if err == nil && routeHost != "" {
				fbInfo.PublicAccess = fmt.Sprintf("https://%s", routeHost)
			}
		}

		fileServingInfo.FileBrowser = fbInfo
	}

	// Update status with FileServingInfo
	vmfr.Status.FileServingInfo = fileServingInfo

	if err := r.Status().Update(ctx, vmfr); err != nil {
		return fmt.Errorf("failed to update status with file serving info: %w", err)
	}

	logger.V(0).Info("Updated file serving info in status",
		"sshClusterAccess", func() string {
			if fileServingInfo.SSH != nil {
				return fileServingInfo.SSH.ClusterAccess
			}
			return ""
		}(),
		"fileBrowserClusterAccess", func() string {
			if fileServingInfo.FileBrowser != nil {
				return fileServingInfo.FileBrowser.ClusterAccess
			}
			return ""
		}(),
		"fileBrowserPublicAccess", func() string {
			if fileServingInfo.FileBrowser != nil {
				return fileServingInfo.FileBrowser.PublicAccess
			}
			return ""
		}())

	return nil
}

// findRouteHost finds the host for a Route by name in the given namespace.
// Returns empty string if Route doesn't exist or host is not yet assigned.
func (r *VirtualMachineFileRestoreReconciler) findRouteHost(ctx context.Context, namespace, routeName string) (string, error) {
	route := &routev1.Route{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      routeName,
		Namespace: namespace,
	}, route)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil // Route not found, not an error
		}
		return "", err
	}

	// Return the route host if available
	if route.Spec.Host != "" {
		return route.Spec.Host, nil
	}

	// Check status for assigned host
	for _, ingress := range route.Status.Ingress {
		if ingress.Host != "" {
			return ingress.Host, nil
		}
	}

	return "", nil
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

	// Create ServiceAccount for file server pods with privileged SCC access
	serviceAccountName := "vmfr-file-server"
	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: namespaceName,
			Labels: map[string]string{
				constant.VMFROriginUUIDLabel: string(vmfr.UID),
				constant.ManagedByLabel:      constant.ManagedByLabelValue,
			},
		},
	}

	err = r.Create(ctx, serviceAccount)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.V(1).Info("ServiceAccount already exists", "serviceAccount", serviceAccountName, "namespace", namespaceName)
		} else {
			return "", fmt.Errorf("failed to create ServiceAccount '%s' in namespace '%s': %w", serviceAccountName, namespaceName, err)
		}
	} else {
		logger.V(0).Info("Created ServiceAccount for file server", "serviceAccount", serviceAccountName, "namespace", namespaceName)
	}

	// Bind ServiceAccount to privileged SCC via RoleBinding
	// This grants the file server pod permission to use privileged mode, hostPath volumes, and spc_t SELinux type
	sccRoleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmfr-file-server-privileged",
			Namespace: namespaceName,
			Labels: map[string]string{
				constant.VMFROriginUUIDLabel: string(vmfr.UID),
				constant.ManagedByLabel:      constant.ManagedByLabelValue,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "system:openshift:scc:privileged",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccountName,
				Namespace: namespaceName,
			},
		},
	}

	err = r.Create(ctx, sccRoleBinding)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.V(1).Info("SCC RoleBinding already exists", "roleBinding", "vmfr-file-server-privileged", "namespace", namespaceName)
		} else {
			return "", fmt.Errorf("failed to create SCC RoleBinding in namespace '%s': %w", namespaceName, err)
		}
	} else {
		logger.V(0).Info("Bound ServiceAccount to privileged SCC", "roleBinding", "vmfr-file-server-privileged", "namespace", namespaceName)
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

// findExistingVeleroRestore finds an existing Velero Restore for the given backup and VMFR.
// Searches by VMFR UID label and backup name annotation.
func (r *VirtualMachineFileRestoreReconciler) findExistingVeleroRestore(
	ctx context.Context,
	backupName string,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
) (*veleroapi.Restore, error) {
	// List all Velero Restores owned by this VMFR
	restoreList := &veleroapi.RestoreList{}
	listOpts := []client.ListOption{
		client.InNamespace(r.OADPNamespace),
		client.MatchingLabels{
			constant.VMFROriginUUIDLabel: string(vmfr.UID),
		},
	}

	if err := r.List(ctx, restoreList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list Velero Restores: %w", err)
	}

	// Find the restore for this specific backup by checking the annotation
	for i := range restoreList.Items {
		if restoreList.Items[i].Annotations[constant.BackupNameAnnotation] == backupName {
			return &restoreList.Items[i], nil
		}
	}

	return nil, fmt.Errorf("no existing Velero Restore found for backup %s", backupName)
}

// checkVeleroRestoresAlreadyCreated checks if Velero Restores have already been created
// by examining if VeleroRestoreName is populated for all available backups in status.
// Returns true if all available backups have VeleroRestoreName set, false otherwise.
func (r *VirtualMachineFileRestoreReconciler) checkVeleroRestoresAlreadyCreated(
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
) bool {
	// Check if any available restore is missing a VeleroRestoreName
	for _, pvcRestore := range vmfr.Status.PVCRestores {
		// Skip synthetic entries (backup-level failures)
		if pvcRestore.PVCName == constant.BackupLevelFailurePVCName {
			continue
		}

		// Check each RestoreInfo for this PVC
		for _, restoreInfo := range pvcRestore.Restores {
			// Only check available backups (those that should have restores created)
			if restoreInfo.State == string(oadptypes.BackupDiscoveryStateAvailable) {
				// If VeleroRestoreName is empty, restores haven't been created yet
				if restoreInfo.VeleroRestoreName == "" {
					return false
				}
			}
		}
	}

	// All available backups have VeleroRestoreName populated
	return true
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

		// IMPLEMENTED FIX: DataDownload PVC Name Mismatch with kubevirt-velero-plugin
		// When using Velero Data Mover with kubevirt-velero-plugin, there's a mismatch between
		// the PVC name in the DataDownload spec and the actual restored PVC name:
		//
		// Problem:
		//   - kubevirt-velero-plugin modifies PVC names during restore (adds backup name prefix + suffix)
		//   - Example: "test-vm-rootdisk" becomes "vm-backup-test-vm-rootdisk-abc123"
		//   - But DataDownload is created with original PVC name in spec.targetVolume.pvc
		//   - DataDownload controller can't find the PVC and gets stuck (no progress)
		//
		// Automatic Fix (implemented in fixDataDownloadPVCNames function):
		//   - Called in the WaitingForRestores phase during monitorVeleroRestores (see line 843)
		//   - Lists DataDownload objects created by our Velero Restores (using VMFR UID label)
		//   - For each DataDownload with empty/New status (not started):
		//     - Finds the corresponding Velero Restore from owner references
		//     - Finds the actual restored PVC name by listing PVCs with velero.io/restore-name label
		//     - Matches PVC by original name annotation (constant.VMFROriginalPVCNameAnnotation)
		//     - Patches DataDownload spec.targetVolume.pvc with the actual PVC name
		//   - Returns count of DataDownloads that were patched
		//   - Errors are logged but don't fail reconciliation (will retry on next reconcile)
		//
		// Implementation Details:
		//   - See fixDataDownloadPVCNames function (line 2715)
		//   - Uses JSON patch to update spec.targetVolume.pvc field
		//   - Handles cases where PVC hasn't been created yet (will retry)
		//   - Skips DataDownloads that are already in progress
		//
		// Impact: Critical for Block mode PVCs with Data Mover (VM disk restores)
		// This fix enables automatic Data Mover restores without manual intervention

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
			// Handle the case where restore already exists (idempotency)
			if apierrors.IsAlreadyExists(err) {
				logger.V(1).Info("Velero Restore already exists, finding existing restore",
					"generateNamePrefix", restoreNamePrefix, "backupName", backupName)

				// Find the existing restore by listing with labels
				existingRestore, findErr := r.findExistingVeleroRestore(ctx, backupName, vmfr)
				if findErr != nil {
					logger.Error(findErr, "Failed to find existing Velero Restore", "backupName", backupName)
					return fmt.Errorf("failed to find existing Velero Restore for backup %s: %w", backupName, findErr)
				}

				// Use the existing restore's name
				actualRestoreName := existingRestore.Name
				backupToRestoreName[backupName] = actualRestoreName
				createdCount++

				logger.V(0).Info("Using existing Velero Restore",
					"restoreName", actualRestoreName,
					"backupName", backupName,
					"targetNamespace", restoreNamespace)
			} else {
				logger.Error(err, "Failed to create Velero Restore", "generateNamePrefix", restoreNamePrefix, "backupName", backupName)
				return fmt.Errorf("failed to create Velero Restore with prefix %s: %w", restoreNamePrefix, err)
			}
		} else {
			// Get the actual name assigned by K8s (available in restore.Name after Create)
			actualRestoreName := restore.Name
			backupToRestoreName[backupName] = actualRestoreName
			createdCount++

			logger.V(0).Info("Created Velero Restore",
				"restoreName", actualRestoreName,
				"backupName", backupName,
				"targetNamespace", restoreNamespace)
		}
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
		case veleroapi.RestorePhaseCompleted, veleroapi.RestorePhaseFinalizing:
			// Treat Finalizing as completed since PVCs are already restored and available for mounting
			completed++
		case veleroapi.RestorePhaseFailed, veleroapi.RestorePhasePartiallyFailed, veleroapi.RestorePhaseFailedValidation:
			failed++
		case veleroapi.RestorePhaseNew, veleroapi.RestorePhaseInProgress, "":
			// Still in progress
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
					// Normalize Finalizing to Completed since from VMFR perspective they're equivalent
					normalizedPhase := restorePhase
					if restorePhase == veleroapi.RestorePhaseFinalizing {
						normalizedPhase = veleroapi.RestorePhaseCompleted
					}

					if restoreInfo.Phase != normalizedPhase {
						logger.V(1).Info("Updating RestoreInfo phase",
							"pvcUID", pvcRestore.PVCUID,
							"restoreName", restoreName,
							"oldPhase", restoreInfo.Phase,
							"newPhase", normalizedPhase,
							"veleroPhase", restorePhase)
						restoreInfo.Phase = normalizedPhase
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
		// The kubevirt-velero plugin restores PVCs with generated names (not original names).
		// Find the actual restored PVC by searching for the Velero Restore label and original name annotation.
		restoredPVCName, err := r.findRestoredPVCName(ctx, restoreNamespace, pvcMount.VeleroRestoreName, pvcMount.PVCName)
		if err != nil {
			logger.V(1).Info("PVC not yet created by Velero restore",
				"originalPVCName", pvcMount.PVCName,
				"veleroRestoreName", pvcMount.VeleroRestoreName,
				"namespace", restoreNamespace,
				"error", err.Error())
			allValid = false
			continue
		}

		logger.V(1).Info("Found restored PVC",
			"originalPVCName", pvcMount.PVCName,
			"restoredPVCName", restoredPVCName,
			"veleroRestoreName", pvcMount.VeleroRestoreName)

		// Get the PVC to check its state
		pvc := &corev1.PersistentVolumeClaim{}
		err = r.Get(ctx, types.NamespacedName{
			Name:      restoredPVCName,
			Namespace: restoreNamespace,
		}, pvc)
		if err != nil {
			return false, fmt.Errorf("failed to get PVC %s: %w", restoredPVCName, err)
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

// findRestoredPVCName finds the actual restored PVC name by searching for PVCs with the given
// Velero Restore label and matching original name annotation
func (r *VirtualMachineFileRestoreReconciler) findRestoredPVCName(
	ctx context.Context,
	restoreNamespace string,
	veleroRestoreName string,
	originalPVCName string,
) (string, error) {
	pvcList := &corev1.PersistentVolumeClaimList{}
	listOpts := []client.ListOption{
		client.InNamespace(restoreNamespace),
		client.MatchingLabels{
			"velero.io/restore-name": veleroRestoreName,
		},
	}

	if err := r.List(ctx, pvcList, listOpts...); err != nil {
		return "", fmt.Errorf("failed to list PVCs for restore %s: %w", veleroRestoreName, err)
	}

	// Filter by label to find the PVC with the matching original name
	for i := range pvcList.Items {
		if pvcList.Items[i].Labels != nil {
			if origName := pvcList.Items[i].Labels[constant.VMFROriginalPVCNameAnnotation]; origName == originalPVCName {
				return pvcList.Items[i].Name, nil
			}
		}
	}

	return "", fmt.Errorf("PVC not found for restore %s with original name %s", veleroRestoreName, originalPVCName)
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

	// Populate the actual restored PVC names and volumeMode (which may differ from original names)
	// We need to query volumeMode to determine whether to use volumeMounts (Filesystem)
	// or volumeDevices (Block) when mounting PVCs in the pod spec
	for i := range pvcMounts {
		restoredName, err := r.findRestoredPVCName(ctx, restoreNamespace, pvcMounts[i].VeleroRestoreName, pvcMounts[i].PVCName)
		if err != nil {
			return fmt.Errorf("failed to find restored PVC name for %s: %w", pvcMounts[i].PVCName, err)
		}
		pvcMounts[i].RestoredPVCName = restoredName

		// Query PVC to get its volumeMode and UID
		pvc := &corev1.PersistentVolumeClaim{}
		err = r.Get(ctx, types.NamespacedName{
			Name:      restoredName,
			Namespace: restoreNamespace,
		}, pvc)
		if err != nil {
			return fmt.Errorf("failed to get PVC %s to query volumeMode: %w", restoredName, err)
		}

		// CRITICAL: Update PVCUID with the restored PVC UID
		// The original PVCUID from status is from the backup, but we need the restored PVC UID
		// for building the correct device path (/dev/pvc-<uid>) for block mode PVCs
		pvcMounts[i].PVCUID = string(pvc.UID)

		// Store volumeMode (defaults to Filesystem if not specified)
		if pvc.Spec.VolumeMode != nil {
			pvcMounts[i].VolumeMode = pvc.Spec.VolumeMode
		} else {
			// Default to Filesystem if not specified (per K8s API defaults)
			defaultMode := corev1.PersistentVolumeFilesystem
			pvcMounts[i].VolumeMode = &defaultMode
		}

		logger.V(1).Info("Resolved PVC info for file server",
			"originalName", pvcMounts[i].PVCName,
			"restoredName", restoredName,
			"restoredUID", pvcMounts[i].PVCUID,
			"volumeMode", *pvcMounts[i].VolumeMode)
	}

	logger.V(0).Info("Creating file server resources",
		"pvcCount", len(pvcMounts),
		"namespace", restoreNamespace)

	// Parse FileAccess spec and build SSH/FileBrowser configurations
	// Note: Credentials were already validated/copied/generated by ensureCredentials()
	// We just need to find the secrets that were prepared and determine which secret names to use
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

		// Find the SSH credentials secret that was prepared by ensureCredentials()
		// The secret is in the restore namespace with labels:
		// - constant.VMFROriginUUIDLabel: string(vmfr.UID)
		// - constant.CredentialTypeLabel: constant.CredentialTypeSSH
		secretName, err := r.findSecretByLabels(
			ctx,
			restoreNamespace,
			string(vmfr.UID),
			constant.CredentialTypeSSH,
		)
		if err != nil {
			return fmt.Errorf("failed to find SSH credentials secret prepared by ensureCredentials: %w", err)
		}

		logger.V(0).Info("Using SSH credentials secret prepared by ensureCredentials",
			"secretName", secretName,
			"namespace", restoreNamespace)

		sshConfig = &SSHAccessConfig{
			Username:                   username,
			CredentialsSecretName:      secretName,
			CredentialsSecretNamespace: restoreNamespace,
			Port:                       port,
		}
	}

	// Configure FileBrowser access if enabled
	if vmfr.Spec.FileAccess != nil && vmfr.Spec.FileAccess.FileBrowser != nil {
		// Use default FileBrowser port
		port := int32(constant.DefaultFileBrowserPort)

		// Find the FileBrowser credentials secret that was prepared by ensureCredentials()
		// The secret is in the restore namespace with labels:
		// - constant.VMFROriginUUIDLabel: string(vmfr.UID)
		// - constant.CredentialTypeLabel: constant.CredentialTypeFileBrowser
		secretName, err := r.findSecretByLabels(
			ctx,
			restoreNamespace,
			string(vmfr.UID),
			constant.CredentialTypeFileBrowser,
		)
		if err != nil {
			return fmt.Errorf("failed to find FileBrowser credentials secret prepared by ensureCredentials: %w", err)
		}

		logger.V(0).Info("Using FileBrowser credentials secret prepared by ensureCredentials",
			"secretName", secretName,
			"namespace", restoreNamespace)

		fileBrowserConfig = &FileBrowserAccessConfig{
			CredentialsSecretName:      secretName,
			CredentialsSecretNamespace: restoreNamespace,
			Port:                       port,
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
		MainContainer:        ptr.To(buildVMFileServerMainContainer(pvcMounts)), // Use VM file server for disk mounting
		SSHAccess:            sshConfig,                                         // Configured SSH access (or nil)
		FileBrowserAccess:    fileBrowserConfig,                                 // Configured FileBrowser access (or nil)
		EnableDualPathAccess: true,                                              // Enable dual-path symlinks
		UseInternalMounts:    true,                                              // CRITICAL: Enable mount propagation for FUSE mounts from vm-file-server
	}

	logger.V(0).Info("File server configuration prepared",
		"sshEnabled", sshConfig != nil,
		"fileBrowserEnabled", fileBrowserConfig != nil)

	// Build pod spec (uses VM file server container)
	// Note: Using Pod instead of Deployment because:
	// 1. ReadWriteOnce PVCs can only be mounted by one pod
	// 2. No cross-namespace owner references allowed (VMFR in OADP ns, Pod in temp ns)
	// 3. Cleanup handled by namespace deletion (namespace has owner ref to VMFR)
	pod, err := buildFileServerPodSpec(podConfig)
	if err != nil {
		return fmt.Errorf("failed to build pod spec: %w", err)
	}

	// Create the pod
	err = r.Create(ctx, pod)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.V(1).Info("File server pod already exists", "podName", pod.Name)
		} else {
			return fmt.Errorf("failed to create file server pod: %w", err)
		}
	} else {
		logger.V(0).Info("Created file server pod", "podName", pod.Name, "namespace", pod.Namespace)
	}

	// Build service configuration
	// Gather service ports based on enabled access methods
	servicePorts := gatherServicePorts(podConfig.SSHAccess, podConfig.FileBrowserAccess)

	// Build service annotations
	// Add TLS certificate generation annotation for FileBrowser (OpenShift specific)
	serviceAnnotations := make(map[string]string)
	if fileBrowserConfig != nil {
		// Request OpenShift to generate a serving certificate for HTTPS
		// The certificate will be stored in the "filebrowser-tls" secret
		serviceAnnotations["service.beta.openshift.io/serving-cert-secret-name"] = "filebrowser-tls"
	}

	serviceConfig := ServiceConfig{
		ServiceName:        fmt.Sprintf("%s-fileserver-svc", vmfr.Name),
		ServiceNamespace:   restoreNamespace,
		VMFRName:           vmfr.Name,
		VMFRNamespace:      vmfr.Namespace,
		VMFRUID:            string(vmfr.UID),
		Ports:              servicePorts,
		ServiceType:        corev1.ServiceTypeClusterIP,
		ServiceAnnotations: serviceAnnotations,
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

	// Create OpenShift Routes if ExposeExternally is enabled
	if err := r.createRoutesIfRequested(ctx, logger, vmfr, service.Name, restoreNamespace); err != nil {
		// Log the error but don't fail the overall operation - Routes are optional
		// and may not be supported on non-OpenShift clusters
		logger.V(0).Info("Failed to create Routes (this is expected on non-OpenShift clusters)",
			"error", err.Error())
	}

	logger.V(0).Info("File server resources created successfully",
		"podName", pod.Name,
		"serviceName", service.Name,
		"namespace", restoreNamespace)

	// Update FileServingInfo in status with access URLs and credentials
	if err := r.updateFileServingInfo(ctx, logger, vmfr, service); err != nil {
		logger.Error(err, "Failed to update file serving info in status")
		// Don't fail the entire operation, just log the error
	}

	return nil
}

// createRoutesIfRequested creates OpenShift Routes for FileBrowser service if ExposeExternally is enabled.
// Routes are optional and only created when explicitly requested via the ExposeExternally flag.
// This function gracefully handles non-OpenShift clusters by returning an error without failing the overall operation.
// Note: SSH routes are not created - SSH access is only available within the cluster (use port-forward for external access).
func (r *VirtualMachineFileRestoreReconciler) createRoutesIfRequested(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	serviceName string,
	namespace string,
) error {
	// Check if FileBrowser Route is requested
	if vmfr.Spec.FileAccess != nil && vmfr.Spec.FileAccess.FileBrowser != nil && vmfr.Spec.FileAccess.FileBrowser.ExposeExternally {
		logger.V(0).Info("Creating OpenShift Route for FileBrowser access")

		routeConfig := RouteConfig{
			RouteName:      serviceName + "-https",
			RouteNamespace: namespace,
			VMFRName:       vmfr.Name,
			VMFRNamespace:  vmfr.Namespace,
			VMFRUID:        string(vmfr.UID),
			ServiceName:    serviceName,
			TargetPort:     "https",
			// FileBrowser uses reencrypt TLS termination (Route terminates external TLS, re-encrypts to backend)
			TLSTermination:                routev1.TLSTerminationReencrypt,
			InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
		}

		fileBrowserRoute, err := buildFileServerRoute(routeConfig)
		if err != nil {
			return fmt.Errorf("failed to build FileBrowser route: %w", err)
		}

		if err := r.Create(ctx, fileBrowserRoute); err != nil {
			if apierrors.IsAlreadyExists(err) {
				logger.V(1).Info("FileBrowser route already exists", "routeName", fileBrowserRoute.Name)
			} else {
				return fmt.Errorf("failed to create FileBrowser route: %w", err)
			}
		} else {
			logger.V(0).Info("Created FileBrowser route",
				"routeName", fileBrowserRoute.Name,
				"namespace", fileBrowserRoute.Namespace)
		}
	}

	return nil
}

// ensureSecretInRestoreNamespace ensures a secret is available in the restore namespace.
// If the secret is already in the restore namespace, it validates and returns the secret name.
// If the secret is in a different namespace, it validates the source, copies it to the restore
// namespace, and tracks the copy with labels for cleanup via finalizer.
//
// Returns the secret name in the restore namespace.
func (r *VirtualMachineFileRestoreReconciler) ensureSecretInRestoreNamespace(
	ctx context.Context,
	secretName string,
	secretNamespace string,
	restoreNamespace string,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	validator func(*corev1.Secret, logr.Logger) error,
	credentialType string, // constant.CredentialTypeSSH or constant.CredentialTypeFileBrowser
	logger logr.Logger,
) (string, error) {

	// If secret namespace matches restore namespace, just validate and return
	if secretNamespace == restoreNamespace {
		logger.V(1).Info("Secret already in restore namespace, validating",
			"secretName", secretName,
			"namespace", restoreNamespace,
			"credentialType", credentialType)

		// Get and validate the secret
		secret := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: secretNamespace}, secret); err != nil {
			return "", fmt.Errorf("failed to get secret %s/%s: %w", secretNamespace, secretName, err)
		}

		// Validate secret contents
		if err := validator(secret, logger); err != nil {
			return "", fmt.Errorf("secret validation failed for %s/%s: %w", secretNamespace, secretName, err)
		}

		logger.V(0).Info("Using existing secret in restore namespace",
			"secretName", secretName,
			"namespace", restoreNamespace,
			"credentialType", credentialType)

		return secretName, nil
	}

	// Secret is in a different namespace - need to copy it
	logger.V(0).Info("Secret in different namespace, copying to restore namespace",
		"sourceSecret", secretName,
		"sourceNamespace", secretNamespace,
		"targetNamespace", restoreNamespace,
		"credentialType", credentialType)

	// Get the source secret
	sourceSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: secretNamespace}, sourceSecret); err != nil {
		return "", fmt.Errorf("failed to get source secret %s/%s: %w", secretNamespace, secretName, err)
	}

	// Validate source secret before copying
	if err := validator(sourceSecret, logger); err != nil {
		return "", fmt.Errorf("source secret validation failed for %s/%s: %w", secretNamespace, secretName, err)
	}

	// Check if copied secret already exists by looking up with labels
	// (using VMFROriginUUIDLabel + CredentialTypeLabel + VMFRManagedCopyLabel)
	secretList := &corev1.SecretList{}
	listOpts := []client.ListOption{
		client.InNamespace(restoreNamespace),
		client.MatchingLabels{
			constant.VMFROriginUUIDLabel:  string(vmfr.UID),
			constant.CredentialTypeLabel:  credentialType,
			constant.VMFRManagedCopyLabel: constant.TrueString,
		},
	}

	if err := r.List(ctx, secretList, listOpts...); err != nil {
		return "", fmt.Errorf("failed to list existing copied secrets: %w", err)
	}

	// Filter to find copy from the same source secret
	for _, existingCopy := range secretList.Items {
		if existingCopy.Annotations["oadp.openshift.io/copied-from-secret"] == secretName &&
			existingCopy.Annotations["oadp.openshift.io/copied-from-ns"] == secretNamespace {
			// Found existing copy, validate and return
			logger.V(1).Info("Copied secret already exists, validating",
				"copiedSecretName", existingCopy.Name,
				"namespace", restoreNamespace)

			if err := validator(&existingCopy, logger); err != nil {
				return "", fmt.Errorf("existing copied secret validation failed for %s/%s: %w", restoreNamespace, existingCopy.Name, err)
			}

			logger.V(0).Info("Using existing copied secret",
				"copiedSecretName", existingCopy.Name,
				"namespace", restoreNamespace)

			return existingCopy.Name, nil
		}
	}

	// Create the copied secret with generateName
	// Format: <vmfr-name>-<credential-type>-
	generateNamePrefix := fmt.Sprintf("%s-%s-", vmfr.Name, credentialType)

	copiedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: generateNamePrefix,
			Namespace:    restoreNamespace,
			Labels: map[string]string{
				constant.ManagedByLabel:       constant.ManagedByLabelValue,
				constant.VMFROriginUUIDLabel:  string(vmfr.UID),
				constant.VMFRManagedCopyLabel: constant.TrueString, // Mark as copied secret for finalizer cleanup
				constant.CredentialTypeLabel:  credentialType,
			},
			Annotations: map[string]string{
				constant.VMFROriginNameAnnotation:      vmfr.Name,
				constant.VMFROriginNamespaceAnnotation: vmfr.Namespace,
				"oadp.openshift.io/copied-from":        fmt.Sprintf("%s/%s", secretNamespace, secretName),
				"oadp.openshift.io/copied-from-secret": secretName,      // Track source secret name (no 63-char limit in annotations)
				"oadp.openshift.io/copied-from-ns":     secretNamespace, // Track source namespace (no 63-char limit in annotations)
				"oadp.openshift.io/generated-by":       "oadp-vm-file-restore-controller",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: sourceSecret.Data, // Copy the data
	}

	// NOTE: We do NOT use owner references here because:
	// 1. VMFR may be in a different namespace (cross-namespace owner refs are rejected)
	// 2. We use labels + finalizer for cleanup instead
	// The finalizer on VMFR will ensure these copied secrets are cleaned up on deletion

	if err := r.Create(ctx, copiedSecret); err != nil {
		return "", fmt.Errorf("failed to create copied secret with prefix %s: %w", generateNamePrefix, err)
	}

	// Get the actual name assigned by Kubernetes
	actualSecretName := copiedSecret.Name

	logger.V(0).Info("Successfully copied secret to restore namespace",
		"sourceSecret", fmt.Sprintf("%s/%s", secretNamespace, secretName),
		"copiedSecret", fmt.Sprintf("%s/%s", restoreNamespace, actualSecretName),
		"credentialType", credentialType)

	return actualSecretName, nil
}

// findSecretByLabels finds a secret in the specified namespace by VMFR UID and credential type labels.
// Returns the name of the first matching secret, or an error if none found.
func (r *VirtualMachineFileRestoreReconciler) findSecretByLabels(
	ctx context.Context,
	namespace string,
	vmfrUID string,
	credentialType string,
) (string, error) {
	// Build label selector
	labels := map[string]string{
		constant.VMFROriginUUIDLabel: vmfrUID,
		constant.CredentialTypeLabel: credentialType,
	}

	// List secrets with matching labels
	secretList := &corev1.SecretList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(labels),
	}

	if err := r.List(ctx, secretList, listOpts...); err != nil {
		return "", fmt.Errorf("failed to list secrets with labels %v: %w", labels, err)
	}

	// Return first match
	if len(secretList.Items) == 0 {
		return "", fmt.Errorf("no secret found with labels %v in namespace %s", labels, namespace)
	}

	if len(secretList.Items) > 1 {
		// This shouldn't happen, but log a warning
		// We'll still use the first one
		log.FromContext(ctx).V(1).Info("Multiple secrets found with same labels, using first",
			"count", len(secretList.Items),
			"namespace", namespace,
			"labels", labels)
	}

	return secretList.Items[0].Name, nil
}

// cleanupAllCredentialSecrets deletes all credential secrets created by this VMFR
// in the restore namespace. This ensures idempotency and prevents duplicate secrets
// from being created by concurrent reconciliations.
func (r *VirtualMachineFileRestoreReconciler) cleanupAllCredentialSecrets(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	namespace string,
) error {
	// List all credential secrets with VMFR UID label
	// Note: We list by VMFR UID and filter by credential type label presence in the loop
	secretList := &corev1.SecretList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{
			constant.VMFROriginUUIDLabel: string(vmfr.UID),
		},
	}

	if err := r.List(ctx, secretList, listOpts...); err != nil {
		return fmt.Errorf("failed to list existing credential secrets: %w", err)
	}

	if len(secretList.Items) == 0 {
		logger.V(1).Info("No existing credential secrets to clean up", "namespace", namespace)
		return nil
	}

	logger.V(0).Info("Cleaning up existing credential secrets before creating new ones",
		"count", len(secretList.Items),
		"namespace", namespace)

	deletedCount := 0
	for i := range secretList.Items {
		secret := &secretList.Items[i]

		// Only delete secrets with credential type label (skip other secrets like copied ones without this label)
		credType := secret.Labels[constant.CredentialTypeLabel]
		if credType == "" {
			logger.V(1).Info("Skipping secret without credential type label",
				"secretName", secret.Name)
			continue
		}

		logger.V(1).Info("Deleting existing credential secret",
			"secretName", secret.Name,
			"credentialType", credType,
			"namespace", namespace)

		if err := r.Delete(ctx, secret); err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("Secret already deleted", "secretName", secret.Name)
				continue
			}
			return fmt.Errorf("failed to delete credential secret %s: %w", secret.Name, err)
		}
		deletedCount++
	}

	if deletedCount > 0 {
		logger.V(0).Info("Cleaned up existing credential secrets",
			"deletedCount", deletedCount,
			"namespace", namespace)
	}

	return nil
}

// ensureCredentials validates and prepares credentials for SSH and FileBrowser access.
// This function handles three scenarios for each credential type:
// 1. CredentialsSecretRef provided → validate and copy to restore namespace if needed
// 2. Inline credentials (SSH publicKey) → validate or use as-is
// 3. No credentials → generate new credentials and create secret in restore namespace
//
// This function should be called AFTER PVCs are restored but BEFORE pod creation.
// It updates the VMFR status to track which secrets to use for pod creation.
func (r *VirtualMachineFileRestoreReconciler) ensureCredentials(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
) error {

	restoreNamespace := vmfr.Status.CreatedNamespace
	if restoreNamespace == "" {
		return fmt.Errorf("restore namespace not found in status")
	}

	sshEnabled := vmfr.Spec.FileAccess != nil && vmfr.Spec.FileAccess.SSH != nil
	fileBrowserEnabled := vmfr.Spec.FileAccess != nil && vmfr.Spec.FileAccess.FileBrowser != nil

	logger.V(0).Info("Ensuring credentials are ready for file server",
		"restoreNamespace", restoreNamespace,
		"sshEnabled", sshEnabled,
		"fileBrowserEnabled", fileBrowserEnabled)

	// If no file access methods enabled, this should have been caught by CRD validation
	// But we check here for safety
	if !sshEnabled && !fileBrowserEnabled {
		return fmt.Errorf("invalid fileAccess configuration: at least one of SSH or FileBrowser must be specified")
	}

	// CRITICAL: Clean up any existing credential secrets from previous reconciliations
	// This prevents duplicate secrets from being created by concurrent reconciliations
	// and ensures idempotency (running this function multiple times produces the same result)
	if err := r.cleanupAllCredentialSecrets(ctx, logger, vmfr, restoreNamespace); err != nil {
		return fmt.Errorf("failed to cleanup existing credential secrets: %w", err)
	}

	// Handle SSH credentials if SSH access is enabled
	if sshEnabled {
		sshSpec := vmfr.Spec.FileAccess.SSH

		if sshSpec.CredentialsSecretRef != nil {
			// Scenario 1: User provided CredentialsSecretRef
			secretName := sshSpec.CredentialsSecretRef.Name
			secretNamespace := sshSpec.CredentialsSecretRef.Namespace

			// Default namespace to OADP namespace if not specified
			if secretNamespace == "" {
				secretNamespace = r.OADPNamespace
			}

			logger.V(0).Info("SSH credentials from secret reference",
				"secretName", secretName,
				"secretNamespace", secretNamespace)

			// Validate and copy secret to restore namespace if needed
			preparedSecretName, err := r.ensureSecretInRestoreNamespace(
				ctx,
				secretName,
				secretNamespace,
				restoreNamespace,
				vmfr,
				function.ValidateSSHSecret,
				constant.CredentialTypeSSH,
				logger,
			)
			if err != nil {
				return fmt.Errorf("failed to ensure SSH secret in restore namespace: %w", err)
			}

			logger.V(1).Info("SSH secret prepared and ready for use",
				"secretName", preparedSecretName,
				"namespace", restoreNamespace)

		} else if sshSpec.PublicKey != "" {
			// Scenario 2: Inline publicKey provided - create secret for consistency
			logger.V(0).Info("SSH credentials from inline publicKey, creating secret")

			// Validate the inline publicKey using robust SSH parser
			if err := function.ValidateSSHPublicKey([]byte(sshSpec.PublicKey)); err != nil {
				return fmt.Errorf("inline SSH publicKey validation failed: %w", err)
			}

			// Create secret with inline publicKey in restore namespace
			username := sshSpec.Username
			if username == "" {
				username = constant.DefaultSSHUsername
			}

			// Use generateName for automatic unique naming
			generateNamePrefix := fmt.Sprintf("%s-ssh-inline-", vmfr.Name)
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: generateNamePrefix,
					Namespace:    restoreNamespace,
					Labels: map[string]string{
						constant.ManagedByLabel:      constant.ManagedByLabelValue,
						constant.VMFROriginUUIDLabel: string(vmfr.UID),
						constant.CredentialTypeLabel: constant.CredentialTypeSSH,
					},
					Annotations: map[string]string{
						constant.VMFROriginNameAnnotation:      vmfr.Name,
						constant.VMFROriginNamespaceAnnotation: vmfr.Namespace,
						"oadp.openshift.io/generated-by":       "oadp-vm-file-restore-controller",
						"oadp.openshift.io/source":             "inline-public-key",
					},
					// NOTE: No OwnerReferences - VMFR may be in different namespace (cross-namespace refs are rejected)
					// Use labels + finalizer for cleanup instead
				},
				Type: corev1.SecretTypeOpaque,
				StringData: map[string]string{
					"username":        username,
					"authorized_keys": sshSpec.PublicKey,
				},
			}

			if err := r.Create(ctx, secret); err != nil {
				return fmt.Errorf("failed to create SSH secret from inline public key: %w", err)
			}

			logger.V(0).Info("Created SSH secret from inline public key",
				"secretName", secret.Name,
				"namespace", restoreNamespace)

		} else {
			// Scenario 3: Generate SSH credentials
			logger.V(0).Info("Generating SSH credentials (no publicKey or secret provided)")

			keyPair, err := function.GenerateSSHKeyPair(logger)
			if err != nil {
				return fmt.Errorf("failed to generate SSH keypair: %w", err)
			}

			// Create secret with generated credentials in restore namespace
			username := sshSpec.Username
			if username == "" {
				username = constant.DefaultSSHUsername
			}

			// Use generateName for automatic unique naming
			generateNamePrefix := fmt.Sprintf("%s-ssh-", vmfr.Name)
			secret := function.CreateSSHCredentialsSecret(
				generateNamePrefix,
				restoreNamespace,
				username,
				keyPair,
				vmfr.Name,
				vmfr.Namespace,
				vmfr.UID,
				logger,
			)

			if err := r.Create(ctx, secret); err != nil {
				return fmt.Errorf("failed to create generated SSH credentials secret: %w", err)
			}

			logger.V(0).Info("Created generated SSH credentials secret",
				"secretName", secret.Name,
				"namespace", restoreNamespace)
		}
	}

	// Handle FileBrowser credentials if FileBrowser access is enabled
	if fileBrowserEnabled {
		fbSpec := vmfr.Spec.FileAccess.FileBrowser

		if fbSpec.CredentialsSecretRef != nil {
			// Scenario 1: User provided CredentialsSecretRef
			secretName := fbSpec.CredentialsSecretRef.Name
			secretNamespace := fbSpec.CredentialsSecretRef.Namespace

			// Default namespace to OADP namespace if not specified (per CRD comment line 156)
			if secretNamespace == "" {
				secretNamespace = r.OADPNamespace
			}

			logger.V(0).Info("FileBrowser credentials from secret reference",
				"secretName", secretName,
				"secretNamespace", secretNamespace)

			// Validate and copy secret to restore namespace if needed
			preparedSecretName, err := r.ensureSecretInRestoreNamespace(
				ctx,
				secretName,
				secretNamespace,
				restoreNamespace,
				vmfr,
				function.ValidateFileBrowserSecret,
				constant.CredentialTypeFileBrowser,
				logger,
			)
			if err != nil {
				return fmt.Errorf("failed to ensure FileBrowser secret in restore namespace: %w", err)
			}

			logger.V(1).Info("FileBrowser secret prepared and ready for use",
				"secretName", preparedSecretName,
				"namespace", restoreNamespace)

		} else {
			// Scenario 2: Generate FileBrowser credentials (always required, no inline option)
			logger.V(0).Info("Generating FileBrowser credentials (no secret provided)")

			credentials, err := function.GenerateFileBrowserCredentials("", logger) // Use default username
			if err != nil {
				return fmt.Errorf("failed to generate FileBrowser credentials: %w", err)
			}

			// Create secret with generated credentials in restore namespace
			// Use generateName for automatic unique naming
			generateNamePrefix := fmt.Sprintf("%s-filebrowser-", vmfr.Name)
			secret := function.CreateFileBrowserCredentialsSecret(
				generateNamePrefix,
				restoreNamespace,
				credentials,
				vmfr.Name,
				vmfr.Namespace,
				vmfr.UID,
				logger,
			)

			if err := r.Create(ctx, secret); err != nil {
				return fmt.Errorf("failed to create generated FileBrowser credentials secret: %w", err)
			}

			logger.V(0).Info("Created generated FileBrowser credentials secret",
				"secretName", secret.Name,
				"namespace", restoreNamespace,
				"username", credentials.Username)
		}
	}

	logger.V(0).Info("All credentials ready for file server creation")
	return nil
}

// fixDataDownloadPVCNames fixes the DataDownload PVC name mismatch issue caused by kubevirt-velero-plugin.
// When using Velero Data Mover with kubevirt-velero-plugin, there's a mismatch between the PVC name
// in the DataDownload spec and the actual restored PVC name:
// - kubevirt-velero-plugin modifies PVC names during restore (adds backup name prefix + suffix)
// - But DataDownload is created with the original PVC name in spec.targetVolume.pvc
// - DataDownload controller can't find the PVC and gets stuck
//
// This function:
// 1. Lists DataDownload resources created by our Velero Restores (using VMFR UID label)
// 2. For each DataDownload with empty/pending status (not started):
//   - Finds the corresponding Velero Restore from annotations
//   - Finds the actual restored PVC name by listing PVCs with velero.io/restore-name label
//   - Matches PVC by original name annotation (constant.VMFROriginalPVCNameAnnotation)
//   - Patches DataDownload spec.targetVolume.pvc with the actual PVC name
//
// Returns the count of DataDownloads that were patched.
func (r *VirtualMachineFileRestoreReconciler) fixDataDownloadPVCNames(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
) (int, error) {

	// Get restore namespace where PVCs were created
	restoreNamespace := vmfr.Status.CreatedNamespace
	if restoreNamespace == "" {
		logger.V(1).Info("Restore namespace not yet created, skipping DataDownload fix")
		return 0, nil
	}

	// IMPORTANT: Velero creates DataDownload resources WITHOUT our custom VMFR UID label
	// We can't list DataDownloads directly by VMFR UID. Instead, we need to:
	// 1. First list Velero Restores created by this VMFR (they DO have our VMFR UID label)
	// 2. Then find DataDownloads associated with those Velero Restores (via velero.io/restore-name label)

	// Step 1: List all Velero Restores owned by this VMFR
	restoreList := &veleroapi.RestoreList{}
	restoreListOpts := []client.ListOption{
		client.InNamespace(r.OADPNamespace),
		client.MatchingLabels{
			constant.VMFROriginUUIDLabel: string(vmfr.UID),
		},
	}

	if err := r.List(ctx, restoreList, restoreListOpts...); err != nil {
		logger.Error(err, "Failed to list Velero Restores for DataDownload lookup")
		return 0, fmt.Errorf("failed to list Velero Restores: %w", err)
	}

	if len(restoreList.Items) == 0 {
		logger.V(1).Info("No Velero Restores found for this VMFR, no DataDownloads to fix")
		return 0, nil
	}

	logger.V(1).Info("Found Velero Restores for VMFR", "count", len(restoreList.Items))

	// Step 2: For each Velero Restore, find associated DataDownloads
	var dataDownloads []veleroapiv2alpha1.DataDownload
	for _, restore := range restoreList.Items {
		// List DataDownloads for this specific Velero Restore using velero.io/restore-name label
		ddList := &veleroapiv2alpha1.DataDownloadList{}
		ddListOpts := []client.ListOption{
			client.InNamespace(r.OADPNamespace),
			client.MatchingLabels{
				"velero.io/restore-name": restore.Name,
			},
		}

		if err := r.List(ctx, ddList, ddListOpts...); err != nil {
			logger.Error(err, "Failed to list DataDownloads for Velero Restore", "restoreName", restore.Name)
			continue // Continue with other restores even if one fails
		}

		dataDownloads = append(dataDownloads, ddList.Items...)
	}

	if len(dataDownloads) == 0 {
		logger.V(1).Info("No DataDownload resources found for this VMFR's Velero Restores")
		return 0, nil
	}

	logger.V(0).Info("Found DataDownload resources", "count", len(dataDownloads))

	fixedCount := 0
	for i := range dataDownloads {
		dataDownload := &dataDownloads[i]

		// Only process DataDownloads that haven't started yet
		// Status.Phase is empty or "New" when DataDownload hasn't started
		if dataDownload.Status.Phase != "" && dataDownload.Status.Phase != "New" {
			logger.V(1).Info("DataDownload already in progress, skipping",
				"name", dataDownload.Name,
				"phase", dataDownload.Status.Phase)
			continue
		}

		// Get the Velero Restore name from DataDownload owner references or labels
		// DataDownload is owned by the Velero Restore that created it
		veleroRestoreName := ""
		for _, ownerRef := range dataDownload.OwnerReferences {
			if ownerRef.Kind == "Restore" && ownerRef.APIVersion == "velero.io/v1" {
				veleroRestoreName = ownerRef.Name
				break
			}
		}

		if veleroRestoreName == "" {
			logger.V(1).Info("DataDownload has no Velero Restore owner reference, skipping",
				"name", dataDownload.Name)
			continue
		}

		// Get the original PVC name from DataDownload spec
		originalPVCName := dataDownload.Spec.TargetVolume.PVC
		if originalPVCName == "" {
			logger.V(1).Info("DataDownload has no target PVC name, skipping",
				"name", dataDownload.Name)
			continue
		}

		logger.V(1).Info("Processing DataDownload",
			"name", dataDownload.Name,
			"veleroRestore", veleroRestoreName,
			"originalPVCName", originalPVCName)

		// Find the actual restored PVC name
		actualPVCName, err := r.findRestoredPVCName(ctx, restoreNamespace, veleroRestoreName, originalPVCName)
		if err != nil {
			logger.V(1).Info("PVC not yet created for DataDownload, will retry later",
				"dataDownload", dataDownload.Name,
				"originalPVCName", originalPVCName,
				"veleroRestore", veleroRestoreName,
				"error", err.Error())
			// Don't treat this as a hard error - PVC might not be created yet
			continue
		}

		// Check if PVC name needs updating
		if actualPVCName == originalPVCName {
			logger.V(1).Info("DataDownload PVC name already correct, no patch needed",
				"name", dataDownload.Name,
				"pvcName", actualPVCName)
			continue
		}

		logger.V(0).Info("Patching DataDownload with correct PVC name",
			"dataDownload", dataDownload.Name,
			"originalPVCName", originalPVCName,
			"actualPVCName", actualPVCName)

		// Patch the DataDownload with the actual PVC name
		// Use JSON patch to update spec.targetVolume.pvc field
		patch := client.RawPatch(
			types.JSONPatchType,
			[]byte(fmt.Sprintf(`[{"op": "replace", "path": "/spec/targetVolume/pvc", "value": "%s"}]`, actualPVCName)),
		)

		if err := r.Patch(ctx, dataDownload, patch); err != nil {
			logger.Error(err, "Failed to patch DataDownload",
				"name", dataDownload.Name,
				"originalPVCName", originalPVCName,
				"actualPVCName", actualPVCName)
			return fixedCount, fmt.Errorf("failed to patch DataDownload %s: %w", dataDownload.Name, err)
		}

		fixedCount++
		logger.V(0).Info("Successfully patched DataDownload",
			"name", dataDownload.Name,
			"originalPVCName", originalPVCName,
			"actualPVCName", actualPVCName)
	}

	if fixedCount > 0 {
		logger.V(0).Info("DataDownload PVC name fix completed",
			"totalDataDownloads", len(dataDownloads),
			"fixedCount", fixedCount)
	}

	return fixedCount, nil
}

// handleVeleroRestoreCleanup deletes Velero Restore objects created by this VMFR.
// This finalizer runs first to ensure Velero Restores are cleaned up before
// other resources that they may reference.
func (r *VirtualMachineFileRestoreReconciler) handleVeleroRestoreCleanup(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
) (bool, error) {
	if !controllerutil.ContainsFinalizer(vmfr, constant.VeleroRestoreCleanupFinalizer) {
		logger.V(1).Info("VeleroRestoreCleanupFinalizer already removed, skipping")
		return false, nil
	}

	logger.V(0).Info("Cleaning up Velero Restore objects")

	// Find all Velero Restore objects created by this VMFR using label selector
	restoreList := &veleroapi.RestoreList{}
	listOpts := []client.ListOption{
		client.InNamespace(r.OADPNamespace),
		client.MatchingLabels{
			constant.VMFROriginUUIDLabel: string(vmfr.UID),
		},
	}

	if err := r.List(ctx, restoreList, listOpts...); err != nil {
		logger.Error(err, "Failed to list Velero Restore objects for cleanup")
		return false, fmt.Errorf("failed to list Velero Restore objects: %w", err)
	}

	logger.V(0).Info("Found Velero Restore objects to delete", "count", len(restoreList.Items))

	deletedCount := 0
	for i := range restoreList.Items {
		restore := &restoreList.Items[i]
		logger.V(1).Info("Deleting Velero Restore", "name", restore.Name, "namespace", restore.Namespace)

		if err := r.Delete(ctx, restore); err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("Velero Restore already deleted", "name", restore.Name)
				continue
			}
			logger.Error(err, "Failed to delete Velero Restore", "name", restore.Name)
			return false, fmt.Errorf("failed to delete Velero Restore %s: %w", restore.Name, err)
		}
		deletedCount++
	}

	logger.V(0).Info("Deleted Velero Restore objects", "count", deletedCount)

	// Remove this finalizer to proceed to next cleanup step
	patch := client.MergeFrom(vmfr.DeepCopy())
	controllerutil.RemoveFinalizer(vmfr, constant.VeleroRestoreCleanupFinalizer)
	if err := r.Patch(ctx, vmfr, patch); err != nil {
		logger.Error(err, "Failed to remove VeleroRestoreCleanupFinalizer")
		return false, fmt.Errorf("failed to remove VeleroRestoreCleanupFinalizer: %w", err)
	}

	logger.V(0).Info("Removed VeleroRestoreCleanupFinalizer")
	return true, nil
}

// handleResourceCleanup cleans up namespace, PVCs, and secrets based on namespace ownership.
// For temporary namespaces created by the controller, the entire namespace is deleted
// (which cascades to all contained resources). For user-provided namespaces, only
// resources created by this controller are individually deleted.
func (r *VirtualMachineFileRestoreReconciler) handleResourceCleanup(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
) (bool, error) {
	if !controllerutil.ContainsFinalizer(vmfr, constant.VMFileRestoreFinalizer) {
		logger.V(1).Info("VMFileRestoreFinalizer already removed, skipping")
		return false, nil
	}

	logger.V(0).Info("Cleaning up VMFR resources")

	restoreNamespace := vmfr.Status.CreatedNamespace
	if restoreNamespace == "" {
		logger.V(0).Info("No restore namespace found in status, skipping resource cleanup")
		patch := client.MergeFrom(vmfr.DeepCopy())
		controllerutil.RemoveFinalizer(vmfr, constant.VMFileRestoreFinalizer)
		if err := r.Patch(ctx, vmfr, patch); err != nil {
			logger.Error(err, "Failed to remove VMFileRestoreFinalizer")
			return false, fmt.Errorf("failed to remove VMFileRestoreFinalizer: %w", err)
		}
		logger.V(0).Info("Removed VMFileRestoreFinalizer (no namespace to clean)")
		return false, nil
	}

	// Check if namespace exists and determine if it's controller-managed
	namespace := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: restoreNamespace}, namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(0).Info("Restore namespace already deleted", "namespace", restoreNamespace)
			patch := client.MergeFrom(vmfr.DeepCopy())
			controllerutil.RemoveFinalizer(vmfr, constant.VMFileRestoreFinalizer)
			if err := r.Patch(ctx, vmfr, patch); err != nil {
				logger.Error(err, "Failed to remove VMFileRestoreFinalizer")
				return false, fmt.Errorf("failed to remove VMFileRestoreFinalizer: %w", err)
			}
			logger.V(0).Info("Removed VMFileRestoreFinalizer (namespace already deleted)")
			return false, nil
		}
		logger.Error(err, "Failed to get restore namespace", "namespace", restoreNamespace)
		return false, fmt.Errorf("failed to get restore namespace: %w", err)
	}

	// Determine cleanup strategy based on namespace ownership
	isTempNamespace := namespace.Labels != nil &&
		namespace.Labels[constant.VMFRTempNamespaceLabel] == constant.TrueString

	if isTempNamespace {
		// Delete the entire temporary namespace - Kubernetes will cascade to all resources
		logger.V(0).Info("Deleting temporary namespace created by controller",
			"namespace", restoreNamespace)

		if err := r.Delete(ctx, namespace); err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("Temporary namespace already deleted", "namespace", restoreNamespace)
			} else {
				logger.Error(err, "Failed to delete temporary namespace", "namespace", restoreNamespace)
				return false, fmt.Errorf("failed to delete temporary namespace: %w", err)
			}
		} else {
			logger.V(0).Info("Temporary namespace deletion initiated", "namespace", restoreNamespace)
		}
	} else {
		// User-provided namespace - selectively delete only controller-created resources
		logger.V(0).Info("Cleaning up controller resources in user-provided namespace",
			"namespace", restoreNamespace)

		// Delete file server pods created by this controller
		if err := r.deleteFileServerPods(ctx, logger, vmfr, restoreNamespace); err != nil {
			return false, err
		}

		// Delete file server services created by this controller
		if err := r.deleteFileServerServices(ctx, logger, vmfr, restoreNamespace); err != nil {
			return false, err
		}

		// Delete PVCs restored by this VMFR's Velero Restore objects
		if err := r.deleteRestoredPVCs(ctx, logger, vmfr, restoreNamespace); err != nil {
			return false, err
		}

		// Delete secrets created or copied by this controller
		if err := r.deleteControllerSecrets(ctx, logger, vmfr, restoreNamespace); err != nil {
			return false, err
		}

		logger.V(0).Info("Completed cleanup of controller resources",
			"namespace", restoreNamespace)
	}

	// Remove the main finalizer - VMFR can now be deleted
	patch := client.MergeFrom(vmfr.DeepCopy())
	controllerutil.RemoveFinalizer(vmfr, constant.VMFileRestoreFinalizer)
	if err := r.Patch(ctx, vmfr, patch); err != nil {
		logger.Error(err, "Failed to remove VMFileRestoreFinalizer")
		return false, fmt.Errorf("failed to remove VMFileRestoreFinalizer: %w", err)
	}

	logger.V(0).Info("Removed VMFileRestoreFinalizer, VMFR cleanup complete")
	return false, nil
}

// deleteRestoredPVCs removes PVCs that were created by this VMFR's Velero Restore operations.
// PVCs are identified by the Velero restore label that matches restore names from VMFR status.
func (r *VirtualMachineFileRestoreReconciler) deleteRestoredPVCs(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	namespace string,
) error {
	logger.V(0).Info("Deleting restored PVCs", "namespace", namespace)

	// Extract unique Velero Restore names from VMFR status
	restoreNames := make(map[string]bool)
	for _, pvcRestore := range vmfr.Status.PVCRestores {
		for _, restoreInfo := range pvcRestore.Restores {
			if restoreInfo.VeleroRestoreName != "" {
				restoreNames[restoreInfo.VeleroRestoreName] = true
			}
		}
	}

	if len(restoreNames) == 0 {
		logger.V(1).Info("No Velero Restore names found in status, skipping PVC deletion")
		return nil
	}

	logger.V(1).Info("Found Velero Restore names from status", "count", len(restoreNames))

	// Delete PVCs for each Velero Restore using the restore label
	deletedCount := 0
	for restoreName := range restoreNames {
		pvcList := &corev1.PersistentVolumeClaimList{}
		listOpts := []client.ListOption{
			client.InNamespace(namespace),
			client.MatchingLabels{
				"velero.io/restore-name": restoreName,
			},
		}

		if err := r.List(ctx, pvcList, listOpts...); err != nil {
			logger.Error(err, "Failed to list PVCs for Velero Restore", "restoreName", restoreName)
			return fmt.Errorf("failed to list PVCs for restore %s: %w", restoreName, err)
		}

		logger.V(1).Info("Found PVCs for Velero Restore",
			"restoreName", restoreName,
			"pvcCount", len(pvcList.Items))

		for i := range pvcList.Items {
			pvc := &pvcList.Items[i]
			logger.V(1).Info("Deleting PVC",
				"pvcName", pvc.Name,
				"namespace", pvc.Namespace,
				"restoreName", restoreName)

			if err := r.Delete(ctx, pvc); err != nil {
				if apierrors.IsNotFound(err) {
					logger.V(1).Info("PVC already deleted", "pvcName", pvc.Name)
					continue
				}
				logger.Error(err, "Failed to delete PVC", "pvcName", pvc.Name)
				return fmt.Errorf("failed to delete PVC %s: %w", pvc.Name, err)
			}
			deletedCount++
		}
	}

	logger.V(0).Info("Deleted restored PVCs", "count", deletedCount, "namespace", namespace)
	return nil
}

// deleteControllerSecrets removes secrets created or copied by this controller.
// Secrets are identified by the VMFROriginUUIDLabel matching this VMFR's UID.
// This includes both copied secrets (from other namespaces) and generated credentials.
func (r *VirtualMachineFileRestoreReconciler) deleteControllerSecrets(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	namespace string,
) error {
	logger.V(0).Info("Deleting controller-created secrets", "namespace", namespace)

	secretList := &corev1.SecretList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{
			constant.VMFROriginUUIDLabel: string(vmfr.UID),
		},
	}

	if err := r.List(ctx, secretList, listOpts...); err != nil {
		logger.Error(err, "Failed to list controller secrets")
		return fmt.Errorf("failed to list controller secrets: %w", err)
	}

	logger.V(0).Info("Found controller-created secrets to delete", "count", len(secretList.Items))

	deletedCount := 0
	for i := range secretList.Items {
		secret := &secretList.Items[i]
		logger.V(1).Info("Deleting controller secret",
			"secretName", secret.Name,
			"namespace", secret.Namespace,
			"isCopy", secret.Labels[constant.VMFRManagedCopyLabel] == constant.TrueString,
			"credentialType", secret.Labels[constant.CredentialTypeLabel])

		if err := r.Delete(ctx, secret); err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("Secret already deleted", "secretName", secret.Name)
				continue
			}
			logger.Error(err, "Failed to delete secret", "secretName", secret.Name)
			return fmt.Errorf("failed to delete secret %s: %w", secret.Name, err)
		}
		deletedCount++
	}

	logger.V(0).Info("Deleted controller-created secrets", "count", deletedCount, "namespace", namespace)
	return nil
}

// deleteFileServerPods removes file server pods (including all sidecars) created by this controller.
// Pods are identified by the VMFROriginUUIDLabel matching this VMFR's UID.
// Since sidecars are part of the pod spec, deleting the pod automatically cleans up all containers.
func (r *VirtualMachineFileRestoreReconciler) deleteFileServerPods(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	namespace string,
) error {
	logger.V(0).Info("Deleting file server pods (including sidecars)", "namespace", namespace)

	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{
			constant.VMFROriginUUIDLabel: string(vmfr.UID),
			"app":                        "vmfr-file-server",
		},
	}

	if err := r.List(ctx, podList, listOpts...); err != nil {
		logger.Error(err, "Failed to list file server pods")
		return fmt.Errorf("failed to list file server pods: %w", err)
	}

	logger.V(0).Info("Found file server pods to delete", "count", len(podList.Items))

	deletedCount := 0
	for i := range podList.Items {
		pod := &podList.Items[i]

		// Log which access methods (sidecars) are enabled on this pod
		accessMethods := pod.Annotations["oadp.openshift.io/vmfr-access-methods"]
		logger.V(1).Info("Deleting file server pod",
			"podName", pod.Name,
			"namespace", pod.Namespace,
			"accessMethods", accessMethods)

		if err := r.Delete(ctx, pod); err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("Pod already deleted", "podName", pod.Name)
				continue
			}
			logger.Error(err, "Failed to delete pod", "podName", pod.Name)
			return fmt.Errorf("failed to delete pod %s: %w", pod.Name, err)
		}
		deletedCount++
	}

	logger.V(0).Info("Deleted file server pods", "count", deletedCount, "namespace", namespace)
	return nil
}

// deleteFileServerServices removes file server services created by this controller.
// Services are identified by the VMFROriginUUIDLabel matching this VMFR's UID.
func (r *VirtualMachineFileRestoreReconciler) deleteFileServerServices(
	ctx context.Context,
	logger logr.Logger,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
	namespace string,
) error {
	logger.V(0).Info("Deleting file server services", "namespace", namespace)

	serviceList := &corev1.ServiceList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{
			constant.VMFROriginUUIDLabel: string(vmfr.UID),
			"app":                        "vmfr-file-server",
		},
	}

	if err := r.List(ctx, serviceList, listOpts...); err != nil {
		logger.Error(err, "Failed to list file server services")
		return fmt.Errorf("failed to list file server services: %w", err)
	}

	logger.V(0).Info("Found file server services to delete", "count", len(serviceList.Items))

	deletedCount := 0
	for i := range serviceList.Items {
		service := &serviceList.Items[i]
		logger.V(1).Info("Deleting file server service",
			"serviceName", service.Name,
			"namespace", service.Namespace)

		if err := r.Delete(ctx, service); err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("Service already deleted", "serviceName", service.Name)
				continue
			}
			logger.Error(err, "Failed to delete service", "serviceName", service.Name)
			return fmt.Errorf("failed to delete service %s: %w", service.Name, err)
		}
		deletedCount++
	}

	logger.V(0).Info("Deleted file server services", "count", deletedCount, "namespace", namespace)
	return nil
}

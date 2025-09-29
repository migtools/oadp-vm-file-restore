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

package controller

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/go-logr/logr"
	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubevirtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	oadpv1alpha1 "github.com/migtools/oadp-vm-file-restore/api/v1alpha1"
	"github.com/migtools/oadp-vm-file-restore/internal/common/constant"
	"github.com/migtools/oadp-vm-file-restore/internal/velerohelpers"
)

// VirtualMachineBackupsDiscoveryReconciler reconciles a VirtualMachineBackupsDiscovery object
type VirtualMachineBackupsDiscoveryReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// OADPNamespace is the namespace where OADP and Velero backups are located
	OADPNamespace string

	// BackupContentsReader for reading backup contents
	BackupContentsReader *velerohelpers.VeleroBackupContentsReader
}

// virtualmachinebackupsdiscoveryReconcileStepFunction defines the signature for VMBD reconciliation steps
type virtualmachinebackupsdiscoveryReconcileStepFunction func(ctx context.Context, logger logr.Logger, vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery) (bool, error)

// +kubebuilder:rbac:groups=oadp.openshift.io,resources=virtualmachinebackupsdiscoveries,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=oadp.openshift.io,resources=virtualmachinebackupsdiscoveries/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=oadp.openshift.io,resources=virtualmachinebackupsdiscoveries/finalizers,verbs=update
// +kubebuilder:rbac:groups=velero.io,resources=backups,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *VirtualMachineBackupsDiscoveryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("VirtualMachineBackupsDiscovery Reconcile start")

	// Get the VirtualMachineBackupsDiscovery object
	vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
	err := r.Get(ctx, req.NamespacedName, vmbd)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info("VirtualMachineBackupsDiscovery not found, skipping reconciliation")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Unable to fetch VirtualMachineBackupsDiscovery")
		return ctrl.Result{}, err
	}

	// Determine which path to take based on current state
	var reconcileSteps []virtualmachinebackupsdiscoveryReconcileStepFunction

	// Debug logging to understand resource state
	logger.V(1).Info("Reconciling VMBD resource",
		"phase", vmbd.Status.Phase,
		"generation", vmbd.Generation,
		"observedGeneration", vmbd.Status.ObservedGeneration,
		"discoveryStatsNil", vmbd.Status.DiscoveryStats == nil)
	if vmbd.Status.DiscoveryStats != nil {
		logger.V(1).Info("Discovery stats",
			"pending", vmbd.Status.DiscoveryStats.Pending,
			"inProgress", vmbd.Status.DiscoveryStats.InProgress,
			"completed", vmbd.Status.DiscoveryStats.Completed,
			"failed", vmbd.Status.DiscoveryStats.Failed,
			"skipped", vmbd.Status.DiscoveryStats.Skipped)
	}

	// Debug the condition evaluations
	logger.V(1).Info("Evaluating reconciliation conditions",
		"phaseEmpty", vmbd.Status.Phase == "",
		"discoveryStatsNil", vmbd.Status.DiscoveryStats == nil,
		"isDiscoveryComplete", r.isDiscoveryComplete(vmbd),
		"generationsMatch", vmbd.Status.ObservedGeneration == vmbd.Generation)

	switch {
	case vmbd.Status.Phase == "":
		// Initial creation path
		logger.V(4).Info("Executing initial creation path")
		reconcileSteps = []virtualmachinebackupsdiscoveryReconcileStepFunction{
			r.initializePhase,
		}

	case vmbd.Status.DiscoveryStats == nil:
		// Discovery initialization path
		logger.V(4).Info("Executing discovery initialization path")
		reconcileSteps = []virtualmachinebackupsdiscoveryReconcileStepFunction{
			r.initializeDiscoveryProcess,
		}

	case r.isDiscoveryComplete(vmbd) && vmbd.Status.ObservedGeneration == vmbd.Generation:
		// Discovery complete and no spec changes - minimal reconciliation
		logger.V(1).Info("Discovery already complete, checking for spec changes")
		reconcileSteps = []virtualmachinebackupsdiscoveryReconcileStepFunction{
			r.checkForSpecChanges,
		}

	case r.isDiscoveryComplete(vmbd) && vmbd.Status.ObservedGeneration != vmbd.Generation:
		// Discovery complete but spec has changed - restart discovery
		logger.V(4).Info("Discovery complete but spec changed, restarting discovery")
		reconcileSteps = []virtualmachinebackupsdiscoveryReconcileStepFunction{
			r.initializeDiscoveryProcess,
		}

	case vmbd.Status.DiscoveryStats != nil && (vmbd.Status.DiscoveryStats.Pending > 0 || vmbd.Status.DiscoveryStats.InProgress > 0):
		// Active discovery process path
		logger.V(4).Info("Executing active discovery process path")
		reconcileSteps = []virtualmachinebackupsdiscoveryReconcileStepFunction{
			r.processDiscoveryBatch,
		}

	default:
		// Finalization path
		logger.V(4).Info("Executing discovery finalization path")
		reconcileSteps = []virtualmachinebackupsdiscoveryReconcileStepFunction{
			r.finalizeDiscoveryProcess,
		}
	}

	// Execute the selected reconciliation steps
	for _, step := range reconcileSteps {
		// Get fresh object for each step to avoid conflicts
		freshVmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
		if err := r.Get(ctx, req.NamespacedName, freshVmbd); err != nil {
			logger.Error(err, "Failed to get fresh object for step execution")
			return ctrl.Result{}, err
		}

		requeue, err := step(ctx, logger, freshVmbd)
		if err != nil {
			return ctrl.Result{}, err
		} else if requeue {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	logger.V(1).Info("VirtualMachineBackupsDiscovery Reconcile exit")
	return ctrl.Result{}, nil
}

// initializePhase initializes the phase for a new VirtualMachineBackupsDiscovery
func (r *VirtualMachineBackupsDiscoveryReconciler) initializePhase(ctx context.Context, logger logr.Logger, vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery) (bool, error) {
	vmbd.Status.Phase = oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseNew
	// Note: ObservedGeneration is NOT set here - only when discovery completes
	if err := r.updateStatusWithRetry(ctx, vmbd); err != nil {
		logger.Error(err, "Failed to update status to New phase")
		return false, err
	}
	logger.V(4).Info("VirtualMachineBackupsDiscovery phase initialized to New")
	return true, nil // Requeue to proceed to next step
}

// initializeDiscoveryProcess sets up the discovery process with all candidate backups
func (r *VirtualMachineBackupsDiscoveryReconciler) initializeDiscoveryProcess(ctx context.Context, logger logr.Logger, vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery) (bool, error) {
	// Set phase to InProgress when starting discovery
	vmbd.Status.Phase = oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseInProgress

	// Clear previous discovery results when restarting due to spec changes
	vmbd.Status.DiscoveryStats = nil
	vmbd.Status.BackupDiscoveryProgress = nil
	vmbd.Status.ValidBackups = nil
	vmbd.Status.InvalidBackups = nil
	vmbd.Status.Conditions = nil

	// Note: ObservedGeneration is NOT set here - only when discovery completes
	if err := r.updateStatusWithRetry(ctx, vmbd); err != nil {
		logger.Error(err, "Failed to update phase to InProgress")
		return false, err
	}
	return r.initializeDiscovery(ctx, logger, vmbd)
}

// parseTimeRange extracts and validates the start and end times from the spec
func (r *VirtualMachineBackupsDiscoveryReconciler) parseTimeRange(vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery) (startTime, endTime time.Time, err error) {
	// Handle StartTime with flexible format support
	if vmbd.Spec.StartTime != nil {
		startTime, err = vmbd.Spec.StartTime.Time()
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid startTime format: %w", err)
		}
	} else {
		startTime = time.Time{} // Beginning of time
	}

	// Handle EndTime with flexible format support
	if vmbd.Spec.EndTime != nil {
		endTime, err = vmbd.Spec.EndTime.Time()
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid endTime format: %w", err)
		}

		// If EndTime appears to be date-only (time is 00:00:00), convert to end of day
		if endTime.Hour() == 0 && endTime.Minute() == 0 && endTime.Second() == 0 && endTime.Nanosecond() == 0 {
			// Check if this was parsed from date-only format by seeing if it's exactly midnight
			year, month, day := endTime.Date()
			startOfDay := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
			if endTime.Equal(startOfDay) {
				endOfDayTime, err := vmbd.Spec.EndTime.GetEndOfDay()
				if err != nil {
					return time.Time{}, time.Time{}, fmt.Errorf("failed to convert endTime to end of day: %w", err)
				}
				endTime, err = endOfDayTime.Time()
				if err != nil {
					return time.Time{}, time.Time{}, fmt.Errorf("failed to parse end of day time: %w", err)
				}
			}
		}
	} else {
		endTime = time.Now() // Current time
	}

	return startTime, endTime, nil
}

// initializeDiscovery sets up the discovery process with all candidate backups
func (r *VirtualMachineBackupsDiscoveryReconciler) initializeDiscovery(ctx context.Context, logger logr.Logger, vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery) (bool, error) {
	logger.V(4).Info("Initializing backup discovery", "oadpNamespace", r.OADPNamespace)

	// List Velero backups from OADP namespace only
	backupList := &velerov1api.BackupList{}
	listOpts := &client.ListOptions{
		Namespace: r.OADPNamespace,
	}
	if err := r.List(ctx, backupList, listOpts); err != nil {
		logger.Error(err, "Failed to list Velero backups in OADP namespace", "namespace", r.OADPNamespace)
		// Set phase to Failed on persistent errors
		vmbd.Status.Phase = oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseFailed
		vmbd.Status.ObservedGeneration = vmbd.Generation
		if err := r.updateStatusWithRetry(ctx, vmbd); err != nil {
			return false, err
		}
		// Return requeue=false, will be retried based on controller manager settings
		return false, err
	}

	// Apply time range filtering and basic spec filtering
	var candidates []velerov1api.Backup

	// Parse time range from spec
	startTime, endTime, err := r.parseTimeRange(vmbd)
	if err != nil {
		return false, err
	}

	// Filter backups based on criteria
	for _, backup := range backupList.Items {
		// Basic validation checks
		if backup.Status.Phase != velerov1api.BackupPhaseCompleted {
			continue
		}

		// Check if backup should be included based on time range and/or explicit list
		includeByTime := !backup.CreationTimestamp.Time.Before(startTime) && !backup.CreationTimestamp.Time.After(endTime)
		includeByExplicitList := false

		// Check if backup is in the explicit list (if provided)
		if len(vmbd.Spec.RequestedBackups) > 0 {
			for _, requestedName := range vmbd.Spec.RequestedBackups {
				if backup.Name == requestedName {
					includeByExplicitList = true
					break
				}
			}
		}

		// Include backup based on selection mode
		shouldInclude := false
		hasTimeRange := vmbd.Spec.StartTime != nil || vmbd.Spec.EndTime != nil
		hasExplicitList := len(vmbd.Spec.RequestedBackups) > 0

		if hasExplicitList && hasTimeRange {
			// Combined mode: include if in explicit list OR in time range
			shouldInclude = includeByExplicitList || includeByTime
		} else if hasExplicitList {
			// Explicit list only mode: only include backups from the requested list
			shouldInclude = includeByExplicitList
		} else {
			// Time range only mode: include backups within the time range
			shouldInclude = includeByTime
		}

		if !shouldInclude {
			continue
		}

		// Create VM object for filtering
		vm := &kubevirtv1.VirtualMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmbd.Spec.VirtualMachineName,
				Namespace: vmbd.Spec.VirtualMachineNamespace,
			},
		}

		// Apply basic spec filtering (namespace inclusion/exclusion)
		if velerohelpers.ValidateVMInBackupSpec(&backup, vm) {
			candidates = append(candidates, backup)
		}
	}

	// Sort candidates by creation time (newest first)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CreationTimestamp.Time.After(candidates[j].CreationTimestamp.Time)
	})

	// Initialize discovery progress tracking
	discoveryProgress := make([]oadpv1alpha1.BackupDiscoveryInfo, len(candidates))
	for i, backup := range candidates {
		discoveryProgress[i] = oadpv1alpha1.BackupDiscoveryInfo{
			VeleroBackupInfo: oadpv1alpha1.VeleroBackupInfo{
				Name:      backup.Name,
				CreatedAt: &backup.CreationTimestamp,
			},
			Status:      oadpv1alpha1.BackupDiscoveryStatusPending,
			LastUpdated: &metav1.Time{Time: time.Now()},
		}
	}

	// Handle missing and filtered backups when explicit list is provided
	var invalidBackups []oadpv1alpha1.InvalidBackupInfo
	var missingBackupsCount int
	var filteredOutBackups []oadpv1alpha1.BackupDiscoveryInfo

	if len(vmbd.Spec.RequestedBackups) > 0 {
		// Check which requested backups exist and classify them
		requestedBackupsStatus := make(map[string]string) // name -> status: "missing", "filtered", "candidate"
		for _, requestedName := range vmbd.Spec.RequestedBackups {
			requestedBackupsStatus[requestedName] = "missing"
		}

		// Check all backups in cluster against requested list
		for _, backup := range backupList.Items {
			if _, isRequested := requestedBackupsStatus[backup.Name]; isRequested {
				// Create VM object for filtering check
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      vmbd.Spec.VirtualMachineName,
						Namespace: vmbd.Spec.VirtualMachineNamespace,
					},
				}

				// Check if backup is valid for this VM (basic spec filtering)
				if velerohelpers.ValidateVMInBackupSpec(&backup, vm) {
					requestedBackupsStatus[backup.Name] = "candidate" // Will go through discovery
				} else {
					requestedBackupsStatus[backup.Name] = "filtered" // Exists but filtered out
				}
			}
		}

		// Process the results
		for requestedName, status := range requestedBackupsStatus {
			switch status {
			case "missing":
				invalidBackups = append(invalidBackups, oadpv1alpha1.InvalidBackupInfo{
					VeleroBackupInfo: oadpv1alpha1.VeleroBackupInfo{
						Name: requestedName,
					},
					Reason: "Backup not found in cluster",
				})
				missingBackupsCount++

			case "filtered":
				// Add to filtered out backups that will show in backupDiscoveryProgress
				filteredOutBackups = append(filteredOutBackups, oadpv1alpha1.BackupDiscoveryInfo{
					VeleroBackupInfo: oadpv1alpha1.VeleroBackupInfo{
						Name: requestedName,
						// Note: We can't easily get CreatedAt for filtered backups here without another lookup
					},
					Status:      oadpv1alpha1.BackupDiscoveryStatusSkipped,
					Message:     "Backup doesn't include target VM namespace",
					LastUpdated: &metav1.Time{Time: time.Now()},
				})
				// "candidate" status backups are already in the candidates slice and will be processed normally
			}
		}

		// Always set invalidBackups for explicit requests (even if empty)
		// This ensures missing backups are properly tracked
		vmbd.Status.InvalidBackups = invalidBackups
	}

	// Combine candidate discovery progress with filtered out backups
	allDiscoveryProgress := append(discoveryProgress, filteredOutBackups...)

	// Initialize discovery stats, including missing and filtered backups in totals
	now := metav1.Time{Time: time.Now()}
	vmbd.Status.DiscoveryStats = &oadpv1alpha1.DiscoveryStatistics{
		TotalCandidates: len(candidates) + missingBackupsCount + len(filteredOutBackups),
		Pending:         len(candidates),
		InProgress:      0,
		Completed:       0,
		Skipped:         len(filteredOutBackups), // Filtered backups are immediately skipped
		Failed:          missingBackupsCount,     // Only missing backups count as failed
		StartTime:       &now,
	}
	vmbd.Status.BackupDiscoveryProgress = allDiscoveryProgress
	// Clear previous discovery results when starting fresh discovery
	vmbd.Status.ValidBackups = nil
	if len(vmbd.Spec.RequestedBackups) == 0 {
		// Only clear InvalidBackups if there are no specific requests
		// If we have specific requests, InvalidBackups may have been populated with missing backups above
		vmbd.Status.InvalidBackups = nil
	}

	if err := r.updateStatusWithRetry(ctx, vmbd); err != nil {
		logger.Error(err, "Failed to initialize discovery status")
		return false, err
	}

	// Log discovery start - condition will be set when complete
	logger.V(4).Info("Started backup discovery", "candidates", len(candidates))
	logger.V(1).Info("Discovery initialized", "candidates", len(candidates))
	return true, nil // Requeue to start processing
}

// processDiscoveryBatch continues the discovery process by checking pending backups
func (r *VirtualMachineBackupsDiscoveryReconciler) processDiscoveryBatch(ctx context.Context, logger logr.Logger, vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery) (bool, error) {
	logger.V(1).Info("Starting processDiscoveryBatch")

	// Find pending backups to process
	var pendingBackups []struct {
		idx    int
		backup oadpv1alpha1.BackupDiscoveryInfo
	}

	for i, backup := range vmbd.Status.BackupDiscoveryProgress {
		if backup.Status == oadpv1alpha1.BackupDiscoveryStatusPending {
			pendingBackups = append(pendingBackups, struct {
				idx    int
				backup oadpv1alpha1.BackupDiscoveryInfo
			}{i, backup})
		}
	}

	logger.V(1).Info("Found pending backups", "count", len(pendingBackups))

	// If no pending backups, check for stuck InProgress backups that need retry
	if len(pendingBackups) == 0 {
		logger.V(1).Info("No pending backups found, checking for stuck InProgress backups")

		// Look for InProgress backups that might be stuck
		for i, backup := range vmbd.Status.BackupDiscoveryProgress {
			if backup.Status == oadpv1alpha1.BackupDiscoveryStatusInProgress {
				logger.V(3).Info("Found stuck InProgress backup, resetting to Pending",
					"backup", backup.Name)

				// Reset stuck InProgress backup to Pending for retry
				vmbd.Status.BackupDiscoveryProgress[i].Status = oadpv1alpha1.BackupDiscoveryStatusPending
				vmbd.Status.BackupDiscoveryProgress[i].Message = "Retrying stuck backup analysis"
				now := metav1.Time{Time: time.Now()}
				vmbd.Status.BackupDiscoveryProgress[i].LastUpdated = &now

				// Update stats
				vmbd.Status.DiscoveryStats.InProgress--
				vmbd.Status.DiscoveryStats.Pending++

				if err := r.updateStatusWithRetry(ctx, vmbd); err != nil {
					logger.Error(err, "Failed to reset stuck backup status")
					return false, err
				}

				return true, nil // Requeue to process the reset backup
			}
		}

		// No pending or stuck backups, finalize discovery
		logger.V(1).Info("No pending or stuck backups, proceeding to finalization")
		return false, nil
	}

	// Process backups in batches to avoid overwhelming the system
	batchSize := constant.BackupDiscoveryBatchSize
	if len(pendingBackups) < batchSize {
		batchSize = len(pendingBackups)
	}

	logger.V(4).Info("Processing backup discovery batch", "batch_size", batchSize, "remaining", len(pendingBackups))

	// Mark batch as in progress
	now := metav1.Time{Time: time.Now()}
	for i := 0; i < batchSize; i++ {
		idx := pendingBackups[i].idx
		vmbd.Status.BackupDiscoveryProgress[idx].Status = oadpv1alpha1.BackupDiscoveryStatusInProgress
		vmbd.Status.BackupDiscoveryProgress[idx].Message = "Analyzing backup contents"
		vmbd.Status.BackupDiscoveryProgress[idx].LastUpdated = &now
	}

	// Update stats
	vmbd.Status.DiscoveryStats.Pending -= batchSize
	vmbd.Status.DiscoveryStats.InProgress += batchSize

	if err := r.updateStatusWithRetry(ctx, vmbd); err != nil {
		logger.Error(err, "Failed to update discovery progress")
		return false, err
	}

	// Process the batch concurrently
	var wg sync.WaitGroup
	results := make([]struct {
		idx     int
		status  oadpv1alpha1.BackupDiscoveryStatus
		message string
	}, batchSize)

	for i := 0; i < batchSize; i++ {
		wg.Add(1)
		idx := pendingBackups[i].idx
		backup := vmbd.Status.BackupDiscoveryProgress[idx]

		go func(idx int, backup oadpv1alpha1.BackupDiscoveryInfo) {
			defer wg.Done()

			// Check if backup contains the VM
			vm := &kubevirtv1.VirtualMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vmbd.Spec.VirtualMachineName,
					Namespace: vmbd.Spec.VirtualMachineNamespace,
				},
			}

			if r.BackupContentsReader == nil {
				results[idx%batchSize] = struct {
					idx     int
					status  oadpv1alpha1.BackupDiscoveryStatus
					message string
				}{
					idx:     idx,
					status:  oadpv1alpha1.BackupDiscoveryStatusFailed,
					message: "Backup contents reader not configured",
				}
				return
			}

			// Fetch the actual backup from the cluster
			backupObj := &velerov1api.Backup{}
			err := r.Get(ctx, client.ObjectKey{
				Name:      backup.Name,
				Namespace: r.OADPNamespace,
			}, backupObj)
			if err != nil {
				if errors.IsNotFound(err) {
					results[idx%batchSize] = struct {
						idx     int
						status  oadpv1alpha1.BackupDiscoveryStatus
						message string
					}{
						idx:     idx,
						status:  oadpv1alpha1.BackupDiscoveryStatusFailed,
						message: "Backup not found in cluster",
					}
					return
				}
				results[idx%batchSize] = struct {
					idx     int
					status  oadpv1alpha1.BackupDiscoveryStatus
					message string
				}{
					idx:     idx,
					status:  oadpv1alpha1.BackupDiscoveryStatusFailed,
					message: fmt.Sprintf("Failed to fetch backup: %v", err),
				}
				return
			}

			contains, err := r.BackupContentsReader.BackupContainsVM(ctx, backupObj, vm.Name, vm.Namespace)
			if err != nil {
				results[idx%batchSize] = struct {
					idx     int
					status  oadpv1alpha1.BackupDiscoveryStatus
					message string
				}{
					idx:     idx,
					status:  oadpv1alpha1.BackupDiscoveryStatusFailed,
					message: fmt.Sprintf("Failed to check backup contents: %v", err),
				}
				return
			}

			if contains {
				results[idx%batchSize] = struct {
					idx     int
					status  oadpv1alpha1.BackupDiscoveryStatus
					message string
				}{
					idx:     idx,
					status:  oadpv1alpha1.BackupDiscoveryStatusCompleted,
					message: "VM found in backup",
				}
			} else {
				results[idx%batchSize] = struct {
					idx     int
					status  oadpv1alpha1.BackupDiscoveryStatus
					message string
				}{
					idx:     idx,
					status:  oadpv1alpha1.BackupDiscoveryStatusSkipped,
					message: "VM not found in backup",
				}
			}
		}(idx, backup)
	}

	wg.Wait()

	// Update results
	var foundBackups []oadpv1alpha1.VeleroBackupInfo
	now = metav1.Time{Time: time.Now()}
	for i := 0; i < batchSize; i++ {
		result := results[i]
		idx := result.idx
		vmbd.Status.BackupDiscoveryProgress[idx].Status = result.status
		vmbd.Status.BackupDiscoveryProgress[idx].Message = result.message
		vmbd.Status.BackupDiscoveryProgress[idx].LastUpdated = &now

		// Add to ValidBackups if completed successfully
		if result.status == oadpv1alpha1.BackupDiscoveryStatusCompleted {
			foundBackups = append(foundBackups, vmbd.Status.BackupDiscoveryProgress[idx].VeleroBackupInfo)
		}
	}

	// Update overall discovery status
	vmbd.Status.ValidBackups = append(vmbd.Status.ValidBackups, foundBackups...)

	vmbd.Status.DiscoveryStats.InProgress -= batchSize
	// Update counts based on results
	for _, result := range results {
		switch result.status {
		case oadpv1alpha1.BackupDiscoveryStatusCompleted:
			vmbd.Status.DiscoveryStats.Completed++
		case oadpv1alpha1.BackupDiscoveryStatusSkipped:
			vmbd.Status.DiscoveryStats.Skipped++
		case oadpv1alpha1.BackupDiscoveryStatusFailed:
			vmbd.Status.DiscoveryStats.Failed++
		}
	}

	if err := r.updateStatusWithRetry(ctx, vmbd); err != nil {
		logger.Error(err, "Failed to update discovery results")
		return false, err
	}

	logger.V(1).Info("Processed discovery batch", "completed", len(foundBackups))

	// Continue processing remaining backups
	return true, nil // Requeue to continue processing
}

// finalizeDiscoveryProcess completes the discovery process
func (r *VirtualMachineBackupsDiscoveryReconciler) finalizeDiscoveryProcess(ctx context.Context, logger logr.Logger, vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery) (bool, error) {

	logger.V(4).Info("Finalizing discovery process")

	// Set completion time
	now := metav1.Time{Time: time.Now()}
	vmbd.Status.DiscoveryStats.CompletionTime = &now

	// Separate valid and invalid backups for ExplicitList mode
	var validBackups []oadpv1alpha1.VeleroBackupInfo
	var invalidBackups []oadpv1alpha1.InvalidBackupInfo

	// Track which requested backups we've found during discovery
	foundRequestedBackups := make(map[string]bool)
	if len(vmbd.Spec.RequestedBackups) > 0 {
		for _, requestedName := range vmbd.Spec.RequestedBackups {
			foundRequestedBackups[requestedName] = false
		}
	}

	for _, backup := range vmbd.Status.BackupDiscoveryProgress {
		if backup.Status == oadpv1alpha1.BackupDiscoveryStatusCompleted {
			validBackups = append(validBackups, backup.VeleroBackupInfo)
		} else {
			// Add both failed and skipped backups to invalidBackups
			// (both are invalid for VM file restoration purposes)
			invalidBackups = append(invalidBackups, oadpv1alpha1.InvalidBackupInfo{
				VeleroBackupInfo: backup.VeleroBackupInfo,
				Reason:           backup.Message,
			})
		}

		// Mark as found if it was explicitly requested (applies to all non-completed backups)
		if len(vmbd.Spec.RequestedBackups) > 0 {
			for _, requestedName := range vmbd.Spec.RequestedBackups {
				if backup.Name == requestedName {
					foundRequestedBackups[requestedName] = true
					break
				}
			}
		}
	}

	// Preserve existing invalid backups that are still in the requested list
	// This includes missing backups that were detected during initialization
	var relevantExistingInvalidBackups []oadpv1alpha1.InvalidBackupInfo
	if len(vmbd.Spec.RequestedBackups) > 0 {
		requestedBackupsSet := make(map[string]bool)
		for _, requestedName := range vmbd.Spec.RequestedBackups {
			requestedBackupsSet[requestedName] = true
		}

		// Include existing invalid backups that are still requested
		for _, existingInvalid := range vmbd.Status.InvalidBackups {
			if requestedBackupsSet[existingInvalid.Name] {
				relevantExistingInvalidBackups = append(relevantExistingInvalidBackups, existingInvalid)
			}
		}
	}

	// Add any missing requested backups that weren't processed during discovery
	missingBackups := []oadpv1alpha1.InvalidBackupInfo{}
	if len(vmbd.Spec.RequestedBackups) > 0 {
		// Track which requested backups were found during discovery
		processedBackups := make(map[string]bool)
		for _, backup := range vmbd.Status.BackupDiscoveryProgress {
			processedBackups[backup.Name] = true
		}

		// Track which backups are already in preserved existing invalid backups to avoid duplicates
		existingInvalidBackups := make(map[string]bool)
		for _, existingInvalid := range relevantExistingInvalidBackups {
			existingInvalidBackups[existingInvalid.Name] = true
		}

		// Add missing requested backups to invalidBackups (only if not already preserved)
		for _, requestedName := range vmbd.Spec.RequestedBackups {
			if !processedBackups[requestedName] && !existingInvalidBackups[requestedName] {
				missingBackups = append(missingBackups, oadpv1alpha1.InvalidBackupInfo{
					VeleroBackupInfo: oadpv1alpha1.VeleroBackupInfo{
						Name: requestedName,
					},
					Reason: "Backup not found in cluster",
				})
			}
		}
	}

	// Remove duplicates from invalidBackups that might already be in relevantExistingInvalidBackups
	existingInvalidBackupsSet := make(map[string]bool)
	for _, existingInvalid := range relevantExistingInvalidBackups {
		existingInvalidBackupsSet[existingInvalid.Name] = true
	}

	var dedupedInvalidBackups []oadpv1alpha1.InvalidBackupInfo
	for _, invalid := range invalidBackups {
		if !existingInvalidBackupsSet[invalid.Name] {
			dedupedInvalidBackups = append(dedupedInvalidBackups, invalid)
		}
	}

	// Merge existing + discovery + missing backups
	allInvalidBackups := append(relevantExistingInvalidBackups, dedupedInvalidBackups...)
	allInvalidBackups = append(allInvalidBackups, missingBackups...)

	// Update final status
	vmbd.Status.ValidBackups = validBackups
	vmbd.Status.InvalidBackups = allInvalidBackups

	// Set final phase and discovery complete condition
	vmbd.Status.Phase = oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseCompleted
	vmbd.Status.ObservedGeneration = vmbd.Generation

	// Set discovery condition without calling Status().Update()
	if len(validBackups) > 0 {
		condition := metav1.Condition{
			Type:               string(oadpv1alpha1.VirtualMachineBackupsDiscoveryConditionComplete),
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "DiscoverySuccessful",
			Message:            fmt.Sprintf("Successfully discovered %d valid backups", len(validBackups)),
		}
		meta.SetStatusCondition(&vmbd.Status.Conditions, condition)
		logger.V(4).Info("Discovery completed successfully", "validBackups", len(validBackups))
	} else {
		condition := metav1.Condition{
			Type:               string(oadpv1alpha1.VirtualMachineBackupsDiscoveryConditionComplete),
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "NoValidBackups",
			Message:            "No backups found containing the specified virtual machine",
		}
		meta.SetStatusCondition(&vmbd.Status.Conditions, condition)
		logger.V(4).Info("Discovery completed but no valid backups found")
	}

	// Get fresh object before final status update to avoid conflicts
	freshVmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
	if err := r.Get(ctx, client.ObjectKey{Name: vmbd.Name, Namespace: vmbd.Namespace}, freshVmbd); err != nil {
		logger.Error(err, "Failed to get fresh object before finalization update")
		return false, err
	}

	// Apply all the calculated changes to the fresh object
	if freshVmbd.Status.DiscoveryStats != nil {
		freshVmbd.Status.DiscoveryStats.CompletionTime = &now
	}
	freshVmbd.Status.ValidBackups = validBackups
	freshVmbd.Status.InvalidBackups = allInvalidBackups
	freshVmbd.Status.Phase = oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseCompleted
	freshVmbd.Status.ObservedGeneration = freshVmbd.Generation

	// Set discovery condition on fresh object
	if len(validBackups) > 0 {
		condition := metav1.Condition{
			Type:               string(oadpv1alpha1.VirtualMachineBackupsDiscoveryConditionComplete),
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "DiscoverySuccessful",
			Message:            fmt.Sprintf("Successfully discovered %d valid backups", len(validBackups)),
		}
		meta.SetStatusCondition(&freshVmbd.Status.Conditions, condition)
		logger.V(4).Info("Discovery completed successfully", "validBackups", len(validBackups))
	} else {
		condition := metav1.Condition{
			Type:               string(oadpv1alpha1.VirtualMachineBackupsDiscoveryConditionComplete),
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "NoValidBackups",
			Message:            "No backups found containing the specified virtual machine",
		}
		meta.SetStatusCondition(&freshVmbd.Status.Conditions, condition)
		logger.V(4).Info("Discovery completed but no valid backups found")
	}

	// Single status update with all final changes on fresh object using retry mechanism
	if err := r.updateStatusWithRetry(ctx, freshVmbd); err != nil {
		logger.Error(err, "Failed to finalize discovery status")
		return false, err
	}

	return false, nil // Discovery complete, no requeue needed
}

// checkForSpecChanges checks if the resource spec has changed and takes appropriate action
func (r *VirtualMachineBackupsDiscoveryReconciler) checkForSpecChanges(ctx context.Context, logger logr.Logger, vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery) (bool, error) {
	// Check if observedGeneration matches current generation
	if vmbd.Status.ObservedGeneration != vmbd.Generation {
		logger.V(4).Info("Spec has changed (generation mismatch), restarting discovery",
			"observedGeneration", vmbd.Status.ObservedGeneration,
			"currentGeneration", vmbd.Generation)

		// Reset status to restart discovery
		vmbd.Status.Phase = oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseNew
		vmbd.Status.ObservedGeneration = 0 // Reset to indicate processing has not started
		vmbd.Status.DiscoveryStats = nil
		vmbd.Status.BackupDiscoveryProgress = nil
		vmbd.Status.ValidBackups = nil
		vmbd.Status.InvalidBackups = nil
		vmbd.Status.Conditions = nil

		if err := r.updateStatusWithRetry(ctx, vmbd); err != nil {
			logger.Error(err, "Failed to reset status for spec change")
			return false, err
		}

		return true, nil // Requeue to start fresh discovery
	}

	// TEMPORARY FIX DISABLED - was causing infinite loop
	// Will manually reset the resource instead

	logger.V(1).Info("No spec changes detected, discovery remains completed")
	return false, nil // No requeue needed
}

// isDiscoveryComplete checks if backup discovery has finished
func (r *VirtualMachineBackupsDiscoveryReconciler) isDiscoveryComplete(vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery) bool {
	if vmbd.Status.DiscoveryStats == nil {
		return false
	}

	stats := vmbd.Status.DiscoveryStats
	// Discovery is complete only if no pending/in-progress work AND phase is Completed
	return stats.Pending == 0 && stats.InProgress == 0 && vmbd.Status.Phase == oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseCompleted
}

// updateStatusWithRetry performs a status update with retry on optimistic locking conflicts
func (r *VirtualMachineBackupsDiscoveryReconciler) updateStatusWithRetry(ctx context.Context, vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery) error {
	// Try up to 3 times to handle optimistic locking conflicts
	for i := 0; i < 3; i++ {
		if err := r.Status().Update(ctx, vmbd); err != nil {
			// If it's an optimistic locking conflict, fetch fresh object and retry
			if errors.IsConflict(err) && i < 2 { // Only retry if not the last attempt
				// Get the fresh object
				freshVmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
				if err := r.Get(ctx, client.ObjectKey{Name: vmbd.Name, Namespace: vmbd.Namespace}, freshVmbd); err != nil {
					return err
				}

				// Copy the current status to the fresh object
				freshVmbd.Status = vmbd.Status
				vmbd = freshVmbd
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("failed to update status after 3 retries")
}

// SetupWithManager sets up the controller with the Manager.
func (r *VirtualMachineBackupsDiscoveryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&oadpv1alpha1.VirtualMachineBackupsDiscovery{}).
		Watches(&velerov1api.Backup{}, handler.EnqueueRequestsFromMapFunc(r.mapBackupToVMBD)).
		Named("virtualmachinebackupsdiscovery").
		Complete(r)
}

// mapBackupToVMBD maps Backup changes to VirtualMachineBackupsDiscovery reconcile requests
func (r *VirtualMachineBackupsDiscoveryReconciler) mapBackupToVMBD(ctx context.Context, obj client.Object) []ctrl.Request {
	// Only trigger reconciliation for backups in the OADP namespace
	if obj.GetNamespace() != r.OADPNamespace {
		return nil
	}

	// Find all VirtualMachineBackupsDiscovery resources that might be affected
	vmbdList := &oadpv1alpha1.VirtualMachineBackupsDiscoveryList{}
	if err := r.List(ctx, vmbdList); err != nil {
		return nil
	}

	requests := make([]ctrl.Request, 0, len(vmbdList.Items))
	for _, vmbd := range vmbdList.Items {
		// Trigger reconciliation for all VMBD resources when backups change
		// This ensures discovery stays current with backup availability
		requests = append(requests, ctrl.Request{
			NamespacedName: client.ObjectKey{
				Name:      vmbd.Name,
				Namespace: vmbd.Namespace,
			},
		})
	}

	return requests
}

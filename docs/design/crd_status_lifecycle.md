# CRD Status Lifecycle Design

**Status**: Approved
**Version**: 2.0 (Velero-Aligned)
**Last Updated**: 2025-10-05
**Authors**: OADP VM File Restore Team

---

## Overview

This document defines the status lifecycle for OADP VM File Restore CRDs using:
- **Phases** (Velero-style, human-readable, secondary)
- **Conditions** (Kubernetes-standard, machine-usable, primary source of truth)

**Affected CRDs**:
- **VirtualMachineBackupsDiscovery** (`vmbd`) - Discovers which Velero backups contain specific VMs
- **VirtualMachineFileRestore** (`vmfr`) - Restores and serves files from discovered VM backups

---

## Design Principles

> Note: Phase names match Velero conventions (InProgress), while condition names match Kubernetes conventions (Progressing). This deliberate difference avoids confusion with upstream APIs.

### 1. Velero-Aligned Phases

Both CRDs use **identical Velero-style phases**:

| Phase | Meaning |
|-------|---------|
| `New` | Object just created, not yet processed |
| `InProgress` | Discovery/restore actively running |
| `Completed` | Succeeded with no errors (perfect) |
| `PartiallyFailed` | Completed but degraded (usable with partial errors) |
| `Failed` | Unrecoverable error, nothing usable |
| `Deleting` | Cleanup in progress (VMFR only) |

### 2. Kubernetes-Standard Conditions

Both CRDs use **identical standard conditions**:

| Condition | Meaning |
|-----------|---------|
| `Progressing` | Operation is actively running |
| `Available` | Resource has usable data/functionality |
| `Degraded` | Partial failures occurred (may still be usable) |
| `Ready` | Summary: resource is usable (Available=True) |

### 3. Condition-First Design

- **Conditions** are the source of truth (automation consumes these)
- **Phases** are derived from conditions (human readability)
- Update conditions first, then derive phase

### 4. Ready = Usable (Not Perfect)

- `Ready=True` for `Completed` (perfect, no issues)
- `Ready=True` for `PartiallyFailed` (degraded but usable)
- `Ready=False` for `New`, `InProgress`, `Failed`, `Deleting`

---

## Phase ↔ Condition Mapping

| Phase | Progressing | Available | Degraded | Ready | Interpretation |
|-------|-------------|-----------|----------|-------|----------------|
| **New** | False | False | False | False | 📝 Just created, not started |
| **InProgress** | **True** | False | False | False | ⏳ Actively running |
| **Completed** | False | **True** | False | **True** | ✅ Perfect success |
| **PartiallyFailed** | False | **True** | **True** | **True** | ⚠️ Usable but degraded |
| **Failed** | False | False | **True** | False | ❌ Complete failure |
| **Deleting** | False | False | False | False | 🗑️ Cleanup in progress |

**Key Insights**:
- **Progressing=True** only during InProgress
- **Available=True** for both Completed and PartiallyFailed (usable)
- **Degraded=True** for PartiallyFailed and Failed (problems occurred)
- **Ready=True** when usable (Completed or PartiallyFailed)
- **PartiallyFailed vs Failed**: Available=True (has usable data) vs Available=False (nothing usable)

---

## API Specifications

### VirtualMachineBackupsDiscovery

```go
// Phases (Velero-aligned)
// +kubebuilder:validation:Enum=New;InProgress;Completed;PartiallyFailed;Failed
type VirtualMachineBackupsDiscoveryPhase string

const (
    VirtualMachineBackupsDiscoveryPhaseNew             = "New"
    VirtualMachineBackupsDiscoveryPhaseInProgress      = "InProgress"
    VirtualMachineBackupsDiscoveryPhaseCompleted       = "Completed"
    VirtualMachineBackupsDiscoveryPhasePartiallyFailed = "PartiallyFailed"
    VirtualMachineBackupsDiscoveryPhaseFailed          = "Failed"
)

// Conditions: Progressing, Available, Degraded, Ready (standard K8s types)
// No custom condition enum - use metav1.Condition with Type field

type VirtualMachineBackupsDiscoveryStatus struct {
    // Phase derived from conditions for human readability
    Phase VirtualMachineBackupsDiscoveryPhase `json:"phase,omitempty"`

    // ObservedGeneration - set at START of reconciliation
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`

    // Conditions - PRIMARY source of truth
    // +listType=map
    // +listMapKey=type
    Conditions []metav1.Condition `json:"conditions,omitempty"`

    ValidBackups            []types.VeleroBackupInfo        `json:"validBackups,omitempty"`
    InvalidBackups          []types.InvalidBackupInfo       `json:"invalidBackups,omitempty"`
    BackupDiscoveryProgress []types.BackupDiscoveryProgress `json:"backupDiscoveryProgress,omitempty"`
    DiscoveryStats          *DiscoveryStatistics            `json:"discoveryStats,omitempty"`
}
```

**Deletion**: No `Deleting` phase (read-only resource, no finalizers needed)

---

### VirtualMachineFileRestore

```go
// Phases (Velero-aligned)
// +kubebuilder:validation:Enum=New;InProgress;Completed;PartiallyFailed;Failed;Deleting
type VirtualMachineFileRestorePhase string

const (
    VirtualMachineFileRestorePhaseNew             = "New"
    VirtualMachineFileRestorePhaseInProgress      = "InProgress"
    VirtualMachineFileRestorePhaseCompleted       = "Completed"
    VirtualMachineFileRestorePhasePartiallyFailed = "PartiallyFailed"
    VirtualMachineFileRestorePhaseFailed          = "Failed"
    VirtualMachineFileRestorePhaseDeleting        = "Deleting"
)

// Conditions: Progressing, Available, Degraded, Ready (standard K8s types)

type VirtualMachineFileRestoreStatus struct {
    Phase              VirtualMachineFileRestorePhase `json:"phase,omitempty"`
    ObservedGeneration int64                          `json:"observedGeneration,omitempty"`

    // +listType=map
    // +listMapKey=type
    Conditions []metav1.Condition `json:"conditions,omitempty"`

    FileServingInfo  *FileServingInfo  `json:"fileServingInfo,omitempty"`
    PVCRestores      []PVCRestoreInfo  `json:"pvcRestores,omitempty"`
    CreatedNamespace string            `json:"createdNamespace,omitempty"`
}
```

**Deletion**: Uses `Deleting` phase (creates child resources, needs finalizers)

---

## Condition Reference

### Progressing

- **True**: Operation in progress (Phase: InProgress)
- **False**: Not running (Phase: New, Completed, PartiallyFailed, Failed, Deleting)
- **Reasons**: `ScanningBackups`, `Validating`, `CreatingRestores`, `WaitingForRestores`, `DiscoveryCompleted`

### Available

- **True**: Resource is usable (Phase: Completed or PartiallyFailed)
- **False**: Not usable (Phase: New, InProgress, Failed)
- **Reasons**: `ValidBackupsFound`, `FileServingReady`, `InProgress`, `NoValidBackupsFound`, `Failed`

**Key**: PartiallyFailed → Available=True (some data usable), Failed → Available=False (nothing usable)

### Degraded

- **True**: Partial failures occurred (Phase: PartiallyFailed or Failed)
- **False**: No issues (Phase: Completed)
- **Reasons**: `PartialFailure`, `SomeBackupsInvalid`, `AllRestoresSucceeded`, `NoFailures`

**Important**: Degraded=True does NOT always mean unusable! When Available=True AND Degraded=True → "usable but not perfect"

### Ready

- **True**: Resource is usable (Phase: Completed or PartiallyFailed)
- **False**: Not usable (Phase: New, InProgress, Failed, Deleting)
- **Calculation**: `Ready = Available`

---

## Phase Derivation Logic

```go
func DerivePhase(conditions []metav1.Condition, deletionTimestamp *metav1.Time) Phase {
    // VMFR only: Check deletion first
    if deletionTimestamp != nil {
        return PhaseDeleting
    }

    progressing := meta.FindStatusCondition(conditions, "Progressing")
    available := meta.FindStatusCondition(conditions, "Available")
    degraded := meta.FindStatusCondition(conditions, "Degraded")

    // InProgress: actively progressing
    if progressing.Status == metav1.ConditionTrue {
        return PhaseInProgress
    }

    // Completed: available, not degraded
    if available.Status == metav1.ConditionTrue && degraded.Status == metav1.ConditionFalse {
        return PhaseCompleted
    }

    // PartiallyFailed: available but degraded
    if available.Status == metav1.ConditionTrue && degraded.Status == metav1.ConditionTrue {
        return PhasePartiallyFailed
    }

    // Failed: not available, degraded
    if available.Status == metav1.ConditionFalse && degraded.Status == metav1.ConditionTrue {
        return PhaseFailed
    }

    // Default: New
    return PhaseNew
}
```

---

## Status Examples

### Example 1: InProgress

```yaml
status:
  phase: InProgress
  conditions:
    - type: Progressing
      status: "True"
      reason: ScanningBackups
      message: "Scanning 15 candidate backups"
    - type: Available
      status: "False"
      reason: InProgress
    - type: Degraded
      status: "False"
      reason: NoFailures
    - type: Ready
      status: "False"
      reason: InProgress
```

---

### Example 2: Completed (Perfect Success)

```yaml
status:
  phase: Completed
  conditions:
    - type: Progressing
      status: "False"
      reason: DiscoveryCompleted
    - type: Available
      status: "True"
      reason: ValidBackupsFound
      message: "Found 5 valid backups"
    - type: Degraded
      status: "False"
      reason: AllBackupsValid
    - type: Ready
      status: "True"
      reason: DiscoveryComplete
  validBackups: [...]
```

**Interpretation**: ✅ Perfect - Ready=True, Degraded=False

---

### Example 3: PartiallyFailed (Usable but Degraded)

```yaml
status:
  phase: PartiallyFailed
  conditions:
    - type: Progressing
      status: "False"
      reason: DiscoveryCompleted
    - type: Available
      status: "True"
      reason: ValidBackupsFound
      message: "Found 3 valid backups, 5 failed"
    - type: Degraded
      status: "True"
      reason: PartialFailure
      message: "5 of 8 backups failed to scan"
    - type: Ready
      status: "True"
      reason: DiscoveryComplete
  validBackups: [...]  # 3 backups
  invalidBackups: [...] # 5 backups
```

**Interpretation**: ⚠️ Usable but degraded - Ready=True (usable), Degraded=True (has issues)

---

### Example 4: Failed (Complete Failure)

```yaml
status:
  phase: Failed
  conditions:
    - type: Progressing
      status: "False"
      reason: DiscoveryFailed
    - type: Available
      status: "False"
      reason: NoBackupStorageLocations
    - type: Degraded
      status: "True"
      reason: CriticalFailure
    - type: Ready
      status: "False"
      reason: Failed
```

**Interpretation**: ❌ Complete failure - Ready=False, nothing usable

---

## Implementation Guidelines

### Controller Pattern

```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    obj := &v1alpha1.VirtualMachineBackupsDiscovery{}
    if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 1. Set ObservedGeneration at START
    obj.Status.ObservedGeneration = obj.Generation

    // 2. Update conditions based on reconciliation logic
    meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
        Type:    "Progressing",
        Status:  metav1.ConditionTrue, // or False
        Reason:  "ScanningBackups",
        Message: "Scanning backups...",
    })
    // Set Available, Degraded, Ready conditions...

    // 3. Derive phase from conditions
    obj.Status.Phase = DerivePhase(obj.Status.Conditions, nil)

    // 4. Update status
    return ctrl.Result{}, r.Status().Update(ctx, obj)
}
```

### Handling Partial Failures

```go
if validCount > 0 && failedCount > 0 {
    // PartiallyFailed: Available=True, Degraded=True, Ready=True
    meta.SetStatusCondition(&conditions, metav1.Condition{
        Type:    "Available",
        Status:  metav1.ConditionTrue,
        Reason:  "ValidBackupsFound",
        Message: fmt.Sprintf("Found %d valid backups", validCount),
    })
    meta.SetStatusCondition(&conditions, metav1.Condition{
        Type:    "Degraded",
        Status:  metav1.ConditionTrue,
        Reason:  "PartialFailure",
        Message: fmt.Sprintf("%d of %d backups failed", failedCount, totalCount),
    })
    meta.SetStatusCondition(&conditions, metav1.Condition{
        Type:   "Ready",
        Status: metav1.ConditionTrue,
        Reason: "DiscoveryComplete",
    })
}
```

### Handling Complete Failures

```go
if validCount == 0 {
    // Failed: Available=False, Degraded=True, Ready=False
    meta.SetStatusCondition(&conditions, metav1.Condition{
        Type:    "Available",
        Status:  metav1.ConditionFalse,
        Reason:  "NoValidBackupsFound",
    })
    meta.SetStatusCondition(&conditions, metav1.Condition{
        Type:    "Degraded",
        Status:  metav1.ConditionTrue,
        Reason:  "CriticalFailure",
    })
    meta.SetStatusCondition(&conditions, metav1.Condition{
        Type:   "Ready",
        Status: metav1.ConditionFalse,
        Reason: "Failed",
    })
}
```

---

## API Consumer Patterns

### Check Readiness

```go
import "k8s.io/apimachinery/pkg/api/meta"

// Check if usable (even if degraded)
func IsReady(obj *v1alpha1.VirtualMachineBackupsDiscovery) bool {
    cond := meta.FindStatusCondition(obj.Status.Conditions, "Ready")
    return cond != nil && cond.Status == metav1.ConditionTrue
}

// Check if perfect (no degradation)
func IsCompleted(obj *v1alpha1.VirtualMachineBackupsDiscovery) bool {
    return obj.Status.Phase == v1alpha1.VirtualMachineBackupsDiscoveryPhaseCompleted
}

// Check if degraded but usable
func IsPartiallyFailed(obj *v1alpha1.VirtualMachineBackupsDiscovery) bool {
    return obj.Status.Phase == v1alpha1.VirtualMachineBackupsDiscoveryPhasePartiallyFailed
}
```

### kubectl Examples

```bash
# Check phase
kubectl get vmbd my-discovery -o jsonpath='{.status.phase}'

# Check if usable (Ready condition)
kubectl get vmbd my-discovery -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}'

# Wait for ready (usable, may be degraded)
kubectl wait --for=condition=Ready vmbd/my-discovery --timeout=5m

# Check if degraded
kubectl get vmbd my-discovery -o jsonpath='{.status.conditions[?(@.type=="Degraded")].status}'
```

---

## Migration from Previous Design

| Old | New (Velero-Aligned) | Action |
|-----|---------------------|--------|
| Phase: `Pending` | Phase: `New` | Update clients |
| Phase: `Progressing`/`Running` | Phase: `InProgress` | Update clients |
| Phase: `Completed` | Phase: `Completed` | ✅ No change |
| Phase: `PartiallyCompleted` | Phase: `PartiallyFailed` | Update clients |
| Phase: `Failed` | Phase: `Failed` | ✅ No change |
| VMFR: `Validating`, `Processing`, `BackingOff`, `Created`, `Running` | ❌ Removed | Use `InProgress` + condition reasons |
| Condition: `Complete` | Conditions: `Available` + `Ready` | Update clients |
| VMFR: `PVCsDiscovered`, `RestoresCreated`, `RestoresCompleted` | ❌ Removed | Use status fields or condition reasons |

### Migration Checklist

**Controller**:
- [ ] Update phase enums to Velero names
- [ ] Remove custom conditions
- [ ] Add standard conditions (Progressing, Available, Degraded, Ready)
- [ ] Implement phase derivation logic
- [ ] Set ObservedGeneration at reconciliation start
- [ ] Run `make manifests generate`

**API Consumers**:
- [ ] Update phase checks
- [ ] Update condition checks
- [ ] Handle PartiallyFailed phase
- [ ] Update kubectl wait commands

---

## References

**Kubernetes**:
- [API Conventions - Status Properties](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties)
- [KEP - Standardize Conditions](https://github.com/kubernetes/enhancements/tree/master/keps/sig-api-machinery/1623-standardize-conditions)

**Velero**:
- [Backup API](https://velero.io/docs/main/api-types/backup/) - Phases: New, InProgress, Completed, PartiallyFailed, Failed
- [Restore API](https://velero.io/docs/main/api-types/restore/)

---

## Quick Reference

### Phases (Both CRDs)

**VMBD**: `New`, `InProgress`, `Completed`, `PartiallyFailed`, `Failed`
**VMFR**: `New`, `InProgress`, `Completed`, `PartiallyFailed`, `Failed`, `Deleting`

### Conditions (Both CRDs)

| Condition | True When | False When |
|-----------|-----------|------------|
| Progressing | InProgress | All other phases |
| Available | Completed or PartiallyFailed | New, InProgress, Failed |
| Degraded | PartiallyFailed or Failed | Completed |
| Ready | Completed or PartiallyFailed | New, InProgress, Failed |

### Decision Tree

```
Is resource usable?
  → Check Ready=True (Completed or PartiallyFailed)

Is resource perfect?
  → Check Phase=Completed (Ready=True, Degraded=False)

Is resource degraded but usable?
  → Check Phase=PartiallyFailed (Ready=True, Degraded=True)

Is operation still running?
  → Check Progressing=True (Phase=InProgress)
```

---

**Document End**

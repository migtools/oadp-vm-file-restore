# VM Backup Discovery Mechanism

## Abstract
The VirtualMachineBackupsDiscovery (VMBD) CRD provides a dedicated resource for discovering which Velero backups contain a specific virtual machine.
It supports both time-based filtering and explicit backup lists, with deep content validation to ensure accurate results.

## Goals
- Dedicated CRD for VM backup discovery independent from file restoration
- Support time-based filtering and explicit backup lists
- Validate backup contents to ensure VM presence (no false positives)
- Comprehensive status reporting with progress tracking
- Reusable discovery results for multiple file restoration operations

## Design Overview
The VMBD CRD runs a discovery workflow:
1. **Selection**: Filter backups by time range and/or explicit list
2. **Validation**: Check backup specs and contents for VM presence
3. **Results**: Populate ValidBackups and InvalidBackups lists with detailed status

## Detailed Design

## API Structure

### Spec
- `VirtualMachineName` and `VirtualMachineNamespace`: Target VM to discover
- `StartTime`/`EndTime`: Optional time range (supports YYYY-MM-DD or RFC3339)
- `RequestedBackups`: Optional list of specific backup names

### Status
- `Phase`: Discovery state (New, InProgress, Completed, Failed)
- `ValidBackups`: Backups containing the VM
- `InvalidBackups`: Requested backups that don't contain VM or weren't found
- `BackupDiscoveryProgress`: Per-backup status with timestamps
- `DiscoveryStats`: Summary statistics

### FlexibleTime Support
The API supports user-friendly time input:
- Date-only: `"2024-01-01"` (auto-converts to start/end of day)
- Full RFC3339: `"2024-01-01T12:30:45Z"` (used as-is)
- Mixed usage supported for flexible range specification

## Discovery Workflow

### Selection Logic
**Exclusive mode selection**: Choose between time range OR explicit list mode
- **Time Range Mode**: When `StartTime`/`EndTime` provided, filter by `backup.CreationTimestamp`
- **Explicit List Mode**: When `RequestedBackups` provided, only include specified backups
- **Combined Mode**: When both provided, include backups in time range OR explicit list
- Missing requested backups are tracked in InvalidBackups

### Validation Steps
1. **Spec validation**: Check Velero backup namespace inclusion/exclusion rules
2. **Status check**: Only process completed backups (`Phase == Completed`)
3. **Content validation**: Use `VeleroBackupContentsReader` for accurate inspection
   - Downloads backup resource list from object storage via Velero APIs
   - Parses actual backup contents to verify VM presence
   - Provides accurate results with proper error handling

### Validation Accuracy and Performance
The discovery process uses a two-stage validation approach optimized for both accuracy and performance:

**Stage 1: Fast Spec-based Validation**
- Check Velero backup namespace inclusion/exclusion rules
- Filters out obviously invalid backups without network calls
- Performance benefit: In-memory validation prevents unnecessary object storage access

**Stage 2: Content Validation via Velero APIs**
- Downloads backup resource list from object storage using Velero's download mechanisms
- Parses actual backed-up resources to verify VM presence
- Uses same approach as `velero describe backup` command
- Provides accurate results with graceful error handling

**Performance Strategy:**
- Stage 1 filtering ensures only promising candidates undergo Stage 2 validation
- Batched, concurrent processing minimizes network latency impact
- Configurable batch sizes for resource control
- Progress tracking with incremental status updates

### Result Categories
- **ValidBackups**: Backups confirmed to contain the VM
- **InvalidBackups**: Requested backups that don't contain VM or weren't found
- **Statistics**: Complete counts and timing information
- **Progress**: Per-backup status with detailed messages

## Integration

### VMBD Controller
Reconciles VirtualMachineBackupsDiscovery resources through:
1. Fetch and validate spec
2. Get candidate backups from cluster
3. Apply selection and validation logic
4. Update status with results

### VMFR Integration
VirtualMachineFileRestore resources reference completed VMBD resources:
- Reference VMBD by name in same namespace
- Wait for discovery completion
- Use ValidBackups list for file serving

## Example Usage

### Time-based Discovery
Discover all backups containing a VM within a specific time range:

```yaml
apiVersion: oadp.openshift.io/v1alpha1
kind: VirtualMachineBackupsDiscovery
metadata:
  name: monthly-discovery
  namespace: openshift-adp
spec:
  virtualMachineName: "production-vm"
  virtualMachineNamespace: "apps"
  startTime: "2024-01-01"           # Date-only format
  endTime: "2024-01-31T23:59:59Z"   # Full RFC3339 format
```

### Explicit Backup List Discovery
Discover specific backups by name:

```yaml
apiVersion: oadp.openshift.io/v1alpha1
kind: VirtualMachineBackupsDiscovery
metadata:
  name: incident-investigation
  namespace: openshift-adp
spec:
  virtualMachineName: "production-vm"
  virtualMachineNamespace: "apps"
  requestedBackups:
    - "backup-before-upgrade"
    - "backup-after-incident"
    - "emergency-backup-jan15"
```

### Combined Time Range and Explicit List
Union approach - includes backups that match time range OR are explicitly requested:

```yaml
apiVersion: oadp.openshift.io/v1alpha1
kind: VirtualMachineBackupsDiscovery
metadata:
  name: comprehensive-discovery
  namespace: openshift-adp
spec:
  virtualMachineName: "production-vm"
  virtualMachineNamespace: "apps"
  startTime: "2024-01-15"
  endTime: "2024-01-20"
  requestedBackups:
    - "backup-before-upgrade"    # Included even if outside time range
    - "special-checkpoint"       # Included even if outside time range
```

### Example Status Results

```yaml
status:
  phase: Completed
  observedGeneration: 1
  conditions:
    - type: Complete
      status: "True"
      reason: DiscoverySuccessful
      message: "Found 3 valid backups containing the VM"
  validBackups:
    - name: "backup-2024-01-16"
      createdAt: "2024-01-16T10:00:00Z"
    - name: "backup-2024-01-18"
      createdAt: "2024-01-18T10:00:00Z"
    - name: "backup-before-upgrade"
      createdAt: "2024-01-10T15:30:00Z"
  invalidBackups:
    - name: "special-checkpoint"
      reason: "VM not found in backup contents"
      createdAt: "2024-01-12T09:00:00Z"
  discoveryStats:
    totalCandidates: 6
    completed: 4
    skipped: 2
    failed: 0
    startTime: "2024-01-25T14:00:00Z"
    completionTime: "2024-01-25T14:02:30Z"
```

## Key Design Decisions

### Spec Version Tracking
When a discovery process begins, the controller records the current `observedGeneration` in the Status. This enables the system to detect if the Status reflects the latest Spec, and to determine when a rediscovery is needed due to changes in the Spec.

### Error Handling
- Individual backup failures don't fail entire discovery
- Missing backups tracked in InvalidBackups with reasons
- ObservedGeneration tracks spec changes for staleness detection

## Security and Compliance

### RBAC
Controller requires read-only access to Velero Backup resources.

### Backup Access
Uses same security model as Velero for backup storage access.
Only reads metadata and contents, never modifies backups.

### Velero Compatibility
Designed for OpenShift Velero fork used in OADP.
Uses standard backup metadata format.

## Future Enhancements
- Automatic garbage collection or based on TTLs

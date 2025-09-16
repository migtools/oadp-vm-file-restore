# VM File Restore Mechanism

## Abstract
The VirtualMachineFileRestore (VMFR) CRD creates file serving resources from validated backup discoveries.
It references completed VirtualMachineBackupsDiscovery (VMBD) resources to provide file access without re-discovering backups.

## Goals
- Reference completed backup discoveries for file serving
- Support selective backup serving from discovery results
- Maintain clear lifecycle management for file serving resources

## Design Overview
VMFR acts as a lightweight orchestrator:
1. **Reference**: Point to completed VMBD resource in same namespace
2. **Validate**: Ensure discovery is complete and backups are available
3. **Select**: Optionally choose specific backups from discovery results
4. **Serve**: Create file serving infrastructure (TODO: implementation pending)

## API Structure

### Spec
- `BackupsDiscoveryRef`: Name of VMBD resource in same namespace
- `SelectedBackups`: Optional list of specific backup names from discovery results
  - If empty, uses all valid backups from discovery
  - Must exist in ValidBackups list of referenced VMBD

### Status
- `Phase`: Lifecycle state (New, BackingOff, Created, Deleting)
- `Conditions`: Standard Kubernetes conditions (Ready)
- `FileServingInfo`: Details about created resources (TODO: implementation pending)

## Controller Workflow

### Current Implementation
1. **Discovery Reference Validation**: Check referenced VMBD exists
2. **Discovery Completion Check**: Wait for VMBD to complete successfully
3. **Valid Backup Verification**: Ensure usable backups are available
4. **Selective Backup Validation**: Validate user-selected backups exist in results

### File Serving Implementation (TODO)
The controller currently stops at validation. File serving infrastructure creation is not yet implemented:
- Pod/service creation for file access
- Backup content mounting and serving
- User access mechanisms
- Resource lifecycle management

### Use Cases for Selective Backups
- **Targeted restoration**: Serve specific date ranges from large discovery results
- **Performance optimization**: Reduce resource usage by serving fewer backups
- **Comparison analysis**: Serve backups from specific dates for file comparison

## Integration with VMBD

### Data Dependencies
VMFR consumes from VMBD status:
- `Conditions`: Discovery completion status
- `ValidBackups`: List of usable backups for file serving

### Example Usage
```yaml
# Serve all discovered backups
apiVersion: oadp.openshift.io/v1alpha1
kind: VirtualMachineFileRestore
metadata:
  name: restore-all-files
spec:
  backupsDiscoveryRef: vm-backup-discovery

---
# Serve specific backups
apiVersion: oadp.openshift.io/v1alpha1
kind: VirtualMachineFileRestore
metadata:
  name: restore-specific-files
spec:
  backupsDiscoveryRef: vm-backup-discovery
  selectedBackups:
    - backup-20240115
    - backup-20240120
```

### Reusability
Multiple VMFR resources can reference the same completed VMBD:
- Different file access patterns from same discovery
- Reduced redundant backup validation
- Support for concurrent file serving scenarios

## Error Handling

### Discovery Reference Issues
- **Missing VMBD**: BackingOff phase, requeue until found
- **Discovery incomplete**: Wait for VMBD completion
- **No valid backups**: BackingOff with clear error message
- **Invalid selected backups**: BackingOff with list of invalid names

### Current Limitations
File serving infrastructure creation is not implemented:
- Resource creation and management
- Backup mounting and access mechanisms
- Network connectivity and user access
- Authentication and authorization

## Status Reporting

### Ready Condition
Primary condition types and reasons:
- `DiscoveryNotFound`: Referenced VMBD not found
- `DiscoveryIncomplete`: VMBD still in progress
- `NoValidBackups`: No usable backups found
- `InvalidSelectedBackups`: Selected backups not in discovery results
- `FileServingCreated`: Infrastructure created (TODO)
- `FileServingFailed`: Setup failed (TODO)

### Current Status Examples
```yaml
# Waiting for discovery
status:
  phase: BackingOff
  conditions:
    - type: Ready
      status: "False"
      reason: DiscoveryIncomplete
      message: "Waiting for backup discovery to complete"

# Invalid selection
status:
  phase: BackingOff
  conditions:
    - type: Ready
      status: "False"
      reason: InvalidSelectedBackups
      message: "selected backups not found: [backup-missing]"
```

## Security and Compatibility

### RBAC Requirements
- Read access to VMBD resources in same namespace
- Create/update/delete access for file serving infrastructure (TODO)
- Status update permissions for VMFR resources

### Resource Isolation
File serving resources created in same namespace as VMFR.
Access model to be defined with file serving implementation.

### Kubernetes Compatibility
Uses standard Kubernetes patterns (conditions, phases, references).

## Implementation Status

### Completed (Phase 1)
- VMFR CRD with discovery reference structure
- Controller logic for discovery validation and completion checking
- Status management for discovery-dependent phases
- Error handling for missing or incomplete discoveries

### TODO (Phase 2)
File serving implementation not yet started:
- **Architecture decision**: Pod-based vs job-based vs integrated approaches
- **Backup access**: How to mount and serve backup content
- **User interface**: File access mechanisms (web UI, CLI, API)
- **Resource management**: Lifecycle, cleanup, and performance optimization
- **Security model**: Authentication, authorization, and isolation

## Future Enhancements
- Automatic cleanup when referenced VMBD is deleted
- File serving infrastructure creation and management
- User access mechanisms and tooling
- Performance optimization for large backup files
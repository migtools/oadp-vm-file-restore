# File Server Directory Structure Design

## Problem Statement

The file-server container needs to support multiple backups per VMFR (VirtualMachineFileRestore) resource. Each backup may contain multiple PVCs (VM disks), and users need to be able to:

1. Identify which backup a file came from
2. Browse files from multiple backups simultaneously
3. Compare files across different backup timepoints
4. Navigate intuitively between backups

## VMFR Design Context

Based on `docs/design/vm_file_restore_mechanism.md` and `docs/design/oadp_vm_file_restore.md`:

**VMFR References Discovery Results**:
```yaml
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

**Multiple Backups Per VMFR**:
- Each VMFR can serve multiple backups (from `selectedBackups` list)
- Each backup can contain multiple PVCs (VM root disk, data disks, etc.)
- Users need to browse files from multiple timepoints

## Directory Structure Design

### Proposed Structure

```
/mnt/filesystems/
├── backup-20240115/                    ← Backup name (from Velero backup)
│   ├── rootdisk/                       ← PVC name (primary VM disk)
│   │   ├── root/                       ← Mounted VM filesystem
│   │   │   ├── etc/
│   │   │   ├── home/
│   │   │   ├── var/
│   │   │   └── ...
│   │   └── boot/                       ← Additional partitions (if any)
│   └── datadisk/                       ← Additional PVC (if VM has multiple disks)
│       └── data/
│           └── ...
│
├── backup-20240120/                    ← Second backup
│   ├── rootdisk/
│   │   └── root/
│   │       └── ...
│   └── datadisk/
│       └── data/
│           └── ...
│
└── METADATA.json                       ← Metadata about all mounts
```

### Example User Navigation

**Compare /etc/config.yaml across backups**:
```bash
# Backup from Jan 15
cat /mnt/filesystems/backup-20240115/rootdisk/root/etc/config.yaml

# Backup from Jan 20
cat /mnt/filesystems/backup-20240120/rootdisk/root/etc/config.yaml

# Diff between backups
diff /mnt/filesystems/backup-20240115/rootdisk/root/etc/config.yaml \
     /mnt/filesystems/backup-20240120/rootdisk/root/etc/config.yaml
```

**Access data disk from specific backup**:
```bash
ls -la /mnt/filesystems/backup-20240115/datadisk/data/databases/
```

## Environment Variables from Controller

The VMFR controller (Issue #7) will pass this information to the file-server pod via environment variables:

### Method 1: Environment Variables (Recommended)

```yaml
containers:
- name: file-server
  image: quay.io/konveyor/oadp-vmfr-access:latest
  env:
  # Backup and PVC mapping (JSON format)
  - name: BACKUP_PVC_MAP
    value: |
      {
        "backup-20240115": [
          {"pvc": "restored-vm-backup-20240115-rootdisk", "name": "rootdisk"},
          {"pvc": "restored-vm-backup-20240115-datadisk", "name": "datadisk"}
        ],
        "backup-20240120": [
          {"pvc": "restored-vm-backup-20240120-rootdisk", "name": "rootdisk"}
        ]
      }

  volumeMounts:
  # All PVCs mounted at /mnt/volumes/{backup-name}/{pvc-name}/
  - name: backup-20240115-rootdisk
    mountPath: /mnt/volumes/backup-20240115/rootdisk
  - name: backup-20240115-datadisk
    mountPath: /mnt/volumes/backup-20240115/datadisk
  - name: backup-20240120-rootdisk
    mountPath: /mnt/volumes/backup-20240120/rootdisk

  # Shared filesystem mount point
  - name: filesystems
    mountPath: /mnt/filesystems

  # Device mounts
  - name: fuse-device
    mountPath: /dev/fuse
  - name: kvm-device
    mountPath: /dev/kvm

volumes:
- name: backup-20240115-rootdisk
  persistentVolumeClaim:
    claimName: restored-vm-backup-20240115-rootdisk
- name: backup-20240115-datadisk
  persistentVolumeClaim:
    claimName: restored-vm-backup-20240115-datadisk
- name: backup-20240120-rootdisk
  persistentVolumeClaim:
    claimName: restored-vm-backup-20240120-rootdisk
# ... device volumes ...
```

### Method 2: ConfigMap (Alternative)

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: file-server-config
data:
  backup-pvc-map.json: |
    {
      "backup-20240115": [
        {"pvc": "restored-vm-backup-20240115-rootdisk", "name": "rootdisk"},
        {"pvc": "restored-vm-backup-20240115-datadisk", "name": "datadisk"}
      ],
      "backup-20240120": [
        {"pvc": "restored-vm-backup-20240120-rootdisk", "name": "rootdisk"}
      ]
    }
---
containers:
- name: file-server
  volumeMounts:
  - name: config
    mountPath: /etc/file-server-config
    readOnly: true

volumes:
- name: config
  configMap:
    name: file-server-config
```

**Recommendation**: Use **Method 1 (Environment Variables)** for simplicity.

## Updated detect-and-mount.sh Logic

The script needs to:
1. Read `BACKUP_PVC_MAP` environment variable (JSON)
2. Parse the JSON to get backup names and their PVCs
3. Mount each PVC to `/mnt/filesystems/{backup-name}/{pvc-name}/`
4. Generate metadata file

### JSON Structure

```json
{
  "backup-20240115": [
    {"pvc": "restored-vm-backup-20240115-rootdisk", "name": "rootdisk"},
    {"pvc": "restored-vm-backup-20240115-datadisk", "name": "datadisk"}
  ],
  "backup-20240120": [
    {"pvc": "restored-vm-backup-20240120-rootdisk", "name": "rootdisk"}
  ]
}
```

### Script Pseudocode

```bash
#!/bin/bash

# 1. Parse BACKUP_PVC_MAP environment variable
BACKUP_MAP="${BACKUP_PVC_MAP:-}"

if [ -z "$BACKUP_MAP" ]; then
    echo "ERROR: BACKUP_PVC_MAP environment variable not set"
    exit 1
fi

# 2. Extract backup names and PVCs
# For each backup in the map:
#   For each PVC in the backup:
#     - Source disk: /mnt/volumes/{backup-name}/{pvc-name}/disk.img
#     - Mount target: /mnt/filesystems/{backup-name}/{pvc-name}/
#     - Mount using guestmount

# 3. Generate metadata
cat > /mnt/filesystems/METADATA.json <<EOF
{
  "mounted_at": "$(date -Iseconds)",
  "backups": [
    {
      "name": "backup-20240115",
      "pvcs": [
        {"name": "rootdisk", "path": "/mnt/filesystems/backup-20240115/rootdisk"},
        {"name": "datadisk", "path": "/mnt/filesystems/backup-20240115/datadisk"}
      ]
    },
    {
      "name": "backup-20240120",
      "pvcs": [
        {"name": "rootdisk", "path": "/mnt/filesystems/backup-20240120/rootdisk"}
      ]
    }
  ]
}
EOF

# 4. Keep container alive
exec sleep infinity
```

## Volume Mount Strategy

### PVC Mounts at /mnt/volumes/

The controller mounts each PVC at a structured path:

```
/mnt/volumes/
├── backup-20240115/
│   ├── rootdisk/
│   │   └── disk.img           ← VM disk image (raw/qcow2)
│   └── datadisk/
│       └── disk.img
│
└── backup-20240120/
    └── rootdisk/
        └── disk.img
```

### Filesystem Mounts at /mnt/filesystems/

After guestmount processes the disk images:

```
/mnt/filesystems/
├── backup-20240115/
│   ├── rootdisk/              ← Mounted filesystem from disk.img
│   │   ├── root/              ← VM's root filesystem
│   │   │   ├── etc/
│   │   │   ├── home/
│   │   │   └── var/
│   │   └── boot/              ← Boot partition (if separate)
│   │
│   └── datadisk/              ← Mounted data disk
│       └── data/              ← Data filesystem
│
└── backup-20240120/
    └── rootdisk/
        └── root/
```

## Metadata File Structure

`/mnt/filesystems/METADATA.json`:

```json
{
  "vmfr_name": "restore-specific-files",
  "vmfr_namespace": "default",
  "vmfr_uid": "abc-123-def",
  "mounted_at": "2025-10-14T18:30:00Z",
  "backups": [
    {
      "backup_name": "backup-20240115",
      "backup_timestamp": "2024-01-15T10:00:00Z",
      "pvcs": [
        {
          "pvc_name": "rootdisk",
          "pvc_claim_name": "restored-vm-backup-20240115-rootdisk",
          "disk_image": "/mnt/volumes/backup-20240115/rootdisk/disk.img",
          "disk_format": "raw",
          "disk_size_bytes": 10737418240,
          "filesystem_type": "xfs",
          "mount_path": "/mnt/filesystems/backup-20240115/rootdisk",
          "mounted_partitions": [
            {"partition": "1", "type": "xfs", "mountpoint": "root"},
            {"partition": "2", "type": "vfat", "mountpoint": "boot"}
          ],
          "mount_status": "success",
          "mount_time_seconds": 32
        },
        {
          "pvc_name": "datadisk",
          "pvc_claim_name": "restored-vm-backup-20240115-datadisk",
          "disk_image": "/mnt/volumes/backup-20240115/datadisk/disk.img",
          "disk_format": "qcow2",
          "disk_size_bytes": 53687091200,
          "filesystem_type": "xfs",
          "mount_path": "/mnt/filesystems/backup-20240115/datadisk",
          "mounted_partitions": [
            {"partition": "1", "type": "xfs", "mountpoint": "data"}
          ],
          "mount_status": "success",
          "mount_time_seconds": 45
        }
      ]
    },
    {
      "backup_name": "backup-20240120",
      "backup_timestamp": "2024-01-20T10:00:00Z",
      "pvcs": [
        {
          "pvc_name": "rootdisk",
          "pvc_claim_name": "restored-vm-backup-20240120-rootdisk",
          "disk_image": "/mnt/volumes/backup-20240120/rootdisk/disk.img",
          "disk_format": "raw",
          "disk_size_bytes": 10737418240,
          "filesystem_type": "xfs",
          "mount_path": "/mnt/filesystems/backup-20240120/rootdisk",
          "mounted_partitions": [
            {"partition": "1", "type": "xfs", "mountpoint": "root"}
          ],
          "mount_status": "success",
          "mount_time_seconds": 28
        }
      ]
    }
  ],
  "statistics": {
    "total_backups": 2,
    "total_pvcs": 3,
    "successful_mounts": 3,
    "failed_mounts": 0,
    "total_mount_time_seconds": 105
  }
}
```

## Controller Integration (Issue #7)

### What the Controller Needs to Do

1. **Get Backup List** from VMFR spec (`selectedBackups`)
2. **Query Velero** to get PVCs for each backup
3. **Restore PVCs** (if not already restored)
4. **Build BACKUP_PVC_MAP** JSON structure
5. **Create Pod** with:
   - Environment variable: `BACKUP_PVC_MAP`
   - Volume mounts for each PVC at `/mnt/volumes/{backup}/{pvc}/`
   - Command: `/usr/local/bin/detect-and-mount.sh`

### Example Controller Logic

```go
func (r *VMFRReconciler) buildPodSpec(vmfr *oadpv1alpha1.VirtualMachineFileRestore) (*corev1.Pod, error) {
    // 1. Build backup-PVC map
    backupMap := make(map[string][]PVCInfo)

    for _, backupName := range vmfr.Spec.SelectedBackups {
        // Query Velero for this backup's PVCs
        pvcs := r.getBackupPVCs(backupName)

        backupMap[backupName] = pvcs
    }

    // 2. Convert to JSON
    backupMapJSON, _ := json.Marshal(backupMap)

    // 3. Build pod spec
    pod := &corev1.Pod{
        Spec: corev1.PodSpec{
            Containers: []corev1.Container{{
                Name:  "file-server",
                Image: "quay.io/konveyor/oadp-vmfr-access:latest",
                Env: []corev1.EnvVar{{
                    Name:  "BACKUP_PVC_MAP",
                    Value: string(backupMapJSON),
                }},
                VolumeMounts: buildVolumeMounts(backupMap),
            }},
            Volumes: buildVolumes(backupMap),
        },
    }

    return pod, nil
}
```

## Benefits of This Design

1. **Clear Organization**: Each backup has its own directory
2. **Easy Comparison**: Users can diff files across backups easily
3. **Scalable**: Supports any number of backups and PVCs
4. **Discoverable**: METADATA.json provides complete mount information
5. **Flexible**: Works with both single and multiple backups
6. **Intuitive**: Path structure mirrors backup → PVC → filesystem hierarchy
7. **Controller-Friendly**: Simple JSON structure for controller to generate

## Alternative Designs Considered

### Alternative 1: Flat Structure
```
/mnt/filesystems/
├── backup-20240115-rootdisk/
├── backup-20240115-datadisk/
└── backup-20240120-rootdisk/
```
**Rejected**: Harder to see which PVCs belong to which backup.

### Alternative 2: PVC-First Structure
```
/mnt/filesystems/
├── rootdisk/
│   ├── backup-20240115/
│   └── backup-20240120/
└── datadisk/
    └── backup-20240115/
```
**Rejected**: Doesn't match user mental model (users think in terms of backups first, then disks).

### Alternative 3: Timestamp-Based
```
/mnt/filesystems/
├── 2024-01-15-10-00-00/
└── 2024-01-20-10-00-00/
```
**Rejected**: Loses backup name, harder to correlate with Velero backup resources.

## Implementation Checklist

- [ ] Update `detect-and-mount.sh` to parse `BACKUP_PVC_MAP`
- [ ] Add JSON parsing logic (use `jq` or Python)
- [ ] Implement directory creation for each backup/PVC
- [ ] Add guestmount calls with structured paths
- [ ] Generate METADATA.json file
- [ ] Update CONTROLLER_INTEGRATION.md with this design
- [ ] Create live testing guide with multiple backups
- [ ] Test with 1 backup, 1 PVC (simple case)
- [ ] Test with 1 backup, 2 PVCs (multi-disk VM)
- [ ] Test with 2 backups, 1 PVC each (multi-timepoint)
- [ ] Test with both raw and qcow2 formats
- [ ] Update all documentation

## Next Steps

1. **Implement Updated detect-and-mount.sh** with BACKUP_PVC_MAP support
2. **Create Live Testing Guide** with step-by-step VM creation and testing
3. **Test End-to-End** with real OpenShift Virtualization environment
4. **Update Controller Integration Guide** with this directory structure
5. **Document in README.md** with examples

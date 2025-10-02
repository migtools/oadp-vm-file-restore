# OADP VM File Server Container

Design and create container with tools to access restored file systems

## Overview

This container provides a standardized environment with comprehensive tools to access and mount VM disk images (qcow2, raw) and various filesystems (ext4, xfs, ntfs, btrfs, etc.) from Velero/OADP backups. It serves as a toolbox for the VMFR (VirtualMachineFileRestore) controller to create file-serving pods.

## Design Decision: All-in-One Container

### Why Not Plugin-Based?

After researching KubeVirt's approach and VM filesystem requirements, we chose an **all-in-one container** approach over a plugin architecture:

**Rationale:**
- ✅ **Limited, well-defined set**: VM filesystem types are finite and well-known (ext4, xfs, ntfs, btrfs, fat)
- ✅ **Lightweight tooling**: All required packages combined are relatively small (~200-300MB)
- ✅ **Simpler deployment**: Single image, no plugin management complexity
- ✅ **Easier debugging**: All tools in one place, straightforward troubleshooting
- ✅ **Maintenance**: Fewer moving parts, single Dockerfile to maintain
- ❌ **Plugin complexity not justified**: Velero's plugin architecture is for cloud providers; our use case is simpler

### Comparison with Velero Plugins

| Aspect | Velero Plugins | OADP VM File Server |
|--------|---------------|---------------------|
| **Use Case** | Multiple cloud providers (AWS, Azure, GCP, etc.) | Fixed set of filesystems (ext4, xfs, ntfs, etc.) |
| **Extensibility Need** | High - new providers constantly added | Low - filesystems are well-established |
| **Complexity** | High - plugin discovery, versioning, lifecycle | Low - single container with all tools |
| **Size Trade-off** | Saves space per plugin | All tools together still lightweight |

## Architecture

### Base Image

**Upstream (this repository):**
- **Base**: Fedora 40
- **Why Fedora?**
  - Red Hat ecosystem (upstream for RHEL)
  - Full multi-arch support (ARM64 + x86_64)
  - All required packages natively available (no EPEL needed)
  - Easier for contributors to build and test
  - Follows standard Red Hat upstream/downstream pattern

**Downstream (Red Hat Product):**
- **Base**: RHEL9 (when Red Hat productizes OADP with this feature)
- Red Hat will rebuild with RHEL base + subscriptions (like OpenShift Virtualization does)
- Same approach as other Red Hat projects (e.g., Velero uses Ubuntu upstream, RHEL downstream)

### Installed Tools

#### Core VM Disk Tools
- `libguestfs-tools` - Comprehensive VM disk manipulation
- `qemu-img` - Disk image format detection and conversion
- `qemu-kvm-core` - QEMU/KVM core functionality
- `nbd` - Network Block Device support for mounting disk images

#### Filesystem Utilities
- `e2fsprogs` - ext2/ext3/ext4 (Linux)
- `xfsprogs` - XFS (Linux)
- `btrfs-progs` - Btrfs (Linux)
- `ntfs-3g` - NTFS (Windows)
- `dosfstools` - FAT/FAT32 (boot partitions, Windows)

#### Partition & LVM Support
- `lvm2` - Logical Volume Manager (common in Linux VMs)
- `parted` - Partition table manipulation
- `gdisk` - GPT partition support

#### Additional Utilities
- `fuse`/`fuse-libs` - Userspace filesystem mounting
- `file` - File type detection
- `util-linux` - blkid, lsblk, mount utilities
- `findutils`, `coreutils` - Basic utilities

### Helper Scripts

#### `/usr/local/bin/detect-and-mount.sh`
Automatically detects VM disk image formats and mounts their filesystems in read-only mode.

**Key Functions:**
- `detect_disk_format()` - Uses qemu-img to identify disk type
- `connect_nbd_device()` - Connects disk to /dev/nbd* using qemu-nbd
- `detect_filesystem()` - Uses blkid to identify filesystem type
- `mount_filesystem()` - Mounts filesystems read-only with appropriate options
- `process_disk_image()` - Orchestrates the mounting process
- `auto_mount_all()` - Discovers and mounts all disks in a directory

**Supported Formats:**
- qcow2 (most common for KubeVirt)
- raw
- Future: vmdk, vdi (easily extendable)

**Supported Filesystems:**
- ext2/ext3/ext4 (Linux)
- XFS (Linux)
- Btrfs (Linux)
- NTFS (Windows)
- FAT/FAT32 (boot partitions)

**Read-Only Safety:**
- All filesystems mounted with `-o ro`
- Filesystem-specific safety options:
  - ext4: `noload` (don't load journal)
  - xfs: `norecovery` (skip journal replay)
  - ntfs: ntfs-3g read-only mode
- qemu-nbd connects in `--read-only` mode

#### `/usr/local/bin/entrypoint.sh`
Simple entrypoint that keeps the container alive for controller management.

**Purpose:**
- Default CMD for the container
- Can be overridden by controller to run specific scripts
- Keeps container running for kubectl exec access

### Security

#### OpenShift Best Practices
- **Non-root user**: UID 1001
- **Group permissions**: GID 0 (root group) for OpenShift's arbitrary UID assignment
- **Directory permissions**: `g+rwX` allows group access

#### Privileged Requirements
**Note**: This container requires privileged access or specific capabilities:
- `CAP_SYS_ADMIN` - To load NBD kernel module (`modprobe nbd`)
- `CAP_SYS_MOUNT` - To mount filesystems

**Controller Integration (Issue #7)**:
The VMFR controller will need to create pods with:
```yaml
securityContext:
  privileged: true
  # OR specific capabilities:
  capabilities:
    add:
      - SYS_ADMIN
      - SYS_MOUNT
```

## Usage

### Building the Container

```bash
# Using podman
podman build -t oadp-vm-file-server:latest containers/file-server/

# Using docker
docker build -t oadp-vm-file-server:latest containers/file-server/
```

### Testing

Run the included test script:

```bash
cd containers/file-server/
./test-container.sh all
```

Test commands:
- `./test-container.sh build` - Build the image
- `./test-container.sh test` - Run validation tests
- `./test-container.sh clean` - Clean up
- `./test-container.sh all` - Build and test (default)

### Manual Testing

#### Start Container
```bash
podman run -it --privileged \
  -v /path/to/disk-images:/mnt/volumes:ro \
  oadp-vm-file-server:latest /bin/bash
```

#### Mount Disk Images
```bash
# Auto-discover and mount all disks in /mnt/volumes/
/usr/local/bin/detect-and-mount.sh

# Or mount a specific disk
/usr/local/bin/detect-and-mount.sh /mnt/volumes/my-vm-disk.qcow2
```

#### Verify Mounts
```bash
# Check mounted filesystems
mount | grep /mnt/filesystems

# List mounted filesystem contents
ls -la /mnt/filesystems/
```

## Directory Structure

```
containers/file-server/
├── Dockerfile                   # Container image definition
├── README.md                    # This file
├── test-container.sh            # Validation script
└── scripts/
    ├── detect-and-mount.sh      # Filesystem detection and mounting
    └── entrypoint.sh            # Default container entrypoint
```

## Integration with OADP Workflow

### Current Status (Issue #6)
✅ Container image with all required tools
✅ Read-only filesystem mounting capability
✅ Automatic disk format detection
✅ Tests and documentation

### Future Integration (Other Issues)

#### Issue #7: Controller Lifecycle Management
The VMFR controller will:
1. Create pods using this container image
2. Mount restored PVCs at `/mnt/volumes/`
3. Override CMD to run `detect-and-mount.sh` on startup
4. Manage pod lifecycle

Example pod spec:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: vm-file-server
spec:
  containers:
  - name: file-server
    image: oadp-vm-file-server:latest
    command: ["/usr/local/bin/detect-and-mount.sh"]
    securityContext:
      privileged: true
    volumeMounts:
    - name: restored-pvc
      mountPath: /mnt/volumes
      readOnly: true
  volumes:
  - name: restored-pvc
    persistentVolumeClaim:
      claimName: restored-vm-disk
```

#### Issue #8: SSH/rsync Access
Add SSH server and rsync to container for remote file access.

#### Issue #9: HTTPS Access
Add HTTP file server (Flask/Python) for web-based file browsing.

## Troubleshooting

### Container Fails to Start
- Check if using privileged mode or required capabilities
- Verify base image is accessible

### NBD Module Load Fails
```
ERROR: Failed to load NBD module. Container may need privileged security context.
```
**Solution**: Run container with `--privileged` or add `CAP_SYS_ADMIN` capability

### Filesystem Not Detected
- Verify disk image format is supported (qcow2, raw)
- Check disk image is not corrupted: `qemu-img check <image>`

### Mount Fails
- Check filesystem type is supported
- Verify partition table is readable: `parted <nbd-device> print`

## References

- [KubeVirt virt-launcher](https://github.com/kubevirt/kubevirt)
- [Velero Restore Process](https://velero.io/docs/)
- [libguestfs Tools](https://libguestfs.org/)
- [QEMU NBD Documentation](https://qemu.readthedocs.io/en/latest/tools/qemu-nbd.html)
- [OpenShift Container Best Practices](https://docs.openshift.com/container-platform/latest/openshift_images/create-images.html)

## License

Licensed under the Apache License, Version 2.0

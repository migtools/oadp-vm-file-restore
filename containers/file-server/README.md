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

**Mounting Approach: FUSE-based (Kubernetes-compatible)**

Uses `guestmount` from libguestfs instead of NBD for Kubernetes compatibility:
- ✅ **No privileged container needed**
- ✅ **No kernel module loading** (no `modprobe nbd`)
- ✅ **No `/dev` access required**
- ✅ **Works with OpenShift SecurityContextConstraints**
- ⚡ Slightly slower than NBD, but security > speed

**Key Functions:**
- `detect_disk_format()` - Uses qemu-img to identify disk type (qcow2, raw, vmdk, vdi)
- `mount_disk_with_guestmount()` - Uses FUSE-based guestmount to mount disk images
- `unmount_disk()` - Safely unmounts FUSE filesystems
- `process_disk_image()` - Orchestrates the mounting process
- `auto_mount_all()` - Discovers and mounts all disks in a directory

**Supported Formats:**
- qcow2 (most common for KubeVirt)
- raw
- vmdk (VMware)
- vdi (VirtualBox)

**Supported Filesystems:**
- ext2/ext3/ext4 (Linux)
- XFS (Linux)
- Btrfs (Linux)
- NTFS (Windows)
- FAT/FAT32 (boot partitions)
- LVM (Logical Volume Manager)

**Read-Only Safety:**
- All filesystems mounted with `--ro` (read-only)
- guestmount ensures data integrity

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

#### Kubernetes/OpenShift Compatibility ✅

**FUSE-based mounting** = **No privileged container needed!**

This container uses `guestmount` (FUSE) instead of NBD, which means:
- ✅ Works with standard Kubernetes security policies
- ✅ Compatible with OpenShift SecurityContextConstraints
- ✅ No `privileged: true` required
- ✅ No kernel module loading
- ✅ No special capabilities needed (beyond standard FUSE access)

**Controller Integration (Issue #7)**:
The VMFR controller creates pods with standard security context:
```yaml
securityContext:
  # No privileged mode needed! ✅
  runAsUser: 1001
  runAsGroup: 0
  fsGroup: 0
  # FUSE access is typically allowed by default in OpenShift/Kubernetes
```

**Note:** FUSE (`/dev/fuse`) access is typically allowed in Kubernetes clusters.
If restricted, only `CAP_SYS_CHROOT` may be needed (not `CAP_SYS_ADMIN`).

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
# FUSE-based mounting - no --privileged needed!
podman run -it \
  --device /dev/fuse \
  -v /path/to/disk-images:/mnt/volumes:ro \
  oadp-vm-file-server:latest /bin/bash
```

**Note:** `--device /dev/fuse` gives FUSE access (typically allowed by default in Kubernetes/OpenShift)

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

Example pod spec (FUSE-based, no privileged mode needed):
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
      # No privileged mode needed with FUSE! ✅
      runAsUser: 1001
      runAsGroup: 0
      fsGroup: 0
    volumeMounts:
    - name: restored-pvc
      mountPath: /mnt/volumes
      readOnly: true
    - name: fuse-device
      mountPath: /dev/fuse
  volumes:
  - name: restored-pvc
    persistentVolumeClaim:
      claimName: restored-vm-disk
  - name: fuse-device
    hostPath:
      path: /dev/fuse
      type: CharDevice
```

#### Issue #8: SSH/rsync Access
Add SSH server and rsync to container for remote file access.

#### Issue #9: HTTPS Access
Add HTTP file server (Flask/Python) for web-based file browsing.

## Troubleshooting

### Container Fails to Start
- Verify base image is accessible
- Check container logs: `podman logs <container-id>`

### guestmount Fails
```
ERROR: Failed to mount <disk> with guestmount
```
**Possible causes:**
1. **FUSE not available**: Ensure `/dev/fuse` is accessible
   - Check: `ls -la /dev/fuse` in container
   - Solution: Add `--device /dev/fuse` to podman/docker run
2. **Disk image corrupted**: Verify with `qemu-img check <image>`
3. **Unsupported format**: Check format with `qemu-img info <image>`

### Filesystem Not Detected
- Verify disk image format is supported (qcow2, raw, vmdk, vdi)
- Check disk image is not corrupted: `qemu-img check <image>`
- Try manual mount: `guestmount -a <disk> -i --ro <mountpoint>`

### Mount Fails in Kubernetes
```
ERROR: fuse: device not found, try 'modprobe fuse' first
```
**Solution**: Ensure pod has access to `/dev/fuse`:
```yaml
volumes:
- name: fuse-device
  hostPath:
    path: /dev/fuse
    type: CharDevice
volumeMounts:
- name: fuse-device
  mountPath: /dev/fuse
```

### Permission Denied
- Ensure container runs with correct user/group (UID 1001, GID 0)
- Check SecurityContextConstraints in OpenShift allow FUSE

## References

- [KubeVirt virt-launcher](https://github.com/kubevirt/kubevirt)
- [Velero Restore Process](https://velero.io/docs/)
- [libguestfs Tools](https://libguestfs.org/)
- [libguestfs guestmount](https://libguestfs.org/guestmount.1.html)
- [FUSE Documentation](https://www.kernel.org/doc/html/latest/filesystems/fuse.html)
- [OpenShift Container Best Practices](https://docs.openshift.com/container-platform/latest/openshift_images/create-images.html)

## License

Licensed under the Apache License, Version 2.0

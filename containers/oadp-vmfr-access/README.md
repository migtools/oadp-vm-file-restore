# OADP VM File Access Container

Design and create container with tools to access restored file systems

## Overview

This container provides a standardized environment with comprehensive tools to access and mount VM disk images (qcow2, raw) and various filesystems (ext4, xfs, ntfs, fat) from Velero/OADP backups. It serves as a toolbox for the VMFR (VirtualMachineFileRestore) controller to create file-serving pods.

## Key Concepts

Understanding the core technologies that make this container work:

### libguestfs - The Core Technology
**What it is:** A set of tools for accessing and modifying virtual machine disk images **without** booting the VM.

**How it works:**
1. Uses an internal **QEMU appliance** (mini virtual machine) to read disk images
2. Launches a small Linux kernel + initrd with filesystem drivers
3. Attaches your VM disk image to this appliance as a virtual disk
4. Reads the filesystem from inside the appliance
5. Exposes the filesystem to the host via FUSE

**Key tools:**
- `guestmount` - Mounts VM disk filesystems via FUSE
- `guestfish` - Interactive shell for disk inspection
- `virt-inspector` - Detects OS and filesystems in disk images

**Why we need it:** It's the **only** way to read VM disk images without booting the VM or requiring privileged kernel operations.

### FUSE (Filesystem in Userspace)
**What it is:** A technology that allows filesystem operations to run in **userspace** instead of kernel space.

**How it works:**
```
┌─────────────────────────────────────────────┐
│ Application (cat /mnt/filesystems/test.txt) │
└──────────────────┬──────────────────────────┘
                   │ read()
┌──────────────────▼──────────────────────────┐
│ Kernel: /dev/fuse device                    │
└──────────────────┬──────────────────────────┘
                   │ FUSE protocol
┌──────────────────▼──────────────────────────┐
│ Userspace: guestmount process               │
│ - Reads from VM disk image                  │
│ - Interprets filesystem (XFS, ext4, etc.)   │
│ - Returns file data to kernel               │
└─────────────────────────────────────────────┘
```

**Why we need it:**
- ✅ No kernel modules required (unlike NBD - Network Block Device)
- ✅ Safer than kernel-level mounting
- ✅ Works in containers without dangerous kernel access
- ✅ Standard technology (used by sshfs, libguestfs, many others)

**Security advantage:** All filesystem code runs as a regular process, not in the kernel.

### QEMU and KVM - Hardware Virtualization
**QEMU:** A machine emulator that can run virtual machines.

**KVM (Kernel-based Virtual Machine):** Linux kernel module that provides **hardware-accelerated** virtualization.

**How libguestfs uses them:**
1. libguestfs creates a **supermin appliance** (minimal Linux kernel + initrd)
2. Launches QEMU to run this appliance as a mini-VM
3. With KVM: Uses hardware virtualization (fast, ~30 seconds to boot)
4. Without KVM: Uses TCG software emulation (extremely slow, ~5 minutes)

**Why we need /dev/kvm:**
```
With KVM:     libguestfs ready in 30 seconds  ✅
Without KVM:  libguestfs ready in 5+ minutes  ❌ (often times out)
```

**Live testing result:** Privileged mode required for `/dev/kvm` access in OpenShift (SELinux restriction).

### Hardware Acceleration Explained
**TCG (Tiny Code Generator):** QEMU's software emulation mode
- Emulates CPU instructions in software
- No hardware virtualization extensions (Intel VT-x / AMD-V)
- 10-100x slower than hardware acceleration

**KVM mode:** Uses CPU virtualization extensions
- Guest instructions run directly on CPU
- Minimal overhead (close to native speed)
- Requires access to `/dev/kvm` device

**Why this matters for OADP:**
- VM file restore should be fast (restore completed → files accessible quickly)
- Without KVM, 5-minute wait time is unacceptable for users
- With KVM, 30-second wait time is reasonable

## Design Decision: All-in-One Container

### Why Not Plugin-Based?

After researching KubeVirt's approach and VM filesystem requirements, we chose an **all-in-one container** approach over a plugin architecture:

**Rationale:**
- ✅ **Limited, well-defined set**: VM filesystem types are finite and well-known (ext4, xfs, ntfs, fat)
- ✅ **Lightweight tooling**: All required packages combined are relatively small (~200-300MB)
- ✅ **Simpler deployment**: Single image, no plugin management complexity
- ✅ **Easier debugging**: All tools in one place, straightforward troubleshooting
- ✅ **Maintenance**: Fewer moving parts, single Dockerfile to maintain
- ❌ **Plugin complexity not justified**: Velero's plugin architecture is for cloud providers; our use case is simpler

### Comparison with Velero Plugins

| Aspect | Velero Plugins | OADP VM File Access Container |
|--------|---------------|---------------------|
| **Use Case** | Multiple cloud providers (AWS, Azure, GCP, etc.) | Fixed set of filesystems (ext4, xfs, ntfs, etc.) |
| **Extensibility Need** | High - new providers constantly added | Low - filesystems are well-established |
| **Complexity** | High - plugin discovery, versioning, lifecycle | Low - single container with all tools |
| **Size Trade-off** | Saves space per plugin | All tools together still lightweight |

## Architecture

### Base Image - Two Dockerfiles Strategy

This project provides **two Dockerfiles** for different build scenarios:

| Dockerfile | Base Image | Build Requirements | Use Case |
|------------|------------|-------------------|----------|
| **Dockerfile** | Fedora 42 | None | **Upstream** - Community development, anyone can build |
| **Dockerfile.rhel** | RHEL 9 UBI | Red Hat subscription | **Downstream** - Red Hat product builds |

Both produce functionally identical runtime images - the choice is about build accessibility vs production alignment.

**Upstream (Dockerfile - Fedora 42):**
- **Base**: `registry.fedoraproject.org/fedora:42`
- **Build Requirements**: None - anyone can build
- **Why Fedora?**
  - Red Hat ecosystem (upstream for RHEL)
  - Full multi-arch support (ARM64 + x86_64)
  - All required packages natively available (no subscription needed)
  - Easier for contributors to build and test
  - Follows standard Red Hat upstream/downstream pattern

**Downstream (Dockerfile.rhel - RHEL 9):**
- **Base**: `registry.access.redhat.com/ubi9/ubi:latest`
- **Build Requirements**: Red Hat subscription credentials (build time only)
- **Why RHEL?**
  - Production alignment with OpenShift Virtualization
  - Red Hat supported and signed packages
  - Same security profile as VM infrastructure
  - Runtime image does NOT require subscription

**For detailed build instructions**, see [BUILD.md](BUILD.md).

### Installed Tools - Why Each Tool Is Needed

This section explains **why** we need each tool and **what** it does, so everyone understands our design decisions.

#### Category 1: VM Disk Image Tools

**`libguestfs`** - **CRITICAL: Our primary tool**
- **Why:** Allows accessing VM disk images without starting the VM
- **What:** Provides `guestmount` (FUSE-based mounting), `guestfish` (disk inspection), `guestunmount`, and tools to read/modify disk images
- **Use Case:** Mount backed-up VM disks to extract files without booting the VM
- **Example:** `guestmount -a disk.qcow2 -i /mnt/disk --ro`
- **Note:** In RHEL 9, `guestmount`/`guestfish` are in the base `libguestfs` package (`libguestfs-tools` does not exist as a separate package in el9)

**`libguestfs-xfs`** - **CRITICAL: Required for RHEL/CentOS VMs**
- **Why:** XFS is the default filesystem for RHEL 7+, CentOS 7+, Fedora (most common in OpenShift)
- **What:** Adds XFS support to libguestfs appliance (mini kernel that runs inside guestmount)
- **Use Case:** Without this, guestmount cannot read XFS filesystems
- **Impact:** Most OpenShift VMs use XFS, so this is essential

**`qemu-img`**
- **Why:** Need to detect disk format before mounting (qcow2 vs raw vs vmdk)
- **What:** CLI tool to inspect and convert VM disk image formats
- **Use Case:** Identify format: `qemu-img info disk.qcow2 | grep "file format"`
- **Example Output:** `file format: qcow2`

#### Category 2: FUSE Support - Kubernetes Security

**`fuse` and `fuse-libs`** - **CRITICAL: Enables non-privileged mounting**
- **Why:** FUSE = Filesystem in Userspace (no kernel modules, no privileged mode!)
- **What:** Allows guestmount to mount disk images in userspace instead of kernel space
- **Use Case:** Mount VM disks in Kubernetes pods **without** privileged SecurityContext
- **Security Benefit:**
  - ✅ No privileged container needed
  - ✅ No kernel module loading (`modprobe nbd` not required)
  - ✅ No `/dev` access required
  - ✅ Works with OpenShift SecurityContextConstraints out of the box
- **Alternative:** NBD (Network Block Device) requires privileged containers - not acceptable for Kubernetes

#### Category 3: Filesystem Tools - Read Different VM Filesystems

**`xfsprogs`** - **Most Important: RHEL/CentOS/Fedora VMs**
- **Why:** XFS is the default filesystem for RHEL 7+, CentOS 7+, Fedora
- **What:** Tools to check, repair, and read XFS filesystems
- **Use Case:** Access XFS filesystems inside VM disk images
- **Coverage:** ~80% of OpenShift VMs use XFS

**`e2fsprogs`** - **ext2/ext3/ext4 (Ubuntu, Debian, older RHEL)**
- **Why:** ext4 is common in Ubuntu VMs, Debian VMs, and older Linux distributions
- **What:** Tools to check, repair, and read ext filesystems
- **Use Case:** Access ext4 filesystems inside VM disk images
- **Coverage:** ~15% of VMs (Ubuntu/Debian-based)

**`libguestfs-winsupport`** - **NTFS (Windows Server VMs)**
- **Why:** NTFS is the Windows filesystem
- **What:** Injects NTFS driver into the libguestfs appliance via supermin
- **Use Case:** Access Windows Server VM disk images
- **Coverage:** ~2% of VMs (Windows workloads)
- **Note:** Replaces `ntfs-3g` which is only available in EPEL, not in RHEL repos. Available in RHEL 9 AppStream.

> **Btrfs is NOT supported:** Red Hat deprecated Btrfs in RHEL 7 and fully removed it in RHEL 8+.
> There is no `btrfs-progs` in RHEL 9 repos (only available via EPEL), and the libguestfs appliance
> kernel (based on RHEL) lacks the `btrfs.ko` module. VMs using Btrfs (primarily SUSE guests)
> cannot be mounted. This is a known limitation.

**`dosfstools`** - **FAT32 (EFI System Partitions)**
- **Why:** FAT32 is used for EFI System Partitions (ESP) on **all** UEFI VMs
- **What:** Tools to read FAT12/FAT16/FAT32 filesystems
- **Use Case:** Access boot partitions in VM disk images
- **Coverage:** Every UEFI VM has an ESP partition

#### Category 4: LVM & Partition Tools - Handle Complex Layouts

**`lvm2`** - **Logical Volume Manager**
- **Why:** Many Linux VMs use LVM for flexible disk management (especially RHEL/CentOS installations)
- **What:** Tools to detect and access LVM physical volumes, volume groups, logical volumes
- **Use Case:** VM disk has LVM layout → guestmount automatically detects and mounts logical volumes
- **Note:** guestmount handles LVM automatically when these tools are present
- **Coverage:** ~60% of RHEL/CentOS VMs use LVM

**`parted`** - **Partition Table Reading (MBR and GPT)**
- **Why:** All VM disks have partition tables (MBR for legacy BIOS, GPT for UEFI)
- **What:** Tool to inspect and manipulate partition tables
- **Use Case:** Identify partitions within a disk image before mounting
- **Coverage:** 100% of VMs have partitions

**`gdisk`** - **GPT Partition Tables (UEFI VMs)**
- **Why:** GPT (GUID Partition Table) is standard for modern UEFI VMs
- **What:** GPT partition table manipulation tool
- **Use Case:** Read modern VM partition layouts (GPT is replacing legacy MBR)
- **Coverage:** ~90% of modern VMs use GPT

#### Category 5: Utility Tools - Detection & Debugging

**`file`** - **File Type Detection**
- **Why:** Detect file types by content (magic numbers), not by extension
- **What:** Inspects file headers to identify actual type
- **Use Case:** Verify a `.qcow2` file is actually a qcow2 image (user might rename files)
- **Example:** `file disk.qcow2` → `QEMU QCOW2 Image (v3)`

**`util-linux`** - **Block Device Utilities**
- **Why:** Provides essential tools like `lsblk`, `blkid`, `mount`
- **What:** Collection of Linux utilities for block devices
- **Use Case:** Inspect mounted filesystems, identify block devices in debug scenarios

**`findutils`, `coreutils`** - **Basic Unix Tools**
- **Why:** Standard Unix tools needed by automation scripts
- **What:** `find`, `ls`, `cat`, `grep`, `awk`, etc.
- **Use Case:** Used by `detect-and-mount.sh` script for automation

### Tool Selection Rationale

**Why this specific combination?**
1. **Coverage:** These tools cover 99%+ of VM backup scenarios in OpenShift
2. **Tested:** All tools are mature, well-tested, and widely used
3. **Available:** All packages are in Fedora 40 base repos (no EPEL needed)
4. **Size:** Combined, these add ~1.1 GB to the image (acceptable for tooling container)
5. **Security:** FUSE-based approach means no privileged containers needed

**What we intentionally excluded:**
- **NBD (Network Block Device):** Requires privileged containers and kernel modules
- **Cloud-specific tools:** Our scope is VM filesystems, not cloud provider APIs
- **Write operations:** All mounts are read-only (`--ro`) for data safety

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
- NTFS (Windows) — via `libguestfs-winsupport`
- FAT/FAT32 (boot partitions)
- LVM (Logical Volume Manager)
- ~~Btrfs~~ — not supported on RHEL 9 (no kernel module or userspace tools available)

**Not Supported:**
- **LUKS-encrypted VM disks** — `cryptsetup` is not included; guestmount cannot unlock encrypted partitions. Supporting LUKS would also require passing decryption keys via Kubernetes Secrets and CRD changes, not just a package install.
- **Btrfs filesystems** — removed from RHEL 8+; primarily affects SUSE guest VMs

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

This section explains **all security decisions** made for this container based on live cluster testing.

#### Security Decision Summary

| Decision | Reason | Alternative Considered | Why Not Used |
|----------|--------|----------------------|--------------|
| **Privileged mode** | `/dev/kvm` access with SELinux | Non-privileged | SELinux blocks /dev/kvm, TCG too slow |
| **qemu user (107:107)** | Matches VM disk ownership | UID 1001 | Permission denied on VM disk PVCs |
| **SELinux MCS labels** | OpenShift pod isolation | Ignore labels | SELinux blocks PVC access |
| **RW PVC mount** | libguestfs disk file requirement | Read-only mount | libguestfs fails (needs write to disk file) |
| **kubevirt-controller SCC** | Allows privileged + hostPath | restricted SCC | Blocks privileged and /dev/fuse |

#### Why Privileged Mode Is Required

**Initial Assumption:** FUSE-based mounting = no privileged mode needed ✅

**Live Testing Reality:** Privileged mode required for performance ⚠️

**Root Cause:**
```
libguestfs → QEMU → /dev/kvm (hardware acceleration)
                     ↑
                     SELinux blocks non-privileged access
```

**SELinux Context Mismatch:**
```bash
# /dev/kvm SELinux label:
crw-rw-rw-. 1 root kvm system_u:object_r:kvm_device_t:s0 /dev/kvm

# Non-privileged container SELinux label:
system_u:system_r:container_t:s0:c123,c456

# Result: container_t cannot access kvm_device_t → Permission denied
```

**With Privileged Mode:**
- SELinux confinement disabled for container
- Can access /dev/kvm (kvm_device_t)
- QEMU uses KVM hardware acceleration
- libguestfs ready in ~30 seconds ✅

**Without Privileged Mode:**
- SELinux blocks /dev/kvm access
- QEMU falls back to TCG (software emulation)
- 10-100x slower
- libguestfs takes 5+ minutes (often times out) ❌

#### Why qemu User (UID 107, GID 107)

**VM Disk File Ownership (OpenShift Virtualization):**
```bash
# Inside VM disk PVC:
ls -la /mnt/volumes/
drwxrwsr-x. 3 root qemu   4096 Oct  9 18:10 .
-rw-rw----. 1 qemu qemu   9.8G Oct  9 22:31 disk.img
             ^^^^  ^^^^
             UID:107 GID:107
```

**Why qemu owns the disk:**
- KubeVirt virt-launcher pods run as qemu user
- QEMU process needs to read/write VM disk files
- OpenShift dynamically assigns UID but uses qemu group

**Initial Attempt (UID 1001 - standard OpenShift practice):**
```bash
$ ls -la /mnt/volumes/
ls: cannot open directory '/mnt/volumes/': Permission denied
```

**Solution:** Run as qemu (107:107) to match VM disk ownership

#### Why SELinux MCS Labels Must Match

**SELinux Multi-Category Security (MCS):**
- OpenShift assigns random MCS labels to each pod for isolation
- Example: `s0:c468,c664`
- Pod can only access PVCs with matching MCS label

**PVC gets labeled on first mount:**
```bash
# VM running, mounts PVC:
ls -laZ /mnt/volumes/
drwxrwsr-x. qemu qemu system_u:object_r:container_file_t:s0:c468,c664 .
                                                          ^^^^^^^^^^^^
                                                          PVC's MCS label
```

**File-server pod with different label:**
```yaml
# Pod gets random label: s0:c2,c28 (different!)
# Result: Permission denied when accessing PVC
```

**Solution:** Explicitly set pod's SELinux level to match PVC:
```yaml
securityContext:
  seLinuxOptions:
    level: "s0:c468,c664"  # Must match PVC!
```

**How to find PVC's label:**
```bash
# Method 1: Check from a pod that can access it (like the VM's virt-launcher)
oc exec virt-launcher-vm-xxx -- ls -laZ /var/run/kubevirt-private/vmi-disks/rootdisk/

# Method 2: Create pod, exec in, check error, adjust label, recreate
oc exec file-server-test -- ls -laZ /mnt/volumes/
```

#### Why PVC Needs Read-Write Mount

**Assumption:** Mounting filesystem read-only = PVC can be read-only ✅

**Reality:** PVC needs **read-write**, filesystem is read-only ⚠️

**Reason:**
```
guestmount mounts filesystem --ro (read-only) ✅
    ↓
But libguestfs needs to:
1. Create COW overlay: /tmp/libguestfsXXX/overlay1.qcow2
   Purpose: Protect original disk from any writes
   Requires: Write access to DISK FILE (not filesystem)

2. QEMU internal operations
   Example: Image locking, metadata
   Requires: Write access to DISK FILE

Result: PVC mount MUST be read-write, but filesystem inside
        is still mounted read-only (safe)
```

**Configuration:**
```yaml
volumeMounts:
- name: vm-disk
  mountPath: /mnt/volumes
  # NO readOnly: true here! ← libguestfs needs RW to disk file

# Inside pod:
guestmount -a /mnt/volumes/disk.img -i --ro /mnt/filesystems
                                           ^^^^ filesystem is still read-only
```

#### Why kubevirt-controller SCC

**SecurityContextConstraints (SCC)** are OpenShift's security policies.

**Requirements not allowed by restricted SCC:**
- ✅ Privileged containers
- ✅ hostPath volumes (/dev/fuse, /dev/kvm)
- ✅ Specific user IDs (107)

**kubevirt-controller SCC:**
- Used by KubeVirt virt-launcher pods (VMs)
- Allows privileged mode
- Allows hostPath volumes
- Allows specific UIDs

**Why this is acceptable:**
- Same security model as running VMs in OpenShift
- If cluster runs VMs, it already trusts this SCC
- OADP file server has same threat model as VMs
- Both need /dev/kvm, both access VM disks

#### Security Justification: Is Privileged Mode Safe?

**Why Privileged Mode is Required:**

After extensive testing (see TESTING-DEVICE-PLUGIN.md), we confirmed that privileged mode is required for:

1. **FUSE mounting** - SELinux blocks `container_t` → `fuse_device_t` access
   - Non-privileged containers cannot access `/dev/fuse` even with `SYS_ADMIN` capability
   - Custom SCCs with capabilities don't grant effective capabilities to non-root containers
   - This is by design in OpenShift's security model

2. **KVM acceleration** - SELinux blocks `container_t` → `kvm_device_t` access
   - Although device plugin works for virt-launcher, libguestfs+FUSE combination requires privileged mode
   - Without KVM: 5+ minute wait time (TCG emulation)
   - With KVM: ~30 second response time

**Alternative Considered:**

The KubeVirt team suggested using libguestfs "direct" backend with APIs instead of FUSE mounting. This would work without privileged mode, but:
- Requires complete redesign of file access layer
- Cannot use standard tools (filebrowser, SSH/rsync)
- Significantly more complex to implement and maintain
- Trade-off: complexity vs security posture

**Decision:** Use privileged mode for simplicity and alignment with KubeVirt patterns.

**Threat Model:**
```
What can file-server pod do?
✅ Read VM disk images (intended purpose)
✅ Access /dev/kvm (hardware acceleration)
✅ Create FUSE mounts (filesystem access)
❌ Network access (none configured)
❌ Host filesystem access (only /dev/fuse, /dev/kvm)
❌ Other pods' data (SELinux MCS isolation)
```

**Comparison with KubeVirt Infrastructure:**
```
virt-handler pod (KubeVirt infrastructure):
- privileged: true
- Manages /dev/kvm access for VMs
- System-level pod

file-server pod:
- privileged: true (for /dev/kvm + /dev/fuse)
- Runs as qemu user
- Read-only filesystem access
- Runs libguestfs (trusted, signed code)

Risk level: SAME or LOWER than KubeVirt infrastructure
```

**Red Hat's Position:**
- If cluster runs OpenShift Virtualization, it already allows privileged pods for infrastructure
- File restore is lower risk than running arbitrary VMs
- Same security model as virt-handler pods
- Limited scope: only accesses specific restored PVCs

#### Container Image Security (UID 1001 in Dockerfile)

**Wait, Dockerfile uses UID 1001, but pod uses 107?**

Yes! This is intentional:

**Dockerfile (build time):**
```dockerfile
USER 1001  # Default for local/testing
```

**Pod spec (runtime):**
```yaml
securityContext:
  runAsUser: 107  # Overrides Dockerfile USER
```

**Why this design:**
- Image works locally without special config (UID 1001)
- Controller overrides to 107 when mounting VM disks
- Best of both worlds: safe defaults + production flexibility

#### Summary: Security Requirements Checklist

For the VMFR controller (Issue #7), when creating file-server pods:

- [ ] Set namespace to privileged Pod Security level
- [ ] Use kubevirt-controller SCC
- [ ] Set `privileged: true`
- [ ] Set `runAsUser: 107, runAsGroup: 107`
- [ ] Mount /dev/fuse (hostPath)
- [ ] Mount /dev/kvm (hostPath)
- [ ] Get PVC's SELinux MCS label
- [ ] Set pod's SELinux level to match PVC
- [ ] Mount PVC read-write (filesystem still mounted read-only inside)

## Usage

### Building the Container

**Quick Start (Upstream - Fedora 42):**
```bash
cd containers/oadp-vmfr-access/
podman build -t oadp-vmfr-access:latest .
```

**For complete build instructions** (including RHEL 9 downstream builds, CI/CD integration, and troubleshooting), see **[BUILD.md](BUILD.md)**.

### Testing

**📋 Complete Testing Guide:** See [TESTING.md](TESTING.md) for comprehensive testing procedures, test results, and validation checklists.

Run the included test script:

```bash
cd containers/oadp-vmfr-access/
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
# Privileged mode required for KVM access (libguestfs performance)
podman run -it --privileged \
  --device /dev/fuse \
  --device /dev/kvm \
  -v /path/to/disk-images:/mnt/volumes \
  oadp-vmfr-access:latest /bin/bash
```

**Note:**
- `--privileged` required for `/dev/kvm` access (QEMU hardware acceleration)
- `--device /dev/fuse` for FUSE-based mounting
- `--device /dev/kvm` for libguestfs performance
- Volume mount is RW (libguestfs requirement), but filesystems mounted read-only

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
containers/oadp-vmfr-access/
├── Dockerfile                              # Upstream container (Fedora 42)
├── konflux.Dockerfile                      # Downstream container (RHEL 9)
├── README.md                               # This file - overview and concepts
├── BUILD.md                                # Complete build instructions
├── TESTING.md                              # Comprehensive testing guide
├── CONTROLLER_INTEGRATION.md               # Integration guide for Issue #7
├── test-container.sh                       # Automated validation script
├── test-examples/
│   ├── test-pod.yaml                       # Working pod spec (live tested)
│   └── oadp-vmfr-access-scc.yaml           # SecurityContextConstraints
└── scripts/
    ├── detect-and-mount.sh                 # Filesystem detection and mounting
    └── entrypoint.sh                       # Default container entrypoint
```

## Complete Workflow: VM Backup → File Restore

This section explains the **end-to-end process** of restoring files from a backed-up VM.

### Step 1: VM Backup (Velero/OADP)
```
User creates VM backup with Velero:
┌──────────────────────────────────────┐
│ Running VM (OpenShift Virtualization)│
│ - Fedora/RHEL with XFS filesystem   │
│ - Files: /root/important-data.txt   │
│ - Disk: PVC with qcow2/raw image    │
└──────────────────┬───────────────────┘
                   │ velero backup create vm-backup
┌──────────────────▼───────────────────┐
│ Velero Backup                        │
│ - VM definition (YAML)               │
│ - VM disk PVC snapshot               │
│ - Stored in S3/object storage        │
└──────────────────────────────────────┘
```

### Step 2: VM Restore (Velero/OADP)
```
User restores VM backup:
┌──────────────────────────────────────┐
│ velero restore create --from-backup  │
└──────────────────┬───────────────────┘
                   │
┌──────────────────▼───────────────────┐
│ Restored PVC                         │
│ - Contains VM disk image (raw/qcow2)│
│ - Name: restored-vm-rootdisk         │
│ - NOT booted yet (just PVC)          │
└──────────────────────────────────────┘
```

### Step 3: File Restore Request (User)
```
User creates VirtualMachineFileRestore CR:
apiVersion: oadp.openshift.io/v1alpha1
kind: VirtualMachineFileRestore
metadata:
  name: restore-important-files
spec:
  vmName: fedora-vm
  pvcName: restored-vm-rootdisk
  files:
  - /root/important-data.txt
  - /etc/config.yaml
```

### Step 4: VMFR Controller Creates File Server Pod
```
VMFR controller watches for FileRestore CR:
┌──────────────────────────────────────┐
│ VMFR Controller                      │
│ 1. Detects FileRestore CR            │
│ 2. Creates file-server pod           │
│ 3. Mounts restored PVC               │
│ 4. Runs detect-and-mount.sh          │
└──────────────────┬───────────────────┘
                   │
┌──────────────────▼───────────────────┐
│ File Server Pod                      │
│ - Image: oadp-vmfr-access:latest     │
│ - Volumes:                           │
│   - PVC → /mnt/volumes/disk.img      │
│   - /dev/fuse → FUSE device          │
│   - /dev/kvm → KVM device            │
│ - Security: privileged, qemu user    │
└──────────────────────────────────────┘
```

### Step 5: libguestfs Mounts Filesystem
```
Inside file-server pod (automatic):
┌──────────────────────────────────────┐
│ /usr/local/bin/detect-and-mount.sh   │
└──────────────────┬───────────────────┘
                   │
┌──────────────────▼───────────────────┐
│ qemu-img info /mnt/volumes/disk.img  │
│ → Detected: raw format               │
└──────────────────┬───────────────────┘
                   │
┌──────────────────▼───────────────────┐
│ guestmount -a disk.img -i --ro \     │
│            /mnt/filesystems           │
│                                      │
│ What happens internally:             │
│ 1. libguestfs builds supermin        │
│    appliance (~30 sec)               │
│ 2. Launches QEMU with KVM            │
│ 3. Attaches disk.img to QEMU VM      │
│ 4. Detects XFS partition             │
│ 5. Mounts XFS via FUSE               │
└──────────────────┬───────────────────┘
                   │
┌──────────────────▼───────────────────┐
│ Filesystem Mounted!                  │
│ /mnt/filesystems/                    │
│ ├── root/                            │
│ │   └── important-data.txt           │
│ ├── etc/                             │
│ │   └── config.yaml                  │
│ └── home/                            │
│     └── user/                        │
└──────────────────────────────────────┘
```

### Step 6: User Accesses Files
```
Multiple access methods (future issues):

Option A: kubectl exec (current)
  kubectl exec file-server-pod -- cat /mnt/filesystems/root/important-data.txt

Option B: SSH/rsync (Issue #8)
  rsync file-server-pod:/mnt/filesystems/root/ ./restored-files/

Option C: HTTPS (Issue #9)
  https://file-server-route.apps.cluster/browse/root/
```

### Technical Details: What Happens During Mount

**Timeline (with KVM):**
```
T+0s    : detect-and-mount.sh starts
T+1s    : qemu-img detects disk format (raw)
T+2s    : guestmount starts
T+5s    : libguestfs builds supermin appliance
T+10s   : QEMU launches with KVM acceleration
T+15s   : QEMU appliance boots (mini Linux kernel)
T+20s   : Appliance detects disk partitions
T+25s   : Appliance detects XFS filesystem
T+30s   : FUSE mount completes
T+30s   : Filesystem ready! ✅
```

**Timeline (without KVM - NOT recommended):**
```
T+0s    : detect-and-mount.sh starts
T+1s    : qemu-img detects disk format
T+2s    : guestmount starts
T+5s    : libguestfs builds supermin appliance
T+10s   : QEMU launches with TCG (software emulation)
T+60s   : QEMU still booting... (99% CPU usage)
T+180s  : QEMU still booting...
T+300s  : Timeout! ❌ (or finally ready but very slow)
```

**This is why /dev/kvm access is critical!**

## Integration with OADP Workflow

### Current Status (Issue #6)
✅ Container image with all required tools
✅ Read-only filesystem mounting capability
✅ Automatic disk format detection
✅ Automated mounting script (`detect-and-mount.sh`)
✅ Tests and documentation
✅ **Live cluster testing completed** (OpenShift Virtualization)
  - Tested with real VM disk (9.8GB raw format, XFS filesystem)
  - Validated end-to-end: VM backup → disk mount → file access
  - All test files successfully accessible via guestmount
  - Automated script tested: `detect-and-mount.sh` successfully mounts filesystem and keeps pod alive
  - Security requirements documented (privileged mode, qemu user, SELinux labels)
  - Pod runs continuously with FUSE mounts active

### Future Integration (Other Issues)

#### Issue #7: Controller Lifecycle Management

**📋 Complete Implementation Guide:** See [CONTROLLER_INTEGRATION.md](CONTROLLER_INTEGRATION.md)

This guide provides:
- ✅ Complete checklist for pod provisioning
- ✅ Security configuration requirements (SELinux, privileged mode, qemu user)
- ✅ Volume mounting specifications
- ✅ Pre-deployment validation steps
- ✅ Status monitoring and verification
- ✅ Troubleshooting common issues
- ✅ Complete working example pod spec

The VMFR controller will:
1. Create pods using this container image
2. Mount restored PVCs at `/mnt/volumes/`
3. Override CMD to run `detect-and-mount.sh` on startup
4. Manage pod lifecycle

Example pod spec (based on live cluster testing):
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: vm-file-server
spec:
  securityContext:
    fsGroup: 107  # qemu group
    supplementalGroups:
    - 107
    seLinuxOptions:
      level: "s0:c468,c664"  # Must match PVC's SELinux label
  containers:
  - name: file-server
    image: quay.io/konveyor/oadp-vmfr-access:oadp-1.6
    command: ["/usr/local/bin/detect-and-mount.sh"]
    securityContext:
      privileged: true    # Required for /dev/kvm access
      runAsUser: 107      # qemu user
      runAsGroup: 107     # qemu group
    volumeMounts:
    - name: restored-pvc
      mountPath: /mnt/volumes  # RW access (libguestfs requirement)
    - name: fuse-device
      mountPath: /dev/fuse
    - name: kvm-device
      mountPath: /dev/kvm
    - name: filesystems
      mountPath: /mnt/filesystems
  volumes:
  - name: restored-pvc
    persistentVolumeClaim:
      claimName: restored-vm-disk
  - name: fuse-device
    hostPath:
      path: /dev/fuse
      type: CharDevice
  - name: kvm-device
    hostPath:
      path: /dev/kvm
      type: CharDevice
  - name: filesystems
    emptyDir: {}
```

**Important Notes:**
- PVC must be mounted **read-write** (libguestfs needs write access to disk file for internal operations)
- Filesystem is still mounted **read-only** inside the container (`--ro` flag in guestmount)
- SELinux MCS labels must match between pod and PVC (check with `oc exec` and `ls -laZ`)

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

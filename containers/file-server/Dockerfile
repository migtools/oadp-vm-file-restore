# OADP VM File Server Container
# ================================
# Issue #6: Design and create container with tools to access restored file systems
#
# Purpose:
#   This container provides a standardized environment with tools to access and mount
#   VM disk images (qcow2, raw) and various filesystems (ext4, xfs, ntfs, btrfs, etc.)
#   from Velero/OADP backups. It serves as a toolbox for the VMFR controller to
#   create file-serving pods.
#
# Design Decision (Issue #6):
#   - All-in-one container approach (not plugin-based)
#   - Includes all necessary filesystem tools and utilities
#   - Simpler to deploy, maintain, and debug
#   - Plugin complexity not justified for well-defined set of VM filesystem types
#
# Upstream/Downstream Strategy:
#   - **Upstream** (this repo): Uses Fedora 40 for multi-arch support (ARM64 + x86_64)
#   - **Downstream** (Red Hat product): Red Hat rebuilds with RHEL9 base + subscriptions
#   - This follows standard Red Hat practice (e.g., Velero uses Ubuntu upstream, RHEL downstream)
#
# Architecture:
#   - Based on Fedora 40 (Red Hat ecosystem, upstream for RHEL)
#   - Full multi-arch support (ARM64 + x86_64)
#   - Non-root user (UID 1001) following OpenShift security best practices
#   - Helper scripts for filesystem detection and read-only mounting
#

FROM registry.fedoraproject.org/fedora:40

# Labels following OpenShift/OADP conventions
LABEL name="oadp-vm-file-server" \
      vendor="Red Hat" \
      summary="OADP VM File Restore Server" \
      description="Container with tools for accessing and mounting VM disk images from OADP backups" \
      io.k8s.description="Provides filesystem tools for VM file restore in OADP" \
      io.openshift.tags="oadp,backup,restore,vm,filesystem"

# ==============================================================================
# Package Installation - All required tools for VM disk and filesystem access
# ==============================================================================
# Fedora 40 has all these packages natively available (no EPEL needed)
# Package selection based on KubeVirt research and VM filesystem requirements
# Total size: ~1.1 GB (acceptable for a specialized tooling container)
#
# See README.md "Installed Tools - Why Each Tool Is Needed" for detailed rationale
#
RUN dnf install -y \
    # =========================================================================
    # Category 1: VM Disk Image Tools - CRITICAL for reading VM disk images
    # =========================================================================
    # libguestfs-tools: PRIMARY TOOL - Mount VM disks without booting the VM
    #   - Provides: guestmount (FUSE-based mounting), guestfish (inspection)
    #   - Use case: Mount backed-up qcow2/raw disk images to extract files
    #   - Example: guestmount -a disk.qcow2 -i /mnt/disk --ro
    libguestfs-tools \
    #
    # libguestfs-xfs: CRITICAL - Required for RHEL/CentOS/Fedora VMs
    #   - Why: XFS is default filesystem for RHEL 7+, CentOS 7+, Fedora
    #   - What: Adds XFS support to libguestfs appliance (mini kernel)
    #   - Impact: Without this, guestmount cannot read XFS filesystems
    #   - Coverage: ~80% of OpenShift VMs use XFS
    libguestfs-xfs \
    #
    # qemu-img: Disk image format detection and conversion
    #   - Why: Need to detect disk format before mounting (qcow2 vs raw vs vmdk)
    #   - Use case: qemu-img info disk.qcow2 | grep "file format"
    qemu-img \
    #
    # =========================================================================
    # Category 2: FUSE Support - CRITICAL for Userspace Filesystem Mounting
    # =========================================================================
    # fuse & fuse-libs: Enable userspace filesystem mounting (required by guestmount)
    #   - Why: FUSE = Filesystem in Userspace (no kernel modules needed!)
    #   - What: Allows guestmount to mount filesystems in userspace instead of kernel space
    #   - How it works:
    #     1. /dev/fuse device provides kernel interface for FUSE
    #     2. guestmount runs in userspace, talks to kernel via /dev/fuse
    #     3. Filesystem operations (read/write) handled by guestmount process
    #     4. No kernel filesystem driver needed (ext4, xfs, etc. all in userspace)
    #   - Security benefits vs NBD (Network Block Device):
    #     ✅ No kernel module loading (modprobe nbd NOT required)
    #     ✅ Cleaner than NBD which requires privileged mode + kernel modules
    #   - Note: Live testing showed privileged mode still required for /dev/kvm access
    #           (libguestfs uses QEMU with KVM for hardware acceleration)
    fuse \
    fuse-libs \
    #
    # =========================================================================
    # Category 3: Filesystem Tools - Read Different VM Filesystems
    # =========================================================================
    # e2fsprogs: ext2/ext3/ext4 filesystem support (Ubuntu, Debian, older RHEL)
    #   - Why: ext4 is common in Ubuntu/Debian VMs
    #   - Coverage: ~15% of VMs
    e2fsprogs \
    #
    # xfsprogs: XFS filesystem support (RHEL, CentOS, Fedora)
    #   - Why: XFS is default for RHEL 7+, CentOS 7+, Fedora
    #   - Coverage: ~80% of OpenShift VMs
    xfsprogs \
    #
    # btrfs-progs: Btrfs filesystem support (SUSE, modern Fedora)
    #   - Why: Btrfs used in SUSE Linux, some Fedora installations
    #   - Coverage: ~3% of VMs
    btrfs-progs \
    #
    # ntfs-3g: NTFS filesystem support (Windows Server VMs)
    #   - Why: NTFS is the Windows filesystem
    #   - What: FUSE-based NTFS driver for reading/writing Windows filesystems
    #   - Coverage: ~2% of VMs (Windows workloads)
    ntfs-3g \
    #
    # dosfstools: FAT12/FAT16/FAT32 filesystem support (EFI System Partitions)
    #   - Why: FAT32 used for EFI System Partitions (ESP) on ALL UEFI VMs
    #   - Coverage: Every UEFI VM has an ESP partition
    dosfstools \
    #
    # =========================================================================
    # Category 4: LVM & Partition Tools - Handle Complex Disk Layouts
    # =========================================================================
    # lvm2: Logical Volume Manager support
    #   - Why: Many Linux VMs use LVM for flexible disk management
    #   - What: Detects and accesses LVM physical volumes, volume groups, logical volumes
    #   - Note: guestmount automatically handles LVM when these tools are present
    #   - Coverage: ~60% of RHEL/CentOS VMs use LVM
    lvm2 \
    #
    # parted: Partition table reading (MBR and GPT)
    #   - Why: ALL VM disks have partition tables
    #   - Coverage: 100% of VMs have partitions
    parted \
    #
    # gdisk: GPT partition table support (UEFI VMs)
    #   - Why: GPT (GUID Partition Table) is standard for modern UEFI VMs
    #   - Coverage: ~90% of modern VMs use GPT
    gdisk \
    #
    # =========================================================================
    # Category 5: Utility Tools - Detection & Debugging
    # =========================================================================
    # file: File type detection by content (magic numbers)
    #   - Why: Verify actual file type, not just extension
    #   - Example: file disk.qcow2 → "QEMU QCOW2 Image (v3)"
    file \
    #
    # util-linux: Block device utilities
    #   - Provides: lsblk, blkid, mount
    #   - Use case: Inspect mounted filesystems, debug block devices
    util-linux \
    #
    # findutils, coreutils: Standard Unix tools
    #   - Provides: find, ls, cat, grep, awk, etc.
    #   - Use case: Used by detect-and-mount.sh automation scripts
    findutils \
    coreutils \
    #
    # Clean up dnf cache to reduce image size
    && dnf clean all \
    && rm -rf /var/cache/dnf

# Copy helper scripts into container
# - detect-and-mount.sh: Auto-detects disk formats and mounts filesystems read-only
# - entrypoint.sh: Simple entrypoint that keeps container alive
COPY scripts/detect-and-mount.sh /usr/local/bin/detect-and-mount.sh
COPY scripts/entrypoint.sh /usr/local/bin/entrypoint.sh

# Make scripts executable
RUN chmod +x /usr/local/bin/detect-and-mount.sh /usr/local/bin/entrypoint.sh

# Create mount points and working directories
# - /mnt/volumes: Where controller will mount restored PVCs containing disk images
# - /mnt/filesystems: Where mounted VM filesystems will appear
# - /var/www/files: Working directory for the container
RUN mkdir -p /mnt/volumes /mnt/filesystems /var/www/files

# Create non-root user for running the file server
# OpenShift Security Best Practices:
# - UID 1001: Standard non-root user ID
# - GID 0: Root group (OpenShift assigns arbitrary UIDs in root group)
# - g+rwX permissions: Allow group access for OpenShift's arbitrary UID assignment
RUN useradd -u 1001 -r -g 0 -m -d /var/www/files -s /sbin/nologin \
    -c "File Server User" fileserver && \
    chown -R 1001:0 /var/www/files /mnt/volumes /mnt/filesystems && \
    chmod -R g+rwX /var/www/files /mnt/volumes /mnt/filesystems

# Set working directory
WORKDIR /var/www/files

# Run as non-root user (OpenShift requirement)
USER 1001

# Default command - keeps container alive for controller management
# The VMFR controller (Issue #7) can override this command to:
# - Run detect-and-mount.sh on pod startup
# - Execute other scripts via kubectl exec
# - Implement custom file serving mechanisms (Issues #8, #9)
CMD ["/usr/local/bin/entrypoint.sh"]

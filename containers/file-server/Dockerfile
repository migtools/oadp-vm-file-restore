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

# Install all required packages for VM disk and filesystem access
# Fedora has all these packages natively available (no EPEL needed)
# Package selection based on KubeVirt research (libguestfs, qemu, etc.)
#
# Core VM disk image tools (qcow2, raw, vmdk support)
# - libguestfs-tools: Comprehensive VM disk manipulation
# - qemu-img: Disk image format detection and conversion
# - qemu-kvm-core: QEMU/KVM core functionality
#
# Network Block Device (NBD) support
# - nbd: Mount qcow2/raw disk images as block devices
#
# Filesystem utilities for common VM filesystems
# - e2fsprogs: ext2/ext3/ext4 (Linux)
# - xfsprogs: XFS (Linux)
# - btrfs-progs: Btrfs (Linux)
# - ntfs-3g: NTFS (Windows)
# - dosfstools: FAT/FAT32 (boot partitions, Windows)
#
# LVM and partition support
# - lvm2: Logical Volume Manager (common in Linux VMs)
# - parted: Partition table manipulation
# - gdisk: GPT partition support
#
# Additional utilities
# - fuse/fuse-libs: Userspace filesystem mounting
# - file: File type detection
# - util-linux: blkid, lsblk, mount, etc.
# - findutils, coreutils: Basic Unix utilities
RUN dnf install -y \
    libguestfs-tools \
    qemu-img \
    qemu-kvm-core \
    nbd \
    e2fsprogs \
    xfsprogs \
    btrfs-progs \
    ntfs-3g \
    dosfstools \
    lvm2 \
    parted \
    gdisk \
    fuse \
    fuse-libs \
    file \
    util-linux \
    findutils \
    coreutils \
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

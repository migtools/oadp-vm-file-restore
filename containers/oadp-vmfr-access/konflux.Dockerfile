# OADP VM File Access Container - DOWNSTREAM (Red Hat Konflux Build)
# =====================================================================
# This is the DOWNSTREAM Dockerfile for Red Hat's Konflux build system
# Used for building official OADP product images
#
# Pattern follows: openshift/oadp-operator konflux.Dockerfile
# Build system: Red Hat Konflux (successor to OSBS)

FROM registry.access.redhat.com/ubi9/ubi:latest

# Metadata following Red Hat container standards
LABEL name="oadp-vmfr-access" \
      vendor="Red Hat" \
      summary="OADP VM File Access Container" \
      description="Container with tools for accessing and mounting VM disk images from OADP backups" \
      io.k8s.description="Provides filesystem tools for VM file restore in OADP" \
      io.openshift.tags="oadp,backup,restore,vm,filesystem" \
      io.k8s.display-name="OADP VM File Access Container" \
      com.redhat.component="oadp-vmfr-access-container" \
      version="1.5.0"

# Install required licenses
COPY containers/oadp-vmfr-access/LICENSE /licenses/LICENSE

# ==============================================================================
# Package Installation - All required tools for VM disk and filesystem access
# ==============================================================================
# Red Hat Konflux builds have access to full RHEL repositories automatically
# No manual subscription management needed (handled by build system)
#
RUN dnf install -y \
    # =========================================================================
    # Category 1: VM Disk Image Tools - CRITICAL for reading VM disk images
    # =========================================================================
    # libguestfs: PRIMARY TOOL - Mount VM disks without booting the VM
    #   - Provides: guestmount (FUSE-based mounting), guestfish (inspection),
    #     guestunmount, virt-copy-in, virt-copy-out, virt-tar-in, virt-tar-out
    #   - Use case: Mount backed-up qcow2/raw disk images to extract files
    #   - Example: guestmount -a disk.qcow2 -i /mnt/disk --ro
    #   - Note: In RHEL 9, guestmount/guestfish moved into the base libguestfs
    #     package (libguestfs-tools does not exist as a separate package in el9)
    libguestfs \
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
    #   - What: Allows guestmount to mount filesystems in userspace
    #   - Security: Cleaner than NBD (no kernel module loading required)
    #   - Note: Privileged mode still required for /dev/kvm access (SELinux restriction)
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
    # libguestfs-winsupport: NTFS filesystem support (Windows Server VMs)
    #   - Why: NTFS is the Windows filesystem
    #   - What: Injects NTFS driver into the libguestfs appliance via supermin
    #   - Available in: RHEL 9 AppStream (official Red Hat package)
    #   - Note: Replaces ntfs-3g which is only available in EPEL, not in RHEL repos
    #   - Coverage: ~2% of VMs (Windows workloads)
    libguestfs-winsupport \
    #
    # NOTE: Btrfs is NOT supported on RHEL 9
    #   - Red Hat deprecated Btrfs in RHEL 7 and fully removed it in RHEL 8+
    #   - No kernel module (btrfs.ko), no btrfs-progs, no libguestfs-btrfs in RHEL 9
    #   - btrfs-progs is only available via EPEL, and even then the libguestfs
    #     appliance kernel (based on RHEL) lacks the btrfs module
    #   - Impact: VMs using Btrfs (primarily SUSE guests) cannot be mounted
    #
    # NOTE: LUKS-encrypted VM disks are NOT supported
    #   - cryptsetup is not included; guestmount cannot unlock encrypted partitions
    #   - Supporting LUKS would also require passing decryption keys via Kubernetes
    #     Secrets and CRD changes — not just a package install
    #
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
    # Category 5: Utility Tools
    # =========================================================================
    # Pre-installed in UBI9 base image (no need to install explicitly):
    #   - util-linux (lsblk, blkid, mount, mountpoint)
    #   - findutils (find, xargs)
    #   - coreutils-single (mkdir, basename, date, sleep, ls, cat, etc.)
    #     Note: Do NOT install 'coreutils' — it conflicts with coreutils-single
    #   - python3 (JSON parsing in detect-and-mount.sh)
    #
    # file: File type detection by content (magic numbers)
    #   - Why: Verify actual file type, not just extension
    #   - Example: file disk.qcow2 → "QEMU QCOW2 Image (v3)"
    file \
    #
    # Clean up dnf cache to reduce image size
    && dnf clean all \
    && rm -rf /var/cache/dnf /var/cache/yum

# Copy helper scripts into container
# - detect-and-mount.sh: Auto-detects disk formats and mounts filesystems read-only
# - entrypoint.sh: Simple entrypoint that keeps container alive
COPY containers/oadp-vmfr-access/scripts/detect-and-mount.sh /usr/local/bin/detect-and-mount.sh
COPY containers/oadp-vmfr-access/scripts/entrypoint.sh /usr/local/bin/entrypoint.sh

# Make scripts executable
RUN chmod +x /usr/local/bin/detect-and-mount.sh /usr/local/bin/entrypoint.sh

# Create mount points and working directories
# - /mnt/volumes: Where controller will mount restored PVCs containing disk images
# - /mnt/filesystems: Where mounted VM filesystems will appear
# - /var/www/files: Working directory for the container
RUN mkdir -p /mnt/volumes /mnt/filesystems /var/www/files

# Create qemu user and group (UID/GID 107 to match KubeVirt)
# Set permissions for qemu user
# Note: UBI 9 minimal may not have qemu user by default, so we create it
RUN groupadd -g 107 qemu 2>/dev/null || true && \
    useradd -u 107 -g 107 -r -m -d /var/www/files -s /sbin/nologin \
        -c "QEMU User" qemu 2>/dev/null || true && \
    chown -R 107:107 /var/www/files /mnt/volumes /mnt/filesystems && \
    chmod -R u+rwX,g+rwX /var/www/files /mnt/volumes /mnt/filesystems

# Set working directory
WORKDIR /var/www/files

# Run as qemu user (matches KubeVirt virt-launcher)
# OpenShift Virtualization Security Context:
# - UID 107: qemu user (matches VM disk ownership in KubeVirt)
# - GID 107: qemu group
# - This matches how virt-launcher pods run
# Note: Pod security context will override this to 107:107 explicitly
USER 107

# Default command - keeps container alive for controller management
# The VMFR controller (Issue #7) can override this command to:
# - Run detect-and-mount.sh on pod startup
# - Execute other scripts via kubectl exec
# - Implement custom file serving mechanisms (Issues #8, #9)
CMD ["/usr/local/bin/entrypoint.sh"]

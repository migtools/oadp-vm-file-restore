#!/bin/bash
# detect-and-mount.sh
# ===================
# Automatically detect and mount VM disk images and filesystems in read-only mode
#
# Purpose:
#   Process VM disk images from restored OADP backups and make their filesystems
#   accessible for file browsing and recovery. Handles various disk formats and
#   filesystem types commonly found in KubeVirt VMs.
#
# Supported Disk Formats:
#   - qcow2 (QEMU Copy-On-Write, most common for KubeVirt)
#   - raw (Raw disk images)
#   - Future: vmdk, vdi (can be added if needed)
#
# Supported Filesystems:
#   - ext2/ext3/ext4 (Linux)
#   - XFS (Linux)
#   - Btrfs (Linux)
#   - NTFS (Windows)
#   - FAT/FAT32 (Boot partitions, Windows)
#
# How It Works:
#   1. detect_disk_format(): Use qemu-img to identify disk image type
#   2. connect_nbd_device(): Connect disk to /dev/nbd* using qemu-nbd
#   3. detect_filesystem(): Use blkid to identify filesystem type on partitions
#   4. mount_filesystem(): Mount each filesystem read-only with appropriate options
#   5. process_disk_image(): Orchestrate the above steps for each disk
#   6. auto_mount_all(): Discover and mount all disks in /mnt/volumes/
#
# Read-Only Safety:
#   - All filesystems mounted with -o ro (read-only)
#   - Filesystem-specific safety options (noload for ext4, norecovery for xfs)
#   - qemu-nbd connects in read-only mode
#
# Usage:
#   - No arguments: Auto-discover and mount all disk images in /mnt/volumes/
#   - With argument: Mount specific disk image
#     Example: detect-and-mount.sh /mnt/volumes/disk.qcow2

set -euo pipefail

# Default mount points
# VOLUME_MOUNT_DIR: Where restored PVCs are mounted (controller provides this)
# FS_MOUNT_DIR: Where VM filesystems will be mounted for user access
VOLUME_MOUNT_DIR="${VOLUME_MOUNT_DIR:-/mnt/volumes}"
FS_MOUNT_DIR="${FS_MOUNT_DIR:-/mnt/filesystems}"

log() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] $*"
}

error() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] ERROR: $*" >&2
}

# detect_disk_format - Identify the disk image format
#
# Uses qemu-img to detect the format of a disk image file.
# This is necessary because VM disks can be in various formats (qcow2, raw, vmdk, etc.)
#
# Args:
#   $1 - Path to disk image file
#
# Returns:
#   Disk format string (qcow2, raw, etc.) via stdout
#   Exit code 1 on error
detect_disk_format() {
    local disk_path="$1"

    if [[ ! -f "$disk_path" ]]; then
        error "Disk image not found: $disk_path"
        return 1
    fi

    # Use qemu-img info to inspect the disk image
    # Example output: "file format: qcow2"
    local format
    format=$(qemu-img info "$disk_path" | grep "^file format:" | awk '{print $3}')

    if [[ -z "$format" ]]; then
        error "Could not detect disk format for: $disk_path"
        return 1
    fi

    echo "$format"
}

# connect_nbd_device - Connect disk image to Network Block Device
#
# Uses qemu-nbd to connect a qcow2/raw disk image to a /dev/nbd* device.
# This allows us to access the disk as if it were a real block device,
# enabling partition detection and filesystem mounting.
#
# NBD (Network Block Device) is a Linux kernel module that allows treating
# remote or file-based storage as a local block device.
#
# Args:
#   $1 - Path to disk image file
#   $2 - Disk format (qcow2, raw, etc.)
#
# Returns:
#   NBD device path (e.g., /dev/nbd0) via stdout
#   Exit code 1 on error
#
# Note: Requires privileged container or CAP_SYS_ADMIN capability to load kernel modules
connect_nbd_device() {
    local disk_path="$1"
    local format="$2"

    # Load NBD kernel module if not already loaded
    # max_part=8 allows up to 8 partitions per NBD device
    if ! lsmod | grep -q nbd; then
        log "Loading NBD kernel module..."
        modprobe nbd max_part=8 || {
            error "Failed to load NBD module. Container may need privileged security context."
            return 1
        }
    fi

    # Find an available NBD device (check /dev/nbd0 through /dev/nbd15)
    local nbd_device
    for i in {0..15}; do
        nbd_device="/dev/nbd$i"
        # Check if device exists and is not already in use
        if [[ -b "$nbd_device" ]] && ! qemu-nbd --list "$nbd_device" >/dev/null 2>&1; then
            log "Connecting $disk_path to $nbd_device (format: $format)"
            # Connect in read-only mode for data safety
            qemu-nbd --read-only --format="$format" --connect="$nbd_device" "$disk_path"

            # Wait for kernel to detect partitions
            sleep 2

            echo "$nbd_device"
            return 0
        fi
    done

    error "No available NBD devices found"
    return 1
}

# detect_filesystem - Identify filesystem type on a partition
#
# Uses blkid to detect the filesystem type (ext4, xfs, ntfs, etc.)
# on a block device or partition.
#
# Args:
#   $1 - Device path (e.g., /dev/nbd0, /dev/nbd0p1)
#
# Returns:
#   Filesystem type string via stdout (e.g., ext4, xfs, ntfs)
#   Returns "unknown" if filesystem cannot be detected
detect_filesystem() {
    local device="$1"

    local fstype
    # blkid reads filesystem metadata to detect type
    fstype=$(blkid -s TYPE -o value "$device" 2>/dev/null || echo "unknown")

    echo "$fstype"
}

# mount_filesystem - Mount a filesystem in read-only mode
#
# Mounts a filesystem with appropriate read-only options for each filesystem type.
# Uses filesystem-specific safety options to prevent any modifications.
#
# Read-Only Options by Filesystem:
#   - ext2/3/4: noload (don't load journal, prevent any writes)
#   - xfs: norecovery (skip journal replay, prevent modifications)
#   - ntfs: ro (ntfs-3g read-only mode)
#   - btrfs: ro (standard read-only)
#   - vfat/fat: ro (standard read-only)
#
# Args:
#   $1 - Device path (e.g., /dev/nbd0p1)
#   $2 - Mount point directory path
#   $3 - Filesystem type
#
# Returns:
#   Exit code 0 on success, 1 on failure
mount_filesystem() {
    local device="$1"
    local mount_point="$2"
    local fstype="$3"

    mkdir -p "$mount_point"

    log "Mounting $device ($fstype) at $mount_point (read-only)"

    case "$fstype" in
        ext2|ext3|ext4)
            # noload: Don't load the journal (prevents writes)
            mount -t "$fstype" -o ro,noload "$device" "$mount_point"
            ;;
        xfs)
            # norecovery: Skip journal replay (read-only safe mode)
            mount -t xfs -o ro,norecovery "$device" "$mount_point"
            ;;
        ntfs)
            # ntfs-3g with read-only option
            ntfs-3g -o ro "$device" "$mount_point"
            ;;
        btrfs)
            # Btrfs with standard read-only
            mount -t btrfs -o ro "$device" "$mount_point"
            ;;
        vfat|fat|msdos)
            # FAT filesystems (common for boot partitions)
            mount -t vfat -o ro "$device" "$mount_point"
            ;;
        *)
            # Attempt generic read-only mount for unknown filesystem types
            log "Unknown filesystem type: $fstype, attempting generic read-only mount"
            mount -o ro "$device" "$mount_point" || {
                error "Failed to mount $device"
                return 1
            }
            ;;
    esac

    log "Successfully mounted $device at $mount_point"
}

# process_disk_image - Main orchestration function to process a single disk image
#
# This function coordinates all steps to make a VM disk image's filesystems accessible:
#   1. Detect disk format (qcow2, raw, etc.)
#   2. Connect disk to NBD device
#   3. Detect partitions
#   4. Mount each partition's filesystem
#
# Handles two scenarios:
#   - Partitioned disks: Mounts each partition separately (e.g., /mnt/filesystems/disk-p1, disk-p2)
#   - Non-partitioned disks: Mounts filesystem directly (e.g., /mnt/filesystems/disk)
#
# Args:
#   $1 - Path to disk image file
#   $2 - (Optional) Volume name for mount point naming (defaults to filename)
#
# Returns:
#   Exit code 0 on success, 1 on failure
process_disk_image() {
    local disk_path="$1"
    local volume_name="${2:-$(basename "$disk_path")}"

    log "Processing disk image: $disk_path"

    # Step 1: Detect the disk format
    local format
    format=$(detect_disk_format "$disk_path")
    log "Detected format: $format"

    # Step 2: Handle qcow2/raw formats using NBD
    if [[ "$format" == "qcow2" ]] || [[ "$format" == "raw" ]]; then
        local nbd_device
        nbd_device=$(connect_nbd_device "$disk_path" "$format")

        if [[ -z "$nbd_device" ]]; then
            error "Failed to connect NBD device"
            return 1
        fi

        # Wait for kernel to detect partitions on the NBD device
        sleep 2

        # Step 3: Try to detect and mount partitions
        local mounted=false

        # Check if disk has partitions (indicated by /dev/nbd0p1, /dev/nbd0p2, etc.)
        if [[ -b "${nbd_device}p1" ]]; then
            # Disk has partitions - mount each one
            log "Disk has partitions, mounting each partition separately"
            for part in "${nbd_device}p"*; do
                if [[ -b "$part" ]]; then
                    local fstype
                    fstype=$(detect_filesystem "$part")

                    if [[ "$fstype" != "unknown" ]] && [[ -n "$fstype" ]]; then
                        # Extract partition number for naming
                        local part_num=${part##*p}
                        local mount_point="$FS_MOUNT_DIR/${volume_name}-p${part_num}"

                        mount_filesystem "$part" "$mount_point" "$fstype" && mounted=true
                    fi
                fi
            done
        else
            # No partitions - filesystem directly on device (less common)
            log "No partitions detected, attempting to mount device directly"
            local fstype
            fstype=$(detect_filesystem "$nbd_device")

            if [[ "$fstype" != "unknown" ]] && [[ -n "$fstype" ]]; then
                local mount_point="$FS_MOUNT_DIR/${volume_name}"
                mount_filesystem "$nbd_device" "$mount_point" "$fstype" && mounted=true
            fi
        fi

        if [[ "$mounted" == "false" ]]; then
            error "No mountable filesystems found on $disk_path"
            return 1
        fi
    else
        log "Unsupported or unknown disk format: $format"
        return 1
    fi

    log "Successfully processed disk image: $disk_path"
}

# auto_mount_all - Auto-discover and mount all disk images in a directory
#
# Scans VOLUME_MOUNT_DIR for common VM disk image file extensions and
# processes each found disk image. This is the default mode when the
# script is run without arguments.
#
# Supported extensions:
#   - .qcow2 (most common for KubeVirt/QEMU)
#   - .raw
#   - .img
#   - .qcow
#   - .vmdk (VMware, less common but supported)
#
# Returns:
#   Always returns 0 (does not fail if no disks found or some fail to mount)
auto_mount_all() {
    log "Auto-mounting all disk images in $VOLUME_MOUNT_DIR"

    local found=false

    # Look for common VM disk image file extensions
    # Bash brace expansion: expands to multiple patterns
    for disk in "$VOLUME_MOUNT_DIR"/*.{qcow2,raw,img,qcow,vmdk}; do
        if [[ -f "$disk" ]]; then
            found=true
            # Process each disk, but don't fail on individual errors
            process_disk_image "$disk" || log "Warning: Failed to process $disk"
        fi
    done

    if [[ "$found" == "false" ]]; then
        log "No disk images found in $VOLUME_MOUNT_DIR"
    fi

    log "Auto-mount completed"
}

# main - Entry point for the script
#
# Two modes of operation:
#   1. No arguments: Auto-discover and mount all disks in VOLUME_MOUNT_DIR
#   2. With arguments: Process the specific disk image provided
#
# This allows the script to be used both by the controller (auto mode)
# and manually by users for specific disks.
main() {
    if [[ $# -eq 0 ]]; then
        # No arguments: auto-discover and mount all disk images
        auto_mount_all
    else
        # Arguments provided: process specific disk image
        process_disk_image "$@"
    fi
}

# Execute main function with all script arguments
main "$@"

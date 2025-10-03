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
#   - vmdk (VMware disks)
#   - vdi (VirtualBox disks)
#
# Supported Filesystems:
#   - ext2/ext3/ext4 (Linux)
#   - XFS (Linux)
#   - Btrfs (Linux)
#   - NTFS (Windows)
#   - FAT/FAT32 (Boot partitions, Windows)
#   - LVM (Logical Volume Manager)
#
# How It Works (FUSE-based approach):
#   1. detect_disk_format(): Use qemu-img to identify disk image type
#   2. mount_disk_with_guestmount(): Use guestmount (FUSE) to mount the entire disk
#   3. process_disk_image(): Orchestrate the mounting for each disk
#   4. auto_mount_all(): Discover and mount all disks in /mnt/volumes/
#
# Why FUSE/guestmount instead of NBD?
#   - Works in Kubernetes without privileged containers
#   - No kernel module loading required (modprobe nbd)
#   - No /dev access needed
#   - Compatible with OpenShift security policies
#   - Trade-off: Slightly slower than NBD, but much better security
#
# Read-Only Safety:
#   - All filesystems mounted with --ro (read-only)
#   - guestmount ensures data safety
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

# mount_disk_with_guestmount - Mount VM disk using FUSE-based guestmount
#
# Uses guestmount (from libguestfs) to mount VM disk images via FUSE.
# This approach works in Kubernetes without requiring privileged containers.
#
# guestmount capabilities:
#   - Auto-detects partitions and filesystems (-i flag)
#   - Supports LVM, RAID, encryption
#   - Works with qcow2, raw, vmdk, vdi formats
#   - Read-only mounting for data safety
#
# Why guestmount over NBD?
#   - No kernel module loading (no modprobe nbd)
#   - No /dev access required
#   - Works with standard Kubernetes security policies
#   - OpenShift SecurityContextConstraints compatible
#
# Args:
#   $1 - Path to disk image file
#   $2 - Mount point directory
#   $3 - Volume name (for logging)
#
# Returns:
#   Exit code 0 on success, 1 on failure
mount_disk_with_guestmount() {
    local disk_path="$1"
    local mount_point="$2"
    local volume_name="$3"

    # Create mount point
    mkdir -p "$mount_point"

    log "Mounting $disk_path at $mount_point using guestmount (FUSE)"

    # Use guestmount with auto-inspection (-i)
    # --ro: Read-only mode (data safety)
    # -a: Add disk image
    # -i: Auto-inspect and mount all filesystems
    # -o allow_other: Allow other users to access (useful for multi-user pods)
    if guestmount -a "$disk_path" -i --ro -o allow_other "$mount_point" 2>&1; then
        log "✓ Successfully mounted $volume_name at $mount_point"
        return 0
    else
        error "Failed to mount $disk_path with guestmount"

        # Try alternative: mount first partition only
        log "Attempting to mount first partition only..."
        if guestmount -a "$disk_path" -m /dev/sda1 --ro -o allow_other "$mount_point" 2>&1; then
            log "✓ Successfully mounted first partition of $volume_name"
            return 0
        fi

        error "All mount attempts failed for $disk_path"
        return 1
    fi
}

# unmount_disk - Safely unmount a guestmount filesystem
#
# Uses guestunmount or fusermount to unmount FUSE filesystems.
# This is useful for cleanup or remounting.
#
# Args:
#   $1 - Mount point to unmount
#
# Returns:
#   Exit code 0 on success
unmount_disk() {
    local mount_point="$1"

    if [[ ! -d "$mount_point" ]]; then
        return 0
    fi

    log "Unmounting $mount_point"

    # Try guestunmount first (preferred)
    if command -v guestunmount >/dev/null 2>&1; then
        guestunmount "$mount_point" || fusermount -u "$mount_point"
    else
        # Fallback to fusermount
        fusermount -u "$mount_point"
    fi

    log "✓ Unmounted $mount_point"
}

# process_disk_image - Main orchestration function to process a single disk image
#
# This function coordinates all steps to make a VM disk image's filesystems accessible:
#   1. Detect disk format (qcow2, raw, etc.)
#   2. Mount entire disk using guestmount (FUSE)
#
# The guestmount tool handles:
#   - Partition detection automatically
#   - LVM volume detection
#   - Filesystem type detection
#   - Read-only mounting
#
# Args:
#   $1 - Path to disk image file
#   $2 - (Optional) Volume name for mount point naming (defaults to filename)
#
# Returns:
#   Exit code 0 on success, 1 on failure
process_disk_image() {
    local disk_path="$1"
    local volume_name="${2:-$(basename "$disk_path" | sed 's/\.[^.]*$//')}"

    log "Processing disk image: $disk_path"

    # Step 1: Detect the disk format (for validation and logging)
    local format
    format=$(detect_disk_format "$disk_path")
    log "Detected format: $format"

    # Step 2: Validate format is supported
    case "$format" in
        qcow2|raw|vmdk|vdi)
            log "Format $format is supported"
            ;;
        *)
            log "Warning: Format $format may not be fully supported, attempting anyway"
            ;;
    esac

    # Step 3: Mount the disk using guestmount (FUSE-based)
    local mount_point="$FS_MOUNT_DIR/${volume_name}"

    if mount_disk_with_guestmount "$disk_path" "$mount_point" "$volume_name"; then
        log "✓ Successfully processed disk image: $disk_path"
        log "Files are now accessible at: $mount_point"
        return 0
    else
        error "Failed to process disk image: $disk_path"
        return 1
    fi
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
#   - .vdi (VirtualBox)
#
# Returns:
#   Always returns 0 (does not fail if no disks found or some fail to mount)
auto_mount_all() {
    log "Auto-mounting all disk images in $VOLUME_MOUNT_DIR"

    local found=false
    local success_count=0
    local fail_count=0

    # Look for common VM disk image file extensions
    # Bash brace expansion: expands to multiple patterns
    for disk in "$VOLUME_MOUNT_DIR"/*.{qcow2,raw,img,qcow,vmdk,vdi}; do
        # Check if file actually exists (glob may not match anything)
        if [[ -f "$disk" ]]; then
            found=true
            log "Found disk image: $disk"

            # Process each disk, but don't fail on individual errors
            if process_disk_image "$disk"; then
                ((success_count++))
            else
                ((fail_count++))
                log "Warning: Failed to process $disk, continuing with remaining disks"
            fi
        fi
    done

    if [[ "$found" == "false" ]]; then
        log "No disk images found in $VOLUME_MOUNT_DIR"
        log "Supported extensions: .qcow2, .raw, .img, .qcow, .vmdk, .vdi"
    else
        log "Auto-mount completed: $success_count successful, $fail_count failed"
    fi

    # List mounted filesystems
    log "Currently mounted filesystems:"
    mount | grep "$FS_MOUNT_DIR" || log "No filesystems currently mounted"
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
    log "OADP VM File Restore - Filesystem Mounter (FUSE-based)"
    log "Using guestmount for Kubernetes-compatible mounting"

    if [[ $# -eq 0 ]]; then
        # No arguments: auto-discover and mount all disk images
        auto_mount_all
    else
        # Arguments provided: process specific disk image
        process_disk_image "$@"
    fi

    log "Mounting operations completed"
    log "Filesystems mounted at: $FS_MOUNT_DIR"
}

# Execute main function with all script arguments
main "$@"

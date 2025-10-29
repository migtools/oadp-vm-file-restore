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
# Directory Structure:
#   When BACKUP_PVC_MAP is provided, creates structured mounts:
#     /mnt/filesystems/
#     ├── backup-20240115/
#     │   ├── rootdisk/
#     │   └── datadisk/
#     └── backup-20240120/
#         └── rootdisk/
#
# Environment Variables:
#   BACKUP_PVC_MAP - JSON mapping of backups to PVCs (from VMFR controller)
#     Example: {"backup-20240115": [{"name": "rootdisk", "path": "/mnt/volumes/backup-20240115/rootdisk"}]}
#
# How It Works (FUSE-based approach):
#   1. Parse BACKUP_PVC_MAP (if provided) or auto-discover
#   2. detect_disk_format(): Use qemu-img to identify disk image type
#   3. mount_disk_with_guestmount(): Use guestmount (FUSE) to mount the entire disk
#   4. Generate METADATA.json with mount information
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
#   - With BACKUP_PVC_MAP env var: Use structured directory layout
#   - No arguments: Auto-discover and mount all disk images in /mnt/volumes/
#   - With argument: Mount specific disk image
#     Example: detect-and-mount.sh /mnt/volumes/disk.qcow2

set -uo pipefail

# Default mount points
# VOLUME_MOUNT_DIR: Where restored PVCs are mounted (controller provides this)
# FS_MOUNT_DIR: Where VM filesystems will be mounted for user access
# Changed from /mnt/filesystems to /restores for direct, organized access
VOLUME_MOUNT_DIR="${VOLUME_MOUNT_DIR:-/mnt/volumes}"
FS_MOUNT_DIR="${FS_MOUNT_DIR:-/restores}"

log() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] $*"
}

error() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] ERROR: $*" >&2
}

# cleanup_mounts - Cleanup function called on script termination
# This is the secondary mechanism (trap-based) that works with the primary
# PreStop hook in the controller. See issue #44.
# Unmounts all FUSE filesystems to prevent pods/PVCs from getting stuck in Terminating state
cleanup_mounts() {
    # Disable strict error handling for cleanup - we want to try everything
    # even if individual operations fail
    set +e
    set +u
    set +o pipefail

    log "[Trap] Received termination signal, cleaning up FUSE mounts..."

    local mounted_count=0
    local unmounted_count=0

    # Find all FUSE mount points under /restores/
    # Structure: /restores/<date>/<backup>/<pvc>/
    if [ -d "$FS_MOUNT_DIR" ]; then
        # Use shopt to handle failed glob patterns gracefully
        shopt -s nullglob 2>/dev/null || true
        for mount_point in "$FS_MOUNT_DIR"/*/*/*; do
            # Check if it's a directory
            if [ -d "$mount_point" ]; then
                # Check if it's actually a mount point
                if mountpoint -q "$mount_point" 2>/dev/null; then
                    mounted_count=$((mounted_count + 1))
                    log "[Trap] Unmounting: $mount_point"
                    # Use existing unmount_disk function
                    unmount_disk "$mount_point" || true
                    unmounted_count=$((unmounted_count + 1))
                fi
            fi
        done
        shopt -u nullglob 2>/dev/null || true
    fi

    log "[Trap] Cleanup complete: unmounted $unmounted_count of $mounted_count FUSE mounts"
    log "[Trap] Exiting gracefully"
}

# Set up trap to handle termination signals
# SIGTERM: Kubernetes sends this when deleting the pod
# SIGINT: User presses Ctrl+C
# EXIT: Script exits for any reason
trap cleanup_mounts SIGTERM SIGINT EXIT

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

    if [[ ! -f "$disk_path" ]] && [[ ! -b "$disk_path" ]]; then
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

# mount_with_structure - Mount backups using BACKUP_PVC_MAP structure
#
# Parses BACKUP_PVC_MAP environment variable and mounts each backup/PVC
# in an organized directory layout:
#   /restores/<date>/<backup-name>/<pvc-name>/
#
# Example: /restores/2025-10-24/test-vm-backup-20250115/test-vm-disk-1/
#
# BACKUP_PVC_MAP format (JSON):
#   {
#     "backup-20240115": [
#       {"name": "rootdisk", "path": "/dev/pvc-xxx", "timestamp": "2025-10-24T01:14:25Z"},
#       {"name": "datadisk", "path": "/dev/pvc-yyy", "timestamp": "2025-10-24T01:14:25Z"}
#     ],
#     "backup-20240120": [...]
#   }
#
# Returns:
#   Exit code 0 on success
mount_with_structure() {
    log "Using organized mounting with BACKUP_PVC_MAP"
    log "Mounting at: /restores/<date>/<backup>/<pvc>/"

    local success_count=0
    local fail_count=0

    # Parse JSON and extract: backup_name, pvc_name, pvc_path, date
    python3 <<'PYEOF' | while IFS='|' read -r backup_name pvc_name pvc_path backup_date; do
import json, os, sys
from datetime import datetime

try:
    backup_map = json.loads(os.environ.get('BACKUP_PVC_MAP', '{}'))
    for backup_name, pvcs in backup_map.items():
        for pvc in pvcs:
            # Extract date from timestamp (YYYY-MM-DD)
            backup_timestamp = pvc.get('timestamp', '')
            if backup_timestamp:
                try:
                    dt = datetime.fromisoformat(backup_timestamp.replace('Z', '+00:00'))
                    backup_date = dt.strftime('%Y-%m-%d')
                except:
                    backup_date = datetime.now().strftime('%Y-%m-%d')
            else:
                backup_date = datetime.now().strftime('%Y-%m-%d')

            print(f"{backup_name}|{pvc.get('name', 'unknown')}|{pvc.get('path', '')}|{backup_date}")
except Exception as e:
    print(f"ERROR: Failed to parse BACKUP_PVC_MAP: {e}", file=sys.stderr)
    sys.exit(1)
PYEOF

        # Skip if parsing failed
        if [[ -z "$backup_name" ]] || [[ -z "$pvc_name" ]] || [[ -z "$pvc_path" ]]; then
            continue
        fi

        log "Processing: $backup_date/$backup_name/$pvc_name"

        # Find disk image in the PVC path (can be regular file or block device)
        local disk_image=""

        # Check if pvc_path itself is a block device (Block mode PVC)
        if [ -b "$pvc_path" ]; then
            disk_image="$pvc_path"
            log "Found block device (Block mode PVC): $disk_image"
        else
            # Filesystem mode PVC: look for disk image files
            for ext in img raw qcow2 qcow vmdk vdi; do
                if [ -f "$pvc_path/disk.$ext" ]; then
                    disk_image="$pvc_path/disk.$ext"
                    break
                fi
            done
        fi

        if [ -z "$disk_image" ]; then
            error "No disk image found in $pvc_path"
            ((fail_count++))
            continue
        fi

        log "Found disk image: $disk_image"

        # Detect format
        local format
        format=$(detect_disk_format "$disk_image") || {
            error "Failed to detect format for $disk_image"
            ((fail_count++))
            continue
        }

        # Create mount point: /restores/<date>/<backup>/<pvc>/
        local mount_point="$FS_MOUNT_DIR/$backup_date/$backup_name/$pvc_name"
        mkdir -p "$mount_point"

        # Mount the disk directly at the organized path
        if mount_disk_with_guestmount "$disk_image" "$mount_point" "$backup_date/$backup_name/$pvc_name"; then
            ((success_count++))
            log "✓ Mounted $backup_date/$backup_name/$pvc_name successfully"
        else
            ((fail_count++))
            error "Failed to mount $backup_date/$backup_name/$pvc_name"
        fi
    done

    log "Organized mounting completed: $success_count successful, $fail_count failed"

    # Generate METADATA.json
    generate_metadata

    return 0
}

# generate_metadata - Create METADATA.json with mount information
#
# Generates a JSON file at /restores/METADATA.json with details
# about all mounted backups and PVCs organized by date.
#
generate_metadata() {
    local metadata_file="$FS_MOUNT_DIR/METADATA.json"

    log "Generating metadata file: $metadata_file"

    # Use Python to generate JSON metadata
    python3 <<'PYEOF'
import json, os, sys
from datetime import datetime

backup_map = json.loads(os.environ.get('BACKUP_PVC_MAP', '{}'))
fs_mount_dir = os.environ.get('FS_MOUNT_DIR', '/restores')

metadata = {
    "mounted_at": datetime.now().isoformat(),
    "mount_structure": "date-organized",
    "path_format": "/restores/<date>/<backup>/<pvc>/",
    "backups": [],
    "statistics": {
        "total_backups": len(backup_map),
        "total_pvcs": sum(len(pvcs) for pvcs in backup_map.values()),
        "successful_mounts": 0,
        "failed_mounts": 0
    }
}

for backup_name, pvcs in backup_map.items():
    backup_entry = {
        "backup_name": backup_name,
        "pvcs": []
    }

    for pvc in pvcs:
        # Extract date from timestamp
        backup_timestamp = pvc.get('timestamp', '')
        if backup_timestamp:
            try:
                dt = datetime.fromisoformat(backup_timestamp.replace('Z', '+00:00'))
                backup_date = dt.strftime('%Y-%m-%d')
            except:
                backup_date = datetime.now().strftime('%Y-%m-%d')
        else:
            backup_date = datetime.now().strftime('%Y-%m-%d')

        # Build mount path: /restores/<date>/<backup>/<pvc>/
        mount_path = f"{fs_mount_dir}/{backup_date}/{backup_name}/{pvc['name']}"

        # Check if mount successful (directory exists and not empty)
        mount_status = "success" if os.path.exists(mount_path) and os.listdir(mount_path) else "failed"

        if mount_status == "success":
            metadata["statistics"]["successful_mounts"] += 1
        else:
            metadata["statistics"]["failed_mounts"] += 1

        pvc_entry = {
            "pvc_name": pvc['name'],
            "source_path": pvc['path'],
            "mount_path": mount_path,
            "backup_date": backup_date,
            "mount_status": mount_status
        }

        backup_entry["pvcs"].append(pvc_entry)

    metadata["backups"].append(backup_entry)

# Write metadata file
metadata_file = f"{fs_mount_dir}/METADATA.json"
with open(metadata_file, 'w') as f:
    json.dump(metadata, f, indent=2)

print(f"✓ Metadata written to {metadata_file}")
PYEOF

    log "✓ Metadata generation complete"
}

# auto_mount_all - Auto-discover and mount all disk images in a directory
#
# Scans VOLUME_MOUNT_DIR for common VM disk image file extensions and
# processes each found disk image. This is the default mode when the
# script is run without arguments.
#
# Supported sources:
#   - Filesystem mode PVCs: Files in VOLUME_MOUNT_DIR with extensions:
#     .qcow2 (most common for KubeVirt/QEMU), .raw, .img, .qcow, .vmdk, .vdi
#   - Block mode PVCs: Block devices at /dev/pvc-* (VM disks as raw block devices)
#
# Returns:
#   Always returns 0 (does not fail if no disks found or some fail to mount)
auto_mount_all() {
    log "Auto-mounting all disk images in $VOLUME_MOUNT_DIR and Block devices at /dev/pvc-*"

    local found=false
    local success_count=0
    local fail_count=0

    # Part 1: Scan for Filesystem mode PVCs (disk image files)
    # Look for common VM disk image file extensions
    # Bash brace expansion: expands to multiple patterns
    for disk in "$VOLUME_MOUNT_DIR"/*.{qcow2,raw,img,qcow,vmdk,vdi}; do
        # Check if file actually exists (glob may not match anything)
        if [[ -f "$disk" ]]; then
            found=true
            log "Found disk image file: $disk"

            # Process each disk, but don't fail on individual errors
            if process_disk_image "$disk"; then
                ((success_count++))
            else
                ((fail_count++))
                log "Warning: Failed to process $disk, continuing with remaining disks"
            fi
        fi
    done

    # Part 2: Scan for Block mode PVCs (block devices at /dev/pvc-*)
    # Block devices are created by Kubernetes when volumeMode: Block is used
    log "Scanning for Block mode PVC devices at /dev/pvc-*"
    for block_device in /dev/pvc-*; do
        # Check if block device actually exists (glob may not match anything)
        if [[ -b "$block_device" ]]; then
            found=true
            log "Found Block mode PVC device: $block_device"

            # Extract PVC UID from device name: /dev/pvc-{uid} -> {uid}
            local pvc_uid="${block_device#/dev/pvc-}"
            local volume_name="pvc-$pvc_uid"

            # Process block device as disk image
            # process_disk_image already handles both files and block devices (see line 91: [[ ! -b "$disk_path" ]])
            if process_disk_image "$block_device" "$volume_name"; then
                ((success_count++))
            else
                ((fail_count++))
                log "Warning: Failed to process Block device $block_device, continuing with remaining devices"
            fi
        fi
    done

    if [[ "$found" == "false" ]]; then
        log "No disk images or Block devices found"
        log "Filesystem mode PVCs: Searched in $VOLUME_MOUNT_DIR for .qcow2, .raw, .img, .qcow, .vmdk, .vdi"
        log "Block mode PVCs: Searched for /dev/pvc-* devices"
    else
        log "Auto-mount completed: $success_count successful, $fail_count failed"
    fi

    # List mounted filesystems
    log "Currently mounted filesystems:"
    mount | grep "$FS_MOUNT_DIR" || true
}

# main - Entry point for the script
#
# Three modes of operation:
#   1. BACKUP_PVC_MAP env var set: Use structured mounting (controller mode)
#   2. No arguments: Auto-discover and mount all disks in VOLUME_MOUNT_DIR
#   3. With arguments: Process the specific disk image provided
#
# This allows the script to be used by:
#   - VMFR controller (with BACKUP_PVC_MAP)
#   - Auto-discovery mode (legacy/simple cases)
#   - Manual mode (specific disk)
main() {
    log "=========================================="
    log "OADP VM File Restore - Filesystem Mounter"
    log "=========================================="
    log "Using guestmount (FUSE-based) for Kubernetes-compatible mounting"
    log ""

    # Check if BACKUP_PVC_MAP is provided (controller mode)
    if [[ -n "${BACKUP_PVC_MAP:-}" ]]; then
        log "Mode: Organized mounting (BACKUP_PVC_MAP provided)"
        log "Creating directory structure: /restores/<date>/<backup>/<pvc>/"
        mount_with_structure
    elif [[ $# -eq 0 ]]; then
        log "Mode: Auto-discovery (scanning $VOLUME_MOUNT_DIR)"
        # No arguments: auto-discover and mount all disk images
        auto_mount_all
    else
        log "Mode: Manual (specific disk image)"
        # Arguments provided: process specific disk image
        process_disk_image "$@"
    fi

    log ""
    log "=========================================="
    log "Mounting operations completed"
    log "=========================================="
    log "Filesystems mounted at: $FS_MOUNT_DIR/<date>/<backup>/<pvc>/"
    log "Example: $FS_MOUNT_DIR/2025-10-24/test-vm-backup-20250115/test-vm-disk-1/"
    log ""
    log "To access files:"
    log "  kubectl exec <pod> -- ls -la $FS_MOUNT_DIR/"
    log ""

    if [[ -f "$FS_MOUNT_DIR/METADATA.json" ]]; then
        log "Metadata available at: $FS_MOUNT_DIR/METADATA.json"
        log "  kubectl exec <pod> -- cat $FS_MOUNT_DIR/METADATA.json"
        log ""
    fi

    # Keep container alive - guestmount process must stay running
    # This is critical: FUSE mounts require the mounting process to remain active
    log "Keeping container alive to maintain FUSE mounts..."
    log "Container will run indefinitely (sleep infinity)"
    log ""
    log "Trap handler installed for graceful cleanup on termination"
    log ""

    # Sleep forever to keep container running
    # IMPORTANT: Do NOT use 'exec sleep' - we need the bash process to stay alive
    # to handle trap signals (SIGTERM/SIGINT) for graceful FUSE unmounting
    # The '& wait $!' pattern allows trap handlers to execute before termination
    # Future: This will be replaced with SSH/HTTP server (Issues #8, #9)
    sleep infinity & wait $!
}

# Execute main function with all script arguments
main "$@"

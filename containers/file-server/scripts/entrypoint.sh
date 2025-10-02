#!/bin/bash
# entrypoint.sh
# Simple entrypoint for the OADP VM file server container
#
# Purpose:
#   - Keeps container running for controller lifecycle management
#   - Can be overridden by controller to run specific scripts
#   - In future issues (#8, #9), this will be enhanced with HTTP/SSH/rsync serving
#
# Usage:
#   - Default (no args): Keeps container alive indefinitely
#   - With args: Executes the provided command
#     Example: docker run <image> /usr/local/bin/detect-and-mount.sh
#
# Integration with Controller (Issue #7):
#   The VMFR controller will create pods with this container and can:
#   - Override command/args to run detect-and-mount.sh on startup
#   - Execute scripts via kubectl exec
#   - Mount restored PVCs at /mnt/volumes/

set -euo pipefail

log() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] $*"
}

error() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] ERROR: $*" >&2
}

main() {
    log "OADP VM File Server Container started"
    log "Container provides tools for VM filesystem access"
    log "Available scripts:"
    log "  - /usr/local/bin/detect-and-mount.sh: Auto-detect and mount VM disk images"
    log ""
    log "This container is ready for controller integration (Issue #7)"
    log "File serving capabilities will be added in issues #8 (SSH/rsync) and #9 (HTTPS)"

    # If arguments provided, execute them
    if [[ $# -gt 0 ]]; then
        log "Executing command: $*"
        exec "$@"
    fi

    # Otherwise, keep container running for controller management
    log "No command provided. Keeping container alive..."
    log "Use 'kubectl exec' to run commands or let controller manage lifecycle"

    # Keep container alive indefinitely
    exec tail -f /dev/null
}

main "$@"

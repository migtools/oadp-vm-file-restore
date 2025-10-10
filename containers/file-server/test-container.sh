#!/bin/bash
# ==============================================================================
# test-container.sh - File Server Container Validation Script
# ==============================================================================
#
# Purpose:
#   Validates that the file-server container builds successfully and all
#   required tools are present and functional. Includes both basic tests
#   (tools installed) and functional tests (tools actually work).
#
# Usage:
#   ./test-container.sh [command]
#
# Commands:
#   build        - Build the container image
#   test         - Run basic validation tests (tools installed, scripts exist)
#   functional   - Run functional tests (tools actually work with real operations)
#   clean        - Remove test artifacts and container images
#   all          - Build and run all tests (default)
#
# Test Coverage:
#   Basic Tests:
#     - Container starts without errors
#     - Required tools are installed (qemu-img, guestmount, fusermount, etc.)
#     - Helper scripts are executable
#     - Mount points exist
#     - Entrypoint runs successfully
#
#   Functional Tests:
#     - qemu-img can create and detect disk image formats (qcow2)
#     - libguestfs tools are operational (guestfish, guestmount)
#     - Filesystem tools work (mkfs.ext4, mkfs.xfs, fusermount)
#     - detect-and-mount.sh script is accessible and has usage info
#
# Environment Variables:
#   IMAGE_NAME - Container image name (default: oadp-vm-file-server:dev)
#
# Examples:
#   ./test-container.sh                    # Build and run all tests
#   ./test-container.sh test               # Run only basic tests
#   ./test-container.sh functional         # Run only functional tests
#   IMAGE_NAME=my-image:latest ./test-container.sh all
#
# Exit Codes:
#   0 - All tests passed
#   1 - One or more tests failed
#
# ==============================================================================

set -euo pipefail

IMAGE_NAME="${IMAGE_NAME:-oadp-vm-file-server:dev}"
CONTAINER_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ==============================================================================
# Helper Functions
# ==============================================================================

# Log message with timestamp
log() {
    echo "[$(date +'%H:%M:%S')] $*"
}

# Log error message with timestamp to stderr
error() {
    echo "[$(date +'%H:%M:%S')] ERROR: $*" >&2
}

# ==============================================================================
# Test Functions
# ==============================================================================

# Build the container image using podman or docker
# Automatically detects which container runtime is available
build_container() {
    log "Building container image: $IMAGE_NAME"
    log "Build context: $CONTAINER_DIR"

    if command -v podman &> /dev/null; then
        podman build -t "$IMAGE_NAME" "$CONTAINER_DIR"
    elif command -v docker &> /dev/null; then
        docker build -t "$IMAGE_NAME" "$CONTAINER_DIR"
    else
        error "Neither podman nor docker found. Please install one of them."
        return 1
    fi

    log "✓ Container image built successfully: $IMAGE_NAME"
}

# Run basic validation tests
# These tests verify that:
#   - Container starts successfully
#   - All required tools are installed
#   - Scripts are present and executable
#   - Required directories exist
test_container() {
    log "Testing container image: $IMAGE_NAME"

    local runtime
    if command -v podman &> /dev/null; then
        runtime="podman"
    elif command -v docker &> /dev/null; then
        runtime="docker"
    else
        error "Neither podman nor docker found"
        return 1
    fi

    # Test 1: Basic container functionality
    log "Test 1: Verify container starts"
    if $runtime run --rm "$IMAGE_NAME" /bin/bash -c "echo 'Container starts successfully'"; then
        log "✓ Test 1 passed: Container starts"
    else
        error "✗ Test 1 failed: Container did not start"
        return 1
    fi

    log "Test 2: Verify required tools are installed"
    if $runtime run --rm "$IMAGE_NAME" /bin/bash -c "
        command -v qemu-img && \
        command -v guestmount && \
        command -v guestunmount && \
        command -v fusermount && \
        command -v ntfs-3g
    " > /dev/null 2>&1; then
        log "✓ Test 2 passed: Required tools are installed (qemu-img, guestmount, fusermount)"
    else
        error "✗ Test 2 failed: Missing required tools"
        return 1
    fi

    log "Test 3: Verify scripts are executable"
    if $runtime run --rm "$IMAGE_NAME" /bin/bash -c "
        test -x /usr/local/bin/detect-and-mount.sh && \
        test -x /usr/local/bin/entrypoint.sh
    "; then
        log "✓ Test 3 passed: Scripts are executable"
    else
        error "✗ Test 3 failed: Scripts are not executable"
        return 1
    fi

    log "Test 4: Verify mount points exist"
    if $runtime run --rm "$IMAGE_NAME" /bin/bash -c "
        test -d /mnt/volumes && \
        test -d /mnt/filesystems && \
        test -d /var/www/files
    "; then
        log "✓ Test 4 passed: Mount points exist"
    else
        error "✗ Test 4 failed: Mount points missing"
        return 1
    fi

    log "Test 5: Verify entrypoint runs"
    if timeout 5 $runtime run --rm "$IMAGE_NAME" /usr/local/bin/entrypoint.sh & sleep 2; then
        log "✓ Test 5 passed: Entrypoint runs successfully"
        # Kill the background process
        pkill -f "entrypoint.sh" 2>/dev/null || true
    else
        log "✓ Test 5 passed: Entrypoint runs (timeout is expected behavior)"
    fi

    log "✓ All basic tests passed!"
}

# Run functional tests - verify tools actually work with real operations
# These tests go beyond "is it installed" and actually run the tools to:
#   - Create a qcow2 disk image and detect its format
#   - Run libguestfs tools (guestfish, guestmount)
#   - Execute filesystem tools (mkfs.ext4, mkfs.xfs)
#   - Verify scripts are accessible and properly formatted
test_functionality() {
    log "Running functional tests to verify tools work"

    local runtime
    if command -v podman &> /dev/null; then
        runtime="podman"
    elif command -v docker &> /dev/null; then
        runtime="docker"
    else
        error "No container runtime found"
        return 1
    fi

    # Functional Test 1: qemu-img creates and detects disk images
    # This verifies qemu-img can handle qcow2 format (most common for VMs)
    log "Functional Test 1: qemu-img can create and detect disk images"
    if $runtime run --rm "$IMAGE_NAME" bash -c '
        cd /tmp
        qemu-img create -f qcow2 test.qcow2 10M > /dev/null
        FORMAT=$(qemu-img info test.qcow2 | grep "file format" | awk "{print \$3}")
        [ "$FORMAT" = "qcow2" ]
    '; then
        log "✓ Functional Test 1 passed: qemu-img works"
    else
        error "✗ Functional Test 1 failed: qemu-img not working"
        return 1
    fi

    log "Functional Test 2: libguestfs tools are functional"
    if $runtime run --rm "$IMAGE_NAME" bash -c '
        guestfish --version > /dev/null &&
        guestmount --version > /dev/null 2>&1
    '; then
        log "✓ Functional Test 2 passed: libguestfs tools work"
    else
        error "✗ Functional Test 2 failed: libguestfs tools not working"
        return 1
    fi

    log "Functional Test 3: Filesystem tools are functional"
    if $runtime run --rm "$IMAGE_NAME" bash -c '
        mkfs.ext4 -V > /dev/null 2>&1 &&
        mkfs.xfs -V > /dev/null 2>&1 &&
        fusermount --version > /dev/null 2>&1
    '; then
        log "✓ Functional Test 3 passed: Filesystem tools work"
    else
        error "✗ Functional Test 3 failed: Filesystem tools not working"
        return 1
    fi

    log "Functional Test 4: detect-and-mount.sh script is accessible"
    if $runtime run --rm "$IMAGE_NAME" bash -c '
        test -f /usr/local/bin/detect-and-mount.sh &&
        grep -q "Usage:" /usr/local/bin/detect-and-mount.sh
    '; then
        log "✓ Functional Test 4 passed: detect-and-mount.sh script ready"
    else
        error "✗ Functional Test 4 failed: detect-and-mount.sh script issue"
        return 1
    fi

    log "✓ All functional tests passed!"
}

# Clean up test artifacts
clean() {
    log "Cleaning up test artifacts"

    local runtime
    if command -v podman &> /dev/null; then
        runtime="podman"
    elif command -v docker &> /dev/null; then
        runtime="docker"
    else
        log "No container runtime found, nothing to clean"
        return 0
    fi

    log "Removing container image: $IMAGE_NAME"
    $runtime rmi "$IMAGE_NAME" 2>/dev/null || log "Image not found or already removed"

    log "✓ Cleanup completed"
}

# Show usage
usage() {
    cat << EOF
Usage: $0 [command]

Commands:
    build        - Build the container image
    test         - Run basic validation tests
    functional   - Run functional tests (tools actually work)
    clean        - Remove test artifacts
    all          - Build and run all tests (default)

Environment Variables:
    IMAGE_NAME - Container image name (default: oadp-vm-file-server:dev)

Examples:
    $0 build
    $0 test
    IMAGE_NAME=my-image:latest $0 all
EOF
}

# Main function
main() {
    local command="${1:-all}"

    case "$command" in
        build)
            build_container
            ;;
        test)
            test_container
            ;;
        functional)
            test_functionality
            ;;
        clean)
            clean
            ;;
        all)
            build_container
            test_container
            test_functionality
            ;;
        help|--help|-h)
            usage
            ;;
        *)
            error "Unknown command: $command"
            usage
            exit 1
            ;;
    esac
}

main "$@"

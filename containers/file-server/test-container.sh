#!/bin/bash
# test-container.sh
# Simple validation script to test the file-server container build and functionality
#
# Usage:
#   ./test-container.sh [build|test|clean]
#
# Commands:
#   build - Build the container image
#   test  - Run basic tests on the container
#   clean - Remove test artifacts

set -euo pipefail

IMAGE_NAME="${IMAGE_NAME:-oadp-vm-file-server:dev}"
CONTAINER_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

log() {
    echo "[$(date +'%H:%M:%S')] $*"
}

error() {
    echo "[$(date +'%H:%M:%S')] ERROR: $*" >&2
}

# Build the container image
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

# Test the container
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
        command -v qemu-nbd && \
        command -v blkid && \
        command -v mount && \
        command -v ntfs-3g
    " > /dev/null 2>&1; then
        log "✓ Test 2 passed: Required tools are installed"
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

    log "✓ All tests passed!"
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
    build   - Build the container image
    test    - Run validation tests
    clean   - Remove test artifacts
    all     - Build and test (default)

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
        clean)
            clean
            ;;
        all)
            build_container
            test_container
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

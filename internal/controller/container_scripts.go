/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package controller contains initialization and lifecycle scripts for VMFR containers
package controller

// fileBrowserInitScript is the initialization script for the FileBrowser sidecar container.
// This script:
// - Reads credentials from the mounted secret (/etc/filebrowser-credentials/)
// - Initializes the FileBrowser database
// - Creates a read-only user with proper permissions
// - Configures branding and settings
// - Starts FileBrowser with TLS support
//
// The script is based on the official example from:
// containers/side-containers/filebrowser/sample-deployment/4_serving_pod.yaml
const fileBrowserInitScript = `set -e

echo "==================================================================="
echo "OADP VM File Restore File Browser Container Starting"
echo "==================================================================="

# Configuration from environment
FB_DATABASE=${FB_DATABASE:-/database/filebrowser.db}
FB_ROOT=${FB_ROOT:-/restores}
FB_PORT=${FB_PORT:-8080}
FB_ADDRESS=${FB_ADDRESS:-0.0.0.0}

# Read credentials from secret
if [ -f /etc/filebrowser-credentials/username ]; then
    FB_USERNAME=$(cat /etc/filebrowser-credentials/username)
else
    FB_USERNAME="oadp"
fi

if [ -f /etc/filebrowser-credentials/password ]; then
    FB_PASSWORD=$(cat /etc/filebrowser-credentials/password)
else
    echo "ERROR: Password not found in secret at /etc/filebrowser-credentials/password"
    exit 1
fi

# Branding configuration
FB_BRANDING_NAME=${FB_BRANDING_NAME:-OADP VM File Restore Browser}

echo "Initializing File Browser..."

# Initialize database if it doesn't exist
if [ ! -f "${FB_DATABASE}" ]; then
    echo "Creating new database at ${FB_DATABASE}..."
    /usr/local/bin/filebrowser config init --database "${FB_DATABASE}"
else
    echo "Database exists at ${FB_DATABASE}"
fi

# Configure branding (idempotent - can run multiple times)
echo "Configuring branding..."

# Only set defaultLoginUser if username is missing or equals "oadp"
if [ -z "${FB_USERNAME}" ] || [ "${FB_USERNAME}" = "oadp" ]; then
    echo "Setting default login user to: oadp"
    /usr/local/bin/filebrowser config set \
      --database "${FB_DATABASE}" \
      --branding.name "${FB_BRANDING_NAME}" \
      --branding.disableUsedPercentage \
      --branding.disableExternal \
      --branding.disableUserProfile \
      --branding.defaultLoginUser "oadp"
else
    echo "Using custom username, not setting default login user"
    /usr/local/bin/filebrowser config set \
      --database "${FB_DATABASE}" \
      --branding.name "${FB_BRANDING_NAME}" \
      --branding.disableUsedPercentage \
      --branding.disableExternal \
      --branding.disableUserProfile
fi

# Check if user exists, if not create it
if /usr/local/bin/filebrowser users ls --database "${FB_DATABASE}" | grep -q "^| ${FB_USERNAME} "; then
    echo "User ${FB_USERNAME} already exists"
else
    echo "Creating read-only user: ${FB_USERNAME}..."
    /usr/local/bin/filebrowser users add "${FB_USERNAME}" "${FB_PASSWORD}" \
      --database "${FB_DATABASE}" \
      --perm.create=false \
      --perm.delete=false \
      --perm.modify=false \
      --perm.rename=false \
      --perm.share=false \
      --perm.execute=false \
      --lockPassword \
      --locale en \
      --hideDotfiles=false \
      --singleClick=false \
      --dateFormat=false
    echo "User ${FB_USERNAME} created successfully"
fi

echo "==================================================================="
echo "File Browser Configuration Complete"
echo "==================================================================="
echo "Service URL: http://${FB_ADDRESS}:${FB_PORT}"
echo "Root Directory: ${FB_ROOT}"
echo ""
echo "User Credentials:"
echo "  Username: ${FB_USERNAME}"
echo "  Password: ********"
echo ""
echo "Permissions (Read-Only):"
echo "  ✓ View files"
echo "  ✓ Download files"
echo "  ✗ Create/Upload"
echo "  ✗ Delete/Modify/Rename/Share/Execute"
echo ""
echo "Branding: ${FB_BRANDING_NAME}"
echo "==================================================================="

# Start File Browser with TLS
echo "Starting File Browser with HTTPS..."
if [ -f /etc/filebrowser-tls/tls.crt ] && [ -f /etc/filebrowser-tls/tls.key ]; then
  echo "TLS certificates found, starting with HTTPS on port ${FB_PORT}"
  exec /usr/local/bin/filebrowser \
    --address "${FB_ADDRESS}" \
    --port "${FB_PORT}" \
    --root "${FB_ROOT}" \
    --database "${FB_DATABASE}" \
    --cert /etc/filebrowser-tls/tls.crt \
    --key /etc/filebrowser-tls/tls.key
else
  echo "WARNING: TLS certificates not found, starting with HTTP"
  exec /usr/local/bin/filebrowser \
    --address "${FB_ADDRESS}" \
    --port "${FB_PORT}" \
    --root "${FB_ROOT}" \
    --database "${FB_DATABASE}"
fi
`

// vmFileServerPreStopScript is the cleanup script for the VM file server main container.
// This script runs as a PreStop lifecycle hook, which executes BEFORE the container receives SIGTERM.
// It ensures all FUSE filesystems (created by guestmount) are properly unmounted before pod termination.
//
// The script:
// - Discovers all FUSE mount points under /restores/*/*/*
// - Unmounts each filesystem using guestunmount (preferred) or fusermount -u (fallback)
// - Uses fail-safe operations (no 'set -e', all commands have '|| true')
// - Always exits with success to prevent blocking pod termination
//
// This works in conjunction with the trap handler in detect-and-mount.sh (belt-and-suspenders approach).
// See issue #44: https://github.com/migtools/oadp-vm-file-restore/issues/44
const vmFileServerPreStopScript = `# Do NOT use 'set -e' - we want to continue even if some commands fail
echo "[PreStop] Starting cleanup of FUSE mounts..."

# Function to safely unmount a FUSE mount point
unmount_fuse() {
    local mount_point="$1"
    echo "[PreStop] Unmounting: $mount_point"

    # Try guestunmount first (preferred for libguestfs FUSE mounts)
    if command -v guestunmount >/dev/null 2>&1; then
        if guestunmount "$mount_point" 2>/dev/null; then
            echo "[PreStop] ✓ Successfully unmounted with guestunmount: $mount_point"
            return 0
        fi
    fi

    # Fallback to fusermount -u for generic FUSE unmounts
    if fusermount -u "$mount_point" 2>/dev/null; then
        echo "[PreStop] ✓ Successfully unmounted with fusermount: $mount_point"
        return 0
    fi

    # If both fail, log but don't fail the hook (mount may already be gone)
    echo "[PreStop] ⚠ Could not unmount: $mount_point (may not be mounted)"
    return 0
}

# Find all FUSE mount points under /restores/
# Structure: /restores/<date>/<backup>/<pvc>/
# We look for depth 3 directories which are the actual mount points

mounted_count=0
unmounted_count=0

# Find mount points at depth 3: /restores/YYYY-MM-DD/backup-name/pvc-name/
if [ -d /restores ]; then
    # Use shopt to handle failed glob patterns gracefully
    shopt -s nullglob
    for mount_point in /restores/*/*/*; do
        # Check if it's a directory
        if [ -d "$mount_point" ]; then
            # Check if it's actually a mount point using mountpoint command
            if mountpoint -q "$mount_point" 2>/dev/null; then
                mounted_count=$((mounted_count + 1))
                unmount_fuse "$mount_point" || true
                unmounted_count=$((unmounted_count + 1))
            fi
        fi
    done
    shopt -u nullglob
fi

echo "[PreStop] Cleanup complete: unmounted $unmounted_count of $mounted_count FUSE mounts"

# Give the system a moment to fully process the unmounts
sleep 1 || true

exit 0
`

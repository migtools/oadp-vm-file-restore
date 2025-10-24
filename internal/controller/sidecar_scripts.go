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

// Package controller contains initialization scripts for sidecar containers
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

#!/bin/bash
set -e

echo "==================================================================="
echo "OADP VM File Restore File Browser Container Starting"
echo "==================================================================="

# Configuration from environment
FB_DATABASE=${FB_DATABASE:-/database/filebrowser.db}
FB_ROOT=${FB_ROOT:-/srv}
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
FB_BRANDING_FILES=${FB_BRANDING_FILES:-/branding}

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
      --branding.defaultLoginUser "oadp" \
      --branding.files "${FB_BRANDING_FILES}"
else
    echo "Using custom username, not setting default login user"
    /usr/local/bin/filebrowser config set \
      --database "${FB_DATABASE}" \
      --branding.name "${FB_BRANDING_NAME}" \
      --branding.disableUsedPercentage \
      --branding.disableExternal \
      --branding.disableUserProfile \
      --branding.files "${FB_BRANDING_FILES}"
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
echo "Database: ${FB_DATABASE}"
echo ""
echo "User Credentials:"
echo "  Username: ${FB_USERNAME}"
echo "  Password: ${FB_PASSWORD}"
echo ""
echo "Permissions (Read-Only):"
echo "  ✓ View files"
echo "  ✓ Download files"
echo "  ✗ Create/Upload"
echo "  ✗ Delete"
echo "  ✗ Modify"
echo "  ✗ Rename"
echo "  ✗ Share"
echo "  ✗ Execute"
echo ""
echo "Branding: ${FB_BRANDING_NAME}"
echo "==================================================================="

# Start File Browser
echo "Starting File Browser..."
exec /usr/local/bin/filebrowser \
  --address "${FB_ADDRESS}" \
  --port "${FB_PORT}" \
  --root "${FB_ROOT}" \
  --database "${FB_DATABASE}"

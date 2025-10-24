#!/bin/bash
set -e

echo "==================================================================="
echo "OADP VM File Restore SSHD Container Starting"
echo "==================================================================="

# Read username from secret (mounted at /ssh-config)
if [ ! -f /ssh-config/username ]; then
    echo "ERROR - SSH username not found in secret at /ssh-config/username"
    exit 1
fi

if [ ! -f /ssh-config/authorized_keys ]; then
    echo "ERROR - SSH authorized_keys not found in secret at /ssh-config/authorized_keys"
    exit 1
fi

SSH_USER=$(cat /ssh-config/username)
OADP_UID=1001
OADP_GID=1001

echo "Configuring SSH for user - ${SSH_USER} (UID ${OADP_UID}, GID ${OADP_GID})"

# Initialize /etc with base files from template
# This is needed because /etc is mounted as emptyDir (writable)
if [ ! -f /etc/passwd ]; then
    echo "Initializing /etc from template..."

    # Check template directory
    echo "Checking /etc-template..."
    ls -la /etc-template/ || { echo "ERROR: Cannot list /etc-template"; exit 1; }

    # Check current /etc
    echo "Current /etc contents before copy:"
    ls -la /etc/ || true

    # Copy all files from template
    echo "Copying files from /etc-template to /etc..."
    cp -av /etc-template/* /etc/ || { echo "ERROR: Copy failed"; exit 1; }

    # Fix permissions so groupadd/useradd can write to these files
    echo "Setting proper permissions on /etc files..."
    chmod 644 /etc/passwd /etc/group
    chmod 600 /etc/shadow /etc/gshadow

    # Check /etc after copy
    echo "Current /etc contents after copy and permission fix:"
    ls -la /etc/

    # Verify required files exist and are writable
    for file in passwd group shadow gshadow nsswitch.conf; do
        if [ ! -f /etc/$file ]; then
            echo "ERROR - /etc/$file is missing after template copy"
            exit 1
        fi
        echo "Verified /etc/$file exists"
    done

    # Verify PAM directory exists
    if [ ! -d /etc/pam.d ]; then
        echo "ERROR - /etc/pam.d directory is missing after template copy"
        exit 1
    fi
    echo "Verified /etc/pam.d exists"

    echo "/etc initialized successfully"
fi

# Create the SSH user with the name from the secret
# Check if user already exists
if id "${SSH_USER}" >/dev/null 2>&1; then
    echo "User ${SSH_USER} already exists"
else
    echo "Creating user ${SSH_USER}..."
    # Create group first
    if ! getent group ${SSH_USER} >/dev/null 2>&1; then
        groupadd -g ${OADP_GID} ${SSH_USER}
    fi
    # Create user with flags to avoid lastlog and mailbox warnings
    # -l: do not add user to lastlog database (read-only filesystem)
    # -M: do not create home directory
    # Redirect stderr to suppress mailbox creation warning
    useradd -l -u ${OADP_UID} -g ${OADP_GID} -d /oadp -s /bin/bash -M ${SSH_USER} 2>&1 | grep -v "Creating mailbox file" || true
    echo "Created user ${SSH_USER} (UID ${OADP_UID}, GID ${OADP_GID})"

    # Unlock the account for SSH key authentication
    # PAM requires the account to have a password (even if unused) to not be locked.
    # Set a random password that will never be used (PasswordAuthentication is disabled).
    # Generate a random 32-character password
    RANDOM_PASSWORD=$(head /dev/urandom | tr -dc A-Za-z0-9 | head -c 32)
    echo "${SSH_USER}:${RANDOM_PASSWORD}" | chpasswd
    echo "Account unlocked with random password (password auth is disabled)"
    unset RANDOM_PASSWORD  # Clear from memory
fi

# Setup SSH directory for the user inside chroot
# Note: authorized_keys is mounted directly from secret at /oadp/.ssh/authorized_keys
# The directory is created automatically by the mount, we just verify it exists
echo "Configuring SSH directory in /oadp/.ssh..."

# Verify the directory exists (created by Kubernetes mount)
if [ -d /oadp/.ssh ]; then
    echo "SSH directory exists - authorized_keys mounted from secret"
    ls -la /oadp/.ssh/
else
    echo "ERROR - /oadp/.ssh directory not found (mount failed?)"
    exit 1
fi

# Note: We cannot change ownership of the mount point, but StrictModes is disabled
# in sshd_config to allow this setup to work securely with other restrictions

# Create device nodes in chroot /dev (needed for scp and other tools)
# /oadp/dev is mounted as emptyDir, so we can create device nodes at runtime
if [ ! -e /oadp/dev/null ]; then
    echo "Creating device nodes in /oadp/dev..."
    mknod -m 666 /oadp/dev/null c 1 3
    mknod -m 666 /oadp/dev/zero c 1 5
    echo "Device nodes created"
fi

# Create minimal /etc files inside chroot for user lookups
# scp and other tools need to resolve UIDs to usernames
mkdir -p /oadp/etc
echo "Creating minimal passwd/group files in chroot /oadp/etc..."

# Create minimal passwd file with root and oadp user
# Note: Shell is set to /bin/bash but ForceCommand prevents it from being used
cat > /oadp/etc/passwd << EOF
root:x:0:0:root:/root:/bin/bash
${SSH_USER}:x:${OADP_UID}:${OADP_GID}:OADP User:/oadp:/bin/bash
EOF

# Create minimal group file
cat > /oadp/etc/group << EOF
root:x:0:
${SSH_USER}:x:${OADP_GID}:
EOF

# Create minimal nsswitch.conf for chroot
cat > /oadp/etc/nsswitch.conf << 'EOF'
passwd:     files
group:      files
EOF

chmod 644 /oadp/etc/passwd /oadp/etc/group /oadp/etc/nsswitch.conf
chown root:root /oadp/etc/passwd /oadp/etc/group /oadp/etc/nsswitch.conf
echo "Chroot /etc files created"

# Create SSH config directory (needed because /etc is emptyDir)
mkdir -p /etc/ssh

# Create a simplified PAM configuration for SSHD that works in containers
# The default RHEL PAM config has SELinux and security modules that don't work in containers
cat > /etc/pam.d/sshd << 'EOFPAM'
#%PAM-1.0
# Simplified PAM configuration for containerized SSHD
# Removed SELinux, sepermit, and other modules that require full system access

# Authentication
auth       required     pam_env.so
auth       sufficient   pam_unix.so nullok
auth       required     pam_deny.so

# Account management
account    required     pam_unix.so

# Password management (not used with key-only auth, but needed by PAM)
password   required     pam_unix.so

# Session management
session    required     pam_unix.so
session    optional     pam_motd.so
EOFPAM

echo "Created simplified PAM configuration for SSHD (container-compatible)"

# Generate host keys if they don't exist
# Note: Keys are generated at runtime for security - each container instance
# has unique keys. Shipping keys in the image would mean all instances share
# the same keys, which is a security vulnerability.
if [ ! -f /etc/ssh/ssh_host_rsa_key ]; then
    echo "Generating SSH host keys..."
    ssh-keygen -A
fi

# Create run directory for sshd
mkdir -p /run/sshd

# Create sshd_config with chroot and forced command
cat > /etc/ssh/sshd_config << EOF
# Secure Read-Only File Transfer Configuration for OADP with Chroot
Port 2222

# Security settings
PermitRootLogin no
PubkeyAuthentication yes
PasswordAuthentication no
ChallengeResponseAuthentication no
KbdInteractiveAuthentication no
# Enable PAM - required for RHEL/UBI
# Provides session management, audit logging, and account policy enforcement
UsePAM yes

# Disable strict mode checking - safe in this context because:
# - authorized_keys is mounted from Kubernetes secret (read-only)
# - Mount point ownership cannot be changed
# - Container has chroot jail, forced command, and read-only filesystem
# - All other security controls remain in place
StrictModes no

# Disable all forwarding and tunneling
X11Forwarding no
AllowTcpForwarding no
AllowStreamLocalForwarding no
PermitTunnel no
GatewayPorts no

# Disable unnecessary features
PrintMotd no
PrintLastLog no
PermitUserEnvironment no

# SFTP subsystem
Subsystem sftp internal-sftp

# Chroot and restrict to read-only operations via forced command
Match User ${SSH_USER}
    ChrootDirectory /oadp
    ForceCommand /bin/oadp-sshd
EOF

echo "==================================================================="
echo "SSHD Configuration Complete - READ-ONLY FILE TRANSFER WITH CHROOT"
echo "==================================================================="
echo "SSH User - ${SSH_USER} (UID ${OADP_UID})"
echo "SSH Port - 2222"
echo "Chroot Jail - /oadp (user sees it as /)"
echo ""
echo "Allowed Operations -"
echo "  ✓ rsync (read-only) - rsync -avz ${SSH_USER}@host:/restores/ /local/"
echo "  ✓ scp (download) - scp ${SSH_USER}@host:/restores/file /local/"
echo "  ✓ sftp - sftp ${SSH_USER}@host"
echo "  ✗ Interactive shell - DISABLED"
echo "  ✗ Write operations - DISABLED"
echo ""
echo "Security -"
echo "  - Chroot jail - User confined to /oadp directory"
echo "  - Read-only mount on /restores/"
echo "  - Can read ALL files regardless of ownership (DAC_READ_SEARCH)"
echo "  - Forced command wrapper blocks unauthorized operations"
echo "  - No shell access, no port forwarding, no tunneling"
echo ""
echo "Data Location (inside chroot) - /restores/"
echo "Authentication - Public key only"
echo "==================================================================="

# Verify /oadp/restores exists (should be mounted from PVC)
if [ -d /oadp/restores ]; then
    echo "Checking /oadp/restores ownership -"
    ls -ld /oadp/restores/ || true
else
    echo "WARNING - /oadp/restores does not exist (PVC not mounted?)"
fi

echo "Starting SSHD on port 2222..."

# Start sshd in foreground
exec /usr/sbin/sshd -D -e

# OADP VM File Restore SSHD Container

Secure SSH/SFTP/rsync container for read-only access to OADP VM backup files with chroot jail isolation.

## Features

**Supported Operations:**
- ✅ rsync (download/read-only), scp (download only), sftp (browsing and download)
- ❌ Interactive shell, write/upload operations, port forwarding, tunneling

**Security:**
- Chroot jail at `/oadp` with Go-based forced command validation
- Read-only root filesystem and data mounts
- Public key authentication only, all SSH features disabled except file transfer
- Minimal capabilities (7 granted: AUDIT_WRITE, CHOWN, DAC_READ_SEARCH, MKNOD, SETUID, SETGID, SYS_CHROOT)
- Multi-architecture support (amd64, arm64)

## Building

### Using Make (Recommended)

```bash
# Single-arch build and push (auto-detects podman/docker)
make build
make push

# Multi-arch build and push
make build-push-multiarch

# Architecture-specific builds
make build-amd64
make build-arm64

# Custom image settings
make build IMAGE_TAG=v1.0.0 IMAGE_ORG=myorg

# Show targets and configuration
make help
make info
```

### Manual Build

**Single-arch:**
```bash
# Podman or Docker
podman build -t quay.io/migtools/oadp-vmfr-sshd:latest -f Containerfile .
docker build -t quay.io/migtools/oadp-vmfr-sshd:latest -f Containerfile .
```

**Multi-arch:**
```bash
# Podman (native multi-arch support)
podman build --platform linux/amd64,linux/arm64 \
  --manifest quay.io/migtools/oadp-vmfr-sshd:latest -f Containerfile .
podman manifest push quay.io/migtools/oadp-vmfr-sshd:latest

# Docker (requires buildx)
docker buildx create --name multiarch-builder --use  # One-time setup
docker buildx build --platform linux/amd64,linux/arm64 \
  -t quay.io/migtools/oadp-vmfr-sshd:latest -f Containerfile --push .
```

**Supported Platforms:** linux/amd64, linux/arm64

### Pushing to Registry

```bash
# Using Make
make login-quay
make build-push

# Manual
podman login quay.io  # or docker login
podman push quay.io/migtools/oadp-vmfr-sshd:latest
```

## Deployment

### Configuration

The container reads configuration from mounted secrets at `/ssh-config/`:
- `username` - SSH username (any valid name, user created at runtime with UID/GID 1001)
- `authorized_keys` - SSH public keys for authentication

### Required Mounts

1. **SSH Config:** `/ssh-config/` (secret with username file)
2. **SSH Keys:** `/oadp/.ssh/authorized_keys` (mount authorized_keys from secret using subPath)
3. **Backup Data:** `/oadp/restores/` (PVC with backup data)
4. **Tmpfs (for read-only root FS):** `/etc`, `/run`, `/tmp`, `/oadp/dev` (all emptyDir)

### Kubernetes Pod Example

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: file-serving-pod
spec:
  securityContext:
    fsGroup: 1001
  containers:
  - name: sshd
    image: quay.io/migtools/oadp-vmfr-sshd:latest
    ports:
    - containerPort: 2222
      name: ssh
    volumeMounts:
    - name: restores-volume
      mountPath: /oadp/restores
      readOnly: true
    - name: ssh-secret
      mountPath: /ssh-config
      readOnly: true
    - name: ssh-secret
      mountPath: /oadp/.ssh/authorized_keys
      subPath: authorized_keys
      readOnly: true
    - name: ssh-etc
      mountPath: /etc
    - name: ssh-run
      mountPath: /run
    - name: ssh-tmp
      mountPath: /tmp
    - name: ssh-dev
      mountPath: /oadp/dev
    securityContext:
      runAsUser: 0
      readOnlyRootFilesystem: true
      allowPrivilegeEscalation: false
      capabilities:
        add: [AUDIT_WRITE, CHOWN, DAC_READ_SEARCH, MKNOD, SETUID, SETGID, SYS_CHROOT]
        drop: [ALL]
  volumes:
  - name: restores-volume
    persistentVolumeClaim:
      claimName: restores-pvc
  - name: ssh-secret
    secret:
      secretName: ssh-oadp
      defaultMode: 0400
  - name: ssh-etc
    emptyDir: {medium: Memory, sizeLimit: 10Mi}
  - name: ssh-run
    emptyDir: {medium: Memory, sizeLimit: 10Mi}
  - name: ssh-tmp
    emptyDir: {medium: Memory, sizeLimit: 50Mi}
  - name: ssh-dev
    emptyDir: {medium: Memory, sizeLimit: 1Mi}
```

## Testing

### Sample Deployment

A complete sample deployment with OpenShift manifests is available in the `sample-deployment/` directory.

#### Quick Start

```bash
# Step 1: Generate SSH key pair
ssh-keygen -t rsa -b 4096 -f oadp-key -C "oadp@openshift-adp" -N ""

# Step 2: Create namespace
oc apply -f sample-deployment/1_namespace.yaml

# Step 3: Create SSH secret with your key
oc create secret generic ssh-oadp -n vmfr-pod-test \
  --from-literal=username=oadp \
  --from-file=authorized_keys=oadp-key.pub

# Step 4: Create PVC for restore data
oc apply -f sample-deployment/3_pvc.yaml

# Step 5: Deploy the file serving pod
oc apply -f sample-deployment/4_serving_pod.yaml

# Step 6: Port forward to access SSH - after the pod to starts
oc port-forward -n vmfr-pod-test pod/file-serving-pod 2222:2222

# Step 7: Download the files using instructions from below paragraph
```

### Using the Deployment

After deploying, port forward and access files:

```bash
# Download with scp
scp -P 2222 -i oadp-key oadp@localhost:/restores/sampledata/vm1/disk1/data.txt /tmp/

# Download with rsync
rsync -avz -e "ssh -p 2222 -i oadp-key" oadp@localhost:/restores/sampledata/ /tmp/vm-restored-backup/

# Browse with sftp
sftp -P 2222 -i oadp-key oadp@localhost
```

## Development

### File Structure

```
side-containers/sshd/
├── Containerfile    # Multi-stage build (Go compile + runtime)
├── entrypoint.sh    # Container entrypoint
├── oadp-sshd.go     # SSH command validator (Go)
├── Makefile         # Build automation
└── README.md        # This file
```

### Chroot Environment

The chroot jail at `/oadp`:
```
/oadp/
├── .ssh/authorized_keys    # Mounted from secret
├── bin/{bash,rsync,scp,oadp-sshd}
├── etc/{passwd,group,nsswitch.conf}  # Created at runtime
├── lib/, lib64/, usr/lib/, usr/lib64/  # Shared libraries
├── usr/libexec/openssh/sftp-server
├── dev/{null,zero}         # Created at runtime
└── restores/               # Mounted PVC
```

**Note:** The container uses multi-stage build (Go compile → minimal runtime). The `oadp-sshd` binary (chmod 711) validates all SSH commands but can be downloaded due to `DAC_READ_SEARCH` capability. Security relies on defense-in-depth (read-only mounts, chroot, forced command), not obscurity.

### Modifying Code

After modifying `entrypoint.sh` or `oadp-sshd.go`:
```bash
make build
```

## Security Considerations

### Defense-in-Depth Model

**Why container runs as root (UID 0):**
- OpenSSH requires root for authentication and privilege separation
- `chroot()` system call requires root even with SYS_CHROOT capability
- Dynamic user creation (`useradd`/`groupadd`) requires root
- File access with `DAC_READ_SEARCH` requires root context

**How security is maintained despite root:**
1. **Read-only root filesystem** - Cannot modify container or install backdoors
2. **Read-only data mount** - Cannot modify backup data (kernel-enforced)
3. **Minimal capabilities** - Only 7 granted (vs ~40 available), all others dropped
4. **No privilege escalation** - Cannot gain additional capabilities
5. **Chroot isolation** - Users confined to `/oadp` directory
6. **Forced command** - Go binary validates all SSH commands, blocks shell access
7. **Ephemeral state** - All writable dirs are tmpfs, lost on restart

### Required Capabilities

- **AUDIT_WRITE** - PAM audit logging (RHEL requirement)
- **CHOWN** - Set ownership during initialization
- **DAC_READ_SEARCH** - Read backup files regardless of ownership/permissions
- **MKNOD** - Create device nodes in chroot at runtime
- **SETUID/SETGID** - SSH privilege dropping
- **SYS_CHROOT** - Enable chroot jail

**Note:** `DAC_READ_SEARCH` allows users to download all files in chroot (including `oadp-sshd` binary). Security does not rely on hiding validation logic.

### Attack Surface

With valid SSH credentials, users can:
- ✅ Download backup files (intended functionality)

Users cannot:
- ❌ Get interactive shell (forced command blocks)
- ❌ Execute arbitrary commands (validated by oadp-sshd)
- ❌ Upload or modify files (read-only mounts)
- ❌ Persist backdoors (read-only filesystem + ephemeral tmpfs)
- ❌ Escape chroot (proper configuration required)

## Customization

### Using Different Username

The SSH username can be customized. Edit the secret:

```yaml
stringData:
  username: myuser  # Can be: oadp, restore, backup, or any valid username
  authorized_keys: |
    ssh-rsa AAAA... myuser@example.com
```

The user will be dynamically created at container startup with:
- Username: From the secret
- UID: 1001 (fixed)
- GID: 1001 (fixed)
- Home: `/oadp` (chroot jail)

This allows you to use different usernames while maintaining consistent file ownership.

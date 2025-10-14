# Building the OADP VM File Server Container

This document explains how to build the file-server container image.

## Two Dockerfiles: Upstream vs Downstream

This project provides **two Dockerfiles** for different use cases:

| Dockerfile | Base Image | Build Requirements | Who Uses It |
|------------|------------|-------------------|-------------|
| **Dockerfile** | Fedora 42 | None | Community, contributors, upstream development |
| **Dockerfile.rhel** | RHEL 9 UBI | Red Hat subscription | Red Hat product builds (downstream) |

Both produce functionally identical images - the choice is about build accessibility vs production alignment.

---

## Building Upstream (Fedora 42) - Recommended for Development

### Prerequisites

**None!** Anyone can build this.

### Build Command

```bash
cd containers/file-server/

# Simple build (uses Dockerfile by default)
podman build -t oadp-vm-file-server:latest .

# Or explicitly specify Dockerfile
podman build -f Dockerfile -t oadp-vm-file-server:latest .
```

### Why Fedora 42?

- ✅ Latest stable Fedora release
- ✅ All packages available without subscription
- ✅ Anyone can build (community friendly)
- ✅ Multi-arch support (x86_64, aarch64)
- ✅ Standard upstream/downstream pattern (like Velero, Prometheus Operator)

---

## Building Downstream (RHEL 9) - For Red Hat Product

### Prerequisites

**Red Hat Subscription Required**

Building with RHEL 9 base requires Red Hat subscription credentials to access full RHEL repositories for packages like `libguestfs-tools`.

The built image does NOT require a subscription to run - only the build process needs credentials.

#### Get a Subscription

You need access to RHEL 9 repositories. This is included with:
- Red Hat Developer Subscription (free for developers)
- Red Hat Enterprise Linux subscription
- Red Hat Employee subscription

Get a free developer subscription at: https://developers.redhat.com/

### Build Methods

#### Method 1: Using Build Arguments (Simplest)

```bash
cd containers/file-server/

podman build -f Dockerfile.rhel \
  --build-arg RHSM_USER="your-redhat-username" \
  --build-arg RHSM_PASS="your-redhat-password" \
  -t oadp-vm-file-server:rhel \
  .
```

**Security Note:** This method passes credentials as build arguments. They won't appear in the final image but may be visible in build logs.

#### Method 2: Using Build Secrets (More Secure)

1. Create secret files:
```bash
echo "your-redhat-username" > /tmp/rhsm-username
echo "your-redhat-password" > /tmp/rhsm-password
chmod 600 /tmp/rhsm-*
```

2. Build with secrets:
```bash
cd containers/file-server/

podman build -f Dockerfile.rhel \
  --secret id=rhsm-username,src=/tmp/rhsm-username \
  --secret id=rhsm-password,src=/tmp/rhsm-password \
  -t oadp-vm-file-server:rhel \
  .
```

3. Clean up secrets:
```bash
rm -f /tmp/rhsm-username /tmp/rhsm-password
```

#### Method 3: Using Environment Variables

```bash
export RHSM_USER="your-redhat-username"
export RHSM_PASS="your-redhat-password"

cd containers/file-server/

podman build -f Dockerfile.rhel \
  --build-arg RHSM_USER="${RHSM_USER}" \
  --build-arg RHSM_PASS="${RHSM_PASS}" \
  -t oadp-vm-file-server:rhel \
  .

unset RHSM_USER RHSM_PASS
```

### RHEL Build Process Explained

The Dockerfile.rhel performs these steps:

1. **Starts from RHEL 9 UBI** (`registry.access.redhat.com/ubi9/ubi:latest`)
2. **Registers with Red Hat subscription** (temporarily)
3. **Installs packages from RHEL repos:**
   - libguestfs-tools (VM disk mounting)
   - libguestfs-xfs (XFS filesystem support)
   - All filesystem and utility tools
4. **Unregisters subscription** (cleanup)
5. **Removes subscription cache** (security)
6. **Adds helper scripts** (detect-and-mount.sh, entrypoint.sh)
7. **Creates qemu user** (UID/GID 107)

**Result:** A container image with all required tools, NO subscription needed to run.

---

## Testing the Build

After building (either Dockerfile), verify the image:

```bash
# Check image was created
podman images | grep oadp-vm-file-server

# Verify libguestfs is installed
podman run --rm oadp-vm-file-server:latest guestmount --version

# Check qemu-img is available
podman run --rm oadp-vm-file-server:latest qemu-img --version

# Verify running as qemu user (UID 107)
podman run --rm --entrypoint /bin/bash oadp-vm-file-server:latest -c "id"
# Expected output: uid=107(qemu) gid=107(qemu)
```

---

## CI/CD Integration

### GitHub Actions - Fedora (Upstream)

```yaml
name: Build OADP File Server (Upstream)

on:
  push:
    branches: [ main ]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Build container (Fedora)
        run: |
          cd containers/file-server
          podman build -t oadp-vm-file-server:latest .

      - name: Push to quay.io
        run: |
          podman login -u="${{ secrets.QUAY_USER }}" \
                      -p="${{ secrets.QUAY_TOKEN }}" \
                      quay.io
          podman tag oadp-vm-file-server:latest \
                     quay.io/oadp/oadp-vm-file-server:latest
          podman push quay.io/oadp/oadp-vm-file-server:latest
```

**Required GitHub Secrets:**
- `QUAY_USER` - Quay.io username
- `QUAY_TOKEN` - Quay.io token

### GitHub Actions - RHEL (Downstream)

```yaml
name: Build OADP File Server (Downstream)

on:
  push:
    branches: [ release-* ]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Build container (RHEL)
        run: |
          cd containers/file-server
          podman build -f Dockerfile.rhel \
            --build-arg RHSM_USER="${{ secrets.RHSM_USERNAME }}" \
            --build-arg RHSM_PASS="${{ secrets.RHSM_PASSWORD }}" \
            -t oadp-vm-file-server:rhel \
            .

      - name: Push to registry.redhat.io
        run: |
          podman login -u="${{ secrets.RH_REGISTRY_USER }}" \
                      -p="${{ secrets.RH_REGISTRY_TOKEN }}" \
                      registry.redhat.io
          podman tag oadp-vm-file-server:rhel \
                     registry.redhat.io/oadp/oadp-vm-file-server:latest
          podman push registry.redhat.io/oadp/oadp-vm-file-server:latest
```

**Required GitHub Secrets:**
- `RHSM_USERNAME` - Red Hat subscription username
- `RHSM_PASSWORD` - Red Hat subscription password
- `RH_REGISTRY_USER` - Red Hat registry username
- `RH_REGISTRY_TOKEN` - Red Hat registry token

---

## Troubleshooting

### Fedora Build Issues

#### Error: "No package libguestfs-tools available"

**Cause:** Fedora version too old or mirrors not synced.

**Fix:**
1. Verify Fedora 42 is available: https://fedoraproject.org/
2. Try with explicit mirror: `dnf install --setopt=fastestmirror=false ...`

### RHEL Build Issues

#### Error: "Unable to read consumer identity"

This is **normal** and can be ignored. UBI images show this warning but subscription-manager still works.

#### Error: "Unable to find a match: libguestfs-tools"

**Cause:** Subscription registration failed or RHEL repos not enabled.

**Fix:**
1. Verify your Red Hat credentials are correct
2. Ensure your subscription includes RHEL 9
3. Check you're not hitting subscription limits

#### Error: "subscription-manager: command not found"

**Cause:** Using wrong base image or wrong Dockerfile.

**Fix:** Ensure building with `-f Dockerfile.rhel` and base is `registry.access.redhat.com/ubi9/ubi:latest`

### Build is Slow

**Expected:** The first build can take 10-15 minutes due to:
- Package repository metadata download
- Installing ~1.1 GB of packages
- (RHEL only) Subscription registration

**Subsequent builds:** Much faster due to layer caching.

---

## Security Considerations

### Credentials in Build Logs

Build logs may contain subscription registration messages (RHEL only). These logs should be:
- ✅ Kept private in CI/CD
- ✅ Not committed to git
- ✅ Rotated if exposed

### Subscription in Final Image

The Dockerfile.rhel explicitly:
- ✅ Unregisters subscription after package installation
- ✅ Removes `/var/lib/rhsm/*` cache
- ✅ Ensures final image has NO subscription data

You can verify:
```bash
podman run --rm --entrypoint /bin/bash oadp-vm-file-server:rhel \
  -c "ls -la /var/lib/rhsm/ 2>&1"
# Expected: directory not found or empty
```

### Image Scanning

Always scan built images for vulnerabilities:
```bash
# Using podman
podman scan oadp-vm-file-server:latest

# Using trivy
trivy image oadp-vm-file-server:latest
```

---

## Multi-Architecture Builds

Both Dockerfiles support multi-arch builds:

### Fedora (Easier - No Subscription)

```bash
podman manifest create oadp-vm-file-server:latest

# Build for x86_64
podman build --platform linux/amd64 \
  --manifest oadp-vm-file-server:latest .

# Build for aarch64
podman build --platform linux/arm64 \
  --manifest oadp-vm-file-server:latest .

# Push manifest
podman manifest push oadp-vm-file-server:latest \
  quay.io/oadp/oadp-vm-file-server:latest
```

### RHEL (Requires Subscription)

```bash
podman manifest create oadp-vm-file-server:rhel

# Build for x86_64
podman build -f Dockerfile.rhel \
  --platform linux/amd64 \
  --build-arg RHSM_USER="..." \
  --build-arg RHSM_PASS="..." \
  --manifest oadp-vm-file-server:rhel .

# Build for aarch64
podman build -f Dockerfile.rhel \
  --platform linux/arm64 \
  --build-arg RHSM_USER="..." \
  --build-arg RHSM_PASS="..." \
  --manifest oadp-vm-file-server:rhel .

# Push manifest
podman manifest push oadp-vm-file-server:rhel \
  registry.redhat.io/oadp/oadp-vm-file-server:latest
```

---

## Local Development and Testing

For local testing without pushing to registry:

### Fedora Build (Quick Testing)

```bash
# Build
podman build -t oadp-vm-file-server:dev .

# Run locally
podman run -it --privileged \
  --device /dev/fuse \
  --device /dev/kvm \
  -v /path/to/test-data:/mnt/volumes \
  oadp-vm-file-server:dev \
  /bin/bash
```

### RHEL Build (Product Testing)

```bash
# Build
podman build -f Dockerfile.rhel \
  --build-arg RHSM_USER="your-username" \
  --build-arg RHSM_PASS="your-password" \
  -t oadp-vm-file-server:rhel-dev .

# Run locally
podman run -it --privileged \
  --device /dev/fuse \
  --device /dev/kvm \
  -v /path/to/test-data:/mnt/volumes \
  oadp-vm-file-server:rhel-dev \
  /bin/bash
```

---

## Quick Reference

### Which Dockerfile should I use?

| Scenario | Use | Build Command |
|----------|-----|---------------|
| Development | `Dockerfile` (Fedora) | `podman build -t my-image .` |
| Contributing to upstream | `Dockerfile` (Fedora) | `podman build -t my-image .` |
| CI/CD for community | `Dockerfile` (Fedora) | `podman build -t my-image .` |
| Red Hat internal builds | `Dockerfile.rhel` (RHEL) | `podman build -f Dockerfile.rhel --build-arg RHSM_USER=... -t my-image .` |
| Product release | `Dockerfile.rhel` (RHEL) | `podman build -f Dockerfile.rhel --build-arg RHSM_USER=... -t my-image .` |

### What's the difference at runtime?

**None.** Both images:
- ✅ Have identical packages and tools
- ✅ Run as qemu user (UID 107)
- ✅ Work identically in OpenShift
- ✅ Have same security profile
- ✅ Don't require subscription to run

The only difference is the build process.

---

## Questions?

- **Where do I get Red Hat credentials?** https://developers.redhat.com/
- **Can I use RHEL for free?** Yes, developer subscription is free
- **Why does RHEL build need subscription?** To access libguestfs packages in RHEL repos
- **Does the runtime image need subscription?** No, only build process
- **Which should I use for development?** Fedora (Dockerfile) - simpler and no credentials needed

## References

- Red Hat Subscription Management: https://access.redhat.com/management
- Red Hat Developer Program: https://developers.redhat.com/
- Fedora Container Images: https://registry.fedoraproject.org/
- UBI Images: https://catalog.redhat.com/software/containers/ubi9/ubi/615bcf606feffc5384e8452e
- Podman Build Secrets: https://docs.podman.io/en/latest/markdown/podman-build.1.html#secret

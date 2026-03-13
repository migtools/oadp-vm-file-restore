# Building the OADP VM File Access Container

This document explains how to build the file-server container image.

## Two Dockerfiles: Upstream vs Downstream

This project provides **two Dockerfiles** for different use cases:

| Dockerfile | Base Image | Build Requirements | Who Uses It |
|------------|------------|-------------------|-------------|
| **Dockerfile** | Fedora 42 | None | Community, contributors, upstream development |
| **konflux.Dockerfile** | RHEL 9 UBI | None (Konflux handles it) | Red Hat Konflux CI/CD (official product builds) |

Both produce functionally identical images - the choice is about build context and automation.

---

## Building Upstream (Fedora 42) - Recommended for Development

### Prerequisites

**None!** Anyone can build this.

### Build Command

```bash
cd containers/file-server/

# Simple build (uses Dockerfile by default)
podman build -t oadp-vmfr-access:latest .

# Or explicitly specify Dockerfile
podman build -f Dockerfile -t oadp-vmfr-access:latest .
```

### Why Fedora 42?

- ✅ Latest stable Fedora release
- ✅ All packages available without subscription
- ✅ Anyone can build (community friendly)
- ✅ Multi-arch support (x86_64, aarch64)
- ✅ Standard upstream/downstream pattern (like Velero, Prometheus Operator)

---

## Building Downstream (RHEL 9) - For Red Hat Product via Konflux

### What is Konflux?

Red Hat Konflux is Red Hat's official container build system (successor to OSBS). It's used for building all official Red Hat product container images, including OADP.

### konflux.Dockerfile

The `konflux.Dockerfile` follows the standard Red Hat pattern (same as `openshift/oadp-operator`) and is specifically designed for automated Konflux builds.

**Key features:**
- ✅ Uses RHEL 9 UBI base image
- ✅ Includes LICENSE file in `/licenses/` (Red Hat requirement)
- ✅ Red Hat-specific metadata labels
- ✅ NO manual subscription management (Konflux handles this)
- ✅ Identical tools and functionality to Fedora build

### How Konflux Builds Work

Konflux builds are **fully automated**:

1. Code merged to tracked branch (e.g., `oadp-1.5`)
2. Konflux detects `konflux.Dockerfile`
3. Build runs with automatic RHEL repository access (no credentials needed)
4. Image pushed to official Red Hat registries
5. Image goes through Red Hat security scanning and certification

### Manual Testing of konflux.Dockerfile (Optional)

For local development/testing, use the **Dockerfile** (Fedora) instead - it's simpler and requires no credentials.

The konflux.Dockerfile is primarily for the automated Red Hat build pipeline.

If you need to test the konflux.Dockerfile locally:
```bash
cd containers/file-server/

# This will work if you have access to Red Hat internal build environment
podman build -f konflux.Dockerfile \
  -t oadp-vmfr-access:konflux \
  .
```

**Note:** Outside the Konflux environment, this may fail due to repository access. Use the Fedora Dockerfile for local testing instead.

### Required Files for Konflux

When Konflux builds, it needs:
- ✅ `konflux.Dockerfile` - The build definition
- ✅ `LICENSE` - Red Hat container policy requirement (symlinked from repo root)
- ✅ `scripts/` - Helper scripts to copy into container

---

## Testing the Build

After building (either Dockerfile), verify the image:

```bash
# Check image was created
podman images | grep oadp-vmfr-access

# Verify libguestfs is installed
podman run --rm oadp-vmfr-access:latest guestmount --version

# Check qemu-img is available
podman run --rm oadp-vmfr-access:latest qemu-img --version

# Verify running as qemu user (UID 107)
podman run --rm --entrypoint /bin/bash oadp-vmfr-access:latest -c "id"
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
          podman build -t oadp-vmfr-access:latest .

      - name: Push to quay.io
        run: |
          podman login -u="${{ secrets.QUAY_USER }}" \
                      -p="${{ secrets.QUAY_TOKEN }}" \
                      quay.io
          podman push quay.io/konveyor/oadp-vmfr-access:oadp-1.6
```

**Required GitHub Secrets:**
- `QUAY_USER` - Quay.io username
- `QUAY_TOKEN` - Quay.io token

### Red Hat Konflux (Downstream)

Red Hat product builds use the **Konflux** build system, not GitHub Actions.

Konflux builds are configured through Red Hat's internal build pipeline and happen automatically when code is merged to tracked branches (e.g., `oadp-1.5`).

**No GitHub Actions configuration needed for downstream** - Konflux handles everything automatically:
- ✅ Detects `konflux.Dockerfile`
- ✅ Provides RHEL repository access
- ✅ Builds multi-arch images
- ✅ Pushes to Red Hat registries
- ✅ Runs security scans
- ✅ Certifies the image

For Konflux configuration, see Red Hat internal documentation.

---

## Troubleshooting

### Fedora Build Issues

#### Error: "No package libguestfs-tools available"

**Cause:** Fedora version too old or mirrors not synced.

**Fix:**
1. Verify Fedora 42 is available: https://fedoraproject.org/
2. Try with explicit mirror: `dnf install --setopt=fastestmirror=false ...`

### Konflux Build Issues

#### Error: "Unable to find a match: libguestfs-tools"

**Cause:** Running konflux.Dockerfile outside the Konflux environment.

**Fix:** Use the Fedora Dockerfile for local development:
```bash
podman build -f Dockerfile -t oadp-vmfr-access:dev .
```

The konflux.Dockerfile is designed for automated Konflux builds only.

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
podman run --rm --entrypoint /bin/bash oadp-vmfr-access:rhel \
  -c "ls -la /var/lib/rhsm/ 2>&1"
# Expected: directory not found or empty
```

### Image Scanning

Always scan built images for vulnerabilities:
```bash
# Using podman
podman scan oadp-vmfr-access:latest

# Using trivy
trivy image oadp-vmfr-access:latest
```

---

## Multi-Architecture Builds

Both Dockerfiles support multi-arch builds:

### Fedora (Upstream)

```bash
podman manifest create oadp-vmfr-access:latest

# Build for x86_64
podman build --platform linux/amd64 \
  --manifest oadp-vmfr-access:latest .

# Build for aarch64
podman build --platform linux/arm64 \
  --manifest oadp-vmfr-access:latest .

# Push manifest
podman manifest push oadp-vmfr-access:latest \
  quay.io/konveyor/oadp-vmfr-access:oadp-1.6
```

### Konflux (Downstream)

Konflux automatically handles multi-arch builds. No manual commands needed - it builds for all supported architectures automatically.

---

## Local Development and Testing

For local testing without pushing to registry:

### Quick Local Testing

```bash
# Build
podman build -t oadp-vmfr-access:dev .

# Run locally
podman run -it --privileged \
  --device /dev/fuse \
  --device /dev/kvm \
  -v /path/to/test-data:/mnt/volumes \
  oadp-vmfr-access:dev \
  /bin/bash
```

For testing Konflux builds, use the same Fedora build - they produce functionally identical images.

---

## Quick Reference

### Which Dockerfile should I use?

| Scenario | Use | Build Command |
|----------|-----|---------------|
| Development | `Dockerfile` (Fedora) | `podman build -t my-image .` |
| Contributing to upstream | `Dockerfile` (Fedora) | `podman build -t my-image .` |
| CI/CD for community | `Dockerfile` (Fedora) | `podman build -t my-image .` |
| Testing | `Dockerfile` (Fedora) | `podman build -t my-image .` |
| Red Hat internal builds | `konflux.Dockerfile` (RHEL) | Automated by Konflux |
| Product release | `konflux.Dockerfile` (RHEL) | Automated by Konflux |

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

- **Which Dockerfile should I use for development?** Always use `Dockerfile` (Fedora) - it's simpler and requires no credentials
- **How do Red Hat product builds happen?** Automatically via Konflux when code is merged
- **Can I test konflux.Dockerfile locally?** Not recommended - use Fedora Dockerfile for local testing instead
- **Do the images differ at runtime?** No - both Fedora and Konflux images have identical tools and behavior
- **Do I need a Red Hat subscription?** Not for development or testing - only Konflux automated builds need access to RHEL repos

## References

- Red Hat Subscription Management: https://access.redhat.com/management
- Red Hat Developer Program: https://developers.redhat.com/
- Fedora Container Images: https://registry.fedoraproject.org/
- UBI Images: https://catalog.redhat.com/software/containers/ubi9/ubi/615bcf606feffc5384e8452e
- Podman Build Secrets: https://docs.podman.io/en/latest/markdown/podman-build.1.html#secret

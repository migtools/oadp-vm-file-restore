# VMFR Controller Integration Guide (Issue #7)

This document provides a complete checklist and implementation guide for the VMFR (VirtualMachineFileRestore) controller to create file-serving pods using the `oadp-vmfr-access` container.

**Based on:** Live cluster testing completed in Issue #6 (OpenShift Virtualization)

---

## Table of Contents

1. [Pre-Deployment Checklist](#pre-deployment-checklist)
2. [Pod Specification Requirements](#pod-specification-requirements)
3. [Security Configuration](#security-configuration)
4. [Volume Mounting](#volume-mounting)
5. [Environment Variables](#environment-variables)
6. [Pod Lifecycle Management](#pod-lifecycle-management)
7. [Validation and Verification](#validation-and-verification)
8. [Troubleshooting](#troubleshooting)
9. [Complete Example Pod Spec](#complete-example-pod-spec)

---

## Pre-Deployment Checklist

Before the controller creates a file-server pod, verify these prerequisites:

### ✅ Cluster Requirements

- [ ] **OpenShift Virtualization** (or KubeVirt) installed
- [ ] **kubevirt-controller SCC** available in cluster
- [ ] **Namespace has privileged Pod Security level** (for privileged containers)
- [ ] `/dev/kvm` device available on worker nodes (hardware virtualization)
- [ ] `/dev/fuse` device available on worker nodes (FUSE support)

### ✅ PVC Requirements

- [ ] **Restored PVC exists** (from Velero backup)
- [ ] **PVC contains VM disk image** (qcow2, raw, img, vmdk, or vdi format)
- [ ] **PVC is not currently mounted by a running VM** (ReadWriteOnce conflict)
- [ ] **Know the PVC's SELinux MCS labels** (see [SELinux Label Discovery](#selinux-label-discovery))

### ✅ Image Requirements

- [ ] **Container image available:** `quay.io/konveyor/oadp-vmfr-access:oadp-1.6`
- [ ] **Image pull secrets configured** (if using private registry)

---

## Pod Specification Requirements

### 1. Pod Metadata

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: <vmfr-name>-file-server  # e.g., "fedora-vm-restore-file-server"
  namespace: <target-namespace>
  labels:
    app: oadp-vmfr-access
    vmfr: <vmfr-cr-name>
    vm: <original-vm-name>
  ownerReferences:
  - apiVersion: oadp.openshift.io/v1alpha1
    kind: VirtualMachineFileRestore
    name: <vmfr-cr-name>
    uid: <vmfr-cr-uid>
    controller: true
    blockOwnerDeletion: true
```

**Why:**
- `ownerReferences`: Ensures pod is cleaned up when VMFR CR is deleted
- Labels: Enable filtering and tracking of file-server pods

---

## Security Configuration

### 2. Pod-Level Security Context

**CRITICAL:** These settings are required for the pod to access VM disk PVCs.

```yaml
spec:
  securityContext:
    # Run as qemu user/group - REQUIRED to access VM disk files
    fsGroup: 107              # qemu group
    supplementalGroups:
    - 107                     # qemu group

    # SELinux label - MUST match the PVC's MCS label
    seLinuxOptions:
      level: "<pvc-mcs-label>"  # e.g., "s0:c468,c664"
```

**TODO for Controller:**
- [ ] **Discover PVC's SELinux MCS label** before creating pod (see [SELinux Label Discovery](#selinux-label-discovery))
- [ ] **Set `seLinuxOptions.level`** to match PVC's label exactly
- [ ] **Always use fsGroup: 107** (qemu group in OpenShift Virtualization)

#### SELinux Label Discovery

**Method 1: Query the virt-launcher pod** (if VM was running before backup)
```bash
# Find the virt-launcher pod that was using this PVC
oc get pods -l kubevirt.io/vm=<vm-name> -o yaml | grep -A 5 seLinuxOptions

# Example output:
# seLinuxOptions:
#   level: s0:c468,c664
```

**Method 2: Create temporary pod to check PVC** (if virt-launcher not available)
```bash
# Create a temporary pod that mounts the PVC
# Check the PVC's SELinux label with: ls -laZ /mnt/disk/
# Extract the MCS label from: system_u:object_r:container_file_t:s0:c468,c664
# Use the level portion: s0:c468,c664
```

**Method 3: Store in VMFR CR** (recommended approach)
- When user creates VMFR CR, they can optionally provide SELinux label
- If not provided, controller uses Method 1 or 2 to discover it
- Store discovered label in VMFR CR status for future reference

### 3. Container-Level Security Context

**CRITICAL:** Privileged mode is required for `/dev/kvm` access with SELinux.

```yaml
containers:
- name: file-server
  securityContext:
    privileged: true        # REQUIRED for /dev/kvm access
    runAsUser: 107          # qemu user
    runAsGroup: 107         # qemu group
```

**Why Privileged Mode:**
- libguestfs uses QEMU with KVM hardware acceleration
- SELinux blocks non-privileged containers from accessing `/dev/kvm` (kvm_device_t)
- Without KVM: QEMU uses TCG (software emulation) - 10-100x slower (5+ minutes vs 30 seconds)
- **Security:** Same security model as KubeVirt virt-launcher pods (already trusted in cluster)

**TODO for Controller:**
- [ ] **Ensure namespace allows privileged pods** (Pod Security Standards)
- [ ] **Use kubevirt-controller SCC** (or create custom SCC with same permissions)

---

## Volume Mounting

### 4. Volume Definitions

```yaml
volumes:
# Volume 1: FUSE device (required for guestmount)
- name: fuse-device
  hostPath:
    path: /dev/fuse
    type: CharDevice

# Volume 2: KVM device (required for performance)
- name: kvm-device
  hostPath:
    path: /dev/kvm
    type: CharDevice

# Volume 3: Restored VM disk PVC(s)
- name: vm-disk-1
  persistentVolumeClaim:
    claimName: <restored-pvc-name>  # e.g., "restored-vm-rootdisk"

# Add more PVCs if restoring multiple disks
# - name: vm-disk-2
#   persistentVolumeClaim:
#     claimName: <restored-pvc-name-2>

# Volume 4: Filesystem mount points (emptyDir)
- name: filesystems
  emptyDir: {}
```

**TODO for Controller:**
- [ ] **Add /dev/fuse hostPath volume**
- [ ] **Add /dev/kvm hostPath volume**
- [ ] **Add PVC volume for each restored disk**
- [ ] **Add emptyDir for filesystem mounts**

### 5. Volume Mounts

```yaml
volumeMounts:
# Mount 1: FUSE device
- name: fuse-device
  mountPath: /dev/fuse

# Mount 2: KVM device
- name: kvm-device
  mountPath: /dev/kvm

# Mount 3: VM disk PVC(s) - mount to /mnt/volumes/
- name: vm-disk-1
  mountPath: /mnt/volumes/<disk-name>.img  # e.g., /mnt/volumes/rootdisk.img
  subPath: disk.img                         # Path to disk image inside PVC
  # NO readOnly: true - libguestfs needs write access to disk file

# If multiple disks:
# - name: vm-disk-2
#   mountPath: /mnt/volumes/<disk-name-2>.img
#   subPath: disk.img

# Mount 4: Filesystem mount points
- name: filesystems
  mountPath: /mnt/filesystems
```

**CRITICAL PVC Mount Notes:**
- **Do NOT use `readOnly: true`** on PVC mounts
  - libguestfs needs write access to disk file for internal operations (COW overlay, locking)
  - Filesystem is still mounted read-only inside container (guestmount --ro)
- **Use `subPath: disk.img`** to mount the disk image file directly
  - Most VM PVCs contain a disk.img file in the root
  - Adjust based on actual PVC structure

**TODO for Controller:**
- [ ] **Mount each restored PVC to /mnt/volumes/<unique-name>**
- [ ] **Ensure unique names if multiple disks** (e.g., rootdisk.img, datadisk.img)
- [ ] **Do NOT set readOnly: true on PVC mounts**
- [ ] **Verify disk file path inside PVC** (usually `disk.img` but can vary)

---

## Environment Variables

### 6. Required Environment Variables

```yaml
env:
# REQUIRED: libguestfs needs writable HOME for cache
- name: HOME
  value: /tmp

# Optional: Override default mount directories
# - name: VOLUME_MOUNT_DIR
#   value: /mnt/volumes    # Default, usually no need to change
# - name: FS_MOUNT_DIR
#   value: /mnt/filesystems # Default, usually no need to change
```

**TODO for Controller:**
- [ ] **Always set HOME=/tmp** (libguestfs requirement)
- [ ] **Optional:** Set custom mount directories if needed

---

## Pod Lifecycle Management

### 7. Container Command and Image

```yaml
containers:
- name: file-server
  image: quay.io/konveyor/oadp-vmfr-access:oadp-1.6
  imagePullPolicy: Always  # Use Always for :dev, IfNotPresent for tagged versions

  # Run detect-and-mount.sh to automatically mount all disks
  command: ["/usr/local/bin/detect-and-mount.sh"]
  # Script will:
  # 1. Scan /mnt/volumes/ for disk images
  # 2. Mount each disk to /mnt/filesystems/<disk-name>/
  # 3. Enter sleep infinity to keep pod alive
```

**TODO for Controller:**
- [ ] **Use command: ["/usr/local/bin/detect-and-mount.sh"]**
- [ ] **Set appropriate imagePullPolicy**
- [ ] **Consider using specific version tag** (not :dev) for production

### 8. Resource Limits

```yaml
resources:
  requests:
    memory: "512Mi"
    cpu: "250m"
  limits:
    memory: "2Gi"    # libguestfs can use significant memory
    cpu: "1000m"     # KVM can use significant CPU during mount
```

**Why These Values:**
- **Memory:** libguestfs QEMU appliance needs ~500MB-1GB during mounting
- **CPU:** QEMU uses significant CPU during initial boot (~30 seconds)
- **After mounting:** Resource usage drops significantly (just FUSE overhead)

**TODO for Controller:**
- [ ] **Set appropriate resource limits** based on cluster capacity
- [ ] **Consider making these configurable** via VMFR CR spec

### 9. Restart Policy

```yaml
spec:
  restartPolicy: Never  # or OnFailure
```

**Recommendation:** Use `Never` for file-server pods
- If mount fails, pod should stay in Error state for debugging
- Controller can detect failure and report in VMFR CR status
- User can delete and recreate VMFR CR to retry

**TODO for Controller:**
- [ ] **Use restartPolicy: Never** for better error visibility
- [ ] **Watch pod status and update VMFR CR status** accordingly

---

## Validation and Verification

### 10. Pod Status Checks

After creating the pod, the controller should verify:

```go
// Wait for pod to start
// Expected: Pod phase = Running (within 1-2 minutes)

// Check container status
// Expected: Container state = Running
// Expected: Container exit code = 0 (if terminated, indicates error)

// Verify logs contain success messages
// Expected logs:
// - "Auto-mount completed: X successful, 0 failed"
// - "Currently mounted filesystems:"
// - "/dev/fuse on /mnt/filesystems/<disk-name>"
// - "Keeping container alive to maintain FUSE mounts..."
```

**TODO for Controller:**
- [ ] **Watch pod status until Running or Error**
- [ ] **Check logs for mount success messages**
- [ ] **Update VMFR CR status with pod name and status**
- [ ] **Report errors if pod fails to start or mount fails**

### 11. Filesystem Accessibility Verification

Optional but recommended: Verify filesystems are actually accessible

```go
// Execute in pod to verify mount
// kubectl exec <pod-name> -- mount | grep /mnt/filesystems

// Expected output:
// /dev/fuse on /mnt/filesystems/<disk-name> type fuse (rw,nosuid,nodev,relatime,...)

// Optional: Try to list filesystem contents
// kubectl exec <pod-name> -- ls /mnt/filesystems/<disk-name>/
```

**TODO for Controller:**
- [ ] **Optional:** Execute `mount` command to verify FUSE mounts
- [ ] **Optional:** List filesystem root to confirm readability
- [ ] **Update VMFR CR status with mount points**

---

## Troubleshooting

### Common Issues and Solutions

#### Issue 1: Pod Fails with "Permission denied" on PVC

**Symptoms:**
```
guestmount: error: cannot access /mnt/volumes/disk.img: Permission denied
```

**Causes:**
- SELinux MCS label mismatch
- Wrong fsGroup (not 107)
- PVC still mounted by running VM

**Solutions:**
- [ ] Verify `seLinuxOptions.level` matches PVC's MCS label exactly
- [ ] Verify `fsGroup: 107` is set
- [ ] Ensure source VM is stopped before creating file-server pod

#### Issue 2: Pod Stays in "Error" State

**Check logs:**
```bash
oc logs <pod-name>
```

**Common causes:**
- Missing HOME=/tmp environment variable
- Disk image format not supported
- Corrupted disk image
- Out of memory (libguestfs needs ~512MB-1GB)

**Solutions:**
- [ ] Ensure HOME=/tmp is set
- [ ] Check disk image format with `qemu-img info`
- [ ] Increase memory limits

#### Issue 3: Pod Starts but Shows "Error" Status

**Symptoms:** Pod runs briefly, then exits with Error

**Cause:** Script exited prematurely (should not happen with current version)

**Solutions:**
- [ ] Check logs for error messages before exit
- [ ] Verify image version is latest (contains `set -uo pipefail` fix)
- [ ] Check if disk image file exists in PVC

#### Issue 4: Slow Performance or Timeouts

**Symptoms:** Pod takes 5+ minutes to start, or times out

**Cause:** KVM not accessible (falling back to TCG software emulation)

**Check:**
```bash
oc logs <pod-name> | grep -i "kvm\|tcg"
```

**Solutions:**
- [ ] Verify privileged: true is set
- [ ] Verify /dev/kvm hostPath volume is mounted
- [ ] Check worker node has /dev/kvm device (`ls -la /dev/kvm`)
- [ ] Verify hardware virtualization enabled in BIOS (for bare metal)

---

## Complete Example Pod Spec

Here's a complete, tested pod specification based on live cluster testing:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: vmfr-fedora-vm-file-server
  namespace: oadp-vm-restore
  labels:
    app: oadp-vmfr-access
    vmfr: fedora-vm-restore
    vm: fedora-vm
  ownerReferences:
  - apiVersion: oadp.openshift.io/v1alpha1
    kind: VirtualMachineFileRestore
    name: fedora-vm-restore
    uid: <vmfr-uid>
    controller: true
    blockOwnerDeletion: true

spec:
  # Pod-level security context
  securityContext:
    fsGroup: 107                    # qemu group - required for VM disk access
    supplementalGroups:
    - 107                           # qemu group
    seLinuxOptions:
      level: "s0:c468,c664"         # MUST match PVC's SELinux MCS label

  containers:
  - name: file-server
    image: quay.io/konveyor/oadp-vmfr-access:oadp-1.6
    imagePullPolicy: Always

    # Run detect-and-mount.sh to automatically mount all disks
    command: ["/usr/local/bin/detect-and-mount.sh"]

    # Environment variables
    env:
    - name: HOME
      value: /tmp                   # Required for libguestfs cache

    # Container-level security context
    securityContext:
      privileged: true              # Required for /dev/kvm access
      runAsUser: 107                # qemu user
      runAsGroup: 107               # qemu group

    # Volume mounts
    volumeMounts:
    - name: fuse-device
      mountPath: /dev/fuse
    - name: kvm-device
      mountPath: /dev/kvm
    - name: vm-disk
      mountPath: /mnt/volumes/rootdisk.img
      subPath: disk.img             # Adjust based on PVC structure
    - name: filesystems
      mountPath: /mnt/filesystems

    # Resource limits
    resources:
      requests:
        memory: "512Mi"
        cpu: "250m"
      limits:
        memory: "2Gi"
        cpu: "1000m"

  # Volumes
  volumes:
  - name: fuse-device
    hostPath:
      path: /dev/fuse
      type: CharDevice
  - name: kvm-device
    hostPath:
      path: /dev/kvm
      type: CharDevice
  - name: vm-disk
    persistentVolumeClaim:
      claimName: restored-fedora-vm-rootdisk
  - name: filesystems
    emptyDir: {}

  restartPolicy: Never
```

---

## Controller Implementation Checklist

### Phase 1: Pre-Creation Validation
- [ ] Verify namespace exists
- [ ] Verify namespace allows privileged pods
- [ ] Verify kubevirt-controller SCC is available
- [ ] Verify restored PVC exists and is available
- [ ] Discover PVC's SELinux MCS label
- [ ] Verify source VM is not running (for ReadWriteOnce PVCs)

### Phase 2: Pod Creation
- [ ] Generate unique pod name
- [ ] Set owner references to VMFR CR
- [ ] Set pod-level security context (fsGroup: 107, SELinux label)
- [ ] Set container-level security context (privileged: true, runAsUser: 107)
- [ ] Add /dev/fuse hostPath volume
- [ ] Add /dev/kvm hostPath volume
- [ ] Add restored PVC volume(s)
- [ ] Add emptyDir for filesystems
- [ ] Mount all volumes correctly
- [ ] Set HOME=/tmp environment variable
- [ ] Set command: ["/usr/local/bin/detect-and-mount.sh"]
- [ ] Set resource limits
- [ ] Set restartPolicy: Never
- [ ] Create pod in cluster

### Phase 3: Status Monitoring
- [ ] Watch pod status until Running or Error
- [ ] Parse logs for mount success/failure
- [ ] Update VMFR CR status with:
  - Pod name
  - Pod status
  - Mount points (e.g., /mnt/filesystems/rootdisk)
  - Error messages (if any)
- [ ] Set VMFR CR conditions (Ready, Available, Failed)

### Phase 4: Cleanup
- [ ] Watch for VMFR CR deletion
- [ ] Pod automatically deleted via ownerReferences
- [ ] Update metrics/status

---

## Testing the Controller Implementation

### Manual Test Procedure

1. **Create test VMFR CR:**
```yaml
apiVersion: oadp.openshift.io/v1alpha1
kind: VirtualMachineFileRestore
metadata:
  name: test-vm-restore
  namespace: test-namespace
spec:
  vmName: test-vm
  pvcName: restored-test-vm-rootdisk
```

2. **Verify controller creates pod:**
```bash
oc get pods -l vmfr=test-vm-restore
# Expected: Pod created with name matching pattern
```

3. **Verify pod reaches Running state:**
```bash
oc get pod <pod-name>
# Expected: STATUS = Running within 1-2 minutes
```

4. **Verify filesystem is mounted:**
```bash
oc exec <pod-name> -- mount | grep /mnt/filesystems
# Expected: /dev/fuse on /mnt/filesystems/...
```

5. **Verify files are accessible:**
```bash
oc exec <pod-name> -- ls /mnt/filesystems/<disk-name>/
# Expected: Directory listing from VM filesystem
```

6. **Verify VMFR CR status is updated:**
```bash
oc get vmfr test-vm-restore -o yaml
# Expected: status.podName, status.ready, status.mountPoints
```

7. **Test cleanup:**
```bash
oc delete vmfr test-vm-restore
# Expected: Pod automatically deleted via ownerReferences
```

---

## References

- **Issue #6:** File-server container implementation and live testing
- **test-pod.yaml:** Working pod configuration from live cluster testing
- **Live Testing Results:** containers/file-server/README.md (Current Status section)
- **Security Requirements:** containers/file-server/README.md (Security section)

---

**Last Updated:** Based on live cluster testing completed in Issue #6 (October 2025)

**Container Image Version:** `quay.io/konveyor/oadp-vmfr-access:oadp-1.6`

**Status:** ✅ Ready for controller implementation

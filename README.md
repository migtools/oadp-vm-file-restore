# OADP VM File Restore

This project provides a Kubernetes controller for discovering and serving files from VM backups created by Velero/OADP.

## Current Implementation Status

**✅ Completed Features:**
- **VirtualMachineBackupsDiscovery**: Complete backup discovery workflow
- **VirtualMachineFileRestore**: Basic controller with PV identification (file serving in progress)

**🚧 In Development:**
- File serving pod creation and management
- Volume restoration and mounting

## Prerequisites

- OpenShift cluster with OADP operator installed
- Virtual machine backups created by Velero/OADP
- oc CLI configured to access the cluster

## Deployment and Testing

### 1. Verify OADP Setup

Before deploying the controller, ensure OADP is properly configured:

```bash
# Check OADP operator is installed
oc get csv -n openshift-adp | grep oadp

# Verify DataProtectionApplication (DPA) is configured
oc get dpa -n openshift-adp

# Check BackupStorageLocation is ready
oc get bsl -n openshift-adp

# List existing backups (There are two Backup kind in OpenShift, specify one with suffix)
oc get backups.velero.io -n openshift-adp
```

### 2. Deploy the Controller

#### Option A: Run Locally (Development)
```bash
# Install CRDs to the cluster
make install

# Run controller locally (recommended for testing)
make run
```

#### Option B: Deploy to OpenShift Cluster
```bash
# Build and deploy the controller
make docker-build docker-push IMG=<your-registry>/oadp-vm-file-restore:latest
make deploy IMG=<your-registry>/oadp-vm-file-restore:latest

# Alternative: Install CRDs only (if controller is already running)
make install

# Check deployment status
oc get pods -n oadp-vm-file-restore-system
```

### 2. Testing Backup Discovery

The discovery functionality identifies which Velero backups contain a specific virtual machine.

**Note**: All discovery and file restore resources must be created in the same namespace as the OADP operator (typically `openshift-adp`).

#### Create a Discovery Resource

**Example 1: Time-based Discovery**
```yaml
apiVersion: oadp.openshift.io/v1alpha1
kind: VirtualMachineBackupsDiscovery
metadata:
  name: my-vm-discovery
  namespace: openshift-adp  # Must be in OADP namespace
spec:
  virtualMachineName: my-vm
  virtualMachineNamespace: my-app-namespace
  startTime: "2025-01-01"
  endTime: "2025-09-17"
```

**Example 2: Explicit Backup List**
```yaml
apiVersion: oadp.openshift.io/v1alpha1
kind: VirtualMachineBackupsDiscovery
metadata:
  name: my-vm-discovery
  namespace: openshift-adp
spec:
  virtualMachineName: my-vm
  virtualMachineNamespace: my-app-namespace
  requestedBackups:
    - backup-2025-09-15
    - backup-2025-09-16
```

#### Monitor Discovery Progress

```bash
# Watch discovery progress (vmbd is short for virtualmachinebackupsdiscovery)
oc get vmbd my-vm-discovery -w

# View detailed status
oc get vmbd my-vm-discovery -o yaml

# Check discovery results
oc get vmbd my-vm-discovery -o jsonpath='{.status.validBackups[*].name}'

# List all discoveries
oc get vmbd

# Show discovery statistics
oc get vmbd my-vm-discovery -o jsonpath='{.status.discoveryStats}'
```

**Tip**: You can also monitor resources through the OpenShift Console under `Administration` → `Custom Resource Definitions` → Search for "VirtualMachineBackupsDiscovery".

#### Discovery Status Fields

- **Phase**: `New` → `InProgress` → `Completed`/`Failed`
- **ValidBackups**: List of backups containing the VM
- **InvalidBackups**: Requested backups that don't contain the VM
- **DiscoveryStats**: Summary statistics (total, completed, failed, etc.)

### 4. Testing File Restore (PV Discovery)

Once discovery is complete, you can create a file restore resource to identify PVs.

#### Create a File Restore Resource

```yaml
apiVersion: oadp.openshift.io/v1alpha1
kind: VirtualMachineFileRestore
metadata:
  name: my-file-restore
  namespace: openshift-adp
spec:
  backupsDiscoveryRef: my-vm-discovery
  # Optional: specify which backups to use
  selectedBackups:
    - backup-2025-09-16
```

#### Monitor File Restore Progress

```bash
# Watch file restore progress (vmfr is short for virtualmachinefilerestore)
oc get vmfr my-file-restore -w

# View discovered PVs
oc get vmfr my-file-restore -o jsonpath='{.status.fileServingInfo.discoveredPVs}'

# Check detailed status
oc get vmfr my-file-restore -o yaml

# List all file restores
oc get vmfr

# Quick status overview
oc get vmfr my-file-restore -o custom-columns="NAME:.metadata.name,PHASE:.status.phase,DISCOVERY:.spec.backupsDiscoveryRef,PVS:.status.fileServingInfo.discoveredPVs"
```

### 5. Common Discovery Scenarios

#### No Valid Backups Found
If discovery completes but finds no valid backups:
- **Phase**: `Completed`
- **Condition**: `Complete: False` with reason `NoValidBackups`
- **Check**: VM name/namespace, backup inclusion/exclusion rules

#### Backup Not Found
If requested backup doesn't exist:
- **InvalidBackups**: Lists missing backups with reason "Backup not found in cluster"

#### VM Not in Backup
If backup exists but doesn't contain the VM:
- **InvalidBackups**: Lists backups with reason "VM not found in backup"

### 6. Troubleshooting

#### Discovery Stuck in InProgress
```bash
# Check controller logs
oc logs -n oadp-vm-file-restore-system deployment/oadp-vm-file-restore-controller-manager

# Check backup accessibility
oc get backups -n openshift-adp

# Verify OADP operator is working
oc get bsl -n openshift-adp

# Check OADP operator status
oc get dpa -n openshift-adp
```

#### File Restore Stuck in BackingOff
```bash
# Check if discovery is complete
oc get vmbd <discovery-name> -o jsonpath='{.status.phase}'

# Verify discovery has valid backups
oc get vmbd <discovery-name> -o jsonpath='{.status.validBackups}'
```

### 7. Development Commands

```bash
# Run tests
make test

# Build locally
make build

# Generate manifests after API changes
make manifests generate

# Format and lint code
make fmt vet lint
```

## Kubebuilder

The project was generated using kubebuilder version `v3.33.0`, running the following commands
```sh
kubebuilder init \
    --plugins go.kubebuilder.io/v4 \
    --project-version 3 \
    --project-name=oadp-vm-file-restore \
    --repo=github.com/migtools/oadp-vm-file-restore \
    --domain=openshift.io
kubebuilder create api \
    --plugins go.kubebuilder.io/v4 \
    --group oadp \
    --version v1alpha1 \
    --kind VirtualMachineFileRestore \
    --resource --controller
make manifests
```

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

// Package controller implements helper functions for file server pod and service creation
package controller

import (
	"encoding/json"
	"fmt"
	"strings"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	oadpv1alpha1 "github.com/migtools/oadp-vm-file-restore/api/v1alpha1"
	oadptypes "github.com/migtools/oadp-vm-file-restore/api/v1alpha1/types"
	"github.com/migtools/oadp-vm-file-restore/internal/common/constant"
)

// PVCMountInfo contains information needed to mount a PVC in the file server pod
type PVCMountInfo struct {
	PVCName           string // Original PVC name (at time of backup)
	PVCNamespace      string
	PVCUID            string
	BackupName        string
	BackupTimestamp   *metav1.Time
	VeleroRestoreName string                       // Name of the Velero Restore CR that restored this PVC
	RestoredPVCName   string                       // Actual name of the restored PVC (may differ from original)
	VolumeMode        *corev1.PersistentVolumeMode // VolumeMode of the PVC (Block or Filesystem), nil if not yet queried
}

// SSHAccessConfig contains configuration for SSH sidecar container
type SSHAccessConfig struct {
	// Username for SSH access
	Username string

	// CredentialsSecretName is the name of the Secret containing SSH credentials
	// The controller ensures this Secret exists before pod creation (via ensureCredentials)
	// +optional
	CredentialsSecretName string

	// CredentialsSecretNamespace is the namespace of the Secret
	// +optional
	CredentialsSecretNamespace string

	// Port for SSH service (hardcoded to constant.DefaultSSHPort)
	Port int32
}

// FileBrowserAccessConfig contains configuration for FileBrowser sidecar container
type FileBrowserAccessConfig struct {
	// CredentialsSecretName is the name of the Secret containing username/password
	// The controller must ensure this Secret exists before pod creation
	CredentialsSecretName string

	// CredentialsSecretNamespace is the namespace of the Secret
	CredentialsSecretNamespace string

	// Port for FileBrowser HTTPS service (hardcoded to constant.DefaultFileBrowserPort)
	Port int32
}

// FileServerPodConfig contains configuration for building a file server pod
type FileServerPodConfig struct {
	// PodName is the name for the file server pod
	PodName string

	// PodNamespace is the namespace where the pod will be created
	PodNamespace string

	// VMFRName is the name of the VirtualMachineFileRestore that owns this pod
	VMFRName string

	// VMFRNamespace is the namespace of the VirtualMachineFileRestore
	VMFRNamespace string

	// VMFRUID is the UID of the VirtualMachineFileRestore (for owner reference)
	VMFRUID string

	// PVCMounts is the list of PVCs to mount in the file server
	// These will be mounted in all containers (main + sidecars) at:
	// - /restores/<date>/<backup-name>/<pvc-name>/
	// Example: /restores/2025-10-24/test-vm-backup-20250115/test-vm-disk-1/
	PVCMounts []PVCMountInfo

	// MainContainer is the primary container that mounts the PVCs
	// If nil, a default busybox-based HTTP server will be used
	// The main container's VolumeMounts will be automatically populated with PVC volumes
	MainContainer *corev1.Container

	// SSHAccess enables SSH/SFTP/SCP/rsync access sidecar
	// If nil, SSH access is disabled
	SSHAccess *SSHAccessConfig

	// FileBrowserAccess enables HTTPS file browser sidecar
	// If nil, FileBrowser access is disabled
	FileBrowserAccess *FileBrowserAccessConfig

	// EnableDualPathAccess is deprecated and no longer used
	// Paths are now organized as: /restores/<date>/<backup>/<pvc-name>/
	// Kept for backward compatibility but has no effect
	EnableDualPathAccess bool

	// UseInternalMounts enables the main container to perform internal mount(2) syscalls
	// When enabled:
	// - Main container gets a shared EmptyDir volume with Bidirectional mount propagation
	// - PVCs are still added as volumes but main container mounts them internally
	// - Sidecars see the internal mounts via HostToContainer propagation
	// - Main container needs privilege/capabilities to perform mount(2)
	// Default: false (use Kubernetes-managed PVC mounts)
	UseInternalMounts bool

	// SharedMountPath is the path where the main container performs internal mounts
	// This path is shared with sidecars via mount propagation
	// Only used when UseInternalMounts is true
	// Example: "/mnt/restore" - main container mounts PVCs under this path
	// Default: "/mnt/restore"
	SharedMountPath string

	// MainContainerSecurityContext defines security settings for the main container
	// When UseInternalMounts is enabled, this should grant mount privileges:
	// - Privileged: true (full access), OR
	// - Capabilities: add SYS_ADMIN (minimal for mount(2))
	// If nil and UseInternalMounts is true, defaults to privileged
	MainContainerSecurityContext *corev1.SecurityContext

	// PodLabels are additional labels to add to the pod (merged with defaults)
	PodLabels map[string]string

	// PodAnnotations are additional annotations to add to the pod
	PodAnnotations map[string]string
}

// ServiceConfig contains configuration for building a Service
type ServiceConfig struct {
	// ServiceName is the name of the service
	ServiceName string

	// ServiceNamespace is the namespace where the service will be created
	ServiceNamespace string

	// VMFRName is the name of the VirtualMachineFileRestore (for labels and owner ref)
	VMFRName string

	// VMFRNamespace is the namespace of the VirtualMachineFileRestore
	VMFRNamespace string

	// VMFRUID is the UID of the VirtualMachineFileRestore (for owner reference)
	VMFRUID string

	// Ports defines the service ports to expose
	// Typically includes HTTP (always), SSH (optional), and FileBrowser HTTPS (optional)
	Ports []corev1.ServicePort

	// ServiceType specifies the type of service (ClusterIP, NodePort, LoadBalancer)
	// Defaults to ClusterIP if not specified
	ServiceType corev1.ServiceType

	// Selector specifies pod selector labels
	// If empty, defaults to selecting the VMFR pod by standard labels
	Selector map[string]string

	// ServiceLabels are additional labels to add to the service
	ServiceLabels map[string]string

	// ServiceAnnotations are additional annotations to add to the service
	ServiceAnnotations map[string]string
}

// RouteConfig contains configuration for building an OpenShift Route
type RouteConfig struct {
	// RouteName is the name of the route
	RouteName string

	// RouteNamespace is the namespace where the route will be created
	RouteNamespace string

	// VMFRName is the name of the VirtualMachineFileRestore (for labels)
	VMFRName string

	// VMFRNamespace is the namespace of the VirtualMachineFileRestore
	VMFRNamespace string

	// VMFRUID is the UID of the VirtualMachineFileRestore (for labels)
	VMFRUID string

	// ServiceName is the name of the target service
	ServiceName string

	// TargetPort is the service port to route to (e.g., "ssh", "https")
	TargetPort string

	// TLSTermination specifies the TLS termination type (passthrough, reencrypt, edge)
	TLSTermination routev1.TLSTerminationType

	// InsecureEdgeTerminationPolicy specifies how to handle HTTP traffic
	InsecureEdgeTerminationPolicy routev1.InsecureEdgeTerminationPolicyType

	// RouteLabels are additional labels to add to the route
	RouteLabels map[string]string

	// RouteAnnotations are additional annotations to add to the route
	RouteAnnotations map[string]string

	// Subdomain is the subdomain for the route (e.g., "vmfr-name.vmfr")
	// When set, the route will use this subdomain instead of auto-generated hostname.
	// Final hostname will be: <subdomain>.<ingress-domain>
	// +optional
	Subdomain string
}

// buildFileServerPodSpec builds a Pod spec for serving files from restored PVCs.
//
// The pod structure:
// - Main container handles primary file access (default: busybox HTTP server)
// - SSH sidecar (optional): provides SSH/SFTP/SCP/rsync access
// - FileBrowser sidecar (optional): provides HTTPS web-based file browser
//
// Mount modes:
// 1. Kubernetes-managed (UseInternalMounts=false, default):
//   - PVCs mounted by kubelet at /restores/<date>/<backup-name>/<pvc-name>/
//   - Example: /restores/2025-10-24/test-vm-backup-20250115/test-vm-disk-1/
//   - Simple and secure, no privileges required
//   - Organized by date for easy browsing
//
// 2. Internal mount(2) (UseInternalMounts=true):
//   - Main container performs mount(2) syscalls internally
//   - Sidecars see mounts via mount propagation (Bidirectional → HostToContainer)
//   - Requires privileged security context or SYS_ADMIN capability
//   - Useful when main container needs mount flexibility
//   - SharedMountPath (e.g., /mnt/restore) - main container mounts here
//   - Sidecars see the same SharedMountPath via propagation
func buildFileServerPodSpec(config FileServerPodConfig) (*corev1.Pod, error) {
	// Validate required config
	if config.PodName == "" {
		return nil, fmt.Errorf("pod name is required")
	}
	if config.PodNamespace == "" {
		return nil, fmt.Errorf("pod namespace is required")
	}
	if config.VMFRName == "" {
		return nil, fmt.Errorf("VMFR name is required for labels")
	}
	if len(config.PVCMounts) == 0 {
		return nil, fmt.Errorf("at least one PVC mount is required")
	}

	// Set defaults for internal mount mode
	if config.UseInternalMounts && config.SharedMountPath == "" {
		config.SharedMountPath = "/mnt/restore"
	}

	// Build PVC volumes, mounts, and devices (always added to pod, even in internal mount mode)
	// Returns volumeMounts for Filesystem PVCs and volumeDevices for Block mode PVCs
	pvcVolumes, pvcVolumeMounts, pvcVolumeDevices := buildPVCVolumesAndMounts(config.PVCMounts)

	// Collect all volumes: PVC volumes + sidecar volumes (SSH, FileBrowser) + utility volumes
	allVolumes := make([]corev1.Volume, 0, len(pvcVolumes)+10)
	allVolumes = append(allVolumes, pvcVolumes...)

	// Add shared mount volume for internal mount mode
	var sharedMountVolumeName string
	if config.UseInternalMounts {
		sharedMountVolumeName = "shared-mounts"
		allVolumes = append(allVolumes, corev1.Volume{
			Name: sharedMountVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	// Note: /restores directory structure is created by Kubernetes mounts
	// No emptyDir volumes needed for the /restores hierarchy

	// Add /dev/fuse hostPath volume (required for guestmount FUSE filesystem)
	allVolumes = append(allVolumes, corev1.Volume{
		Name: "fuse-device",
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/dev/fuse",
				Type: ptr.To(corev1.HostPathCharDev),
			},
		},
	})

	// Add /dev/kvm hostPath volume (required for KVM hardware acceleration)
	allVolumes = append(allVolumes, corev1.Volume{
		Name: "kvm-device",
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/dev/kvm",
				Type: ptr.To(corev1.HostPathCharDev),
			},
		},
	})

	// Add emptyDir for filesystem mount points (where guestmount will mount filesystems)
	allVolumes = append(allVolumes, corev1.Volume{
		Name: "filesystems",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	// Collect init containers and sidecar containers
	var allInitContainers []corev1.Container
	var sidecarContainers []corev1.Container

	// Add SSH sidecar if enabled
	if config.SSHAccess != nil {
		sshContainer, sshVolumes := buildSSHSidecar(
			config.SSHAccess,
			config.UseInternalMounts,
			pvcVolumeMounts,
		)
		sidecarContainers = append(sidecarContainers, sshContainer)
		allVolumes = append(allVolumes, sshVolumes...)
	}

	// Add FileBrowser sidecar if enabled
	if config.FileBrowserAccess != nil {
		fileBrowserContainer, fileBrowserVolumes := buildFileBrowserSidecar(
			config.FileBrowserAccess,
			config.UseInternalMounts,
			pvcVolumeMounts,
		)
		sidecarContainers = append(sidecarContainers, fileBrowserContainer)
		allVolumes = append(allVolumes, fileBrowserVolumes...)
	}

	// Build or use default main container
	var mainContainer corev1.Container
	if config.MainContainer != nil {
		mainContainer = *config.MainContainer
	} else {
		// Default main container: simple busybox HTTP server
		mainContainer = buildDefaultMainContainer(pvcVolumeMounts)
	}

	// Configure main container based on mount mode
	if config.UseInternalMounts {
		// Internal mount mode: main container performs mount(2) syscalls
		// Add shared mount volume with Bidirectional propagation
		mountPropagation := corev1.MountPropagationBidirectional
		mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, corev1.VolumeMount{
			Name:             sharedMountVolumeName,
			MountPath:        config.SharedMountPath,
			MountPropagation: &mountPropagation,
		})

		// Also mount PVC volumes so main container can access them for internal mounting
		// These are the "source" volumes that main will mount into SharedMountPath
		// Add both volumeMounts (for Filesystem PVCs) and volumeDevices (for Block PVCs)
		mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, pvcVolumeMounts...)
		mainContainer.VolumeDevices = append(mainContainer.VolumeDevices, pvcVolumeDevices...)

		// Add /dev/fuse and /dev/kvm devices (required for guestmount with Block mode PVCs)
		mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, corev1.VolumeMount{
			Name:      "fuse-device",
			MountPath: "/dev/fuse",
		})
		mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, corev1.VolumeMount{
			Name:      "kvm-device",
			MountPath: "/dev/kvm",
		})

		// For Block mode PVCs: Add filesystems emptyDir at /restores/ for FUSE mounts
		// For Filesystem mode PVCs: Don't mount filesystems emptyDir (PVCs are already mounted at /restores/*)
		if len(pvcVolumeDevices) > 0 {
			// Block mode: Mount filesystems emptyDir at /restores/ where guestmount will create FUSE mounts
			// Structure: /restores/<date>/<backup>/<pvc>/
			// CRITICAL: Use Bidirectional propagation so sidecars can see the FUSE mounts
			mountPropagation := corev1.MountPropagationBidirectional
			mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, corev1.VolumeMount{
				Name:             "filesystems",
				MountPath:        "/restores",
				MountPropagation: &mountPropagation,
			})
		}

		// Apply security context for mount(2) privileges
		if config.MainContainerSecurityContext != nil {
			mainContainer.SecurityContext = config.MainContainerSecurityContext
		} else {
			// Default to privileged for mount(2) capability
			mainContainer.SecurityContext = &corev1.SecurityContext{
				Privileged: ptr.To(true),
			}
		}
	} else {
		// Kubernetes-managed mode: kubelet handles PVC mounting
		// Inject PVC volume mounts (Filesystem mode) and volume devices (Block mode) into the main container
		mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, pvcVolumeMounts...)
		mainContainer.VolumeDevices = append(mainContainer.VolumeDevices, pvcVolumeDevices...)

		// Add /dev/fuse and /dev/kvm devices (required for guestmount with Block mode PVCs)
		mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, corev1.VolumeMount{
			Name:      "fuse-device",
			MountPath: "/dev/fuse",
		})
		mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, corev1.VolumeMount{
			Name:      "kvm-device",
			MountPath: "/dev/kvm",
		})

		// For Block mode PVCs: Add filesystems emptyDir at /restores/ for FUSE mounts
		// For Filesystem mode PVCs: Don't mount filesystems emptyDir (PVCs are already mounted at /restores/*)
		if len(pvcVolumeDevices) > 0 {
			// Block mode: Mount filesystems emptyDir at /restores/ where guestmount will create FUSE mounts
			// Structure: /restores/<date>/<backup>/<pvc>/
			mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, corev1.VolumeMount{
				Name:      "filesystems",
				MountPath: "/restores",
			})
		}
	}

	// Combine main container with sidecars
	containers := make([]corev1.Container, 0, 1+len(sidecarContainers))
	containers = append(containers, mainContainer)
	containers = append(containers, sidecarContainers...)

	// Build labels and annotations
	labels := buildPodLabels(config)
	annotations := buildPodAnnotations(config)

	// Build pod spec
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        config.PodName,
			Namespace:   config.PodNamespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			// Use dedicated ServiceAccount for file server pods
			// This ServiceAccount is created by the controller in ensureRestoreNamespace
			// and is bound to the privileged SCC to allow hostPath volumes and privileged containers
			ServiceAccountName: "vmfr-file-server",
			InitContainers:     allInitContainers,
			Containers:         containers,
			Volumes:            allVolumes,
			RestartPolicy:      corev1.RestartPolicyAlways,
			// Share process namespace when SSH is enabled
			// This allows containers to work together and enables sshd to manage processes
			ShareProcessNamespace: ptr.To(config.SSHAccess != nil),
			// Pod-level security context
			// Required for accessing VM disk images with qemu user/group permissions
			SecurityContext: &corev1.PodSecurityContext{
				// fsGroup: 107 (qemu group in OpenShift Virtualization)
				// This ensures volumes are accessible by the qemu user/group
				FSGroup: ptr.To(int64(107)),
				// supplementalGroups: [107] - Grants qemu group membership to all containers
				SupplementalGroups: []int64{107},
				// SELinux: Use spc_t (Super Privileged Container) type
				// This disables SELinux enforcement for volume access, similar to Velero's spcNoRelabeling option
				// See: https://velero.io/docs/main/data-movement-backup-pvc-configuration/
				SELinuxOptions: &corev1.SELinuxOptions{
					Type: "spc_t",
				},
			},
		},
	}

	// NOTE: No owner references added to the Pod
	// Kubernetes does not allow cross-namespace owner references (VMFR is in OADP namespace,
	// Pod is in temp restore namespace). Instead, cleanup is handled by:
	// 1. Temp namespace has owner reference to VMFR
	// 2. When VMFR is deleted, namespace is deleted
	// 3. Namespace deletion cascades to all resources including this Pod
	// Labels (VMFROriginUUIDLabel) are used for tracking ownership instead

	return pod, nil
}

// buildPVCVolumesAndMounts creates volumes, volume mounts, and volume devices for PVCs.
// Handles both Block and Filesystem mode PVCs correctly:
// - Block mode PVCs: VM disks stored as raw block devices, exposed via volumeDevices at /dev/pvc-{uid}
// - Filesystem mode PVCs: Mounted at /restores/<date>/<backup-name>/<pvc-name>/
//
// Path structure: /restores/<YYYY-MM-DD>/<backup-name>/<pvc-name>/
// Example: /restores/2025-10-24/test-vm-backup-20250115/test-vm-disk-1/
//
// Returns:
// - volumes: Kubernetes Volume objects referencing the PVCs
// - volumeMounts: Mount points for Filesystem mode PVCs
// - volumeDevices: Device mappings for Block mode PVCs
func buildPVCVolumesAndMounts(pvcMounts []PVCMountInfo) ([]corev1.Volume, []corev1.VolumeMount, []corev1.VolumeDevice) {
	volumes := make([]corev1.Volume, 0, len(pvcMounts))
	volumeMounts := make([]corev1.VolumeMount, 0, len(pvcMounts))
	volumeDevices := make([]corev1.VolumeDevice, 0, len(pvcMounts))

	for _, pvcMount := range pvcMounts {
		// Create unique volume name for this PVC
		volumeName := fmt.Sprintf("pvc-%s", pvcMount.PVCUID)

		// Determine which PVC name to use: RestoredPVCName if available, otherwise PVCName
		// RestoredPVCName is populated when kubevirt-velero-plugin changes the PVC name during restore
		pvcClaimName := pvcMount.RestoredPVCName
		if pvcClaimName == "" {
			pvcClaimName = pvcMount.PVCName
		}

		// Add volume referencing the restored PVC (always added regardless of mode)
		volumes = append(volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcClaimName,
					ReadOnly:  false, // libguestfs needs write access for internal operations
				},
			},
		})

		// Determine how to expose the PVC based on its volumeMode
		// Default to Filesystem if volumeMode is nil (Kubernetes default)
		isBlockMode := pvcMount.VolumeMode != nil && *pvcMount.VolumeMode == corev1.PersistentVolumeBlock

		if isBlockMode {
			// Block mode: Expose as raw block device at /dev/pvc-{uid}
			// libguestfs/QEMU can directly access the block device containing the VM disk image
			devicePath := fmt.Sprintf("/dev/pvc-%s", pvcMount.PVCUID)
			volumeDevices = append(volumeDevices, corev1.VolumeDevice{
				Name:       volumeName,
				DevicePath: devicePath,
			})
		} else {
			// Filesystem mode: Mount at /restores/<date>/<backup-name>/<pvc-name>/
			// Organized by date for easy browsing, with backup name and PVC name for clarity
			// Format: /restores/2025-10-24/test-vm-backup-20250115/test-vm-disk-1/
			date := formatBackupDateForPath(pvcMount.BackupTimestamp)
			mountPath := fmt.Sprintf("/restores/%s/%s/%s", date, pvcMount.BackupName, pvcMount.PVCName)
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: mountPath,
				ReadOnly:  false, // libguestfs needs write access
			})
		}
	}

	return volumes, volumeMounts, volumeDevices
}

// formatBackupDateForPath formats a backup timestamp as YYYY-MM-DD for directory structure
// Returns "unknown-date" if timestamp is nil
func formatBackupDateForPath(timestamp *metav1.Time) string {
	if timestamp == nil {
		return "unknown-date"
	}
	return timestamp.Time.Format("2006-01-02")
}

// buildDefaultMainContainer creates a default busybox HTTP server container
func buildDefaultMainContainer(pvcVolumeMounts []corev1.VolumeMount) corev1.Container {
	volumeMounts := make([]corev1.VolumeMount, len(pvcVolumeMounts))
	copy(volumeMounts, pvcVolumeMounts)

	return corev1.Container{
		Name:  "file-server",
		Image: "busybox:latest",
		Command: []string{
			"/bin/sh",
			"-c",
			// Simple HTTP server serving from root (/) which includes our mount points
			fmt.Sprintf("httpd -f -p %d -h /", constant.DefaultFileServerPort),
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: constant.DefaultFileServerPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		VolumeMounts: volumeMounts,
	}
}

// buildVMFileServerMainContainer creates a VM file server container for mounting VM disk images
// This container uses libguestfs/QEMU to mount VM disks and provide file-level access
// Based on CONTROLLER_INTEGRATION.md from Issue #6/#7
func buildVMFileServerMainContainer(pvcMounts []PVCMountInfo) corev1.Container {
	// Build BACKUP_PVC_MAP JSON for the file server script
	backupPVCMap := buildBackupPVCMapJSON(pvcMounts)

	return corev1.Container{
		Name:  "vm-file-server",
		Image: constant.VMFileServerImage,
		// Run detect-and-mount.sh to automatically mount all disk images
		Command: []string{"/usr/local/bin/detect-and-mount.sh"},

		// Environment variables
		Env: []corev1.EnvVar{
			{
				Name:  "HOME",
				Value: "/tmp", // Required for libguestfs cache
			},
			{
				Name:  "BACKUP_PVC_MAP",
				Value: backupPVCMap, // JSON mapping of backups to PVCs for the file server script
			},
		},

		// Lifecycle hooks
		// PreStop: Kubernetes-native cleanup hook (primary mechanism)
		// This is guaranteed to run BEFORE the container receives SIGTERM
		// Works in conjunction with trap handler in detect-and-mount.sh (secondary mechanism)
		// See issue #44: https://github.com/migtools/oadp-vm-file-restore/issues/44
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"/bin/bash", "-c", vmFileServerPreStopScript},
				},
			},
		},

		// Container-level security context
		// CRITICAL: Privileged mode required for /dev/kvm access with SELinux
		SecurityContext: &corev1.SecurityContext{
			Privileged: ptr.To(true),       // Required for /dev/kvm and /dev/fuse access
			RunAsUser:  ptr.To(int64(107)), // qemu user
			RunAsGroup: ptr.To(int64(107)), // qemu group
		},

		// Resource limits
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("512Mi"),
				corev1.ResourceCPU:    resource.MustParse("250m"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("2Gi"),   // libguestfs can use significant memory
				corev1.ResourceCPU:    resource.MustParse("1000m"), // KVM uses CPU during mount
			},
		},
	}
}

// buildBackupPVCMapJSON builds a JSON string mapping backup names to PVC information
// This is used by the file server script to understand the backup/PVC structure
//
// Format: {"backup-name": [{"name": "pvc-name", "path": "/dev/pvc-{uid}", "timestamp": "2025-10-23T18:40:00Z"}]}
//
// The "path" field refers to the device path for block mode PVCs, used by libguestfs/QEMU
// PVCs are actually mounted by Kubernetes at: /restores/<date>/<backup-name>/<pvc-name>/
//
// Example output:
//
//	{
//	  "test-vm-backup-20250115": [
//	    {"name": "test-vm-disk-1", "path": "/dev/pvc-5685cd9a-56b4-4482-9236-17b7cc4b0dff", "timestamp": "2025-10-23T18:40:00Z"}
//	  ]
//	}
func buildBackupPVCMapJSON(pvcMounts []PVCMountInfo) string {
	// Group PVCs by backup name
	backupMap := make(map[string][]map[string]string)

	for _, pvcMount := range pvcMounts {
		backupName := pvcMount.BackupName

		// Build device path for block mode PVCs (used by libguestfs/QEMU)
		// Note: Filesystem mode PVCs are mounted by Kubernetes at /restores/<date>/<backup>/<pvc-name>/
		devicePath := fmt.Sprintf("/dev/pvc-%s", pvcMount.PVCUID)

		// Format timestamp as ISO8601
		timestamp := ""
		if pvcMount.BackupTimestamp != nil {
			timestamp = pvcMount.BackupTimestamp.Format("2006-01-02T15:04:05Z")
		}

		// Create PVC entry
		pvcEntry := map[string]string{
			"name":      pvcMount.PVCName,
			"path":      devicePath,
			"timestamp": timestamp,
		}

		backupMap[backupName] = append(backupMap[backupName], pvcEntry)
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(backupMap)
	if err != nil {
		// If marshaling fails, return empty JSON object (script will skip dual-path creation)
		return "{}"
	}

	return string(jsonBytes)
}

// buildPodLabels creates labels for the pod
func buildPodLabels(config FileServerPodConfig) map[string]string {
	labels := map[string]string{
		constant.VMFROriginUUIDLabel: config.VMFRUID,
		constant.ManagedByLabel:      constant.ManagedByLabelValue,
		"app":                        "vmfr-file-server",
	}

	// Merge with additional labels
	for k, v := range config.PodLabels {
		labels[k] = v
	}

	return labels
}

// buildPodAnnotations creates annotations for the pod
func buildPodAnnotations(config FileServerPodConfig) map[string]string {
	annotations := map[string]string{
		constant.VMFROriginNameAnnotation:      config.VMFRName,
		constant.VMFROriginNamespaceAnnotation: config.VMFRNamespace,
		"oadp.openshift.io/vmfr-pvc-count":     fmt.Sprintf("%d", len(config.PVCMounts)),
	}

	// Add enabled access methods
	var accessMethods []string
	if config.SSHAccess != nil {
		accessMethods = append(accessMethods, "ssh")
	}
	if config.FileBrowserAccess != nil {
		accessMethods = append(accessMethods, "filebrowser")
	}
	if len(accessMethods) > 0 {
		annotations["oadp.openshift.io/vmfr-access-methods"] = strings.Join(accessMethods, ",")
	}

	// Merge with additional annotations
	for k, v := range config.PodAnnotations {
		annotations[k] = v
	}

	return annotations
}

// buildFileServerService builds a Service spec for exposing file server pod ports
// Supports multiple ports for multi-container pods (main + sidecars)
func buildFileServerService(config ServiceConfig) (*corev1.Service, error) {
	// Validate required parameters
	if config.ServiceName == "" {
		return nil, fmt.Errorf("service name is required")
	}
	if config.ServiceNamespace == "" {
		return nil, fmt.Errorf("service namespace is required")
	}
	if config.VMFRName == "" {
		return nil, fmt.Errorf("VMFR name is required for labels")
	}
	if len(config.Ports) == 0 {
		return nil, fmt.Errorf("at least one port is required")
	}

	// Default service type
	serviceType := config.ServiceType
	if serviceType == "" {
		serviceType = corev1.ServiceTypeClusterIP
	}

	// Default selector
	selector := config.Selector
	if len(selector) == 0 {
		selector = map[string]string{
			constant.VMFROriginUUIDLabel: config.VMFRUID,
			"app":                        "vmfr-file-server",
		}
	}

	// Build default labels
	defaultLabels := map[string]string{
		constant.VMFROriginUUIDLabel: config.VMFRUID,
		constant.ManagedByLabel:      constant.ManagedByLabelValue,
		"app":                        "vmfr-file-server",
	}

	// Merge with additional labels
	for k, v := range config.ServiceLabels {
		defaultLabels[k] = v
	}

	// Build annotations
	annotations := map[string]string{
		constant.VMFROriginNameAnnotation:      config.VMFRName,
		constant.VMFROriginNamespaceAnnotation: config.VMFRNamespace,
	}
	// Merge with additional annotations
	for k, v := range config.ServiceAnnotations {
		annotations[k] = v
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        config.ServiceName,
			Namespace:   config.ServiceNamespace,
			Labels:      defaultLabels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports:    config.Ports,
			Type:     serviceType,
		},
	}

	// IMPORTANT: Do NOT add cross-namespace owner references!
	// Services are created in the same namespace as the Deployment/Pod (the restore namespace),
	// which may be different from the VMFR namespace. Cross-namespace owner references
	// are rejected by the Kubernetes garbage collector.
	//
	// Instead, we use labels (already added above) to track ownership,
	// and the controller's finalizer logic will clean up the Service on VMFR deletion.

	return service, nil
}

// buildFileServerRoute builds an OpenShift Route for exposing file server services externally
// Routes enable external access to SSH or FileBrowser services running in the cluster
func buildFileServerRoute(config RouteConfig) (*routev1.Route, error) {
	// Validate required parameters
	if config.RouteName == "" {
		return nil, fmt.Errorf("route name is required")
	}
	if config.RouteNamespace == "" {
		return nil, fmt.Errorf("route namespace is required")
	}
	if config.VMFRName == "" {
		return nil, fmt.Errorf("VMFR name is required for labels")
	}
	if config.ServiceName == "" {
		return nil, fmt.Errorf("service name is required")
	}
	if config.TargetPort == "" {
		return nil, fmt.Errorf("target port is required")
	}

	// Build default labels
	defaultLabels := map[string]string{
		constant.VMFROriginUUIDLabel: config.VMFRUID,
		constant.ManagedByLabel:      constant.ManagedByLabelValue,
		"app":                        "vmfr-file-server",
	}

	// Merge with additional labels
	for k, v := range config.RouteLabels {
		defaultLabels[k] = v
	}

	// Build annotations
	annotations := map[string]string{
		constant.VMFROriginNameAnnotation:      config.VMFRName,
		constant.VMFROriginNamespaceAnnotation: config.VMFRNamespace,
	}
	// Merge with additional annotations
	for k, v := range config.RouteAnnotations {
		annotations[k] = v
	}

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:        config.RouteName,
			Namespace:   config.RouteNamespace,
			Labels:      defaultLabels,
			Annotations: annotations,
		},
		Spec: routev1.RouteSpec{
			Subdomain: config.Subdomain,
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: config.ServiceName,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString(config.TargetPort),
			},
			TLS: &routev1.TLSConfig{
				Termination:                   config.TLSTermination,
				InsecureEdgeTerminationPolicy: config.InsecureEdgeTerminationPolicy,
			},
			WildcardPolicy: routev1.WildcardPolicyNone,
		},
	}

	// IMPORTANT: Do NOT add cross-namespace owner references!
	// Routes are created in the same namespace as the Service (the restore namespace),
	// which may be different from the VMFR namespace. Cross-namespace owner references
	// are rejected by the Kubernetes garbage collector.
	//
	// Instead, we use labels (already added above) to track ownership,
	// and the controller's finalizer logic will clean up the Route on VMFR deletion.

	return route, nil
}

// extractPVCMountsFromVMFR extracts PVC mount information from VMFR status
// Returns all successfully completed restores (including multiple backup versions of the same PVC)
func extractPVCMountsFromVMFR(vmfr *oadpv1alpha1.VirtualMachineFileRestore) []PVCMountInfo {
	var pvcMounts []PVCMountInfo

	for _, pvcRestore := range vmfr.Status.PVCRestores {
		// Skip synthetic entries (backup-level failures)
		if pvcRestore.PVCName == constant.BackupLevelFailurePVCName {
			continue
		}

		// Mount ALL successfully completed restores for this PVC
		// This allows users to access different backup versions of the same PVC
		// Restores are already sorted by timestamp (newest first) in processDiscoveryResults
		for _, restoreInfo := range pvcRestore.Restores {
			// Include both Completed and Finalizing phases (PVCs are ready in both cases)
			if (restoreInfo.Phase == "Completed" || restoreInfo.Phase == "Finalizing") && restoreInfo.State == string(oadptypes.BackupDiscoveryStateAvailable) {
				pvcMounts = append(pvcMounts, PVCMountInfo{
					PVCName:           pvcRestore.PVCName,
					PVCNamespace:      pvcRestore.PVCNamespace,
					PVCUID:            pvcRestore.PVCUID,
					BackupName:        restoreInfo.VeleroBackupName,
					BackupTimestamp:   restoreInfo.Timestamp,
					VeleroRestoreName: restoreInfo.VeleroRestoreName,
				})
				// Continue to next restore - mount all backup versions
			}
		}
	}

	return pvcMounts
}

// gatherServicePorts collects service ports based on enabled access methods
func gatherServicePorts(sshConfig *SSHAccessConfig, fileBrowserConfig *FileBrowserAccessConfig) []corev1.ServicePort {
	var ports []corev1.ServicePort

	// Add default HTTP port (always enabled for basic access)
	ports = append(ports, buildDefaultHTTPServicePort())

	// Add SSH port if enabled
	if sshConfig != nil {
		ports = append(ports, corev1.ServicePort{
			Name:       "ssh",
			Port:       sshConfig.Port,
			TargetPort: intstr.FromInt(int(sshConfig.Port)),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	// Add FileBrowser HTTPS port if enabled
	if fileBrowserConfig != nil {
		ports = append(ports, corev1.ServicePort{
			Name:       "https",
			Port:       fileBrowserConfig.Port,
			TargetPort: intstr.FromInt(int(fileBrowserConfig.Port)),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	return ports
}

// buildDefaultHTTPServicePort creates a default service port for HTTP access
func buildDefaultHTTPServicePort() corev1.ServicePort {
	return corev1.ServicePort{
		Name:       "http",
		Port:       constant.DefaultFileServerPort,
		TargetPort: intstr.FromInt(int(constant.DefaultFileServerPort)),
		Protocol:   corev1.ProtocolTCP,
	}
}

// buildSSHSidecar creates an SSH sidecar container with associated volumes and init containers.
//
// The SSH server provides read-only SFTP/SCP/rsync access to restored PVC data.
// Uses custom OADP SSH image with chroot environment for enhanced security.
//
// Implementation details:
// - Uses constant.SSHSidecarImage (quay.io/konveyor/oadp-vmfr-access-sshd:latest)
// - Runs as root for authentication and chroot (security hardened with capabilities)
// - Read-only root filesystem with emptyDir volumes for runtime directories
// - Supports public key authentication only
// - Chroot environment restricts access to /oadp directory
func buildSSHSidecar(
	config *SSHAccessConfig,
	useInternalMounts bool,
	pvcVolumeMounts []corev1.VolumeMount,
) (corev1.Container, []corev1.Volume) {

	// SSH server container using custom OADP SSH image
	// This image provides a secure, chroot-based SSH server with read-only access
	container := corev1.Container{
		Name:  "sshd",
		Image: constant.SSHSidecarImage,
		Ports: []corev1.ContainerPort{
			{
				Name:          "ssh",
				ContainerPort: config.Port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		// SecurityContext for SSH server
		// SSHD needs to run as root for authentication and chroot
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr.To(int64(0)),
			ReadOnlyRootFilesystem:   ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					"AUDIT_WRITE",     // Required for PAM audit logging (RHEL requirement)
					"CHOWN",           // Required to set ownership of SSH directory at startup
					"DAC_READ_SEARCH", // Allows reading files regardless of ownership/permissions
					"MKNOD",           // Required to create device nodes in /oadp/dev
					"SETGID",          // Required for sshd to drop privileges
					"SETUID",          // Required for sshd to drop privileges
					"SYS_CHROOT",      // Required for ChrootDirectory
				},
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}

	// Add volumes for read-only root filesystem
	volumes := make([]corev1.Volume, 0, 6)

	// EmptyDir volumes for runtime directories (required for read-only root filesystem)
	sshEtcVolume := corev1.Volume{
		Name: "ssh-etc",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium:    corev1.StorageMediumMemory,
				SizeLimit: resource.NewQuantity(10*1024*1024, resource.BinarySI), // 10Mi
			},
		},
	}
	sshRunVolume := corev1.Volume{
		Name: "ssh-run",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium:    corev1.StorageMediumMemory,
				SizeLimit: resource.NewQuantity(10*1024*1024, resource.BinarySI), // 10Mi
			},
		},
	}
	sshTmpVolume := corev1.Volume{
		Name: "ssh-tmp",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium:    corev1.StorageMediumMemory,
				SizeLimit: resource.NewQuantity(50*1024*1024, resource.BinarySI), // 50Mi
			},
		},
	}
	sshDevVolume := corev1.Volume{
		Name: "ssh-dev",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium:    corev1.StorageMediumMemory,
				SizeLimit: resource.NewQuantity(1*1024*1024, resource.BinarySI), // 1Mi
			},
		},
	}
	sshOadpEtcVolume := corev1.Volume{
		Name: "ssh-oadp-etc",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium:    corev1.StorageMediumMemory,
				SizeLimit: resource.NewQuantity(1*1024*1024, resource.BinarySI), // 1Mi
			},
		},
	}

	volumes = append(volumes, sshEtcVolume, sshRunVolume, sshTmpVolume, sshDevVolume, sshOadpEtcVolume)

	// Mount runtime directories
	container.VolumeMounts = append(container.VolumeMounts,
		corev1.VolumeMount{
			Name:      "ssh-etc",
			MountPath: "/etc",
		},
		corev1.VolumeMount{
			Name:      "ssh-run",
			MountPath: "/run",
		},
		corev1.VolumeMount{
			Name:      "ssh-tmp",
			MountPath: "/tmp",
		},
		corev1.VolumeMount{
			Name:      "ssh-dev",
			MountPath: "/oadp/dev",
		},
		corev1.VolumeMount{
			Name:      "ssh-oadp-etc",
			MountPath: "/oadp/etc",
		},
	)

	// Add Secret volume for SSH credentials
	// The controller ensures the Secret exists before pod creation (via ensureCredentials)
	credentialsVolume := corev1.Volume{
		Name: "ssh-secret",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  config.CredentialsSecretName,
				DefaultMode: ptr.To(int32(0444)), // Readable by all (needed for oadp user to read authorized_keys)
			},
		},
	}
	volumes = append(volumes, credentialsVolume)

	// Mount secret at /ssh-config for entrypoint script
	container.VolumeMounts = append(container.VolumeMounts,
		corev1.VolumeMount{
			Name:      "ssh-secret",
			MountPath: "/ssh-config",
			ReadOnly:  true,
		},
		// Also mount authorized_keys directly from secret to avoid permission issues
		corev1.VolumeMount{
			Name:      "ssh-secret",
			MountPath: "/oadp/.ssh/authorized_keys",
			SubPath:   "authorized_keys",
			ReadOnly:  true,
		},
	)

	// Configure volume mounts for restored data based on PVC mode
	// SSH chroot is at /oadp/, so we mount at /oadp/restores/
	// Users (chrooted to /oadp/) see files at /restores/<date>/<backup>/<pvc>/
	if useInternalMounts {
		// Internal mount mode: use filesystems volume with propagation
		// The vm-file-server container mounts filesystems at /restores and creates FUSE mounts there
		// Sidecars need mount propagation to see those FUSE mounts
		mountPropagation := corev1.MountPropagationHostToContainer
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:             "filesystems",
			MountPath:        "/oadp/restores",
			ReadOnly:         true,
			MountPropagation: &mountPropagation,
		})
	} else {
		// Kubernetes-managed mode: different approach for Filesystem vs Block PVCs
		if len(pvcVolumeMounts) > 0 {
			// Filesystem mode PVCs: Mount PVCs directly at /oadp/restores/<date>/<backup>/<pvc>/
			// Adjust paths from /restores/* to /oadp/restores/* for chroot environment
			for _, mount := range pvcVolumeMounts {
				adjustedMount := mount
				adjustedMount.MountPath = "/oadp" + mount.MountPath
				container.VolumeMounts = append(container.VolumeMounts, adjustedMount)
			}
		} else {
			// Block mode PVCs: Mount the shared filesystems emptyDir at /oadp/restores/
			// The vm-file-server container creates FUSE mounts inside this shared volume
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name:      "filesystems",
				MountPath: "/oadp/restores",
				ReadOnly:  true,
			})
		}
	}

	// No init containers needed - the custom image handles all setup
	return container, volumes
}

// buildFileBrowserSidecar creates a FileBrowser sidecar container with associated volumes and init containers.
//
// The FileBrowser server provides web-based file browsing access to restored PVC data.
// Uses custom OADP FileBrowser image with TLS support and read-only permissions.
//
// Implementation details:
// - Uses constant.FileBrowserSidecarImage (quay.io/konveyor/oadp-vmfr-access-filebrowser:latest)
// - Runs as non-root (UID 1000) with read-only root filesystem
// - Serves HTTPS on port 8443 with TLS certificates from OpenShift service CA
// - Configured with authentication from Secret credentials
// - Read-only file browser with download capabilities
// - Custom branding for OADP VM File Restore
func buildFileBrowserSidecar(
	config *FileBrowserAccessConfig,
	useInternalMounts bool,
	pvcVolumeMounts []corev1.VolumeMount,
) (corev1.Container, []corev1.Volume) {

	// FileBrowser always serves from /restores/
	// For Filesystem mode PVCs: /restores/<date>/<backup>/<pvc>/ (Kubernetes mounts)
	// For Block mode PVCs: /restores/<date>/<backup>/<pvc>/ (guestmount FUSE mounts in shared emptyDir)
	fbRoot := "/restores"

	// FileBrowser container using custom OADP FileBrowser image
	// This image provides TLS support and read-only configuration
	container := corev1.Container{
		Name:  "filebrowser",
		Image: constant.FileBrowserSidecarImage,
		// Command to initialize FileBrowser with credentials from secret
		// The initialization script is defined in sidecar_scripts.go
		Command: []string{"/bin/bash", "-c"},
		Args:    []string{fileBrowserInitScript},
		Ports: []corev1.ContainerPort{
			{
				Name:          "https",
				ContainerPort: config.Port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		// Environment variables for FileBrowser configuration
		Env: []corev1.EnvVar{
			{
				Name:  "FB_ROOT",
				Value: fbRoot, // Serve from appropriate directory based on PVC mode
			},
			{
				Name:  "FB_PORT",
				Value: fmt.Sprintf("%d", config.Port),
			},
			{
				Name:  "FB_ADDRESS",
				Value: "0.0.0.0",
			},
			{
				Name:  "FB_DATABASE",
				Value: "/database/filebrowser.db",
			},
			{
				Name:  "FB_BRANDING_NAME",
				Value: "OADP VM File Restore Browser",
			},
		},
		// SecurityContext for FileBrowser
		// Runs as non-root with minimal permissions
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr.To(int64(1000)),
			RunAsGroup:               ptr.To(int64(1000)),
			RunAsNonRoot:             ptr.To(true),
			ReadOnlyRootFilesystem:   ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}

	// Add volumes for FileBrowser
	volumes := make([]corev1.Volume, 0, 4)

	// Database volume for FileBrowser state (in-memory)
	databaseVolume := corev1.Volume{
		Name: "database",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium:    corev1.StorageMediumMemory,
				SizeLimit: resource.NewQuantity(10*1024*1024, resource.BinarySI), // 10Mi
			},
		},
	}
	volumes = append(volumes, databaseVolume)

	// Tmp volume for read-only root filesystem
	tmpVolume := corev1.Volume{
		Name: "tmp",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium:    corev1.StorageMediumMemory,
				SizeLimit: resource.NewQuantity(10*1024*1024, resource.BinarySI), // 10Mi
			},
		},
	}
	volumes = append(volumes, tmpVolume)

	// TLS certificates from OpenShift service CA
	// Note: This secret is auto-generated by OpenShift when the service has the annotation:
	//   service.beta.openshift.io/serving-cert-secret-name: filebrowser-tls
	tlsVolume := corev1.Volume{
		Name: "filebrowser-tls",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  "filebrowser-tls",
				DefaultMode: ptr.To(int32(0400)),
			},
		},
	}
	volumes = append(volumes, tlsVolume)

	// Add credentials Secret volume
	credentialsVolume := corev1.Volume{
		Name: "filebrowser-credentials",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  config.CredentialsSecretName,
				DefaultMode: ptr.To(int32(0444)), // Readable by all (needed for file browser container user)
			},
		},
	}
	volumes = append(volumes, credentialsVolume)

	// Mount volumes
	container.VolumeMounts = append(container.VolumeMounts,
		corev1.VolumeMount{
			Name:      "database",
			MountPath: "/database",
		},
		corev1.VolumeMount{
			Name:      "tmp",
			MountPath: "/tmp",
		},
		corev1.VolumeMount{
			Name:      "filebrowser-tls",
			MountPath: "/etc/filebrowser-tls",
			ReadOnly:  true,
		},
		corev1.VolumeMount{
			Name:      "filebrowser-credentials",
			MountPath: "/etc/filebrowser-credentials",
			ReadOnly:  true,
		},
	)

	// Configure volume mounts for restored data based on PVC mode
	if useInternalMounts {
		// Internal mount mode: use filesystems volume with propagation
		// The vm-file-server container mounts filesystems at /restores and creates FUSE mounts there
		// Sidecars need mount propagation to see those FUSE mounts
		mountPropagation := corev1.MountPropagationHostToContainer
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:             "filesystems",
			MountPath:        "/restores",
			ReadOnly:         true,
			MountPropagation: &mountPropagation,
		})
	} else {
		// Kubernetes-managed mode: different approach for Filesystem vs Block PVCs
		if len(pvcVolumeMounts) > 0 {
			// Filesystem mode PVCs: Mount PVCs directly at /restores/<date>/<backup>/<pvc>/
			// Each PVC is mounted by Kubernetes at its organized path
			container.VolumeMounts = append(container.VolumeMounts, pvcVolumeMounts...)
		} else {
			// Block mode PVCs: Mount the shared filesystems emptyDir at /restores/
			// The vm-file-server container creates FUSE mounts inside this shared volume
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name:      "filesystems",
				MountPath: "/restores",
				ReadOnly:  true,
			})
		}
	}

	// No init containers needed - the custom image handles all setup
	return container, volumes
}

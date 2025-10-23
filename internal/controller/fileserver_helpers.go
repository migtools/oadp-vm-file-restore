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
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
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
	// - /restores_by_name/<backup-name>/<pvc-uid>
	// - /restores_by_date/<date>/<pvc-name> (via symlinks)
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

	// EnableDualPathAccess creates symlinks for dual-path PVC access
	// If true, an init container creates symlinks:
	// - /restores_by_name/<backup>/<uid> (actual mount)
	// - /restores_by_date/<date>/<name> (symlink)
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

// buildFileServerPodSpec builds a Pod spec for serving files from restored PVCs.
//
// The pod structure:
// - Main container handles primary file access (default: busybox HTTP server)
// - SSH sidecar (optional): provides SSH/SFTP/SCP/rsync access
// - FileBrowser sidecar (optional): provides HTTPS web-based file browser
// - PVCs are mounted at dual paths for flexible access (via symlinks)
//
// Mount modes:
// 1. Kubernetes-managed (UseInternalMounts=false, default):
//   - PVCs mounted by kubelet at /restores_by_name/<backup>/<uid>
//   - Simple and secure, no privileges required
//
// 2. Internal mount(2) (UseInternalMounts=true):
//   - Main container performs mount(2) syscalls internally
//   - Sidecars see mounts via mount propagation (Bidirectional → HostToContainer)
//   - Requires privileged security context or SYS_ADMIN capability
//   - Useful when main container needs mount flexibility
//
// Mount paths (Kubernetes-managed mode):
// - /restores_by_name/<backup-name>/<pvc-uid> (primary mount)
// - /restores_by_date/<backup-date>/<pvc-name> (symlink, if EnableDualPathAccess=true)
//
// Mount paths (Internal mount mode):
// - SharedMountPath (e.g., /mnt/restore) - main container mounts here
// - Sidecars see the same SharedMountPath via propagation
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

	// Add EmptyDir for dual-path symlinks if enabled
	if config.EnableDualPathAccess {
		allVolumes = append(allVolumes, corev1.Volume{
			Name: "restores-by-date",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

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
		sshContainer, sshVolumes, sshInitContainers := buildSSHSidecar(
			config.SSHAccess,
			config.UseInternalMounts,
			sharedMountVolumeName,
			config.SharedMountPath,
			pvcVolumeMounts,
			config.EnableDualPathAccess,
		)
		sidecarContainers = append(sidecarContainers, sshContainer)
		allVolumes = append(allVolumes, sshVolumes...)
		allInitContainers = append(allInitContainers, sshInitContainers...)
	}

	// Add FileBrowser sidecar if enabled
	if config.FileBrowserAccess != nil {
		fileBrowserContainer, fileBrowserVolumes, fileBrowserInitContainers := buildFileBrowserSidecar(
			config.FileBrowserAccess,
			config.UseInternalMounts,
			sharedMountVolumeName,
			config.SharedMountPath,
			pvcVolumeMounts,
			config.EnableDualPathAccess,
		)
		sidecarContainers = append(sidecarContainers, fileBrowserContainer)
		allVolumes = append(allVolumes, fileBrowserVolumes...)
		allInitContainers = append(allInitContainers, fileBrowserInitContainers...)
	}

	// Add dual-path symlink init container if enabled
	if config.EnableDualPathAccess {
		symlinkInit := buildInitContainerForDualPathSymlinks(config.PVCMounts)
		// Add PVC volume mounts to symlink init container
		symlinkInit.VolumeMounts = append(symlinkInit.VolumeMounts, pvcVolumeMounts...)
		// Prepend so it runs before sidecar init containers
		allInitContainers = append([]corev1.Container{symlinkInit}, allInitContainers...)
	}

	// Build or use default main container
	var mainContainer corev1.Container
	if config.MainContainer != nil {
		mainContainer = *config.MainContainer
	} else {
		// Default main container: simple busybox HTTP server
		mainContainer = buildDefaultMainContainer(pvcVolumeMounts, config.EnableDualPathAccess)
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
		mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, pvcVolumeMounts...)

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

		// If dual-path is enabled, also mount the symlink directory
		if config.EnableDualPathAccess {
			mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, corev1.VolumeMount{
				Name:      "restores-by-date",
				MountPath: "/restores_by_date",
				ReadOnly:  true,
			})
		}

		// Add /dev/fuse device mount (required for guestmount)
		mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, corev1.VolumeMount{
			Name:      "fuse-device",
			MountPath: "/dev/fuse",
		})

		// Add /dev/kvm device mount (required for KVM acceleration)
		mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, corev1.VolumeMount{
			Name:      "kvm-device",
			MountPath: "/dev/kvm",
		})

		// Add filesystems mount point (where guestmount will create filesystem mounts)
		mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, corev1.VolumeMount{
			Name:      "filesystems",
			MountPath: "/mnt/filesystems",
		})
	}

	// Combine main container with sidecars
	containers := []corev1.Container{mainContainer}
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

// buildFileServerDeployment builds a Deployment spec for serving files from restored PVCs.
// This wraps the pod spec from buildFileServerPodSpec in a Deployment for better lifecycle management.
//
// Using a Deployment instead of a bare Pod provides:
// - Automatic pod restart on failure
// - Better integration with Kubernetes lifecycle (no cross-namespace owner reference issues)
// - Production-ready deployment patterns
// - Simplified updates and rollbacks
func buildFileServerDeployment(config FileServerPodConfig) (*appsv1.Deployment, error) {
	// Build the pod spec using existing buildFileServerPodSpec function
	pod, err := buildFileServerPodSpec(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build pod spec for deployment: %w", err)
	}

	// Extract pod template from the built pod
	podTemplate := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      pod.Labels,
			Annotations: pod.Annotations,
		},
		Spec: pod.Spec,
	}

	// Build selector labels - must match pod template labels
	selectorLabels := map[string]string{
		constant.VMFROriginUUIDLabel: config.VMFRUID,
		"app":                        "vmfr-file-server",
	}

	// Build deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        config.PodName, // Use same name as pod would have
			Namespace:   config.PodNamespace,
			Labels:      pod.Labels,
			Annotations: pod.Annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)), // Single replica for file server
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Template: podTemplate,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType, // Recreate to avoid conflicts with PVC mounts
			},
		},
	}

	// IMPORTANT: Do NOT add cross-namespace owner references!
	// Kubernetes garbage collector rejects owner references where the owner
	// is in a different namespace than the owned resource.
	//
	// Instead of owner references, we use:
	// 1. Labels to track ownership (already added above)
	// 2. Finalizers on the VMFR to clean up resources on deletion
	//
	// The label constant.VMFROriginUUIDLabel uniquely identifies the owning VMFR
	// Name and namespace are stored in annotations for reference
	// and allow the controller to find and delete this Deployment during finalizer cleanup.

	return deployment, nil
}

// buildPVCVolumesAndMounts creates volumes, volume mounts, and volume devices for PVCs.
// Handles both Block and Filesystem mode PVCs correctly:
// - Block mode PVCs: VM disks stored as raw block devices, exposed via volumeDevices at /dev/pvc-{uid}
// - Filesystem mode PVCs: Traditional file storage, mounted via volumeMounts at /restores_by_name/{backup}/{uid}
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
			// Filesystem mode: Mount at /restores_by_name/<backup-name>/<pvc-uid>
			// Traditional file access for filesystem-based storage
			mountPath := fmt.Sprintf("/restores_by_name/%s/%s", pvcMount.BackupName, pvcMount.PVCUID)
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: mountPath,
				ReadOnly:  false, // libguestfs needs write access
			})
		}
	}

	return volumes, volumeMounts, volumeDevices
}

// buildDefaultMainContainer creates a default busybox HTTP server container
func buildDefaultMainContainer(pvcVolumeMounts []corev1.VolumeMount, enableDualPath bool) corev1.Container {
	volumeMounts := make([]corev1.VolumeMount, len(pvcVolumeMounts))
	copy(volumeMounts, pvcVolumeMounts)

	// Add dual-path mount if enabled
	if enableDualPath {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "restores-by-date",
			MountPath: "/restores_by_date",
			ReadOnly:  true,
		})
	}

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
func buildVMFileServerMainContainer() corev1.Container {
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

// buildInitContainerForDualPathSymlinks creates an init container that sets up symlinks
// to provide dual-path access to the same PVCs:
// - /restores_by_name/<backup-name>/<pvc-uid> (actual mount point)
// - /restores_by_date/<date>/<pvc-name> (symlink to above)
func buildInitContainerForDualPathSymlinks(pvcMounts []PVCMountInfo) corev1.Container {
	// Build shell commands to create symlink structure
	commands := []string{"set -e"} // Exit on error

	// Create base directory
	commands = append(commands, "mkdir -p /restores_by_date")

	for _, pvcMount := range pvcMounts {
		// Format date as YYYY-MM-DD
		backupDate := formatBackupDate(pvcMount.BackupTimestamp)

		// Source: /restores_by_name/<backup-name>/<pvc-uid>
		sourcePath := fmt.Sprintf("/restores_by_name/%s/%s", pvcMount.BackupName, pvcMount.PVCUID)

		// Target: /restores_by_date/<date>/<pvc-name>
		targetDir := fmt.Sprintf("/restores_by_date/%s", backupDate)
		targetPath := fmt.Sprintf("%s/%s", targetDir, pvcMount.PVCName)

		// Create date directory and symlink
		commands = append(commands,
			fmt.Sprintf("mkdir -p %s", targetDir),
			fmt.Sprintf("ln -sf %s %s", sourcePath, targetPath),
		)
	}

	commands = append(commands, "echo 'Dual-path symlinks created successfully'")

	return corev1.Container{
		Name:  "setup-dual-path-symlinks",
		Image: "quay.io/quay/busybox:latest",
		Command: []string{
			"/bin/sh",
			"-c",
			strings.Join(commands, "\n"),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "restores-by-date",
				MountPath: "/restores_by_date",
			},
		},
	}
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

// extractPVCMountsFromVMFR extracts PVC mount information from VMFR status
// Returns only successfully completed restores (one per PVC, choosing most recent)
func extractPVCMountsFromVMFR(vmfr *oadpv1alpha1.VirtualMachineFileRestore) []PVCMountInfo {
	var pvcMounts []PVCMountInfo

	for _, pvcRestore := range vmfr.Status.PVCRestores {
		// Skip synthetic entries (backup-level failures)
		if pvcRestore.PVCName == constant.BackupLevelFailurePVCName {
			continue
		}

		// Find the most recent successfully completed restore for this PVC
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
				// Only mount the most recent successful restore per PVC
				break
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

// formatBackupDate formats a timestamp as YYYY-MM-DD for mount path organization
func formatBackupDate(timestamp *metav1.Time) string {
	if timestamp == nil {
		return "unknown-date"
	}
	return timestamp.Time.Format("2006-01-02")
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
// Credentials can come from inline config, a Secret, or controller-generated keys.
//
// Implementation details:
// - Uses linuxserver/openssh-server image for flexibility and security
// - Runs as non-root user for better security posture
// - Configures restricted read-only shell environment
// - Supports both public key and password authentication
// - Init container sets up SSH user configuration from inline or Secret credentials
//
//nolint:unparam // Some parameters used in future mount modes
func buildSSHSidecar(
	config *SSHAccessConfig,
	useInternalMounts bool,
	sharedMountVolumeName string,
	sharedMountPath string,
	pvcVolumeMounts []corev1.VolumeMount,
	enableDualPath bool,
) (corev1.Container, []corev1.Volume, []corev1.Container) {

	// SSH server container using linuxserver/openssh-server
	// This image provides a secure, configurable SSH server that runs as non-root
	container := corev1.Container{
		Name:  "ssh-server",
		Image: "lscr.io/linuxserver/openssh-server:latest",
		Ports: []corev1.ContainerPort{
			{
				Name:          "ssh",
				ContainerPort: config.Port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env: []corev1.EnvVar{
			{
				Name:  "PUID",
				Value: "1000", // Run as non-root user
			},
			{
				Name:  "PGID",
				Value: "1000",
			},
			{
				Name:  "USER_NAME",
				Value: config.Username,
			},
			{
				// Port configuration for SSH service
				Name:  "SSH_PORT",
				Value: fmt.Sprintf("%d", config.Port),
			},
			{
				// Disable password authentication (only public key auth is supported)
				Name:  "PASSWORD_ACCESS",
				Value: "false",
			},
			{
				// Enable public key authentication
				Name:  "PUBLIC_KEY_DIR",
				Value: "/config/ssh_keys",
			},
		},
	}

	// Add shared volume for SSH server config and user home directory
	// This is needed for SSH server to store its configuration and user data
	var volumes []corev1.Volume
	sshConfigVolume := corev1.Volume{
		Name: "ssh-config",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
	volumes = append(volumes, sshConfigVolume)

	// Mount the SSH config volume
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      "ssh-config",
		MountPath: "/config",
	})

	// Configure volume mounts based on mount mode
	if useInternalMounts {
		// Internal mount mode: use shared mount volume with propagation
		mountPropagation := corev1.MountPropagationHostToContainer
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:             sharedMountVolumeName,
			MountPath:        sharedMountPath,
			ReadOnly:         true,
			MountPropagation: &mountPropagation,
		})
	} else {
		// Kubernetes-managed mode: mount PVCs directly
		container.VolumeMounts = append(container.VolumeMounts, pvcVolumeMounts...)
		if enableDualPath {
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name:      "restores-by-date",
				MountPath: "/restores_by_date",
				ReadOnly:  true,
			})
		}
	}

	// Add Secret volume and init container for SSH configuration
	// The controller ensures the Secret exists before pod creation (via ensureCredentials)
	var initContainers []corev1.Container

	// Credentials from Secret: mount the secret and use init container to configure
	credentialsVolume := corev1.Volume{
		Name: "ssh-credentials",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: config.CredentialsSecretName,
			},
		},
	}
	volumes = append(volumes, credentialsVolume)

	// Init container to set up SSH configuration from Secret
	initContainer := buildSSHInitContainerFromSecret()
	initContainers = append(initContainers, initContainer)

	return container, volumes, initContainers
}

// buildSSHInitContainerFromSecret creates an init container that configures SSH from a Secret.
// The Secret must contain the publicKey field for public key authentication.
// Password authentication is not supported - only public key auth is enabled.
func buildSSHInitContainerFromSecret() corev1.Container {
	// Shell script to configure SSH user from Secret credentials
	setupScript := `#!/bin/sh
set -e

echo "Setting up SSH configuration from Secret..."

# Create SSH keys directory
mkdir -p /config/ssh_keys

# Copy public key from secret for key-based authentication
if [ ! -f /credentials/publicKey ]; then
    echo "ERROR: publicKey not found in Secret"
    exit 1
fi

echo "Configuring public key authentication..."
cp /credentials/publicKey /config/ssh_keys/authorized_keys
chmod 600 /config/ssh_keys/authorized_keys

# Note: No chown needed - OpenShift's restricted-v2 SCC doesn't allow it,
# and EmptyDir volumes are created with pod's fsGroup ownership automatically

echo "SSH configuration completed successfully (public key auth only)"
`

	return corev1.Container{
		Name:  "setup-ssh-credentials",
		Image: "quay.io/quay/busybox:latest",
		Command: []string{
			"/bin/sh",
			"-c",
			setupScript,
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "ssh-config",
				MountPath: "/config",
			},
			{
				Name:      "ssh-credentials",
				MountPath: "/credentials",
				ReadOnly:  true,
			},
		},
	}
}

// buildSSHInitContainerInline creates an init container that configures SSH from inline publicKey.
func buildSSHInitContainerInline(publicKey string) corev1.Container {
	// Shell script to configure SSH user from inline publicKey
	// The publicKey is embedded directly in the script
	setupScript := fmt.Sprintf(`#!/bin/sh
set -e

echo "Setting up SSH configuration from inline publicKey..."

# Create SSH keys directory
mkdir -p /config/ssh_keys

# Write the public key to authorized_keys
cat > /config/ssh_keys/authorized_keys <<'EOF'
%s
EOF

chmod 600 /config/ssh_keys/authorized_keys

# Note: No chown needed - OpenShift's restricted-v2 SCC doesn't allow it,
# and EmptyDir volumes are created with pod's fsGroup ownership automatically

echo "SSH configuration completed successfully"
`, publicKey)

	return corev1.Container{
		Name:  "setup-ssh-credentials",
		Image: "quay.io/quay/busybox:latest",
		Command: []string{
			"/bin/sh",
			"-c",
			setupScript,
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "ssh-config",
				MountPath: "/config",
			},
		},
	}
}

// buildFileBrowserSidecar creates a FileBrowser sidecar container with associated volumes and init containers.
//
// The FileBrowser server provides web-based file browsing access to restored PVC data.
// Credentials are read from a Secret containing username and password.
//
// Implementation details:
// - Uses filebrowser/filebrowser official image
// - Runs on the specified port (defaults to 443 in API, but often 8443 in practice)
// - Configured with authentication from Secret credentials
// - Init container sets up FileBrowser database and user configuration
// - Serves files in read-only mode for safety
//
//nolint:unparam // Some parameters used in future mount modes
func buildFileBrowserSidecar(
	config *FileBrowserAccessConfig,
	useInternalMounts bool,
	sharedMountVolumeName string,
	sharedMountPath string,
	pvcVolumeMounts []corev1.VolumeMount,
	enableDualPath bool,
) (corev1.Container, []corev1.Volume, []corev1.Container) {

	// FileBrowser container using official filebrowser/filebrowser image
	// This provides a modern, lightweight web-based file browser
	container := corev1.Container{
		Name:  "filebrowser",
		Image: "filebrowser/filebrowser:latest",
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: config.Port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		// FileBrowser command-line arguments
		// --noauth: disabled because we want authentication
		// --port: listening port
		// --database: path to database file
		// --root: root directory to serve (we'll use / to serve all mount points)
		Args: []string{
			"--port", fmt.Sprintf("%d", config.Port),
			"--database", "/config/filebrowser.db",
			"--root", "/",
			"--address", "0.0.0.0",
		},
	}

	// Add volumes for FileBrowser configuration and database
	var volumes []corev1.Volume

	// Config volume for FileBrowser database
	configVolume := corev1.Volume{
		Name: "filebrowser-config",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
	volumes = append(volumes, configVolume)

	// Mount the config volume
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      "filebrowser-config",
		MountPath: "/config",
	})

	// Add credentials Secret volume
	credentialsVolume := corev1.Volume{
		Name: "filebrowser-credentials",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: config.CredentialsSecretName,
			},
		},
	}
	volumes = append(volumes, credentialsVolume)

	// Configure volume mounts based on mount mode
	if useInternalMounts {
		// Internal mount mode: use shared mount volume with propagation
		mountPropagation := corev1.MountPropagationHostToContainer
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:             sharedMountVolumeName,
			MountPath:        sharedMountPath,
			ReadOnly:         true,
			MountPropagation: &mountPropagation,
		})
	} else {
		// Kubernetes-managed mode: mount PVCs directly
		container.VolumeMounts = append(container.VolumeMounts, pvcVolumeMounts...)
		if enableDualPath {
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name:      "restores-by-date",
				MountPath: "/restores_by_date",
				ReadOnly:  true,
			})
		}
	}

	// Init container to configure FileBrowser database and create user
	var initContainers []corev1.Container
	initContainer := buildFileBrowserInitContainer()
	initContainers = append(initContainers, initContainer)

	return container, volumes, initContainers
}

// buildFileBrowserInitContainer creates an init container that initializes FileBrowser.
// Sets up the database, creates a user from Secret credentials, and configures settings.
func buildFileBrowserInitContainer() corev1.Container {
	// Shell script to initialize FileBrowser database and create user
	// The filebrowser CLI is used to configure the database before the server starts
	setupScript := `#!/bin/sh
set -e

echo "Initializing FileBrowser configuration..."

# Read credentials from Secret
if [ ! -f /credentials/username ] || [ ! -f /credentials/password ]; then
    echo "ERROR: username or password not found in Secret"
    exit 1
fi

USERNAME=$(cat /credentials/username)
PASSWORD=$(cat /credentials/password)

echo "Creating FileBrowser database and user..."

# Initialize the database (creates default admin user)
/filebrowser config init --database /config/filebrowser.db

# Set the root directory to / (will serve all mounted paths)
/filebrowser config set --database /config/filebrowser.db --root /

# Set address and port (these are defaults, but being explicit)
/filebrowser config set --database /config/filebrowser.db --address 0.0.0.0

# Delete the default admin user
/filebrowser users rm admin --database /config/filebrowser.db || true

# Create the user from Secret credentials
echo "Creating user: $USERNAME"
echo "$PASSWORD" | /filebrowser users add "$USERNAME" --database /config/filebrowser.db --perm.create=false --perm.delete=false --perm.modify=false --perm.rename=false

# Set permissions to read-only
/filebrowser users update "$USERNAME" --database /config/filebrowser.db --perm.create=false --perm.delete=false --perm.modify=false --perm.rename=false

echo "FileBrowser initialization completed successfully"
echo "User '$USERNAME' created with read-only permissions"
`

	return corev1.Container{
		Name:  "setup-filebrowser",
		Image: "filebrowser/filebrowser:latest",
		Command: []string{
			"/bin/sh",
			"-c",
			setupScript,
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "filebrowser-config",
				MountPath: "/config",
			},
			{
				Name:      "filebrowser-credentials",
				MountPath: "/credentials",
				ReadOnly:  true,
			},
		},
	}
}

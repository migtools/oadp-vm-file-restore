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

// Package velerohelpers provides utilities for reading and discovering Velero backup contents
// using legitimate Velero APIs and mechanisms for VM backup validation.
//
// This file contains the current/recommended VeleroBackupContentsReader implementation
// that uses Velero's download APIs for content validation.
package velerohelpers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/sirupsen/logrus"
	veleroapi "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/cmd/util/downloadrequest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BackupMetadata represents the metadata.json structure from Velero backup storage
type BackupMetadata struct {
	Items []BackupResourceItem `json:"items"`
}

// BackupResourceItem represents an individual resource item in backup metadata
type BackupResourceItem struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}

// VeleroBackupContentsReader validates VM presence in Velero backups using two-stage validation
type VeleroBackupContentsReader struct {
	k8sClient             client.Client
	logger                logrus.FieldLogger
	insecureSkipTLSVerify bool
	caCertFile            string
	downloadTimeout       time.Duration
}

// NewVeleroBackupContentsReader creates a new backup contents reader
func NewVeleroBackupContentsReader() *VeleroBackupContentsReader {
	logger := logrus.WithField("component", "backup-contents-reader")

	return &VeleroBackupContentsReader{
		logger:                logger,
		insecureSkipTLSVerify: false,
		caCertFile:            "",
		downloadTimeout:       time.Minute * 5,
	}
}

// SetClient sets the Kubernetes client (called by controller setup)
func (r *VeleroBackupContentsReader) SetClient(k8sClient client.Client) {
	r.k8sClient = k8sClient
}

// BackupContainsVM validates whether a backup contains the specified virtual machine
// using a two-stage validation approach:
// 1. Spec-based validation: Check backup's included/excluded namespaces
// 2. Content validation: Fetch and parse backup metadata from object storage (TODO)
func (r *VeleroBackupContentsReader) BackupContainsVM(ctx context.Context, backup *veleroapi.Backup, vmName, vmNamespace string) (bool, error) {
	logger := r.logger.WithFields(logrus.Fields{
		"backup":      backup.Name,
		"vm":          vmName,
		"vmNamespace": vmNamespace,
	})

	// Stage 1: Spec-based validation - Quick check of backup inclusion/exclusion rules
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vmName,
			Namespace: vmNamespace,
		},
	}
	if !ValidateVMInBackupSpec(backup, vm) {
		logger.Debug("VM namespace not included in backup spec")
		return false, nil
	}

	// Stage 2: Content validation - Fetch actual backup metadata from object storage
	metadata, err := r.FetchBackupMetadata(ctx, backup)
	if err != nil {
		// If metadata fetch fails, fall back to spec-based validation only
		// This maintains compatibility when BSL access is not available
		logger.WithError(err).Debug("Content validation failed, using spec-based validation only")
		return true, nil
	}

	// Check if VM is actually present in the backup metadata
	return r.hasVMInMetadata(metadata, vmName, vmNamespace), nil
}

// FetchBackupMetadata downloads and parses backup metadata from object storage using Velero's download mechanism
func (r *VeleroBackupContentsReader) FetchBackupMetadata(ctx context.Context, backup *veleroapi.Backup) (*BackupMetadata, error) {
	logger := r.logger.WithField("backup", backup.Name)

	// Download backup resource list (contains the actual backed up resources)
	buf := new(bytes.Buffer)
	err := downloadrequest.Stream(
		ctx,
		r.k8sClient,
		backup.Namespace,
		backup.Name,
		veleroapi.DownloadTargetKindBackupResourceList,
		buf,
		r.downloadTimeout,
		r.insecureSkipTLSVerify,
		r.caCertFile,
	)

	if err != nil {
		if err == downloadrequest.ErrNotFound {
			logger.Debug("Backup resource list not found - backup may be in progress or from older Velero version")
			return nil, err
		}
		return nil, err
	}

	// Parse the resource list into our BackupMetadata format
	var resourceList map[string][]string
	if err := json.NewDecoder(buf).Decode(&resourceList); err != nil {
		logger.WithError(err).Error("Failed to decode backup resource list JSON")
		return nil, err
	}

	logger.WithField("resourceListKeys", len(resourceList)).Debug("Decoded backup resource list")

	// Convert Velero's resource list format to our BackupMetadata format
	metadata := &BackupMetadata{Items: []BackupResourceItem{}}
	pvcCount := 0

	for gvk, resources := range resourceList {
		// Parse GVK format (e.g., "v1/VirtualMachine.kubevirt.io")
		kind := extractKindFromGVK(gvk)
		logger.WithFields(logrus.Fields{
			"gvk":           gvk,
			"kind":          kind,
			"resourceCount": len(resources),
		}).Debug("Processing resource group")

		for _, resource := range resources {
			// Parse resource format (e.g., "namespace/name")
			namespace, name := parseNamespacedName(resource)
			metadata.Items = append(metadata.Items, BackupResourceItem{
				APIVersion: gvk,
				Kind:       kind,
				Namespace:  namespace,
				Name:       name,
			})

			if kind == "PersistentVolumeClaim" {
				pvcCount++
				logger.WithFields(logrus.Fields{
					"pvcName":      name,
					"pvcNamespace": namespace,
				}).Debug("Found PVC in backup resource list")
			}
		}
	}

	logger.WithFields(logrus.Fields{
		"totalResources": len(metadata.Items),
		"totalPVCs":      pvcCount,
	}).Info("Successfully fetched backup metadata")
	return metadata, nil
}

// hasVMInMetadata checks if the specified VM is present in the backup metadata
func (r *VeleroBackupContentsReader) hasVMInMetadata(metadata *BackupMetadata, vmName, vmNamespace string) bool {
	return hasVMInBackupResources(vmName, vmNamespace, func(yield func(kind, namespace, name string) bool) {
		for _, item := range metadata.Items {
			if !yield(item.Kind, item.Namespace, item.Name) {
				return
			}
		}
	})
}

// FetchPVCFromBackup extracts the specific PVC resource from backup tar file
func (r *VeleroBackupContentsReader) FetchPVCFromBackup(ctx context.Context, backup *veleroapi.Backup, pvcName, pvcNamespace string) (*corev1.PersistentVolumeClaim, error) {
	logger := r.logger.WithFields(logrus.Fields{
		"backup":       backup.Name,
		"pvcName":      pvcName,
		"pvcNamespace": pvcNamespace,
	})

	// Download backup contents (the tar.gz file)
	buf := new(bytes.Buffer)
	err := downloadrequest.Stream(
		ctx,
		r.k8sClient,
		backup.Namespace,
		backup.Name,
		veleroapi.DownloadTargetKindBackupContents,
		buf,
		r.downloadTimeout,
		r.insecureSkipTLSVerify,
		r.caCertFile,
	)

	if err != nil {
		if err == downloadrequest.ErrNotFound {
			logger.WithFields(logrus.Fields{
				"backupNamespace": backup.Namespace,
				"downloadTarget":  veleroapi.DownloadTargetKindBackupContents,
			}).Debug("Backup contents not found - this could indicate missing backup data in storage")
			return nil, err
		}
		logger.WithError(err).WithFields(logrus.Fields{
			"backupNamespace": backup.Namespace,
			"downloadTarget":  veleroapi.DownloadTargetKindBackupContents,
		}).Error("Failed to download backup contents")
		return nil, err
	}

	// Extract PVC resource from tar file
	pvc, err := r.extractPVCFromBackupTar(buf.Bytes(), pvcName, pvcNamespace)
	if err != nil {
		logger.WithError(err).Error("Failed to extract PVC from backup tar")
		return nil, err
	}

	if pvc == nil {
		logger.Debug("PVC not found in backup tar")
		return nil, downloadrequest.ErrNotFound
	}

	logger.WithField("pvcUID", pvc.UID).Debug("Successfully fetched PVC from backup tar")
	return pvc, nil
}

// ExtractVMFromBackupMetadata extracts VM information from backup metadata for the specified VM
// This uses the backup resource list first to confirm VM presence, then downloads the specific VM resource
func (r *VeleroBackupContentsReader) ExtractVMFromBackupMetadata(ctx context.Context, backup *veleroapi.Backup, vmName, vmNamespace string) (*kubevirtv1.VirtualMachine, error) {
	logger := r.logger.WithFields(logrus.Fields{
		"backup":      backup.Name,
		"vmName":      vmName,
		"vmNamespace": vmNamespace,
	})

	// First, get backup metadata to confirm the VM is present
	metadata, err := r.FetchBackupMetadata(ctx, backup)
	if err != nil {
		logger.WithError(err).Debug("Failed to fetch backup metadata, using fallback approach")
		// Fallback: try to download individual VM resource directly
		return r.downloadVMResource(ctx, backup, vmName, vmNamespace)
	}

	// Check if VM is present in the backup metadata
	vmFound := false
	for _, item := range metadata.Items {
		if item.Kind == "VirtualMachine" && item.Name == vmName && item.Namespace == vmNamespace {
			vmFound = true
			logger.WithFields(logrus.Fields{"vmName": vmName, "vmNamespace": vmNamespace}).Debug("VM found in backup metadata")
			break
		}
	}

	if !vmFound {
		logger.Debug("VM not found in backup metadata")
		return nil, downloadrequest.ErrNotFound
	}

	// VM is confirmed to be in backup, now download the specific VM resource
	return r.downloadVMResource(ctx, backup, vmName, vmNamespace)
}

// downloadVMResource extracts VM information from backup tar file
func (r *VeleroBackupContentsReader) downloadVMResource(ctx context.Context, backup *veleroapi.Backup, vmName, vmNamespace string) (*kubevirtv1.VirtualMachine, error) {
	logger := r.logger.WithFields(logrus.Fields{
		"backup":      backup.Name,
		"vmName":      vmName,
		"vmNamespace": vmNamespace,
	})

	// Download backup contents (the tar.gz file)
	logger.Debug("Downloading backup tar to extract individual VM resource")

	buf := new(bytes.Buffer)
	err := downloadrequest.Stream(
		ctx,
		r.k8sClient,
		backup.Namespace,
		backup.Name,
		veleroapi.DownloadTargetKindBackupContents,
		buf,
		r.downloadTimeout,
		r.insecureSkipTLSVerify,
		r.caCertFile,
	)

	if err != nil {
		if err == downloadrequest.ErrNotFound {
			logger.Debug("Backup contents not found - backup may be in progress or from older Velero version")
			return nil, err
		}
		logger.WithError(err).Error("Failed to download backup contents")
		return nil, err
	}

	// Extract VM resource from tar file
	vm, err := r.extractVMFromBackupTar(buf.Bytes(), vmName, vmNamespace)
	if err != nil {
		logger.WithError(err).Error("Failed to extract VM from backup tar")
		return nil, err
	}

	if vm == nil {
		logger.Debug("VM not found in backup tar")
		return nil, downloadrequest.ErrNotFound
	}

	logger.Debug("Successfully extracted VM from backup tar")
	return vm, nil
}

// extractVMFromBackupTar extracts a specific VM resource from the backup tar file
func (r *VeleroBackupContentsReader) extractVMFromBackupTar(backupTarData []byte, vmName, vmNamespace string) (*kubevirtv1.VirtualMachine, error) {
	logger := r.logger.WithFields(logrus.Fields{
		"vmName":      vmName,
		"vmNamespace": vmNamespace,
	})

	// Expected VM resource path in tar: resources/virtualmachines.kubevirt.io/namespaces/{namespace}/{name}.json
	expectedVMPath := fmt.Sprintf("resources/virtualmachines.kubevirt.io/namespaces/%s/%s.json", vmNamespace, vmName)
	logger.WithField("expectedPath", expectedVMPath).Debug("Looking for VM resource in backup tar")

	// Create a gzip reader
	gzipReader, err := gzip.NewReader(bytes.NewReader(backupTarData))
	if err != nil {
		logger.WithError(err).Error("Failed to create gzip reader for backup tar")
		return nil, err
	}
	defer func() { _ = gzipReader.Close() }()

	// Create a tar reader
	tarReader := tar.NewReader(gzipReader)

	// Iterate through tar entries
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // End of tar file
		}
		if err != nil {
			logger.WithError(err).Error("Failed to read next tar entry")
			return nil, err
		}

		// Check if this is the VM resource file we're looking for
		if header.Name == expectedVMPath {
			logger.WithField("path", header.Name).Debug("Found VM resource file in backup tar")

			// Read the VM resource content
			vmResourceData, err := io.ReadAll(tarReader)
			if err != nil {
				logger.WithError(err).Error("Failed to read VM resource data from tar")
				return nil, err
			}

			// Parse the VM resource JSON
			var vm kubevirtv1.VirtualMachine
			if err := json.Unmarshal(vmResourceData, &vm); err != nil {
				logger.WithError(err).Error("Failed to unmarshal VM resource JSON")
				return nil, err
			}

			// Verify this is the correct VM
			if vm.Name != vmName || vm.Namespace != vmNamespace {
				logger.WithFields(logrus.Fields{
					"expectedName":      vmName,
					"expectedNamespace": vmNamespace,
					"actualName":        vm.Name,
					"actualNamespace":   vm.Namespace,
				}).Warn("VM resource has unexpected name/namespace")
				return nil, fmt.Errorf("VM resource has unexpected name/namespace")
			}

			logger.Debug("Successfully extracted VM resource from backup tar")
			return &vm, nil
		}
	}

	logger.WithField("expectedPath", expectedVMPath).Debug("VM resource not found in backup tar")
	return nil, nil // VM not found
}

// extractPVCFromBackupTar extracts a specific PVC resource from the backup tar file
func (r *VeleroBackupContentsReader) extractPVCFromBackupTar(backupTarData []byte, pvcName, pvcNamespace string) (*corev1.PersistentVolumeClaim, error) {
	logger := r.logger.WithFields(logrus.Fields{
		"pvcName":      pvcName,
		"pvcNamespace": pvcNamespace,
	})

	// Expected PVC resource path in tar: resources/persistentvolumeclaims/namespaces/{namespace}/{name}.json
	expectedPVCPath := fmt.Sprintf("resources/persistentvolumeclaims/namespaces/%s/%s.json", pvcNamespace, pvcName)
	logger.WithField("expectedPath", expectedPVCPath).Debug("Looking for PVC resource in backup tar")

	// Create a gzip reader
	gzipReader, err := gzip.NewReader(bytes.NewReader(backupTarData))
	if err != nil {
		logger.WithError(err).Error("Failed to create gzip reader for backup tar")
		return nil, err
	}
	defer func() { _ = gzipReader.Close() }()

	// Create a tar reader
	tarReader := tar.NewReader(gzipReader)

	// Iterate through tar entries
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // End of tar file
		}
		if err != nil {
			logger.WithError(err).Error("Failed to read next tar entry")
			return nil, err
		}

		// Check if this is the PVC resource file we're looking for
		if header.Name == expectedPVCPath {
			logger.WithField("path", header.Name).Debug("Found PVC resource file in backup tar")

			// Read the PVC resource content
			pvcResourceData, err := io.ReadAll(tarReader)
			if err != nil {
				logger.WithError(err).Error("Failed to read PVC resource data from tar")
				return nil, err
			}

			// Parse the PVC resource JSON
			var pvc corev1.PersistentVolumeClaim
			if err := json.Unmarshal(pvcResourceData, &pvc); err != nil {
				logger.WithError(err).Error("Failed to unmarshal PVC resource JSON")
				return nil, err
			}

			// Verify this is the correct PVC
			if pvc.Name != pvcName || pvc.Namespace != pvcNamespace {
				logger.WithFields(logrus.Fields{
					"expectedName":      pvcName,
					"expectedNamespace": pvcNamespace,
					"actualName":        pvc.Name,
					"actualNamespace":   pvc.Namespace,
				}).Warn("PVC resource has unexpected name/namespace")
				return nil, fmt.Errorf("PVC resource has unexpected name/namespace")
			}

			logger.WithField("pvcUID", pvc.UID).Debug("Successfully extracted PVC resource from backup tar")
			return &pvc, nil
		}
	}

	logger.WithField("expectedPath", expectedPVCPath).Debug("PVC resource not found in backup tar")
	return nil, nil // PVC not found
}

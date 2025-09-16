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
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/sirupsen/logrus"
	veleroapi "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/cmd/util/downloadrequest"
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
	metadata, err := r.fetchBackupMetadata(ctx, backup)
	if err != nil {
		// If metadata fetch fails, fall back to spec-based validation only
		// This maintains compatibility when BSL access is not available
		logger.WithError(err).Debug("Content validation failed, using spec-based validation only")
		return true, nil
	}

	// Check if VM is actually present in the backup metadata
	return r.hasVMInMetadata(metadata, vmName, vmNamespace), nil
}

// fetchBackupMetadata downloads and parses backup metadata from object storage using Velero's download mechanism
func (r *VeleroBackupContentsReader) fetchBackupMetadata(ctx context.Context, backup *veleroapi.Backup) (*BackupMetadata, error) {
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
		return nil, err
	}

	// Convert Velero's resource list format to our BackupMetadata format
	metadata := &BackupMetadata{Items: []BackupResourceItem{}}
	for gvk, resources := range resourceList {
		// Parse GVK format (e.g., "v1/VirtualMachine.kubevirt.io")
		kind := extractKindFromGVK(gvk)
		for _, resource := range resources {
			// Parse resource format (e.g., "namespace/name")
			namespace, name := parseNamespacedName(resource)
			metadata.Items = append(metadata.Items, BackupResourceItem{
				APIVersion: gvk,
				Kind:       kind,
				Namespace:  namespace,
				Name:       name,
			})
		}
	}

	logger.WithField("resourceCount", len(metadata.Items)).Debug("Successfully fetched backup metadata")
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

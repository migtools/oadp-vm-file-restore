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

package types

// PVCInfo represents a PVC from a backup and all restores associated with it.
// The combination of PVCUID + PVCName ensures uniqueness across multiple backups.
type PVCInfo struct {
	// UID of the PVC at the time of the backup
	PVCUID string `json:"pvcUID"`

	// Name of the PVC at the time of the backup
	PVCName string `json:"pvcName"`

	// Namespace of the PVC at the time of the backup
	PVCNamespace string `json:"pvcNamespace"`

	// Size of the PVC in human-readable format (e.g., "5Gi", "30Gi")
	// +optional
	Size string `json:"size,omitempty"`
}

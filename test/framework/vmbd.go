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

// Package framework provides testing utilities for VirtualMachineBackupsDiscovery and VirtualMachineFileRestore
package framework

import (
	"context"
	"fmt"
	"time"

	oadpv1alpha1 "github.com/migtools/oadp-vm-file-restore/api/v1alpha1"
	ginkgo "github.com/onsi/ginkgo/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	vmbdPollInterval = 5 * time.Second
	vmbdWaitTime     = 10 * time.Minute
)

// CreateVMBackupsDiscovery creates a VirtualMachineBackupsDiscovery resource
func CreateVMBackupsDiscovery(
	ctx context.Context,
	k8sClient client.Client,
	vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery,
) error {
	ginkgo.By(fmt.Sprintf("Creating VirtualMachineBackupsDiscovery %s in namespace %s", vmbd.Name, vmbd.Namespace))
	return k8sClient.Create(ctx, vmbd)
}

// NewVMBackupsDiscovery creates a minimal VirtualMachineBackupsDiscovery definition
func NewVMBackupsDiscovery(name, namespace, vmName, vmNamespace string) *oadpv1alpha1.VirtualMachineBackupsDiscovery {
	return &oadpv1alpha1.VirtualMachineBackupsDiscovery{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
			VirtualMachineName:      vmName,
			VirtualMachineNamespace: vmNamespace,
		},
	}
}

// GetVMBackupsDiscovery retrieves a VirtualMachineBackupsDiscovery resource
func GetVMBackupsDiscovery(
	ctx context.Context,
	k8sClient client.Client,
	name, namespace string,
) (*oadpv1alpha1.VirtualMachineBackupsDiscovery, error) {
	vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, vmbd)
	return vmbd, err
}

// WaitForVMBackupsDiscoveryPhase waits for VMBD to reach specified phase(s)
//
//nolint:dupl // Similar to WaitForVMFileRestorePhase but for different type
func WaitForVMBackupsDiscoveryPhase(
	ctx context.Context,
	k8sClient client.Client,
	name, namespace string,
	phases ...oadpv1alpha1.VirtualMachineBackupsDiscoveryPhase,
) error {
	ginkgo.By(fmt.Sprintf("Waiting for VMBD %s to reach phase: %v", name, phases))

	//nolint:staticcheck // PollImmediate still works for our use case
	return wait.PollImmediate(vmbdPollInterval, vmbdWaitTime, func() (bool, error) {
		vmbd, err := GetVMBackupsDiscovery(ctx, k8sClient, name, namespace)
		if err != nil {
			ginkgo.By(fmt.Sprintf("Failed to get VMBD: %v", err))
			return false, nil
		}

		currentPhase := vmbd.Status.Phase
		ginkgo.By(fmt.Sprintf("Current VMBD phase: %s", currentPhase))

		for _, phase := range phases {
			if currentPhase == phase {
				ginkgo.By(fmt.Sprintf("VMBD reached phase: %s", phase))
				return true, nil
			}
		}

		// Fail fast if Failed phase and not expected
		if currentPhase == oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseFailed {
			for _, phase := range phases {
				if phase == oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseFailed {
					return false, nil // Expected failed, keep waiting
				}
			}
			return false, fmt.Errorf("VMBD entered Failed phase unexpectedly")
		}

		return false, nil
	})
}

// WaitForVMBackupsDiscoveryCompleted waits for VMBD to reach Completed or PartiallyFailed
func WaitForVMBackupsDiscoveryCompleted(
	ctx context.Context,
	k8sClient client.Client,
	name, namespace string,
) (*oadpv1alpha1.VirtualMachineBackupsDiscovery, error) {
	err := WaitForVMBackupsDiscoveryPhase(ctx, k8sClient, name, namespace,
		oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseCompleted,
		oadpv1alpha1.VirtualMachineBackupsDiscoveryPhasePartiallyFailed)
	if err != nil {
		return nil, err
	}
	return GetVMBackupsDiscovery(ctx, k8sClient, name, namespace)
}

// DeleteVMBackupsDiscovery deletes a VirtualMachineBackupsDiscovery resource
func DeleteVMBackupsDiscovery(ctx context.Context, k8sClient client.Client, name, namespace string) error {
	vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	ginkgo.By(fmt.Sprintf("Deleting VirtualMachineBackupsDiscovery %s in namespace %s", name, namespace))
	return client.IgnoreNotFound(k8sClient.Delete(ctx, vmbd))
}

// GetValidBackupNames returns the list of valid backup names from VMBD status
func GetValidBackupNames(vmbd *oadpv1alpha1.VirtualMachineBackupsDiscovery) []string {
	names := make([]string, 0, len(vmbd.Status.ValidBackups))
	for _, backup := range vmbd.Status.ValidBackups {
		names = append(names, backup.Name)
	}
	return names
}

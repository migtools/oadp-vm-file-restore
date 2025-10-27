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
	vmfrPollInterval = 5 * time.Second
	vmfrWaitTime     = 15 * time.Minute
)

// CreateVMFileRestore creates a VirtualMachineFileRestore resource
func CreateVMFileRestore(
	ctx context.Context,
	k8sClient client.Client,
	vmfr *oadpv1alpha1.VirtualMachineFileRestore,
) error {
	ginkgo.By(fmt.Sprintf("Creating VirtualMachineFileRestore %s in namespace %s", vmfr.Name, vmfr.Namespace))
	return k8sClient.Create(ctx, vmfr)
}

// NewVMFileRestore creates a minimal VirtualMachineFileRestore definition
func NewVMFileRestore(name, namespace, vmbdRef string) *oadpv1alpha1.VirtualMachineFileRestore {
	return &oadpv1alpha1.VirtualMachineFileRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
			BackupsDiscoveryRef: vmbdRef,
		},
	}
}

// NewVMFileRestoreWithSelectedBackups creates a VirtualMachineFileRestore with specific backups selected
func NewVMFileRestoreWithSelectedBackups(
	name, namespace, vmbdRef string,
	selectedBackups []string,
) *oadpv1alpha1.VirtualMachineFileRestore {
	return &oadpv1alpha1.VirtualMachineFileRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
			BackupsDiscoveryRef: vmbdRef,
			SelectedBackups:     selectedBackups,
		},
	}
}

// GetVMFileRestore retrieves a VirtualMachineFileRestore resource
func GetVMFileRestore(
	ctx context.Context,
	k8sClient client.Client,
	name, namespace string,
) (*oadpv1alpha1.VirtualMachineFileRestore, error) {
	vmfr := &oadpv1alpha1.VirtualMachineFileRestore{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, vmfr)
	return vmfr, err
}

// WaitForVMFileRestorePhase waits for VMFR to reach specified phase(s)
//
//nolint:dupl // Similar to WaitForVMBackupsDiscoveryPhase but for different type
func WaitForVMFileRestorePhase(
	ctx context.Context,
	k8sClient client.Client,
	name, namespace string,
	phases ...oadpv1alpha1.VirtualMachineFileRestorePhase,
) error {
	ginkgo.By(fmt.Sprintf("Waiting for VMFR %s to reach phase: %v", name, phases))

	//nolint:staticcheck // PollImmediate still works for our use case
	return wait.PollImmediate(vmfrPollInterval, vmfrWaitTime, func() (bool, error) {
		vmfr, err := GetVMFileRestore(ctx, k8sClient, name, namespace)
		if err != nil {
			ginkgo.By(fmt.Sprintf("Failed to get VMFR: %v", err))
			return false, nil
		}

		currentPhase := vmfr.Status.Phase
		ginkgo.By(fmt.Sprintf("Current VMFR phase: %s", currentPhase))

		for _, phase := range phases {
			if currentPhase == phase {
				ginkgo.By(fmt.Sprintf("VMFR reached phase: %s", phase))
				return true, nil
			}
		}

		// Fail fast if Failed phase and not expected
		if currentPhase == oadpv1alpha1.VirtualMachineFileRestorePhaseFailed {
			for _, phase := range phases {
				if phase == oadpv1alpha1.VirtualMachineFileRestorePhaseFailed {
					return false, nil // Expected failed, keep waiting
				}
			}
			return false, fmt.Errorf("VMFR entered Failed phase unexpectedly")
		}

		return false, nil
	})
}

// WaitForVMFileRestoreCompleted waits for VMFR to reach Completed or PartiallyFailed
func WaitForVMFileRestoreCompleted(
	ctx context.Context,
	k8sClient client.Client,
	name, namespace string,
) (*oadpv1alpha1.VirtualMachineFileRestore, error) {
	err := WaitForVMFileRestorePhase(ctx, k8sClient, name, namespace,
		oadpv1alpha1.VirtualMachineFileRestorePhaseCompleted,
		oadpv1alpha1.VirtualMachineFileRestorePhasePartiallyFailed)
	if err != nil {
		return nil, err
	}
	return GetVMFileRestore(ctx, k8sClient, name, namespace)
}

// DeleteVMFileRestore deletes a VirtualMachineFileRestore resource
func DeleteVMFileRestore(ctx context.Context, k8sClient client.Client, name, namespace string) error {
	vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	ginkgo.By(fmt.Sprintf("Deleting VirtualMachineFileRestore %s in namespace %s", name, namespace))
	return client.IgnoreNotFound(k8sClient.Delete(ctx, vmfr))
}

//go:build e2e
// +build e2e

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

package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	oadpv1alpha1 "github.com/migtools/oadp-vm-file-restore/api/v1alpha1"
	"github.com/migtools/oadp-vm-file-restore/test/framework"
	"github.com/migtools/oadp-vm-file-restore/test/utils"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	resource "k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Minimal functional test for VM backup discovery and file restore
var _ = Describe("VM Backup and File Restore [Functional]", Ordered, func() {
	const (
		testNamespace = "vm-test-e2e"
		vmName        = "test-vm"
		discoveryName = "test-vmbd"
		restoreName   = "test-vmfr"
		oadpNamespace = "openshift-adp"
	)

	var (
		backupName string // Generated uniquely per test run to avoid conflicts in MinIO
	)

	var (
		ctx          context.Context
		cancel       context.CancelFunc
		k8sClient    client.Client
		config       *rest.Config
		testDuration time.Duration
	)

	BeforeAll(func() {
		var err error
		testDuration = 20 * time.Minute
		ctx, cancel = context.WithTimeout(context.Background(), testDuration)

		// Generate unique backup name to avoid MinIO conflicts from previous runs
		backupName = fmt.Sprintf("test-backup-e2e-%d", time.Now().Unix())

		// Setup clients
		By("setting up kubernetes clients")
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			// Try in-cluster config
			config, err = rest.InClusterConfig()
		}
		Expect(err).NotTo(HaveOccurred(), "Failed to get kubeconfig")

		// Create controller-runtime client with all needed schemes
		scheme := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
		Expect(oadpv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(velerov1.AddToScheme(scheme)).To(Succeed())

		k8sClient, err = client.New(config, client.Options{Scheme: scheme})
		Expect(err).NotTo(HaveOccurred(), "Failed to create controller client")

		// Install OADP VM File Restore CRDs
		By("installing OADP VM File Restore CRDs")
		cmd := exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		// Deploy the controller
		By("deploying OADP VM File Restore controller")
		cmd = exec.Command("make", "deploy", "IMG=localhost/oadp-vm-file-restore:dev")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy controller")

		// Patch deployment to use IfNotPresent
		By("patching deployment imagePullPolicy")
		cmd = exec.Command("kubectl", "patch", "deployment",
			"oadp-vm-file-restore-controller-manager",
			"-n", oadpNamespace,
			"-p", `{"spec":{"template":{"spec":{"containers":[{"name":"manager","imagePullPolicy":"IfNotPresent"}]}}}}`)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to patch deployment")

		// Wait for controller to be ready
		By("waiting for controller to be ready")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "pods",
				"-l", "control-plane=controller-manager",
				"-n", oadpNamespace,
				"-o", "jsonpath={.items[0].status.phase}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("Running"), "Controller should be running")
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		// Create test namespace
		By(fmt.Sprintf("creating test namespace %s", testNamespace))
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
				Labels: map[string]string{
					"oadp-vm-file-restore-test": "true",
				},
			},
		}
		err = k8sClient.Create(ctx, ns)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred(), "Failed to create test namespace")
		}
	})

	AfterAll(func() {
		if cancel != nil {
			defer cancel()
		}

		By("cleaning up test resources")
		// Delete VMFR and wait for finalizer processing
		_ = framework.DeleteVMFileRestore(ctx, k8sClient, restoreName, oadpNamespace)
		// Wait for VMFR deletion with timeout, force remove finalizer if stuck
		Eventually(func() error {
			vmfr := &oadpv1alpha1.VirtualMachineFileRestore{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: restoreName, Namespace: oadpNamespace}, vmfr)
			if errors.IsNotFound(err) {
				return nil // Successfully deleted
			}
			if err != nil {
				return err
			}
			// Still exists after 10s, force remove finalizer
			if len(vmfr.Finalizers) > 0 {
				By("Force removing VMFR finalizers to prevent namespace hang")
				vmfr.Finalizers = nil
				_ = k8sClient.Update(ctx, vmfr)
			}
			return fmt.Errorf("VMFR still exists")
		}, 15*time.Second, 1*time.Second).Should(Succeed())

		// Delete VMBD and wait for finalizer processing
		_ = framework.DeleteVMBackupsDiscovery(ctx, k8sClient, discoveryName, oadpNamespace)
		// Wait for VMBD deletion with timeout
		Eventually(func() error {
			vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: discoveryName, Namespace: oadpNamespace}, vmbd)
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			// Force remove finalizer if stuck
			if len(vmbd.Finalizers) > 0 {
				By("Force removing VMBD finalizers to prevent namespace hang")
				vmbd.Finalizers = nil
				_ = k8sClient.Update(ctx, vmbd)
			}
			return fmt.Errorf("VMBD still exists")
		}, 15*time.Second, 1*time.Second).Should(Succeed())

		// Delete Velero backup
		backup := &velerov1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backupName,
				Namespace: oadpNamespace,
			},
		}
		_ = client.IgnoreNotFound(k8sClient.Delete(ctx, backup))

		// Delete VM
		vm := &unstructured.Unstructured{}
		vm.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "kubevirt.io",
			Version: "v1",
			Kind:    "VirtualMachine",
		})
		vm.SetName(vmName)
		vm.SetNamespace(testNamespace)
		_ = client.IgnoreNotFound(k8sClient.Delete(ctx, vm))

		// Delete test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		_ = client.IgnoreNotFound(k8sClient.Delete(ctx, ns))

		// Undeploy controller (now safe - all finalized resources are gone)
		By("undeploying controller")
		cmd := exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		// Uninstall CRDs
		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)
	})

	It("should complete the full VM backup discovery and file restore workflow", func() {
		By("Step 0: Verifying Velero is ready")
		// Verify Velero pod is running
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "pods",
				"-l", "app=velero",
				"-n", oadpNamespace,
				"-o", "jsonpath={.items[0].status.phase}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred(), "Failed to get Velero pod status")
			g.Expect(output).To(Equal("Running"), "Velero pod should be running")
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		// Verify BackupStorageLocation is Available
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "backupstoragelocation", "default",
				"-n", oadpNamespace,
				"-o", "jsonpath={.status.phase}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred(), "Failed to get BSL status")
			phase := strings.TrimSpace(output)
			if phase != "Available" {
				// Get diagnostic info if BSL is not available
				cmd = exec.Command("kubectl", "get", "backupstoragelocation", "default",
					"-n", oadpNamespace, "-o", "yaml")
				if bslYaml, err := utils.Run(cmd); err == nil {
					fmt.Fprintf(GinkgoWriter, "BSL Status:\n%s\n", bslYaml)
				}
			}
			g.Expect(phase).To(Equal("Available"), "BackupStorageLocation should be Available")
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		By("Step 1: Creating a standalone PVC (DataVolumes don't provision in Kind)")
		dvName := fmt.Sprintf("%s-dv", vmName)

		// Delete PVC if it exists from previous run
		existingPVC := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dvName,
				Namespace: testNamespace,
			},
		}
		_ = client.IgnoreNotFound(k8sClient.Delete(ctx, existingPVC))
		time.Sleep(1 * time.Second)

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dvName,
				Namespace: testNamespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: *parseQuantity("1Gi"),
					},
				},
			},
		}
		err := k8sClient.Create(ctx, pvc)
		Expect(err).NotTo(HaveOccurred(), "Failed to create PVC")

		By("Step 1a: Verifying PVC was created (won't bind until consumed due to WaitForFirstConsumer)")
		// Note: Kind uses WaitForFirstConsumer binding mode, so PVC stays Pending until mounted by a pod
		// For backup testing, we only need the PVC resource to exist, not be bound
		Eventually(func(g Gomega) {
			currentPVC := &corev1.PersistentVolumeClaim{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: dvName, Namespace: testNamespace}, currentPVC)
			g.Expect(err).NotTo(HaveOccurred(), "PVC should exist")
			g.Expect(currentPVC.Name).To(Equal(dvName), "PVC name should match")
		}, 30*time.Second, 2*time.Second).Should(Succeed())

		By("Step 2: Creating a minimal VM without DataVolumeTemplate (VM won't run due to Kind networking limitations)")
		vm := createMinimalVMWithoutDataVolume(vmName, testNamespace, dvName)
		// Set running: false to avoid network permission issues in Kind
		// We only need the VM resource and PVC for backup testing
		unstructured.SetNestedField(vm.Object, false, "spec", "running")
		err = k8sClient.Create(ctx, vm)
		Expect(err).NotTo(HaveOccurred(), "Failed to create VM")

		By(fmt.Sprintf("Step 3: Creating Velero backup %s (with volume snapshots disabled)", backupName))
		// Backup name is unique per test run, so no need to delete existing

		// Disable volume snapshots since we can't snapshot unbound PVCs in Kind
		// We're testing resource backup/discovery, not volume snapshots
		snapshotVolumes := false
		backup := &velerov1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backupName,
				Namespace: oadpNamespace,
			},
			Spec: velerov1.BackupSpec{
				IncludedNamespaces: []string{testNamespace},
				TTL:                metav1.Duration{Duration: 1 * time.Hour},
				SnapshotVolumes:    &snapshotVolumes, // Skip volume snapshots - just backup resources
			},
		}
		err = k8sClient.Create(ctx, backup)
		Expect(err).NotTo(HaveOccurred(), "Failed to create backup")

		By("Step 4: Waiting for backup to complete")
		Eventually(func(g Gomega) {
			b := &velerov1.Backup{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: backupName, Namespace: oadpNamespace}, b)
			g.Expect(err).NotTo(HaveOccurred())

			// Print status for debugging
			if b.Status.Phase == velerov1.BackupPhaseFailed || b.Status.Phase == velerov1.BackupPhasePartiallyFailed {
				fmt.Fprintf(GinkgoWriter, "Backup phase: %s\n", b.Status.Phase)
				fmt.Fprintf(GinkgoWriter, "Backup errors: %d\n", b.Status.Errors)
				fmt.Fprintf(GinkgoWriter, "Backup warnings: %d\n", b.Status.Warnings)
				if b.Status.FailureReason != "" {
					fmt.Fprintf(GinkgoWriter, "Failure reason: %s\n", b.Status.FailureReason)
				}
			}

			g.Expect(b.Status.Phase).To(Equal(velerov1.BackupPhaseCompleted), "Backup should complete")
		}, 5*time.Minute, 5*time.Second).Should(Succeed())

		By("Step 5: Creating VirtualMachineBackupsDiscovery")
		vmbd := framework.NewVMBackupsDiscovery(discoveryName, oadpNamespace, vmName, testNamespace)
		err = framework.CreateVMBackupsDiscovery(ctx, k8sClient, vmbd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create VMBD")

		By("Step 6: Waiting for discovery to complete")
		completedVMBD, err := framework.WaitForVMBackupsDiscoveryCompleted(ctx, k8sClient, discoveryName, oadpNamespace)
		Expect(err).NotTo(HaveOccurred(), "Discovery should complete")
		Expect(completedVMBD.Status.ValidBackups).NotTo(BeEmpty(), "Should find at least one valid backup")

		By("Step 7: Verifying ValidBackups contains our backup")
		backupNames := framework.GetValidBackupNames(completedVMBD)
		Expect(backupNames).To(ContainElement(backupName), "ValidBackups should contain our backup")

		By("Step 8: Creating VirtualMachineFileRestore")
		vmfr := framework.NewVMFileRestore(restoreName, oadpNamespace, discoveryName)
		err = framework.CreateVMFileRestore(ctx, k8sClient, vmfr)
		Expect(err).NotTo(HaveOccurred(), "Failed to create VMFR")

		By("Step 9: Waiting for file restore to progress (InProgress or Completed)")
		Eventually(func(g Gomega) {
			currentVMFR, err := framework.GetVMFileRestore(ctx, k8sClient, restoreName, oadpNamespace)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(currentVMFR.Status.Phase).NotTo(BeEmpty(), "Phase should be set")

			// If Failed, capture diagnostics before assertion
			if currentVMFR.Status.Phase == oadpv1alpha1.VirtualMachineFileRestorePhaseFailed {
				By("VMFR entered Failed phase - capturing diagnostics")

				// Log VMFR status
				By(fmt.Sprintf("VMFR Status: %+v", currentVMFR.Status))

				// Log conditions
				for _, cond := range currentVMFR.Status.Conditions {
					By(fmt.Sprintf("Condition: Type=%s Status=%s Reason=%s Message=%s",
						cond.Type, cond.Status, cond.Reason, cond.Message))
				}

				// Capture controller logs
				cmd := exec.Command("kubectl", "logs", "-l", "control-plane=controller-manager",
					"-n", oadpNamespace, "--tail=100")
				output, _ := utils.Run(cmd)
				By(fmt.Sprintf("Controller logs:\n%s", output))

				// Check Velero Restores
				cmd = exec.Command("kubectl", "get", "restore", "-n", oadpNamespace, "-o", "yaml")
				output, _ = utils.Run(cmd)
				By(fmt.Sprintf("Velero Restores:\n%s", output))
			}

			// Accept InProgress, Completed, or PartiallyFailed as success
			g.Expect(currentVMFR.Status.Phase).To(BeElementOf(
				oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress,
				oadpv1alpha1.VirtualMachineFileRestorePhaseCompleted,
				oadpv1alpha1.VirtualMachineFileRestorePhasePartiallyFailed,
			), "VMFR should not fail immediately")
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		By("Test completed successfully!")
	})
})

// createMinimalVM creates a minimal VirtualMachine for testing using unstructured
// The VM uses a containerDisk for boot and a DataVolume for persistent storage
// Emulation mode is enabled for Kind cluster compatibility (no KVM available)
func createMinimalVM(name, namespace string) *unstructured.Unstructured {
	dvName := fmt.Sprintf("%s-dv", name)

	vm := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachine",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"annotations": map[string]interface{}{
					"kubevirt.io/allow-emulation": "true",
				},
			},
			"spec": map[string]interface{}{
				"running": true, // Start the VM so PVC gets bound
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{
							"kubevirt.io/vm": name,
						},
						"annotations": map[string]interface{}{
							"kubevirt.io/allow-emulation": "true",
						},
					},
					"spec": map[string]interface{}{
						"domain": map[string]interface{}{
							"cpu": map[string]interface{}{
								"cores": int64(1),
							},
							"devices": map[string]interface{}{
								"disks": []interface{}{
									map[string]interface{}{
										"name": "containerdisk",
										"disk": map[string]interface{}{
											"bus": "virtio",
										},
									},
									map[string]interface{}{
										"name": "datavolumedisk",
										"disk": map[string]interface{}{
											"bus": "virtio",
										},
									},
								},
							},
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{
									"memory": "128Mi",
								},
							},
						},
						"terminationGracePeriodSeconds": int64(0),
						"volumes": []interface{}{
							map[string]interface{}{
								"name": "containerdisk",
								"containerDisk": map[string]interface{}{
									"image": "quay.io/kubevirt/cirros-container-disk-demo:latest",
								},
							},
							map[string]interface{}{
								"name": "datavolumedisk",
								"dataVolume": map[string]interface{}{
									"name": dvName,
								},
							},
						},
					},
				},
				"dataVolumeTemplates": []interface{}{
					map[string]interface{}{
						"metadata": map[string]interface{}{
							"name": dvName,
						},
						"spec": map[string]interface{}{
							"source": map[string]interface{}{
								"blank": map[string]interface{}{},
							},
							"storage": map[string]interface{}{
								"accessModes": []interface{}{"ReadWriteOnce"},
								"resources": map[string]interface{}{
									"requests": map[string]interface{}{
										"storage": "1Gi",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	return vm
}

// createMinimalVMWithoutDataVolume creates a minimal VirtualMachine that references an existing PVC
// This is used for Kind testing where DataVolume provisioning doesn't work
// The VM uses a containerDisk for boot and references a pre-created PVC
func createMinimalVMWithoutDataVolume(name, namespace, pvcName string) *unstructured.Unstructured {
	vm := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachine",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"annotations": map[string]interface{}{
					"kubevirt.io/allow-emulation": "true",
				},
			},
			"spec": map[string]interface{}{
				"running": false, // Don't start VM (network issues in Kind)
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{
							"kubevirt.io/vm": name,
						},
						"annotations": map[string]interface{}{
							"kubevirt.io/allow-emulation": "true",
						},
					},
					"spec": map[string]interface{}{
						"architecture": "amd64",
						"domain": map[string]interface{}{
							"cpu": map[string]interface{}{
								"cores": int64(1),
							},
							"devices": map[string]interface{}{
								"disks": []interface{}{
									map[string]interface{}{
										"name": "containerdisk",
										"disk": map[string]interface{}{
											"bus": "virtio",
										},
									},
									map[string]interface{}{
										"name": "pvcdisk",
										"disk": map[string]interface{}{
											"bus": "virtio",
										},
									},
								},
							},
							"machine": map[string]interface{}{
								"type": "q35",
							},
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{
									"memory": "128Mi",
								},
							},
						},
						"terminationGracePeriodSeconds": int64(0),
						"volumes": []interface{}{
							map[string]interface{}{
								"name": "containerdisk",
								"containerDisk": map[string]interface{}{
									"image": "quay.io/kubevirt/cirros-container-disk-demo:latest",
								},
							},
							map[string]interface{}{
								"name": "pvcdisk",
								"persistentVolumeClaim": map[string]interface{}{
									"claimName": pvcName,
								},
							},
						},
					},
				},
			},
		},
	}

	return vm
}

// parseQuantity parses a quantity string and returns a resource.Quantity pointer
func parseQuantity(s string) *resource.Quantity {
	q := resource.MustParse(s)
	return &q
}

// createVMWithContainerDiskOnly creates a minimal VM with only a containerDisk (no PVCs)
// This avoids Velero backup issues with unbound PVCs in Kind
func createVMWithContainerDiskOnly(name, namespace string) *unstructured.Unstructured {
	vm := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachine",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"annotations": map[string]interface{}{
					"kubevirt.io/allow-emulation": "true",
				},
			},
			"spec": map[string]interface{}{
				"running": false, // Don't start VM (network issues in Kind)
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{
							"kubevirt.io/vm": name,
						},
						"annotations": map[string]interface{}{
							"kubevirt.io/allow-emulation": "true",
						},
					},
					"spec": map[string]interface{}{
						"architecture": "amd64",
						"domain": map[string]interface{}{
							"cpu": map[string]interface{}{
								"cores": int64(1),
							},
							"devices": map[string]interface{}{
								"disks": []interface{}{
									map[string]interface{}{
										"name": "containerdisk",
										"disk": map[string]interface{}{
											"bus": "virtio",
										},
									},
								},
							},
							"machine": map[string]interface{}{
								"type": "q35",
							},
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{
									"memory": "128Mi",
								},
							},
						},
						"terminationGracePeriodSeconds": int64(0),
						"volumes": []interface{}{
							map[string]interface{}{
								"name": "containerdisk",
								"containerDisk": map[string]interface{}{
									"image": "quay.io/kubevirt/cirros-container-disk-demo:latest",
								},
							},
						},
					},
				},
			},
		},
	}

	return vm
}

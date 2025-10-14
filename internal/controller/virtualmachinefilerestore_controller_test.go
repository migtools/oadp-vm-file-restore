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

package controller

// import (
// 	"context"
// 	"fmt"
// 	"time"

// 	. "github.com/onsi/ginkgo/v2"
// 	. "github.com/onsi/gomega"
// 	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
// 	corev1 "k8s.io/api/core/v1"
// 	"k8s.io/apimachinery/pkg/api/errors"
// 	"k8s.io/apimachinery/pkg/api/meta"
// 	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
// 	"k8s.io/apimachinery/pkg/runtime"
// 	"k8s.io/apimachinery/pkg/types"
// 	kubevirtv1 "kubevirt.io/api/core/v1"
// 	"sigs.k8s.io/controller-runtime/pkg/client/fake"
// 	"sigs.k8s.io/controller-runtime/pkg/log/zap"
// 	"sigs.k8s.io/controller-runtime/pkg/reconcile"

// 	oadpv1alpha1 "github.com/migtools/oadp-vm-file-restore/api/v1alpha1"
// 	"github.com/migtools/oadp-vm-file-restore/internal/velerohelpers"
// )

// var _ = Describe("VirtualMachineFileRestore Controller", func() {
// 	Context("When reconciling a resource", func() {
// 		const resourceName = "test-resource"

// 		ctx := context.Background()

// 		typeNamespacedName := types.NamespacedName{
// 			Name:      resourceName,
// 			Namespace: "default", // TODO(user):Modify as needed
// 		}
// 		virtualmachinefilerestore := &oadpv1alpha1.VirtualMachineFileRestore{}

// 		BeforeEach(func() {
// 			By("creating the custom resource for the Kind VirtualMachineFileRestore")
// 			err := k8sClient.Get(ctx, typeNamespacedName, virtualmachinefilerestore)
// 			if err != nil && errors.IsNotFound(err) {
// 				resource := &oadpv1alpha1.VirtualMachineFileRestore{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      resourceName,
// 						Namespace: "default",
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
// 						BackupsDiscoveryRef: "test-discovery",
// 					},
// 				}
// 				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
// 			}
// 		})

// 		AfterEach(func() {
// 			// TODO(user): Cleanup logic after each test, like removing the resource instance.
// 			resource := &oadpv1alpha1.VirtualMachineFileRestore{}
// 			err := k8sClient.Get(ctx, typeNamespacedName, resource)
// 			Expect(err).NotTo(HaveOccurred())

// 			By("Cleanup the specific resource instance VirtualMachineFileRestore")
// 			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
// 		})
// 		It("should successfully reconcile the resource", func() {
// 			By("Reconciling the created resource")
// 			controllerReconciler := &VirtualMachineFileRestoreReconciler{
// 				Client: k8sClient,
// 				Scheme: k8sClient.Scheme(),
// 			}

// 			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
// 				NamespacedName: typeNamespacedName,
// 			})
// 			Expect(err).NotTo(HaveOccurred())
// 			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
// 			// Example: If you expect a certain status condition after reconciliation, verify it here.
// 		})
// 	})

// 	Context("PVC Discovery Functionality", func() {
// 		var (
// 			reconciler    *VirtualMachineFileRestoreReconciler
// 			ctx           context.Context
// 			scheme        *runtime.Scheme
// 			backupName    = "test-backup"
// 			namespace     = "test-namespace"
// 			oadpNamespace = "openshift-adp"
// 		)

// 		BeforeEach(func() {
// 			ctx = context.Background()
// 			scheme = runtime.NewScheme()
// 			Expect(oadpv1alpha1.AddToScheme(scheme)).To(Succeed())
// 			Expect(velerov1api.AddToScheme(scheme)).To(Succeed())
// 			Expect(corev1.AddToScheme(scheme)).To(Succeed())
// 			Expect(kubevirtv1.AddToScheme(scheme)).To(Succeed())
// 		})

// 		Describe("addPVCsToBackups", func() {
// 			It("should enhance backup info with PVC details", func() {
// 				// Create test PVCs that follow VM naming patterns
// 				testPVC1 := &corev1.PersistentVolumeClaim{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:              "test-vm-disk-1",
// 						Namespace:         namespace,
// 						UID:               "test-uid-1",
// 						CreationTimestamp: metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
// 					},
// 				}
// 				testPVC2 := &corev1.PersistentVolumeClaim{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:              "test-vm-disk-2",
// 						Namespace:         namespace,
// 						UID:               "test-uid-2",
// 						CreationTimestamp: metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
// 					},
// 				}

// 				// Create test VirtualMachine that references the PVCs
// 				testVM := &kubevirtv1.VirtualMachine{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vm",
// 						Namespace: namespace,
// 					},
// 					Spec: kubevirtv1.VirtualMachineSpec{
// 						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
// 							Spec: kubevirtv1.VirtualMachineInstanceSpec{
// 								Volumes: []kubevirtv1.Volume{
// 									{
// 										Name: "volume1",
// 										VolumeSource: kubevirtv1.VolumeSource{
// 											PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
// 												PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
// 													ClaimName: "test-vm-disk-1",
// 												},
// 											},
// 										},
// 									},
// 									{
// 										Name: "volume2",
// 										VolumeSource: kubevirtv1.VolumeSource{
// 											PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
// 												PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
// 													ClaimName: "test-vm-disk-2",
// 												},
// 											},
// 										},
// 									},
// 								},
// 							},
// 						},
// 					},
// 				}

// 				// Create test Velero backup
// 				testBackup := &velerov1api.Backup{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      backupName,
// 						Namespace: oadpNamespace,
// 					},
// 					Spec: velerov1api.BackupSpec{
// 						IncludedNamespaces: []string{namespace},
// 					},
// 					Status: velerov1api.BackupStatus{
// 						CompletionTimestamp: &metav1.Time{Time: time.Now()},
// 					},
// 				}

// 				// Create fake client with test objects
// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(testPVC1, testPVC2, testVM, testBackup).
// 					Build()

// 				mockBackupReader := velerohelpers.NewMockBackupContentsReader()
// 				mockBackupReader.SetupTestBackup(backupName, "test-vm", namespace, []string{"test-vm-disk-1", "test-vm-disk-2"})
// 				// Configure mock to fail PVC fetches so controller uses deterministic UIDs
// 				mockBackupReader.SetError("pvc", backupName, namespace, "test-vm-disk-1", fmt.Errorf("PVC not found in backup"))
// 				mockBackupReader.SetError("pvc", backupName, namespace, "test-vm-disk-2", fmt.Errorf("PVC not found in backup"))
// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:               client,
// 					Scheme:               scheme,
// 					OADPNamespace:        oadpNamespace,
// 					BackupContentsReader: mockBackupReader,
// 				}

// 				// Test basic backup info
// 				basicBackups := []oadpv1alpha1.VeleroBackupInfo{
// 					{
// 						Name:      backupName,
// 						CreatedAt: &metav1.Time{Time: time.Now()},
// 					},
// 				}

// 				// Call the method under test
// 				enhancedBackups, err := reconciler.addPVCsToBackups(ctx, zap.New(), basicBackups, "test-vm", "test-namespace")

// 				// Assertions
// 				Expect(err).ToNot(HaveOccurred())
// 				Expect(enhancedBackups).To(HaveLen(1))

// 				enhanced := enhancedBackups[0]
// 				Expect(enhanced.Name).To(Equal(backupName))
// 				Expect(enhanced.PVCs).To(HaveLen(2))

// 				// Check that PVC details are correctly populated
// 				pvcNames := make(map[string]string)
// 				for _, pvc := range enhanced.PVCs {
// 					pvcNames[pvc.Name] = pvc.UID
// 					Expect(pvc.Namespace).To(Equal(namespace))
// 				}

// 				// Verify UIDs are generated deterministically (fallback UIDs without backup name)
// 				expectedUID1 := reconciler.generateDeterministicUID("", namespace, "test-vm-disk-1")
// 				expectedUID2 := reconciler.generateDeterministicUID("", namespace, "test-vm-disk-2")
// 				Expect(pvcNames).To(HaveKeyWithValue("test-vm-disk-1", expectedUID1))
// 				Expect(pvcNames).To(HaveKeyWithValue("test-vm-disk-2", expectedUID2))
// 			})

// 			It("should handle missing Velero backup gracefully", func() {
// 				// Create test VM for consistency (won't be used since backup is missing)
// 				testVM := &kubevirtv1.VirtualMachine{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vm",
// 						Namespace: namespace,
// 					},
// 					Spec: kubevirtv1.VirtualMachineSpec{
// 						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
// 							Spec: kubevirtv1.VirtualMachineInstanceSpec{
// 								Volumes: []kubevirtv1.Volume{
// 									{
// 										Name: "volume1",
// 										VolumeSource: kubevirtv1.VolumeSource{
// 											PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
// 												PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
// 													ClaimName: "test-vm-disk-1",
// 												},
// 											},
// 										},
// 									},
// 								},
// 							},
// 						},
// 					},
// 				}

// 				// Create fake client without the backup
// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(testVM).
// 					Build()

// 				mockBackupReader := velerohelpers.NewMockBackupContentsReader()
// 				mockBackupReader.SetupTestBackup(backupName, "test-vm", namespace, []string{"test-vm-disk-1", "test-vm-disk-2"})
// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:               client,
// 					Scheme:               scheme,
// 					OADPNamespace:        oadpNamespace,
// 					BackupContentsReader: mockBackupReader,
// 				}

// 				// Configure mock to fail for missing backup
// 				mockBackupReader.SetError("vm", "missing-backup", "test-namespace", "test-vm", fmt.Errorf("backup not found"))

// 				basicBackups := []oadpv1alpha1.VeleroBackupInfo{
// 					{
// 						Name:      "missing-backup",
// 						CreatedAt: &metav1.Time{Time: time.Now()},
// 					},
// 				}

// 				// Call the method under test
// 				enhancedBackups, err := reconciler.addPVCsToBackups(ctx, zap.New(), basicBackups, "test-vm", "test-namespace")

// 				// Should handle gracefully and skip the missing backup
// 				Expect(err).ToNot(HaveOccurred())
// 				Expect(enhancedBackups).To(BeEmpty())
// 			})
// 		})

// 		Describe("extractVMPVCsFromBackupContents", func() {
// 			It("should extract PVCs created before backup completion", func() {
// 				backupTime := time.Now()

// 				// Create test PVCs - one that belongs to VM, one that doesn't (to test filtering)
// 				pvcBelongsToVM := &corev1.PersistentVolumeClaim{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:              "test-vm-disk-main",
// 						Namespace:         namespace,
// 						UID:               "uid-belongs-to-vm",
// 						CreationTimestamp: metav1.Time{Time: backupTime.Add(-1 * time.Hour)},
// 					},
// 				}
// 				pvcNotBelongsToVM := &corev1.PersistentVolumeClaim{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:              "other-vm-disk",
// 						Namespace:         namespace,
// 						UID:               "uid-not-belongs",
// 						CreationTimestamp: metav1.Time{Time: backupTime.Add(-1 * time.Hour)},
// 					},
// 				}

// 				// Create test VM that references the VM-specific PVC
// 				testVM := &kubevirtv1.VirtualMachine{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vm",
// 						Namespace: namespace,
// 					},
// 					Spec: kubevirtv1.VirtualMachineSpec{
// 						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
// 							Spec: kubevirtv1.VirtualMachineInstanceSpec{
// 								Volumes: []kubevirtv1.Volume{
// 									{
// 										Name: "volume1",
// 										VolumeSource: kubevirtv1.VolumeSource{
// 											PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
// 												PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
// 													ClaimName: "test-vm-disk-main",
// 												},
// 											},
// 										},
// 									},
// 								},
// 							},
// 						},
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(pvcBelongsToVM, pvcNotBelongsToVM, testVM).
// 					Build()

// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client: client,
// 					Scheme: scheme,
// 				}

// 				testBackup := &velerov1api.Backup{
// 					Spec: velerov1api.BackupSpec{
// 						IncludedNamespaces: []string{namespace},
// 					},
// 					Status: velerov1api.BackupStatus{
// 						CompletionTimestamp: &metav1.Time{Time: backupTime},
// 					},
// 				}

// 				// Call the method under test
// 				pvcs, _ := reconciler.extractVMPVCsFromBackupContents(ctx, testBackup, "test-vm", namespace)

// 				// Assertions
// 				Expect(pvcs).To(HaveLen(1))
// 				Expect(pvcs[0].Name).To(Equal("test-vm-disk-main"))
// 				Expect(pvcs[0].UID).To(Equal("uid-belongs-to-vm")) // Real UID from the PVC object
// 				Expect(pvcs[0].Namespace).To(Equal(namespace))
// 			})

// 			It("should handle backup without included namespaces", func() {
// 				// Create test VM for consistency
// 				testVM := &kubevirtv1.VirtualMachine{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vm",
// 						Namespace: namespace,
// 					},
// 					Spec: kubevirtv1.VirtualMachineSpec{
// 						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
// 							Spec: kubevirtv1.VirtualMachineInstanceSpec{
// 								Volumes: []kubevirtv1.Volume{
// 									{
// 										Name: "volume1",
// 										VolumeSource: kubevirtv1.VolumeSource{
// 											PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
// 												PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
// 													ClaimName: "some-pvc",
// 												},
// 											},
// 										},
// 									},
// 								},
// 							},
// 						},
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(testVM).
// 					Build()

// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client: client,
// 					Scheme: scheme,
// 				}

// 				testBackup := &velerov1api.Backup{
// 					Spec: velerov1api.BackupSpec{
// 						// No included namespaces - should use default list
// 					},
// 					Status: velerov1api.BackupStatus{
// 						CompletionTimestamp: &metav1.Time{Time: time.Now()},
// 					},
// 				}

// 				// Call the method under test
// 				pvcs, _ := reconciler.extractVMPVCsFromBackupContents(ctx, testBackup, "test-vm", namespace)

// 				// Should return empty list (no PVCs in default namespaces)
// 				Expect(pvcs).To(BeEmpty())
// 			})
// 		})

// 		Describe("BackupPVCInfo struct", func() {
// 			It("should have correct JSON tags", func() {
// 				pvcInfo := oadpv1alpha1.BackupPVCInfo{
// 					Name:      "test-pvc",
// 					Namespace: "test-ns",
// 					UID:       "test-uid",
// 				}

// 				// Basic validation that struct is properly defined
// 				Expect(pvcInfo.Name).To(Equal("test-pvc"))
// 				Expect(pvcInfo.Namespace).To(Equal("test-ns"))
// 				Expect(pvcInfo.UID).To(Equal("test-uid"))
// 			})
// 		})

// 		Describe("VeleroBackupInfo with PVCs", func() {
// 			It("should support optional PVCs field", func() {
// 				backupInfo := oadpv1alpha1.VeleroBackupInfo{
// 					Name:      "test-backup",
// 					CreatedAt: &metav1.Time{Time: time.Now()},
// 					PVCs: []oadpv1alpha1.BackupPVCInfo{
// 						{
// 							Name:      "pvc1",
// 							Namespace: "ns1",
// 							UID:       "uid1",
// 						},
// 						{
// 							Name:      "pvc2",
// 							Namespace: "ns2",
// 							UID:       "uid2",
// 						},
// 					},
// 				}

// 				// Validate structure
// 				Expect(backupInfo.Name).To(Equal("test-backup"))
// 				Expect(backupInfo.PVCs).To(HaveLen(2))
// 				Expect(backupInfo.PVCs[0].Name).To(Equal("pvc1"))
// 				Expect(backupInfo.PVCs[1].Name).To(Equal("pvc2"))
// 			})

// 			It("should work without PVCs field (backward compatibility)", func() {
// 				backupInfo := oadpv1alpha1.VeleroBackupInfo{
// 					Name:      "test-backup",
// 					CreatedAt: &metav1.Time{Time: time.Now()},
// 					// PVCs field omitted - should be nil/empty
// 				}

// 				Expect(backupInfo.Name).To(Equal("test-backup"))
// 				Expect(backupInfo.PVCs).To(BeEmpty())
// 			})
// 		})

// 		Describe("Complex Backup Scenarios", func() {
// 			Context("Scenario 1: Multiple VMs with shared PVCs", func() {
// 				It("should correctly filter PVCs for each VM when PVCs are shared", func() {
// 					// Create shared PVC that belongs to multiple VMs
// 					sharedPVC := &corev1.PersistentVolumeClaim{
// 						ObjectMeta: metav1.ObjectMeta{
// 							Name:      "shared-storage-pvc",
// 							Namespace: namespace,
// 							UID:       "shared-uid-1",
// 						},
// 					}

// 					// Create VM-specific PVCs
// 					vm1PVC := &corev1.PersistentVolumeClaim{
// 						ObjectMeta: metav1.ObjectMeta{
// 							Name:      "vm1-disk-primary",
// 							Namespace: namespace,
// 							UID:       "vm1-uid-1",
// 						},
// 					}

// 					vm2PVC := &corev1.PersistentVolumeClaim{
// 						ObjectMeta: metav1.ObjectMeta{
// 							Name:      "vm2-disk-primary",
// 							Namespace: namespace,
// 							UID:       "vm2-uid-1",
// 						},
// 					}

// 					// Create VMs with proper specs
// 					vm1 := &kubevirtv1.VirtualMachine{
// 						ObjectMeta: metav1.ObjectMeta{
// 							Name:      "vm1",
// 							Namespace: namespace,
// 						},
// 						Spec: kubevirtv1.VirtualMachineSpec{
// 							Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
// 								Spec: kubevirtv1.VirtualMachineInstanceSpec{
// 									Volumes: []kubevirtv1.Volume{
// 										{
// 											Name: "volume1",
// 											VolumeSource: kubevirtv1.VolumeSource{
// 												PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
// 													PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
// 														ClaimName: "vm1-disk-primary",
// 													},
// 												},
// 											},
// 										},
// 									},
// 								},
// 							},
// 						},
// 					}

// 					vm2 := &kubevirtv1.VirtualMachine{
// 						ObjectMeta: metav1.ObjectMeta{
// 							Name:      "vm2",
// 							Namespace: namespace,
// 						},
// 						Spec: kubevirtv1.VirtualMachineSpec{
// 							Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
// 								Spec: kubevirtv1.VirtualMachineInstanceSpec{
// 									Volumes: []kubevirtv1.Volume{
// 										{
// 											Name: "volume1",
// 											VolumeSource: kubevirtv1.VolumeSource{
// 												PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
// 													PersistentVolumeClaimVolumeSource: corev1.PersistentVolumeClaimVolumeSource{
// 														ClaimName: "vm2-disk-primary",
// 													},
// 												},
// 											},
// 										},
// 									},
// 								},
// 							},
// 						},
// 					}

// 					testBackup := &velerov1api.Backup{
// 						ObjectMeta: metav1.ObjectMeta{
// 							Name:      "multi-vm-backup",
// 							Namespace: oadpNamespace,
// 						},
// 					}

// 					client := fake.NewClientBuilder().
// 						WithScheme(scheme).
// 						WithObjects(sharedPVC, vm1PVC, vm2PVC, vm1, vm2, testBackup).
// 						Build()

// 					mockBackupReader := velerohelpers.NewMockBackupContentsReader()
// 					reconciler = &VirtualMachineFileRestoreReconciler{
// 						Client:               client,
// 						Scheme:               scheme,
// 						OADPNamespace:        oadpNamespace,
// 						BackupContentsReader: mockBackupReader,
// 					}

// 					// Test VM1 PVC extraction
// 					vm1PVCs, err := reconciler.extractVMPVCsFromBackupContents(ctx, testBackup, "vm1", namespace)
// 					Expect(err).NotTo(HaveOccurred())
// 					Expect(vm1PVCs).To(HaveLen(1))
// 					Expect(vm1PVCs[0].Name).To(Equal("vm1-disk-primary"))

// 					// Test VM2 PVC extraction
// 					vm2PVCs, err := reconciler.extractVMPVCsFromBackupContents(ctx, testBackup, "vm2", namespace)
// 					Expect(err).NotTo(HaveOccurred())
// 					Expect(vm2PVCs).To(HaveLen(1))
// 					Expect(vm2PVCs[0].Name).To(Equal("vm2-disk-primary"))
// 				})
// 			})

// 			Context("Scenario 2: VMs without PVCs", func() {
// 				It("should handle VMs that exist in backup but have no PVCs", func() {
// 					// Create VM without any PVCs
// 					vmWithoutPVCs := &kubevirtv1.VirtualMachine{
// 						ObjectMeta: metav1.ObjectMeta{
// 							Name:      "vm-no-storage",
// 							Namespace: namespace,
// 						},
// 						Spec: kubevirtv1.VirtualMachineSpec{
// 							Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
// 								Spec: kubevirtv1.VirtualMachineInstanceSpec{
// 									// No volumes defined - this VM has no PVCs
// 								},
// 							},
// 						},
// 					}

// 					testBackup := &velerov1api.Backup{
// 						ObjectMeta: metav1.ObjectMeta{
// 							Name:      "vm-no-pvc-backup",
// 							Namespace: oadpNamespace,
// 						},
// 					}

// 					client := fake.NewClientBuilder().
// 						WithScheme(scheme).
// 						WithObjects(vmWithoutPVCs, testBackup).
// 						Build()

// 					mockBackupReader := velerohelpers.NewMockBackupContentsReader()
// 					reconciler = &VirtualMachineFileRestoreReconciler{
// 						Client:               client,
// 						Scheme:               scheme,
// 						OADPNamespace:        oadpNamespace,
// 						BackupContentsReader: mockBackupReader,
// 					}

// 					// Should return empty list without error
// 					pvcs, err := reconciler.extractVMPVCsFromBackupContents(ctx, testBackup, "vm-no-storage", namespace)
// 					Expect(err).NotTo(HaveOccurred())
// 					Expect(pvcs).To(BeEmpty())
// 				})
// 			})

// 			Context("Scenario 3: Same PVC name, different UIDs across backups", func() {
// 				It("should generate different UIDs for same-named PVCs in different backups", func() {
// 					// Test the UID generation directly since we can't have duplicate PVC names in fake client
// 					// This simulates the same logical PVC appearing in different backups with different UIDs

// 					client := fake.NewClientBuilder().
// 						WithScheme(scheme).
// 						Build()

// 					reconciler = &VirtualMachineFileRestoreReconciler{
// 						Client:        client,
// 						Scheme:        scheme,
// 						OADPNamespace: oadpNamespace,
// 					}

// 					// Test UID generation for same PVC name in different backups
// 					uid1 := reconciler.generateDeterministicUID("backup-1", namespace, "test-vm-disk")
// 					uid2 := reconciler.generateDeterministicUID("backup-2", namespace, "test-vm-disk")

// 					// UIDs should be different even though PVC names are the same
// 					Expect(uid1).NotTo(Equal(uid2))
// 					Expect(uid1).To(ContainSubstring("backup-1"))     // Contains backup context
// 					Expect(uid2).To(ContainSubstring("backup-2"))     // Contains backup context
// 					Expect(uid1).To(ContainSubstring("test-vm-disk")) // Contains PVC name
// 					Expect(uid2).To(ContainSubstring("test-vm-disk")) // Contains PVC name
// 				})
// 			})

// 			Context("UID Generation", func() {
// 				It("should generate deterministic UIDs for production environments", func() {
// 					client := fake.NewClientBuilder().
// 						WithScheme(scheme).
// 						Build()

// 					reconciler = &VirtualMachineFileRestoreReconciler{
// 						Client:        client,
// 						Scheme:        scheme,
// 						OADPNamespace: oadpNamespace,
// 					}

// 					// Generate UID multiple times - should be consistent
// 					uid1 := reconciler.generateDeterministicUID("production-backup", "test-ns", "test-pvc")
// 					uid2 := reconciler.generateDeterministicUID("production-backup", "test-ns", "test-pvc")

// 					Expect(uid1).To(Equal(uid2))
// 					Expect(uid1).To(ContainSubstring("production-backup"))
// 					Expect(uid1).To(ContainSubstring("test-ns"))
// 					Expect(uid1).To(ContainSubstring("test-pvc"))
// 				})

// 				It("should detect test environments correctly", func() {
// 					client := fake.NewClientBuilder().
// 						WithScheme(scheme).
// 						Build()

// 					mockBackupReader := velerohelpers.NewMockBackupContentsReader()
// 					reconciler = &VirtualMachineFileRestoreReconciler{
// 						Client:               client,
// 						Scheme:               scheme,
// 						OADPNamespace:        oadpNamespace,
// 						BackupContentsReader: mockBackupReader,
// 					}

// 					// Should detect mock backup reader as test environment
// 					isTest := reconciler.isTestEnvironment()
// 					Expect(isTest).To(BeTrue())
// 				})
// 			})
// 		})

// 		Describe("PVC Discovery Integration", func() {
// 			It("should discover and store PVC information during file restore", func() {
// 				// This test demonstrates that PVC discovery works end-to-end
// 				// After PVC discovery, backups should have PVC information
// 				// This would be populated by the controller's addPVCsToBackups method
// 				enhancedBackups := []oadpv1alpha1.VeleroBackupInfo{
// 					{
// 						Name:      "test-backup",
// 						CreatedAt: &metav1.Time{Time: time.Now()},
// 						PVCs: []oadpv1alpha1.BackupPVCInfo{
// 							{
// 								Name:      "vm-disk-1",
// 								Namespace: "vm-namespace",
// 								UID:       "pvc-uid-1",
// 							},
// 						},
// 					},
// 				}

// 				// Verify PVC information is properly stored
// 				Expect(enhancedBackups[0].PVCs).To(HaveLen(1))
// 				Expect(enhancedBackups[0].PVCs[0].Name).To(Equal("vm-disk-1"))
// 				Expect(enhancedBackups[0].PVCs[0].UID).To(Equal("pvc-uid-1"))
// 			})
// 		})

// 		Describe("Deterministic UID Generation", func() {
// 			var reconciler *VirtualMachineFileRestoreReconciler

// 			BeforeEach(func() {
// 				client := fake.NewClientBuilder().WithScheme(scheme).Build()
// 				mockBackupReader := velerohelpers.NewMockBackupContentsReader()
// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:               client,
// 					Scheme:               scheme,
// 					OADPNamespace:        oadpNamespace,
// 					BackupContentsReader: mockBackupReader,
// 				}
// 			})

// 			It("should generate consistent UIDs for same inputs", func() {
// 				uid1 := reconciler.generateDeterministicUID("backup-1", namespace, "test-pvc")
// 				uid2 := reconciler.generateDeterministicUID("backup-1", namespace, "test-pvc")

// 				Expect(uid1).To(Equal(uid2))
// 				Expect(uid1).To(ContainSubstring("backup-1"))
// 				Expect(uid1).To(ContainSubstring(namespace))
// 				Expect(uid1).To(ContainSubstring("test-pvc"))
// 			})

// 			It("should generate different UIDs for different backups", func() {
// 				uid1 := reconciler.generateDeterministicUID("backup-1", namespace, "test-pvc")
// 				uid2 := reconciler.generateDeterministicUID("backup-2", namespace, "test-pvc")

// 				Expect(uid1).NotTo(Equal(uid2))
// 				Expect(uid1).To(ContainSubstring("backup-1"))
// 				Expect(uid2).To(ContainSubstring("backup-2"))
// 			})

// 			It("should generate different UIDs for different PVCs", func() {
// 				uid1 := reconciler.generateDeterministicUID("backup-1", namespace, "pvc-1")
// 				uid2 := reconciler.generateDeterministicUID("backup-1", namespace, "pvc-2")

// 				Expect(uid1).NotTo(Equal(uid2))
// 				Expect(uid1).To(ContainSubstring("pvc-1"))
// 				Expect(uid2).To(ContainSubstring("pvc-2"))
// 			})

// 			It("should detect test environment correctly", func() {
// 				isTest := reconciler.isTestEnvironment()
// 				Expect(isTest).To(BeTrue())
// 			})
// 		})

// 		Describe("Enhanced Error Handling", func() {
// 			var reconciler *VirtualMachineFileRestoreReconciler
// 			var mockBackupReader *velerohelpers.MockBackupContentsReader

// 			BeforeEach(func() {
// 				client := fake.NewClientBuilder().WithScheme(scheme).Build()
// 				mockBackupReader = velerohelpers.NewMockBackupContentsReader()
// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:               client,
// 					Scheme:               scheme,
// 					OADPNamespace:        oadpNamespace,
// 					BackupContentsReader: mockBackupReader,
// 				}
// 			})

// 			It("should handle backup metadata fetch errors gracefully", func() {
// 				// Configure mock to fail VM extraction (which happens during metadata processing)
// 				mockBackupReader.SetError("vm", backupName, namespace, "test-vm", fmt.Errorf("backup storage unavailable"))

// 				basicBackups := []oadpv1alpha1.VeleroBackupInfo{
// 					{
// 						Name:      backupName,
// 						CreatedAt: &metav1.Time{Time: time.Now()},
// 					},
// 				}

// 				enhancedBackups, err := reconciler.addPVCsToBackups(ctx, zap.New(), basicBackups, "test-vm", namespace)

// 				// Should return empty list when backup metadata is unavailable
// 				Expect(err).ToNot(HaveOccurred())
// 				Expect(enhancedBackups).To(BeEmpty())
// 			})

// 			It("should handle VM extraction errors gracefully", func() {
// 				// Configure mock to fail VM extraction
// 				mockBackupReader.SetError("vm", backupName, namespace, "test-vm", fmt.Errorf("VM not found in backup"))

// 				basicBackups := []oadpv1alpha1.VeleroBackupInfo{
// 					{
// 						Name:      backupName,
// 						CreatedAt: &metav1.Time{Time: time.Now()},
// 					},
// 				}

// 				enhancedBackups, err := reconciler.addPVCsToBackups(ctx, zap.New(), basicBackups, "test-vm", namespace)

// 				// Should return empty list when VM is not found
// 				Expect(err).ToNot(HaveOccurred())
// 				Expect(enhancedBackups).To(BeEmpty())
// 			})

// 			It("should handle backup contents reader configured to fail all operations", func() {
// 				mockBackupReader.ShouldFailAll = true

// 				basicBackups := []oadpv1alpha1.VeleroBackupInfo{
// 					{
// 						Name:      backupName,
// 						CreatedAt: &metav1.Time{Time: time.Now()},
// 					},
// 				}

// 				enhancedBackups, err := reconciler.addPVCsToBackups(ctx, zap.New(), basicBackups, "test-vm", namespace)

// 				Expect(err).ToNot(HaveOccurred())
// 				Expect(enhancedBackups).To(BeEmpty())
// 			})

// 			It("should handle concurrent backup processing correctly", func() {
// 				// Setup multiple backups
// 				backup1 := "backup-1"
// 				backup2 := "backup-2"

// 				mockBackupReader.SetupTestBackup(backup1, "test-vm", namespace, []string{"pvc-1"})
// 				mockBackupReader.SetupTestBackup(backup2, "test-vm", namespace, []string{"pvc-2"})
// 				// Configure mock to fail PVC fetches so controller uses deterministic UIDs
// 				mockBackupReader.SetError("pvc", backup1, namespace, "pvc-1", fmt.Errorf("PVC not found in backup"))
// 				mockBackupReader.SetError("pvc", backup2, namespace, "pvc-2", fmt.Errorf("PVC not found in backup"))

// 				basicBackups := []oadpv1alpha1.VeleroBackupInfo{
// 					{Name: backup1, CreatedAt: &metav1.Time{Time: time.Now()}},
// 					{Name: backup2, CreatedAt: &metav1.Time{Time: time.Now()}},
// 				}

// 				enhancedBackups, err := reconciler.addPVCsToBackups(ctx, zap.New(), basicBackups, "test-vm", namespace)

// 				Expect(err).ToNot(HaveOccurred())
// 				Expect(enhancedBackups).To(HaveLen(2))

// 				// Verify each backup has its own PVCs with correct UIDs (fallback UIDs without backup name)
// 				for _, backup := range enhancedBackups {
// 					Expect(backup.PVCs).To(HaveLen(1))
// 					expectedUID := reconciler.generateDeterministicUID("", namespace, backup.PVCs[0].Name)
// 					Expect(backup.PVCs[0].UID).To(Equal(expectedUID))
// 				}
// 			})
// 		})

// 		Describe("Reconciler Integration with Mocking", func() {
// 			It("should successfully reconcile with mock backup reader", func() {
// 				By("Creating a test VirtualMachineFileRestore")

// 				// Create the discovery resource that the VMFR references
// 				discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-discovery",
// 						Namespace: namespace,
// 					},
// 					Status: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
// 						ValidBackups: []oadpv1alpha1.VeleroBackupInfo{
// 							{Name: "test-backup", CreatedAt: &metav1.Time{Time: time.Now()}},
// 						},
// 						Conditions: []metav1.Condition{
// 							{
// 								Type:               string(oadpv1alpha1.VirtualMachineBackupsDiscoveryConditionComplete),
// 								Status:             metav1.ConditionTrue,
// 								LastTransitionTime: metav1.Now(),
// 								Reason:             "DiscoveryCompleted",
// 								Message:            "Discovery completed successfully",
// 							},
// 						},
// 					},
// 				}

// 				vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vmfr",
// 						Namespace: namespace,
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
// 						BackupsDiscoveryRef: "test-discovery",
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(discovery, vmfr).
// 					WithStatusSubresource(&oadpv1alpha1.VirtualMachineFileRestore{}, &oadpv1alpha1.VirtualMachineBackupsDiscovery{}).
// 					Build()
// 				mockBackupReader := velerohelpers.NewMockBackupContentsReader()

// 				reconciler := &VirtualMachineFileRestoreReconciler{
// 					Client:               client,
// 					Scheme:               scheme,
// 					OADPNamespace:        oadpNamespace,
// 					BackupContentsReader: mockBackupReader,
// 				}

// 				By("Reconciling the resource")
// 				typeNamespacedName := types.NamespacedName{
// 					Name:      "test-vmfr",
// 					Namespace: namespace,
// 				}

// 				_, err := reconciler.Reconcile(ctx, reconcile.Request{
// 					NamespacedName: typeNamespacedName,
// 				})

// 				// Should not panic or cause timeouts due to proper mocking
// 				Expect(err).NotTo(HaveOccurred())
// 			})
// 		})

// 		Describe("PVC Discovery with validation failures", func() {
// 			It("should populate pvcRestores field even when backup CRDs are deleted", func() {
// 				// Create discovery resource with VM info
// 				discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-discovery",
// 						Namespace: namespace,
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
// 						VirtualMachineName:      "test-vm",
// 						VirtualMachineNamespace: "vm-namespace",
// 					},
// 					Status: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
// 						ValidBackups: []oadpv1alpha1.VeleroBackupInfo{
// 							{Name: "deleted-backup-1", CreatedAt: &metav1.Time{Time: time.Now()}},
// 							{Name: "deleted-backup-2", CreatedAt: &metav1.Time{Time: time.Now()}},
// 						},
// 						Conditions: []metav1.Condition{
// 							{
// 								Type:               string(oadpv1alpha1.VirtualMachineBackupsDiscoveryConditionComplete),
// 								Status:             metav1.ConditionTrue,
// 								LastTransitionTime: metav1.Now(),
// 								Reason:             "DiscoveryCompleted",
// 								Message:            "Discovery completed successfully",
// 							},
// 						},
// 					},
// 				}

// 				vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vmfr",
// 						Namespace: namespace,
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
// 						BackupsDiscoveryRef: "test-discovery",
// 					},
// 				}

// 				// Create client WITHOUT the backup CRDs (simulating deleted backups)
// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(discovery, vmfr).
// 					WithStatusSubresource(&oadpv1alpha1.VirtualMachineFileRestore{}).
// 					Build()

// 				mockBackupReader := velerohelpers.NewMockBackupContentsReader()
// 				reconciler := &VirtualMachineFileRestoreReconciler{
// 					Client:               client,
// 					Scheme:               scheme,
// 					OADPNamespace:        oadpNamespace,
// 					BackupContentsReader: mockBackupReader,
// 				}

// 				// Call the PVC discovery method directly to test status population
// 				err := reconciler.discoverAndStorePVCInfo(ctx, zap.New(), vmfr)
// 				Expect(err).NotTo(HaveOccurred()) // Should not error even with validation failures

// 				// Verify pvcRestores field is populated with minimal PVC info
// 				Expect(vmfr.Status.PVCRestores).NotTo(BeEmpty())
// 				Expect(vmfr.Status.PVCRestores).To(HaveLen(4)) // 4 minimal PVCs per VM

// 				// Each PVC should have restore info for both failed backups
// 				for _, pvcRestore := range vmfr.Status.PVCRestores {
// 					Expect(pvcRestore.Restores).To(HaveLen(2)) // 2 backups
// 					for _, restore := range pvcRestore.Restores {
// 						Expect(restore.State).To(Equal("backup-deleted"))
// 						Expect([]string{"deleted-backup-1", "deleted-backup-2"}).To(ContainElement(restore.BackupName))
// 					}
// 				}

// 				// Verify conditions reflect the validation failure
// 				pvcCondition := meta.FindStatusCondition(vmfr.Status.Conditions, string(oadpv1alpha1.VirtualMachineFileRestoreConditionPVCsDiscovered))
// 				Expect(pvcCondition).NotTo(BeNil())
// 				Expect(pvcCondition.Status).To(Equal(metav1.ConditionFalse))
// 				Expect(pvcCondition.Reason).To(Equal("PVCDiscoveryFailed"))
// 				Expect(pvcCondition.Message).To(ContainSubstring("validation failed 2 out of 2 backups have been deleted from the cluster"))

// 				readyCondition := meta.FindStatusCondition(vmfr.Status.Conditions, string(oadpv1alpha1.VirtualMachineFileRestoreConditionReady))
// 				Expect(readyCondition).NotTo(BeNil())
// 				Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
// 				Expect(readyCondition.Reason).To(Equal("BackupsDeleted"))
// 			})

// 			It("should populate pvcRestores field even when backup files are missing from storage", func() {
// 				// Create discovery resource with VM info
// 				discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-discovery",
// 						Namespace: namespace,
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
// 						VirtualMachineName:      "test-vm",
// 						VirtualMachineNamespace: "vm-namespace",
// 					},
// 					Status: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
// 						ValidBackups: []oadpv1alpha1.VeleroBackupInfo{
// 							{Name: "missing-files-backup", CreatedAt: &metav1.Time{Time: time.Now()}},
// 						},
// 						Conditions: []metav1.Condition{
// 							{
// 								Type:               string(oadpv1alpha1.VirtualMachineBackupsDiscoveryConditionComplete),
// 								Status:             metav1.ConditionTrue,
// 								LastTransitionTime: metav1.Now(),
// 								Reason:             "DiscoveryCompleted",
// 								Message:            "Discovery completed successfully",
// 							},
// 						},
// 					},
// 				}

// 				vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vmfr",
// 						Namespace: namespace,
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
// 						BackupsDiscoveryRef: "test-discovery",
// 					},
// 				}

// 				// Create backup CRD but configure mock to fail with "file not found"
// 				backup := &velerov1api.Backup{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "missing-files-backup",
// 						Namespace: oadpNamespace,
// 					},
// 					Status: velerov1api.BackupStatus{
// 						CompletionTimestamp: &metav1.Time{Time: time.Now()},
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(discovery, vmfr, backup).
// 					WithStatusSubresource(&oadpv1alpha1.VirtualMachineFileRestore{}).
// 					Build()

// 				mockBackupReader := velerohelpers.NewMockBackupContentsReader()
// 				// Configure mock to fail VM extraction with "file not found" error
// 				mockBackupReader.SetError("vm", "missing-files-backup", "vm-namespace", "test-vm", fmt.Errorf("file not found"))

// 				reconciler := &VirtualMachineFileRestoreReconciler{
// 					Client:               client,
// 					Scheme:               scheme,
// 					OADPNamespace:        oadpNamespace,
// 					BackupContentsReader: mockBackupReader,
// 				}

// 				// Call the PVC discovery method directly to test status population
// 				err := reconciler.discoverAndStorePVCInfo(ctx, zap.New(), vmfr)
// 				Expect(err).NotTo(HaveOccurred()) // Should not error even with validation failures

// 				// Verify pvcRestores field is populated with minimal PVC info
// 				Expect(vmfr.Status.PVCRestores).NotTo(BeEmpty())
// 				Expect(vmfr.Status.PVCRestores).To(HaveLen(4)) // 4 minimal PVCs per VM

// 				// Each PVC should have restore info for the failed backup
// 				for _, pvcRestore := range vmfr.Status.PVCRestores {
// 					Expect(pvcRestore.Restores).To(HaveLen(1)) // 1 backup
// 					restore := pvcRestore.Restores[0]
// 					Expect(restore.State).To(Equal("backup-missing"))
// 					Expect(restore.BackupName).To(Equal("missing-files-backup"))
// 				}

// 				// Verify conditions reflect the validation failure
// 				pvcCondition := meta.FindStatusCondition(vmfr.Status.Conditions, string(oadpv1alpha1.VirtualMachineFileRestoreConditionPVCsDiscovered))
// 				Expect(pvcCondition).NotTo(BeNil())
// 				Expect(pvcCondition.Status).To(Equal(metav1.ConditionFalse))
// 				Expect(pvcCondition.Reason).To(Equal("PVCDiscoveryFailed"))
// 				Expect(pvcCondition.Message).To(ContainSubstring("validation failed 1 out of 1 backups have missing backup files in storage"))

// 				readyCondition := meta.FindStatusCondition(vmfr.Status.Conditions, string(oadpv1alpha1.VirtualMachineFileRestoreConditionReady))
// 				Expect(readyCondition).NotTo(BeNil())
// 				Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
// 				Expect(readyCondition.Reason).To(Equal("BackupFilesMissing"))
// 			})
// 		})
// 	})

// 	Context("Namespace Management", func() {
// 		var (
// 			reconciler    *VirtualMachineFileRestoreReconciler
// 			ctx           context.Context
// 			scheme        *runtime.Scheme
// 			namespace     = "test-namespace"
// 			oadpNamespace = "openshift-adp"
// 		)

// 		BeforeEach(func() {
// 			ctx = context.Background()
// 			scheme = runtime.NewScheme()
// 			Expect(oadpv1alpha1.AddToScheme(scheme)).To(Succeed())
// 			Expect(velerov1api.AddToScheme(scheme)).To(Succeed())
// 			Expect(corev1.AddToScheme(scheme)).To(Succeed())
// 			Expect(kubevirtv1.AddToScheme(scheme)).To(Succeed())
// 		})

// 		Context("Using existing namespace", func() {
// 			It("should use specified existing namespace successfully", func() {
// 				existingNamespace := &corev1.Namespace{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "existing-restore-ns",
// 					},
// 				}

// 				// Create discovery resource
// 				discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-discovery",
// 						Namespace: namespace,
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
// 						VirtualMachineName:      "test-vm",
// 						VirtualMachineNamespace: namespace,
// 					},
// 				}

// 				vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vmfr",
// 						Namespace: namespace,
// 						UID:       "test-vmfr-uid",
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
// 						BackupsDiscoveryRef: "test-discovery",
// 						RestoreNamespace:    "existing-restore-ns",
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(existingNamespace, discovery, vmfr).
// 					Build()

// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:        client,
// 					Scheme:        scheme,
// 					OADPNamespace: oadpNamespace,
// 				}

// 				restoreNamespace, err := reconciler.ensureRestoreNamespace(ctx, zap.New(), vmfr)
// 				Expect(err).NotTo(HaveOccurred())
// 				Expect(restoreNamespace).To(Equal("existing-restore-ns"))

// 				// Verify the namespace still exists and was not modified
// 				ns := &corev1.Namespace{}
// 				err = client.Get(ctx, types.NamespacedName{Name: "existing-restore-ns"}, ns)
// 				Expect(err).NotTo(HaveOccurred())
// 				Expect(ns.Labels).NotTo(HaveKey("oadp.openshift.io/vm-file-restore-temp"))
// 			})

// 			It("should fail when specified namespace does not exist", func() {
// 				// Create discovery resource
// 				discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-discovery",
// 						Namespace: namespace,
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
// 						VirtualMachineName:      "test-vm",
// 						VirtualMachineNamespace: namespace,
// 					},
// 				}

// 				vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vmfr",
// 						Namespace: namespace,
// 						UID:       "test-vmfr-uid",
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
// 						BackupsDiscoveryRef: "test-discovery",
// 						RestoreNamespace:    "non-existent-namespace",
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(discovery, vmfr).
// 					Build()

// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:        client,
// 					Scheme:        scheme,
// 					OADPNamespace: oadpNamespace,
// 				}

// 				_, err := reconciler.ensureRestoreNamespace(ctx, zap.New(), vmfr)
// 				Expect(err).To(HaveOccurred())
// 				Expect(err.Error()).To(ContainSubstring("specified restore namespace 'non-existent-namespace' does not exist"))
// 			})
// 		})

// 		Context("Automatic namespace creation", func() {
// 			It("should create temporary namespace with default naming", func() {
// 				// Create discovery resource
// 				discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-discovery",
// 						Namespace: namespace,
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
// 						VirtualMachineName:      "test-vm",
// 						VirtualMachineNamespace: "vm-namespace",
// 					},
// 				}

// 				vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
// 					TypeMeta: metav1.TypeMeta{
// 						APIVersion: "oadp.openshift.io/v1alpha1",
// 						Kind:       "VirtualMachineFileRestore",
// 					},
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vmfr",
// 						Namespace: namespace,
// 						UID:       "12345678-1234-5678-9012-123456789012",
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
// 						BackupsDiscoveryRef: "test-discovery",
// 						// No RestoreNamespace specified - should create temporary
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(discovery, vmfr).
// 					Build()

// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:        client,
// 					Scheme:        scheme,
// 					OADPNamespace: oadpNamespace,
// 				}

// 				restoreNamespace, err := reconciler.ensureRestoreNamespace(ctx, zap.New(), vmfr)
// 				Expect(err).NotTo(HaveOccurred())
// 				Expect(restoreNamespace).To(Equal("vm-namespace-test-vm-12345678"))

// 				// Verify the namespace was created with proper labels and owner references
// 				ns := &corev1.Namespace{}
// 				err = client.Get(ctx, types.NamespacedName{Name: restoreNamespace}, ns)
// 				Expect(err).NotTo(HaveOccurred())
// 				Expect(ns.Labels).To(HaveKeyWithValue("oadp.openshift.io/vm-file-restore", "test-vmfr"))
// 				Expect(ns.Labels).To(HaveKeyWithValue("oadp.openshift.io/vm-file-restore-temp", "true"))
// 				Expect(ns.Labels).To(HaveKeyWithValue("oadp.openshift.io/managed-by", "oadp-vm-file-restore-controller"))
// 				Expect(ns.OwnerReferences).To(HaveLen(1))
// 				Expect(ns.OwnerReferences[0].Name).To(Equal("test-vmfr"))
// 				Expect(ns.OwnerReferences[0].Kind).To(Equal("VirtualMachineFileRestore"))
// 				Expect(ns.OwnerReferences[0].APIVersion).To(Equal(vmfr.APIVersion))
// 			})

// 			It("should create temporary namespace with custom prefix", func() {
// 				// Create discovery resource
// 				discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-discovery",
// 						Namespace: namespace,
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
// 						VirtualMachineName:      "test-vm",
// 						VirtualMachineNamespace: "vm-namespace",
// 					},
// 				}

// 				vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
// 					TypeMeta: metav1.TypeMeta{
// 						APIVersion: "oadp.openshift.io/v1alpha1",
// 						Kind:       "VirtualMachineFileRestore",
// 					},
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vmfr",
// 						Namespace: namespace,
// 						UID:       "12345678-1234-5678-9012-123456789012",
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
// 						BackupsDiscoveryRef: "test-discovery",
// 						NamespacePrefix:     "custom-prefix",
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(discovery, vmfr).
// 					Build()

// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:        client,
// 					Scheme:        scheme,
// 					OADPNamespace: oadpNamespace,
// 				}

// 				restoreNamespace, err := reconciler.ensureRestoreNamespace(ctx, zap.New(), vmfr)
// 				Expect(err).NotTo(HaveOccurred())
// 				Expect(restoreNamespace).To(Equal("custom-prefix-vm-namespace-test-vm-12345678"))

// 				// Verify the namespace was created
// 				ns := &corev1.Namespace{}
// 				err = client.Get(ctx, types.NamespacedName{Name: restoreNamespace}, ns)
// 				Expect(err).NotTo(HaveOccurred())
// 			})

// 			It("should handle namespace name length limits", func() {
// 				// Create discovery resource with very long names
// 				discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-discovery",
// 						Namespace: namespace,
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
// 						VirtualMachineName:      "very-long-virtual-machine-name-that-exceeds-normal-limits",
// 						VirtualMachineNamespace: "very-long-namespace-name-that-also-exceeds-normal-limits",
// 					},
// 				}

// 				vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vmfr",
// 						Namespace: namespace,
// 						UID:       "12345678-1234-5678-9012-123456789012",
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
// 						BackupsDiscoveryRef: "test-discovery",
// 						NamespacePrefix:     "very-long-prefix-that-might-cause-issues",
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(discovery, vmfr).
// 					Build()

// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:        client,
// 					Scheme:        scheme,
// 					OADPNamespace: oadpNamespace,
// 				}

// 				restoreNamespace, err := reconciler.ensureRestoreNamespace(ctx, zap.New(), vmfr)
// 				Expect(err).NotTo(HaveOccurred())
// 				Expect(len(restoreNamespace)).To(BeNumerically("<=", 63)) // DNS-1123 limit
// 				Expect(restoreNamespace).To(ContainSubstring("12345678")) // Should preserve unique suffix
// 			})

// 			It("should generate unique namespace names for multiple VMs", func() {
// 				// Create discovery resource
// 				discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-discovery",
// 						Namespace: namespace,
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
// 						VirtualMachineName:      "test-vm",
// 						VirtualMachineNamespace: "vm-namespace",
// 					},
// 				}

// 				vmfr1 := &oadpv1alpha1.VirtualMachineFileRestore{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vmfr-1",
// 						Namespace: namespace,
// 						UID:       "11111111-1111-1111-1111-111111111111",
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
// 						BackupsDiscoveryRef: "test-discovery",
// 					},
// 				}

// 				vmfr2 := &oadpv1alpha1.VirtualMachineFileRestore{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vmfr-2",
// 						Namespace: namespace,
// 						UID:       "22222222-2222-2222-2222-222222222222",
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
// 						BackupsDiscoveryRef: "test-discovery",
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(discovery, vmfr1, vmfr2).
// 					Build()

// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:        client,
// 					Scheme:        scheme,
// 					OADPNamespace: oadpNamespace,
// 				}

// 				namespace1, err1 := reconciler.ensureRestoreNamespace(ctx, zap.New(), vmfr1)
// 				Expect(err1).NotTo(HaveOccurred())

// 				namespace2, err2 := reconciler.ensureRestoreNamespace(ctx, zap.New(), vmfr2)
// 				Expect(err2).NotTo(HaveOccurred())

// 				// Should generate different namespace names
// 				Expect(namespace1).NotTo(Equal(namespace2))
// 				Expect(namespace1).To(ContainSubstring("11111111"))
// 				Expect(namespace2).To(ContainSubstring("22222222"))
// 			})

// 			It("should handle namespace creation conflicts gracefully", func() {
// 				// Create discovery resource
// 				discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-discovery",
// 						Namespace: namespace,
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
// 						VirtualMachineName:      "test-vm",
// 						VirtualMachineNamespace: "vm-namespace",
// 					},
// 				}

// 				vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vmfr",
// 						Namespace: namespace,
// 						UID:       "12345678-1234-5678-9012-123456789012",
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
// 						BackupsDiscoveryRef: "test-discovery",
// 					},
// 				}

// 				// Pre-create a namespace with the same name that would be generated
// 				expectedName := "vm-namespace-test-vm-12345678"
// 				existingNS := &corev1.Namespace{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: expectedName,
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(discovery, vmfr, existingNS).
// 					Build()

// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:        client,
// 					Scheme:        scheme,
// 					OADPNamespace: oadpNamespace,
// 				}

// 				restoreNamespace, err := reconciler.ensureRestoreNamespace(ctx, zap.New(), vmfr)
// 				Expect(err).NotTo(HaveOccurred())
// 				Expect(restoreNamespace).To(Equal(expectedName))
// 			})
// 		})

// 		Context("Namespace cleanup", func() {
// 			It("should not delete user-specified namespaces", func() {
// 				existingNamespace := &corev1.Namespace{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "user-specified-ns",
// 					},
// 				}

// 				now := metav1.Now()
// 				vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:              "test-vmfr",
// 						Namespace:         namespace,
// 						DeletionTimestamp: &now,                       // Simulate deletion
// 						Finalizers:        []string{"test-finalizer"}, // Required for deletion timestamp
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
// 						RestoreNamespace: "user-specified-ns",
// 					},
// 					Status: oadpv1alpha1.VirtualMachineFileRestoreStatus{
// 						CreatedNamespace: "user-specified-ns",
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(existingNamespace, vmfr).
// 					WithStatusSubresource(&oadpv1alpha1.VirtualMachineFileRestore{}).
// 					Build()

// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:        client,
// 					Scheme:        scheme,
// 					OADPNamespace: oadpNamespace,
// 				}

// 				_, err := reconciler.handleResourceDeletion(ctx, zap.New(), vmfr)
// 				Expect(err).NotTo(HaveOccurred())

// 				// Verify the namespace still exists
// 				ns := &corev1.Namespace{}
// 				err = client.Get(ctx, types.NamespacedName{Name: "user-specified-ns"}, ns)
// 				Expect(err).NotTo(HaveOccurred())
// 			})

// 			It("should delete temporary namespaces on VMFR deletion", func() {
// 				tempNamespace := &corev1.Namespace{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "temp-restore-ns",
// 						Labels: map[string]string{
// 							"oadp.openshift.io/vm-file-restore-temp": "true",
// 							"oadp.openshift.io/managed-by":           "oadp-vm-file-restore-controller",
// 						},
// 						OwnerReferences: []metav1.OwnerReference{
// 							{
// 								APIVersion: "oadp.openshift.io/v1alpha1",
// 								Kind:       "VirtualMachineFileRestore",
// 								Name:       "test-vmfr",
// 								UID:        "test-uid",
// 							},
// 						},
// 					},
// 				}

// 				now := metav1.Now()
// 				vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:              "test-vmfr",
// 						Namespace:         namespace,
// 						UID:               "test-uid",
// 						DeletionTimestamp: &now,                             // Simulate deletion
// 						Finalizers:        []string{VMFileRestoreFinalizer}, // Required for deletion timestamp
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
// 						// No RestoreNamespace specified - was auto-created
// 					},
// 					Status: oadpv1alpha1.VirtualMachineFileRestoreStatus{
// 						CreatedNamespace: "temp-restore-ns",
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(tempNamespace, vmfr).
// 					WithStatusSubresource(&oadpv1alpha1.VirtualMachineFileRestore{}).
// 					Build()

// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:        client,
// 					Scheme:        scheme,
// 					OADPNamespace: oadpNamespace,
// 				}

// 				_, err := reconciler.handleResourceDeletion(ctx, zap.New(), vmfr)
// 				Expect(err).NotTo(HaveOccurred())

// 				// Verify the namespace was deleted
// 				ns := &corev1.Namespace{}
// 				err = client.Get(ctx, types.NamespacedName{Name: "temp-restore-ns"}, ns)
// 				Expect(errors.IsNotFound(err)).To(BeTrue())
// 			})

// 			It("should skip cleanup of unowned namespaces", func() {
// 				// Create a namespace that does NOT have the proper temp label - should not be deleted
// 				unownedNamespace := &corev1.Namespace{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "unowned-ns",
// 						Labels: map[string]string{
// 							"some-other-label": "true",
// 						},
// 						// No temp label and no owner references - should NOT be deleted
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(unownedNamespace).
// 					Build()

// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:        client,
// 					Scheme:        scheme,
// 					OADPNamespace: oadpNamespace,
// 				}

// 				// Test the cleanup function directly - it should recognize this namespace as unowned
// 				err := reconciler.cleanupTemporaryNamespace(ctx, zap.New(), "unowned-ns")
// 				Expect(err).NotTo(HaveOccurred())

// 				// Verify the namespace was NOT deleted (it should still exist)
// 				ns := &corev1.Namespace{}
// 				err = client.Get(ctx, types.NamespacedName{Name: "unowned-ns"}, ns)
// 				Expect(err).NotTo(HaveOccurred())
// 			})

// 			It("should handle cleanup errors gracefully", func() {
// 				vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vmfr",
// 						Namespace: namespace,
// 					},
// 					Status: oadpv1alpha1.VirtualMachineFileRestoreStatus{
// 						CreatedNamespace: "non-existent-ns",
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(vmfr).
// 					Build()

// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:        client,
// 					Scheme:        scheme,
// 					OADPNamespace: oadpNamespace,
// 				}

// 				err := reconciler.cleanupTemporaryNamespace(ctx, zap.New(), "non-existent-ns")
// 				Expect(err).NotTo(HaveOccurred()) // Should handle gracefully
// 			})
// 		})

// 		Context("Namespace name generation", func() {
// 			It("should generate valid DNS-1123 namespace names", func() {
// 				discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-discovery",
// 						Namespace: namespace,
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
// 						VirtualMachineName:      "Test_VM_Name",     // Contains underscores
// 						VirtualMachineNamespace: "Test_Namespace_1", // Contains underscores
// 					},
// 				}

// 				vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "test-vmfr",
// 						Namespace: namespace,
// 						UID:       "12345678-1234-5678-9012-123456789012",
// 					},
// 					Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
// 						BackupsDiscoveryRef: "test-discovery",
// 						NamespacePrefix:     "Test_Prefix", // Contains underscores
// 					},
// 				}

// 				client := fake.NewClientBuilder().
// 					WithScheme(scheme).
// 					WithObjects(discovery, vmfr).
// 					Build()

// 				reconciler = &VirtualMachineFileRestoreReconciler{
// 					Client:        client,
// 					Scheme:        scheme,
// 					OADPNamespace: oadpNamespace,
// 				}

// 				namespaceName, err := reconciler.generateTemporaryNamespaceName(ctx, zap.New(), vmfr)
// 				Expect(err).NotTo(HaveOccurred())
// 				Expect(namespaceName).To(Equal("test-prefix-test-namespace-1-test-vm-name-12345678"))
// 				Expect(namespaceName).To(MatchRegexp(`^[a-z0-9-]+$`)) // Valid DNS-1123 format
// 				Expect(len(namespaceName)).To(BeNumerically("<=", 63))
// 			})
// 		})
// 	})
// })

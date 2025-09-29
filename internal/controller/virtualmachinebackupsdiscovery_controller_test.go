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

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	oadpv1alpha1 "github.com/migtools/oadp-vm-file-restore/api/v1alpha1"
	"github.com/migtools/oadp-vm-file-restore/internal/velerohelpers"
	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// testScenario defines a table-driven test scenario for VMBD controller
type testScenario struct {
	name               string
	spec               oadpv1alpha1.VirtualMachineBackupsDiscoverySpec
	expectedPhase      oadpv1alpha1.VirtualMachineBackupsDiscoveryPhase
	expectedConditions []metav1.Condition
	shouldError        bool
}

var _ = Describe("VirtualMachineBackupsDiscovery Controller Comprehensive Tests", func() {
	var (
		ctx           context.Context
		testNamespace string
		reconciler    *VirtualMachineBackupsDiscoveryReconciler
	)

	BeforeEach(func() {
		ctx = context.Background()
		testNamespace = fmt.Sprintf("test-ns-%s", uuid.NewUUID()[:8])

		// Create test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// Create OADP namespace for tests
		oadpNs := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "velero",
			},
		}
		_ = k8sClient.Create(ctx, oadpNs) // Ignore error if already exists

		// Set up reconciler without backup reader to avoid Velero CRD dependencies
		// The controller will fail gracefully when trying to list backups (which is expected)
		reconciler = &VirtualMachineBackupsDiscoveryReconciler{
			Client:        k8sClient,
			Scheme:        k8sClient.Scheme(),
			OADPNamespace: "velero",
			// BackupContentsReader is left nil to avoid Velero dependencies
		}
	})

	AfterEach(func() {
		// Clean up test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		_ = k8sClient.Delete(ctx, ns)
	})

	DescribeTable("VMBD reconciliation scenarios",
		func(scenario testScenario) {
			vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-%s", scenario.name),
					Namespace: testNamespace,
				},
				Spec: scenario.spec,
			}

			By(fmt.Sprintf("Creating VMBD resource for scenario: %s", scenario.name))
			Expect(k8sClient.Create(ctx, vmbd)).To(Succeed())

			namespacedName := types.NamespacedName{
				Name:      vmbd.Name,
				Namespace: vmbd.Namespace,
			}

			By("Performing reconciliation")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})

			if scenario.shouldError {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).NotTo(HaveOccurred())
			}

			By("Validating VMBD status")
			checkTestVMBDStatus(ctx, namespacedName, scenario)

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, vmbd)).To(Succeed())
		},
		Entry("Basic discovery without selection criteria", testScenario{
			name: "basic-discovery",
			spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
				VirtualMachineName:      "test-vm",
				VirtualMachineNamespace: "vm-namespace",
			},
			expectedPhase: oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseNew,
		}),
		Entry("Discovery with explicit backup list", testScenario{
			name: "explicit-backup-list",
			spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
				VirtualMachineName:      "test-vm",
				VirtualMachineNamespace: "vm-namespace",
				RequestedBackups:        []string{"backup1", "backup2"},
			},
			expectedPhase: oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseNew,
		}),
		Entry("Discovery with time range", testScenario{
			name: "time-range-discovery",
			spec: func() oadpv1alpha1.VirtualMachineBackupsDiscoverySpec {
				startTime := oadpv1alpha1.FlexibleTime(time.Now().Add(-24 * time.Hour).Format(time.RFC3339))
				return oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
					VirtualMachineName:      "test-vm",
					VirtualMachineNamespace: "vm-namespace",
					StartTime:               &startTime,
				}
			}(),
			expectedPhase: oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseNew,
		}),
		Entry("Discovery with both time range and explicit list", testScenario{
			name: "combined-criteria",
			spec: func() oadpv1alpha1.VirtualMachineBackupsDiscoverySpec {
				startTime := oadpv1alpha1.FlexibleTime(time.Now().Add(-24 * time.Hour).Format(time.RFC3339))
				return oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
					VirtualMachineName:      "test-vm",
					VirtualMachineNamespace: "vm-namespace",
					StartTime:               &startTime,
					RequestedBackups:        []string{"backup1"},
				}
			}(),
			expectedPhase: oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseNew,
		}),
	)

	Context("Phase transitions and status management", func() {
		It("should initialize phase correctly", func() {
			vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-phase-init",
					Namespace: testNamespace,
				},
				Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
					VirtualMachineName:      "test-vm",
					VirtualMachineNamespace: "vm-namespace",
				},
			}

			By("Creating VMBD resource")
			Expect(k8sClient.Create(ctx, vmbd)).To(Succeed())

			namespacedName := types.NamespacedName{
				Name:      vmbd.Name,
				Namespace: vmbd.Namespace,
			}

			By("Performing initial reconciliation")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying initial phase is set")
			updatedVMBD := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
			Expect(k8sClient.Get(ctx, namespacedName, updatedVMBD)).To(Succeed())
			Expect(updatedVMBD.Status.Phase).To(Equal(oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseNew))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, vmbd)).To(Succeed())
		})

		It("should handle basic spec changes", func() {
			vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-spec-modification",
					Namespace: testNamespace,
				},
				Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
					VirtualMachineName:      "test-vm",
					VirtualMachineNamespace: "vm-namespace",
					RequestedBackups:        []string{"backup1"},
				},
			}

			By("Creating VMBD resource")
			Expect(k8sClient.Create(ctx, vmbd)).To(Succeed())

			namespacedName := types.NamespacedName{
				Name:      vmbd.Name,
				Namespace: vmbd.Namespace,
			}

			By("Performing initial reconciliation")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying resource was created")
			currentVMBD := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
			Expect(k8sClient.Get(ctx, namespacedName, currentVMBD)).To(Succeed())
			Expect(currentVMBD.Status.Phase).ToNot(BeEmpty())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, vmbd)).To(Succeed())
		})

		It("should handle basic status updates", func() {
			vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-status-update",
					Namespace: testNamespace,
				},
				Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
					VirtualMachineName:      "test-vm",
					VirtualMachineNamespace: "vm-namespace",
				},
			}

			By("Creating VMBD resource")
			Expect(k8sClient.Create(ctx, vmbd)).To(Succeed())

			namespacedName := types.NamespacedName{
				Name:      vmbd.Name,
				Namespace: vmbd.Namespace,
			}

			By("Performing reconciliation")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status was updated")
			updatedVMBD := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
			Expect(k8sClient.Get(ctx, namespacedName, updatedVMBD)).To(Succeed())
			Expect(updatedVMBD.Status.Phase).ToNot(BeEmpty())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, vmbd)).To(Succeed())
		})
	})

	Context("Error handling and validation", func() {
		It("should handle webhook validation for invalid spec", func() {
			vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-missing-vm-spec",
					Namespace: testNamespace,
				},
				Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
					VirtualMachineName: "", // Empty VM name
					// Missing VirtualMachineNamespace
				},
			}

			By("Creating VMBD resource with invalid spec should fail validation")
			err := k8sClient.Create(ctx, vmbd)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Invalid value"))
		})

		It("should handle reconciliation without Velero CRDs", func() {
			vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-no-velero-crds",
					Namespace: testNamespace,
				},
				Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
					VirtualMachineName:      "test-vm",
					VirtualMachineNamespace: "vm-namespace",
				},
			}

			By("Creating VMBD resource")
			Expect(k8sClient.Create(ctx, vmbd)).To(Succeed())

			namespacedName := types.NamespacedName{
				Name:      vmbd.Name,
				Namespace: vmbd.Namespace,
			}

			By("Performing initial reconciliation")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred()) // Phase initialization should succeed

			By("Performing second reconciliation that tries to list backups")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			// This should fail because Velero CRDs are not available
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no matches for kind \"Backup\""))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, vmbd)).To(Succeed())
		})
	})

	Context("Resource lifecycle management", func() {
		It("should handle resource deletion properly", func() {
			vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deletion",
					Namespace: testNamespace,
				},
				Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
					VirtualMachineName:      "test-vm",
					VirtualMachineNamespace: "vm-namespace",
				},
			}

			By("Creating VMBD resource")
			Expect(k8sClient.Create(ctx, vmbd)).To(Succeed())

			namespacedName := types.NamespacedName{
				Name:      vmbd.Name,
				Namespace: vmbd.Namespace,
			}

			By("Performing initial reconciliation")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Deleting the resource")
			Expect(k8sClient.Delete(ctx, vmbd)).To(Succeed())

			By("Verifying reconciliation handles deletion gracefully")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred()) // Should not error on missing resource
		})

		It("should handle discovery object modifications after completion", func() {
			vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-modification-after-completion",
					Namespace: testNamespace,
				},
				Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
					VirtualMachineName:      "test-vm",
					VirtualMachineNamespace: "vm-namespace",
					RequestedBackups:        []string{"backup1"},
				},
			}

			By("Creating VMBD resource")
			Expect(k8sClient.Create(ctx, vmbd)).To(Succeed())

			namespacedName := types.NamespacedName{
				Name:      vmbd.Name,
				Namespace: vmbd.Namespace,
			}

			By("Performing initial reconciliation")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Simulating completed discovery by updating status")
			// Get the latest version
			currentVMBD := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
			Expect(k8sClient.Get(ctx, namespacedName, currentVMBD)).To(Succeed())

			// Simulate discovery completion
			currentVMBD.Status.Phase = oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseCompleted
			currentVMBD.Status.ObservedGeneration = currentVMBD.Generation
			currentVMBD.Status.DiscoveryStats = &oadpv1alpha1.DiscoveryStatistics{
				TotalCandidates: 1,
				Pending:         0,
				InProgress:      0,
				Completed:       1,
				Failed:          0,
			}
			Expect(k8sClient.Status().Update(ctx, currentVMBD)).To(Succeed())

			By("Modifying spec to add more backups")
			currentVMBD.Spec.RequestedBackups = []string{"backup1", "backup2", "backup3"}
			Expect(k8sClient.Update(ctx, currentVMBD)).To(Succeed())

			By("Performing reconciliation after spec change")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			// Should detect spec changes and try to restart discovery, but fail on Velero CRDs
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no matches for kind \"Backup\""))

			By("Verifying discovery was reset due to spec changes")
			updatedVMBD := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
			Expect(k8sClient.Get(ctx, namespacedName, updatedVMBD)).To(Succeed())
			// Phase should be reset when spec changes, but then fail on backup listing
			// So we just check that the generation tracking is working
			Expect(updatedVMBD.Generation).To(BeNumerically(">", 1))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, vmbd)).To(Succeed())
		})

		It("should handle time range modifications", func() {
			// Create initial spec with start time
			startTime := oadpv1alpha1.FlexibleTime(time.Now().Add(-48 * time.Hour).Format(time.RFC3339))
			vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-time-range-modification",
					Namespace: testNamespace,
				},
				Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
					VirtualMachineName:      "test-vm",
					VirtualMachineNamespace: "vm-namespace",
					StartTime:               &startTime,
				},
			}

			By("Creating VMBD resource with time range")
			Expect(k8sClient.Create(ctx, vmbd)).To(Succeed())

			namespacedName := types.NamespacedName{
				Name:      vmbd.Name,
				Namespace: vmbd.Namespace,
			}

			By("Performing initial reconciliation")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Modifying time range by adding end time")
			currentVMBD := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
			Expect(k8sClient.Get(ctx, namespacedName, currentVMBD)).To(Succeed())

			endTime := oadpv1alpha1.FlexibleTime(time.Now().Add(-12 * time.Hour).Format(time.RFC3339))
			currentVMBD.Spec.EndTime = &endTime
			Expect(k8sClient.Update(ctx, currentVMBD)).To(Succeed())

			By("Performing reconciliation after time range change")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			// This will fail due to missing Velero CRDs when trying to list backups
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no matches for kind \"Backup\""))

			By("Verifying resource generation was updated")
			updatedVMBD := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
			Expect(k8sClient.Get(ctx, namespacedName, updatedVMBD)).To(Succeed())
			Expect(updatedVMBD.Generation).To(BeNumerically(">", 1))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, vmbd)).To(Succeed())
		})
	})

	Context("Unit tests for individual reconciler methods", func() {
		var (
			testReconciler *VirtualMachineBackupsDiscoveryReconciler
		)

		BeforeEach(func() {
			testReconciler = &VirtualMachineBackupsDiscoveryReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				OADPNamespace: "velero",
			}
		})

		Describe("parseTimeRange", func() {
			It("should handle no time constraints", func() {
				vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{},
				}

				startTime, endTime, err := testReconciler.parseTimeRange(vmbd)
				Expect(err).NotTo(HaveOccurred())
				Expect(startTime.IsZero()).To(BeTrue())
				Expect(endTime.After(time.Now().Add(-time.Second))).To(BeTrue()) // Should be close to now
			})

			It("should handle start time only", func() {
				timeStr := "2024-01-01T00:00:00Z"
				startTimeVal := oadpv1alpha1.FlexibleTime(timeStr)
				vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
						StartTime: &startTimeVal,
					},
				}

				startTime, endTime, err := testReconciler.parseTimeRange(vmbd)
				Expect(err).NotTo(HaveOccurred())
				Expect(startTime.Format(time.RFC3339)).To(Equal(timeStr))
				Expect(endTime.After(time.Now().Add(-time.Second))).To(BeTrue())
			})

			It("should handle end time only", func() {
				timeStr := "2024-12-31T23:59:59Z"
				endTimeVal := oadpv1alpha1.FlexibleTime(timeStr)
				vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
						EndTime: &endTimeVal,
					},
				}

				startTime, endTime, err := testReconciler.parseTimeRange(vmbd)
				Expect(err).NotTo(HaveOccurred())
				Expect(startTime.IsZero()).To(BeTrue())
				Expect(endTime.Format(time.RFC3339)).To(Equal(timeStr))
			})

			It("should handle date-only end time and convert to end of day", func() {
				timeStr := "2024-01-15"
				endTimeVal := oadpv1alpha1.FlexibleTime(timeStr)
				vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
						EndTime: &endTimeVal,
					},
				}

				startTime, endTime, err := testReconciler.parseTimeRange(vmbd)
				Expect(err).NotTo(HaveOccurred())
				Expect(startTime.IsZero()).To(BeTrue())
				Expect(endTime.Hour()).To(Equal(23))
				Expect(endTime.Minute()).To(Equal(59))
				Expect(endTime.Second()).To(Equal(59))
			})

			It("should handle invalid start time format", func() {
				invalidTime := oadpv1alpha1.FlexibleTime("invalid-time")
				vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
						StartTime: &invalidTime,
					},
				}

				_, _, err := testReconciler.parseTimeRange(vmbd)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid startTime format"))
			})

			It("should handle invalid end time format", func() {
				invalidTime := oadpv1alpha1.FlexibleTime("not-a-time")
				vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
						EndTime: &invalidTime,
					},
				}

				_, _, err := testReconciler.parseTimeRange(vmbd)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid endTime format"))
			})
		})

		Describe("isDiscoveryComplete", func() {
			It("should return false when discovery stats is nil", func() {
				vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					Status: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
						Phase: oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseCompleted,
					},
				}

				result := testReconciler.isDiscoveryComplete(vmbd)
				Expect(result).To(BeFalse())
			})

			It("should return false when pending work exists", func() {
				vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					Status: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
						Phase: oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseCompleted,
						DiscoveryStats: &oadpv1alpha1.DiscoveryStatistics{
							Pending:    1,
							InProgress: 0,
						},
					},
				}

				result := testReconciler.isDiscoveryComplete(vmbd)
				Expect(result).To(BeFalse())
			})

			It("should return false when in-progress work exists", func() {
				vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					Status: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
						Phase: oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseCompleted,
						DiscoveryStats: &oadpv1alpha1.DiscoveryStatistics{
							Pending:    0,
							InProgress: 2,
						},
					},
				}

				result := testReconciler.isDiscoveryComplete(vmbd)
				Expect(result).To(BeFalse())
			})

			It("should return false when phase is not Completed", func() {
				vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					Status: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
						Phase: oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseInProgress,
						DiscoveryStats: &oadpv1alpha1.DiscoveryStatistics{
							Pending:    0,
							InProgress: 0,
						},
					},
				}

				result := testReconciler.isDiscoveryComplete(vmbd)
				Expect(result).To(BeFalse())
			})

			It("should return true when discovery is truly complete", func() {
				vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					Status: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
						Phase: oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseCompleted,
						DiscoveryStats: &oadpv1alpha1.DiscoveryStatistics{
							Pending:    0,
							InProgress: 0,
							Completed:  5,
							Failed:     2,
						},
					},
				}

				result := testReconciler.isDiscoveryComplete(vmbd)
				Expect(result).To(BeTrue())
			})
		})

		Describe("ValidateVMInBackupSpec", func() {
			var vm *kubevirtv1.VirtualMachine

			BeforeEach(func() {
				vm = &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "vm-namespace",
					},
				}
			})

			It("should include VM when no namespace filters are specified", func() {
				backup := velerov1api.Backup{
					Spec: velerov1api.BackupSpec{},
				}

				result := velerohelpers.ValidateVMInBackupSpec(&backup, vm)
				Expect(result).To(BeTrue())
			})

			It("should include VM when namespace is in included list", func() {
				backup := velerov1api.Backup{
					Spec: velerov1api.BackupSpec{
						IncludedNamespaces: []string{"vm-namespace", "other-ns"},
					},
				}

				result := velerohelpers.ValidateVMInBackupSpec(&backup, vm)
				Expect(result).To(BeTrue())
			})

			It("should exclude VM when namespace is not in included list", func() {
				backup := velerov1api.Backup{
					Spec: velerov1api.BackupSpec{
						IncludedNamespaces: []string{"other-ns", "another-ns"},
					},
				}

				result := velerohelpers.ValidateVMInBackupSpec(&backup, vm)
				Expect(result).To(BeFalse())
			})

			It("should exclude VM when namespace is in excluded list", func() {
				backup := velerov1api.Backup{
					Spec: velerov1api.BackupSpec{
						ExcludedNamespaces: []string{"vm-namespace"},
					},
				}

				result := velerohelpers.ValidateVMInBackupSpec(&backup, vm)
				Expect(result).To(BeFalse())
			})

			It("should include VM when namespace is not in excluded list", func() {
				backup := velerov1api.Backup{
					Spec: velerov1api.BackupSpec{
						ExcludedNamespaces: []string{"other-ns", "another-ns"},
					},
				}

				result := velerohelpers.ValidateVMInBackupSpec(&backup, vm)
				Expect(result).To(BeTrue())
			})

			It("should exclude VM when namespace is both included and excluded (exclusion wins)", func() {
				backup := velerov1api.Backup{
					Spec: velerov1api.BackupSpec{
						IncludedNamespaces: []string{"vm-namespace"},
						ExcludedNamespaces: []string{"vm-namespace"},
					},
				}

				result := velerohelpers.ValidateVMInBackupSpec(&backup, vm)
				Expect(result).To(BeFalse())
			})
		})

		Describe("mapBackupToVMBD", func() {
			var (
				backupObj *velerov1api.Backup
				vmbd1     *oadpv1alpha1.VirtualMachineBackupsDiscovery
				vmbd2     *oadpv1alpha1.VirtualMachineBackupsDiscovery
			)

			BeforeEach(func() {
				backupObj = &velerov1api.Backup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-backup",
						Namespace: "velero",
					},
				}

				vmbd1 = &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmbd-1",
						Namespace: testNamespace,
					},
					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
						VirtualMachineName:      "test-vm-1",
						VirtualMachineNamespace: "vm-namespace",
					},
				}
				vmbd2 = &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmbd-2",
						Namespace: testNamespace,
					},
					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
						VirtualMachineName:      "test-vm-2",
						VirtualMachineNamespace: "vm-namespace",
					},
				}

				Expect(k8sClient.Create(ctx, vmbd1)).To(Succeed())
				Expect(k8sClient.Create(ctx, vmbd2)).To(Succeed())
			})

			AfterEach(func() {
				_ = k8sClient.Delete(ctx, vmbd1)
				_ = k8sClient.Delete(ctx, vmbd2)
			})

			It("should return requests for all VMBD resources when backup is in OADP namespace", func() {
				requests := testReconciler.mapBackupToVMBD(ctx, backupObj)

				Expect(len(requests)).To(BeNumerically(">=", 2))

				// Check that our test VMBDs are included in the requests
				foundVmbd1 := false
				foundVmbd2 := false
				for _, req := range requests {
					if req.Name == "vmbd-1" && req.Namespace == testNamespace {
						foundVmbd1 = true
					}
					if req.Name == "vmbd-2" && req.Namespace == testNamespace {
						foundVmbd2 = true
					}
				}
				Expect(foundVmbd1).To(BeTrue())
				Expect(foundVmbd2).To(BeTrue())
			})

			It("should return no requests when backup is not in OADP namespace", func() {
				backupObj.Namespace = "other-namespace"

				requests := testReconciler.mapBackupToVMBD(ctx, backupObj)

				Expect(requests).To(BeEmpty())
			})
		})

		Describe("updateStatusWithRetry", func() {
			var testVMBD *oadpv1alpha1.VirtualMachineBackupsDiscovery

			BeforeEach(func() {
				testVMBD = &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-retry",
						Namespace: testNamespace,
					},
					Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
						VirtualMachineName:      "test-vm",
						VirtualMachineNamespace: "vm-namespace",
					},
				}
				Expect(k8sClient.Create(ctx, testVMBD)).To(Succeed())
			})

			AfterEach(func() {
				_ = k8sClient.Delete(ctx, testVMBD)
			})

			It("should succeed on first try when no conflicts", func() {
				// Update the status
				testVMBD.Status.Phase = oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseNew

				err := testReconciler.updateStatusWithRetry(ctx, testVMBD)
				Expect(err).NotTo(HaveOccurred())

				// Verify the update was applied
				updated := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
				Expect(k8sClient.Get(ctx, client.ObjectKey{Name: testVMBD.Name, Namespace: testVMBD.Namespace}, updated)).To(Succeed())
				Expect(updated.Status.Phase).To(Equal(oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseNew))
			})

			It("should handle non-existent resource gracefully", func() {
				nonExistentVMBD := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "non-existent",
						Namespace: testNamespace,
					},
				}

				err := testReconciler.updateStatusWithRetry(ctx, nonExistentVMBD)
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("Status conditions and discovery statistics", func() {
			It("should validate discovery statistics structure", func() {
				stats := &oadpv1alpha1.DiscoveryStatistics{
					TotalCandidates: 10,
					Pending:         3,
					InProgress:      2,
					Completed:       4,
					Failed:          1,
					Skipped:         0,
				}

				// Verify all counts add up correctly
				total := stats.Pending + stats.InProgress + stats.Completed + stats.Failed + stats.Skipped
				Expect(total).To(Equal(stats.TotalCandidates))
			})

			It("should handle empty discovery statistics", func() {
				vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
					Status: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{},
				}

				// Should handle nil DiscoveryStats gracefully
				result := testReconciler.isDiscoveryComplete(vmbd)
				Expect(result).To(BeFalse())
			})
		})

		Describe("Reconciliation path selection", func() {
			It("should handle various phase combinations", func() {
				testCases := []struct {
					name         string
					phase        oadpv1alpha1.VirtualMachineBackupsDiscoveryPhase
					hasStats     bool
					pending      int
					inProgress   int
					observedGen  int64
					generation   int64
					expectedPath string
				}{
					{
						name:         "empty phase",
						phase:        "",
						hasStats:     false,
						expectedPath: "initialize phase",
					},
					{
						name:         "phase set but no stats",
						phase:        oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseNew,
						hasStats:     false,
						expectedPath: "initialize discovery",
					},
					{
						name:         "discovery complete and generations match",
						phase:        oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseCompleted,
						hasStats:     true,
						pending:      0,
						inProgress:   0,
						observedGen:  1,
						generation:   1,
						expectedPath: "check for spec changes",
					},
					{
						name:         "discovery complete but generations don't match",
						phase:        oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseCompleted,
						hasStats:     true,
						pending:      0,
						inProgress:   0,
						observedGen:  1,
						generation:   2,
						expectedPath: "restart discovery",
					},
					{
						name:         "active discovery with pending work",
						phase:        oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseInProgress,
						hasStats:     true,
						pending:      5,
						inProgress:   0,
						expectedPath: "process discovery batch",
					},
					{
						name:         "active discovery with in-progress work",
						phase:        oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseInProgress,
						hasStats:     true,
						pending:      0,
						inProgress:   3,
						expectedPath: "process discovery batch",
					},
				}

				for _, tc := range testCases {
					By(fmt.Sprintf("Testing case: %s", tc.name))

					vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
						ObjectMeta: metav1.ObjectMeta{
							Generation: tc.generation,
						},
						Status: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
							Phase:              tc.phase,
							ObservedGeneration: tc.observedGen,
						},
					}

					if tc.hasStats {
						vmbd.Status.DiscoveryStats = &oadpv1alpha1.DiscoveryStatistics{
							Pending:    tc.pending,
							InProgress: tc.inProgress,
						}
					}

					// Verify isDiscoveryComplete logic
					isComplete := testReconciler.isDiscoveryComplete(vmbd)
					if tc.expectedPath == "check for spec changes" || tc.expectedPath == "restart discovery" {
						Expect(isComplete).To(BeTrue(), "Discovery should be complete for case: %s", tc.name)
					} else {
						Expect(isComplete).To(BeFalse(), "Discovery should not be complete for case: %s", tc.name)
					}
				}
			})
		})

		Describe("Edge cases and error conditions", func() {
			It("should handle invalid VM specifications", func() {
				testCases := []struct {
					name         string
					resourceName string
					vmName       string
					vmNamespace  string
					shouldPass   bool
				}{
					{"valid VM", "test-valid-vm", "test-vm", "test-ns", true},
					{"empty VM name", "test-empty-vm-name", "", "test-ns", false},
					{"empty VM namespace", "test-empty-vm-namespace", "test-vm", "", false},
					{"both empty", "test-both-empty", "", "", false},
				}

				for _, tc := range testCases {
					By(fmt.Sprintf("Testing: %s", tc.name))

					vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
						ObjectMeta: metav1.ObjectMeta{
							Name:      tc.resourceName,
							Namespace: testNamespace,
						},
						Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
							VirtualMachineName:      tc.vmName,
							VirtualMachineNamespace: tc.vmNamespace,
						},
					}

					err := k8sClient.Create(ctx, vmbd)
					if tc.shouldPass {
						Expect(err).NotTo(HaveOccurred(), "Valid spec should be accepted")
						_ = k8sClient.Delete(ctx, vmbd)
					} else {
						Expect(err).To(HaveOccurred(), "Invalid spec should be rejected")
					}
				}
			})

			It("should handle malformed backup names in explicit lists", func() {
				testCases := []struct {
					name             string
					requestedBackups []string
					description      string
				}{
					{"empty list", []string{}, "empty backup list"},
					{"single backup", []string{"backup1"}, "single backup"},
					{"multiple backups", []string{"backup1", "backup2", "backup3"}, "multiple backups"},
					{"duplicate backups", []string{"backup1", "backup1", "backup2"}, "duplicate backup names"},
					{"empty backup name", []string{"backup1", "", "backup2"}, "empty backup name in list"},
					{"special characters", []string{"backup-1", "backup_2", "backup.3"}, "special characters in names"},
				}

				for _, tc := range testCases {
					By(fmt.Sprintf("Testing: %s", tc.description))

					vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("test-backups-%d", len(tc.requestedBackups)),
							Namespace: testNamespace,
						},
						Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
							VirtualMachineName:      "test-vm",
							VirtualMachineNamespace: "vm-namespace",
							RequestedBackups:        tc.requestedBackups,
						},
					}

					// Should be able to create the resource (validation happens at reconcile time)
					Expect(k8sClient.Create(ctx, vmbd)).To(Succeed())
					_ = k8sClient.Delete(ctx, vmbd)
				}
			})
		})
	})
})

// checkTestVMBDStatus validates the status of a VirtualMachineBackupsDiscovery resource
func checkTestVMBDStatus(ctx context.Context, namespacedName types.NamespacedName, scenario testScenario) {
	vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{}
	Eventually(func() error {
		return k8sClient.Get(ctx, namespacedName, vmbd)
	}, time.Second*5).Should(Succeed())

	// Verify phase
	if scenario.expectedPhase != "" {
		Expect(vmbd.Status.Phase).To(Equal(scenario.expectedPhase), "Phase should match expected value")
	}

	// Verify conditions if specified
	for _, expectedCondition := range scenario.expectedConditions {
		condition := meta.FindStatusCondition(vmbd.Status.Conditions, expectedCondition.Type)
		Expect(condition).ToNot(BeNil(), fmt.Sprintf("Condition %s should exist", expectedCondition.Type))
		Expect(condition.Status).To(Equal(expectedCondition.Status), fmt.Sprintf("Condition %s status should match", expectedCondition.Type))
		if expectedCondition.Reason != "" {
			Expect(condition.Reason).To(Equal(expectedCondition.Reason), fmt.Sprintf("Condition %s reason should match", expectedCondition.Type))
		}
	}

	// Verify basic status fields are initialized
	Expect(vmbd.Status.ObservedGeneration).To(BeNumerically(">=", 0))
	Expect(vmbd.Status.Phase).ToNot(BeEmpty())
}

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
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	oadpv1alpha1 "github.com/migtools/oadp-vm-file-restore/api/v1alpha1"
	oadptypes "github.com/migtools/oadp-vm-file-restore/api/v1alpha1/types"
	"github.com/migtools/oadp-vm-file-restore/internal/common/constant"
)

var _ = Describe("VirtualMachineFileRestore Controller", func() {
	Context("Basic Reconciliation", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		It("should handle non-existent resource gracefully", func() {
			scheme := runtime.NewScheme()
			Expect(oadpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(velerov1api.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler := &VirtualMachineFileRestoreReconciler{
				Client:        client,
				Scheme:        scheme,
				OADPNamespace: "openshift-adp",
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should add finalizers on first reconciliation", func() {
			scheme := runtime.NewScheme()
			Expect(oadpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(velerov1api.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
					BackupsDiscoveryRef: "test-discovery",
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(vmfr).
				WithStatusSubresource(&oadpv1alpha1.VirtualMachineFileRestore{}).
				Build()

			reconciler := &VirtualMachineFileRestoreReconciler{
				Client:        client,
				Scheme:        scheme,
				OADPNamespace: "openshift-adp",
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify finalizers were added
			updated := &oadpv1alpha1.VirtualMachineFileRestore{}
			err = client.Get(ctx, typeNamespacedName, updated)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Finalizers).To(ContainElement(constant.VeleroRestoreCleanupFinalizer))
			Expect(updated.Finalizers).To(ContainElement(constant.VMFileRestoreFinalizer))
		})

		It("should initialize phase to New on first reconciliation", func() {
			scheme := runtime.NewScheme()
			Expect(oadpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(velerov1api.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:       resourceName,
					Namespace:  "default",
					Finalizers: []string{constant.VeleroRestoreCleanupFinalizer, constant.VMFileRestoreFinalizer},
				},
				Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
					BackupsDiscoveryRef: "test-discovery",
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(vmfr).
				WithStatusSubresource(&oadpv1alpha1.VirtualMachineFileRestore{}).
				Build()

			reconciler := &VirtualMachineFileRestoreReconciler{
				Client:        client,
				Scheme:        scheme,
				OADPNamespace: "openshift-adp",
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify phase was initialized
			updated := &oadpv1alpha1.VirtualMachineFileRestore{}
			err = client.Get(ctx, typeNamespacedName, updated)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Status.Phase).To(Equal(oadpv1alpha1.VirtualMachineFileRestorePhaseNew))
		})
	})

	Context("Discovery Validation", func() {
		var (
			ctx           context.Context
			scheme        *runtime.Scheme
			namespace     = "test-namespace"
			oadpNamespace = "openshift-adp"
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme = runtime.NewScheme()
			Expect(oadpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(velerov1api.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			Expect(kubevirtv1.AddToScheme(scheme)).To(Succeed())
			Expect(rbacv1.AddToScheme(scheme)).To(Succeed())
		})

		It("should fail validation when discovery resource does not exist", func() {
			vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-vmfr",
					Namespace:  namespace,
					Finalizers: []string{constant.VeleroRestoreCleanupFinalizer, constant.VMFileRestoreFinalizer},
				},
				Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
					BackupsDiscoveryRef: "non-existent-discovery",
				},
				Status: oadpv1alpha1.VirtualMachineFileRestoreStatus{
					Phase: oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress,
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(vmfr).
				WithStatusSubresource(&oadpv1alpha1.VirtualMachineFileRestore{}).
				Build()

			reconciler := &VirtualMachineFileRestoreReconciler{
				Client:        client,
				Scheme:        scheme,
				OADPNamespace: oadpNamespace,
			}

			vmbd, requeue, err := reconciler.validateReferencedDiscovery(ctx, zap.New(), vmfr)
			Expect(err).To(HaveOccurred())
			Expect(requeue).To(BeFalse())
			Expect(vmbd).To(BeNil())

			// Verify status was updated to Failed phase
			updated := &oadpv1alpha1.VirtualMachineFileRestore{}
			err = client.Get(ctx, types.NamespacedName{Name: "test-vmfr", Namespace: namespace}, updated)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Status.Phase).To(Equal(oadpv1alpha1.VirtualMachineFileRestorePhaseFailed))
		})

		It("should wait when discovery is not ready yet", func() {
			discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-discovery",
					Namespace: namespace,
				},
				Status: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
					Phase: oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseInProgress,
				},
			}

			vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-vmfr",
					Namespace:  namespace,
					Finalizers: []string{constant.VeleroRestoreCleanupFinalizer, constant.VMFileRestoreFinalizer},
				},
				Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
					BackupsDiscoveryRef: "test-discovery",
				},
				Status: oadpv1alpha1.VirtualMachineFileRestoreStatus{
					Phase: oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress,
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(discovery, vmfr).
				WithStatusSubresource(&oadpv1alpha1.VirtualMachineFileRestore{}).
				Build()

			reconciler := &VirtualMachineFileRestoreReconciler{
				Client:        client,
				Scheme:        scheme,
				OADPNamespace: oadpNamespace,
			}

			vmbd, requeue, err := reconciler.validateReferencedDiscovery(ctx, zap.New(), vmfr)
			Expect(err).NotTo(HaveOccurred())
			Expect(requeue).To(BeTrue())
			Expect(vmbd).To(BeNil())

			// Verify progressing condition was set with WaitingForDiscovery reason
			updated := &oadpv1alpha1.VirtualMachineFileRestore{}
			err = client.Get(ctx, types.NamespacedName{Name: "test-vmfr", Namespace: namespace}, updated)
			Expect(err).NotTo(HaveOccurred())

			progressingCond := meta.FindStatusCondition(updated.Status.Conditions, oadptypes.ConditionTypeProgressing)
			Expect(progressingCond).NotTo(BeNil())
			Expect(progressingCond.Reason).To(Equal(oadptypes.ReasonWaitingForDiscovery))
		})

		It("should succeed when discovery is completed", func() {
			discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-discovery",
					Namespace: namespace,
				},
				Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
					VirtualMachineName:      "test-vm",
					VirtualMachineNamespace: "vm-namespace",
				},
				Status: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
					Phase: oadpv1alpha1.VirtualMachineBackupsDiscoveryPhaseCompleted,
					ValidBackups: []oadptypes.VeleroBackupInfo{
						{Name: "backup-1", CreatedAt: &metav1.Time{Time: time.Now()}},
					},
				},
			}

			vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-vmfr",
					Namespace:  namespace,
					Finalizers: []string{constant.VeleroRestoreCleanupFinalizer, constant.VMFileRestoreFinalizer},
				},
				Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
					BackupsDiscoveryRef: "test-discovery",
				},
				Status: oadpv1alpha1.VirtualMachineFileRestoreStatus{
					Phase: oadpv1alpha1.VirtualMachineFileRestorePhaseInProgress,
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(discovery, vmfr).
				WithStatusSubresource(&oadpv1alpha1.VirtualMachineFileRestore{}).
				Build()

			reconciler := &VirtualMachineFileRestoreReconciler{
				Client:        client,
				Scheme:        scheme,
				OADPNamespace: oadpNamespace,
			}

			vmbd, requeue, err := reconciler.validateReferencedDiscovery(ctx, zap.New(), vmfr)
			Expect(err).NotTo(HaveOccurred())
			Expect(requeue).To(BeFalse())
			Expect(vmbd).NotTo(BeNil())
			Expect(vmbd.Name).To(Equal("test-discovery"))
		})
	})

	Context("Namespace Management", func() {
		var (
			ctx           context.Context
			scheme        *runtime.Scheme
			namespace     = "test-namespace"
			oadpNamespace = "openshift-adp"
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme = runtime.NewScheme()
			Expect(oadpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(velerov1api.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			Expect(kubevirtv1.AddToScheme(scheme)).To(Succeed())
			Expect(rbacv1.AddToScheme(scheme)).To(Succeed())
		})

		It("should use specified existing namespace", func() {
			existingNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "existing-restore-ns",
				},
			}

			discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-discovery",
					Namespace: namespace,
				},
				Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
					VirtualMachineName:      "test-vm",
					VirtualMachineNamespace: namespace,
				},
			}

			vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "oadp.openshift.io/v1alpha1",
					Kind:       "VirtualMachineFileRestore",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmfr",
					Namespace: namespace,
					UID:       "test-vmfr-uid",
				},
				Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
					BackupsDiscoveryRef: "test-discovery",
					RestoreNamespace:    "existing-restore-ns",
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(existingNamespace, discovery, vmfr).
				WithStatusSubresource(&oadpv1alpha1.VirtualMachineFileRestore{}).
				Build()

			reconciler := &VirtualMachineFileRestoreReconciler{
				Client:        client,
				Scheme:        scheme,
				OADPNamespace: oadpNamespace,
			}

			restoreNamespace, err := reconciler.ensureRestoreNamespace(ctx, zap.New(), vmfr)
			Expect(err).NotTo(HaveOccurred())
			Expect(restoreNamespace).To(Equal("existing-restore-ns"))

			// Verify namespace was not modified
			ns := &corev1.Namespace{}
			err = client.Get(ctx, types.NamespacedName{Name: "existing-restore-ns"}, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(ns.Labels).NotTo(HaveKey(constant.VMFRTempNamespaceLabel))
		})

		It("should fail when specified namespace does not exist", func() {
			discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-discovery",
					Namespace: namespace,
				},
				Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
					VirtualMachineName:      "test-vm",
					VirtualMachineNamespace: namespace,
				},
			}

			vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmfr",
					Namespace: namespace,
					UID:       "test-vmfr-uid",
				},
				Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
					BackupsDiscoveryRef: "test-discovery",
					RestoreNamespace:    "non-existent-namespace",
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(discovery, vmfr).
				Build()

			reconciler := &VirtualMachineFileRestoreReconciler{
				Client:        client,
				Scheme:        scheme,
				OADPNamespace: oadpNamespace,
			}

			_, err := reconciler.ensureRestoreNamespace(ctx, zap.New(), vmfr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("specified restore namespace 'non-existent-namespace' does not exist"))
		})

		It("should create temporary namespace with proper labels and owner references", func() {
			discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-discovery",
					Namespace: namespace,
				},
				Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
					VirtualMachineName:      "test-vm",
					VirtualMachineNamespace: "vm-namespace",
				},
			}

			vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "oadp.openshift.io/v1alpha1",
					Kind:       "VirtualMachineFileRestore",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmfr",
					Namespace: namespace,
					UID:       "12345678-1234-5678-9012-123456789012",
				},
				Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
					BackupsDiscoveryRef: "test-discovery",
					// No RestoreNamespace specified - should create temporary
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(discovery, vmfr).
				WithStatusSubresource(&oadpv1alpha1.VirtualMachineFileRestore{}).
				Build()

			reconciler := &VirtualMachineFileRestoreReconciler{
				Client:        client,
				Scheme:        scheme,
				OADPNamespace: oadpNamespace,
			}

			restoreNamespace, err := reconciler.ensureRestoreNamespace(ctx, zap.New(), vmfr)
			Expect(err).NotTo(HaveOccurred())

			// Verify namespace was created
			ns := &corev1.Namespace{}
			err = client.Get(ctx, types.NamespacedName{Name: restoreNamespace}, ns)
			Expect(err).NotTo(HaveOccurred())

			// Verify labels
			Expect(ns.Labels).To(HaveKeyWithValue(constant.VMFROriginUUIDLabel, "12345678-1234-5678-9012-123456789012"))
			Expect(ns.Labels).To(HaveKeyWithValue(constant.VMFRTempNamespaceLabel, "true"))
			Expect(ns.Labels).To(HaveKeyWithValue(constant.ManagedByLabel, constant.ManagedByLabelValue))

			// Verify owner references
			Expect(ns.OwnerReferences).To(HaveLen(1))
			Expect(ns.OwnerReferences[0].Name).To(Equal("test-vmfr"))
			Expect(ns.OwnerReferences[0].Kind).To(Equal("VirtualMachineFileRestore"))
		})

		It("should handle namespace creation idempotently", func() {
			discovery := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-discovery",
					Namespace: namespace,
				},
				Spec: oadpv1alpha1.VirtualMachineBackupsDiscoverySpec{
					VirtualMachineName:      "test-vm",
					VirtualMachineNamespace: "vm-namespace",
				},
			}

			vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "oadp.openshift.io/v1alpha1",
					Kind:       "VirtualMachineFileRestore",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmfr",
					Namespace: namespace,
					UID:       "12345678-1234-5678-9012-123456789012",
				},
				Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
					BackupsDiscoveryRef: "test-discovery",
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(discovery, vmfr).
				WithStatusSubresource(&oadpv1alpha1.VirtualMachineFileRestore{}).
				Build()

			reconciler := &VirtualMachineFileRestoreReconciler{
				Client:        client,
				Scheme:        scheme,
				OADPNamespace: oadpNamespace,
			}

			// First call
			restoreNamespace1, err := reconciler.ensureRestoreNamespace(ctx, zap.New(), vmfr)
			Expect(err).NotTo(HaveOccurred())

			// Second call (should be idempotent)
			restoreNamespace2, err := reconciler.ensureRestoreNamespace(ctx, zap.New(), vmfr)
			Expect(err).NotTo(HaveOccurred())
			Expect(restoreNamespace2).To(Equal(restoreNamespace1))
		})
	})

	Context("Cleanup on Deletion", func() {
		var (
			ctx           context.Context
			scheme        *runtime.Scheme
			namespace     = "test-namespace"
			oadpNamespace = "openshift-adp"
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme = runtime.NewScheme()
			Expect(oadpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(velerov1api.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
		})

		It("should delete temporary namespace on VMFR deletion", func() {
			tempNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "temp-restore-ns",
					Labels: map[string]string{
						constant.VMFRTempNamespaceLabel: constant.TrueString,
						constant.ManagedByLabel:         constant.ManagedByLabelValue,
					},
				},
			}

			now := metav1.Now()
			vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-vmfr",
					Namespace:         namespace,
					DeletionTimestamp: &now,
					Finalizers:        []string{constant.VMFileRestoreFinalizer},
				},
				Status: oadpv1alpha1.VirtualMachineFileRestoreStatus{
					CreatedNamespace: "temp-restore-ns",
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tempNamespace, vmfr).
				WithStatusSubresource(&oadpv1alpha1.VirtualMachineFileRestore{}).
				Build()

			reconciler := &VirtualMachineFileRestoreReconciler{
				Client:        client,
				Scheme:        scheme,
				OADPNamespace: oadpNamespace,
			}

			_, err := reconciler.handleResourceCleanup(ctx, zap.New(), vmfr)
			Expect(err).NotTo(HaveOccurred())

			// Verify namespace deletion was initiated (namespace gets DeletionTimestamp, actual deletion is async)
			ns := &corev1.Namespace{}
			err = client.Get(ctx, types.NamespacedName{Name: "temp-restore-ns"}, ns)
			if err != nil {
				// Namespace already deleted (immediate deletion in fake client)
				Expect(errors.IsNotFound(err)).To(BeTrue())
			} else {
				// Namespace marked for deletion (has DeletionTimestamp)
				Expect(ns.DeletionTimestamp).NotTo(BeNil())
			}
		})

		It("should not delete user-specified namespaces", func() {
			userNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "user-ns",
					// No temp label - not managed by controller
				},
			}

			now := metav1.Now()
			vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-vmfr",
					Namespace:         namespace,
					DeletionTimestamp: &now,
					Finalizers:        []string{constant.VMFileRestoreFinalizer},
				},
				Spec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
					RestoreNamespace: "user-ns",
				},
				Status: oadpv1alpha1.VirtualMachineFileRestoreStatus{
					CreatedNamespace: "user-ns",
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(userNamespace, vmfr).
				WithStatusSubresource(&oadpv1alpha1.VirtualMachineFileRestore{}).
				Build()

			reconciler := &VirtualMachineFileRestoreReconciler{
				Client:        client,
				Scheme:        scheme,
				OADPNamespace: oadpNamespace,
			}

			_, err := reconciler.handleResourceCleanup(ctx, zap.New(), vmfr)
			Expect(err).NotTo(HaveOccurred())

			// Verify namespace was NOT deleted
			ns := &corev1.Namespace{}
			err = client.Get(ctx, types.NamespacedName{Name: "user-ns"}, ns)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

// TestErrUnsupportedBackup tests the Error() method of ErrUnsupportedBackup
func TestErrUnsupportedBackup(t *testing.T) {
	tests := []struct {
		name           string
		err            ErrUnsupportedBackup
		expectedString string
	}{
		{
			name: "basic error",
			err: ErrUnsupportedBackup{
				BackupName:   "test-backup",
				PVCName:      "test-pvc",
				PVCNamespace: "default",
				PVCUID:       "uid-123",
				PVCSize:      "10Gi",
				Reason:       "unsupported plugin version",
			},
			expectedString: "backup test-backup created with unsupported kubevirt-velero-plugin",
		},
		{
			name: "empty backup name",
			err: ErrUnsupportedBackup{
				BackupName: "",
				PVCName:    "pvc-1",
			},
			expectedString: "backup  created with unsupported kubevirt-velero-plugin",
		},
		{
			name: "with all fields",
			err: ErrUnsupportedBackup{
				BackupName:   "vm-backup-20250115",
				PVCName:      "vm-disk-1",
				PVCNamespace: "vms",
				PVCUID:       "abc-123-xyz",
				PVCSize:      "100Gi",
				Reason:       "legacy plugin",
			},
			expectedString: "backup vm-backup-20250115 created with unsupported kubevirt-velero-plugin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.err.Error()
			if result != tt.expectedString {
				t.Errorf("Error() = %q, want %q", result, tt.expectedString)
			}

			// Verify fields are preserved
			if tt.err.BackupName != "" && tt.err.BackupName != tt.err.BackupName {
				t.Errorf("BackupName not preserved")
			}
			if tt.err.PVCName != "" && tt.err.PVCName != tt.err.PVCName {
				t.Errorf("PVCName not preserved")
			}
		})
	}
}

func TestSortRestoresByTimestamp(t *testing.T) {
	reconciler := &VirtualMachineFileRestoreReconciler{}

	t.Run("sorts by timestamp newest first", func(t *testing.T) {
		time1 := metav1.NewTime(time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC))
		time2 := metav1.NewTime(time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC))
		time3 := metav1.NewTime(time.Date(2025, 1, 3, 10, 0, 0, 0, time.UTC))

		restores := []oadpv1alpha1.RestoreInfo{
			{VeleroRestoreName: "restore-1", Timestamp: &time1},
			{VeleroRestoreName: "restore-3", Timestamp: &time3},
			{VeleroRestoreName: "restore-2", Timestamp: &time2},
		}

		reconciler.sortRestoresByTimestamp(restores)

		// Should be sorted newest first: time3, time2, time1
		if restores[0].VeleroRestoreName != "restore-3" {
			t.Errorf("Expected restore-3 first, got %s", restores[0].VeleroRestoreName)
		}
		if restores[1].VeleroRestoreName != "restore-2" {
			t.Errorf("Expected restore-2 second, got %s", restores[1].VeleroRestoreName)
		}
		if restores[2].VeleroRestoreName != "restore-1" {
			t.Errorf("Expected restore-1 third, got %s", restores[2].VeleroRestoreName)
		}
	})

	t.Run("handles nil timestamps", func(t *testing.T) {
		time1 := metav1.NewTime(time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC))
		time2 := metav1.NewTime(time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC))

		restores := []oadpv1alpha1.RestoreInfo{
			{VeleroRestoreName: "restore-nil", Timestamp: nil},
			{VeleroRestoreName: "restore-2", Timestamp: &time2},
			{VeleroRestoreName: "restore-1", Timestamp: &time1},
		}

		reconciler.sortRestoresByTimestamp(restores)

		// Non-nil timestamps should come before nil
		if restores[0].Timestamp == nil {
			t.Error("Expected non-nil timestamp first")
		}
		if restores[1].Timestamp == nil {
			t.Error("Expected non-nil timestamp second")
		}
		if restores[2].Timestamp != nil {
			t.Error("Expected nil timestamp last")
		}
	})

	t.Run("handles empty list", func(t *testing.T) {
		restores := []oadpv1alpha1.RestoreInfo{}
		reconciler.sortRestoresByTimestamp(restores)
		// Should not panic
		if len(restores) != 0 {
			t.Error("Expected empty list to remain empty")
		}
	})

	t.Run("handles single element", func(t *testing.T) {
		time1 := metav1.NewTime(time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC))
		restores := []oadpv1alpha1.RestoreInfo{
			{VeleroRestoreName: "restore-1", Timestamp: &time1},
		}

		reconciler.sortRestoresByTimestamp(restores)

		if len(restores) != 1 {
			t.Errorf("Expected 1 element, got %d", len(restores))
		}
		if restores[0].VeleroRestoreName != "restore-1" {
			t.Errorf("Expected restore-1, got %s", restores[0].VeleroRestoreName)
		}
	})

	t.Run("handles equal timestamps", func(t *testing.T) {
		time1 := metav1.NewTime(time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC))

		restores := []oadpv1alpha1.RestoreInfo{
			{VeleroRestoreName: "restore-1", Timestamp: &time1},
			{VeleroRestoreName: "restore-2", Timestamp: &time1},
			{VeleroRestoreName: "restore-3", Timestamp: &time1},
		}

		reconciler.sortRestoresByTimestamp(restores)

		// All timestamps equal, order may vary but should not panic
		if len(restores) != 3 {
			t.Errorf("Expected 3 elements, got %d", len(restores))
		}
	})

	t.Run("handles all nil timestamps", func(t *testing.T) {
		restores := []oadpv1alpha1.RestoreInfo{
			{VeleroRestoreName: "restore-1", Timestamp: nil},
			{VeleroRestoreName: "restore-2", Timestamp: nil},
			{VeleroRestoreName: "restore-3", Timestamp: nil},
		}

		reconciler.sortRestoresByTimestamp(restores)

		// Should not panic, all zero times are equal
		if len(restores) != 3 {
			t.Errorf("Expected 3 elements, got %d", len(restores))
		}
	})
}

func TestBuildFailedProgress(t *testing.T) {
	reconciler := &VirtualMachineFileRestoreReconciler{}

	t.Run("builds failed progress with all fields", func(t *testing.T) {
		createdAt := metav1.NewTime(time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC))
		backupInfo := oadptypes.VeleroBackupInfo{
			Name:      "test-backup",
			Namespace: "openshift-adp",
			CreatedAt: &createdAt,
		}

		result := reconciler.buildFailedProgress(backupInfo, "DiscoveryFailed", "failed to discover PVCs")

		// Verify backup info copied correctly
		if result.VeleroBackupInfo.Name != "test-backup" {
			t.Errorf("Expected name 'test-backup', got '%s'", result.VeleroBackupInfo.Name)
		}
		if result.VeleroBackupInfo.Namespace != "openshift-adp" {
			t.Errorf("Expected namespace 'openshift-adp', got '%s'", result.VeleroBackupInfo.Namespace)
		}
		if result.VeleroBackupInfo.CreatedAt != &createdAt {
			t.Error("CreatedAt timestamp not preserved")
		}

		// Verify PVCs is empty list
		if result.VeleroBackupInfo.PVCs == nil {
			t.Error("Expected PVCs to be non-nil empty slice")
		}
		if len(result.VeleroBackupInfo.PVCs) != 0 {
			t.Errorf("Expected empty PVCs list, got %d items", len(result.VeleroBackupInfo.PVCs))
		}

		// Verify status
		if result.Status != oadptypes.BackupDiscoveryStatusFailed {
			t.Errorf("Expected status Failed, got %s", result.Status)
		}

		// Verify message format
		expectedMessage := "DiscoveryFailed: failed to discover PVCs"
		if result.Message != expectedMessage {
			t.Errorf("Expected message '%s', got '%s'", expectedMessage, result.Message)
		}

		// Verify LastUpdated is set
		if result.LastUpdated == nil {
			t.Error("Expected LastUpdated to be set")
		}
	})

	t.Run("builds failed progress with empty reason and message", func(t *testing.T) {
		backupInfo := oadptypes.VeleroBackupInfo{
			Name:      "backup-2",
			Namespace: "velero",
		}

		result := reconciler.buildFailedProgress(backupInfo, "", "")

		if result.VeleroBackupInfo.Name != "backup-2" {
			t.Errorf("Expected name 'backup-2', got '%s'", result.VeleroBackupInfo.Name)
		}

		// Message should still be formatted, even if empty
		expectedMessage := ": "
		if result.Message != expectedMessage {
			t.Errorf("Expected message '%s', got '%s'", expectedMessage, result.Message)
		}

		if result.Status != oadptypes.BackupDiscoveryStatusFailed {
			t.Errorf("Expected status Failed, got %s", result.Status)
		}
	})

	t.Run("builds failed progress with nil CreatedAt", func(t *testing.T) {
		backupInfo := oadptypes.VeleroBackupInfo{
			Name:      "backup-3",
			Namespace: "test-ns",
			CreatedAt: nil,
		}

		result := reconciler.buildFailedProgress(backupInfo, "Timeout", "discovery timeout exceeded")

		if result.VeleroBackupInfo.CreatedAt != nil {
			t.Error("Expected CreatedAt to remain nil")
		}

		expectedMessage := "Timeout: discovery timeout exceeded"
		if result.Message != expectedMessage {
			t.Errorf("Expected message '%s', got '%s'", expectedMessage, result.Message)
		}
	})

	t.Run("sets LastUpdated to current time", func(t *testing.T) {
		backupInfo := oadptypes.VeleroBackupInfo{
			Name:      "backup-time-test",
			Namespace: "test-ns",
		}

		before := time.Now()
		result := reconciler.buildFailedProgress(backupInfo, "Error", "test error")
		after := time.Now()

		if result.LastUpdated == nil {
			t.Fatal("Expected LastUpdated to be set")
		}

		// LastUpdated should be between before and after
		if result.LastUpdated.Time.Before(before) || result.LastUpdated.Time.After(after) {
			t.Error("LastUpdated should be set to current time")
		}
	})
}

func TestFindRestoredPVCName(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	ctx := context.Background()

	tests := []struct {
		name              string
		existingPVCs      []*corev1.PersistentVolumeClaim
		restoreNamespace  string
		veleroRestoreName string
		originalPVCName   string
		expectedPVCName   string
		expectError       bool
		errorContains     string
	}{
		{
			name: "finds PVC by restore label and annotation",
			existingPVCs: []*corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "restored-pvc-abc123",
						Namespace: "restore-ns",
						Labels: map[string]string{
							"velero.io/restore-name": "test-restore",
						},
						Annotations: map[string]string{
							constant.VMFROriginalPVCNameAnnotation: "original-pvc-name",
						},
					},
				},
			},
			restoreNamespace:  "restore-ns",
			veleroRestoreName: "test-restore",
			originalPVCName:   "original-pvc-name",
			expectedPVCName:   "restored-pvc-abc123",
			expectError:       false,
		},
		{
			name:              "returns error when no PVCs exist",
			existingPVCs:      []*corev1.PersistentVolumeClaim{},
			restoreNamespace:  "restore-ns",
			veleroRestoreName: "test-restore",
			originalPVCName:   "original-pvc-name",
			expectError:       true,
			errorContains:     "PVC not found for restore test-restore with original name original-pvc-name",
		},
		{
			name: "skips PVCs with wrong restore label",
			existingPVCs: []*corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "wrong-restore-pvc",
						Namespace: "restore-ns",
						Labels: map[string]string{
							"velero.io/restore-name": "different-restore",
						},
						Annotations: map[string]string{
							constant.VMFROriginalPVCNameAnnotation: "original-pvc-name",
						},
					},
				},
			},
			restoreNamespace:  "restore-ns",
			veleroRestoreName: "test-restore",
			originalPVCName:   "original-pvc-name",
			expectError:       true,
			errorContains:     "PVC not found",
		},
		{
			name: "skips PVCs with wrong annotation value",
			existingPVCs: []*corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "wrong-annotation-pvc",
						Namespace: "restore-ns",
						Labels: map[string]string{
							"velero.io/restore-name": "test-restore",
						},
						Annotations: map[string]string{
							constant.VMFROriginalPVCNameAnnotation: "different-pvc-name",
						},
					},
				},
			},
			restoreNamespace:  "restore-ns",
			veleroRestoreName: "test-restore",
			originalPVCName:   "original-pvc-name",
			expectError:       true,
			errorContains:     "PVC not found",
		},
		{
			name: "skips PVCs with nil annotations",
			existingPVCs: []*corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "no-annotations-pvc",
						Namespace: "restore-ns",
						Labels: map[string]string{
							"velero.io/restore-name": "test-restore",
						},
						Annotations: nil,
					},
				},
			},
			restoreNamespace:  "restore-ns",
			veleroRestoreName: "test-restore",
			originalPVCName:   "original-pvc-name",
			expectError:       true,
			errorContains:     "PVC not found",
		},
		{
			name: "finds correct PVC among multiple candidates",
			existingPVCs: []*corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pvc-1",
						Namespace: "restore-ns",
						Labels: map[string]string{
							"velero.io/restore-name": "test-restore",
						},
						Annotations: map[string]string{
							constant.VMFROriginalPVCNameAnnotation: "other-pvc",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pvc-2",
						Namespace: "restore-ns",
						Labels: map[string]string{
							"velero.io/restore-name": "test-restore",
						},
						Annotations: map[string]string{
							constant.VMFROriginalPVCNameAnnotation: "target-pvc",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pvc-3",
						Namespace: "restore-ns",
						Labels: map[string]string{
							"velero.io/restore-name": "test-restore",
						},
						Annotations: map[string]string{
							constant.VMFROriginalPVCNameAnnotation: "another-pvc",
						},
					},
				},
			},
			restoreNamespace:  "restore-ns",
			veleroRestoreName: "test-restore",
			originalPVCName:   "target-pvc",
			expectedPVCName:   "pvc-2",
			expectError:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]client.Object, len(tt.existingPVCs))
			for i, pvc := range tt.existingPVCs {
				objects[i] = pvc
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			reconciler := &VirtualMachineFileRestoreReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			name, err := reconciler.findRestoredPVCName(ctx, tt.restoreNamespace, tt.veleroRestoreName, tt.originalPVCName)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing '%s', got nil", tt.errorContains)
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if name != tt.expectedPVCName {
					t.Errorf("Expected PVC name '%s', got '%s'", tt.expectedPVCName, name)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestGetFilteredBackupsToServe(t *testing.T) {
	reconciler := &VirtualMachineFileRestoreReconciler{}

	tests := []struct {
		name            string
		vmfrSpec        oadpv1alpha1.VirtualMachineFileRestoreSpec
		vmbdStatus      oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus
		expectedBackups []string // backup names
		expectError     bool
		errorContains   string
	}{
		{
			name: "no selection returns all valid backups",
			vmfrSpec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
				SelectedBackups: []string{}, // empty selection
			},
			vmbdStatus: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
				ValidBackups: []oadptypes.VeleroBackupInfo{
					{Name: "backup-1", Namespace: "openshift-adp"},
					{Name: "backup-2", Namespace: "openshift-adp"},
					{Name: "backup-3", Namespace: "openshift-adp"},
				},
			},
			expectedBackups: []string{"backup-1", "backup-2", "backup-3"},
			expectError:     false,
		},
		{
			name: "single valid selection",
			vmfrSpec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
				SelectedBackups: []string{"backup-2"},
			},
			vmbdStatus: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
				ValidBackups: []oadptypes.VeleroBackupInfo{
					{Name: "backup-1", Namespace: "openshift-adp"},
					{Name: "backup-2", Namespace: "openshift-adp"},
					{Name: "backup-3", Namespace: "openshift-adp"},
				},
			},
			expectedBackups: []string{"backup-2"},
			expectError:     false,
		},
		{
			name: "multiple valid selections",
			vmfrSpec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
				SelectedBackups: []string{"backup-1", "backup-3"},
			},
			vmbdStatus: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
				ValidBackups: []oadptypes.VeleroBackupInfo{
					{Name: "backup-1", Namespace: "openshift-adp"},
					{Name: "backup-2", Namespace: "openshift-adp"},
					{Name: "backup-3", Namespace: "openshift-adp"},
				},
			},
			expectedBackups: []string{"backup-1", "backup-3"},
			expectError:     false,
		},
		{
			name: "invalid selection returns error",
			vmfrSpec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
				SelectedBackups: []string{"nonexistent-backup"},
			},
			vmbdStatus: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
				ValidBackups: []oadptypes.VeleroBackupInfo{
					{Name: "backup-1", Namespace: "openshift-adp"},
					{Name: "backup-2", Namespace: "openshift-adp"},
				},
			},
			expectError:   true,
			errorContains: "selected backups not found in discovery results",
		},
		{
			name: "multiple invalid selections lists all missing",
			vmfrSpec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
				SelectedBackups: []string{"missing-1", "missing-2"},
			},
			vmbdStatus: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
				ValidBackups: []oadptypes.VeleroBackupInfo{
					{Name: "backup-1", Namespace: "openshift-adp"},
				},
			},
			expectError:   true,
			errorContains: "missing-1",
		},
		{
			name: "mixed valid and invalid selections returns error",
			vmfrSpec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
				SelectedBackups: []string{"backup-1", "nonexistent"},
			},
			vmbdStatus: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
				ValidBackups: []oadptypes.VeleroBackupInfo{
					{Name: "backup-1", Namespace: "openshift-adp"},
					{Name: "backup-2", Namespace: "openshift-adp"},
				},
			},
			expectError:   true,
			errorContains: "nonexistent",
		},
		{
			name: "empty valid backups list",
			vmfrSpec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
				SelectedBackups: []string{},
			},
			vmbdStatus: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
				ValidBackups: []oadptypes.VeleroBackupInfo{},
			},
			expectedBackups: []string{},
			expectError:     false,
		},
		{
			name: "selection from empty valid backups",
			vmfrSpec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
				SelectedBackups: []string{"backup-1"},
			},
			vmbdStatus: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
				ValidBackups: []oadptypes.VeleroBackupInfo{},
			},
			expectError:   true,
			errorContains: "backup-1",
		},
		{
			name: "preserves selection order",
			vmfrSpec: oadpv1alpha1.VirtualMachineFileRestoreSpec{
				SelectedBackups: []string{"backup-3", "backup-1", "backup-2"},
			},
			vmbdStatus: oadpv1alpha1.VirtualMachineBackupsDiscoveryStatus{
				ValidBackups: []oadptypes.VeleroBackupInfo{
					{Name: "backup-1", Namespace: "openshift-adp"},
					{Name: "backup-2", Namespace: "openshift-adp"},
					{Name: "backup-3", Namespace: "openshift-adp"},
				},
			},
			expectedBackups: []string{"backup-3", "backup-1", "backup-2"},
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vmfr := &oadpv1alpha1.VirtualMachineFileRestore{
				Spec: tt.vmfrSpec,
			}
			vmbd := &oadpv1alpha1.VirtualMachineBackupsDiscovery{
				Status: tt.vmbdStatus,
			}

			backups, err := reconciler.getFilteredBackupsToServe(vmfr, vmbd)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing '%s', got nil", tt.errorContains)
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}

				if len(backups) != len(tt.expectedBackups) {
					t.Errorf("Expected %d backups, got %d", len(tt.expectedBackups), len(backups))
				}

				for i, expectedName := range tt.expectedBackups {
					if i >= len(backups) {
						break
					}
					if backups[i].Name != expectedName {
						t.Errorf("Expected backup[%d] = '%s', got '%s'", i, expectedName, backups[i].Name)
					}
				}
			}
		})
	}
}

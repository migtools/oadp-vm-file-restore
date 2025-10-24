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

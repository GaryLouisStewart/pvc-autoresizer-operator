package controller

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1 "github.com/garylouisstewart/pvc-autoresizer-operator/api/v1alpha1"
)

// Define a Ginkgo Describe block instead of a standard Test function
var _ = Describe("AutoResizePolicy Reconciler", func() {

	It("Should resize PVC when threshold is exceeded", func() {
		// Use the global variables (k8sClient, ctx) initialized in suite_test.go
		namespace := "default"

		policy := testPolicy(namespace)
		pvc := testPVC(namespace)
		sc := testStorageClass()

		// Create dependencies
		Expect(k8sClient.Create(ctx, sc)).To(Succeed())

		hostPathType := corev1.HostPathDirectoryOrCreate

		pv := &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pv",
			},
			Spec: corev1.PersistentVolumeSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Capacity: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("5Gi"),
				},
				PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
				StorageClassName:              "standard",
				PersistentVolumeSource: corev1.PersistentVolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/tmp/data",
						Type: ptr(hostPathType),
					},
				},
			},
		}

		Expect(k8sClient.Create(ctx, pv)).To(Succeed())
		Expect(k8sClient.Create(ctx, pvc)).To(Succeed())

		pvcBindPatch := client.MergeFrom(pvc.DeepCopy())
		pvc.Spec.VolumeName = pv.Name
		Expect(k8sClient.Patch(ctx, pvc, pvcBindPatch)).To(Succeed())

		pvcStatusPatch := client.MergeFrom(pvc.DeepCopy())
		pvc.Status.Phase = corev1.ClaimBound
		pvc.Status.Capacity = corev1.ResourceList{
			corev1.ResourceStorage: resource.MustParse("5Gi"),
		}
		Expect(k8sClient.Status().Patch(ctx, pvc, pvcStatusPatch)).To(Succeed())
		Expect(k8sClient.Create(ctx, policy)).To(Succeed())

		// Setup the Reconciler
		// We use the global scheme.Scheme which is populated in suite_test.go
		reconciler := &AutoResizePolicyReconciler{
			Client:   k8sClient,
			Scheme:   scheme.Scheme,
			Recorder: record.NewFakeRecorder(100),
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      policy.Name,
				Namespace: policy.Namespace,
			},
		}

		// Run the Reconcile loop
		_, err := reconciler.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		// Fetch the updated PVC to verify changes
		var updatedPVC corev1.PersistentVolumeClaim
		Expect(k8sClient.Get(
			ctx,
			types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace},
			&updatedPVC,
		)).To(Succeed())

		Expect(updatedPVC.Spec.Resources.Requests).NotTo(BeNil())

		// Extract value to variable to allow pointer method call
		newSize := updatedPVC.Spec.Resources.Requests[corev1.ResourceStorage]

		// Expectation: 5Gi * 1.25 = 6.25Gi.
		// MustParse("6Gi") is 6*(1024^3). 6.25 is larger.
		// We expect the new size to be strictly greater than 6Gi (or exactly 6.25Gi)
		Expect(newSize.Cmp(resource.MustParse("6Gi"))).To(BeNumerically(">", 0),
			fmt.Sprintf("Expected size > 6Gi, got %s", newSize.String()))
	})
})

// --- Helper Functions ---

func testPolicy(namespace string) *v1alpha1.AutoResizePolicy {
	return &v1alpha1.AutoResizePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-policy",
			Namespace: namespace,
		},
		Spec: v1alpha1.AutoResizePolicySpec{
			ThresholdPercent: 85,
			IncreasePercent:  25,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
			},
		},
	}
}

func testPVC(namespace string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pvc",
			Namespace: namespace,
			Labels: map[string]string{
				"app": "test",
			},
			Annotations: map[string]string{
				"pvc.gary.io/usage": "85",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: ptr("standard"),
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("5Gi"),
				},
			},
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
		},
	}
}

func ptr[T any](v T) *T {
	return &v
}

func testStorageClass() *storagev1.StorageClass {
	return &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "standard",
		},
		AllowVolumeExpansion: ptr(true),
		// this provisioner is the default storage class
		// provisioner on 'kind' which is used for bootstrapping the test environments
		Provisioner: "rancher.io/local-path",
	}
}

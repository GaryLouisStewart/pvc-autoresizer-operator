package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("PVC resize via Patch", func() {
	It("resizes the PVC without nil'ing resource requests", func() {
		ctx := context.Background()

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "default"},
		}

		err := k8sClient.Create(ctx, ns)
		Expect(client.IgnoreAlreadyExists(err)).To(Succeed())

		sc := testStorageClass()
		err = k8sClient.Create(ctx, sc)
		Expect(client.IgnoreAlreadyExists(err)).To(Succeed())

		HostPathType := corev1.HostPathDirectoryOrCreate
		pv := &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pv-patch",
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
						Path: "/tmp/data-patch",
						Type: ptr(HostPathType),
					},
				},
			},
		}

		err = k8sClient.Create(ctx, pv)
		Expect(client.IgnoreAlreadyExists(err)).To(Succeed())

		pvc := testPVC("default")
		pvc.Name = "test-pvc-resize"
		err = k8sClient.Create(ctx, pvc)
		Expect(client.IgnoreAlreadyExists(err)).To(Succeed())

		pvcBindPatch := client.MergeFrom(pvc.DeepCopy())
		pvc.Spec.VolumeName = pv.Name
		Expect(k8sClient.Patch(ctx, pvc, pvcBindPatch)).To(Succeed())

		pvcStatusPatch := client.MergeFrom(pvc.DeepCopy())
		pvc.Status.Phase = corev1.ClaimBound
		pvc.Status.Capacity = corev1.ResourceList{
			corev1.ResourceStorage: resource.MustParse("5Gi"),
		}

		Expect(k8sClient.Status().Patch(ctx, pvc, pvcStatusPatch)).To(Succeed())

		originalSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		newSize := calculateNewSize(originalSize, 25)

		Expect(resizePVC(ctx, k8sClient, pvc, newSize)).To(Succeed())

		var updated corev1.PersistentVolumeClaim
		Expect(k8sClient.Get(ctx,
			types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace},
			&updated,
		)).To(Succeed())

		Expect(updated.Spec.Resources.Requests).NotTo(BeNil())

		currentQuantity := updated.Spec.Resources.Requests[corev1.ResourceStorage]

		Expect(
			currentQuantity.Cmp(originalSize),
		).To(BeNumerically(">", 0))
	})
})

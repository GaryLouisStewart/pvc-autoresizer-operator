package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func resizePVC(
	ctx context.Context,
	c client.Client,
	pvc *corev1.PersistentVolumeClaim,
	newSize resource.Quantity,
) error {

	original := client.MergeFrom(pvc.DeepCopy())

	if pvc.Spec.Resources.Requests == nil {
		pvc.Spec.Resources.Requests = corev1.ResourceList{}
	}

	pvc.Spec.Resources.Requests[corev1.ResourceStorage] = newSize

	return c.Patch(ctx, pvc, original)
}

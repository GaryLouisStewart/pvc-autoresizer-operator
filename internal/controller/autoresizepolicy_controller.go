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
	//"math"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	controllerruntime "sigs.k8s.io/controller-runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"fmt"

	//"github.com/go-logr/logr/funcr"
	//"github.com/emicklei/go-restful/v3/log"
	"github.com/garylouisstewart/pvc-autoresizer-operator/api/v1alpha1"
	storagev1alpha1 "github.com/garylouisstewart/pvc-autoresizer-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	//metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//"k8s.io/apimachinery/pkg/labels"
)

// AutoResizePolicyReconciler reconciles a AutoResizePolicy object
type AutoResizePolicyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=storage.synaptikltd.io,resources=autoresizepolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.synaptikltd.io,resources=autoresizepolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.synaptikltd.io,resources=autoresizepolicies/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AutoResizePolicy object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *AutoResizePolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	// TODO(user): your logic here

	var autoResizePolicy v1alpha1.AutoResizePolicy
	if err := r.Get(ctx, req.NamespacedName, &autoResizePolicy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// build a selector from our custom-resource

	listOpts := []client.ListOption{
		client.InNamespace(req.Namespace),
	}

	if autoResizePolicy.Spec.Selector != nil {
		//labelSelector, err := metav1.LabelSelectorAsSelector(autoResizePolicy.Spec.Selector)
		//if err != nil {
		//	logf.Log.Error(err, "invalid label selector in AutoResizePolicy", "name", autoResizePolicy.Name)
		//	return ctrl.Result{}, nil
		//}
		//listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: labelSelector})

		if len(autoResizePolicy.Spec.Selector.MatchLabels) > 0 {
			listOpts = append(
				listOpts,
				client.MatchingLabels(autoResizePolicy.Spec.Selector.MatchLabels),
			)
		}
	}

	// gather and list all pvc(s), using pvcList as a store
	// search by using MatchingLabelSelector
	var pvcList corev1.PersistentVolumeClaimList

	if err := r.List(ctx, &pvcList, listOpts...); err != nil {
		logf.Log.Error(err, "unable to list PVCs")
		return ctrl.Result{}, err
	}

	fmt.Printf("DEBUG: PVCs listed count=%d\n", len(pvcList.Items))

	resizedPVCs := []string{}
	failedPVCs := []string{}

	// Loop through and check current requested usage on each PVC

	for _, pvc := range pvcList.Items {

		// extract current size
		currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]

		// gather current disk usage from the annotation/ prometheus / etc.
		usageStr := pvc.Annotations["pvc.gary.io/usage"] //v1: annotations
		usagePercent, err := strconv.Atoi(usageStr)
		if err != nil {
			logf.Log.Error(err, "Invalid usage annotation", "pvc", pvc.Name)
			continue
		}

		// compare against the threshold percentage

		if usagePercent < autoResizePolicy.Spec.ThresholdPercent {
			// threshold hasn't been exceeded so skip.
			continue
		}

		// check if the volume supports expansion before attempting a patch, this
		// prevents the operator from fail in clusters where it is disabled.

		if pvc.Spec.StorageClassName == nil {
			logf.Log.Info("PVC has no StorageClass, skipping", "pvc", pvc.Name)
			continue
		}

		var sc storagev1.StorageClass
		if err := r.Client.Get(ctx, types.NamespacedName{Name: *pvc.Spec.StorageClassName}, &sc); err != nil {
			logf.Log.Error(err, "failed to get StorageClass", "sc", *pvc.Spec.StorageClassName)
			continue
		}

		if sc.AllowVolumeExpansion == nil || !*sc.AllowVolumeExpansion {
			logf.Log.Info("StorageClass does not allow expansion, skipping",
				"pvc", pvc.Name,
				"sc", sc.Name,
			)
			continue
		}

		// get the new size using our helper function above.
		newSize := calculateNewSize(currentSize, autoResizePolicy.Spec.IncreasePercent)

		// prepare a deepcopy

		if err := resizePVC(ctx, r.Client, &pvc, newSize); err != nil {
			logf.Log.Error(err, "failed to patch PVC",
				"pvc", pvc.Name,
				"namespace", pvc.Namespace,
				"newSize", newSize.String(),
			)

			failedPVCs = append(failedPVCs, pvc.Namespace+"/"+pvc.Name)

			r.Recorder.Event(
				&pvc,
				corev1.EventTypeWarning,
				"ResizeFailed",
				err.Error(),
			)
			continue

		}

		logf.Log.Info("Resized PVC successfully",
			"pvc", pvc.Name,
			"oldSize", currentSize.String(),
			"newSize", newSize.String(),
		)
		// omit event on success to show in kubectl describe pvc/pvcname
		r.Recorder.Event(
			&pvc,
			corev1.EventTypeNormal,
			"Resized",
			fmt.Sprintf("Resized from %s to %s", currentSize.String(), newSize.String()),
		)

		resizedPVCs = append(resizedPVCs, fmt.Sprintf("%s/%s", pvc.Namespace, pvc.Name))
	}

	if len(resizedPVCs) > 0 {
		r.Recorder.Event(
			&autoResizePolicy,
			corev1.EventTypeNormal,
			"PVCResized",
			fmt.Sprintf("Resized %d PVC(s): %s",
				len(resizedPVCs),
				strings.Join(resizedPVCs, ", "),
			),
		)
	}

	if len(failedPVCs) > 0 {
		r.Recorder.Event(
			&autoResizePolicy,
			corev1.EventTypeWarning,
			"ResizeFailed",
			fmt.Sprintf("%d PVC(s) failed: %s",
				len(failedPVCs),
				strings.Join(failedPVCs, ", "),
			),
		)
	}
	return controllerruntime.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AutoResizePolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&storagev1alpha1.AutoResizePolicy{}).
		Named("autoresizepolicy").
		Complete(r)
}

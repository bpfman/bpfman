/*
Copyright 2024.
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

package bpfmanoperator

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman/bpfman-operator/apis/v1alpha1"
	"github.com/bpfman/bpfman/bpfman-operator/internal"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=fentryprograms,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfman.io,resources=fentryprograms/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfman.io,resources=fentryprograms/finalizers,verbs=update

type FentryProgramReconciler struct {
	ReconcilerCommon
}

func (r *FentryProgramReconciler) getRecCommon() *ReconcilerCommon {
	return &r.ReconcilerCommon
}

func (r *FentryProgramReconciler) getFinalizer() string {
	return internal.FentryProgramControllerFinalizer
}

// SetupWithManager sets up the controller with the Manager.
func (r *FentryProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.FentryProgram{}).
		// Watch bpfPrograms which are owned by FentryPrograms
		Watches(
			&source.Kind{Type: &bpfmaniov1alpha1.BpfProgram{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(statusChangedPredicate(), internal.BpfProgramTypePredicate(internal.Tracing.String()))),
		).
		Complete(r)
}

func (r *FentryProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = log.FromContext(ctx)

	fentryProgram := &bpfmaniov1alpha1.FentryProgram{}
	if err := r.Get(ctx, req.NamespacedName, fentryProgram); err != nil {
		// Reconcile was triggered by bpfProgram event, get parent FentryProgram Object.
		if errors.IsNotFound(err) {
			bpfProgram := &bpfmaniov1alpha1.BpfProgram{}
			if err := r.Get(ctx, req.NamespacedName, bpfProgram); err != nil {
				if errors.IsNotFound(err) {
					r.Logger.V(1).Info("bpfProgram not found stale reconcile, exiting", "Name", req.NamespacedName)
				} else {
					r.Logger.Error(err, "failed getting bpfProgram Object", "Name", req.NamespacedName)
				}
				return ctrl.Result{}, nil
			}

			// Get owning FentryProgram object from ownerRef
			ownerRef := metav1.GetControllerOf(bpfProgram)
			if ownerRef == nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfProgram Object owner")
			}

			if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: ownerRef.Name}, fentryProgram); err != nil {
				if errors.IsNotFound(err) {
					r.Logger.Info("Fentry Program from ownerRef not found stale reconcile exiting", "Name", req.NamespacedName)
				} else {
					r.Logger.Error(err, "failed getting FentryProgram Object from ownerRef", "Name", req.NamespacedName)
				}
				return ctrl.Result{}, nil
			}

		} else {
			r.Logger.Error(err, "failed getting FentryProgram Object", "Name", req.NamespacedName)
			return ctrl.Result{}, nil
		}
	}

	return reconcileBpfProgram(ctx, r, fentryProgram)
}

func (r *FentryProgramReconciler) updateStatus(ctx context.Context, name string, cond bpfmaniov1alpha1.ProgramConditionType, message string) (ctrl.Result, error) {
	// Sometimes we end up with a stale FentryProgram due to races, do this
	// get to ensure we're up to date before attempting a status update.
	prog := &bpfmaniov1alpha1.FentryProgram{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: name}, prog); err != nil {
		r.Logger.V(1).Info("failed to get fresh FentryProgram object...requeuing")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	return r.ReconcilerCommon.updateCondition(ctx, prog, &prog.Status.Conditions, cond, message)
}

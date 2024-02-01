/*
Copyright 2022.

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
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman/bpfman-operator/apis/v1alpha1"
	internal "github.com/bpfman/bpfman/bpfman-operator/internal"
	bpfmanHelpers "github.com/bpfman/bpfman/bpfman-operator/pkg/helpers"
	"github.com/go-logr/logr"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=bpfprograms,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

const (
	retryDurationOperator = 5 * time.Second
)

// ReconcilerCommon reconciles a BpfProgram object
type ReconcilerCommon struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

// bpfmanReconciler defines a k8s reconciler which can program bpfman.
type ProgramReconciler interface {
	getRecCommon() *ReconcilerCommon
	updateStatus(ctx context.Context,
		name string,
		cond bpfmaniov1alpha1.ProgramConditionType,
		message string) (ctrl.Result, error)
	getFinalizer() string
}

func reconcileBpfProgram(ctx context.Context, rec ProgramReconciler, prog client.Object) (ctrl.Result, error) {
	r := rec.getRecCommon()
	progName := prog.GetName()

	r.Logger.V(1).Info("Reconciling Program", "ProgramName", progName)

	if !controllerutil.ContainsFinalizer(prog, internal.BpfmanOperatorFinalizer) {
		return r.addFinalizer(ctx, prog, internal.BpfmanOperatorFinalizer)
	}

	// reconcile Program Object on all other events
	// list all existing bpfProgram state for the given Program
	bpfPrograms := &bpfmaniov1alpha1.BpfProgramList{}

	// Only list bpfPrograms for this Program
	opts := []client.ListOption{client.MatchingLabels{internal.BpfProgramOwnerLabel: progName}}

	if err := r.List(ctx, bpfPrograms, opts...); err != nil {
		r.Logger.Error(err, "failed to get freshPrograms for full reconcile")
		return ctrl.Result{}, nil
	}

	// List all nodes since a bpfprogram object will always be created for each
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes, &client.ListOptions{}); err != nil {
		r.Logger.Error(err, "failed getting nodes for full reconcile")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	// If the program isn't being deleted, make sure that each node has at
	// least one bpfprogram object.  If not, Return NotYetLoaded Status.
	if prog.GetDeletionTimestamp().IsZero() {
		for _, node := range nodes.Items {
			nodeFound := false
			for _, program := range bpfPrograms.Items {
				bpfProgramNode := program.ObjectMeta.Labels[internal.K8sHostLabel]
				if node.Name == bpfProgramNode {
					nodeFound = true
					break
				}
			}
			if !nodeFound {
				return rec.updateStatus(ctx, progName, bpfmaniov1alpha1.ProgramNotYetLoaded, "")
			}
		}
	}

	failedBpfPrograms := []string{}
	finalApplied := []string{}
	// Make sure no bpfPrograms had any issues in the loading or unloading process
	for _, bpfProgram := range bpfPrograms.Items {

		if controllerutil.ContainsFinalizer(&bpfProgram, rec.getFinalizer()) {
			finalApplied = append(finalApplied, bpfProgram.Name)
		}

		if bpfmanHelpers.IsBpfProgramConditionFailure(&bpfProgram.Status.Conditions) {
			failedBpfPrograms = append(failedBpfPrograms, bpfProgram.Name)
		}
	}

	if !prog.GetDeletionTimestamp().IsZero() {
		// Only remove bpfman-operator finalizer if all bpfProgram Objects are ready to be pruned  (i.e there are no
		// bpfPrograms with a finalizer)
		if len(finalApplied) == 0 {
			// Causes Requeue
			return r.removeFinalizer(ctx, prog, internal.BpfmanOperatorFinalizer)
		}

		// Causes Requeue
		return rec.updateStatus(ctx, progName, bpfmaniov1alpha1.ProgramDeleteError, fmt.Sprintf("Program Deletion failed on the following bpfProgram Objects: %v",
			finalApplied))
	}

	if len(failedBpfPrograms) != 0 {
		// Causes Requeue
		return rec.updateStatus(ctx, progName, bpfmaniov1alpha1.ProgramReconcileError,
			fmt.Sprintf("bpfProgramReconciliation failed on the following bpfProgram Objects: %v", failedBpfPrograms))
	}

	// Causes Requeue
	return rec.updateStatus(ctx, progName, bpfmaniov1alpha1.ProgramReconcileSuccess, "")
}

func (r *ReconcilerCommon) removeFinalizer(ctx context.Context, prog client.Object, finalizer string) (ctrl.Result, error) {
	r.Logger.V(1).Info("Program is deleted remove finalizer", "ProgramName", prog.GetName())

	if changed := controllerutil.RemoveFinalizer(prog, finalizer); changed {
		err := r.Update(ctx, prog)
		if err != nil {
			r.Logger.Error(err, "failed to remove bpfProgram Finalizer")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}
	}

	return ctrl.Result{}, nil
}

func (r *ReconcilerCommon) addFinalizer(ctx context.Context, prog client.Object, finalizer string) (ctrl.Result, error) {
	controllerutil.AddFinalizer(prog, internal.BpfmanOperatorFinalizer)

	err := r.Update(ctx, prog)
	if err != nil {
		r.Logger.V(1).Info("failed adding bpfman-operator finalizer to Program...requeuing")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	return ctrl.Result{}, nil
}

// Only reconcile if a bpfprogram object's status has been updated.
func statusChangedPredicate() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObject := e.ObjectOld.(*bpfmaniov1alpha1.BpfProgram)
			newObject := e.ObjectNew.(*bpfmaniov1alpha1.BpfProgram)
			return !reflect.DeepEqual(oldObject.Status, newObject.Status)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}
}

func (r *ReconcilerCommon) updateCondition(ctx context.Context, obj client.Object, conditions *[]metav1.Condition, cond bpfmaniov1alpha1.ProgramConditionType, message string) (ctrl.Result, error) {

	r.Logger.V(1).Info("updateCondition()", "existing conds", conditions, "new cond", cond)

	if conditions != nil {
		numConditions := len(*conditions)

		if numConditions == 1 {
			if (*conditions)[0].Type == string(cond) {
				// No change, so just return false -- not updated
				return ctrl.Result{}, nil
			} else {
				// We're changing the condition, so delete this one.  The
				// new condition will be added below.
				*conditions = nil
			}
		} else if numConditions > 1 {
			// We should only ever have one condition, so we shouldn't hit this
			// case.  However, if we do, log a message, delete the existing
			// conditions, and add the new one below.
			r.Logger.Info("more than one BpfProgramCondition", "numConditions", numConditions)
			*conditions = nil
		}
		// if numConditions == 0, just add the new condition below.
	}

	meta.SetStatusCondition(conditions, cond.Condition(message))

	if err := r.Status().Update(ctx, obj); err != nil {
		r.Logger.V(1).Info("failed to set *Program object status...requeuing")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	r.Logger.V(1).Info("condition updated", "new condition", cond)
	return ctrl.Result{}, nil
}

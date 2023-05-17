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

package bpfdagent

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	bpfdiov1alpha1 "github.com/bpfd-dev/bpfd/bpfd-operator/apis/v1alpha1"
	bpfdagentinternal "github.com/bpfd-dev/bpfd/bpfd-operator/controllers/bpfd-agent/internal"

	internal "github.com/bpfd-dev/bpfd/bpfd-operator/internal"

	gobpfd "github.com/bpfd-dev/bpfd/clients/gobpfd/v1"
	v1 "k8s.io/api/core/v1"
)

//+kubebuilder:rbac:groups=bpfd.io,resources=tracepointprograms,verbs=get;list;watch

// BpfProgramReconciler reconciles a BpfProgram object
type TracepointProgramReconciler struct {
	ReconcilerCommon
	currentTracepointProgram *bpfdiov1alpha1.TracepointProgram
	ourNode                  *v1.Node
}

func (r *TracepointProgramReconciler) getRecCommon() *ReconcilerCommon {
	return &r.ReconcilerCommon
}

func (r *TracepointProgramReconciler) getFinalizer() string {
	return internal.TracepointProgramControllerFinalizer
}

func (r *TracepointProgramReconciler) getRecType() string {
	return internal.Tracepoint.String()
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfd-Agent should reconcile whenever a TracepointProgram is updated,
// load the program to the node via bpfd, and then create a bpfProgram object
// to reflect per node state information.
func (r *TracepointProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfdiov1alpha1.TracepointProgram{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		Owns(&bpfdiov1alpha1.BpfProgram{}, builder.WithPredicates(internal.BpfProgramTypePredicate(internal.Tracepoint.String()))).
		// Only trigger reconciliation if node labels change since that could
		// make the TracepointProgram no longer select the Node. Additionally only
		// care about node events specific to our node
		Watches(
			&source.Kind{Type: &v1.Node{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(predicate.LabelChangedPredicate{}, nodePredicate(r.NodeName))),
		).
		Complete(r)
}

func (r *TracepointProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentTracepointProgram = &bpfdiov1alpha1.TracepointProgram{}
	r.ourNode = &v1.Node{}

	r.Logger = log.FromContext(ctx)

	// Lookup K8s node object for this bpfd-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfd-agent node %s : %v",
			req.NamespacedName, err)
	}

	tracepointPrograms := &bpfdiov1alpha1.TracepointProgramList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, tracepointPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting TcPrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(tracepointPrograms.Items) == 0 {
		return ctrl.Result{Requeue: false}, nil
	}

	// Get existing ebpf state from bpfd.
	programMap, err := bpfdagentinternal.ListBpfdPrograms(ctx, r.BpfdClient, internal.Tc)
	if err != nil {
		r.Logger.Error(err, "failed to list loaded bpfd programs")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
	}

	// Reconcile every TcProgram Object
	// note: This doesn't necessarily result in any extra grpc calls to bpfd
	for _, tcProgram := range tracepointPrograms.Items {
		r.Logger.Info("TracepointProgramController is reconciling", "key", req)
		r.currentTracepointProgram = &tcProgram
		retry, err := reconcileProgram(ctx, r, r.currentTracepointProgram, &r.currentTracepointProgram.Spec.BpfProgramCommon, r.ourNode, programMap)
		if err != nil {
			r.Logger.Error(err, "Reconciling TracepointProgram Failed", "TracepointProgramName", r.currentTracepointProgram.Name, "Retrying", retry)
			return ctrl.Result{Requeue: retry, RequeueAfter: retryDurationAgent}, nil
		}
	}

	return ctrl.Result{Requeue: false}, nil
}

// reconcileBpfdPrograms ONLY reconciles the bpfd state for a single BpfProgram.
// It does interact with the k8s API in any way.
func (r *TracepointProgramReconciler) reconcileBpfdPrograms(ctx context.Context,
	existingBpfPrograms map[string]*gobpfd.ListResponse_ListResult,
	bytecode interface{},
	isNodeSelected bool,
	isBeingDeleted bool) (bpfProgramConditionType, error) {

	r.expectedPrograms = map[string]map[string]string{}

	r.Logger.V(1).Info("Existing bpfProgramEntries", "ExistingEntries", r.bpfProgram.Spec.Programs)
	// DeepCopy the existing programs
	for k, v := range r.bpfProgram.Spec.Programs {
		r.expectedPrograms[k] = v
	}

	loadRequest := &gobpfd.LoadRequest{}

	id := bpfdagentinternal.GenIdFromName(r.currentTracepointProgram.Name)

	loadRequest.Common = bpfdagentinternal.BuildBpfdCommon(bytecode, r.currentTracepointProgram.Spec.SectionName, internal.Tracepoint, id, r.currentTracepointProgram.Spec.GlobalData)

	loadRequest.AttachInfo = &gobpfd.LoadRequest_TracepointAttachInfo{
		TracepointAttachInfo: &gobpfd.TracepointAttachInfo{
			Tracepoint: r.currentTracepointProgram.Spec.Name,
		},
	}

	existingProgram, doesProgramExist := existingBpfPrograms[id]
	if !doesProgramExist {
		r.Logger.V(1).Info("TracepointProgram doesn't exist on node")

		// If TracepointProgram is being deleted just exit
		if isBeingDeleted {
			return BpfProgCondNotLoaded, nil
		}

		// Make sure if we're not selected just exit
		if !isNodeSelected {
			return BpfProgCondNotSelected, nil
		}

		// otherwise load it
		bpfProgramEntry, err := bpfdagentinternal.LoadBpfdProgram(ctx, r.BpfdClient, loadRequest)
		if err != nil {
			r.Logger.Error(err, "Failed to load TcProgram")
			return BpfProgCondNotLoaded, err
		}

		r.expectedPrograms[id] = bpfProgramEntry

		return BpfProgCondLoaded, nil
	}

	// BpfProgram exists but either TracepointProgram is being deleted or node is no
	// longer selected....unload program
	if isBeingDeleted || !isNodeSelected {
		r.Logger.V(1).Info("TcProgram exists on Node but is scheduled for deletion or node is no longer selected", "isDeleted", isBeingDeleted,
			"isSelected", isNodeSelected)
		if err := bpfdagentinternal.UnloadBpfdProgram(ctx, r.BpfdClient, id); err != nil {
			r.Logger.Error(err, "Failed to unload TcProgram")
			return BpfProgCondLoaded, err
		}
		delete(r.expectedPrograms, id)

		// continue to next program
		return BpfProgCondNotSelected, nil
	}

	r.Logger.V(1).WithValues("expectedProgram", loadRequest).WithValues("existingProgram", existingProgram).Info("StateMatch")
	// BpfProgram exists but is not correct state, unload and recreate
	if !bpfdagentinternal.DoesProgExist(existingProgram, loadRequest) {
		if err := bpfdagentinternal.UnloadBpfdProgram(ctx, r.BpfdClient, id); err != nil {
			r.Logger.Error(err, "Failed to unload TcProgram")
			return BpfProgCondNotUnloaded, err
		}

		bpfProgramEntry, err := bpfdagentinternal.LoadBpfdProgram(ctx, r.BpfdClient, loadRequest)
		if err != nil {
			r.Logger.Error(err, "Failed to load TcProgram")
			return BpfProgCondNotLoaded, err
		}

		r.expectedPrograms[id] = bpfProgramEntry
	} else {
		// Program already exists, but bpfProgram K8s Object might not be up to date
		if _, ok := r.bpfProgram.Spec.Programs[id]; !ok {
			maps, err := bpfdagentinternal.GetMapsForUUID(id)
			if err != nil {
				r.Logger.Error(err, "failed to get bpfProgram's Maps")
				return BpfProgCondNotLoaded, err
			}

			r.expectedPrograms[id] = maps
		} else {
			// Program exists and bpfProgram K8s Object is up to date
			r.Logger.V(1).Info("Ignoring Object Change nothing to do in bpfd")
		}
	}

	return BpfProgCondLoaded, nil
}

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

package bpfmanagent

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanagentinternal "github.com/bpfman/bpfman/bpfman-operator/controllers/bpfman-agent/internal"

	internal "github.com/bpfman/bpfman/bpfman-operator/internal"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	v1 "k8s.io/api/core/v1"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=tracepointprograms,verbs=get;list;watch

// BpfProgramReconciler reconciles a BpfProgram object
type TracepointProgramReconciler struct {
	ReconcilerCommon
	currentTracepointProgram *bpfmaniov1alpha1.TracepointProgram
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
// The Bpfman-Agent should reconcile whenever a TracepointProgram is updated,
// load the program to the node via bpfman, and then create a bpfProgram object
// to reflect per node state information.
func (r *TracepointProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.TracepointProgram{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		Owns(&bpfmaniov1alpha1.BpfProgram{},
			builder.WithPredicates(predicate.And(
				internal.BpfProgramTypePredicate(internal.Tracepoint.String()),
				internal.BpfProgramNodePredicate(r.NodeName)),
			),
		).
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

func (r *TracepointProgramReconciler) expectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfProgramList, error) {
	progs := &bpfmaniov1alpha1.BpfProgramList{}

	for _, tracepoint := range r.currentTracepointProgram.Spec.Names {
		// sanitize tracepoint name to work in a bpfProgram name
		sanatizedTrace := strings.Replace(strings.Replace(tracepoint, "/", "-", -1), "_", "-", -1)
		bpfProgramName := fmt.Sprintf("%s-%s-%s", r.currentTracepointProgram.Name, r.NodeName, sanatizedTrace)

		annotations := map[string]string{internal.TracepointProgramTracepoint: tracepoint}

		prog, err := r.createBpfProgram(ctx, bpfProgramName, r.getFinalizer(), r.currentTracepointProgram, r.getRecType(), annotations)
		if err != nil {
			return nil, fmt.Errorf("failed to create BpfProgram %s: %v", bpfProgramName, err)
		}

		progs.Items = append(progs.Items, *prog)
	}

	return progs, nil
}

func (r *TracepointProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentTracepointProgram = &bpfmaniov1alpha1.TracepointProgram{}
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("tracept")

	ctxLogger := log.FromContext(ctx)
	ctxLogger.Info("Reconcile Tracepoint: Enter", "ReconcileKey", req)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	tracepointPrograms := &bpfmaniov1alpha1.TracepointProgramList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, tracepointPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting TracepointPrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(tracepointPrograms.Items) == 0 {
		r.Logger.Info("TracepointProgramController found no Tracepoint Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	// Get existing ebpf state from bpfman.
	programMap, err := bpfmanagentinternal.ListBpfmanPrograms(ctx, r.BpfmanClient, internal.Tracepoint)
	if err != nil {
		r.Logger.Error(err, "failed to list loaded bpfman programs")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
	}

	// Reconcile each TracepointProgram. Don't return error here because it will trigger an infinite reconcile loop, instead
	// report the error to user and retry if specified. For some errors the controller may not decide to retry.
	// Note: This only results in grpc calls to bpfman if we need to change something
	requeue := false // initialize requeue to false
	for _, tracepointProgram := range tracepointPrograms.Items {
		r.Logger.Info("TracepointProgramController is reconciling", "currentTracePtProgram", tracepointProgram.Name)
		r.currentTracepointProgram = &tracepointProgram
		result, err := reconcileProgram(ctx, r, r.currentTracepointProgram, &r.currentTracepointProgram.Spec.BpfProgramCommon, r.ourNode, programMap)
		if err != nil {
			r.Logger.Error(err, "Reconciling TracepointProgram Failed", "TracepointProgramName", r.currentTracepointProgram.Name, "ReconcileResult", result.String())
		}

		switch result {
		case internal.Unchanged:
			// continue with next program
		case internal.Updated:
			// return
			return ctrl.Result{Requeue: false}, nil
		case internal.Requeue:
			// remember to do a requeue when we're done and continue with next program
			requeue = true
		}
	}

	if requeue {
		// A requeue has been requested
		return ctrl.Result{RequeueAfter: retryDurationAgent}, nil
	} else {
		// We've made it through all the programs in the list without anything being
		// updated and a reque has not been requested.
		return ctrl.Result{Requeue: false}, nil
	}
}

func (r *TracepointProgramReconciler) buildTracepointLoadRequest(
	bytecode *gobpfman.BytecodeLocation,
	uuid string,
	bpfProgram *bpfmaniov1alpha1.BpfProgram,
	mapOwnerId *uint32) *gobpfman.LoadRequest {

	return &gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentTracepointProgram.Spec.BpfFunctionName,
		ProgramType: uint32(internal.Tracepoint),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TracepointAttachInfo{
				TracepointAttachInfo: &gobpfman.TracepointAttachInfo{
					Tracepoint: bpfProgram.Annotations[internal.TracepointProgramTracepoint],
				},
			},
		},
		Metadata:   map[string]string{internal.UuidMetadataKey: uuid, internal.ProgramNameKey: r.currentTracepointProgram.Name},
		GlobalData: r.currentTracepointProgram.Spec.GlobalData,
		MapOwnerId: mapOwnerId,
	}
}

// reconcileBpfmanPrograms ONLY reconciles the bpfman state for a single BpfProgram.
// It does not interact with the k8s API in any way.
func (r *TracepointProgramReconciler) reconcileBpfmanProgram(ctx context.Context,
	existingBpfPrograms map[string]*gobpfman.ListResponse_ListResult,
	bytecodeSelector *bpfmaniov1alpha1.BytecodeSelector,
	bpfProgram *bpfmaniov1alpha1.BpfProgram,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus) (bpfmaniov1alpha1.BpfProgramConditionType, error) {

	r.Logger.V(1).Info("Existing bpfProgram", "UUID", bpfProgram.UID, "Name", bpfProgram.Name, "CurrentTracepointProgram", r.currentTracepointProgram.Name)
	uuid := string(bpfProgram.UID)

	getLoadRequest := func() (*gobpfman.LoadRequest, bpfmaniov1alpha1.BpfProgramConditionType, error) {
		bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, bytecodeSelector)
		if err != nil {
			return nil, bpfmaniov1alpha1.BpfProgCondBytecodeSelectorError, fmt.Errorf("failed to process bytecode selector: %v", err)
		}
		loadRequest := r.buildTracepointLoadRequest(bytecode, uuid, bpfProgram, mapOwnerStatus.mapOwnerId)
		return loadRequest, bpfmaniov1alpha1.BpfProgCondNone, nil
	}

	existingProgram, doesProgramExist := existingBpfPrograms[uuid]
	if !doesProgramExist {
		r.Logger.V(1).Info("TracepointProgram doesn't exist on node")

		// If TracepointProgram is being deleted just exit
		if isBeingDeleted {
			return bpfmaniov1alpha1.BpfProgCondUnloaded, nil
		}

		// Make sure if we're not selected just exit
		if !isNodeSelected {
			return bpfmaniov1alpha1.BpfProgCondNotSelected, nil
		}

		// Make sure if the Map Owner is set but not found then just exit
		if mapOwnerStatus.isSet && !mapOwnerStatus.isFound {
			return bpfmaniov1alpha1.BpfProgCondMapOwnerNotFound, nil
		}

		// Make sure if the Map Owner is set but not loaded then just exit
		if mapOwnerStatus.isSet && !mapOwnerStatus.isLoaded {
			return bpfmaniov1alpha1.BpfProgCondMapOwnerNotLoaded, nil
		}

		// otherwise load it
		loadRequest, condition, err := getLoadRequest()
		if err != nil {
			return condition, err
		}

		r.progId, err = bpfmanagentinternal.LoadBpfmanProgram(ctx, r.BpfmanClient, loadRequest)
		if err != nil {
			r.Logger.Error(err, "Failed to load TracepointProgram")
			return bpfmaniov1alpha1.BpfProgCondNotLoaded, nil
		}

		r.Logger.Info("bpfman called to load TracepointProgram on Node", "Name", bpfProgram.Name, "ID", uuid)
		return bpfmaniov1alpha1.BpfProgCondLoaded, nil
	}

	// prog ID should already have been set if program doesn't exist
	id, err := bpfmanagentinternal.GetID(bpfProgram)
	if err != nil {
		r.Logger.Error(err, "Failed to get program ID")
		return bpfmaniov1alpha1.BpfProgCondNotLoaded, nil
	}

	// BpfProgram exists but either TracepointProgram is being deleted, node is no
	// longer selected, or map is not available....unload program
	if isBeingDeleted || !isNodeSelected ||
		(mapOwnerStatus.isSet && (!mapOwnerStatus.isFound || !mapOwnerStatus.isLoaded)) {
		r.Logger.V(1).Info("TracepointProgram exists on Node but is scheduled for deletion, not selected, or map not available",
			"isDeleted", isBeingDeleted, "isSelected", isNodeSelected, "mapIsSet", mapOwnerStatus.isSet,
			"mapIsFound", mapOwnerStatus.isFound, "mapIsLoaded", mapOwnerStatus.isLoaded)

		if err := bpfmanagentinternal.UnloadBpfmanProgram(ctx, r.BpfmanClient, *id); err != nil {
			r.Logger.Error(err, "Failed to unload TracepointProgram")
			return bpfmaniov1alpha1.BpfProgCondNotUnloaded, nil
		}

		r.Logger.Info("bpfman called to unload TracepointProgram on Node", "Name", bpfProgram.Name, "UUID", id)

		if isBeingDeleted {
			return bpfmaniov1alpha1.BpfProgCondUnloaded, nil
		}

		if !isNodeSelected {
			return bpfmaniov1alpha1.BpfProgCondNotSelected, nil
		}

		if mapOwnerStatus.isSet && !mapOwnerStatus.isFound {
			return bpfmaniov1alpha1.BpfProgCondMapOwnerNotFound, nil
		}

		if mapOwnerStatus.isSet && !mapOwnerStatus.isLoaded {
			return bpfmaniov1alpha1.BpfProgCondMapOwnerNotLoaded, nil
		}
	}

	// BpfProgram exists but is not correct state, unload and recreate
	loadRequest, condition, err := getLoadRequest()
	if err != nil {
		return condition, err
	}

	r.Logger.V(1).WithValues("expectedProgram", loadRequest).WithValues("existingProgram", existingProgram).Info("StateMatch")

	isSame, reasons := bpfmanagentinternal.DoesProgExist(existingProgram, loadRequest)
	if !isSame {
		r.Logger.V(1).Info("TracepointProgram is in wrong state, unloading and reloading", "Reason", reasons)

		if err := bpfmanagentinternal.UnloadBpfmanProgram(ctx, r.BpfmanClient, *id); err != nil {
			r.Logger.Error(err, "Failed to unload TracepointProgram")
			return bpfmaniov1alpha1.BpfProgCondNotUnloaded, nil
		}

		r.progId, err = bpfmanagentinternal.LoadBpfmanProgram(ctx, r.BpfmanClient, loadRequest)
		if err != nil {
			r.Logger.Error(err, "Failed to load TracepointProgram")
			return bpfmaniov1alpha1.BpfProgCondNotLoaded, err
		}

		r.Logger.Info("bpfman called to reload TracepointProgram on Node", "Name", bpfProgram.Name, "UUID", id)
	} else {
		// Program exists and bpfProgram K8s Object is up to date
		r.Logger.V(1).Info("Ignoring Object Change nothing to do in bpfman")
		r.progId = id
	}

	return bpfmaniov1alpha1.BpfProgCondLoaded, nil
}

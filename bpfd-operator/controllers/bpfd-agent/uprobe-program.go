/*
Copyright 2023.

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
	"strings"

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

//+kubebuilder:rbac:groups=bpfd.dev,resources=uprobeprograms,verbs=get;list;watch

// BpfProgramReconciler reconciles a BpfProgram object
type UprobeProgramReconciler struct {
	ReconcilerCommon
	currentUprobeProgram *bpfdiov1alpha1.UprobeProgram
	ourNode              *v1.Node
}

func (r *UprobeProgramReconciler) getRecCommon() *ReconcilerCommon {
	return &r.ReconcilerCommon
}

func (r *UprobeProgramReconciler) getFinalizer() string {
	return internal.UprobeProgramControllerFinalizer
}

func (r *UprobeProgramReconciler) getRecType() string {
	return internal.UprobeString
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfd-Agent should reconcile whenever a UprobeProgram is updated,
// load the program to the node via bpfd, and then create a bpfProgram object
// to reflect per node state information.
func (r *UprobeProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfdiov1alpha1.UprobeProgram{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		Owns(&bpfdiov1alpha1.BpfProgram{},
			builder.WithPredicates(predicate.And(
				internal.BpfProgramTypePredicate(internal.UprobeString),
				internal.BpfProgramNodePredicate(r.NodeName)),
			),
		).
		// Only trigger reconciliation if node labels change since that could
		// make the UprobeProgram no longer select the Node. Additionally only
		// care about node events specific to our node
		Watches(
			&source.Kind{Type: &v1.Node{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(predicate.LabelChangedPredicate{}, nodePredicate(r.NodeName))),
		).
		Complete(r)
}

func (r *UprobeProgramReconciler) expectedBpfPrograms(ctx context.Context) (*bpfdiov1alpha1.BpfProgramList, error) {
	progs := &bpfdiov1alpha1.BpfProgramList{}

	for _, target := range r.currentUprobeProgram.Spec.Targets {
		// sanitize uprobe name to work in a bpfProgram name
		sanatizedUprobe := strings.Replace(strings.Replace(target, "/", "-", -1), "_", "-", -1)
		bpfProgramName := fmt.Sprintf("%s-%s-%s", r.currentUprobeProgram.Name, r.NodeName, sanatizedUprobe)

		annotations := map[string]string{internal.UprobeProgramTarget: target}

		prog, err := r.createBpfProgram(ctx, bpfProgramName, r.getFinalizer(), r.currentUprobeProgram, r.getRecType(), annotations)
		if err != nil {
			return nil, fmt.Errorf("failed to create BpfProgram %s: %v", bpfProgramName, err)
		}

		progs.Items = append(progs.Items, *prog)
	}

	return progs, nil
}

func (r *UprobeProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentUprobeProgram = &bpfdiov1alpha1.UprobeProgram{}
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("uprobe")

	ctxLogger := log.FromContext(ctx)
	ctxLogger.Info("Reconcile Uprobe: Enter", "ReconcileKey", req)

	// Lookup K8s node object for this bpfd-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfd-agent node %s : %v",
			req.NamespacedName, err)
	}

	uprobePrograms := &bpfdiov1alpha1.UprobeProgramList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, uprobePrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting UprobePrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(uprobePrograms.Items) == 0 {
		r.Logger.Info("UprobeProgramController found no Uprobe Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	// Get existing ebpf state from bpfd. Since both uprobes and kprobes have
	// the same kernel ProgramType, we use internal.Kprobe below.
	programMap, err := bpfdagentinternal.ListBpfdPrograms(ctx, r.BpfdClient, internal.Kprobe)
	if err != nil {
		r.Logger.Error(err, "failed to list loaded bpfd programs")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
	}

	// Reconcile each UprobeProgram. Don't return error here because it will trigger an infinite reconcile loop, instead
	// report the error to user and retry if specified. For some errors the controller may not decide to retry.
	// Note: This only results in grpc calls to bpfd if we need to change something
	requeue := false // initialize requeue to false
	for _, uprobeProgram := range uprobePrograms.Items {
		r.Logger.Info("UprobeProgramController is reconciling", "currentUprobeProgram", uprobeProgram.Name)
		r.currentUprobeProgram = &uprobeProgram
		result, err := reconcileProgram(ctx, r, r.currentUprobeProgram, &r.currentUprobeProgram.Spec.BpfProgramCommon, r.ourNode, programMap)
		if err != nil {
			r.Logger.Error(err, "Reconciling UprobeProgram Failed", "UprobeProgramName", r.currentUprobeProgram.Name, "ReconcileResult", result.String())
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

func (r *UprobeProgramReconciler) buildUprobeLoadRequest(
	bytecode *gobpfd.BytecodeLocation,
	uuid string,
	bpfProgram *bpfdiov1alpha1.BpfProgram,
	mapOwnerId *uint32) *gobpfd.LoadRequest {

	// Namespace isn't supported yet in bpfd, so set it to an empty string.
	namespace := ""

	return &gobpfd.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentUprobeProgram.Spec.SectionName,
		ProgramType: uint32(internal.Kprobe),
		Attach: &gobpfd.AttachInfo{
			Info: &gobpfd.AttachInfo_UprobeAttachInfo{
				UprobeAttachInfo: &gobpfd.UprobeAttachInfo{
					FnName:    &r.currentUprobeProgram.Spec.FunctionName,
					Offset:    r.currentUprobeProgram.Spec.Offset,
					Target:    bpfProgram.Annotations[internal.UprobeProgramTarget],
					Retprobe:  r.currentUprobeProgram.Spec.RetProbe,
					Namespace: &namespace,
				},
			},
		},
		Metadata:   map[string]string{internal.UuidMetadataKey: uuid, internal.ProgramNameKey: r.currentUprobeProgram.Name},
		GlobalData: r.currentUprobeProgram.Spec.GlobalData,
		MapOwnerId: mapOwnerId,
	}
}

// reconcileBpfdPrograms ONLY reconciles the bpfd state for a single BpfProgram.
// It does not interact with the k8s API in any way.
func (r *UprobeProgramReconciler) reconcileBpfdProgram(ctx context.Context,
	existingBpfPrograms map[string]*gobpfd.ListResponse_ListResult,
	bytecodeSelector *bpfdiov1alpha1.BytecodeSelector,
	bpfProgram *bpfdiov1alpha1.BpfProgram,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus) (bpfdiov1alpha1.BpfProgramConditionType, error) {

	r.Logger.V(1).Info("Existing bpfProgram", "ExistingMaps", bpfProgram.Spec.Maps, "UUID", bpfProgram.UID, "Name", bpfProgram.Name, "CurrentUprobeProgram", r.currentUprobeProgram.Name)

	uuid := bpfProgram.UID

	getLoadRequest := func() (*gobpfd.LoadRequest, bpfdiov1alpha1.BpfProgramConditionType, error) {
		bytecode, err := bpfdagentinternal.GetBytecode(r.Client, bytecodeSelector)
		if err != nil {
			return nil, bpfdiov1alpha1.BpfProgCondBytecodeSelectorError, fmt.Errorf("failed to process bytecode selector: %v", err)
		}
		loadRequest := r.buildUprobeLoadRequest(bytecode, string(uuid), bpfProgram, mapOwnerStatus.mapOwnerId)
		return loadRequest, bpfdiov1alpha1.BpfProgCondNone, nil
	}

	existingProgram, doesProgramExist := existingBpfPrograms[string(uuid)]
	if !doesProgramExist {
		r.Logger.V(1).Info("UprobeProgram doesn't exist on node")

		// If UprobeProgram is being deleted just exit
		if isBeingDeleted {
			return bpfdiov1alpha1.BpfProgCondUnloaded, nil
		}

		// Make sure if we're not selected just exit
		if !isNodeSelected {
			return bpfdiov1alpha1.BpfProgCondNotSelected, nil
		}

		// Make sure if the Map Owner is set but not found then just exit
		if mapOwnerStatus.isSet && !mapOwnerStatus.isFound {
			return bpfdiov1alpha1.BpfProgCondMapOwnerNotFound, nil
		}

		// Make sure if the Map Owner is set but not loaded then just exit
		if mapOwnerStatus.isSet && !mapOwnerStatus.isLoaded {
			return bpfdiov1alpha1.BpfProgCondMapOwnerNotLoaded, nil
		}

		// otherwise load it
		loadRequest, condition, err := getLoadRequest()
		if err != nil {
			return condition, err
		}

		r.progId, err = bpfdagentinternal.LoadBpfdProgram(ctx, r.BpfdClient, loadRequest)
		if err != nil {
			r.Logger.Error(err, "Failed to load UprobeProgram")
			return bpfdiov1alpha1.BpfProgCondNotLoaded, nil
		}

		r.Logger.Info("bpfd called to load UprobeProgram on Node", "Name", bpfProgram.Name, "UUID", uuid)
		return bpfdiov1alpha1.BpfProgCondLoaded, nil
	}

	// prog ID should already have been set if program exists
	id, err := bpfdagentinternal.GetID(bpfProgram)
	if err != nil {
		r.Logger.Error(err, "Failed to get program ID")
		return bpfdiov1alpha1.BpfProgCondNotLoaded, nil
	}

	// BpfProgram exists but either UprobeProgram is being deleted, node is no
	// longer selected, or map is not available....unload program
	if isBeingDeleted || !isNodeSelected ||
		(mapOwnerStatus.isSet && (!mapOwnerStatus.isFound || !mapOwnerStatus.isLoaded)) {
		r.Logger.V(1).Info("UprobeProgram exists on Node but is scheduled for deletion, not selected, or map not available",
			"isDeleted", isBeingDeleted, "isSelected", isNodeSelected, "mapIsSet", mapOwnerStatus.isSet,
			"mapIsFound", mapOwnerStatus.isFound, "mapIsLoaded", mapOwnerStatus.isLoaded)

		if err := bpfdagentinternal.UnloadBpfdProgram(ctx, r.BpfdClient, *id); err != nil {
			r.Logger.Error(err, "Failed to unload UprobeProgram")
			return bpfdiov1alpha1.BpfProgCondNotUnloaded, nil
		}

		r.Logger.Info("bpfd called to unload UprobeProgram on Node", "Name", bpfProgram.Name, "UUID", uuid)

		if isBeingDeleted {
			return bpfdiov1alpha1.BpfProgCondUnloaded, nil
		}

		if !isNodeSelected {
			return bpfdiov1alpha1.BpfProgCondNotSelected, nil
		}

		if mapOwnerStatus.isSet && !mapOwnerStatus.isFound {
			return bpfdiov1alpha1.BpfProgCondMapOwnerNotFound, nil
		}

		if mapOwnerStatus.isSet && !mapOwnerStatus.isLoaded {
			return bpfdiov1alpha1.BpfProgCondMapOwnerNotLoaded, nil
		}
	}

	// BpfProgram exists but is not correct state, unload and recreate
	loadRequest, condition, err := getLoadRequest()
	if err != nil {
		return condition, err
	}

	r.Logger.V(1).WithValues("expectedProgram", loadRequest).WithValues("existingProgram", existingProgram).Info("StateMatch")

	isSame, reasons := bpfdagentinternal.DoesProgExist(existingProgram, loadRequest)
	if !isSame {
		r.Logger.V(1).Info("UprobeProgram is in wrong state, unloading and reloading", "Reason", reasons)
		if err := bpfdagentinternal.UnloadBpfdProgram(ctx, r.BpfdClient, *id); err != nil {
			r.Logger.Error(err, "Failed to unload UprobeProgram")
			return bpfdiov1alpha1.BpfProgCondNotUnloaded, nil
		}

		r.progId, err = bpfdagentinternal.LoadBpfdProgram(ctx, r.BpfdClient, loadRequest)
		if err != nil {
			r.Logger.Error(err, "Failed to load UprobeProgram")
			return bpfdiov1alpha1.BpfProgCondNotLoaded, err
		}

		r.Logger.Info("bpfd called to reload UprobeProgram on Node", "Name", bpfProgram.Name, "UUID", uuid)
	} else {
		// Program exists and bpfProgram K8s Object is up to date
		r.Logger.V(1).Info("Ignoring Object Change nothing to do in bpfd")
	}

	return bpfdiov1alpha1.BpfProgCondLoaded, nil
}

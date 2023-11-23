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

package bpfmanagent

import (
	"context"
	"fmt"
	"strings"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanagentinternal "github.com/bpfman/bpfman/bpfman-operator/controllers/bpfman-agent/internal"
	"github.com/bpfman/bpfman/bpfman-operator/internal"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=kprobeprograms,verbs=get;list;watch

// BpfProgramReconciler reconciles a BpfProgram object
type KprobeProgramReconciler struct {
	ReconcilerCommon
	currentKprobeProgram *bpfmaniov1alpha1.KprobeProgram
	ourNode              *v1.Node
}

func (r *KprobeProgramReconciler) getRecCommon() *ReconcilerCommon {
	return &r.ReconcilerCommon
}

func (r *KprobeProgramReconciler) getFinalizer() string {
	return internal.KprobeProgramControllerFinalizer
}

func (r *KprobeProgramReconciler) getRecType() string {
	return internal.Kprobe.String()
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfman-Agent should reconcile whenever a KprobeProgram is updated,
// load the program to the node via bpfman, and then create a bpfProgram object
// to reflect per node state information.
func (r *KprobeProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.KprobeProgram{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		Owns(&bpfmaniov1alpha1.BpfProgram{},
			builder.WithPredicates(predicate.And(
				internal.BpfProgramTypePredicate(internal.Kprobe.String()),
				internal.BpfProgramNodePredicate(r.NodeName)),
			),
		).
		// Only trigger reconciliation if node labels change since that could
		// make the KprobeProgram no longer select the Node. Additionally only
		// care about node events specific to our node
		Watches(
			&source.Kind{Type: &v1.Node{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(predicate.LabelChangedPredicate{}, nodePredicate(r.NodeName))),
		).
		Complete(r)
}

func (r *KprobeProgramReconciler) expectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfProgramList, error) {
	progs := &bpfmaniov1alpha1.BpfProgramList{}

	for _, function := range r.currentKprobeProgram.Spec.FunctionNames {
		// sanitize kprobe name to work in a bpfProgram name
		sanatizedKprobe := strings.Replace(strings.Replace(function, "/", "-", -1), "_", "-", -1)
		bpfProgramName := fmt.Sprintf("%s-%s-%s", r.currentKprobeProgram.Name, r.NodeName, sanatizedKprobe)

		annotations := map[string]string{internal.KprobeProgramFunction: function}

		prog, err := r.createBpfProgram(ctx, bpfProgramName, r.getFinalizer(), r.currentKprobeProgram, r.getRecType(), annotations)
		if err != nil {
			return nil, fmt.Errorf("failed to create BpfProgram %s: %v", bpfProgramName, err)
		}

		progs.Items = append(progs.Items, *prog)
	}

	return progs, nil
}

func (r *KprobeProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentKprobeProgram = &bpfmaniov1alpha1.KprobeProgram{}
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("kprobe")

	ctxLogger := log.FromContext(ctx)
	ctxLogger.Info("Reconcile Kprobe: Enter", "ReconcileKey", req)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	kprobePrograms := &bpfmaniov1alpha1.KprobeProgramList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, kprobePrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting KprobePrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(kprobePrograms.Items) == 0 {
		r.Logger.Info("KprobeProgramController found no Kprobe Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	// Get existing ebpf state from bpfman.
	programMap, err := bpfmanagentinternal.ListBpfmanPrograms(ctx, r.BpfmanClient, internal.Kprobe)
	if err != nil {
		r.Logger.Error(err, "failed to list loaded bpfman programs")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
	}

	// Reconcile each KprobeProgram. Don't return error here because it will trigger an infinite reconcile loop, instead
	// report the error to user and retry if specified. For some errors the controller may not decide to retry.
	// Note: This only results in grpc calls to bpfman if we need to change something
	requeue := false // initialize requeue to false
	for _, kprobeProgram := range kprobePrograms.Items {
		r.Logger.Info("KprobeProgramController is reconciling", "currentKprobeProgram", kprobeProgram.Name)
		r.currentKprobeProgram = &kprobeProgram
		result, err := reconcileProgram(ctx, r, r.currentKprobeProgram, &r.currentKprobeProgram.Spec.BpfProgramCommon, r.ourNode, programMap)
		if err != nil {
			r.Logger.Error(err, "Reconciling KprobeProgram Failed", "KprobeProgramName", r.currentKprobeProgram.Name, "ReconcileResult", result.String())
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

func (r *KprobeProgramReconciler) buildKprobeLoadRequest(
	bytecode *gobpfman.BytecodeLocation,
	uuid string,
	bpfProgram *bpfmaniov1alpha1.BpfProgram,
	mapOwnerId *uint32) *gobpfman.LoadRequest {

	// Namespace isn't supported yet in bpfman, so set it to an empty string.
	namespace := ""

	return &gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentKprobeProgram.Spec.BpfFunctionName,
		ProgramType: uint32(internal.Kprobe),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_KprobeAttachInfo{
				KprobeAttachInfo: &gobpfman.KprobeAttachInfo{
					FnName:    bpfProgram.Annotations[internal.KprobeProgramFunction],
					Offset:    r.currentKprobeProgram.Spec.Offset,
					Retprobe:  r.currentKprobeProgram.Spec.RetProbe,
					Namespace: &namespace,
				},
			},
		},
		Metadata:   map[string]string{internal.UuidMetadataKey: uuid, internal.ProgramNameKey: r.currentKprobeProgram.Name},
		GlobalData: r.currentKprobeProgram.Spec.GlobalData,
		MapOwnerId: mapOwnerId,
	}
}

// reconcileBpfmanPrograms ONLY reconciles the bpfman state for a single BpfProgram.
// It does not interact with the k8s API in any way.
func (r *KprobeProgramReconciler) reconcileBpfmanProgram(ctx context.Context,
	existingBpfPrograms map[string]*gobpfman.ListResponse_ListResult,
	bytecodeSelector *bpfmaniov1alpha1.BytecodeSelector,
	bpfProgram *bpfmaniov1alpha1.BpfProgram,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus) (bpfmaniov1alpha1.BpfProgramConditionType, error) {

	r.Logger.V(1).Info("Existing bpfProgram", "UUID", bpfProgram.UID, "Name", bpfProgram.Name, "CurrentKprobeProgram", r.currentKprobeProgram.Name)

	uuid := bpfProgram.UID

	getLoadRequest := func() (*gobpfman.LoadRequest, bpfmaniov1alpha1.BpfProgramConditionType, error) {
		bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, bytecodeSelector)
		if err != nil {
			return nil, bpfmaniov1alpha1.BpfProgCondBytecodeSelectorError, fmt.Errorf("failed to process bytecode selector: %v", err)
		}
		loadRequest := r.buildKprobeLoadRequest(bytecode, string(uuid), bpfProgram, mapOwnerStatus.mapOwnerId)
		return loadRequest, bpfmaniov1alpha1.BpfProgCondNone, nil
	}

	existingProgram, doesProgramExist := existingBpfPrograms[string(uuid)]
	if !doesProgramExist {
		r.Logger.V(1).Info("KprobeProgram doesn't exist on node")

		// If KprobeProgram is being deleted just exit
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
			r.Logger.Error(err, "Failed to load KprobeProgram")
			return bpfmaniov1alpha1.BpfProgCondNotLoaded, nil
		}

		r.Logger.Info("bpfman called to load KprobeProgram on Node", "Name", bpfProgram.Name, "UUID", uuid)
		return bpfmaniov1alpha1.BpfProgCondLoaded, nil
	}

	// prog ID should already have been set if program exists
	id, err := bpfmanagentinternal.GetID(bpfProgram)
	if err != nil {
		r.Logger.Error(err, "Failed to get program ID")
		return bpfmaniov1alpha1.BpfProgCondNotLoaded, nil
	}

	// BpfProgram exists but either KprobeProgram is being deleted, node is no
	// longer selected, or map is not available....unload program
	if isBeingDeleted || !isNodeSelected ||
		(mapOwnerStatus.isSet && (!mapOwnerStatus.isFound || !mapOwnerStatus.isLoaded)) {
		r.Logger.V(1).Info("KprobeProgram exists on Node but is scheduled for deletion, not selected, or map not available",
			"isDeleted", isBeingDeleted, "isSelected", isNodeSelected, "mapIsSet", mapOwnerStatus.isSet,
			"mapIsFound", mapOwnerStatus.isFound, "mapIsLoaded", mapOwnerStatus.isLoaded, "id", id)

		if err := bpfmanagentinternal.UnloadBpfmanProgram(ctx, r.BpfmanClient, *id); err != nil {
			r.Logger.Error(err, "Failed to unload KprobeProgram")
			return bpfmaniov1alpha1.BpfProgCondNotUnloaded, nil
		}

		r.Logger.Info("bpfman called to unload KprobeProgram on Node", "Name", bpfProgram.Name, "UUID", uuid)

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
		r.Logger.V(1).Info("KprobeProgram is in wrong state, unloading and reloading", "Reason", reasons)
		if err := bpfmanagentinternal.UnloadBpfmanProgram(ctx, r.BpfmanClient, *id); err != nil {
			r.Logger.Error(err, "Failed to unload KprobeProgram")
			return bpfmaniov1alpha1.BpfProgCondNotUnloaded, nil
		}

		r.progId, err = bpfmanagentinternal.LoadBpfmanProgram(ctx, r.BpfmanClient, loadRequest)
		if err != nil {
			r.Logger.Error(err, "Failed to load KprobeProgram")
			return bpfmaniov1alpha1.BpfProgCondNotLoaded, err
		}

		r.Logger.Info("bpfman called to reload KprobeProgram on Node", "Name", bpfProgram.Name, "UUID", uuid)
	} else {
		// Program exists and bpfProgram K8s Object is up to date
		r.Logger.V(1).Info("Ignoring Object Change nothing to do in bpfman")
		r.progId = id
	}

	return bpfmaniov1alpha1.BpfProgCondLoaded, nil
}

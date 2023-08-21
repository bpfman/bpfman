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

//+kubebuilder:rbac:groups=bpfd.dev,resources=kprobeprograms,verbs=get;list;watch

// BpfProgramReconciler reconciles a BpfProgram object
type KprobeProgramReconciler struct {
	ReconcilerCommon
	currentKprobeProgram *bpfdiov1alpha1.KprobeProgram
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
// The Bpfd-Agent should reconcile whenever a KprobeProgram is updated,
// load the program to the node via bpfd, and then create a bpfProgram object
// to reflect per node state information.
func (r *KprobeProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfdiov1alpha1.KprobeProgram{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		Owns(&bpfdiov1alpha1.BpfProgram{},
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

func (r *KprobeProgramReconciler) expectedBpfPrograms(ctx context.Context) (*bpfdiov1alpha1.BpfProgramList, error) {
	progs := &bpfdiov1alpha1.BpfProgramList{}

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
	r.currentKprobeProgram = &bpfdiov1alpha1.KprobeProgram{}
	r.ourNode = &v1.Node{}
	r.Logger = log.FromContext(ctx)

	// Lookup K8s node object for this bpfd-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfd-agent node %s : %v",
			req.NamespacedName, err)
	}

	kprobePrograms := &bpfdiov1alpha1.KprobeProgramList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, kprobePrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting KprobePrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(kprobePrograms.Items) == 0 {
		return ctrl.Result{Requeue: false}, nil
	}

	// Get existing ebpf state from bpfd.
	programMap, err := bpfdagentinternal.ListBpfdPrograms(ctx, r.BpfdClient, internal.Kprobe)
	if err != nil {
		r.Logger.Error(err, "failed to list loaded bpfd programs")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
	}

	// Reconcile each KprobeProgram. Don't return error here because it will trigger an infinite reconcile loop, instead
	// report the error to user and retry if specified. For some errors the controller may not decide to retry.
	// Note: This only results in grpc calls to bpfd if we need to change something
	requeue := false // initialize requeue to false
	for _, kprobeProgram := range kprobePrograms.Items {
		r.Logger.Info("KprobeProgramController is reconciling", "key", req)
		r.currentKprobeProgram = &kprobeProgram
		result, err := reconcileProgram(ctx, r, r.currentKprobeProgram, &r.currentKprobeProgram.Spec.BpfProgramCommon, r.ourNode, programMap)
		if err != nil {
			r.Logger.Error(err, "Reconciling KprobeProgram Failed", "KprobeProgramName", r.currentKprobeProgram.Name, "ReconcileResult", ReconcileResultToString(result))
		}

		switch result {
		case Unchanged:
			// Nothing changed, so continue to the next program in the list if there is one.
		case Updated:
			// Since a k8s object has been updated that will trigger a new
			// reconcile for this object type, we want to exit the loop now to
			// avoid having multiple reconciles happening concurrently.
			return ctrl.Result{Requeue: false}, nil
		case Requeue:
			// A requeue has been requested.  Since there weren't any changes to
			// k8s objects, we don't need to exit this loop now and can continue
			// processing the rest of the programs in the list.
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
	bytecode interface{},
	id string,
	bpfProgram *bpfdiov1alpha1.BpfProgram,
	mapOwnerUuid types.UID) *gobpfd.LoadRequest {
	loadRequest := &gobpfd.LoadRequest{}
	loadRequest.Common = bpfdagentinternal.BuildBpfdCommon(
		bytecode,
		r.currentKprobeProgram.Spec.SectionName,
		internal.Kprobe, id,
		r.currentKprobeProgram.Spec.GlobalData,
		mapOwnerUuid,
	)
	// Namespace isn't supported yet in bpfd, so set it to an empty string.
	namespace := ""
	loadRequest.AttachInfo = &gobpfd.LoadRequest_KprobeAttachInfo{
		KprobeAttachInfo: &gobpfd.KprobeAttachInfo{
			FnName:    bpfProgram.Annotations[internal.KprobeProgramFunction],
			Offset:    r.currentKprobeProgram.Spec.Offset,
			Retprobe:  r.currentKprobeProgram.Spec.RetProbe,
			Namespace: &namespace,
		},
	}

	return loadRequest
}

// reconcileBpfdPrograms ONLY reconciles the bpfd state for a single BpfProgram.
// It does not interact with the k8s API in any way.
func (r *KprobeProgramReconciler) reconcileBpfdProgram(ctx context.Context,
	existingBpfPrograms map[string]*gobpfd.ListResponse_ListResult,
	bytecodeSelector *bpfdiov1alpha1.BytecodeSelector,
	bpfProgram *bpfdiov1alpha1.BpfProgram,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus) (bpfdiov1alpha1.BpfProgramConditionType, error) {

	r.Logger.V(1).Info("Existing bpfProgram", "ExistingMaps", bpfProgram.Spec.Maps, "UUID", bpfProgram.UID, "Name", bpfProgram.Name, "CurrentKprobeProgram", r.currentKprobeProgram.Name)

	id := string(bpfProgram.UID)

	existingProgram, doesProgramExist := existingBpfPrograms[id]
	if !doesProgramExist {
		r.Logger.V(1).Info("KprobeProgram doesn't exist on node")

		// If KprobeProgram is being deleted just exit
		if isBeingDeleted {
			return bpfdiov1alpha1.BpfProgCondNotLoaded, nil
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
		bytecode, err := bpfdagentinternal.GetBytecode(r.Client, bytecodeSelector)
		if err != nil {
			return bpfdiov1alpha1.BpfProgCondBytecodeSelectorError, fmt.Errorf("failed to process bytecode selector: %v", err)
		}

		loadRequest := r.buildKprobeLoadRequest(bytecode, id, bpfProgram, mapOwnerStatus.mapOwnerUuid)

		bpfProgramEntry, err := bpfdagentinternal.LoadBpfdProgram(ctx, r.BpfdClient, loadRequest)
		if err != nil {
			r.Logger.Error(err, "Failed to load KprobeProgram")
			return bpfdiov1alpha1.BpfProgCondNotLoaded, nil
		}

		r.expectedMaps = bpfProgramEntry

		return bpfdiov1alpha1.BpfProgCondLoaded, nil
	}

	// BpfProgram exists but either KprobeProgram is being deleted, node is no
	// longer selected, or map is not available....unload program
	if isBeingDeleted || !isNodeSelected ||
		(mapOwnerStatus.isSet && (!mapOwnerStatus.isFound || !mapOwnerStatus.isLoaded)) {
		r.Logger.V(1).Info("KprobeProgram exists on Node but is scheduled for deletion, not selected, or map not available",
			"isDeleted", isBeingDeleted, "isSelected", isNodeSelected, "mapIsSet", mapOwnerStatus.isSet,
			"mapIsFound", mapOwnerStatus.isFound, "mapIsLoaded", mapOwnerStatus.isLoaded)

		if err := bpfdagentinternal.UnloadBpfdProgram(ctx, r.BpfdClient, id); err != nil {
			r.Logger.Error(err, "Failed to unload KprobeProgram")
			return bpfdiov1alpha1.BpfProgCondNotUnloaded, nil
		}
		r.expectedMaps = nil

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
	bytecode, err := bpfdagentinternal.GetBytecode(r.Client, bytecodeSelector)
	if err != nil {
		return bpfdiov1alpha1.BpfProgCondBytecodeSelectorError, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	loadRequest := r.buildKprobeLoadRequest(bytecode, id, bpfProgram, mapOwnerStatus.mapOwnerUuid)

	r.Logger.V(1).WithValues("expectedProgram", loadRequest).WithValues("existingProgram", existingProgram).Info("StateMatch")

	isSame, reasons := bpfdagentinternal.DoesProgExist(existingProgram, loadRequest)
	if !isSame {
		r.Logger.V(1).Info("KprobeProgram is in wrong state, unloading and reloading", "Reason", reasons)
		if err := bpfdagentinternal.UnloadBpfdProgram(ctx, r.BpfdClient, id); err != nil {
			r.Logger.Error(err, "Failed to unload KprobeProgram")
			return bpfdiov1alpha1.BpfProgCondNotUnloaded, nil
		}

		bpfProgramEntry, err := bpfdagentinternal.LoadBpfdProgram(ctx, r.BpfdClient, loadRequest)
		if err != nil {
			r.Logger.Error(err, "Failed to load KprobeProgram")
			return bpfdiov1alpha1.BpfProgCondNotLoaded, err
		}

		r.expectedMaps = bpfProgramEntry
	} else {
		// Program exists and bpfProgram K8s Object is up to date
		r.Logger.V(1).Info("Ignoring Object Change nothing to do in bpfd")
		r.expectedMaps = bpfProgram.Spec.Maps
	}

	return bpfdiov1alpha1.BpfProgCondLoaded, nil
}

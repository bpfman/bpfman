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

//+kubebuilder:rbac:groups=bpfd.dev,resources=xdpprograms,verbs=get;list;watch

// BpfProgramReconciler reconciles a BpfProgram object
type XdpProgramReconciler struct {
	ReconcilerCommon
	currentXdpProgram *bpfdiov1alpha1.XdpProgram
	ourNode           *v1.Node
	interfaces        []string
}

func (r *XdpProgramReconciler) getRecCommon() *ReconcilerCommon {
	return &r.ReconcilerCommon
}

func (r *XdpProgramReconciler) getFinalizer() string {
	return internal.XdpProgramControllerFinalizer
}

func (r *XdpProgramReconciler) getRecType() string {
	return internal.Xdp.String()
}

// Must match with bpfd internal types
func xdpProceedOnToInt(proceedOn []bpfdiov1alpha1.XdpProceedOnValue) []int32 {
	var out []int32

	for _, p := range proceedOn {
		switch p {
		case "aborted":
			out = append(out, 0)
		case "drop":
			out = append(out, 1)
		case "pass":
			out = append(out, 2)
		case "tx":
			out = append(out, 3)
		case "redirect":
			out = append(out, 4)
		case "dispatcher_return":
			out = append(out, 31)
		}
	}

	return out
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfd-Agent should reconcile whenever a XdpProgram is updated,
// load the program to the node via bpfd, and then create a bpfProgram object
// to reflect per node state information.
func (r *XdpProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfdiov1alpha1.XdpProgram{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		Owns(&bpfdiov1alpha1.BpfProgram{},
			builder.WithPredicates(predicate.And(
				internal.BpfProgramTypePredicate(internal.Xdp.String()),
				internal.BpfProgramNodePredicate(r.NodeName)),
			),
		).
		// Only trigger reconciliation if node labels change since that could
		// make the XdpProgram no longer select the Node. Additionally only
		// care about node events specific to our node
		Watches(
			&source.Kind{Type: &v1.Node{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(predicate.LabelChangedPredicate{}, nodePredicate(r.NodeName))),
		).
		Complete(r)
}

func (r *XdpProgramReconciler) expectedBpfPrograms(ctx context.Context) (*bpfdiov1alpha1.BpfProgramList, error) {
	progs := &bpfdiov1alpha1.BpfProgramList{}

	for _, iface := range r.interfaces {
		bpfProgramName := fmt.Sprintf("%s-%s-%s", r.currentXdpProgram.Name, r.NodeName, iface)
		annotations := map[string]string{internal.XdpProgramInterface: iface}

		prog, err := r.createBpfProgram(ctx, bpfProgramName, r.getFinalizer(), r.currentXdpProgram, r.getRecType(), annotations)
		if err != nil {
			return nil, fmt.Errorf("failed to create BpfProgram %s: %v", bpfProgramName, err)
		}

		progs.Items = append(progs.Items, *prog)
	}

	return progs, nil
}

func (r *XdpProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentXdpProgram = &bpfdiov1alpha1.XdpProgram{}
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("xdp")
	var err error

	ctxLogger := log.FromContext(ctx)
	ctxLogger.Info("Reconcile XDP: Enter", "ReconcileKey", req)

	// Lookup K8s node object for this bpfd-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfd-agent node %s : %v",
			req.NamespacedName, err)
	}

	xdpPrograms := &bpfdiov1alpha1.XdpProgramList{}
	opts := []client.ListOption{}

	if err := r.List(ctx, xdpPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting XdpPrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(xdpPrograms.Items) == 0 {
		r.Logger.Info("XdpProgramController found no XDP Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	// Get existing ebpf state from bpfd.
	programMap, err := bpfdagentinternal.ListBpfdPrograms(ctx, r.BpfdClient, internal.Xdp)
	if err != nil {
		r.Logger.Error(err, "failed to list loaded bpfd programs")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
	}
	r.Logger.V(1).Info("Existing XDP programs", "loaded-xdp-programs", programMap)

	// Reconcile each XdpProgram. Don't return error here because it will trigger an infinite reconcile loop, instead
	// report the error to user and retry if specified. For some errors the controller may not decide to retry.
	// Note: This only results in grpc calls to bpfd if we need to change something
	requeue := false // initialize requeue to false
	for _, xdpProgram := range xdpPrograms.Items {
		r.Logger.Info("XdpProgramController is reconciling", "currentXdpProgram", xdpProgram.Name)
		r.currentXdpProgram = &xdpProgram

		r.interfaces, err = getInterfaces(&r.currentXdpProgram.Spec.InterfaceSelector, r.ourNode)
		if err != nil {
			r.Logger.Error(err, "failed to get interfaces for XdpProgram")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
		}

		// Reconcile each XdpProgram. Don't return error here because it will trigger an infinite reconcile loop, instead
		// report the error to user and retry if specified. For some errors the controller may not decide to retry.
		result, err := reconcileProgram(ctx, r, r.currentXdpProgram, &r.currentXdpProgram.Spec.BpfProgramCommon, r.ourNode, programMap)
		if err != nil {
			r.Logger.Error(err, "Reconciling XdpProgram Failed", "XdpProgramName", r.currentXdpProgram.Name, "ReconcileResult", result.String())
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

func (r *XdpProgramReconciler) buildXdpLoadRequest(
	bytecode interface{},
	uuid string,
	iface string,
	mapOwnerId *uint32) *gobpfd.LoadRequest {
	loadRequest := &gobpfd.LoadRequest{}
	loadRequest.Common = bpfdagentinternal.BuildBpfdCommon(
		bytecode,
		r.currentXdpProgram.Spec.SectionName,
		internal.Xdp,
		map[string]string{internal.UuidMetadataKey: uuid, internal.ProgramNameKey: r.currentXdpProgram.Name},
		r.currentXdpProgram.Spec.GlobalData,
		mapOwnerId,
	)
	loadRequest.AttachInfo = &gobpfd.LoadRequest_XdpAttachInfo{
		XdpAttachInfo: &gobpfd.XDPAttachInfo{
			Priority:  r.currentXdpProgram.Spec.Priority,
			Iface:     iface,
			ProceedOn: xdpProceedOnToInt(r.currentXdpProgram.Spec.ProceedOn),
		},
	}

	return loadRequest
}

// reconcileBpfdPrograms ONLY reconciles the bpfd state for a single BpfProgram.
// It does not interact with the k8s API in any way.
func (r *XdpProgramReconciler) reconcileBpfdProgram(ctx context.Context,
	existingBpfPrograms map[string]*gobpfd.ListResponse_ListResult,
	bytecodeSelector *bpfdiov1alpha1.BytecodeSelector,
	bpfProgram *bpfdiov1alpha1.BpfProgram,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus) (bpfdiov1alpha1.BpfProgramConditionType, error) {

	r.Logger.V(1).Info("Existing bpfProgram", "ExistingMaps", bpfProgram.Spec.Maps, "UUID", bpfProgram.UID, "Name", bpfProgram.Name, "CurrentXdpProgram", r.currentXdpProgram.Name)
	iface := bpfProgram.Annotations[internal.XdpProgramInterface]

	var err error
	uuid := string(bpfProgram.UID)

	getLoadRequest := func() (*gobpfd.LoadRequest, bpfdiov1alpha1.BpfProgramConditionType, error) {
		bytecode, err := bpfdagentinternal.GetBytecode(r.Client, bytecodeSelector)
		if err != nil {
			return nil, bpfdiov1alpha1.BpfProgCondBytecodeSelectorError, fmt.Errorf("failed to process bytecode selector: %v", err)
		}
		loadRequest := r.buildXdpLoadRequest(bytecode, uuid, iface, mapOwnerStatus.mapOwnerId)
		return loadRequest, bpfdiov1alpha1.BpfProgCondNone, nil
	}

	existingProgram, doesProgramExist := existingBpfPrograms[uuid]
	if !doesProgramExist {
		r.Logger.V(1).Info("XdpProgram doesn't exist on node for iface", "interface", iface)

		// If XdpProgram is being deleted just break out and remove finalizer
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
		loadRequest, condition, err := getLoadRequest()
		if err != nil {
			return condition, err
		}

		r.progId, r.expectedMaps, err = bpfdagentinternal.LoadBpfdProgram(ctx, r.BpfdClient, loadRequest)
		if err != nil {
			r.Logger.Error(err, "Failed to load XdpProgram")
			return bpfdiov1alpha1.BpfProgCondNotLoaded, nil
		}

		r.Logger.Info("bpfd called to load XdpProgram on Node", "Name", bpfProgram.Name, "UUID", uuid)
		return bpfdiov1alpha1.BpfProgCondLoaded, nil
	}

	// prog ID should already have been set
	id, err := bpfdagentinternal.GetID(bpfProgram)
	if err != nil {
		r.Logger.Error(err, "Failed to get program ID")
		return bpfdiov1alpha1.BpfProgCondNotLoaded, nil
	}

	// BpfProgram exists but either XdpProgram is being deleted, node is no
	// longer selected, or map is not available....unload program
	if isBeingDeleted || !isNodeSelected ||
		(mapOwnerStatus.isSet && (!mapOwnerStatus.isFound || !mapOwnerStatus.isLoaded)) {
		r.Logger.V(1).Info("XdpProgram exists on Node but is scheduled for deletion, not selected, or map not available",
			"isDeleted", isBeingDeleted, "isSelected", isNodeSelected, "mapIsSet", mapOwnerStatus.isSet,
			"mapIsFound", mapOwnerStatus.isFound, "mapIsLoaded", mapOwnerStatus.isLoaded)

		if err := bpfdagentinternal.UnloadBpfdProgram(ctx, r.BpfdClient, *id); err != nil {
			r.Logger.Error(err, "Failed to unload XdpProgram")
			return bpfdiov1alpha1.BpfProgCondNotUnloaded, nil
		}
		r.expectedMaps = nil

		r.Logger.Info("bpfd called to unload XdpProgram on Node", "Name", bpfProgram.Name, "UUID", id)

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

	isSame, reasons := bpfdagentinternal.DoesProgExist(existingProgram, loadRequest)
	if !isSame {
		r.Logger.V(1).Info("XdpProgram is in wrong state, unloading and reloading", "Reason", reasons)

		if err := bpfdagentinternal.UnloadBpfdProgram(ctx, r.BpfdClient, *id); err != nil {
			r.Logger.Error(err, "Failed to unload XdpProgram")
			return bpfdiov1alpha1.BpfProgCondNotUnloaded, nil
		}

		r.progId, r.expectedMaps, err = bpfdagentinternal.LoadBpfdProgram(ctx, r.BpfdClient, loadRequest)
		if err != nil {
			r.Logger.Error(err, "Failed to load XdpProgram")
			return bpfdiov1alpha1.BpfProgCondNotLoaded, nil
		}

		r.Logger.Info("bpfd called to reload XdpProgram on Node", "Name", bpfProgram.Name, "UUID", id)
	} else {
		// Program exists and bpfProgram K8s Object is up to date
		r.Logger.V(1).Info("Ignoring Object Change nothing to do in bpfd")
		r.progId = id
		r.expectedMaps = bpfProgram.Spec.Maps
	}

	return bpfdiov1alpha1.BpfProgCondLoaded, nil
}

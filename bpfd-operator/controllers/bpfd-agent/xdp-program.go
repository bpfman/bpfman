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
	r.Logger = log.FromContext(ctx)
	var err error

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
		return ctrl.Result{Requeue: false}, nil
	}

	// Get existing ebpf state from bpfd.
	programMap, err := bpfdagentinternal.ListBpfdPrograms(ctx, r.BpfdClient, internal.Xdp)
	if err != nil {
		r.Logger.Error(err, "failed to list loaded bpfd programs")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
	}
	r.Logger.V(1).WithValues("loaded-xdp-programs", programMap).Info("Existing XDP programs")

	// Reconcile every XdpProgram Object
	// note: This doesn't necessarily result in any extra grpc calls to bpfd
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
		retry, err := reconcileProgram(ctx, r, r.currentXdpProgram, &r.currentXdpProgram.Spec.BpfProgramCommon, r.ourNode, programMap)
		if err != nil {
			r.Logger.Error(err, "Reconciling XdpProgram Failed", "XdpProgramName", r.currentXdpProgram.Name, "Retrying", retry)
			return ctrl.Result{Requeue: retry, RequeueAfter: retryDurationAgent}, nil
		}
	}

	return ctrl.Result{Requeue: false}, nil
}

// reconcileBpfdPrograms ONLY reconciles the bpfd state for a single BpfProgram.
// It does not interact with the k8s API in any way.
func (r *XdpProgramReconciler) reconcileBpfdProgram(ctx context.Context,
	existingBpfPrograms map[string]*gobpfd.ListResponse_ListResult,
	bytecode interface{},
	bpfProgram *bpfdiov1alpha1.BpfProgram,
	isNodeSelected bool,
	isBeingDeleted bool) (bpfdiov1alpha1.BpfProgramConditionType, error) {

	r.Logger.V(1).Info("Existing bpfProgram", "ExistingMaps", bpfProgram.Spec.Maps, "UUID", bpfProgram.UID, "Name", bpfProgram.Name, "CurrentXdpProgram", r.currentXdpProgram.Name)
	iface := bpfProgram.Annotations[internal.XdpProgramInterface]

	loadRequest := &gobpfd.LoadRequest{}
	var err error
	id := string(bpfProgram.UID)
	loadRequest.Common = bpfdagentinternal.BuildBpfdCommon(bytecode, r.currentXdpProgram.Spec.SectionName, internal.Xdp, id, r.currentXdpProgram.Spec.GlobalData)

	loadRequest.AttachInfo = &gobpfd.LoadRequest_XdpAttachInfo{
		XdpAttachInfo: &gobpfd.XDPAttachInfo{
			Priority:  r.currentXdpProgram.Spec.Priority,
			Iface:     iface,
			ProceedOn: xdpProceedOnToInt(r.currentXdpProgram.Spec.ProceedOn),
		},
	}

	existingProgram, doesProgramExist := existingBpfPrograms[id]
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

		// otherwise load it
		r.expectedMaps, err = bpfdagentinternal.LoadBpfdProgram(ctx, r.BpfdClient, loadRequest)
		if err != nil {
			r.Logger.Error(err, "Failed to load XdpProgram")
			return bpfdiov1alpha1.BpfProgCondNotLoaded, nil
		}

		r.Logger.V(1).WithValues("UUID", id, "maps", r.expectedMaps).Info("Loaded XdpProgram on Node")
		return bpfdiov1alpha1.BpfProgCondLoaded, nil
	}

	// BpfProgram exists but either XdpProgram is being deleted or node is no
	// longer selected....unload program
	if isBeingDeleted || !isNodeSelected {
		r.Logger.V(1).Info("XdpProgram exists on Node but is scheduled for deletion or node is no longer selected", "isDeleted", isBeingDeleted,
			"isSelected", isNodeSelected)
		if err := bpfdagentinternal.UnloadBpfdProgram(ctx, r.BpfdClient, id); err != nil {
			r.Logger.Error(err, "Failed to unload XdpProgram")
			return bpfdiov1alpha1.BpfProgCondNotUnloaded, nil
		}
		r.expectedMaps = nil

		if isBeingDeleted {
			return bpfdiov1alpha1.BpfProgCondUnloaded, nil
		}

		return bpfdiov1alpha1.BpfProgCondNotSelected, nil
	}

	// BpfProgram exists but is not correct state, unload and recreate
	isSame, reasons := bpfdagentinternal.DoesProgExist(existingProgram, loadRequest)
	if !isSame {
		r.Logger.V(1).Info("XdpProgram is in wrong state, unloading and reloading", "Reason", reasons)
		if err := bpfdagentinternal.UnloadBpfdProgram(ctx, r.BpfdClient, id); err != nil {
			r.Logger.Error(err, "Failed to unload XdpProgram")
			return bpfdiov1alpha1.BpfProgCondNotUnloaded, nil
		}

		r.expectedMaps, err = bpfdagentinternal.LoadBpfdProgram(ctx, r.BpfdClient, loadRequest)
		if err != nil {
			r.Logger.Error(err, "Failed to load XdpProgram")
			return bpfdiov1alpha1.BpfProgCondNotLoaded, nil
		}

		r.Logger.V(1).WithValues("UUID", id, "ProgramEntry", r.expectedMaps).Info("ReLoaded XdpProgram on Node")
	} else {
		// Program exists and bpfProgram K8s Object is up to date
		r.Logger.V(1).Info("Ignoring Object Change nothing to do in bpfd")
		r.expectedMaps = bpfProgram.Spec.Maps
	}

	return bpfdiov1alpha1.BpfProgCondLoaded, nil
}

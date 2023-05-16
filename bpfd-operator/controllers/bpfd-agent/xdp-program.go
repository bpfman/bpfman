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

//+kubebuilder:rbac:groups=bpfd.io,resources=xdpprograms,verbs=get;list;watch

// BpfProgramReconciler reconciles a BpfProgram object
type XdpProgramReconciler struct {
	ReconcilerCommon
	currentXdpProgram *bpfdiov1alpha1.XdpProgram
	ourNode           *v1.Node
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
		Owns(&bpfdiov1alpha1.BpfProgram{}, builder.WithPredicates(internal.BpfProgramTypePredicate(internal.Xdp.String()))).
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

func (r *XdpProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentXdpProgram = &bpfdiov1alpha1.XdpProgram{}
	r.ourNode = &v1.Node{}

	r.Logger = log.FromContext(ctx)

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
	programMap, err := bpfdagentinternal.ListBpfdPrograms(ctx, r.BpfdClient, internal.Tc)
	if err != nil {
		r.Logger.Error(err, "failed to list loaded bpfd programs")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
	}

	// Reconcile every XdpProgram Object
	// note: This doesn't necessarily result in any extra grpc calls to bpfd
	for _, XdpProgram := range xdpPrograms.Items {
		r.Logger.Info("bpfd-agent is reconciling", "XdpProgram", XdpProgram.Name)
		r.currentXdpProgram = &XdpProgram
		retry, err := reconcileProgram(ctx, r, r.currentXdpProgram, &r.currentXdpProgram.Spec.BpfProgramCommon, r.ourNode, programMap)
		if err != nil {
			r.Logger.Error(err, "Reconciling XdpProgram Failed", "XdpProgramName", r.currentXdpProgram.Name, "Retrying", retry)
			return ctrl.Result{Requeue: retry, RequeueAfter: retryDurationAgent}, nil
		}
	}

	return ctrl.Result{Requeue: false}, nil
}

// reconcileBpfdPrograms ONLY reconciles the bpfd state for a single BpfProgram.
// It does interact with the k8s API in any way.
func (r *XdpProgramReconciler) reconcileBpfdPrograms(ctx context.Context,
	existingBpfPrograms map[string]*gobpfd.ListResponse_ListResult,
	bytecode interface{},
	isNodeSelected bool,
	isBeingDeleted bool) (bpfProgramConditionType, error) {

	ifaces, err := getInterfaces(&r.currentXdpProgram.Spec.InterfaceSelector, r.ourNode)
	if err != nil {
		return BpfProgCondNotLoaded, fmt.Errorf("failed to get interfaces for XdpProgram %s: %v", r.currentXdpProgram.Name, err)
	}

	r.Logger.V(1).Info("Existing bpfProgramEntries", "ExistingEntries", r.bpfProgram.Spec.Programs)
	bpfProgramEntries := make(map[string]map[string]string)
	// DeepCopy the existing programs
	for k, v := range r.bpfProgram.Spec.Programs {
		bpfProgramEntries[k] = v
	}

	for _, iface := range ifaces {
		loadRequest := &gobpfd.LoadRequest{}

		id := bpfdagentinternal.GenIdFromName(fmt.Sprintf("%s-%s", r.currentXdpProgram.Name, iface))

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
			r.Logger.V(1).Info("XdpProgram doesn't exist on node")

			// If XdpProgram is being deleted just break out and remove finalizer
			if isBeingDeleted {
				break
			}

			// Make sure if we're not selected just exit
			if !isNodeSelected {
				break
			}

			// otherwise load it
			bpfProgramEntry, err := bpfdagentinternal.LoadBpfdProgram(ctx, r.BpfdClient, loadRequest)
			if err != nil {
				r.Logger.Error(err, "Failed to load XdpProgram")
				return BpfProgCondNotLoaded, err
			}

			bpfProgramEntries[id] = bpfProgramEntry
			r.Logger.V(1).WithValues("UUID", id, "ProgramEntry", bpfProgramEntries).Info("Loaded XdpProgram on Node")

			// Move to next program
			continue
		}

		// BpfProgram exists but either XdpProgram is being deleted or node is no
		// longer selected....unload program
		if isBeingDeleted || !isNodeSelected {
			r.Logger.V(1).Info("XdpProgram exists on Node but is scheduled for deletion or node is no longer selected", "isDeleted", isBeingDeleted,
				"isSelected", isNodeSelected)
			if err := bpfdagentinternal.UnloadBpfdProgram(ctx, r.BpfdClient, id); err != nil {
				r.Logger.Error(err, "Failed to unload XdpProgram")
				return BpfProgCondLoaded, err
			}
			delete(bpfProgramEntries, id)

			// continue to next program
			continue
		}

		r.Logger.V(1).WithValues("expectedProgram", loadRequest).WithValues("existingProgram", existingProgram).Info("StateMatch")
		// BpfProgram exists but is not correct state, unload and recreate
		if !bpfdagentinternal.DoesProgExist(existingProgram, loadRequest) {
			r.Logger.V(1).Info("XdpProgram is in wrong state, unloading and reloading")
			if err := bpfdagentinternal.UnloadBpfdProgram(ctx, r.BpfdClient, id); err != nil {
				r.Logger.Error(err, "Failed to unload XdpProgram")
				return BpfProgCondNotUnloaded, err
			}

			bpfProgramEntry, err := bpfdagentinternal.LoadBpfdProgram(ctx, r.BpfdClient, loadRequest)
			if err != nil {
				r.Logger.Error(err, "Failed to load XdpProgram")
				return BpfProgCondNotLoaded, err
			}

			r.Logger.V(1).WithValues("UUID", id, "ProgramEntry", bpfProgramEntry).Info("ReLoaded XdpProgram on Node")
			bpfProgramEntries[id] = bpfProgramEntry
		} else {
			// Program already exists, but bpfProgram K8s Object might not be up to date
			r.Logger.V(1).Info("XdpProgram already loaded on Node")
			if _, ok := r.bpfProgram.Spec.Programs[id]; !ok {
				maps, err := bpfdagentinternal.GetMapsForUUID(id)
				if err != nil {
					r.Logger.Error(err, "failed to get bpfProgram's Maps")
					return BpfProgCondNotLoaded, err
				}

				bpfProgramEntries[id] = maps
			} else {
				// Program exists and bpfProgram K8s Object is up to date
				r.Logger.V(1).Info("Ignoring Object Change nothing to do in bpfd")
			}
		}
	}

	r.Logger.V(1).Info("Setting bpfProgramEntries", "Entries", bpfProgramEntries)
	r.expectedPrograms = bpfProgramEntries

	if isBeingDeleted {
		return BpfProgCondUnloaded, nil
	}

	if !isNodeSelected {
		return BpfProgCondNotSelected, nil
	}

	return BpfProgCondLoaded, nil
}

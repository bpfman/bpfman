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
	"k8s.io/apimachinery/pkg/types"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=xdpprograms,verbs=get;list;watch

// BpfProgramReconciler reconciles a BpfProgram object
type XdpProgramReconciler struct {
	ReconcilerCommon
	currentXdpProgram *bpfmaniov1alpha1.XdpProgram
	interfaces        []string
	ourNode           *v1.Node
}

func (r *XdpProgramReconciler) getFinalizer() string {
	return internal.XdpProgramControllerFinalizer
}

func (r *XdpProgramReconciler) getRecType() string {
	return internal.Xdp.String()
}

func (r *XdpProgramReconciler) getProgType() internal.ProgramType {
	return internal.Xdp
}

func (r *XdpProgramReconciler) getName() string {
	return r.currentXdpProgram.Name
}

func (r *XdpProgramReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *XdpProgramReconciler) getBpfProgramCommon() *bpfmaniov1alpha1.BpfProgramCommon {
	return &r.currentXdpProgram.Spec.BpfProgramCommon
}

func (r *XdpProgramReconciler) setCurrentProgram(program client.Object) error {
	var err error
	var ok bool

	r.currentXdpProgram, ok = program.(*bpfmaniov1alpha1.XdpProgram)
	if !ok {
		return fmt.Errorf("failed to cast program to XdpProgram")
	}

	r.interfaces, err = getInterfaces(&r.currentXdpProgram.Spec.InterfaceSelector, r.ourNode)
	if err != nil {
		return fmt.Errorf("failed to get interfaces for XdpProgram: %v", err)
	}

	return nil
}

// Must match with bpfman internal types
func xdpProceedOnToInt(proceedOn []bpfmaniov1alpha1.XdpProceedOnValue) []int32 {
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
// The Bpfman-Agent should reconcile whenever a XdpProgram is updated,
// load the program to the node via bpfman, and then create a bpfProgram object
// to reflect per node state information.
func (r *XdpProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.XdpProgram{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		Owns(&bpfmaniov1alpha1.BpfProgram{},
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

func (r *XdpProgramReconciler) getExpectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfProgramList, error) {
	progs := &bpfmaniov1alpha1.BpfProgramList{}

	for _, iface := range r.interfaces {
		bpfProgramName := fmt.Sprintf("%s-%s-%s", r.currentXdpProgram.Name, r.NodeName, iface)
		annotations := map[string]string{internal.XdpProgramInterface: iface}

		prog, err := r.createBpfProgram(bpfProgramName, r.getFinalizer(), r.currentXdpProgram, r.getRecType(), annotations)
		if err != nil {
			return nil, fmt.Errorf("failed to create BpfProgram %s: %v", bpfProgramName, err)
		}

		progs.Items = append(progs.Items, *prog)
	}

	return progs, nil
}

func (r *XdpProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentXdpProgram = &bpfmaniov1alpha1.XdpProgram{}
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("xdp")

	ctxLogger := log.FromContext(ctx)
	ctxLogger.Info("Reconcile XDP: Enter", "ReconcileKey", req)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	xdpPrograms := &bpfmaniov1alpha1.XdpProgramList{}
	opts := []client.ListOption{}

	if err := r.List(ctx, xdpPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting XdpPrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(xdpPrograms.Items) == 0 {
		r.Logger.Info("XdpProgramController found no XDP Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	// Create a list of Xdp programs to pass into reconcileCommon()
	var xdpObjects []client.Object = make([]client.Object, len(xdpPrograms.Items))
	for i := range xdpPrograms.Items {
		xdpObjects[i] = &xdpPrograms.Items[i]
	}

	// Reconcile each TcProgram.
	return r.reconcileCommon(ctx, r, xdpObjects)
}

func (r *XdpProgramReconciler) getLoadRequest(bpfProgram *bpfmaniov1alpha1.BpfProgram, mapOwnerId *uint32) (*gobpfman.LoadRequest, error) {
	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.currentXdpProgram.Spec.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	loadRequest := gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentXdpProgram.Spec.BpfFunctionName,
		ProgramType: uint32(internal.Xdp),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_XdpAttachInfo{
				XdpAttachInfo: &gobpfman.XDPAttachInfo{
					Priority:  r.currentXdpProgram.Spec.Priority,
					Iface:     bpfProgram.Annotations[internal.XdpProgramInterface],
					ProceedOn: xdpProceedOnToInt(r.currentXdpProgram.Spec.ProceedOn),
				},
			},
		},
		Metadata:   map[string]string{internal.UuidMetadataKey: string(bpfProgram.UID), internal.ProgramNameKey: r.currentXdpProgram.Name},
		GlobalData: r.currentXdpProgram.Spec.GlobalData,
		MapOwnerId: mapOwnerId,
	}

	return &loadRequest, nil
}

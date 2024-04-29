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
	"github.com/bpfman/bpfman/bpfman-operator/internal"

	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	v1 "k8s.io/api/core/v1"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=tcprograms,verbs=get;list;watch

// TcProgramReconciler reconciles a tcProgram object by creating multiple
// bpfProgram objects and managing bpfman for each one.
type TcProgramReconciler struct {
	ReconcilerCommon
	currentTcProgram *bpfmaniov1alpha1.TcProgram
	interfaces       []string
	ourNode          *v1.Node
}

func (r *TcProgramReconciler) getFinalizer() string {
	return internal.TcProgramControllerFinalizer
}

func (r *TcProgramReconciler) getRecType() string {
	return internal.Tc.String()
}

func (r *TcProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tc
}

func (r *TcProgramReconciler) getName() string {
	return r.currentTcProgram.Name
}

func (r *TcProgramReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *TcProgramReconciler) getBpfProgramCommon() *bpfmaniov1alpha1.BpfProgramCommon {
	return &r.currentTcProgram.Spec.BpfProgramCommon
}

func (r *TcProgramReconciler) setCurrentProgram(program client.Object) error {
	var err error
	var ok bool

	r.currentTcProgram, ok = program.(*bpfmaniov1alpha1.TcProgram)
	if !ok {
		return fmt.Errorf("failed to cast program to TcProgram")
	}

	r.interfaces, err = getInterfaces(&r.currentTcProgram.Spec.InterfaceSelector, r.ourNode)
	if err != nil {
		return fmt.Errorf("failed to get interfaces for TcProgram: %v", err)
	}

	return nil
}

// Must match with bpfman internal types
func tcProceedOnToInt(proceedOn []bpfmaniov1alpha1.TcProceedOnValue) []int32 {
	var out []int32

	for _, p := range proceedOn {
		switch p {
		case "unspec":
			out = append(out, -1)
		case "ok":
			out = append(out, 0)
		case "reclassify":
			out = append(out, 1)
		case "shot":
			out = append(out, 2)
		case "pipe":
			out = append(out, 3)
		case "stolen":
			out = append(out, 4)
		case "queued":
			out = append(out, 5)
		case "repeat":
			out = append(out, 6)
		case "redirect":
			out = append(out, 7)
		case "trap":
			out = append(out, 8)
		case "dispatcher_return":
			out = append(out, 30)
		}
	}

	return out
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfman-Agent should reconcile whenever a TcProgram is updated,
// load the program to the node via bpfman, and then create bpfProgram object(s)
// to reflect per node state information.
func (r *TcProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.TcProgram{}, builder.WithPredicates(predicate.And(
			predicate.GenerationChangedPredicate{},
			predicate.ResourceVersionChangedPredicate{}),
		),
		).
		Owns(&bpfmaniov1alpha1.BpfProgram{},
			builder.WithPredicates(predicate.And(
				internal.BpfProgramTypePredicate(internal.Tc.String()),
				internal.BpfProgramNodePredicate(r.NodeName)),
			),
		).
		// Only trigger reconciliation if node labels change since that could
		// make the TcProgram no longer select the Node. Additionally only
		// care about events specific to our node
		Watches(
			&source.Kind{Type: &v1.Node{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(predicate.LabelChangedPredicate{}, nodePredicate(r.NodeName))),
		).
		Complete(r)
}

func (r *TcProgramReconciler) getExpectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfProgramList, error) {
	progs := &bpfmaniov1alpha1.BpfProgramList{}
	for _, iface := range r.interfaces {
		bpfProgramName := fmt.Sprintf("%s-%s-%s", r.currentTcProgram.Name, r.NodeName, iface)
		annotations := map[string]string{internal.TcProgramInterface: iface}

		prog, err := r.createBpfProgram(bpfProgramName, r.getFinalizer(), r.currentTcProgram, r.getRecType(), annotations)
		if err != nil {
			return nil, fmt.Errorf("failed to create BpfProgram %s: %v", bpfProgramName, err)
		}

		progs.Items = append(progs.Items, *prog)
	}

	return progs, nil
}

func (r *TcProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentTcProgram = &bpfmaniov1alpha1.TcProgram{}
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("tc")

	ctxLogger := log.FromContext(ctx)
	ctxLogger.Info("Reconcile TC: Enter", "ReconcileKey", req)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	tcPrograms := &bpfmaniov1alpha1.TcProgramList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, tcPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting TcPrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(tcPrograms.Items) == 0 {
		r.Logger.Info("TcProgramController found no TC Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	// Create a list of tc programs to pass into reconcileCommon()
	var tcObjects []client.Object = make([]client.Object, len(tcPrograms.Items))
	for i := range tcPrograms.Items {
		tcObjects[i] = &tcPrograms.Items[i]
	}

	// Reconcile each TcProgram.
	return r.reconcileCommon(ctx, r, tcObjects)
}

func (r *TcProgramReconciler) getLoadRequest(bpfProgram *bpfmaniov1alpha1.BpfProgram, mapOwnerId *uint32) (*gobpfman.LoadRequest, error) {
	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.currentTcProgram.Spec.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	loadRequest := gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentTcProgram.Spec.BpfFunctionName,
		ProgramType: uint32(internal.Tc),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TcAttachInfo{
				TcAttachInfo: &gobpfman.TCAttachInfo{
					Priority:  r.currentTcProgram.Spec.Priority,
					Iface:     bpfProgram.Annotations[internal.TcProgramInterface],
					Direction: r.currentTcProgram.Spec.Direction,
					ProceedOn: tcProceedOnToInt(r.currentTcProgram.Spec.ProceedOn),
				},
			},
		},
		Metadata:   map[string]string{internal.UuidMetadataKey: string(bpfProgram.UID), internal.ProgramNameKey: r.currentTcProgram.Name},
		GlobalData: r.currentTcProgram.Spec.GlobalData,
		MapOwnerId: mapOwnerId,
	}

	return &loadRequest, nil
}

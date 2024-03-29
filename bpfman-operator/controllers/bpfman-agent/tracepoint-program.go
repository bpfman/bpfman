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
	ourNode                  *v1.Node
	currentTracepointProgram *bpfmaniov1alpha1.TracepointProgram
}

func (r *TracepointProgramReconciler) getFinalizer() string {
	return internal.TracepointProgramControllerFinalizer
}

func (r *TracepointProgramReconciler) getRecType() string {
	return internal.Tracepoint.String()
}

func (r *TracepointProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tracepoint
}

func (r *TracepointProgramReconciler) getName() string {
	return r.currentTracepointProgram.Name
}

func (r *TracepointProgramReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *TracepointProgramReconciler) getBpfProgramCommon() *bpfmaniov1alpha1.BpfProgramCommon {
	return &r.currentTracepointProgram.Spec.BpfProgramCommon
}

func (r *TracepointProgramReconciler) setCurrentProgram(program client.Object) error {
	var ok bool

	r.currentTracepointProgram, ok = program.(*bpfmaniov1alpha1.TracepointProgram)
	if !ok {
		return fmt.Errorf("failed to cast program to TracepointProgram")
	}

	return nil
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

func (r *TracepointProgramReconciler) getExpectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfProgramList, error) {
	progs := &bpfmaniov1alpha1.BpfProgramList{}

	for _, tracepoint := range r.currentTracepointProgram.Spec.Names {
		// sanitize tracepoint name to work in a bpfProgram name
		sanatizedTrace := strings.Replace(strings.Replace(tracepoint, "/", "-", -1), "_", "-", -1)
		bpfProgramName := fmt.Sprintf("%s-%s-%s", r.currentTracepointProgram.Name, r.NodeName, sanatizedTrace)

		annotations := map[string]string{internal.TracepointProgramTracepoint: tracepoint}

		prog, err := r.createBpfProgram(bpfProgramName, r.getFinalizer(), r.currentTracepointProgram, r.getRecType(), annotations)
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

	// Create a list of Tracepoint programs to pass into reconcileCommon()
	var tracepointObjects []client.Object = make([]client.Object, len(tracepointPrograms.Items))
	for i := range tracepointPrograms.Items {
		tracepointObjects[i] = &tracepointPrograms.Items[i]
	}

	// Reconcile each TcProgram.
	return r.reconcileCommon(ctx, r, tracepointObjects)
}

func (r *TracepointProgramReconciler) getLoadRequest(bpfProgram *bpfmaniov1alpha1.BpfProgram, mapOwnerId *uint32) (*gobpfman.LoadRequest, error) {
	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.currentTracepointProgram.Spec.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	loadRequest := gobpfman.LoadRequest{
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
		Metadata:   map[string]string{internal.UuidMetadataKey: string(bpfProgram.UID), internal.ProgramNameKey: r.currentTracepointProgram.Name},
		GlobalData: r.currentTracepointProgram.Spec.GlobalData,
		MapOwnerId: mapOwnerId,
	}

	return &loadRequest, nil
}

/*
Copyright 2024.

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

//+kubebuilder:rbac:groups=bpfman.io,resources=fentryprograms,verbs=get;list;watch

// BpfProgramReconciler reconciles a BpfProgram object
type FentryProgramReconciler struct {
	ReconcilerCommon
	currentFentryProgram *bpfmaniov1alpha1.FentryProgram
	ourNode              *v1.Node
}

func (r *FentryProgramReconciler) getFinalizer() string {
	return internal.FentryProgramControllerFinalizer
}

func (r *FentryProgramReconciler) getRecType() string {
	return internal.FentryString
}

func (r *FentryProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tracing
}

func (r *FentryProgramReconciler) getName() string {
	return r.currentFentryProgram.Name
}

func (r *FentryProgramReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *FentryProgramReconciler) getBpfProgramCommon() *bpfmaniov1alpha1.BpfProgramCommon {
	return &r.currentFentryProgram.Spec.BpfProgramCommon
}

func (r *FentryProgramReconciler) setCurrentProgram(program client.Object) error {
	var ok bool

	r.currentFentryProgram, ok = program.(*bpfmaniov1alpha1.FentryProgram)
	if !ok {
		return fmt.Errorf("failed to cast program to FentryProgram")
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfman-Agent should reconcile whenever a FentryProgram is updated,
// load the program to the node via bpfman, and then create a bpfProgram object
// to reflect per node state information.
func (r *FentryProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.FentryProgram{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		Owns(&bpfmaniov1alpha1.BpfProgram{},
			builder.WithPredicates(predicate.And(
				internal.BpfProgramTypePredicate(internal.FentryString),
				internal.BpfProgramNodePredicate(r.NodeName)),
			),
		).
		// Only trigger reconciliation if node labels change since that could
		// make the FentryProgram no longer select the Node. Additionally only
		// care about node events specific to our node
		Watches(
			&source.Kind{Type: &v1.Node{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(predicate.LabelChangedPredicate{}, nodePredicate(r.NodeName))),
		).
		Complete(r)
}

func (r *FentryProgramReconciler) getExpectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfProgramList, error) {
	progs := &bpfmaniov1alpha1.BpfProgramList{}

	// sanitize fentry name to work in a bpfProgram name
	sanatizedFentry := strings.Replace(strings.Replace(r.currentFentryProgram.Spec.FunctionName, "/", "-", -1), "_", "-", -1)
	bpfProgramName := fmt.Sprintf("%s-%s-%s", r.currentFentryProgram.Name, r.NodeName, sanatizedFentry)

	annotations := map[string]string{internal.FentryProgramFunction: r.currentFentryProgram.Spec.FunctionName}

	prog, err := r.createBpfProgram(bpfProgramName, r.getFinalizer(), r.currentFentryProgram, r.getRecType(), annotations)
	if err != nil {
		return nil, fmt.Errorf("failed to create BpfProgram %s: %v", bpfProgramName, err)
	}

	progs.Items = append(progs.Items, *prog)

	return progs, nil
}

func (r *FentryProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentFentryProgram = &bpfmaniov1alpha1.FentryProgram{}
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("fentry")

	ctxLogger := log.FromContext(ctx)
	ctxLogger.Info("Reconcile Fentry: Enter", "ReconcileKey", req)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	fentryPrograms := &bpfmaniov1alpha1.FentryProgramList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, fentryPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting FentryPrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(fentryPrograms.Items) == 0 {
		r.Logger.Info("FentryProgramController found no Fentry Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	// Create a list of fentry programs to pass into reconcileCommon()
	var fentryObjects []client.Object = make([]client.Object, len(fentryPrograms.Items))
	for i := range fentryPrograms.Items {
		fentryObjects[i] = &fentryPrograms.Items[i]
	}

	// Reconcile each FentryProgram.
	return r.reconcileCommon(ctx, r, fentryObjects)
}

func (r *FentryProgramReconciler) getLoadRequest(bpfProgram *bpfmaniov1alpha1.BpfProgram, mapOwnerId *uint32) (*gobpfman.LoadRequest, error) {
	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.currentFentryProgram.Spec.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	loadRequest := gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentFentryProgram.Spec.BpfFunctionName,
		ProgramType: uint32(internal.Tracing),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_FentryAttachInfo{
				FentryAttachInfo: &gobpfman.FentryAttachInfo{
					FnName: bpfProgram.Annotations[internal.FentryProgramFunction],
				},
			},
		},
		Metadata:   map[string]string{internal.UuidMetadataKey: string(bpfProgram.UID), internal.ProgramNameKey: r.currentFentryProgram.Name},
		GlobalData: r.currentFentryProgram.Spec.GlobalData,
		MapOwnerId: mapOwnerId,
	}

	return &loadRequest, nil
}

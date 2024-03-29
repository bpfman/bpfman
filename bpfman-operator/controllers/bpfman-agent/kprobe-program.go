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

func (r *KprobeProgramReconciler) getFinalizer() string {
	return internal.KprobeProgramControllerFinalizer
}

func (r *KprobeProgramReconciler) getRecType() string {
	return internal.Kprobe.String()
}

func (r *KprobeProgramReconciler) getProgType() internal.ProgramType {
	return internal.Kprobe
}

func (r *KprobeProgramReconciler) getName() string {
	return r.currentKprobeProgram.Name
}

func (r *KprobeProgramReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *KprobeProgramReconciler) getBpfProgramCommon() *bpfmaniov1alpha1.BpfProgramCommon {
	return &r.currentKprobeProgram.Spec.BpfProgramCommon
}

func (r *KprobeProgramReconciler) setCurrentProgram(program client.Object) error {
	var ok bool

	r.currentKprobeProgram, ok = program.(*bpfmaniov1alpha1.KprobeProgram)
	if !ok {
		return fmt.Errorf("failed to cast program to KprobeProgram")
	}

	return nil
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

func (r *KprobeProgramReconciler) getExpectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfProgramList, error) {
	progs := &bpfmaniov1alpha1.BpfProgramList{}

	// sanitize kprobe name to work in a bpfProgram name
	sanatizedKprobe := strings.Replace(strings.Replace(r.currentKprobeProgram.Spec.FunctionName, "/", "-", -1), "_", "-", -1)
	bpfProgramName := fmt.Sprintf("%s-%s-%s", r.currentKprobeProgram.Name, r.NodeName, sanatizedKprobe)

	annotations := map[string]string{internal.KprobeProgramFunction: r.currentKprobeProgram.Spec.FunctionName}

	prog, err := r.createBpfProgram(bpfProgramName, r.getFinalizer(), r.currentKprobeProgram, r.getRecType(), annotations)
	if err != nil {
		return nil, fmt.Errorf("failed to create BpfProgram %s: %v", bpfProgramName, err)
	}

	progs.Items = append(progs.Items, *prog)

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

	// Create a list of kprobe programs to pass into reconcileCommon()
	var kprobeObjects []client.Object = make([]client.Object, len(kprobePrograms.Items))
	for i := range kprobePrograms.Items {
		kprobeObjects[i] = &kprobePrograms.Items[i]
	}

	// Reconcile each KprobeProgram.
	return r.reconcileCommon(ctx, r, kprobeObjects)
}

func (r *KprobeProgramReconciler) getLoadRequest(bpfProgram *bpfmaniov1alpha1.BpfProgram, mapOwnerId *uint32) (*gobpfman.LoadRequest, error) {
	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.currentKprobeProgram.Spec.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	// Container PID isn't supported yet in bpfman, so set it to zero.
	var container_pid int32 = 0

	loadRequest := gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentKprobeProgram.Spec.BpfFunctionName,
		ProgramType: uint32(internal.Kprobe),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_KprobeAttachInfo{
				KprobeAttachInfo: &gobpfman.KprobeAttachInfo{
					FnName:       bpfProgram.Annotations[internal.KprobeProgramFunction],
					Offset:       r.currentKprobeProgram.Spec.Offset,
					Retprobe:     r.currentKprobeProgram.Spec.RetProbe,
					ContainerPid: &container_pid,
				},
			},
		},
		Metadata:   map[string]string{internal.UuidMetadataKey: string(bpfProgram.UID), internal.ProgramNameKey: r.currentKprobeProgram.Name},
		GlobalData: r.currentKprobeProgram.Spec.GlobalData,
		MapOwnerId: mapOwnerId,
	}

	return &loadRequest, nil
}

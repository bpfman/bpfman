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
	"strconv"
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

//+kubebuilder:rbac:groups=bpfman.io,resources=uprobeprograms,verbs=get;list;watch

// BpfProgramReconciler reconciles a BpfProgram object
type UprobeProgramReconciler struct {
	ReconcilerCommon
	currentUprobeProgram *bpfmaniov1alpha1.UprobeProgram
	ourNode              *v1.Node
}

func (r *UprobeProgramReconciler) getFinalizer() string {
	return internal.UprobeProgramControllerFinalizer
}

func (r *UprobeProgramReconciler) getRecType() string {
	return internal.UprobeString
}

func (r *UprobeProgramReconciler) getProgType() internal.ProgramType {
	return internal.Kprobe
}

func (r *UprobeProgramReconciler) getName() string {
	return r.currentUprobeProgram.Name
}

func (r *UprobeProgramReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *UprobeProgramReconciler) getBpfProgramCommon() *bpfmaniov1alpha1.BpfProgramCommon {
	return &r.currentUprobeProgram.Spec.BpfProgramCommon
}

func (r *UprobeProgramReconciler) setCurrentProgram(program client.Object) error {
	var ok bool

	r.currentUprobeProgram, ok = program.(*bpfmaniov1alpha1.UprobeProgram)
	if !ok {
		return fmt.Errorf("failed to cast program to UprobeProgram")
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfman-Agent should reconcile whenever a UprobeProgram is updated,
// load the program to the node via bpfman, and then create a bpfProgram object
// to reflect per node state information.
func (r *UprobeProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.UprobeProgram{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		Owns(&bpfmaniov1alpha1.BpfProgram{},
			builder.WithPredicates(predicate.And(
				internal.BpfProgramTypePredicate(internal.UprobeString),
				internal.BpfProgramNodePredicate(r.NodeName)),
			),
		).
		// Trigger reconciliation if node labels change since that could make
		// the UprobeProgram no longer select the Node.  Trigger on pod events
		// for when uprobes are attached inside containers. In both cases, only
		// care about events specific to our node
		Watches(
			&source.Kind{Type: &v1.Node{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(predicate.LabelChangedPredicate{}, nodePredicate(r.NodeName))),
		).
		// Watch for changes in Pod resources in case we are using a container selector.
		Watches(
			&source.Kind{Type: &v1.Pod{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(podOnNodePredicate(r.NodeName)),
		).
		Complete(r)
}

// Figure out the list of container pids the uProbe needs to be attached to.
func (r *UprobeProgramReconciler) getUprobeContainerInfo(ctx context.Context) (*[]containerInfo, error) {

	clientSet, err := getClientset()
	if err != nil {
		return nil, fmt.Errorf("failed to get clientset: %v", err)
	}

	// Get the list of pods that match the selector.
	podList, err := getPodsForNode(ctx, clientSet, r.currentUprobeProgram.Spec.Containers, r.NodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod list: %v", err)
	}

	// Get the list of containers in the list of pods that match the selector.
	containers, err := getContainerInfo(podList, r.currentUprobeProgram.Spec.Containers.ContainerNames, r.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to get container info: %v", err)
	}

	r.Logger.V(1).Info("from getContainerInfo", "containers", containers)

	return containers, nil
}

func (r *UprobeProgramReconciler) getExpectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfProgramList, error) {
	progs := &bpfmaniov1alpha1.BpfProgramList{}

	// sanitize uprobe name to work in a bpfProgram name
	sanatizedUprobe := strings.Replace(strings.Replace(r.currentUprobeProgram.Spec.Target, "/", "-", -1), "_", "-", -1)
	bpfProgramNameBase := fmt.Sprintf("%s-%s-%s", r.currentUprobeProgram.Name, r.NodeName, sanatizedUprobe)

	if r.currentUprobeProgram.Spec.Containers != nil {

		// Some containers were specified, so we need to get the containers
		containerInfo, err := r.getUprobeContainerInfo(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get container pids: %v", err)
		}
		if containerInfo == nil || len(*containerInfo) == 0 {
			// There were no errors, but the container selector didn't
			// select any containers on this node.

			annotations := map[string]string{
				internal.UprobeProgramTarget:      r.currentUprobeProgram.Spec.Target,
				internal.UprobeNoContainersOnNode: "true",
			}

			bpfProgramName := fmt.Sprintf("%s-%s", bpfProgramNameBase, "no-containers-on-node")

			prog, err := r.createBpfProgram(bpfProgramName, r.getFinalizer(), r.currentUprobeProgram, r.getRecType(), annotations)
			if err != nil {
				return nil, fmt.Errorf("failed to create BpfProgram %s: %v", bpfProgramNameBase, err)
			}

			progs.Items = append(progs.Items, *prog)
		} else {

			// We got some containers, so create the bpfPrograms for each one.
			for i := range *containerInfo {
				container := (*containerInfo)[i]

				annotations := map[string]string{internal.UprobeProgramTarget: r.currentUprobeProgram.Spec.Target}
				annotations[internal.UprobeContainerPid] = strconv.FormatInt(container.pid, 10)

				bpfProgramName := fmt.Sprintf("%s-%s-%s", bpfProgramNameBase, container.podName, container.containerName)

				prog, err := r.createBpfProgram(bpfProgramName, r.getFinalizer(), r.currentUprobeProgram, r.getRecType(), annotations)
				if err != nil {
					return nil, fmt.Errorf("failed to create BpfProgram %s: %v", bpfProgramName, err)
				}

				progs.Items = append(progs.Items, *prog)
			}
		}
	} else {
		annotations := map[string]string{internal.UprobeProgramTarget: r.currentUprobeProgram.Spec.Target}

		prog, err := r.createBpfProgram(bpfProgramNameBase, r.getFinalizer(), r.currentUprobeProgram, r.getRecType(), annotations)
		if err != nil {
			return nil, fmt.Errorf("failed to create BpfProgram %s: %v", bpfProgramNameBase, err)
		}

		progs.Items = append(progs.Items, *prog)
	}

	return progs, nil
}

func (r *UprobeProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentUprobeProgram = &bpfmaniov1alpha1.UprobeProgram{}
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("uprobe")

	ctxLogger := log.FromContext(ctx)
	ctxLogger.Info("Reconcile Uprobe: Enter", "ReconcileKey", req)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	uprobePrograms := &bpfmaniov1alpha1.UprobeProgramList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, uprobePrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting UprobePrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(uprobePrograms.Items) == 0 {
		r.Logger.Info("UprobeProgramController found no Uprobe Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	// Create a list of Uprobe programs to pass into reconcileCommon()
	var uprobeObjects []client.Object = make([]client.Object, len(uprobePrograms.Items))
	for i := range uprobePrograms.Items {
		uprobeObjects[i] = &uprobePrograms.Items[i]
	}

	// Reconcile each TcProgram.
	return r.reconcileCommon(ctx, r, uprobeObjects)
}

func (r *UprobeProgramReconciler) getLoadRequest(bpfProgram *bpfmaniov1alpha1.BpfProgram, mapOwnerId *uint32) (*gobpfman.LoadRequest, error) {
	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.currentUprobeProgram.Spec.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	var uprobeAttachInfo *gobpfman.UprobeAttachInfo

	var containerPid int32
	hasContainerPid := false

	containerPidStr, ok := bpfProgram.Annotations[internal.UprobeContainerPid]

	if ok {
		containerPidInt64, err := strconv.ParseInt(containerPidStr, 10, 32)
		if err != nil {
			r.Logger.Error(err, "ParseInt() error on containerPidStr", "containerPidStr", containerPidStr)
		} else {
			containerPid = int32(containerPidInt64)
			hasContainerPid = true
		}
	}

	uprobeAttachInfo = &gobpfman.UprobeAttachInfo{
		FnName:   &r.currentUprobeProgram.Spec.FunctionName,
		Offset:   r.currentUprobeProgram.Spec.Offset,
		Target:   bpfProgram.Annotations[internal.UprobeProgramTarget],
		Retprobe: r.currentUprobeProgram.Spec.RetProbe,
	}

	if hasContainerPid {
		uprobeAttachInfo.ContainerPid = &containerPid
	}

	loadRequest := gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentUprobeProgram.Spec.BpfFunctionName,
		ProgramType: uint32(internal.Kprobe),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_UprobeAttachInfo{
				UprobeAttachInfo: uprobeAttachInfo,
			},
		},
		Metadata:   map[string]string{internal.UuidMetadataKey: string(bpfProgram.UID), internal.ProgramNameKey: r.currentUprobeProgram.Name},
		GlobalData: r.currentUprobeProgram.Spec.GlobalData,
		MapOwnerId: mapOwnerId,
	}

	return &loadRequest, nil
}

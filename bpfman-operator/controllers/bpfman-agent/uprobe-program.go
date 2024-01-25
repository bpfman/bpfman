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

func (r *UprobeProgramReconciler) getRecCommon() *ReconcilerCommon {
	return &r.ReconcilerCommon
}

func (r *UprobeProgramReconciler) getFinalizer() string {
	return internal.UprobeProgramControllerFinalizer
}

func (r *UprobeProgramReconciler) getRecType() string {
	return internal.UprobeString
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
			builder.WithPredicates(PodOnNodePredicate{NodeName: r.NodeName}),
		).
		Complete(r)
}

// Figure out the list of container pids the uProbe needs to be attached to.
func (r *UprobeProgramReconciler) getUprobeContainerInfo(ctx context.Context) (*[]uprobeContainerInfo, error) {

	clientSet, err := getClientset()
	if err != nil {
		return nil, fmt.Errorf("failed to get clientset: %v", err)
	}

	// Get the list of pods that match the selector.
	podList, err := getPods(ctx, clientSet, r.currentUprobeProgram.Spec.Containers, r.NodeName)
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

func (r *UprobeProgramReconciler) expectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfProgramList, error) {
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

			prog, err := r.createBpfProgram(ctx, bpfProgramName, r.getFinalizer(), r.currentUprobeProgram, r.getRecType(), annotations)
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

				prog, err := r.createBpfProgram(ctx, bpfProgramName, r.getFinalizer(), r.currentUprobeProgram, r.getRecType(), annotations)
				if err != nil {
					return nil, fmt.Errorf("failed to create BpfProgram %s: %v", bpfProgramName, err)
				}

				progs.Items = append(progs.Items, *prog)
			}
		}
	} else {
		annotations := map[string]string{internal.UprobeProgramTarget: r.currentUprobeProgram.Spec.Target}

		prog, err := r.createBpfProgram(ctx, bpfProgramNameBase, r.getFinalizer(), r.currentUprobeProgram, r.getRecType(), annotations)
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

	// Get existing ebpf state from bpfman. Since both uprobes and kprobes have
	// the same kernel ProgramType, we use internal.Kprobe below.
	programMap, err := bpfmanagentinternal.ListBpfmanPrograms(ctx, r.BpfmanClient, internal.Kprobe)
	if err != nil {
		r.Logger.Error(err, "failed to list loaded bpfman programs")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
	}

	// Reconcile each UprobeProgram. Don't return error here because it will trigger an infinite reconcile loop, instead
	// report the error to user and retry if specified. For some errors the controller may not decide to retry.
	// Note: This only results in grpc calls to bpfman if we need to change something
	requeue := false // initialize requeue to false
	for _, uprobeProgram := range uprobePrograms.Items {
		r.Logger.Info("UprobeProgramController is reconciling", "currentUprobeProgram", uprobeProgram.Name)
		r.currentUprobeProgram = &uprobeProgram
		result, err := reconcileProgram(ctx, r, r.currentUprobeProgram, &r.currentUprobeProgram.Spec.BpfProgramCommon, r.ourNode, programMap)
		if err != nil {
			r.Logger.Error(err, "Reconciling UprobeProgram Failed", "UprobeProgramName", r.currentUprobeProgram.Name, "ReconcileResult", result.String())
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

func (r *UprobeProgramReconciler) buildUprobeLoadRequest(
	bytecode *gobpfman.BytecodeLocation,
	uuid string,
	bpfProgram *bpfmaniov1alpha1.BpfProgram,
	mapOwnerId *uint32) *gobpfman.LoadRequest {

	var uprobeAttachInfo *gobpfman.UprobeAttachInfo

	var containerPid int32
	hasContainerPid := false

	containerPidStr, ok := bpfProgram.Annotations[internal.UprobeContainerPid]

	if ok {
		containerPidInt64, err := strconv.ParseInt(containerPidStr, 10, 32)
		if err != nil {
			r.Logger.Error(err, "ParseInt() error on containerPidStr", containerPidStr)
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

	return &gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentUprobeProgram.Spec.BpfFunctionName,
		ProgramType: uint32(internal.Kprobe),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_UprobeAttachInfo{
				UprobeAttachInfo: uprobeAttachInfo,
			},
		},
		Metadata:   map[string]string{internal.UuidMetadataKey: uuid, internal.ProgramNameKey: r.currentUprobeProgram.Name},
		GlobalData: r.currentUprobeProgram.Spec.GlobalData,
		MapOwnerId: mapOwnerId,
	}
}

// reconcileBpfmanPrograms ONLY reconciles the bpfman state for a single BpfProgram.
// It does not interact with the k8s API in any way.
func (r *UprobeProgramReconciler) reconcileBpfmanProgram(ctx context.Context,
	existingBpfPrograms map[string]*gobpfman.ListResponse_ListResult,
	bytecodeSelector *bpfmaniov1alpha1.BytecodeSelector,
	bpfProgram *bpfmaniov1alpha1.BpfProgram,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus) (bpfmaniov1alpha1.BpfProgramConditionType, error) {

	r.Logger.V(1).Info("Existing bpfProgram", "UUID", bpfProgram.UID, "Name", bpfProgram.Name, "CurrentUprobeProgram", r.currentUprobeProgram.Name)

	uuid := bpfProgram.UID

	getLoadRequest := func() (*gobpfman.LoadRequest, bpfmaniov1alpha1.BpfProgramConditionType, error) {
		bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, bytecodeSelector)
		if err != nil {
			return nil, bpfmaniov1alpha1.BpfProgCondBytecodeSelectorError, fmt.Errorf("failed to process bytecode selector: %v", err)
		}
		loadRequest := r.buildUprobeLoadRequest(bytecode, string(uuid), bpfProgram, mapOwnerStatus.mapOwnerId)
		return loadRequest, bpfmaniov1alpha1.BpfProgCondNone, nil
	}

	noContainers := noContainersOnNode(bpfProgram)

	existingProgram, doesProgramExist := existingBpfPrograms[string(uuid)]
	if !doesProgramExist {
		r.Logger.V(1).Info("UprobeProgram doesn't exist on node")

		// If UprobeProgram is being deleted just exit
		if isBeingDeleted {
			return bpfmaniov1alpha1.BpfProgCondUnloaded, nil
		}

		// Make sure if we're not selected just exit
		if !isNodeSelected {
			return bpfmaniov1alpha1.BpfProgCondNotSelected, nil
		}

		// If a container selector is present but there were no matching
		// containers on this node, just exit.
		if noContainers {
			r.Logger.V(1).Info("Program does not exist and there are no matching containers on this node")
			return bpfmaniov1alpha1.BpfProgCondNoContainersOnNode, nil
		}

		// Make sure if the Map Owner is set but not found then just exit
		if mapOwnerStatus.isSet && !mapOwnerStatus.isFound {
			return bpfmaniov1alpha1.BpfProgCondMapOwnerNotFound, nil
		}

		// Make sure if the Map Owner is set but not loaded then just exit
		if mapOwnerStatus.isSet && !mapOwnerStatus.isLoaded {
			return bpfmaniov1alpha1.BpfProgCondMapOwnerNotLoaded, nil
		}

		// otherwise load it
		loadRequest, condition, err := getLoadRequest()
		if err != nil {
			return condition, err
		}

		r.progId, err = bpfmanagentinternal.LoadBpfmanProgram(ctx, r.BpfmanClient, loadRequest)
		if err != nil {
			r.Logger.Error(err, "Failed to load UprobeProgram")
			return bpfmaniov1alpha1.BpfProgCondNotLoaded, nil
		}

		r.Logger.Info("bpfman called to load UprobeProgram on Node", "Name", bpfProgram.Name, "UUID", uuid)
		return bpfmaniov1alpha1.BpfProgCondLoaded, nil
	}

	// prog ID should already have been set if program exists
	id, err := bpfmanagentinternal.GetID(bpfProgram)
	if err != nil {
		r.Logger.Error(err, "Failed to get program ID")
		return bpfmaniov1alpha1.BpfProgCondNotLoaded, nil
	}

	// BpfProgram exists but either UprobeProgram is being deleted, node is no
	// longer selected, or map is not available....unload program
	if isBeingDeleted || !isNodeSelected || noContainers ||
		(mapOwnerStatus.isSet && (!mapOwnerStatus.isFound || !mapOwnerStatus.isLoaded)) {
		r.Logger.V(1).Info("UprobeProgram exists on Node but is scheduled for deletion, not selected, or map not available",
			"isDeleted", isBeingDeleted, "isSelected", isNodeSelected, "mapIsSet", mapOwnerStatus.isSet,
			"mapIsFound", mapOwnerStatus.isFound, "mapIsLoaded", mapOwnerStatus.isLoaded)

		if err := bpfmanagentinternal.UnloadBpfmanProgram(ctx, r.BpfmanClient, *id); err != nil {
			r.Logger.Error(err, "Failed to unload UprobeProgram")
			return bpfmaniov1alpha1.BpfProgCondNotUnloaded, nil
		}

		r.Logger.Info("bpfman called to unload UprobeProgram on Node", "Name", bpfProgram.Name, "Program ID", id)

		if isBeingDeleted {
			return bpfmaniov1alpha1.BpfProgCondUnloaded, nil
		}

		if !isNodeSelected {
			return bpfmaniov1alpha1.BpfProgCondNotSelected, nil
		}

		if mapOwnerStatus.isSet && !mapOwnerStatus.isFound {
			return bpfmaniov1alpha1.BpfProgCondMapOwnerNotFound, nil
		}

		if mapOwnerStatus.isSet && !mapOwnerStatus.isLoaded {
			return bpfmaniov1alpha1.BpfProgCondMapOwnerNotLoaded, nil
		}
	}

	// BpfProgram exists but is not correct state, unload and recreate
	loadRequest, condition, err := getLoadRequest()
	if err != nil {
		return condition, err
	}

	r.Logger.V(1).WithValues("expectedProgram", loadRequest).WithValues("existingProgram", existingProgram).Info("StateMatch")

	isSame, reasons := bpfmanagentinternal.DoesProgExist(existingProgram, loadRequest)
	if !isSame {
		r.Logger.V(1).Info("UprobeProgram is in wrong state, unloading and reloading", "Reason", reasons)
		if err := bpfmanagentinternal.UnloadBpfmanProgram(ctx, r.BpfmanClient, *id); err != nil {
			r.Logger.Error(err, "Failed to unload UprobeProgram")
			return bpfmaniov1alpha1.BpfProgCondNotUnloaded, nil
		}

		r.progId, err = bpfmanagentinternal.LoadBpfmanProgram(ctx, r.BpfmanClient, loadRequest)
		if err != nil {
			r.Logger.Error(err, "Failed to load UprobeProgram")
			return bpfmaniov1alpha1.BpfProgCondNotLoaded, err
		}

		r.Logger.Info("bpfman called to reload UprobeProgram on Node", "Name", bpfProgram.Name, "UUID", uuid)
	} else {
		// Program exists and bpfProgram K8s Object is up to date
		r.Logger.V(1).Info("Ignoring Object Change nothing to do in bpfman")
		r.progId = id
	}

	return bpfmaniov1alpha1.BpfProgCondLoaded, nil
}

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
	"reflect"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanagentinternal "github.com/bpfman/bpfman/bpfman-operator/controllers/bpfman-agent/internal"
	"github.com/bpfman/bpfman/bpfman-operator/internal"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	"github.com/go-logr/logr"
	"google.golang.org/grpc"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=bpfprograms,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfprograms/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=tcprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=xdpprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=tracepointprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=kprobeprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=uprobeprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=fentryprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=fexityprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get

const (
	retryDurationAgent = 5 * time.Second
)

// ReconcilerCommon provides a skeleton for all *Program Reconcilers.
type ReconcilerCommon struct {
	client.Client
	Scheme       *runtime.Scheme
	GrpcConn     *grpc.ClientConn
	BpfmanClient gobpfman.BpfmanClient
	Logger       logr.Logger
	NodeName     string
	progId       *uint32
}

// bpfmanReconciler defines a generic bpfProgram K8s object reconciler which can
// program bpfman from user intent in the K8s CRDs.
type bpfmanReconciler interface {
	// SetupWithManager registers the reconciler with the manager and defines
	// which kubernetes events will trigger a reconcile.
	SetupWithManager(mgr ctrl.Manager) error
	// Reconcile is the main entry point to the reconciler. It will be called by
	// the controller runtime when something happens that the reconciler is
	// interested in. When Reconcile is invoked, it initializes some state in
	// the given bpfmanReconciler, retrieves a list of all programs of the given
	// type, and then calls reconcileCommon.
	Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
	// getFinalizer returns the string used for the finalizer to prevent the
	// BpfProgram object from deletion until cleanup can be performed
	getFinalizer() string
	// getRecType returns the type of the reconciler.  This is often the string
	// representation of the ProgramType, but in cases where there are multiple
	// reconcilers for a single ProgramType, it may be different (e.g., uprobe,
	// fentry, and fexit)
	getRecType() string
	// getProgType returns the ProgramType used by bpfman for the bpfPrograms
	// the reconciler manages.
	getProgType() internal.ProgramType
	// getName returns the name of the current program being reconciled.
	getName() string
	// getExpectedBpfPrograms returns the list of BpfPrograms that are expected
	// to be loaded on the current node.
	getExpectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfProgramList, error)
	// getLoadRequest returns the LoadRequest that should be sent to bpfman to
	// load the given BpfProgram.
	getLoadRequest(bpfProgram *bpfmaniov1alpha1.BpfProgram, mapOwnerId *uint32) (*gobpfman.LoadRequest, error)
	// getNode returns node object for the current node.
	getNode() *v1.Node
	// getBpfProgramCommon returns the BpfProgramCommon object for the current
	// Program being reconciled.
	getBpfProgramCommon() *bpfmaniov1alpha1.BpfProgramCommon
	// setCurrentProgram sets the current *Program for the reconciler as well as
	// any other related state needed.
	setCurrentProgram(program client.Object) error
}

// reconcileCommon is the common reconciler loop called by each bpfman
// reconciler.  It reconciles each program in the list.  reconcileCommon should
// not return error because it will trigger an infinite reconcile loop.
// Instead, it should report the error to user and retry if specified. For some
// errors the controller may decide not to retry. Note: This only results in
// calls to bpfman if we need to change something
func (r *ReconcilerCommon) reconcileCommon(ctx context.Context, rec bpfmanReconciler,
	programs []client.Object) (ctrl.Result, error) {

	r.Logger.V(1).Info("Start reconcileCommon()")

	// Get existing ebpf state from bpfman.
	loadedBpfPrograms, err := bpfmanagentinternal.ListBpfmanPrograms(ctx, r.BpfmanClient, rec.getProgType())
	if err != nil {
		r.Logger.Error(err, "failed to list loaded bpfman programs")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
	}

	requeue := false // initialize requeue to false
	for _, program := range programs {
		r.Logger.V(1).Info("Reconciling program", "Name", program.GetName())

		// Save the *Program CRD of the current program being reconciled
		err := rec.setCurrentProgram(program)
		if err != nil {
			r.Logger.Error(err, "Failed to set current program")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
		}

		result, err := r.reconcileProgram(ctx, rec, program, loadedBpfPrograms)
		if err != nil {
			r.Logger.Error(err, "Reconciling program failed", "Program Name", rec.getName, "ReconcileResult", result.String())
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

// reconcileBpfmanPrograms ONLY reconciles the bpfman state for a single BpfProgram.
// It does not interact with the k8s API in any way.
func (r *ReconcilerCommon) reconcileBpfProgram(ctx context.Context,
	rec bpfmanReconciler,
	loadedBpfPrograms map[string]*gobpfman.ListResponse_ListResult,
	bpfProgram *bpfmaniov1alpha1.BpfProgram,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus) (bpfmaniov1alpha1.BpfProgramConditionType, error) {

	r.Logger.V(1).Info("enter reconcileBpfmanProgram()", "bpfProgram", bpfProgram.Name, "CurrentProgram", rec.getName())

	uuid := bpfProgram.UID
	noContainersOnNode := noContainersOnNode(bpfProgram)
	loadedBpfProgram, isLoaded := loadedBpfPrograms[string(uuid)]
	shouldBeLoaded := bpfProgramShouldBeLoaded(isNodeSelected, isBeingDeleted, noContainersOnNode, mapOwnerStatus)

	r.Logger.V(1).Info("reconcileBpfmanProgram()", "shouldBeLoaded", shouldBeLoaded, "isLoaded", isLoaded)

	switch isLoaded {
	case true:
		// prog ID should already have been set if program is loaded
		id, err := bpfmanagentinternal.GetID(bpfProgram)
		if err != nil {
			r.Logger.Error(err, "Failed to get bpf program ID")
			return bpfmaniov1alpha1.BpfProgCondNotLoaded, nil
		}
		switch shouldBeLoaded {
		case true:
			// The program is loaded and it should be loaded.
			// Confirm it's in the correct state.
			loadRequest, err := rec.getLoadRequest(bpfProgram, mapOwnerStatus.mapOwnerId)
			if err != nil {
				return bpfmaniov1alpha1.BpfProgCondBytecodeSelectorError, err
			}

			r.Logger.V(1).WithValues("loadRequest", loadRequest).WithValues("loadedBpfProgram", loadedBpfProgram).Info("StateMatch")

			isSame, reasons := bpfmanagentinternal.DoesProgExist(loadedBpfProgram, loadRequest)
			if !isSame {
				r.Logger.V(1).Info("bpf program is in wrong state, unloading and reloading", "reason", reasons, "bpfProgram Name", bpfProgram.Name, "bpf program ID", id)
				if err := bpfmanagentinternal.UnloadBpfmanProgram(ctx, r.BpfmanClient, *id); err != nil {
					r.Logger.Error(err, "Failed to unload BPF Program")
					return bpfmaniov1alpha1.BpfProgCondNotUnloaded, nil
				}

				r.Logger.Info("Calling bpfman to load bpf program on Node", "bpfProgram Name", bpfProgram.Name)
				r.progId, err = bpfmanagentinternal.LoadBpfmanProgram(ctx, r.BpfmanClient, loadRequest)
				if err != nil {
					r.Logger.Error(err, "Failed to load bpf program")
					return bpfmaniov1alpha1.BpfProgCondNotLoaded, err
				}
			} else {
				// Program exists and bpfProgram K8s Object is up to date
				r.Logger.V(1).Info("Program is in correct state.  Nothing to do in bpfman")
				r.progId = id
			}
		case false:
			// The program is loaded but it shouldn't be loaded.
			r.Logger.Info("Calling bpfman to unload program on node", "bpfProgram Name", bpfProgram.Name, "Program ID", id)
			if err := bpfmanagentinternal.UnloadBpfmanProgram(ctx, r.BpfmanClient, *id); err != nil {
				r.Logger.Error(err, "Failed to unload Program")
				return bpfmaniov1alpha1.BpfProgCondNotUnloaded, nil
			}
		}
	case false:
		switch shouldBeLoaded {
		case true:
			// The program isn't loaded but it should be loaded.
			loadRequest, err := rec.getLoadRequest(bpfProgram, mapOwnerStatus.mapOwnerId)
			if err != nil {
				return bpfmaniov1alpha1.BpfProgCondBytecodeSelectorError, err
			}

			r.Logger.Info("Calling bpfman to load program on node", "bpfProgram name", bpfProgram.Name)
			r.progId, err = bpfmanagentinternal.LoadBpfmanProgram(ctx, r.BpfmanClient, loadRequest)
			if err != nil {
				r.Logger.Error(err, "Failed to load Program")
				return bpfmaniov1alpha1.BpfProgCondNotLoaded, nil
			}
		case false:
			// The program isn't loaded and it shouldn't be loaded.
		}
	}

	// The BPF program was sucessfully reconciled.
	return r.reconcileBpfProgramSuccessCondition(
		isLoaded,
		shouldBeLoaded,
		isNodeSelected,
		isBeingDeleted,
		noContainersOnNode,
		mapOwnerStatus), nil
}

// reconcileBpfProgramSuccessCondition returns the proper condition for a
// successful reconcile of a bpfProgram based on the given parameters.
func (r *ReconcilerCommon) reconcileBpfProgramSuccessCondition(
	isLoaded bool,
	shouldBeLoaded bool,
	isNodeSelected bool,
	isBeingDeleted bool,
	noContainersOnNode bool,
	mapOwnerStatus *MapOwnerParamStatus) bpfmaniov1alpha1.BpfProgramConditionType {

	switch isLoaded {
	case true:
		switch shouldBeLoaded {
		case true:
			// The program is loaded and it should be loaded.
			return bpfmaniov1alpha1.BpfProgCondLoaded
		case false:
			// The program is loaded but it shouldn't be loaded.
			if isBeingDeleted {
				return bpfmaniov1alpha1.BpfProgCondUnloaded
			}
			if !isNodeSelected {
				return bpfmaniov1alpha1.BpfProgCondNotSelected
			}
			if noContainersOnNode {
				return bpfmaniov1alpha1.BpfProgCondNoContainersOnNode
			}
			if mapOwnerStatus.isSet && !mapOwnerStatus.isFound {
				return bpfmaniov1alpha1.BpfProgCondMapOwnerNotFound
			}
			if mapOwnerStatus.isSet && !mapOwnerStatus.isLoaded {
				return bpfmaniov1alpha1.BpfProgCondMapOwnerNotLoaded
			}
			// If we get here, there's a problem.  All of the possible reasons
			// that a program should not be loaded should have been handled
			// above.
			r.Logger.Error(nil, "unhandled case in isLoaded && !shouldBeLoaded")
			return bpfmaniov1alpha1.BpfProgCondUnloaded
		}
	case false:
		switch shouldBeLoaded {
		case true:
			// The program isn't loaded but it should be loaded.
			return bpfmaniov1alpha1.BpfProgCondLoaded
		case false:
			// The program isn't loaded and it shouldn't be loaded.
			if isBeingDeleted {
				return bpfmaniov1alpha1.BpfProgCondUnloaded
			}
			if !isNodeSelected {
				return bpfmaniov1alpha1.BpfProgCondNotSelected
			}
			if noContainersOnNode {
				return bpfmaniov1alpha1.BpfProgCondNoContainersOnNode
			}
			if mapOwnerStatus.isSet && !mapOwnerStatus.isFound {
				return bpfmaniov1alpha1.BpfProgCondMapOwnerNotFound
			}
			if mapOwnerStatus.isSet && !mapOwnerStatus.isLoaded {
				return bpfmaniov1alpha1.BpfProgCondMapOwnerNotLoaded
			}
			// If we get here, there's a problem.  All of the possible reasons
			// that a program should not be loaded should have been handled
			// above.
			r.Logger.Error(nil, "unhandled case in !isLoaded && !shouldBeLoaded")
			return bpfmaniov1alpha1.BpfProgCondUnloaded
		}
	}

	// We should never get here, but need this return to satisfy the compiler.
	r.Logger.Error(nil, "unhandled case in reconcileBpfProgramSuccessCondition()")
	return bpfmaniov1alpha1.BpfProgCondNone
}

func bpfProgramShouldBeLoaded(
	isNodeSelected bool,
	isBeingDeleted bool,
	noContainersOnNode bool,
	mapOwnerStatus *MapOwnerParamStatus) bool {
	return isNodeSelected && !isBeingDeleted && !noContainersOnNode && mapOk(mapOwnerStatus)
}

func mapOk(mapOwnerStatus *MapOwnerParamStatus) bool {
	return !mapOwnerStatus.isSet || (mapOwnerStatus.isSet && mapOwnerStatus.isFound && mapOwnerStatus.isLoaded)
}

// Only return node updates for our node (all events)
func nodePredicate(nodeName string) predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetLabels()["kubernetes.io/hostname"] == nodeName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetLabels()["kubernetes.io/hostname"] == nodeName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetLabels()["kubernetes.io/hostname"] == nodeName
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetLabels()["kubernetes.io/hostname"] == nodeName
		},
	}
}

// Predicate to watch for Pod events on a specific node which checks if the
// event's Pod is scheduled on the given node.
func podOnNodePredicate(nodeName string) predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			pod, ok := e.Object.(*v1.Pod)
			return ok && pod.Spec.NodeName == nodeName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			pod, ok := e.Object.(*v1.Pod)
			return ok && pod.Spec.NodeName == nodeName && pod.Status.Phase == v1.PodRunning
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			pod, ok := e.ObjectNew.(*v1.Pod)
			return ok && pod.Spec.NodeName == nodeName && pod.Status.Phase == v1.PodRunning
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			pod, ok := e.Object.(*v1.Pod)
			return ok && pod.Spec.NodeName == nodeName
		},
	}
}

func isNodeSelected(selector *metav1.LabelSelector, nodeLabels map[string]string) (bool, error) {
	// Logic to check if this node is selected by the *Program object
	selectorTool, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, fmt.Errorf("failed to parse nodeSelector: %v",
			err)
	}

	nodeLabelSet, err := labels.ConvertSelectorToLabelsMap(labels.FormatLabels(nodeLabels))
	if err != nil {
		return false, fmt.Errorf("failed to parse node labels : %v",
			err)
	}

	return selectorTool.Matches(nodeLabelSet), nil
}

func getInterfaces(interfaceSelector *bpfmaniov1alpha1.InterfaceSelector, ourNode *v1.Node) ([]string, error) {
	var interfaces []string

	if interfaceSelector.Interfaces != nil {
		return *interfaceSelector.Interfaces, nil
	}

	if interfaceSelector.PrimaryNodeInterface != nil {
		nodeIface, err := bpfmanagentinternal.GetPrimaryNodeInterface(ourNode)
		if err != nil {
			return nil, err
		}

		interfaces = append(interfaces, nodeIface)
		return interfaces, nil
	}

	return nil, fmt.Errorf("no interfaces selected")
}

// removeFinalizer removes the finalizer from the BpfProgram object if is applied,
// returning if the action resulted in a kube API update or not along with any
// errors.
func (r *ReconcilerCommon) removeFinalizer(ctx context.Context, o client.Object, finalizer string) bool {
	changed := controllerutil.RemoveFinalizer(o, finalizer)
	if changed {
		r.Logger.Info("Removing finalizer from bpfProgram", "object name", o.GetName())
		err := r.Update(ctx, o)
		if err != nil {
			r.Logger.Error(err, "failed to remove bpfProgram Finalizer")
			return true
		}
	}

	return changed
}

// updateStatus updates the status of a BpfProgram object if needed, returning
// false if the status was already set for the given bpfProgram, meaning reconciliation
// may continue.
func (r *ReconcilerCommon) updateStatus(ctx context.Context, bpfProgram *bpfmaniov1alpha1.BpfProgram, cond bpfmaniov1alpha1.BpfProgramConditionType) bool {

	r.Logger.V(1).Info("updateStatus()", "existing conds", bpfProgram.Status.Conditions, "new cond", cond)

	if bpfProgram.Status.Conditions != nil {
		numConditions := len(bpfProgram.Status.Conditions)

		if numConditions == 1 {
			if bpfProgram.Status.Conditions[0].Type == string(cond) {
				// No change, so just return false -- not updated
				return false
			} else {
				// We're changing the condition, so delete this one.  The
				// new condition will be added below.
				bpfProgram.Status.Conditions = nil
			}
		} else if numConditions > 1 {
			// We should only ever have one condition, so we shouldn't hit this
			// case.  However, if we do, log a message, delete the existing
			// conditions, and add the new one below.
			r.Logger.Info("more than one BpfProgramCondition", "numConditions", numConditions)
			bpfProgram.Status.Conditions = nil
		}
		// if numConditions == 0, just add the new condition below.
	}

	meta.SetStatusCondition(&bpfProgram.Status.Conditions, cond.Condition())

	r.Logger.V(1).Info("Updating bpfProgram condition", "bpfProgram", bpfProgram.Name, "condition", cond.Condition().Type)
	if err := r.Status().Update(ctx, bpfProgram); err != nil {
		r.Logger.Error(err, "failed to set bpfProgram object status")
	}

	r.Logger.V(1).Info("condition updated", "new condition", cond)
	return true
}

func (r *ReconcilerCommon) getExistingBpfPrograms(ctx context.Context,
	program metav1.Object) (map[string]bpfmaniov1alpha1.BpfProgram, error) {

	bpfProgramList := &bpfmaniov1alpha1.BpfProgramList{}

	// Only list bpfPrograms for this *Program and the controller's node
	opts := []client.ListOption{
		client.MatchingLabels{internal.BpfProgramOwnerLabel: program.GetName(), internal.K8sHostLabel: r.NodeName},
	}

	err := r.List(ctx, bpfProgramList, opts...)
	if err != nil {
		return nil, err
	}

	existingBpfPrograms := map[string]bpfmaniov1alpha1.BpfProgram{}
	for _, bpfProg := range bpfProgramList.Items {
		existingBpfPrograms[bpfProg.GetName()] = bpfProg
	}

	return existingBpfPrograms, nil
}

// createBpfProgram moves some shared logic for building bpfProgram objects
// into a central location.
func (r *ReconcilerCommon) createBpfProgram(
	bpfProgramName string,
	finalizer string,
	owner metav1.Object,
	ownerType string,
	annotations map[string]string) (*bpfmaniov1alpha1.BpfProgram, error) {
	bpfProg := &bpfmaniov1alpha1.BpfProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name:       bpfProgramName,
			Finalizers: []string{finalizer},
			Labels: map[string]string{internal.BpfProgramOwnerLabel: owner.GetName(),
				internal.K8sHostLabel: r.NodeName},
			Annotations: annotations,
		},
		Spec: bpfmaniov1alpha1.BpfProgramSpec{
			Type: ownerType,
		},
		Status: bpfmaniov1alpha1.BpfProgramStatus{Conditions: []metav1.Condition{}},
	}

	// Make the corresponding BpfProgramConfig the owner
	if err := ctrl.SetControllerReference(owner, bpfProg, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to bpfProgram object owner reference: %v", err)
	}

	return bpfProg, nil
}

// Programs may be deleted for one of two reasons.  The first is that the global
// *Program object is being deleted.  The second is that the something has
// changed on the node that is causing the need to remove individual
// bpfPrograms. Typically this happens when containers that used to match a
// container selector are deleted and the eBPF programs that were installed in
// them need to be removed.  This function handles both of these cases.
//
// For the first case, deletion of a *Program takes a few steps if there are
// existing bpfPrograms:
//  1. Reconcile the bpfProgram (take bpfman cleanup steps).
//  2. Remove any finalizers from the bpfProgram Object.
//  3. Update the condition on the bpfProgram to BpfProgCondUnloaded so the
//     operator knows it's safe to remove the parent Program Object, which
//     is when the bpfProgram is automatically deleted by the owner-reference.
//
// For the second case, we need to do the first 2 steps, and then explicitly
// delete the bpfPrograms that are no longer needed.
func (r *ReconcilerCommon) handleProgDelete(
	ctx context.Context,
	rec bpfmanReconciler,
	existingBpfPrograms map[string]bpfmaniov1alpha1.BpfProgram,
	loadedBpfPrograms map[string]*gobpfman.ListResponse_ListResult,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus,
) (internal.ReconcileResult, error) {
	r.Logger.V(1).Info("handleProgDelete()", "isBeingDeleted", isBeingDeleted, "isNodeSelected",
		isNodeSelected, "mapOwnerStatus", mapOwnerStatus)
	for _, bpfProgram := range existingBpfPrograms {
		r.Logger.V(1).Info("Deleting bpfProgram", "Name", bpfProgram.Name)
		// Reconcile the bpfProgram if error write condition and exit with
		// retry.
		cond, err := r.reconcileBpfProgram(ctx,
			rec,
			loadedBpfPrograms,
			&bpfProgram,
			isNodeSelected,
			true, // delete program
			mapOwnerStatus,
		)
		if err != nil {
			r.updateStatus(ctx, &bpfProgram, cond)
			return internal.Requeue, fmt.Errorf("failed to delete bpfman program: %v", err)
		}

		if r.removeFinalizer(ctx, &bpfProgram, rec.getFinalizer()) {
			return internal.Updated, nil
		}

		if isBeingDeleted {
			// We're deleting these programs because the *Program is being
			// deleted, so update the status and the program will be deleted
			// when the owner is deleted.
			if r.updateStatus(ctx, &bpfProgram, cond) {
				return internal.Updated, nil
			}
		} else {
			// We're deleting these programs because they were not expected due
			// to changes that caused the containers to not be selected anymore.
			// So, explicitly delete them.
			opts := client.DeleteOptions{}
			r.Logger.Info("Deleting bpfProgram", "Name", bpfProgram.Name, "Owner", bpfProgram.GetName())
			if err := r.Delete(ctx, &bpfProgram, &opts); err != nil {
				return internal.Requeue, fmt.Errorf("failed to create bpfProgram object: %v", err)
			}
			return internal.Updated, nil
		}
	}

	// We're done reconciling.
	r.Logger.Info("Finished reconciling", "program name", rec.getName())
	return internal.Unchanged, nil
}

// handleProgCreateOrUpdate compares the expected bpfPrograms to the existing
// bpfPrograms.  If a bpfProgram is expected but doesn't exist, it is created.
// If an expected bpfProgram exists, it is reconciled. If a bpfProgram exists
// but is not expected, it is deleted.
func (r *ReconcilerCommon) handleProgCreateOrUpdate(
	ctx context.Context,
	rec bpfmanReconciler,
	program client.Object,
	existingBpfPrograms map[string]bpfmaniov1alpha1.BpfProgram,
	expectedBpfPrograms *bpfmaniov1alpha1.BpfProgramList,
	loadedBpfPrograms map[string]*gobpfman.ListResponse_ListResult,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus,
) (internal.ReconcileResult, error) {
	r.Logger.V(1).Info("handleProgCreateOrUpdate()", "isBeingDeleted", isBeingDeleted, "isNodeSelected",
		isNodeSelected, "mapOwnerStatus", mapOwnerStatus)
	// If the *Program isn't being deleted ALWAYS create the bpfPrograms
	// even if the node isn't selected
	for _, expectedBpfProgram := range expectedBpfPrograms.Items {
		r.Logger.V(1).Info("Creating or Updating", "Name", expectedBpfProgram.Name)
		existingBpfProgram, exists := existingBpfPrograms[expectedBpfProgram.Name]
		if exists {
			// Remove the bpfProgram from the existingPrograms map so we know
			// not to delete it below.
			delete(existingBpfPrograms, expectedBpfProgram.Name)
		} else {
			// Create a new bpfProgram Object for this program.
			opts := client.CreateOptions{}
			r.Logger.Info("Creating bpfProgram", "Name", expectedBpfProgram.Name, "Owner", program.GetName())
			if err := r.Create(ctx, &expectedBpfProgram, &opts); err != nil {
				return internal.Requeue, fmt.Errorf("failed to create bpfProgram object: %v", err)
			}
			return internal.Updated, nil
		}

		// bpfProgram Object exists go ahead and reconcile it, if there is
		// an error write condition and exit with retry.
		cond, err := r.reconcileBpfProgram(ctx,
			rec,
			loadedBpfPrograms,
			&existingBpfProgram,
			isNodeSelected,
			isBeingDeleted,
			mapOwnerStatus,
		)
		if err != nil {
			if r.updateStatus(ctx, &existingBpfProgram, cond) {
				// Return an error the first time.
				return internal.Updated, fmt.Errorf("failed to reconcile bpfman program: %v", err)
			}
		} else {
			// Make sure if we're not selected exit and write correct condition
			if cond == bpfmaniov1alpha1.BpfProgCondNotSelected ||
				cond == bpfmaniov1alpha1.BpfProgCondMapOwnerNotFound ||
				cond == bpfmaniov1alpha1.BpfProgCondMapOwnerNotLoaded ||
				cond == bpfmaniov1alpha1.BpfProgCondNoContainersOnNode {
				// Write NodeNodeSelected status
				if r.updateStatus(ctx, &existingBpfProgram, cond) {
					r.Logger.V(1).Info("Update condition from bpfman reconcile", "condition", cond)
					return internal.Updated, nil
				} else {
					continue
				}
			}

			existingId, err := bpfmanagentinternal.GetID(&existingBpfProgram)
			if err != nil {
				return internal.Requeue, fmt.Errorf("failed to get kernel id from bpfProgram: %v", err)
			}

			// If bpfProgram Maps OR the program ID annotation isn't up to date just update it and return
			if !reflect.DeepEqual(existingId, r.progId) {
				r.Logger.Info("Updating bpfProgram Object", "Id", r.progId, "bpfProgram", existingBpfProgram.Name)
				// annotations should be populated on create
				existingBpfProgram.Annotations[internal.IdAnnotation] = strconv.FormatUint(uint64(*r.progId), 10)
				if err := r.Update(ctx, &existingBpfProgram, &client.UpdateOptions{}); err != nil {
					return internal.Requeue, fmt.Errorf("failed to update bpfProgram's Programs: %v", err)
				}
				return internal.Updated, nil
			}

			if r.updateStatus(ctx, &existingBpfProgram, cond) {
				return internal.Updated, nil
			}
		}
	}

	// We're done reconciling the expected programs.  If any unexpected programs
	// exist, delete them and return the result.
	if len(existingBpfPrograms) > 0 {
		return r.handleProgDelete(ctx, rec, existingBpfPrograms, loadedBpfPrograms, isNodeSelected, isBeingDeleted, mapOwnerStatus)
	} else {
		// We're done reconciling.
		r.Logger.Info("Finished reconciling", "program name", rec.getName())
		return internal.Unchanged, nil
	}
}

// reconcileProgram is called by ALL *Program controllers, and contains much of
// the core logic for taking *Program objects, turning them into bpfProgram
// object(s), and ultimately telling the custom controller types to load real
// bpf programs on the node via bpfman. Additionally it acts as a central point for
// interacting with the K8s API. This function will exit if any action is taken
// against the K8s API. If the function returns a retry boolean and error, the
// reconcile will be retried based on a default 5 second interval if the retry
// boolean is set to `true`.
func (r *ReconcilerCommon) reconcileProgram(ctx context.Context,
	rec bpfmanReconciler,
	program client.Object,
	loadedBpfPrograms map[string]*gobpfman.ListResponse_ListResult) (internal.ReconcileResult, error) {

	r.Logger.V(1).Info("reconcileProgram", "name", program.GetName())

	// Determine which node local actions should be taken based on whether the node is selected
	// OR if the *Program is being deleted.
	isNodeSelected, err := isNodeSelected(&rec.getBpfProgramCommon().NodeSelector, rec.getNode().Labels)
	if err != nil {
		return internal.Requeue, fmt.Errorf("failed to check if node is selected: %v", err)
	}

	isBeingDeleted := !program.GetDeletionTimestamp().IsZero()

	// Query the K8s API to get a list of existing bpfPrograms for this *Program
	// on this node.
	existingBpfPrograms, err := r.getExistingBpfPrograms(ctx, program)
	if err != nil {
		return internal.Requeue, fmt.Errorf("failed to get existing bpfPrograms: %v", err)
	}

	// Determine if the MapOwnerSelector was set, and if so, see if the MapOwner
	// ID can be found.
	mapOwnerStatus, err := r.processMapOwnerParam(ctx, &rec.getBpfProgramCommon().MapOwnerSelector)
	if err != nil {
		return internal.Requeue, fmt.Errorf("failed to determine map owner: %v", err)
	}
	r.Logger.V(1).Info("ProcessMapOwnerParam",
		"isSet", mapOwnerStatus.isSet,
		"isFound", mapOwnerStatus.isFound,
		"isLoaded", mapOwnerStatus.isLoaded,
		"mapOwnerid", mapOwnerStatus.mapOwnerId)

	switch isBeingDeleted {
	case true:
		return r.handleProgDelete(ctx, rec, existingBpfPrograms, loadedBpfPrograms,
			isNodeSelected, isBeingDeleted, mapOwnerStatus)
	case false:
		// Generate the list of BpfPrograms for this *Program. This handles the
		// one *Program to many BpfPrograms (e.g., One *Program maps to multiple
		// interfaces because of PodSelector, or one *Program needs to be
		// installed in multiple containers because of ContainerSelector).
		expectedBpfPrograms, err := rec.getExpectedBpfPrograms(ctx)
		if err != nil {
			return internal.Requeue, fmt.Errorf("failed to get expected bpfPrograms: %v", err)
		}
		return r.handleProgCreateOrUpdate(ctx, rec, program, existingBpfPrograms, expectedBpfPrograms, loadedBpfPrograms,
			isNodeSelected, isBeingDeleted, mapOwnerStatus)
	}

	// This return should never be reached, but it's here to satisfy the compiler.
	return internal.Unchanged, nil
}

// MapOwnerParamStatus provides the output from a MapOwerSelector being parsed.
type MapOwnerParamStatus struct {
	isSet      bool
	isFound    bool
	isLoaded   bool
	mapOwnerId *uint32
}

// This function parses the MapOwnerSelector Labor Selector field from the
// BpfProgramCommon struct in the *Program Objects. The labels should map to
// a BpfProgram Object that this *Program wants to share maps with. If found, this
// function returns the ID of the BpfProgram that owns the map on this node.
// Found or not, this function also returns some flags (isSet, isFound, isLoaded)
// to help with the processing and setting of the proper condition on the BpfProgram Object.
func (r *ReconcilerCommon) processMapOwnerParam(
	ctx context.Context,
	selector *metav1.LabelSelector) (*MapOwnerParamStatus, error) {
	mapOwnerStatus := &MapOwnerParamStatus{}

	// Parse the MapOwnerSelector label selector.
	mapOwnerSelectorMap, err := metav1.LabelSelectorAsMap(selector)
	if err != nil {
		mapOwnerStatus.isSet = true
		return mapOwnerStatus, fmt.Errorf("failed to parse MapOwnerSelector: %v", err)
	}

	// If no data was entered, just return with default values, all flags set to false.
	if len(mapOwnerSelectorMap) == 0 {
		return mapOwnerStatus, nil
	} else {
		mapOwnerStatus.isSet = true

		// Add the labels from the MapOwnerSelector to a map and add an additional
		// label to filter on just this node. Call K8s to find all the eBPF programs
		// that match this filter.
		labelMap := client.MatchingLabels{internal.K8sHostLabel: r.NodeName}
		for key, value := range mapOwnerSelectorMap {
			labelMap[key] = value
		}
		opts := []client.ListOption{labelMap}
		bpfProgramList := &bpfmaniov1alpha1.BpfProgramList{}
		r.Logger.V(1).Info("MapOwner Labels:", "opts", opts)
		err := r.List(ctx, bpfProgramList, opts...)
		if err != nil {
			return mapOwnerStatus, err
		}

		// If no BpfProgram Objects were found, or more than one, then return.
		if len(bpfProgramList.Items) == 0 {
			return mapOwnerStatus, nil
		} else if len(bpfProgramList.Items) > 1 {
			return mapOwnerStatus, fmt.Errorf("MapOwnerSelector resolved to multiple bpfProgram Objects")
		} else {
			mapOwnerStatus.isFound = true

			// Get bpfProgram based on UID meta
			prog, err := bpfmanagentinternal.GetBpfmanProgram(ctx, r.BpfmanClient, bpfProgramList.Items[0].GetUID())
			if err != nil {
				return nil, fmt.Errorf("failed to get bpfman program for BpfProgram with UID %s: %v", bpfProgramList.Items[0].GetUID(), err)
			}

			kernelInfo := prog.GetKernelInfo()
			if kernelInfo == nil {
				return nil, fmt.Errorf("failed to process bpfman program for BpfProgram with UID %s: %v", bpfProgramList.Items[0].GetUID(), err)
			}
			mapOwnerStatus.mapOwnerId = &kernelInfo.Id

			// Get most recent condition from the one eBPF Program and determine
			// if the BpfProgram is loaded or not.
			conLen := len(bpfProgramList.Items[0].Status.Conditions)
			if conLen > 0 &&
				bpfProgramList.Items[0].Status.Conditions[conLen-1].Type ==
					string(bpfmaniov1alpha1.BpfProgCondLoaded) {
				mapOwnerStatus.isLoaded = true
			}

			return mapOwnerStatus, nil
		}
	}
}

// get Clientset returns a kubernetes clientset.
func getClientset() (*kubernetes.Clientset, error) {

	// get the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("error getting config: %v", err)
	}
	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error creating clientset: %v", err)
	}

	return clientset, nil
}

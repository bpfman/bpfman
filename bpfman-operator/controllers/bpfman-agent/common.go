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
// program bpfman from user intent in K8s CRDs.
type bpfmanReconciler interface {
	getRecCommon() *ReconcilerCommon
	reconcileBpfmanProgram(context.Context,
		map[string]*gobpfman.ListResponse_ListResult,
		*bpfmaniov1alpha1.BytecodeSelector,
		*bpfmaniov1alpha1.BpfProgram,
		bool,
		bool,
		*MapOwnerParamStatus) (bpfmaniov1alpha1.BpfProgramConditionType, error)
	getFinalizer() string
	getRecType() string
	expectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfProgramList, error)
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
func (r *ReconcilerCommon) updateStatus(ctx context.Context, prog *bpfmaniov1alpha1.BpfProgram, cond bpfmaniov1alpha1.BpfProgramConditionType) bool {

	r.Logger.V(1).Info("updateStatus()", "existing conds", prog.Status.Conditions, "new cond", cond)

	if prog.Status.Conditions != nil {
		numConditions := len(prog.Status.Conditions)

		if numConditions == 1 {
			if prog.Status.Conditions[0].Type == string(cond) {
				// No change, so just return false -- not updated
				return false
			} else {
				// We're changing the condition, so delete this one.  The
				// new condition will be added below.
				prog.Status.Conditions = nil
			}
		} else if numConditions > 1 {
			// We should only ever have one condition, so we shouldn't hit this
			// case.  However, if we do, log a message, delete the existing
			// conditions, and add the new one below.
			r.Logger.Info("more than one BpfProgramCondition", "numConditions", numConditions)
			prog.Status.Conditions = nil
		}
		// if numConditions == 0, just add the new condition below.
	}

	meta.SetStatusCondition(&prog.Status.Conditions, cond.Condition())

	r.Logger.V(1).Info("Updating bpfProgram condition", "bpfProgram", prog.Name, "condition", cond.Condition().Type)
	if err := r.Status().Update(ctx, prog); err != nil {
		r.Logger.Error(err, "failed to set bpfProgram object status")
	}

	r.Logger.V(1).Info("condition updated", "new condition", cond)
	return true
}

func (r *ReconcilerCommon) getExistingBpfProgs(ctx context.Context, owner metav1.Object) (map[string]bpfmaniov1alpha1.BpfProgram, error) {
	bpfProgramList := &bpfmaniov1alpha1.BpfProgramList{}

	// Only list bpfPrograms for this *Program and the controller's node
	opts := []client.ListOption{
		client.MatchingLabels{internal.BpfProgramOwnerLabel: owner.GetName(), internal.K8sHostLabel: r.NodeName},
	}

	err := r.List(ctx, bpfProgramList, opts...)
	if err != nil {
		return nil, err
	}

	existingProgs := map[string]bpfmaniov1alpha1.BpfProgram{}
	for _, bpfProg := range bpfProgramList.Items {
		existingProgs[bpfProg.GetName()] = bpfProg
	}

	return existingProgs, nil
}

// createBpfProgram moves some shared logic for building bpfProgram objects
// into a central location.
func (r *ReconcilerCommon) createBpfProgram(ctx context.Context,
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
func handleProgDelete(
	ctx context.Context,
	r *ReconcilerCommon,
	common *bpfmaniov1alpha1.BpfProgramCommon,
	rec bpfmanReconciler,
	existingPrograms map[string]bpfmaniov1alpha1.BpfProgram,
	programMap map[string]*gobpfman.ListResponse_ListResult,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus,
) (internal.ReconcileResult, error) {
	for _, prog := range existingPrograms {
		// Reconcile the bpfProgram if error write condition and exit with
		// retry.
		cond, err := rec.reconcileBpfmanProgram(ctx,
			programMap,
			&common.ByteCode,
			&prog,
			isNodeSelected,
			true, // delete program
			mapOwnerStatus,
		)
		if err != nil {
			r.updateStatus(ctx, &prog, cond)
			return internal.Requeue, fmt.Errorf("failed to delete bpfman program: %v", err)
		}

		if r.removeFinalizer(ctx, &prog, rec.getFinalizer()) {
			return internal.Updated, nil
		}

		if isBeingDeleted {
			// We're deleting these programs because the *Program is being
			// deleted, so update the status and the program will be deleted
			// when the owner is deleted.
			if r.updateStatus(ctx, &prog, cond) {
				return internal.Updated, nil
			}
		} else {
			// We're deleting these programs because they were not expected due
			// to changes that caused the containers to not be selected anymore.
			// So, explicitly delete them.
			opts := client.DeleteOptions{}
			r.Logger.Info("Deleting bpfProgram", "Name", prog.Name, "Owner", prog.GetName())
			if err := r.Delete(ctx, &prog, &opts); err != nil {
				return internal.Requeue, fmt.Errorf("failed to create bpfProgram object: %v", err)
			}
			return internal.Updated, nil
		}
	}

	// We're done reconciling.
	r.Logger.Info("Nothing to do for this program")
	return internal.Unchanged, nil
}

// handleProgCreateOrUpdate compares the expected bpfPrograms to the existing
// bpfPrograms.  If a bpfProgram is expected but doesn't exist, it is created.
// If an expected bpfProgram exists, it is reconciled. If a bpfProgram exists
// but is not expected, it is deleted.
func handleProgCreateOrUpdate(
	ctx context.Context,
	r *ReconcilerCommon,
	common *bpfmaniov1alpha1.BpfProgramCommon,
	rec bpfmanReconciler,
	program client.Object,
	existingPrograms map[string]bpfmaniov1alpha1.BpfProgram,
	expectedPrograms *bpfmaniov1alpha1.BpfProgramList,
	programMap map[string]*gobpfman.ListResponse_ListResult,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus,
) (internal.ReconcileResult, error) {
	// If the *Program isn't being deleted ALWAYS create the bpfPrograms
	// even if the node isn't selected
	for _, expectedProg := range expectedPrograms.Items {
		prog, exists := existingPrograms[expectedProg.Name]
		if exists {
			// Remove the bpfProgram from the existingPrograms map so we know
			// not to delete it below.
			delete(existingPrograms, expectedProg.Name)
		} else {
			// Create a new bpfProgram Object for this program.
			opts := client.CreateOptions{}
			r.Logger.Info("Creating bpfProgram", "Name", expectedProg.Name, "Owner", program.GetName())
			if err := r.Create(ctx, &expectedProg, &opts); err != nil {
				return internal.Requeue, fmt.Errorf("failed to create bpfProgram object: %v", err)
			}
			existingPrograms[expectedProg.Name] = prog
			return internal.Updated, nil
		}

		// bpfProgram Object exists go ahead and reconcile it, if there is
		// an error write condition and exit with retry.
		cond, err := rec.reconcileBpfmanProgram(ctx,
			programMap,
			&common.ByteCode,
			&prog,
			isNodeSelected,
			isBeingDeleted,
			mapOwnerStatus,
		)
		if err != nil {
			if r.updateStatus(ctx, &prog, cond) {
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
				if r.updateStatus(ctx, &prog, cond) {
					r.Logger.V(1).Info("Update condition from bpfman reconcile", "condition", cond)
					return internal.Updated, nil
				} else {
					continue
				}
			}

			existingId, err := bpfmanagentinternal.GetID(&prog)
			if err != nil {
				return internal.Requeue, fmt.Errorf("failed to get kernel id from bpfProgram: %v", err)
			}

			// If bpfProgram Maps OR the program ID annotation isn't up to date just update it and return
			if !reflect.DeepEqual(existingId, r.progId) {
				r.Logger.Info("Updating bpfProgram Object", "Id", r.progId, "bpfProgram", prog.Name)
				// annotations should be populate on create
				prog.Annotations[internal.IdAnnotation] = strconv.FormatUint(uint64(*r.progId), 10)
				if err := r.Update(ctx, &prog, &client.UpdateOptions{}); err != nil {
					return internal.Requeue, fmt.Errorf("failed to update bpfProgram's Programs: %v", err)
				}
				return internal.Updated, nil
			}

			if r.updateStatus(ctx, &prog, cond) {
				return internal.Updated, nil
			}
		}
	}

	// We're done reconciling the expected programs.  If any unexpected programs
	// exist, delete them and return the result.
	return handleProgDelete(ctx, r, common, rec, existingPrograms, programMap, isNodeSelected, isBeingDeleted, mapOwnerStatus)
}

// reconcileProgram is called by ALL *Program controllers, and contains much of
// the core logic for taking *Program objects, turning them into bpfProgram
// object(s), and ultimately telling the custom controller types to load real
// bpf programs on the node via bpfman. Additionally it acts as a central point for
// interacting with the K8s API. This function will exit if any action is taken
// against the K8s API. If the function returns a retry boolean and error, the
// reconcile will be retried based on a default 5 second interval if the retry
// boolean is set to `true`.
func reconcileProgram(ctx context.Context,
	rec bpfmanReconciler,
	program client.Object,
	common *bpfmaniov1alpha1.BpfProgramCommon,
	ourNode *v1.Node,
	programMap map[string]*gobpfman.ListResponse_ListResult) (internal.ReconcileResult, error) {

	// initialize reconciler state
	r := rec.getRecCommon()

	// Determine which node local actions should be taken based on whether the node is selected
	// OR if the *Program is being deleted.
	isNodeSelected, err := isNodeSelected(&common.NodeSelector, ourNode.Labels)
	if err != nil {
		return internal.Requeue, fmt.Errorf("failed to check if node is selected: %v", err)
	}

	isBeingDeleted := !program.GetDeletionTimestamp().IsZero()

	// Query the K8s API to get a list of existing bpfPrograms for this *Program
	// on this node.
	existingPrograms, err := r.getExistingBpfProgs(ctx, program)
	if err != nil {
		return internal.Requeue, fmt.Errorf("failed to get existing bpfPrograms: %v", err)
	}

	// Determine if the MapOwnerSelector was set, and if so, see if the MapOwner
	// ID can be found.
	mapOwnerStatus, err := ProcessMapOwnerParam(ctx, &common.MapOwnerSelector, r)
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
		return handleProgDelete(ctx, r, common, rec, existingPrograms,
			programMap, isNodeSelected, isBeingDeleted, mapOwnerStatus)
	case false:
		// Generate the list of BpfPrograms for this *Program. This handles the
		// one *Program to many BpfPrograms (e.g., One *Program maps to multiple
		// interfaces because of PodSelector, or one *Program needs to be
		// installed in multiple containers because of ContainerSelector).
		expectedPrograms, err := rec.expectedBpfPrograms(ctx)
		if err != nil {
			return internal.Requeue, fmt.Errorf("failed to get expected bpfPrograms: %v", err)
		}
		return handleProgCreateOrUpdate(ctx, r, common, rec, program, existingPrograms,
			expectedPrograms, programMap, isNodeSelected, isBeingDeleted, mapOwnerStatus)
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
func ProcessMapOwnerParam(ctx context.Context,
	selector *metav1.LabelSelector,
	r *ReconcilerCommon) (*MapOwnerParamStatus, error) {
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

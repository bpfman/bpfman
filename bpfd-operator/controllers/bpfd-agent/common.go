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
	"reflect"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	bpfdiov1alpha1 "github.com/bpfd-dev/bpfd/bpfd-operator/apis/v1alpha1"
	bpfdagentinternal "github.com/bpfd-dev/bpfd/bpfd-operator/controllers/bpfd-agent/internal"
	"github.com/bpfd-dev/bpfd/bpfd-operator/internal"
	gobpfd "github.com/bpfd-dev/bpfd/clients/gobpfd/v1"
	"github.com/go-logr/logr"
	"google.golang.org/grpc"
)

//+kubebuilder:rbac:groups=bpfd.dev,resources=bpfprograms,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfd.dev,resources=bpfprograms/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfd.dev,resources=bpfprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfd.dev,resources=tcprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfd.dev,resources=xdpprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfd.dev,resources=tracepointprograms/finalizers,verbs=update
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
	BpfdClient   gobpfd.LoaderClient
	Logger       logr.Logger
	NodeName     string
	bpfPrograms  map[string]bpfdiov1alpha1.BpfProgram
	expectedMaps map[string]string
}

// bpfdReconciler defines a generic bpfProgram K8s object reconciler which can
// program bpfd from user intent in K8s CRDs.
type bpfdReconciler interface {
	getRecCommon() *ReconcilerCommon
	reconcileBpfdProgram(context.Context,
		map[string]*gobpfd.ListResponse_ListResult,
		interface{},
		*bpfdiov1alpha1.BpfProgram,
		bool,
		bool) (bpfdiov1alpha1.BpfProgramConditionType, error)
	getFinalizer() string
	getRecType() string
	buildBpfPrograms(ctx context.Context) (*bpfdiov1alpha1.BpfProgramList, error)
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

func getInterfaces(interfaceSelector *bpfdiov1alpha1.InterfaceSelector, ourNode *v1.Node) ([]string, error) {
	var interfaces []string

	if interfaceSelector.Interfaces != nil {
		return *interfaceSelector.Interfaces, nil
	}

	if interfaceSelector.PrimaryNodeInterface != nil {
		nodeIface, err := bpfdagentinternal.GetPrimaryNodeInterface(ourNode)
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
func (r *ReconcilerCommon) removeFinalizer(ctx context.Context, o client.Object, finalizer string) (bool, error) {
	r.Logger.V(1).WithValues("object name", o.GetName()).Info("bpfProgram is deleted, don't load program, remove finalizer")

	changed := controllerutil.RemoveFinalizer(o, finalizer)
	if changed {
		err := r.Update(ctx, o)
		if err != nil {
			return true, err
		}
	}

	return changed, nil
}

// updateStatus updates the status of a BpfProgram object if needed, returning
// if the status was already set for the given bpfProgram, meaning reconciliation
// may continue.
func (r *ReconcilerCommon) updateStatus(ctx context.Context, prog *bpfdiov1alpha1.BpfProgram, cond bpfdiov1alpha1.BpfProgramConditionType) (bool, error) {
	if prog.Status.Conditions != nil {
		// Get most recent condition
		recentIdx := len(prog.Status.Conditions) - 1

		condition := prog.Status.Conditions[recentIdx]

		if condition.Type == string(cond) {
			return true, nil
		}
	}

	meta.SetStatusCondition(&prog.Status.Conditions, cond.Condition())
	r.Logger.V(1).WithValues("bpfProgram", prog.Name, "condition", cond.Condition().Type).Info("Updating bpfProgram condition")
	if err := r.Status().Update(ctx, prog); err != nil {
		r.Logger.Error(err, "failed to set bpfProgram object status")
		return false, nil
	}

	return false, nil
}

func (r *ReconcilerCommon) getBpfPrograms(ctx context.Context, owner metav1.Object) error {
	bpfProgramList := &bpfdiov1alpha1.BpfProgramList{}

	// Only list bpfPrograms for this *Program and the controller's node
	opts := []client.ListOption{
		client.MatchingLabels{internal.BpfProgramOwnerLabel: owner.GetName(), internal.K8sHostLabel: r.NodeName},
	}

	err := r.List(ctx, bpfProgramList, opts...)
	if err != nil {
		return err
	}

	for _, bpfProg := range bpfProgramList.Items {
		r.bpfPrograms[bpfProg.GetName()] = bpfProg
	}

	return nil
}

// createBpfProgram moves some shared logic for building bpfProgram objects
// into a central location.
func (r *ReconcilerCommon) createBpfProgram(ctx context.Context,
	bpfProgramName string,
	finalizer string,
	owner metav1.Object,
	ownerType string,
	annotations map[string]string) (*bpfdiov1alpha1.BpfProgram, error) {
	bpfProg := &bpfdiov1alpha1.BpfProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name:       bpfProgramName,
			Finalizers: []string{finalizer},
			Labels: map[string]string{internal.BpfProgramOwnerLabel: owner.GetName(),
				internal.K8sHostLabel: r.NodeName},
			Annotations: annotations,
		},
		Spec: bpfdiov1alpha1.BpfProgramSpec{
			Type: ownerType,
		},
		Status: bpfdiov1alpha1.BpfProgramStatus{Conditions: []metav1.Condition{}},
	}

	// Make the corresponding BpfProgramConfig the owner
	if err := ctrl.SetControllerReference(owner, bpfProg, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to bpfProgram object owner reference: %v", err)
	}

	return bpfProg, nil
}

// reconcileProgram is called by ALL *Program controllers, and contains much of
// the core logic for taking *Program objects, turning them into bpfProgram
// object(s), and ultimately telling the custom controller types to load real
// bpf programs on the node via bpfd. Additionally it acts as a central point for
// interacting with the K8s API.
func reconcileProgram(ctx context.Context,
	rec bpfdReconciler,
	program client.Object,
	common *bpfdiov1alpha1.BpfProgramCommon,
	ourNode *v1.Node,
	programMap map[string]*gobpfd.ListResponse_ListResult) (bool, error) {

	r := rec.getRecCommon()
	r.bpfPrograms = map[string]bpfdiov1alpha1.BpfProgram{}

	// Populate already existing bpfPrograms for *Program.
	err := r.getBpfPrograms(ctx, program)
	if err != nil {
		return false, fmt.Errorf("failed to get bpfPrograms: %v", err)
	}

	// Get and Create any new bpfPrograms exiting after each creation since
	// the K8s API "create" action will re-trigger the reconcile loop allowing the
	// logic to continue incrementally.
	progList, err := rec.buildBpfPrograms(ctx)
	if err != nil {
		r.Logger.Error(err, "failed to create bpfPrograms")
		return true, nil
	}

	for _, prog := range progList.Items {
		_, exists := r.bpfPrograms[prog.Name]
		if !exists {
			opts := client.CreateOptions{}
			r.Logger.Info("creating bpfProgram", "Name", prog.Name)
			if err := r.Create(ctx, &prog, &opts); err != nil {
				return true, fmt.Errorf("failed to create bpfProgram object: %v", err)
			}
			r.bpfPrograms[prog.Name] = prog
			return false, nil
		}
	}

	isNodeSelected, err := isNodeSelected(&common.NodeSelector, ourNode.Labels)
	if err != nil {
		return false, fmt.Errorf("failed to check if node is selected: %v", err)
	}

	isBeingDeleted := !program.GetDeletionTimestamp().IsZero()

	bytecode, err := bpfdagentinternal.GetBytecode(r.Client, &common.ByteCode)
	if err != nil {
		return false, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	for _, bpfProg := range r.bpfPrograms {
		progCond, err := rec.reconcileBpfdProgram(ctx, programMap, bytecode, &bpfProg, isNodeSelected, isBeingDeleted)
		if err != nil {
			r.Logger.Error(err, "Failed to reconcile bpfd")
		}

		// Deletion of a bpfProgram takes two reconciles
		// 1. Remove the finalizer
		// 2. Update the condition to BpfProgCondUnloaded so the operator knows it's
		//    safe to remove the parent Program Object, which is when the bpfProgram
		//	  is automatically deleted by the owner-reference.
		if isBeingDeleted {
			changed, err := r.removeFinalizer(ctx, &bpfProg, rec.getFinalizer())
			if err != nil {
				r.Logger.Error(err, "failed to set remove bpfProgram Finalizer")
				return true, nil
			}
			if changed {
				return false, nil
			}
			return r.updateStatus(ctx, &bpfProg, progCond)
		}

		// Make sure if we're not selected exit and write correct condition
		if !isNodeSelected {
			r.Logger.V(1).Info("Program does not select this node")
			// Write NodeNodeSelected status
			return r.updateStatus(ctx, &bpfProg, progCond)
		}

		// If bpfProgram Maps isn't up to date just update it and return
		if !reflect.DeepEqual(bpfProg.Spec.Maps, r.expectedMaps) && len(r.expectedMaps) != 0 {
			r.Logger.V(1).Info("Updating bpfProgram Object", "Maps", r.expectedMaps, "bpfProgram", bpfProg.Name)
			bpfProg.Spec.Maps = r.expectedMaps
			if err := r.Update(ctx, &bpfProg, &client.UpdateOptions{}); err != nil {
				r.Logger.Error(err, "failed to update bpfProgram's Programs")
				return true, nil
			}
			return false, nil
		}

		alreadySet, err := r.updateStatus(ctx, &bpfProg, progCond)
		if err != nil {
			r.Logger.Error(err, "failed to update bpfProgram's Status")
			return true, nil
		}

		// Only stop reconcile loop if we updated the status for a bpfprogram
		if !alreadySet {
			return false, nil
		}
	}

	return false, nil
}

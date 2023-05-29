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

	//"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	//"k8s.io/apimachinery/pkg/types"

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
	v1 "k8s.io/api/core/v1"
)

//+kubebuilder:rbac:groups=bpfd.io,resources=bpfprograms,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfd.io,resources=bpfprograms/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfd.io,resources=bpfprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfd.io,resources=tcprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfd.io,resources=xdpprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfd.io,resources=tracepointprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get

const (
	retryDurationAgent = 5 * time.Second
)

// ReconcilerCommon provides a skeleton for a all Program Reconcilers.
type ReconcilerCommon struct {
	client.Client
	Scheme       *runtime.Scheme
	GrpcConn     *grpc.ClientConn
	BpfdClient   gobpfd.LoaderClient
	Logger       logr.Logger
	NodeName     string
	bpfPrograms  map[string]*bpfdiov1alpha1.BpfProgram
	expectedMaps map[string]string
}

// bpfdReconciler defines a k8s reconciler which can program bpfd.
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
	createBpfPrograms(ctx context.Context) (bool, error)
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

	if interfaceSelector.Interface != nil {
		interfaces = append(interfaces, *interfaceSelector.Interface)
		return interfaces, nil
	}

	if interfaceSelector.PrimaryNodeInterface != nil {
		nodeIface, err := bpfdagentinternal.GetPrimaryNodeInterface(ourNode)
		if err != nil {
			return nil, err
		}

		interfaces = append(interfaces, nodeIface)
		return interfaces, nil
	}

	return interfaces, fmt.Errorf("no interfaces selected")

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

// updateStatus updates the status of the BpfProgram object if needed, returning
// if the update should be retried and any errors.
func (r *ReconcilerCommon) updateStatus(ctx context.Context, prog *bpfdiov1alpha1.BpfProgram, cond bpfdiov1alpha1.BpfProgramConditionType) (bool, error) {
	// If status is already set just exit
	if prog.Status.Conditions != nil {
		// Get most recent condition
		recentIdx := len(prog.Status.Conditions) - 1

		condition := prog.Status.Conditions[recentIdx]

		if condition.Type == string(cond) {
			return false, nil
		}
	}

	meta.SetStatusCondition(&prog.Status.Conditions, cond.Condition())

	if err := r.Status().Update(ctx, prog); err != nil {
		r.Logger.Error(err, "failed to set bpfProgram object status")
		return true, nil
	}

	return false, nil
}

func (r *ReconcilerCommon) getBpfPrograms(ctx context.Context, owner metav1.Object) error {
	bpfProgramList := &bpfdiov1alpha1.BpfProgramList{}

	// Only list bpfPrograms for this *Program and the controller's node
	opts := []client.ListOption{
		client.MatchingLabels{internal.BpfProgramOwnerLabel: owner.GetName()},
		client.MatchingLabels{internal.K8sHostLabel: r.NodeName},
	}

	err := r.List(ctx, bpfProgramList, opts...)
	if err != nil {
		return err
	}

	for _, bpfProg := range bpfProgramList.Items {
		r.bpfPrograms[bpfProg.GetName()] = &bpfProg
	}

	return nil
}

func (r *ReconcilerCommon) createBpfProgram(ctx context.Context,
	bpfProgramName string,
	finalizer string,
	owner metav1.Object,
	ownerType string,
	annotations map[string]string) error {
	// Create the bpfProgram Object if it doesn't exist
	var bpfProg *bpfdiov1alpha1.BpfProgram

	r.Logger.Info("creating bpfProgram", "Name", bpfProgramName)
	bpfProg = &bpfdiov1alpha1.BpfProgram{
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
		return fmt.Errorf("failed to bpfProgram object owner reference: %v", err)
	}

	opts := client.CreateOptions{}
	if err := r.Create(ctx, bpfProg, &opts); err != nil {
		return fmt.Errorf("failed to create bpfProgram object: %v", err)
	}

	// Only append if we're creating a new BpfProgram
	r.bpfPrograms[bpfProgramName] = bpfProg

	return nil
}

func reconcileProgram(ctx context.Context,
	rec bpfdReconciler,
	program client.Object,
	common *bpfdiov1alpha1.BpfProgramCommon,
	ourNode *v1.Node,
	programMap map[string]*gobpfd.ListResponse_ListResult) (bool, error) {

	// Initialize bpfProgram
	r := rec.getRecCommon()
	r.bpfPrograms = make(map[string]*bpfdiov1alpha1.BpfProgram)

	// Populate already existing bpfPrograms
	err := r.getBpfPrograms(ctx, program)
	if err != nil {
		return false, fmt.Errorf("failed to get bpfPrograms: %v", err)
	}

	r.Logger.V(1).WithValues("expected bpfPrograms", r.bpfPrograms)
	// Create any new bpfPrograms exiting after each creation since "create" will
	// re-trigger this reconcile.
	allProgsCreated, err := rec.createBpfPrograms(ctx)
	if err != nil {
		r.Logger.Error(err, "failed to create bpfPrograms")
		return true, nil
	}
	// Still more bpfProgram Objects to create on later reconcile
	if !allProgsCreated {
		return false, nil
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

		progCond, err := rec.reconcileBpfdProgram(ctx, programMap, bytecode, bpfProg, isNodeSelected, isBeingDeleted)
		if err != nil {
			r.Logger.Error(err, "Failed to reconcile bpfd")
		}

		// Deletion of a bpfProgram takes two reconciles
		// 1. Remove the finalizer
		// 2. Update the condition to BpfProgCondUnloaded so the operator knows it's
		//    safe to remove the parent Program Object, which is when the bpfProgram
		//	  is automatically deleted by the owner-reference.
		if isBeingDeleted {
			changed, err := r.removeFinalizer(ctx, bpfProg, rec.getFinalizer())
			if err != nil {
				r.Logger.Error(err, "failed to set remove bpfProgram Finalizer")
				return true, nil
			}
			if changed {
				return false, nil
			}
			return r.updateStatus(ctx, bpfProg, progCond)
		}

		// Make sure if we're not selected exit and write correct condition
		if !isNodeSelected {
			r.Logger.V(1).Info("Program does not select this node")
			// Write NodeNodeSelected status
			return r.updateStatus(ctx, bpfProg, progCond)
		}

		// If bpfProgram Maps isn't up to date just update it and return
		if !reflect.DeepEqual(bpfProg.Spec.Maps, r.expectedMaps) && len(r.expectedMaps) != 0 {
			r.Logger.V(1).Info("Updating bpfProgram Object", "Maps", r.expectedMaps)
			bpfProg.Spec.Maps = r.expectedMaps
			if err := r.Update(ctx, bpfProg, &client.UpdateOptions{}); err != nil {
				r.Logger.Error(err, "failed to update bpfProgram's Programs")
				return true, nil
			}
			return false, nil
		}

		return r.updateStatus(ctx, bpfProg, progCond)
	}

	return false, nil
}

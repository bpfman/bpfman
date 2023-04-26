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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	bpfdiov1alpha1 "github.com/bpfd-dev/bpfd/bpfd-operator/apis/v1alpha1"
	bpfdagentinternal "github.com/bpfd-dev/bpfd/bpfd-operator/controllers/bpfd-agent/internal"
	gobpfd "github.com/bpfd-dev/bpfd/clients/gobpfd/v1"
	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	v1 "k8s.io/api/core/v1"
)

//+kubebuilder:rbac:groups=bpfd.io,resources=bpfprograms,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfd.io,resources=bpfprograms/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfd.io,resources=bpfprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=secrets,namespace=bpfd,verbs=get

// ReconcilerCommon provides a skeleton for a all Program Reconcilers.
type ReconcilerCommon struct {
	client.Client
	Scheme           *runtime.Scheme
	GrpcConn         *grpc.ClientConn
	BpfdClient       gobpfd.LoaderClient
	Logger           logr.Logger
	NodeName         string
	Namespace        string
	bpfProgram       *bpfdiov1alpha1.BpfProgram
	expectedPrograms map[string]map[string]string
}

// bpfdReconciler defines a k8s reconciler which can program bpfd.
type bpfdReconciler interface {
	getRecCommon() *ReconcilerCommon
	reconcileBpfdPrograms(context.Context,
		map[string]*gobpfd.ListResponse_ListResult,
		interface{},
		bool,
		bool) (bpfProgramConditionType, error)
	getFinalizer() string
	getRecType() string
}

type bpfProgramConditionType string

const (
	retryDurationAgent                             = 5 * time.Second
	BpfProgCondLoaded      bpfProgramConditionType = "Loaded"
	BpfProgCondNotLoaded   bpfProgramConditionType = "NotLoaded"
	BpfProgCondNotUnloaded bpfProgramConditionType = "NotUnLoaded"
	BpfProgCondNotSelected bpfProgramConditionType = "NotSelected"
	BpfProgCondUnloaded    bpfProgramConditionType = "Unloaded"
)

func (b bpfProgramConditionType) Condition() metav1.Condition {
	cond := metav1.Condition{}

	switch b {
	case BpfProgCondLoaded:
		cond = metav1.Condition{
			Type:    string(BpfProgCondLoaded),
			Status:  metav1.ConditionTrue,
			Reason:  "bpfdLoaded",
			Message: "Successfully loaded bpfProgram",
		}
	case BpfProgCondNotLoaded:
		cond = metav1.Condition{
			Type:    string(BpfProgCondNotLoaded),
			Status:  metav1.ConditionTrue,
			Reason:  "bpfdNotLoaded",
			Message: "Failed to load bpfProgram",
		}
	case BpfProgCondNotUnloaded:
		cond = metav1.Condition{
			Type:    string(BpfProgCondNotUnloaded),
			Status:  metav1.ConditionTrue,
			Reason:  "bpfdNotUnloaded",
			Message: "Failed to unload bpfProgram",
		}
	case BpfProgCondNotSelected:
		cond = metav1.Condition{
			Type:    string(BpfProgCondNotSelected),
			Status:  metav1.ConditionTrue,
			Reason:  "nodeNotSelected",
			Message: "This node is not selected to run the bpfProgram",
		}
	case BpfProgCondUnloaded:
		cond = metav1.Condition{
			Type:    string(BpfProgCondUnloaded),
			Status:  metav1.ConditionTrue,
			Reason:  "bpfdUnloaded",
			Message: "This BpfProgram object and all it's bpfd programs have been unloaded",
		}
	}

	return cond
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
	// Logic to check if this node is selected by the BpfProgramConfig object
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
func (r *ReconcilerCommon) updateStatus(ctx context.Context, prog *bpfdiov1alpha1.BpfProgram, cond bpfProgramConditionType) (bool, error) {
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

func reconcileProgram(ctx context.Context,
	rec bpfdReconciler,
	program client.Object,
	common *bpfdiov1alpha1.BpfProgramCommon,
	ourNode *v1.Node,
	programMap map[string]*gobpfd.ListResponse_ListResult) (bool, error) {

	// Initialize bpfProgram
	r := rec.getRecCommon()
	r.bpfProgram = &bpfdiov1alpha1.BpfProgram{}

	bpfProgramName := fmt.Sprintf("%s-%s", program.GetName(), r.NodeName)

	// Always create the bpfProgram Object if it doesn't exist
	err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: bpfProgramName}, r.bpfProgram)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Info("bpfProgram object doesn't exist creating...", "Name", bpfProgramName)
			r.bpfProgram = &bpfdiov1alpha1.BpfProgram{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bpfProgramName,
					Finalizers: []string{rec.getFinalizer()},
					Labels:     map[string]string{"ownedByProgram": program.GetName()},
				},
				Spec: bpfdiov1alpha1.BpfProgramSpec{
					Node:     r.NodeName,
					Type:     rec.getRecType(),
					Programs: make(map[string]map[string]string),
				},
				Status: bpfdiov1alpha1.BpfProgramStatus{Conditions: []metav1.Condition{}},
			}

			// Make the corresponding BpfProgramConfig the owner
			if err = ctrl.SetControllerReference(program, r.bpfProgram, r.Scheme); err != nil {
				return false, fmt.Errorf("failed to bpfProgram object owner reference: %v", err)
			}

			opts := client.CreateOptions{}
			if err = r.Create(ctx, r.bpfProgram, &opts); err != nil {
				return false, fmt.Errorf("failed to create bpfProgram object: %v",
					err)
			}

			return false, nil
		} else {
			return false, fmt.Errorf("failed getting bpfProgram %s : %v",
				bpfProgramName, err)
		}
	}

	isNodeSelected, err := isNodeSelected(&common.NodeSelector, ourNode.Labels)
	if err != nil {
		return false, fmt.Errorf("failed to check if node is selected: %v", err)
	}

	isBeingDeleted := !program.GetDeletionTimestamp().IsZero()

	bytecode, err := bpfdagentinternal.GetBytecode(r.Client, r.Namespace, &common.ByteCode)
	if err != nil {
		return false, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	progCond, err := rec.reconcileBpfdPrograms(ctx, programMap, bytecode, isNodeSelected, isBeingDeleted)
	if err != nil {
		r.Logger.Error(err, "Failed to reconcile bpfd")
	}

	// Deletion of a bpfProgram takes two reconciles
	// 1. Remove the finalizer
	// 2. Update the condition to 'BpfProgCondUnloaded' so the operator knows it's
	//    safe to remove the parent Program Object, which is when the bpfProgram
	//	  is automatically deleted by the owner-reference.
	if isBeingDeleted {
		changed, err := r.removeFinalizer(ctx, r.bpfProgram, rec.getFinalizer())
		if err != nil {
			r.Logger.Error(err, "failed to set remove bpfProgram Finalizer")
			return true, nil
		}
		if changed {
			return false, nil
		}
		return r.updateStatus(ctx, r.bpfProgram, progCond)
	}

	// Make sure if we're not selected exit and write correct condition
	if !isNodeSelected {
		r.Logger.V(1).Info("Program does not select this node")
		// Write NodeNodeSelected status
		return r.updateStatus(ctx, r.bpfProgram, progCond)
	}

	// If bpfProgram isn't up to date just update it and return
	if !reflect.DeepEqual(r.bpfProgram.Spec.Programs, r.expectedPrograms) {
		r.Logger.V(1).Info("Updating bpfProgram Object", "Programs", r.expectedPrograms)
		r.bpfProgram.Spec.Programs = r.expectedPrograms
		if err := r.Update(ctx, r.bpfProgram, &client.UpdateOptions{}); err != nil {
			r.Logger.Error(err, "failed to update bpfProgram's Programs")
			return true, nil
		}
		return false, nil
	}

	return r.updateStatus(ctx, r.bpfProgram, progCond)
}

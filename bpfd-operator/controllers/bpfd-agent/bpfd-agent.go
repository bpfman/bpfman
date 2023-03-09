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
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	bpfdiov1alpha1 "github.com/redhat-et/bpfd/bpfd-operator/apis/v1alpha1"
	"github.com/redhat-et/bpfd/bpfd-operator/internal"
	gobpfd "github.com/redhat-et/bpfd/clients/gobpfd/v1"
	"google.golang.org/grpc"
	v1 "k8s.io/api/core/v1"
)

// BpfProgramReconciler reconciles a BpfProgram object
type BpfProgramReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	GrpcConn   *grpc.ClientConn
	BpfdClient gobpfd.LoaderClient
	NodeName   string
	Logger     logr.Logger
}

type bpfProgramConditionType string

const (
	BpfdAgentFinalizer                             = "bpfd.io.agent/finalizer"
	retryDurationAgent                             = 5 * time.Second
	BpfProgCondLoaded      bpfProgramConditionType = "Loaded"
	BpfProgCondNotLoaded   bpfProgramConditionType = "NotLoaded"
	BpfProgCondNotUnloaded bpfProgramConditionType = "NotUnLoaded"
	BpfProgCondNotSelected bpfProgramConditionType = "NotSelected"
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
	}

	return cond
}

//+kubebuilder:rbac:groups=bpfd.io,resources=bpfprograms,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfd.io,resources=bpfprograms/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfd.io,resources=bpfprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfd.io,resources=bpfprogramconfigs,verbs=get;list;watch
//+kubebuilder:rbac:groups=bpfd.io,resources=bpfprogramconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// This should be called in the following scenarios
// 1. A new BpfProgramConfig Object is created
// 2. An BpfProgramConfig Object is Updated (i.e one of the following fields change
//   - NodeSelector
//   - Priority
//   - AttachPoint
//   - Bytecodesource
//
// 3. Our NodeLabels are updated and the Node is no longer selected by an BpfProgramConfig
//
// 4. An bpfProgramConfig Object is deleted
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *BpfProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = log.FromContext(ctx)

	r.Logger.Info("bpfd-agent is reconciling", "request", req.String())

	// Lookup K8s node object for this bpfd-agent This should always succeed
	ourNode := &v1.Node{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfd-agent node %s : %v",
			req.NamespacedName, err)
	}

	BpfProgramConfigs := &bpfdiov1alpha1.BpfProgramConfigList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, BpfProgramConfigs, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting BpfProgramConfigs for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(BpfProgramConfigs.Items) == 0 {
		return ctrl.Result{Requeue: false}, nil
	}

	// Get existing ebpf state from bpfd.
	nodeState, err := r.listBpfdPrograms(ctx)
	if err != nil {
		r.Logger.Error(err, "failed to list loaded bpfd programs")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
	}

	// Rebuild BpfProgramConfig.Spec from nodeState to compare to desired state
	existingConfigs, err := internal.CreateExistingState(nodeState)
	if err != nil {
		r.Logger.Error(err, "failed to generate node state to k8s state mapping")
		return ctrl.Result{Requeue: false, RequeueAfter: retryDurationAgent}, nil
	}

	// Reconcile every BpfProgramConfig Object
	// note: This doesn't necessarily result in any extra grpc calls to bpfd
	for _, BpfProgramConfig := range BpfProgramConfigs.Items {
		retry, err := r.reconcileBpfProgramConfig(ctx, &BpfProgramConfig, ourNode, existingConfigs)
		if err != nil {
			r.Logger.Error(err, "Reconciling BpfProgramConfig Failed", "BpfProgramConfigName", BpfProgramConfig.Name, "Retrying", retry)
			return ctrl.Result{Requeue: retry, RequeueAfter: retryDurationAgent}, nil
		}
	}

	return ctrl.Result{Requeue: false}, nil
}

// reconcileBpfProgramConfig reconciles the existing node state to the user intent
// within a single BpfProgramConfig Object.
func (r *BpfProgramReconciler) reconcileBpfProgramConfig(ctx context.Context,
	BpfProgramConfig *bpfdiov1alpha1.BpfProgramConfig,
	ourNode *v1.Node,
	nodeState map[internal.ProgramKey]internal.ExistingReq) (bool, error) {

	bpfProgram := &bpfdiov1alpha1.BpfProgram{}
	bpfProgramName := fmt.Sprintf("%s-%s", BpfProgramConfig.Name, r.NodeName)
	isNodeSelected := false

	// Always create the bpfProgram Object if it doesn't exist
	err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: bpfProgramName}, bpfProgram)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Info("bpfProgram doesn't exist creating...")
			bpfProgram = &bpfdiov1alpha1.BpfProgram{
				ObjectMeta: metav1.ObjectMeta{
					Name:       bpfProgramName,
					Finalizers: []string{BpfdAgentFinalizer},
					Labels:     map[string]string{"owningConfig": BpfProgramConfig.Name},
				},
				Spec: bpfdiov1alpha1.BpfProgramSpec{
					Programs: map[string]bpfdiov1alpha1.BpfProgramMeta{},
				},
				Status: bpfdiov1alpha1.BpfProgramStatus{Conditions: []metav1.Condition{}},
			}

			// Make the corresponding BpfProgramConfig the owner
			if err = ctrl.SetControllerReference(BpfProgramConfig, bpfProgram, r.Scheme); err != nil {
				return false, fmt.Errorf("failed to bpfProgram object owner reference: %v", err)
			}

			opts := client.CreateOptions{}
			if err = r.Create(ctx, bpfProgram, &opts); err != nil {
				return false, fmt.Errorf("failed to create bpfProgram object: %v",
					err)
			}
		} else {
			return false, fmt.Errorf("failed getting bpfProgram %s : %v",
				bpfProgramName, err)
		}
	}

	// Logic to check if this node is selected by the BpfProgramConfig object
	selector, err := metav1.LabelSelectorAsSelector(&BpfProgramConfig.Spec.NodeSelector)
	if err != nil {
		return false, fmt.Errorf("failed to parse nodeSelector: %v",
			err)
	}

	nodeLabelSet, err := labels.ConvertSelectorToLabelsMap(labels.FormatLabels(ourNode.Labels))
	if err != nil {
		return false, fmt.Errorf("failed to parse node labels : %v",
			err)
	}

	isNodeSelected = selector.Matches(nodeLabelSet)

	r.Logger.V(1).Info("Bpfd Node State Dump", "NodeState", nodeState)

	// Compare the desired state to existing bpfd state
	v, ok := nodeState[internal.ProgramKey{Name: BpfProgramConfig.Spec.Name, ProgType: BpfProgramConfig.Spec.Type}]
	// bpfProgram doesn't exist on node
	if !ok {
		r.Logger.V(1).Info("bpfProgram doesn't exist on node")

		// If BpfProgramConfig is being deleted just remove agent finalizer so the
		// owner relationship can take care of cleanup
		if !BpfProgramConfig.DeletionTimestamp.IsZero() {
			if controllerutil.ContainsFinalizer(bpfProgram, BpfdAgentFinalizer) {
				controllerutil.RemoveFinalizer(bpfProgram, BpfdAgentFinalizer)
				err := r.Update(ctx, bpfProgram)
				if err != nil {
					return false, err
				}
			}

			return false, nil
		}

		// Make sure if we're not selected just exit
		if !isNodeSelected {
			// Write NodeNodeSelected status
			r.updateStatus(ctx, bpfProgram, BpfProgCondNotSelected)

			return false, nil
		}

		// otherwise load it

		// TODO(astoycos) This will need to end up being a list of loadRequests
		// if a given BpfProgramConfig selects more than one attach point
		// (i.e if we support a pod LabelSelector for interfaces) For now
		// we only support specifying a single BpfProgramAttachPoint in the API so
		// there will only be a single loadRequest per BpfProgramConfig Object.
		loadRequest, err := internal.BuildBpfdLoadRequest(BpfProgramConfig)
		if err != nil {
			r.updateStatus(ctx, bpfProgram, BpfProgCondNotLoaded)

			return true, fmt.Errorf("failed to generate bpfd load request: %v",
				err)
		}

		return r.loadBpfdProgram(ctx, loadRequest, bpfProgram)
	}

	// BpfProgram exists but either BpfProgramConfig is being deleted or node is no
	// longer selected....unload program
	if !BpfProgramConfig.DeletionTimestamp.IsZero() || !isNodeSelected {
		r.Logger.V(1).Info("bpfProgram exists on Node but is scheduled for deletion or node is no longer selected", "isDeleted", !BpfProgramConfig.DeletionTimestamp.IsZero(),
			"isSelected", isNodeSelected)
		if controllerutil.ContainsFinalizer(bpfProgram, BpfdAgentFinalizer) {
			if retry, err := r.unloadBpfdProgram(ctx, bpfProgram); err != nil {
				return retry, err
			}

			// Remove bpfd-agentFinalizer. Once all finalizers have been
			// removed, the object will be deleted.
			controllerutil.RemoveFinalizer(bpfProgram, BpfdAgentFinalizer)
			err := r.Update(ctx, bpfProgram)
			if err != nil {
				return false, err
			}

			// If K8s hasn't cleaned up here it means we're no longer selected
			// write NodeNodeSelected status ignoring error (object may not exist)
			r.updateStatus(ctx, bpfProgram, BpfProgCondNotSelected)
		}

		return false, nil
	}

	BpfProgramConfig.Spec.NodeSelector = metav1.LabelSelector{}

	// TODO(astoycos) This will need to end up being a list of loadRequests
	// if a given BpfProgramConfig selects more than one attach point
	// (i.e if we support a pod LabelSelector for interfaces) For now
	// we only support specifying a single BpfProgramAttachPoint in the API so
	// there will only be a single loadRequest per BpfProgramConfig Object.
	loadRequest, err := internal.BuildBpfdLoadRequest(BpfProgramConfig)
	if err != nil {
		r.updateStatus(ctx, bpfProgram, BpfProgCondNotLoaded)

		return true, fmt.Errorf("failed to generate bpfd load request: %v",
			err)
	}

	// Temporary hacks for state which won't match yet based on list API
	// Proceed-on updates are not currently supported
	if BpfProgramConfig.Spec.Type == "XDP" || BpfProgramConfig.Spec.Type == "TC" {
		BpfProgramConfig.Spec.AttachPoint.NetworkMultiAttach.ProceedOn = nil
		v.Req.AttachPoint.NetworkMultiAttach.ProceedOn = nil
	}

	r.Logger.V(1).Info("desired k8s state vs existing state", "BpfProgramConfigSpec",
		BpfProgramConfig.Spec, "existingState", *v.Req)

	// BpfProgram exists but is not correct state, unload and recreate
	if !reflect.DeepEqual(*v.Req, BpfProgramConfig.Spec) {
		if retry, err := r.unloadBpfdProgram(ctx, bpfProgram); err != nil {
			return retry, err
		}

		// Re-create correct version
		return r.loadBpfdProgram(ctx, loadRequest, bpfProgram)
	} else {
		// Program already exists, but bpfProgram K8s Object might not be up to date
		if _, ok := bpfProgram.Spec.Programs[v.Uuid]; !ok {
			maps, err := internal.GetMapsForUUID(v.Uuid)
			if err != nil {
				r.Logger.Error(err, "failed to get bpfProgram's Maps")
				maps = map[string]string{}
			}

			bpfProgram.Spec.Programs[v.Uuid] = bpfdiov1alpha1.BpfProgramMeta{
				AttachPoint: internal.AttachConversion(loadRequest),
				Maps:        maps,
			}

			r.Logger.V(1).Info("Updating programs from nodestate", "Programs", bpfProgram.Spec.Programs)
			// Update bpfProgram once successfully loaded
			if err = r.Update(ctx, bpfProgram, &client.UpdateOptions{}); err != nil {
				return false, fmt.Errorf("failed to create bpfProgram object: %v",
					err)
			}

			r.updateStatus(ctx, bpfProgram, BpfProgCondLoaded)
		} else {
			// Program exists and bpfProgram K8s Object is up to date
			r.Logger.V(1).Info("Ignoring Object Change nothing to reconcile on node")
		}
	}

	return false, nil
}

func (r *BpfProgramReconciler) loadBpfdProgram(ctx context.Context, loadRequest *gobpfd.LoadRequest, prog *bpfdiov1alpha1.BpfProgram) (bool, error) {
	var res *gobpfd.LoadResponse

	res, err := r.BpfdClient.Load(ctx, loadRequest)
	if err != nil {
		r.updateStatus(ctx, prog, BpfProgCondNotLoaded)

		return true, fmt.Errorf("failed to load bpfProgram via bpfd: %v",
			err)
	}
	uuid := res.GetId()

	maps, err := internal.GetMapsForUUID(uuid)
	if err != nil {
		r.Logger.Error(err, "failed to get bpfProgram's Maps")
		maps = map[string]string{}
	}

	prog.Spec.Programs[uuid] = bpfdiov1alpha1.BpfProgramMeta{
		AttachPoint: internal.AttachConversion(loadRequest),
		Maps:        maps,
	}

	r.Logger.V(1).Info("updating programs after load", "Programs", prog.Spec.Programs)
	// Update bpfProgram once successfully loaded
	if err = r.Update(ctx, prog, &client.UpdateOptions{}); err != nil {
		return false, fmt.Errorf("failed to update bpfProgram programs: %v",
			err)
	}

	r.updateStatus(ctx, prog, BpfProgCondLoaded)

	return false, nil
}

func (r *BpfProgramReconciler) unloadBpfdProgram(ctx context.Context, prog *bpfdiov1alpha1.BpfProgram) (bool, error) {
	if len(prog.Spec.Programs) == 0 {
		r.Logger.Info("no programs to remove")
		return false, nil
	}

	for uuid := range prog.Spec.Programs {
		r.Logger.Info("unloading ebpf program via bpfd", "program-uuid", uuid)

		_, err := r.BpfdClient.Unload(ctx, internal.BuildBpfdUnloadRequest(uuid))
		if err != nil {
			r.updateStatus(ctx, prog, BpfProgCondNotUnloaded)

			return true, fmt.Errorf("failed to unload bpfProgram via bpfd: %v",
				err)
		}
		delete(prog.Spec.Programs, uuid)
	}

	r.Logger.V(1).Info("updating programs after unload", "Programs", prog.Spec.Programs)
	// Update bpfProgram once successfully unloaded
	if err := r.Update(ctx, prog, &client.UpdateOptions{}); err != nil {
		return false, fmt.Errorf("failed to update bpfProgram programs: %v",
			err)
	}

	return false, nil
}

func (r *BpfProgramReconciler) listBpfdPrograms(ctx context.Context) ([]*gobpfd.ListResponse_ListResult, error) {
	listReq := gobpfd.ListRequest{}

	listResponse, err := r.BpfdClient.List(ctx, &listReq)
	if err != nil {
		return nil, err
	}

	return listResponse.Results, nil
}

func (r *BpfProgramReconciler) updateStatus(ctx context.Context, prog *bpfdiov1alpha1.BpfProgram, cond bpfProgramConditionType) {
	meta.SetStatusCondition(&prog.Status.Conditions, cond.Condition())

	if err := r.Status().Update(ctx, prog); err != nil {
		r.Logger.Error(err, "failed to set bpfProgram object status")
	}
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfd-Agent should reconcile whenever a BpfProgramConfig is updated,
// load the program to the node via bpfd, and then create a bpfProgram object
// to reflect per node state information.
func (r *BpfProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfdiov1alpha1.BpfProgramConfig{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		Owns(&bpfdiov1alpha1.BpfProgram{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		// Only trigger reconciliation if node labels change since that could
		// make the BpfProgramConfig no longer select the Node. Additionally only
		// care about node events specific to our node
		Watches(
			&source.Kind{Type: &v1.Node{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(predicate.LabelChangedPredicate{}, nodePredicate(r.NodeName))),
		).
		Complete(r)
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

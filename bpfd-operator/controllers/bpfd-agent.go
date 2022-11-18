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

package controllers

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

	bpfdiov1alpha1 "github.com/redhat-et/bpfd/api/v1alpha1"
	gobpfd "github.com/redhat-et/bpfd/clients/gobpfd/v1"
	"github.com/redhat-et/bpfd/internal"
	"google.golang.org/grpc"
	v1 "k8s.io/api/core/v1"
)

// EbpfProgramReconciler reconciles a EbpfProgram object
type EbpfProgramReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	GrpcConn   *grpc.ClientConn
	BpfdClient gobpfd.LoaderClient
	NodeName   string
}

// existingRequests rebuilds the LoadRequests needed to actually get the node
// to the desired state
type existingReq struct {
	uuid string
	req  *bpfdiov1alpha1.EbpfProgramConfigSpec
}

const bpfdAgentFinalizer = "bpfd.io.agent/finalizer"
const retryDuration = 10 * time.Second

//+kubebuilder:rbac:groups=bpfd.io,resources=ebpfprograms,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfd.io,resources=ebpfprograms/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfd.io,resources=ebpfprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfd.io,resources=ebpfprogramconfigs,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the EbpfProgram object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
// This should be called in the following scenarios
// 1. A new ebpfProgramConfig Object is created
// 2. An ebpfProgramConfig Object is Updated (i.e one of the following fields change
//   - NodeSelector
//   - Priority
//   - AttachPoint
//   - Bytecodesource
//
// 3. Our NodeLabels are updated and the Node is no longer selected by an EbpfProgramConfig
//
// 4. And ebpfProgramCongfig Object is deleted
//
// To cover each case mentioned above the Reconcile loop does the following
// 1. If (ebpfProgram object doesn't exist && our node is selected) Load and Create ebpfProgram object
// 2. If (ebpfProgram exists && our node isn't selected) Remove and Delete ebpfProgram object
// 3. If (ebpfProgram exists && our node is Selected)
//   - List All programs
//   - Get Priority, AttachPoint, and BytecodeSrc from output
//   - Match ^ with what's in the current ebpfProgramConfig
//   - Reconcile (Remove and Add) if information has changed
//     -> Update program Map
//
// 4. If Deletion Timestamp is set on ebpfProgramConfig Remove and Delete ebpfProgram
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *EbpfProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	l.Info("ebpf-agent is reconciling", "request", req.String())

	// Lookup Ks node object for this bpfd-agent This should always succeed
	ourNode := &v1.Node{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfd-agent node %s : %v",
			req.NamespacedName, err)
	}

	ebpfProgramConfigs := &bpfdiov1alpha1.EbpfProgramConfigList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, ebpfProgramConfigs, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting ebpfProgramConfigs for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(ebpfProgramConfigs.Items) == 0 {
		return ctrl.Result{Requeue: false}, nil
	}

	// Get existing ebpf state on the node.
	nodeState, err := r.listBpfdPrograms(ctx)
	if err != nil {
		l.Error(err, "failed to list loaded bpfd programs")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDuration}, nil
	}

	existingRequests := map[string]existingReq{}
	// Rebuild EbpfProgramConfig.Spec from nodeState to compare to desired state
	for _, bpfdProg := range nodeState {
		existingConfigSpec := &bpfdiov1alpha1.EbpfProgramConfigSpec{
			Name:     bpfdProg.Name,
			Type:     bpfdProg.ProgramType.String(),
			Priority: bpfdProg.GetNetworkMultiAttach().Priority,
			ByteCode: bpfdiov1alpha1.ByteCodeSource{
				ImageUrl: &bpfdProg.Path,
			},
			AttachPoint: bpfdiov1alpha1.EbpfProgramAttachPoint{
				Interface: &bpfdProg.GetNetworkMultiAttach().Iface,
			},
			NodeSelector: metav1.LabelSelector{},
		}

		existingRequests[bpfdProg.Name] = existingReq{
			uuid: bpfdProg.Id,
			req:  existingConfigSpec,
		}
	}

	// Reconcile every ebpfProgramConfig Object
	// note: This doesn't necessarily result in any extra grpc calls to bpfd
	for _, ebpfProgramConfig := range ebpfProgramConfigs.Items {
		retry, err := r.reconcileEbpfProgramConfig(ctx, &ebpfProgramConfig, ourNode, existingRequests)
		if err != nil {
			l.Error(err, "Reconciling ebpfProgramConfig Failed", "ebpfProgramConfigName", ebpfProgramConfig.Name)
			return ctrl.Result{Requeue: retry, RequeueAfter: retryDuration}, nil
		}
	}

	return ctrl.Result{Requeue: false}, nil
}

// reconcileEbpfProgramConfig reconciles the existing node state to the user intent
// within a single EbpfProgramConfig Object.
func (r *EbpfProgramReconciler) reconcileEbpfProgramConfig(ctx context.Context,
	ebpfProgramConfig *bpfdiov1alpha1.EbpfProgramConfig,
	ourNode *v1.Node,
	nodeState map[string]existingReq) (bool, error) {

	l := log.FromContext(ctx)
	ebpfProgram := &bpfdiov1alpha1.EbpfProgram{}
	ebpfProgramName := fmt.Sprintf("%s-%s", ebpfProgramConfig.Name, r.NodeName)
	isNodeSelected := false

	// Always create the ebpfProgram Object if it doesn't exist
	err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: ebpfProgramName}, ebpfProgram)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("ebpfProgram doesn't exist creating...")
			ebpfProgram = &bpfdiov1alpha1.EbpfProgram{
				ObjectMeta: metav1.ObjectMeta{
					Name:       ebpfProgramName,
					Finalizers: []string{bpfdAgentFinalizer},
				},
				Spec: bpfdiov1alpha1.EbpfProgramSpec{
					ProgramMap: map[string]bpfdiov1alpha1.EbpfProgramAttachPoint{},
					Maps:       map[string]string{},
				},
				Status: bpfdiov1alpha1.EbpfProgramStatus{Conditions: []metav1.Condition{}},
			}

			ctrl.SetControllerReference(ebpfProgramConfig, ebpfProgram, r.Scheme)

			opts := client.CreateOptions{}
			if err = r.Create(ctx, ebpfProgram, &opts); err != nil {
				return false, fmt.Errorf("failed to create ebpfProgram object: %v",
					err)
			}
		} else {
			return false, fmt.Errorf("failed getting ebpfProgram %s : %v",
				ebpfProgramName, err)
		}
	}

	// Logic to check if this node is selected by the ebpfProgramConfig object
	selector, err := metav1.LabelSelectorAsSelector(&ebpfProgramConfig.Spec.NodeSelector)
	if err != nil {
		return false, fmt.Errorf("failed to parse nodeSelector: %v",
			err)
	}

	// TODO (astoycos) not 100% sure this is right
	nodeLabelSet, err := labels.ConvertSelectorToLabelsMap(labels.FormatLabels(ourNode.Labels))
	if err != nil {
		return false, fmt.Errorf("failed to parse node labels : %v",
			err)
	}

	isNodeSelected = selector.Matches(nodeLabelSet)

	// inline function for updating the status of the ebpfProgramObject
	updateStatusFunc := func(condition metav1.Condition) {
		meta.SetStatusCondition(&ebpfProgram.Status.Conditions, condition)

		if err = r.Status().Update(ctx, ebpfProgram); err != nil {
			l.Error(err, "failed to set ebpfProgram object status")
		}
	}

	// inline function for loading and ebpfProgram via bpfd
	loadFunc := func(loadRequest *gobpfd.LoadRequest) (bool, error) {
		l.Info("loading ebpf program via bpfd")

		uuid, err := r.loadBpfdProgram(ctx, loadRequest)
		if err != nil {
			failedLoadedCondition := metav1.Condition{
				Type:    "NotLoaded",
				Status:  metav1.ConditionTrue,
				Reason:  "bpfdNotLoaded",
				Message: "Failed to load ebpfProgram",
			}

			updateStatusFunc(failedLoadedCondition)

			return true, fmt.Errorf("failed to load ebpfProgram via bpfd: %v",
				err)
		}

		// TODO(astoycos) this wont' always be a multi Attach program
		ebpfProgram.Spec.ProgramMap = map[string]bpfdiov1alpha1.EbpfProgramAttachPoint{uuid: {Interface: &loadRequest.GetNetworkMultiAttach().Iface}}

		// Update ebpfProgram once successfully loaded
		if err = r.Update(ctx, ebpfProgram, &client.UpdateOptions{}); err != nil {
			return false, fmt.Errorf("failed to create ebpfProgram object: %v",
				err)
		}

		loadedCondition := metav1.Condition{
			Type:    "Loaded",
			Status:  metav1.ConditionTrue,
			Reason:  "bpfdLoaded",
			Message: "Successfully loaded ebpfProgram",
		}

		updateStatusFunc(loadedCondition)

		l.Info("Program loaded via bpfd", "bpfd-program-uuid", uuid)
		return false, nil
	}

	// This function unloads the bpf program via bpfd and removes the bpfd-agent
	// finalizer from the ebpfProgram Object
	unloadFunc := func() (bool, error) {
		for uuid := range ebpfProgram.Spec.ProgramMap {
			unloadRequest, err := internal.BuildBpfdUnloadRequest(uuid)
			if err != nil {
				// Add a condition and exit do requeue, bpfd might become ready
				return true, fmt.Errorf("failed to generate bpfd unload request: %v",
					err)
			}

			err = r.unloadBpfdProgram(ctx, unloadRequest)
			if err != nil {
				failUnloadCondition := metav1.Condition{
					Type:    "NotUnloaded",
					Status:  metav1.ConditionTrue,
					Reason:  "bpfdNotUnloaded",
					Message: "Failed to unload ebpfProgram",
				}

				updateStatusFunc(failUnloadCondition)

				return true, fmt.Errorf("failed to unload ebpfProgram via bpfd: %v",
					err)
			}

			l.Info("Program loaded via bpfd", "bpfd-program-uuid", uuid)
		}

		return false, nil
	}

	// TODO(astoycos) This will need to end up being a list of loadRequests
	// if a given ebpfProgramConfig selects more than one attach point
	// (i.e if we support a pod LabelSelector for interfaces) For now
	// we only support specifying a single node interface in the API so
	// there will only be a single loadRequest per ebpfProgramConfig Object.
	loadRequest, err := internal.BuildBpfdLoadRequest(ebpfProgramConfig)
	if err != nil {
		return true, fmt.Errorf("failed to generate bpfd load request: %v",
			err)
	}

	// Compare the desired state to existing node state
	v, ok := nodeState[ebpfProgramConfig.Spec.Name]
	// ebpfProgram doesn't exist on node
	if !ok {
		// Make sure if we're not selected just exit
		if !isNodeSelected {
			// Write NodeNodeSelected status
			nodeNotSelectedCondition := metav1.Condition{
				Type:    "NotSelected",
				Status:  metav1.ConditionTrue,
				Reason:  "nodeNotSelected",
				Message: "This node is not selected to run the ebpfProgram",
			}
			updateStatusFunc(nodeNotSelectedCondition)

			return false, nil
		}

		// If EbpfProgramConfig is being deleted just remove agent finalizer so the
		// owner relationship can take care of cleanup
		if !ebpfProgramConfig.DeletionTimestamp.IsZero() {
			if controllerutil.ContainsFinalizer(ebpfProgram, bpfdAgentFinalizer) {
				controllerutil.RemoveFinalizer(ebpfProgram, bpfdAgentFinalizer)
				err := r.Update(ctx, ebpfProgram)
				if err != nil {
					return false, err
				}
			}

			return false, nil
		}

		// otherwise load it
		return loadFunc(loadRequest)
	}

	// EbpfProgram exists but either EbpfProgramConfig is being deleted or node is no
	// longer selected....unload program
	if !ebpfProgramConfig.DeletionTimestamp.IsZero() || !isNodeSelected {
		if controllerutil.ContainsFinalizer(ebpfProgram, bpfdAgentFinalizer) {
			if retry, err := unloadFunc(); err != nil {
				return retry, err
			}

			// Remove bpfd-agentFinalizer. Once all finalizers have been
			// removed, the object will be deleted.
			controllerutil.RemoveFinalizer(ebpfProgram, bpfdAgentFinalizer)
			err := r.Update(ctx, ebpfProgram)
			if err != nil {
				return false, err
			}

			// If K8s hasn't cleaned up here it means we're no longer selected
			// write NodeNodeSelected status ignoring error (object may not exist)
			nodeNotSelectedCondition := metav1.Condition{
				Type:    "NotSelected",
				Status:  metav1.ConditionTrue,
				Reason:  "nodeNotSelected",
				Message: "This node is no longer selected to run the ebpfProgram",
			}
			updateStatusFunc(nodeNotSelectedCondition)
		}
		return false, nil
	}

	// TODO (astoycos) blocked on https://github.com/redhat-et/bpfd/issues/177
	// Temporary hacks for state which won't match yet based on list API
	ebpfProgramConfig.Spec.NodeSelector = metav1.LabelSelector{}
	ebpfProgramConfig.Spec.ByteCode.ImageUrl = nil
	v.req.ByteCode.ImageUrl = nil

	// EbpfProgram exists but is not correct state
	if !reflect.DeepEqual(*v.req, ebpfProgramConfig.Spec) {
		// Program is already loaded but not in the right state... Unload it and load new one
		unloadRequest, err := internal.BuildBpfdUnloadRequest(v.uuid)
		if err != nil {
			// Add a condition and exit do requeue, bpfd might become ready
			return true, fmt.Errorf("failed to generate bpfd unload request: %v",
				err)
		}

		err = r.unloadBpfdProgram(ctx, unloadRequest)
		if err != nil {
			failUnloadCondition := metav1.Condition{
				Type:    "NotUnloaded",
				Status:  metav1.ConditionTrue,
				Reason:  "bpfdNotUnloaded",
				Message: "Failed to unload ebpfProgram",
			}

			updateStatusFunc(failUnloadCondition)

			return true, fmt.Errorf("failed to unload ebpfProgram via bpfd: %v",
				err)
		}

		// Re-create correct version
		return loadFunc(loadRequest)
	}

	l.Info("Ignoring Object Change nothing to reconcile on node")

	return false, nil
}

func (r *EbpfProgramReconciler) loadBpfdProgram(ctx context.Context, loadRequest *gobpfd.LoadRequest) (string, error) {
	var res *gobpfd.LoadResponse
	res, err := r.BpfdClient.Load(ctx, loadRequest)
	if err != nil {
		r.GrpcConn.Close()
		return "", err
	}
	id := res.GetId()

	return id, nil
}

func (r *EbpfProgramReconciler) unloadBpfdProgram(ctx context.Context, unloadRequest *gobpfd.UnloadRequest) error {
	_, err := r.BpfdClient.Unload(ctx, unloadRequest)
	if err != nil {
		r.GrpcConn.Close()
		return err
	}
	return nil
}

func (r *EbpfProgramReconciler) listBpfdPrograms(ctx context.Context) ([]*gobpfd.ListResponse_ListResult, error) {
	listReq := gobpfd.ListRequest{}

	listResponse, err := r.BpfdClient.List(ctx, &listReq)
	if err != nil {
		r.GrpcConn.Close()
		return nil, err
	}

	return listResponse.Results, nil
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfd-Agent should reconcile whenever a ebpfProgramConfig is updated,
// load the program to the node via bpfd, and then create a ebpfProgram object
// to reflect per node state information.
func (r *EbpfProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfdiov1alpha1.EbpfProgramConfig{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		Owns(&bpfdiov1alpha1.EbpfProgram{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		// Only trigger reconciliation if node labels change since that could
		// make the EbpfProgramConfig no longer select the Node. Additionally only
		// care about node events specific to our node
		Watches(
			&source.Kind{Type: &v1.Node{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(predicate.LabelChangedPredicate{}, nodePredicate(r.NodeName))),
		).
		Complete(r)
}

// Only return node updates for our node
func nodePredicate(nodeName string) predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetLabels()["kubernetes.io/hostname"] == nodeName
		},
	}
}

func shouldRecreate(ebpfProgram *bpfdiov1alpha1.EbpfProgram, ebpfProgramConfig *bpfdiov1alpha1.EbpfProgramConfig) bool {
	recreate := false

	// If ebpfProgram already exists and the attach points are represented correctly
	// we don't need to reconcile
	// Currently there is only one type of attach point (interface) in the future this
	// will be extended with features such as Pod Interface selectors (allowing to select pod veth interfaces etc)
	for _, v := range ebpfProgram.Spec.ProgramMap {
		if v == ebpfProgramConfig.Spec.AttachPoint {
			break
		}
	}

	return recreate
}

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
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sigs.k8s.io/controller-runtime/pkg/event"


	bpfdiov1alpha1 "github.com/redhat-et/bpfd/api/v1alpha1"
	gobpfd "github.com/redhat-et/bpfd/clients/gobpfd/v1"
	v1 "k8s.io/api/core/v1"
)

// EbpfProgramReconciler reconciles a EbpfProgram object
type EbpfProgramReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	BpfdClient gobpfd.LoaderClient
}

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
// This should be called when
// 1. A new ebpfProgramConfig Object is created
// 2. An ebpfProgramConfig Object is Updated (i.e one of the following fields change
//   - NodeSelector
//   - Priority
//   - AttachPoint
//   - Bytecodesource
//
// 3. And ebpfProgramCongfig Object is deleted
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

	l.Info("In ebpf-agent")
	// Lookup the EbpfProgramConfig instance for this reconcile request
	ebpf_program_config := &bpfdiov1alpha1.EbpfProgramConfig{}
	r.Get(ctx, req.NamespacedName, ebpf_program_config)

	// Get the nodename where this pod is running
	nodeName := os.Getenv("NODENAME")

	ebpfProgramName := fmt.Sprintf("%s-%s", ebpf_program_config.GetName(), nodeName)

	return ctrl.Result{}, nil
}

func shouldReconcile(ebpfProgram bpfdiov1alpha1.EbpfProgram, ebpfProgramConfig bpfdiov1alpha1.EbpfProgramConfig) (bool, error) {
	reconcile := false

	// If ebpfProgram already exists and the attach points are represented correctly
	// we don't need to reconcile
	// Currently there is only one type of attach point (interface) in the future this
	// will be extended with features such as Pod Interface selectors (allowing to select pod veth interfaces etc)
	for _, v := range ebpfProgram.Spec.ProgramMap {
		if v == ebpfProgramConfig.Spec.AttachPoint {
			break
		}
	}
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfd-Agent should reconcile whenever a ebpfProgramConfig is updated,
// load the program to the node via bpfd, and then create a ebpfProgram object
// to reflect per node state information.
func (r *EbpfProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// we only care about the Node we live on
	nodeName := os.Getenv("NODENAME")

	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfdiov1alpha1.EbpfProgramConfig{}, builder.WithPredicates(predicate.GenerationChangedPredicate{}),).
		Owns(&bpfdiov1alpha1.EbpfProgram{}).
		Watches(
			&source.Kind{Type: &v1.Node{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.LabelChangedPredicate{}),
		).
		Complete(r)
}

// func ebpfProgramPredicate() predicate.Funcs {
// 	return predicate.Funcs{
// 		UpdateFunc: func(e event.UpdateEvent) bool {
// 			return e.ObjectOld.DeepCopyObject().GetObjectKind().GroupVersionKind().ToAPIVersionAndKind()
// 		},
// 	}
// }
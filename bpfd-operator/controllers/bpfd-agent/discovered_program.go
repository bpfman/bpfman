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

package bpfdagent

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	bpfdiov1alpha1 "github.com/bpfd-dev/bpfd/bpfd-operator/apis/v1alpha1"
	bpfdagentinternal "github.com/bpfd-dev/bpfd/bpfd-operator/controllers/bpfd-agent/internal"
	"github.com/bpfd-dev/bpfd/bpfd-operator/internal"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

const (
	syncDurationDiscoveredController = 30 * time.Second
)

type DiscoveredProgramReconciler struct {
	ReconcilerCommon
}

// SetupWithManager sets up the controller with the Manager.
func (r *DiscoveredProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("DiscoveredProgramController").
		// Only trigger reconciliation if node object changed. Additionally always
		// exit the end of a reconcile with a requeue so that the discovered programs
		// are kept up to date.
		Watches(
			&source.Kind{Type: &v1.Node{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(predicate.ResourceVersionChangedPredicate{}, nodePredicate(r.NodeName))),
		).
		Watches(
			&source.Kind{Type: &bpfdiov1alpha1.BpfProgram{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(
				internal.BpfProgramNodePredicate(r.NodeName)),
				internal.DiscoveredBpfProgramPredicate(),
			),
		).
		Complete(r)
}

// Reconcile ALL discovered bpf programs on the system whenever an event is received.
func (r *DiscoveredProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = log.FromContext(ctx)

	// Get existing ebpf state from bpfd.
	programs, err := bpfdagentinternal.ListAllPrograms(ctx, r.BpfdClient)
	if err != nil {
		r.Logger.Error(err, "failed to list loaded bpf programs")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
	}

	for _, p := range programs {

		// skip bpf programs loaded by bpfd, their corresponding bpfProgram object
		// will be managed by another controller.
		if p.Id != nil {
			continue
		}

		// TODO(astoycos) across the operator we need a better way to validate that
		// the name we're building is a valid k8's object name i.e meets the
		// regex: '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*'
		// laid out here -> https://github.com/kubernetes/apimachinery/blob/v0.27.4/pkg/util/validation/validation.go#L43C6-L43C21
		bpfProgName := ""
		if len(p.Name) == 0 {
			bpfProgName = fmt.Sprintf("%d-%s", p.BpfId, r.NodeName)
		} else {
			bpfProgName = fmt.Sprintf("%s-%d-%s", strings.ReplaceAll(p.Name, "_", "-"), p.BpfId, r.NodeName)
		}

		existingBpfProg := &bpfdiov1alpha1.BpfProgram{}

		expectedBpfProg := &bpfdiov1alpha1.BpfProgram{
			ObjectMeta: metav1.ObjectMeta{
				Name: bpfProgName,
				Labels: map[string]string{internal.DiscoveredLabel: "",
					internal.K8sHostLabel: r.NodeName},
				Annotations: bpfdagentinternal.Build_kernel_info_annotations(p),
			},
			Spec: bpfdiov1alpha1.BpfProgramSpec{
				Type: internal.ProgramType(p.ProgramType).String(),
			},
			Status: bpfdiov1alpha1.BpfProgramStatus{Conditions: []metav1.Condition{}},
		}

		// If the bpfProgram object doesn't exist create it.
		err = r.Get(ctx, types.NamespacedName{Name: bpfProgName, Namespace: v1.NamespaceAll}, existingBpfProg)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Logger.V(1).Info("Creating discovered bpfProgram object", "name", expectedBpfProg.Name)
				err = r.Create(ctx, expectedBpfProg)
				if err != nil {
					r.Logger.Error(err, "failed to create bpfProgram object")
					return ctrl.Result{Requeue: false}, nil
				}

				return ctrl.Result{Requeue: false}, nil
			}

			r.Logger.Error(err, "failed to build bpfProgram object")
			return ctrl.Result{Requeue: false}, nil
		}

		// If the bpfProgram object does exist but is stale update it.
		if !reflect.DeepEqual(expectedBpfProg.Annotations, existingBpfProg.Annotations) {
			if err := r.Update(ctx, expectedBpfProg); err != nil {
				r.Logger.Error(err, "failed to build bpfProgram object")
			}

			return ctrl.Result{Requeue: false}, nil
		}

	}

	// If we've created all of the programs, make sure to exit with a retry
	// so that we resync on a 30 second interval.
	return ctrl.Result{Requeue: true, RequeueAfter: syncDurationDiscoveredController}, nil
}

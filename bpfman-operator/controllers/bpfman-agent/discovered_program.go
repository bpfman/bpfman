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
	"reflect"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanagentinternal "github.com/bpfman/bpfman/bpfman-operator/controllers/bpfman-agent/internal"
	"github.com/bpfman/bpfman/bpfman-operator/internal"
	v1 "k8s.io/api/core/v1"
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
			&source.Kind{Type: &bpfmaniov1alpha1.BpfProgram{}},
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
	r.Logger = ctrl.Log.WithName("discvr")

	// The Discovery Reconciler gets called more than others so this tends to be a noisy log,
	// so moved to a Trace log.
	ctxLogger := log.FromContext(ctx)
	ctxLogger.V(2).Info("Reconcile DiscoveredProgs: Enter", "ReconcileKey", req)

	// Get existing ebpf state from bpfman.
	programs, err := bpfmanagentinternal.ListAllPrograms(ctx, r.BpfmanClient)
	if err != nil {
		r.Logger.Error(err, "failed to list loaded bpf programs")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
	}

	// get all existing "discovered" bpfProgram Objects for this node
	opts := []client.ListOption{
		client.MatchingLabels{internal.DiscoveredLabel: "", internal.K8sHostLabel: r.NodeName},
	}

	existingPrograms := bpfmaniov1alpha1.BpfProgramList{}
	err = r.Client.List(ctx, &existingPrograms, opts...)
	if err != nil {
		r.Logger.Error(err, "failed to list existing discovered bpfProgram objects")
	}

	// build an indexable map of existing programs based on bpfProgram name
	existingProgramIndex := map[string]bpfmaniov1alpha1.BpfProgram{}
	for _, p := range existingPrograms.Items {
		existingProgramIndex[p.Name] = p
	}

	for _, p := range programs {
		kernelInfo := p.GetKernelInfo()
		if kernelInfo == nil {
			continue
		}

		programInfo := p.GetInfo()
		if programInfo != nil {
			// skip bpf programs loaded by bpfman, their corresponding bpfProgram object
			// will be managed by another controller.
			metadata := programInfo.GetMetadata()
			if _, ok := metadata[internal.UuidMetadataKey]; ok {
				continue
			}
		}

		// TODO(astoycos) across the agent we need a better way to validate that
		// the object name we're building is a valid k8's object name i.e meets the
		// regex: '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*'
		// laid out here -> https://github.com/kubernetes/apimachinery/blob/v0.27.4/pkg/util/validation/validation.go#L43C6-L43C21
		bpfProgName := ""
		if len(kernelInfo.Name) == 0 {
			bpfProgName = fmt.Sprintf("%d-%s", kernelInfo.Id, r.NodeName)
		} else {
			bpfProgName = fmt.Sprintf("%s-%d-%s", strings.ReplaceAll(kernelInfo.Name, "_", "-"), kernelInfo.Id, r.NodeName)
		}

		expectedBpfProg := &bpfmaniov1alpha1.BpfProgram{
			ObjectMeta: metav1.ObjectMeta{
				Name: bpfProgName,
				Labels: map[string]string{internal.DiscoveredLabel: "",
					internal.K8sHostLabel: r.NodeName},
				Annotations: bpfmanagentinternal.Build_kernel_info_annotations(p),
			},
			Spec: bpfmaniov1alpha1.BpfProgramSpec{
				Type: internal.ProgramType(kernelInfo.ProgramType).String(),
			},
			Status: bpfmaniov1alpha1.BpfProgramStatus{Conditions: []metav1.Condition{}},
		}

		existingBpfProg, ok := existingProgramIndex[bpfProgName]
		// If the bpfProgram object doesn't exist create it.
		if !ok {
			r.Logger.Info("Creating discovered bpfProgram object", "name", expectedBpfProg.Name)
			err = r.Create(ctx, expectedBpfProg)
			if err != nil {
				r.Logger.Error(err, "failed to create bpfProgram object")
			}

			return ctrl.Result{Requeue: false}, nil
		}

		// If the bpfProgram object does exist but is stale update it.
		if !reflect.DeepEqual(expectedBpfProg.Annotations, existingBpfProg.Annotations) {
			if err := r.Update(ctx, expectedBpfProg); err != nil {
				r.Logger.Error(err, "failed to update discovered bpfProgram object", "name", expectedBpfProg.Name)
			}

			return ctrl.Result{Requeue: false}, nil
		}

		delete(existingProgramIndex, bpfProgName)
	}

	// Delete any stale discovered programs
	for _, prog := range existingProgramIndex {
		r.Logger.Info("Deleting stale discovered bpfProgram object", "name", prog.Name)
		if err := r.Delete(ctx, &prog, &client.DeleteOptions{}); err != nil {
			r.Logger.Error(err, "failed to delete stale discoverd bpfProgram object", "name", prog.Name)
		}

		return ctrl.Result{Requeue: false}, nil
	}

	// If we've finished reconciling everything, make sure to exit with a retry
	// so that we resync on a 30 second interval.
	return ctrl.Result{Requeue: true, RequeueAfter: syncDurationDiscoveredController}, nil
}

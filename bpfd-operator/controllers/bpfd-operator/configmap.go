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

package bpfdoperator

import (
	"context"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorinternal "github.com/redhat-et/bpfd/bpfd-operator/controllers/bpfd-operator/internal"
	"github.com/redhat-et/bpfd/bpfd-operator/internal"
)

// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create

type BpfdConfigReconciler struct {
	ReconcilerCommon
}

// SetupWithManager sets up the controller with the Manager.
func (r *BpfdConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Watch the bpfd-daemon configmap to configure the bpfd deployment across the whole cluster
		For(&corev1.ConfigMap{},
			builder.WithPredicates(bpfdConfigPredicate())).
		// This only watches the bpfd daemonset which is stored on disk and will be created
		// by this operator. We're doing a manual watch since the operator (As a controller)
		// doesn't really want to have an owner-ref since we don't have a CRD for
		// configuring it, only a configmap.
		Owns(
			&appsv1.DaemonSet{},
			builder.WithPredicates(bpfdDaemonPredicate())).
		Complete(r)
}

func (r *BpfdConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = log.FromContext(ctx)

	bpfdConfig := &corev1.ConfigMap{}
	if err := r.Get(ctx, req.NamespacedName, bpfdConfig); err != nil {
		if !errors.IsNotFound(err) {
			r.Logger.Error(err, "failed getting bpfd config", "req", req.NamespacedName)
			return ctrl.Result{}, nil
		}
	} else {
		return r.ReconcileBpfdConfig(ctx, req, bpfdConfig)
	}

	return ctrl.Result{}, nil
}

func (r *BpfdConfigReconciler) ReconcileBpfdConfig(ctx context.Context, req ctrl.Request, bpfdConfig *corev1.ConfigMap) (ctrl.Result, error) {
	bpfdDeployment := &appsv1.DaemonSet{}
	staticBpfdDeployment := operatorinternal.LoadAndConfigureBpfdDs(bpfdConfig)
	r.Logger.V(1).Info("StaticBpfdDeployment is", "DS", staticBpfdDeployment)

	if err := r.Get(ctx, types.NamespacedName{Namespace: bpfdConfig.Data["bpfd.namespace"], Name: internal.BpfdDsName}, bpfdDeployment); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Info("Creating Bpfd Daemon")
			// Causes Requeue
			if err := r.Create(ctx, staticBpfdDeployment); err != nil {
				r.Logger.Error(err, "Failed to create Bpfd Daemon")
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
			}
			return ctrl.Result{}, nil
		}

		r.Logger.Error(err, "Failed to get bpfd daemon")
		return ctrl.Result{}, nil
	}

	if !bpfdConfig.DeletionTimestamp.IsZero() {
		controllerutil.RemoveFinalizer(bpfdDeployment, bpfdOperatorFinalizer)

		err := r.Update(ctx, bpfdDeployment)
		if err != nil {
			r.Logger.Error(err, "failed removing bpfd-operator finalizer from bpfdDs")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}

		if err = r.Delete(ctx, bpfdDeployment); err != nil {
			r.Logger.Error(err, "failed deleting bpfd DS")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}
	}

	if !reflect.DeepEqual(staticBpfdDeployment.Spec, bpfdDeployment.Spec) {
		r.Logger.Info("Reconciling bpfd")

		// Causes Requeue
		if err := r.Update(ctx, staticBpfdDeployment); err != nil {
			r.Logger.Error(err, "failed reconciling bpfd deployment")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}
	}

	return ctrl.Result{}, nil
}

// Only reconcile on bpfd-daemon Daemonset events.
func bpfdDaemonPredicate() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetName() == internal.BpfdDsName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetName() == internal.BpfdDsName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetName() == internal.BpfdDsName
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetName() == internal.BpfdDsName
		},
	}
}

// Only reconcile on bpfd-config configmap events.
func bpfdConfigPredicate() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetName() == internal.BpfdConfigName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetName() == internal.BpfdConfigName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetName() == internal.BpfdConfigName
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetName() == internal.BpfdConfigName
		},
	}
}

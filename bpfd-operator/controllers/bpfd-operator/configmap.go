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
	"io"
	"os"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/bpfd-dev/bpfd/bpfd-operator/internal"
)

// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=bpfd.io,resources=configmaps/finalizers,verbs=update

type BpfdConfigReconciler struct {
	ReconcilerCommon
	StaticBpfdDsPath string
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
		if updated := controllerutil.AddFinalizer(bpfdConfig, "bpfd.io.operator/finalizer"); updated {
			if err := r.Update(ctx, bpfdConfig); err != nil {
				r.Logger.Error(err, "failed adding bpfd-operator finalizer to bpfd config")
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
			}
		} else {
			return r.ReconcileBpfdConfig(ctx, req, bpfdConfig)
		}
	}

	return ctrl.Result{}, nil
}

func (r *BpfdConfigReconciler) ReconcileBpfdConfig(ctx context.Context, req ctrl.Request, bpfdConfig *corev1.ConfigMap) (ctrl.Result, error) {
	bpfdDeployment := &appsv1.DaemonSet{}
	staticBpfdDeployment := LoadAndConfigureBpfdDs(bpfdConfig, r.StaticBpfdDsPath)
	r.Logger.V(1).Info("StaticBpfdDeployment is", "DS", staticBpfdDeployment)

	if err := r.Get(ctx, types.NamespacedName{Namespace: bpfdConfig.Namespace, Name: internal.BpfdDsName}, bpfdDeployment); err != nil {
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
		r.Logger.Info("Deleting bpfd daemon and config")
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

		controllerutil.RemoveFinalizer(bpfdConfig, bpfdOperatorFinalizer)
		err = r.Update(ctx, bpfdConfig)
		if err != nil {
			r.Logger.Error(err, "failed removing bpfd-operator finalizer from bpfd config")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}

		return ctrl.Result{}, nil
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

func LoadAndConfigureBpfdDs(config *corev1.ConfigMap, path string) *appsv1.DaemonSet {
	// Load static bpfd deployment from disk
	file, err := os.Open(path)
	if err != nil {
		panic(err)
	}

	b, err := io.ReadAll(file)
	if err != nil {
		panic(err)
	}

	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, _ := decode(b, nil, nil)

	staticBpfdDeployment := obj.(*appsv1.DaemonSet)

	// Runtime Configurable fields
	bpfdImage := config.Data["bpfd.image"]
	bpfdAgentImage := config.Data["bpfd.agent.image"]
	bpfdLogLevel := config.Data["bpfd.log.level"]

	// Annotate the log level on the ds so we get automatic restarts on changes.
	if staticBpfdDeployment.Spec.Template.ObjectMeta.Annotations == nil {
		staticBpfdDeployment.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
	}
	staticBpfdDeployment.Spec.Template.ObjectMeta.Annotations["bpfd.io.bpfd.loglevel"] = bpfdLogLevel
	staticBpfdDeployment.Name = "bpfd-daemon"
	staticBpfdDeployment.Namespace = config.Namespace
	staticBpfdDeployment.Spec.Template.Spec.Containers[0].Image = bpfdImage
	staticBpfdDeployment.Spec.Template.Spec.Containers[1].Image = bpfdAgentImage
	controllerutil.AddFinalizer(staticBpfdDeployment, "bpfd.io.operator/finalizer")

	return staticBpfdDeployment
}

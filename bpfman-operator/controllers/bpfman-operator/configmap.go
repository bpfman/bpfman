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

package bpfmanoperator

import (
	"context"
	"io"
	"os"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/bpfman/bpfman/bpfman-operator/internal"
)

// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=storage.k8s.io,resources=csidrivers,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=bpfman.io,resources=configmaps/finalizers,verbs=update

type BpfmanConfigReconciler struct {
	ReconcilerCommon
	BpfmanStandardDeployment string
	CsiDriverDeployment      string
}

// SetupWithManager sets up the controller with the Manager.
func (r *BpfmanConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Watch the bpfman-daemon configmap to configure the bpfman deployment across the whole cluster
		For(&corev1.ConfigMap{},
			builder.WithPredicates(bpfmanConfigPredicate())).
		// This only watches the bpfman daemonset which is stored on disk and will be created
		// by this operator. We're doing a manual watch since the operator (As a controller)
		// doesn't really want to have an owner-ref since we don't have a CRD for
		// configuring it, only a configmap.
		Owns(
			&appsv1.DaemonSet{},
			builder.WithPredicates(bpfmanDaemonPredicate())).
		Complete(r)
}

func (r *BpfmanConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = log.FromContext(ctx)

	bpfmanConfig := &corev1.ConfigMap{}
	if err := r.Get(ctx, req.NamespacedName, bpfmanConfig); err != nil {
		if !errors.IsNotFound(err) {
			r.Logger.Error(err, "failed getting bpfman config", "ReconcileObject", req.NamespacedName)
			return ctrl.Result{}, nil
		}
	} else {
		if updated := controllerutil.AddFinalizer(bpfmanConfig, internal.BpfmanOperatorFinalizer); updated {
			if err := r.Update(ctx, bpfmanConfig); err != nil {
				r.Logger.Error(err, "failed adding bpfman-operator finalizer to bpfman config")
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
			}
		} else {
			return r.ReconcileBpfmanConfig(ctx, req, bpfmanConfig)
		}
	}

	return ctrl.Result{}, nil
}

func (r *BpfmanConfigReconciler) ReconcileBpfmanConfig(ctx context.Context, req ctrl.Request, bpfmanConfig *corev1.ConfigMap) (ctrl.Result, error) {
	bpfmanDeployment := &appsv1.DaemonSet{}

	staticBpfmanDeployment := LoadAndConfigureBpfmanDs(bpfmanConfig, r.BpfmanStandardDeployment)
	r.Logger.V(1).Info("StaticBpfmanDeployment with CSI", "DS", staticBpfmanDeployment)
	bpfmanCsiDriver := &storagev1.CSIDriver{}
	// one-shot try to create bpfman's CSIDriver object if it doesn't exist, does not re-trigger reconcile.
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: internal.BpfmanCsiDriverName}, bpfmanCsiDriver); err != nil {
		if errors.IsNotFound(err) {
			bpfmanCsiDriver = LoadCsiDriver(r.CsiDriverDeployment)

			r.Logger.Info("Creating Bpfman csi driver object")
			if err := r.Create(ctx, bpfmanCsiDriver); err != nil {
				r.Logger.Error(err, "Failed to create Bpfman csi driver")
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
			}
		}
	}

	if err := r.Get(ctx, types.NamespacedName{Namespace: bpfmanConfig.Namespace, Name: internal.BpfmanDsName}, bpfmanDeployment); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Info("Creating Bpfman Daemon")
			// Causes Requeue
			if err := r.Create(ctx, staticBpfmanDeployment); err != nil {
				r.Logger.Error(err, "Failed to create Bpfman Daemon")
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
			}
			return ctrl.Result{}, nil
		}

		r.Logger.Error(err, "Failed to get bpfman daemon")
		return ctrl.Result{}, nil
	}

	if !bpfmanConfig.DeletionTimestamp.IsZero() {
		r.Logger.Info("Deleting bpfman daemon and config")
		controllerutil.RemoveFinalizer(bpfmanDeployment, internal.BpfmanOperatorFinalizer)

		err := r.Update(ctx, bpfmanDeployment)
		if err != nil {
			r.Logger.Error(err, "failed removing bpfman-operator finalizer from bpfmanDs")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}

		bpfmanCsiDriver := &storagev1.CSIDriver{}

		// one-shot try to delete bpfman's CSIDriver object only if it exists.
		if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: internal.BpfmanCsiDriverName}, bpfmanCsiDriver); err == nil {
			r.Logger.Info("Deleting Bpfman csi driver object")
			if err := r.Delete(ctx, bpfmanCsiDriver); err != nil {
				r.Logger.Error(err, "Failed to delete Bpfman csi driver")
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
			}
		}

		if err = r.Delete(ctx, bpfmanDeployment); err != nil {
			r.Logger.Error(err, "failed deleting bpfman DS")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}

		controllerutil.RemoveFinalizer(bpfmanConfig, internal.BpfmanOperatorFinalizer)
		err = r.Update(ctx, bpfmanConfig)
		if err != nil {
			r.Logger.Error(err, "failed removing bpfman-operator finalizer from bpfman config")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}

		return ctrl.Result{}, nil
	}

	if !reflect.DeepEqual(staticBpfmanDeployment.Spec, bpfmanDeployment.Spec) {
		r.Logger.Info("Reconciling bpfman")

		// Causes Requeue
		if err := r.Update(ctx, staticBpfmanDeployment); err != nil {
			r.Logger.Error(err, "failed reconciling bpfman deployment")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}
	}

	return ctrl.Result{}, nil
}

// Only reconcile on bpfman-daemon Daemonset events.
func bpfmanDaemonPredicate() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetName() == internal.BpfmanDsName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetName() == internal.BpfmanDsName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetName() == internal.BpfmanDsName
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetName() == internal.BpfmanDsName
		},
	}
}

// Only reconcile on bpfman-config configmap events.
func bpfmanConfigPredicate() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetName() == internal.BpfmanConfigName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetName() == internal.BpfmanConfigName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetName() == internal.BpfmanConfigName
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetName() == internal.BpfmanConfigName
		},
	}
}

func LoadCsiDriver(path string) *storagev1.CSIDriver {
	// Load static bpfman deployment from disk
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

	return obj.(*storagev1.CSIDriver)
}

func LoadAndConfigureBpfmanDs(config *corev1.ConfigMap, path string) *appsv1.DaemonSet {
	// Load static bpfman deployment from disk
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

	staticBpfmanDeployment := obj.(*appsv1.DaemonSet)

	// Runtime Configurable fields
	bpfmanImage := config.Data["bpfman.image"]
	bpfmanAgentImage := config.Data["bpfman.agent.image"]
	bpfmanLogLevel := config.Data["bpfman.log.level"]
	bpfmanAgentLogLevel := config.Data["bpfman.agent.log.level"]

	// Annotate the log level on the ds so we get automatic restarts on changes.
	if staticBpfmanDeployment.Spec.Template.ObjectMeta.Annotations == nil {
		staticBpfmanDeployment.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
	}

	staticBpfmanDeployment.Spec.Template.ObjectMeta.Annotations["bpfman.io.bpfman.loglevel"] = bpfmanLogLevel
	staticBpfmanDeployment.Spec.Template.ObjectMeta.Annotations["bpfman.io.bpfman.agent.loglevel"] = bpfmanAgentLogLevel
	staticBpfmanDeployment.Name = "bpfman-daemon"
	staticBpfmanDeployment.Namespace = config.Namespace
	staticBpfmanDeployment.Spec.Template.Spec.Containers[0].Image = bpfmanImage
	staticBpfmanDeployment.Spec.Template.Spec.Containers[1].Image = bpfmanAgentImage
	controllerutil.AddFinalizer(staticBpfmanDeployment, internal.BpfmanOperatorFinalizer)

	return staticBpfmanDeployment
}

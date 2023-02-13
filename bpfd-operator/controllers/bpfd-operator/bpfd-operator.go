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
	"fmt"
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	bpfdagent "github.com/redhat-et/bpfd/bpfd-operator/controllers/bpfd-agent"
	"github.com/redhat-et/bpfd/bpfd-operator/internal"
)

type BpfProgramConfigConditionType string

const (
	bpfdOperatorFinalizer                                       = "bpfd.io.operator/finalizer"
	retryDurationOperator                                       = 5 * time.Second
	BpfProgConfigNotYetLoaded     BpfProgramConfigConditionType = "NotYetLoaded"
	BpfProgConfigReconcileError   BpfProgramConfigConditionType = "ReconcileError"
	BpfProgConfigReconcileSuccess BpfProgramConfigConditionType = "ReconcileSuccess"
	BpfProgConfigDeleteError      BpfProgramConfigConditionType = "DeleteError"
)

func (b BpfProgramConfigConditionType) Condition(message string) metav1.Condition {
	cond := metav1.Condition{}

	switch b {
	case BpfProgConfigNotYetLoaded:
		if len(message) == 0 {
			message = "Waiting for BpfProgramConfig Object to be reconciled to all nodes"
		}

		cond = metav1.Condition{
			Type:    string(BpfProgConfigNotYetLoaded),
			Status:  metav1.ConditionTrue,
			Reason:  "ProgramsNotYetLoaded",
			Message: message,
		}
	case BpfProgConfigReconcileError:
		if len(message) == 0 {
			message = "bpfProgramReconciliation failed"
		}

		cond = metav1.Condition{
			Type:    string(BpfProgConfigReconcileError),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileError",
			Message: message,
		}
	case BpfProgConfigReconcileSuccess:
		if len(message) == 0 {
			message = "bpfProgramReconciliation Succeeded on all nodes"
		}

		cond = metav1.Condition{
			Type:    string(BpfProgConfigReconcileSuccess),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileSuccess",
			Message: message,
		}
	case BpfProgConfigDeleteError:
		if len(message) == 0 {
			message = "bpfProgramConfig Deletion failed"
		}

		cond = metav1.Condition{
			Type:    string(BpfProgConfigDeleteError),
			Status:  metav1.ConditionTrue,
			Reason:  "DeleteError",
			Message: message,
		}
	}

	return cond
}

// BpfProgramConfigReconciler reconciles a BpfProgramConfig object
type BpfProgramConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

//+kubebuilder:rbac:groups=bpfd.io,resources=bpfprograms,verbs=get;list;watch
//+kubebuilder:rbac:groups=bpfd.io,resources=bpfprogramconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfd.io,resources=bpfprogramconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfd.io,resources=bpfprogramconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *BpfProgramConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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

	BpfProgramConfig := &bpfdiov1alpha1.BpfProgramConfig{}
	if err := r.Get(ctx, req.NamespacedName, BpfProgramConfig); err != nil {
		// list all BpfProgramConfig objects with
		if errors.IsNotFound(err) {
			// TODO(astoycos) we could simplify this logic by making the name of the
			// generated bpfProgram object a bit more deterministic
			bpfProgram := &bpfdiov1alpha1.BpfProgram{}
			if err := r.Get(ctx, req.NamespacedName, bpfProgram); err != nil {
				if errors.IsNotFound(err) {
					r.Logger.V(1).Info("bpfProgram not found stale reconcile, exiting", "bpfProgramName", req.NamespacedName)
				} else {
					r.Logger.Error(err, "failed getting bpfProgram Object", "bpfProgramName", req.NamespacedName)
				}
				return ctrl.Result{}, nil
			}

			// Get owning BpfProgramConfig object from ownerRef
			ownerRef := metav1.GetControllerOf(bpfProgram)
			if ownerRef == nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfProgram Object owner")
			}

			if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: ownerRef.Name}, BpfProgramConfig); err != nil {
				if errors.IsNotFound(err) {
					r.Logger.Info("bpfProgramConfig from ownerRef not found stale reconcile exiting", "bpfProgramConfigName", req.NamespacedName)
				} else {
					r.Logger.Error(err, "failed getting BpfProgramConfig Object from ownerRef", "bpfProgramConfigName", req.NamespacedName)
				}
				return ctrl.Result{}, nil
			}

		} else {
			r.Logger.Error(err, "failed getting BpfProgramConfig Object", "bpfProgramConfigName", req.NamespacedName)
			return ctrl.Result{}, nil
		}
	}

	return r.ReconcileBpfProgramConfig(ctx, BpfProgramConfig)
}

func (r *BpfProgramConfigReconciler) ReconcileBpfProgramConfig(ctx context.Context, bpfProgramConfig *bpfdiov1alpha1.BpfProgramConfig) (ctrl.Result, error) {
	r.Logger.V(1).Info("Reconciling bpfProgramConfig", "bpfProgramConfig", bpfProgramConfig.Name)

	if !controllerutil.ContainsFinalizer(bpfProgramConfig, bpfdOperatorFinalizer) {
		return r.addFinalizer(ctx, bpfProgramConfig.Name)
	}

	// reconcile BpfProgramConfig Object on all other events
	// list all existing bpfProgram state for the given BpfProgramConfig
	bpfPrograms := &bpfdiov1alpha1.BpfProgramList{}

	// Only list bpfPrograms for this BpfProgramConfig
	opts := []client.ListOption{client.MatchingLabels{"owningConfig": bpfProgramConfig.Name}}

	if err := r.List(ctx, bpfPrograms, opts...); err != nil {
		r.Logger.Error(err, "failed getting BpfProgramConfigs for full reconcile")
		return ctrl.Result{}, nil
	}

	// List all nodes since an bpfprogram object will always be created for each
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes, &client.ListOptions{}); err != nil {
		r.Logger.Error(err, "failed getting nodes for full reconcile")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	// Return NotYetLoaded Status if
	// BpfPrograms for each node haven't been created by bpfd-agent and the config isn't
	// being deleted.
	if len(nodes.Items) != len(bpfPrograms.Items) && bpfProgramConfig.DeletionTimestamp.IsZero() {
		// Causes Requeue
		return r.updateStatus(ctx, bpfProgramConfig.Name, BpfProgConfigNotYetLoaded, "")
	}

	failedBpfPrograms := []string{}
	finalApplied := []string{}
	// Make sure no bpfPrograms had any issues in the loading or unloading process
	for _, bpfProgram := range bpfPrograms.Items {

		if controllerutil.ContainsFinalizer(&bpfProgram, bpfdagent.BpfdAgentFinalizer) {
			finalApplied = append(finalApplied, bpfProgram.Name)
		}

		if bpfProgram.Status.Conditions == nil {
			return ctrl.Result{}, nil
		}

		// Get most recent condition
		recentIdx := len(bpfProgram.Status.Conditions) - 1

		condition := bpfProgram.Status.Conditions[recentIdx]

		if condition.Type == string(bpfdagent.BpfProgCondNotLoaded) || condition.Type == string(bpfdagent.BpfProgCondNotUnloaded) {
			failedBpfPrograms = append(failedBpfPrograms, bpfProgram.Name)
		}
	}

	if !bpfProgramConfig.DeletionTimestamp.IsZero() {
		// Only remove bpfd-operator finalizer if all bpfProgram Objects are ready to be pruned  (i.e finalizers have been removed)
		if len(finalApplied) == 0 {
			// Causes Requeue
			return r.removeFinalizer(ctx, bpfProgramConfig.Name)
		}

		// Causes Requeue
		return r.updateStatus(ctx, bpfProgramConfig.Name, BpfProgConfigDeleteError, fmt.Sprintf("bpfProgramConfig Deletion failed on the following bpfProgram Objects: %v",
			finalApplied))
	}

	if len(failedBpfPrograms) != 0 {
		// Causes Requeue
		return r.updateStatus(ctx, bpfProgramConfig.Name, BpfProgConfigReconcileError,
			fmt.Sprintf("bpfProgramReconciliation failed on the following bpfProgram Objects: %v", failedBpfPrograms))
	}

	// Causes Requeue
	return r.updateStatus(ctx, bpfProgramConfig.Name, BpfProgConfigReconcileSuccess, "")
}

func (r *BpfProgramConfigReconciler) ReconcileBpfdConfig(ctx context.Context, req ctrl.Request, bpfdConfig *corev1.ConfigMap) (ctrl.Result, error) {
	bpfdDeployment := &appsv1.DaemonSet{}
	staticBpfdDeployment := internal.LoadAndConfigureBpfdDs(bpfdConfig)
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

func (r *BpfProgramConfigReconciler) updateStatus(ctx context.Context, name string, cond BpfProgramConfigConditionType, message string) (ctrl.Result, error) {
	// Sometimes we end up with a stale bpfProgramConfig due to races, do this
	// get to ensure we're up to date before attempting a finalizer removal.
	prog := &bpfdiov1alpha1.BpfProgramConfig{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: name}, prog); err != nil {
		r.Logger.V(1).Info("failed to get fresh bpfProgramConfig object...requeuing")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}
	meta.SetStatusCondition(&prog.Status.Conditions, cond.Condition(message))

	if err := r.Status().Update(ctx, prog); err != nil {
		r.Logger.V(1).Info("failed to set bpfProgramConfig object status...requeuing")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	return ctrl.Result{}, nil
}

func (r *BpfProgramConfigReconciler) removeFinalizer(ctx context.Context, name string) (ctrl.Result, error) {
	// Sometimes we end up with a stale bpfProgramConfig due to races, do this
	// get to ensure we're up to date before attempting a status update.
	prog := &bpfdiov1alpha1.BpfProgramConfig{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: name}, prog); err != nil {
		r.Logger.V(1).Info("failed to get fresh bpfProgramConfig object...requeuing")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}
	controllerutil.RemoveFinalizer(prog, bpfdOperatorFinalizer)

	err := r.Update(ctx, prog)
	if err != nil {
		r.Logger.V(1).Info("failed removing bpfd-operator finalizer to BpfProgramConfig...requeuing")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	return ctrl.Result{}, nil
}

func (r *BpfProgramConfigReconciler) addFinalizer(ctx context.Context, name string) (ctrl.Result, error) {
	// Sometimes we end up with a stale bpfProgramConfig due to races, do this
	// get to ensure we're up to date before attempting a status update.
	prog := &bpfdiov1alpha1.BpfProgramConfig{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: name}, prog); err != nil {
		r.Logger.V(1).Info("failed to get fresh bpfProgramConfig object...requeuing")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}
	controllerutil.AddFinalizer(prog, bpfdOperatorFinalizer)

	err := r.Update(ctx, prog)
	if err != nil {
		r.Logger.V(1).Info("failed adding bpfd-operator finalizer to BpfProgramConfig...requeuing")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BpfProgramConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfdiov1alpha1.BpfProgramConfig{}).
		// This only watches the bpfd daemonset which is stored on disk and will be created
		// by this operator. We're doing a manual watch since the operator (As a controller)
		// doesn't really want to have an owner-ref since we don't have a CRD for
		// configuring it, only a configmap.
		Owns(
			&appsv1.DaemonSet{},
			builder.WithPredicates(bpfdDaemonPredicate())).
		// Watch the bpfd-daemon configmap to configure the bpfd deployment across the whole cluster
		Watches(
			&source.Kind{Type: &corev1.ConfigMap{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(bpfdConfigPredicate()),
		).
		// Watch all bpfPrograms so we can update status's
		// TODO(Astoycos) Add custom map function here to relate back to bpfProgramConfig
		Watches(
			&source.Kind{Type: &bpfdiov1alpha1.BpfProgram{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(statusChangedPredicate()),
		).
		Complete(r)
}

// Only reconcile if a bpfprogram object's status has been updated.
func statusChangedPredicate() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObject := e.ObjectOld.(*bpfdiov1alpha1.BpfProgram)
			newObject := e.ObjectNew.(*bpfdiov1alpha1.BpfProgram)
			return !reflect.DeepEqual(oldObject.Status, newObject.Status)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}
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

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
	"io/ioutil"
	"os"
	"reflect"
	"time"

	"gopkg.in/yaml.v2"

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
	bpfdiov1alpha1 "github.com/redhat-et/bpfd/bpfd-operator/api/v1alpha1"
	bpfdagent "github.com/redhat-et/bpfd/bpfd-operator/pkg/bpfd-agent"
)

type BpfProgramConfigConditionType string

const (
	bpfdOperatorFinalizer                                       = "bpfd.io.operator/finalizer"
	retryDurationOperator                                       = 10 * time.Second
	BpfdDaemonManifestPath                                      = "./config/bpfd-deployment/daemonset.yaml"
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
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the BpfProgramConfig object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *BpfProgramConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = log.FromContext(ctx)

	r.Logger.Info("bpfd-operator is reconciling", "request", req.String())

	// See if the bpfd deployment was manually edited and reconcile to correct
	// state
	bpfdDeployment := &appsv1.DaemonSet{}
	if err := r.Get(ctx, req.NamespacedName, bpfdDeployment); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Info("reconcile not triggered by bpfd daemon change")
		} else {
			r.Logger.Error(err, "failed getting bpfd-agent node", "NodeName", req.NamespacedName)
			return ctrl.Result{}, nil
		}
	} else {
		// Load static bpfd deployment from disk
		file, err := os.Open(BpfdDaemonManifestPath)
		if err != nil {
			panic(err)
		}

		b, err := ioutil.ReadAll(file)
		if err != nil {
			panic(err)
		}

		staticBpfdDeployment := &appsv1.DaemonSet{}
		err = yaml.Unmarshal(b, staticBpfdDeployment)
		if err != nil {
			panic(err)
		}

		if !reflect.DeepEqual(staticBpfdDeployment.Spec.Template, bpfdDeployment.Spec.Template) {
			if err := r.Update(ctx, staticBpfdDeployment); err != nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed reconciling bpfd deployment %s : %v",
					req.NamespacedName, err)
			}
		}

		return ctrl.Result{}, nil
	}

	BpfProgramConfig := &bpfdiov1alpha1.BpfProgramConfig{}
	if err := r.Get(ctx, req.NamespacedName, BpfProgramConfig); err != nil {
		// list all BpfProgramConfig objects with
		if errors.IsNotFound(err) {
			r.Logger.Info("reconcile not triggered by BpfProgramConfig change")

			// TODO(astoycos) we could simplify this logic by making the name of the
			// generated bpfProgram object a bit more deterministic
			bpfProgram := &bpfdiov1alpha1.BpfProgram{}
			if err := r.Get(ctx, req.NamespacedName, bpfProgram); err != nil {
				if errors.IsNotFound(err) {
					r.Logger.Info("bpfProgram Not found stale event, exiting", "bpfProgramName", req.NamespacedName)
				}
				r.Logger.Error(err, "failed getting bpfProgram Object", "bpfProgramName", req.NamespacedName)
				return ctrl.Result{}, nil
			}

			// Get owning BpfProgramConfig object from ownerRef
			ownerRef := metav1.GetControllerOf(bpfProgram)
			if ownerRef == nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfProgram Object owner")
			}

			if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: ownerRef.Name}, BpfProgramConfig); err != nil {
				r.Logger.Error(err, "failed getting BpfProgramConfig Object from ownerRef")
				return ctrl.Result{}, nil
			}

		} else {
			r.Logger.Error(err, "failed getting BpfProgramConfig Object", "bpfProgramConfigName", req.NamespacedName)
			return ctrl.Result{}, nil
		}
	}

	if !controllerutil.ContainsFinalizer(BpfProgramConfig, bpfdOperatorFinalizer) {
		// 2 Once we have an BpfProgramConfig add out finalizer if not since we own it
		controllerutil.AddFinalizer(BpfProgramConfig, bpfdOperatorFinalizer)

		err := r.Update(ctx, BpfProgramConfig)
		if err != nil {
			r.Logger.Error(err, "failed adding bpfd-operator finalizer to BpfProgramConfig")
			return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
		}
	}

	// reconcile BpfProgramConfig Object on all other events
	// list all existing bpfProgram state for the given BpfProgramConfig
	bpfPrograms := &bpfdiov1alpha1.BpfProgramList{}

	// Only list bpfPrograms for this BpfProgramConfig
	opts := []client.ListOption{client.MatchingLabels{"owningConfig": BpfProgramConfig.Name}}

	if err := r.List(ctx, bpfPrograms, opts...); err != nil {
		r.Logger.Error(err, "failed getting BpfProgramConfigs for full reconcile")
		return ctrl.Result{}, nil
	}

	// List all nodes since an bpfprogram object will always be created
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes, &client.ListOptions{}); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting BpfProgramConfig Nodes for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	// Return NotYetLoaded Status if
	// BpfPrograms for this haven't been created by bpfd-agent and the config isn't
	// being deleted.
	if len(nodes.Items) != len(bpfPrograms.Items) && BpfProgramConfig.DeletionTimestamp.IsZero() {
		r.updateStatus(ctx, BpfProgramConfig, BpfProgConfigNotYetLoaded, "")
		return ctrl.Result{Requeue: false}, nil
	}

	failedBpfPrograms := []string{}
	finalApplied := []string{}
	// Make sure no bpfPrograms had any issues in the loading process
	for _, bpfProgram := range bpfPrograms.Items {

		if controllerutil.ContainsFinalizer(&bpfProgram, bpfdagent.BpfdAgentFinalizer) {
			finalApplied = append(finalApplied, bpfProgram.Name)
		}

		if bpfProgram.Status.Conditions == nil {
			continue
		}

		for _, condition := range bpfProgram.Status.Conditions {
			if condition.Type == string(bpfdagent.BpfProgCondNotLoaded) || condition.Type == string(bpfdagent.BpfProgCondNotUnloaded) {
				failedBpfPrograms = append(failedBpfPrograms, bpfProgram.Name)
			}
		}
	}

	if !BpfProgramConfig.DeletionTimestamp.IsZero() {
		// Only remove bpfd-operator finalizer if all bpfProgram Objects are ready to be pruned  (i.e finalizers have been removed)
		if len(finalApplied) == 0 {
			return r.removeFinalizer(ctx, BpfProgramConfig)
		} else {
			r.updateStatus(ctx, BpfProgramConfig, BpfProgConfigDeleteError, fmt.Sprintf("bpfProgramConfig Deletion failed on the following bpfProgram Objects: %v",
				finalApplied))
		}

		return ctrl.Result{}, nil
	}

	if len(failedBpfPrograms) != 0 {
		r.updateStatus(ctx, BpfProgramConfig, BpfProgConfigReconcileError,
			fmt.Sprintf("bpfProgramReconciliation failed on the following bpfProgram Objects: %v", failedBpfPrograms))
	} else {
		r.updateStatus(ctx, BpfProgramConfig, BpfProgConfigReconcileSuccess, "")
	}

	return ctrl.Result{}, nil
}

func (r *BpfProgramConfigReconciler) updateStatus(ctx context.Context, prog *bpfdiov1alpha1.BpfProgramConfig, cond BpfProgramConfigConditionType, message string) {
	// Sometimes we end up with a stale bpfProgramConfig due to races, do this
	// get to ensure we're up to date before attempting a finalizer removal.
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: prog.Name}, prog); err != nil {
		r.Logger.Error(err, "failed to get fresh bpfProgramConfig object default to existing")
	}
	meta.SetStatusCondition(&prog.Status.Conditions, cond.Condition(message))

	if err := r.Status().Update(ctx, prog); err != nil {
		r.Logger.Error(err, "failed to set bpfProgramConfig object status")
	}
}

func (r *BpfProgramConfigReconciler) removeFinalizer(ctx context.Context, prog *bpfdiov1alpha1.BpfProgramConfig) (ctrl.Result, error) {
	// Sometimes we end up with a stale bpfProgramConfig due to races, do this
	// get to ensure we're up to date before attempting a status update.
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: prog.Name}, prog); err != nil {
		r.Logger.Error(err, "failed to get fresh bpfProgramConfig object default to existing")
	}
	controllerutil.RemoveFinalizer(prog, bpfdOperatorFinalizer)

	err := r.Update(ctx, prog)
	if err != nil {
		r.Logger.Error(err, "failed removing bpfd-operator finalizer to BpfProgramConfig")
		return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BpfProgramConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfdiov1alpha1.BpfProgramConfig{}).
		// This only owns the bpfd-config configmap which should be created in the original deployment
		// Owns(&corev1.ConfigMap{}, builder.WithPredicates(configPredicate())).
		// This only owns the bpfd daemonset which should be created in the original
		// All we do in this configuration loop is make sure it still matches the ds on disk
		// eventually we may need to do some fancy reconciliation here but for now we don't
		Owns(&appsv1.DaemonSet{}, builder.WithPredicates(daemonPredicate())).
		Watches(
			&source.Kind{Type: &bpfdiov1alpha1.BpfProgram{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, statusChangedPredicate())),
		).
		Complete(r)
}

// Only return node updates for our configMap
func statusChangedPredicate() predicate.Funcs {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObject := e.ObjectOld.(*bpfdiov1alpha1.BpfProgram)
			newObject := e.ObjectNew.(*bpfdiov1alpha1.BpfProgram)
			return !reflect.DeepEqual(oldObject.Status, newObject.Status)
		},
	}
}

// Only return node updates for our configMap
func daemonPredicate() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetName() == "bpfd-daemon"
		},
	}
}

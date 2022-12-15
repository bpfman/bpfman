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

	bpfdiov1alpha1 "github.com/redhat-et/bpfd/bpfd-operator/api/v1alpha1"
	bpfdagent "github.com/redhat-et/bpfd/bpfd-operator/pkg/bpfd-agent"
)

type BpfProgramConfigConditionType string

const (
	bpfdOperatorFinalizer                                        = "bpfd.io.operator/finalizer"
	retryDurationOperator                                        = 10 * time.Second
	BpfdDaemonManifestPath                                       = "./config/bpfd-deployment/daemonset.yaml"
	EbpfProgConfigNotYetLoaded     BpfProgramConfigConditionType = "NotYetLoaded"
	EbpfProgConfigReconcileError   BpfProgramConfigConditionType = "ReconcileError"
	EbpfProgConfigReconcileSuccess BpfProgramConfigConditionType = "ReconcileSuccess"
	EbpfProgConfigDeleteError      BpfProgramConfigConditionType = "DeleteError"
)

// BpfProgramConfigReconciler reconciles a BpfProgramConfig object
type BpfProgramConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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
	l := log.FromContext(ctx)

	l.Info("bpfd-operator is reconciling", "request", req.String())

	// See if the bpfd deployment was manually edited and reconcile to correct
	// state
	bpfdDeployment := &appsv1.DaemonSet{}
	if err := r.Get(ctx, req.NamespacedName, bpfdDeployment); err != nil {
		if errors.IsNotFound(err) {
			l.Info("reconcile not triggered by bpfd daemon change")

		} else {
			return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfd-agent node %s : %v",
				req.NamespacedName, err)
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
			l.Info("reconcile not triggered by BpfProgramConfig change")

			// TODO(astoycos) we could simplify this logic by making the name of the
			// generated bpfProgram object a bit more deterministic
			bpfProgram := &bpfdiov1alpha1.BpfProgram{}
			if err := r.Get(ctx, req.NamespacedName, bpfProgram); err != nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfProgram Object %s : %v",
					req.NamespacedName, err)
			}

			// Get owning BpfProgramConfig object from ownerRef
			ownerRef := metav1.GetControllerOf(bpfProgram)
			if ownerRef == nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfProgram Object owner")
			}

			if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: ownerRef.Name}, BpfProgramConfig); err != nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting BpfProgramConfig Object from ownerRef: %v", err)
			}

		} else {
			return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting BpfProgramConfig Object %s : %v",
				req.NamespacedName, err)
		}
	}

	if !controllerutil.ContainsFinalizer(BpfProgramConfig, bpfdOperatorFinalizer) {
		// 2 Once we have an BpfProgramConfig add out finalizer if not since we own it
		controllerutil.AddFinalizer(BpfProgramConfig, bpfdOperatorFinalizer)

		err := r.Update(ctx, BpfProgramConfig)
		if err != nil {
			return ctrl.Result{Requeue: false}, fmt.Errorf("failed adding bpfd-operator finalizer to BpfProgramConfig %s : %v",
				BpfProgramConfig.Name, err)
		}
	}

	// inline function for updating the status of the bpfProgramObject
	updateStatusFunc := func(condition metav1.Condition) {
		meta.SetStatusCondition(&BpfProgramConfig.Status.Conditions, condition)

		if err := r.Status().Update(ctx, BpfProgramConfig); err != nil {
			l.Error(err, "failed to set bpfProgram object status")
		}
	}

	// reconcile BpfProgramConfig Object on all other events
	// list all existing bpfProgram state for the given BpfProgramConfig
	bpfPrograms := &bpfdiov1alpha1.BpfProgramList{}

	// Only list bpfPrograms for this BpfProgramConfig
	opts := []client.ListOption{client.MatchingLabels{"owningConfig": BpfProgramConfig.Name}}

	if err := r.List(ctx, bpfPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting BpfProgramConfigs for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	// List all nodes since an bpfprogram object will always be created
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes, &client.ListOptions{}); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting BpfProgramConfig Nodes for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	// Return NotYetLoaded Status
	// BpfPrograms for this haven't been created by bpfd-agent
	if len(nodes.Items) != len(bpfPrograms.Items) {
		notYetLoaded := metav1.Condition{
			Type:    string(EbpfProgConfigNotYetLoaded),
			Status:  metav1.ConditionTrue,
			Reason:  "ProgramsNotYetLoaded",
			Message: "Waiting for BpfProgramConfig Object to be reconciled to all nodes",
		}

		updateStatusFunc(notYetLoaded)

		return ctrl.Result{Requeue: false}, nil
	}

	failedBpfPrograms := []string{}
	finalApplied := []string{}
	// Make sure no bpfPrograms hade any issues in the loading process
	for _, bpfProgram := range bpfPrograms.Items {

		if controllerutil.ContainsFinalizer(&bpfProgram, bpfdagent.BpfdAgentFinalizer) {
			finalApplied = append(finalApplied, bpfProgram.Name)
		}

		if bpfProgram.Status.Conditions == nil {
			continue
		}

		for _, condition := range bpfProgram.Status.Conditions {
			if condition.Type == string(bpfdagent.EbpfProgCondNotLoaded) || condition.Type == string(bpfdagent.EbpfProgCondNotUnloaded) {
				failedBpfPrograms = append(failedBpfPrograms, bpfProgram.Name)
			}
		}
	}

	if !BpfProgramConfig.DeletionTimestamp.IsZero() {
		// Only remove bpfd-operator finalizer if all bpfProgram Objects are ready to be pruned  (i.e finalizers have been removed)
		if len(finalApplied) == 0 {
			controllerutil.RemoveFinalizer(BpfProgramConfig, bpfdOperatorFinalizer)

			err := r.Update(ctx, BpfProgramConfig)
			if err != nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed adding bpfd-operator finalizer to BpfProgramConfig %s : %v",
					BpfProgramConfig.Name, err)
			}
		} else {
			ReconcileError := metav1.Condition{
				Type:   string(EbpfProgConfigDeleteError),
				Status: metav1.ConditionTrue,
				Reason: "DeleteError",
				Message: fmt.Sprintf("bpfProgramConfig Deletion failed on the following bpfProgram Objects: %v",
					finalApplied),
			}

			updateStatusFunc(ReconcileError)
		}

		return ctrl.Result{}, nil
	}

	if len(failedBpfPrograms) != 0 {
		ReconcileError := metav1.Condition{
			Type:   string(EbpfProgConfigReconcileError),
			Status: metav1.ConditionTrue,
			Reason: "ReconcileError",
			Message: fmt.Sprintf("bpfProgramReconciliation failed on the following bpfProgram Objects: %v",
				failedBpfPrograms),
		}

		updateStatusFunc(ReconcileError)
	} else {
		ReconcileSuccess := metav1.Condition{
			Type:    string(EbpfProgConfigReconcileSuccess),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileSuccess",
			Message: "bpfProgramReconciliation Succeeded on all nodes",
		}

		updateStatusFunc(ReconcileSuccess)
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

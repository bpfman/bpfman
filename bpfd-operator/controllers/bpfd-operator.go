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

	bpfdiov1alpha1 "github.com/redhat-et/bpfd/api/v1alpha1"
)

type ebpfProgramConfigConditionType string

const (
	bpfdOperatorFinalizer                                         = "bpfd.io.operator/finalizer"
	retryDurationOperator                                         = 10 * time.Second
	BpfdDaemonManifestPath                                        = "./config/bpfd-deployment/daemonset.yaml"
	ebpfProgConfigNotYetLoaded     ebpfProgramConfigConditionType = "NotYetLoaded"
	ebpfProgConfigReconcileError   ebpfProgramConfigConditionType = "ReconcileError"
	ebpfProgConfigReconcileSuccess ebpfProgramConfigConditionType = "ReconcileSuccess"
)

// EbpfProgramConfigReconciler reconciles a EbpfProgramConfig object
type EbpfProgramConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=bpfd.io,resources=ebpfprogramconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfd.io,resources=ebpfprogramconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfd.io,resources=ebpfprogramconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the EbpfProgramConfig object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *EbpfProgramConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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

	ebpfProgramConfig := &bpfdiov1alpha1.EbpfProgramConfig{}
	if err := r.Get(ctx, req.NamespacedName, ebpfProgramConfig); err != nil {
		// list all ebpfProgramConfig objects with
		if errors.IsNotFound(err) {
			l.Info("reconcile not triggered by ebpfProgramConfig change")

			// TODO(astoycos) we could simplify this logic by making the name of the
			// generated ebpfProgram object a bit more deterministic
			ebpfProgram := &bpfdiov1alpha1.EbpfProgram{}
			if err := r.Get(ctx, req.NamespacedName, ebpfProgram); err != nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting ebpfProgram Object %s : %v",
					req.NamespacedName, err)
			}

			// Get owning ebpfProgramConfig object from ownerRef
			ownerRef := metav1.GetControllerOf(ebpfProgram)
			if ownerRef == nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting ebpfProgram Object owner")
			}

			if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: ownerRef.Name}, ebpfProgramConfig); err != nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting ebpfProgramConfig Object from ownerRef: %v", err)
			}

		} else {
			return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting ebpfProgramConfig Object %s : %v",
				req.NamespacedName, err)
		}
	}

	if controllerutil.ContainsFinalizer(ebpfProgramConfig, bpfdOperatorFinalizer) && !ebpfProgramConfig.DeletionTimestamp.IsZero() {
		// 2 Once we have an ebpfProgramConfig add out finalizer if not since we own it
		controllerutil.RemoveFinalizer(ebpfProgramConfig, bpfdOperatorFinalizer)

		err := r.Update(ctx, ebpfProgramConfig)
		if err != nil {
			return ctrl.Result{Requeue: false}, fmt.Errorf("failed adding bpfd-operator finalizer to ebpfProgramConfig %s : %v",
				ebpfProgramConfig.Name, err)
		}

		return ctrl.Result{}, nil
	} else {
		// 2 Once we have an ebpfProgramConfig add out finalizer if not since we own it
		controllerutil.AddFinalizer(ebpfProgramConfig, bpfdOperatorFinalizer)

		err := r.Update(ctx, ebpfProgramConfig)
		if err != nil {
			return ctrl.Result{Requeue: false}, fmt.Errorf("failed adding bpfd-operator finalizer to ebpfProgramConfig %s : %v",
				ebpfProgramConfig.Name, err)
		}
	}

	// inline function for updating the status of the ebpfProgramObject
	updateStatusFunc := func(condition metav1.Condition) {
		meta.SetStatusCondition(&ebpfProgramConfig.Status.Conditions, condition)

		if err := r.Status().Update(ctx, ebpfProgramConfig); err != nil {
			l.Error(err, "failed to set ebpfProgram object status")
		}
	}

	// reconcile ebpfProgramConfig Object on all other events
	// list all existing ebpfProgram state for the given ebpfProgramConfig
	ebpfPrograms := &bpfdiov1alpha1.EbpfProgramList{}

	// Only list ebpfPrograms for this ebpfProgramConfig
	opts := []client.ListOption{client.MatchingLabels{"owningConfig": ebpfProgramConfig.Name}}

	if err := r.List(ctx, ebpfPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting ebpfProgramConfigs for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes, []client.ListOption{}...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting ebpfProgramConfigs for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	// Return NotYetLoaded Status
	// EbpfPrograms for this haven't been created by bpfd-agent
	if len(nodes.Items) != len(ebpfPrograms.Items) {
		notYetLoaded := metav1.Condition{
			Type:    string(ebpfProgConfigNotYetLoaded),
			Status:  metav1.ConditionTrue,
			Reason:  "ProgramsNotYetLoaded",
			Message: "Waiting for ebpfProgramConfig Object to be reconciled to all nodes",
		}

		updateStatusFunc(notYetLoaded)

		return ctrl.Result{Requeue: false}, nil
	}

	failedEbpfPrograms := []string{}
	// Make sure no ebpfPrograms have any issues issue
	for _, ebpfProgram := range ebpfPrograms.Items {

		if ebpfProgram.Status.Conditions == nil {
			continue
		}

		for _, condition := range ebpfProgram.Status.Conditions {
			if condition.Type == string(ebpfProgCondNotLoaded) || condition.Type == string(ebpfProgCondNotUnloaded) {
				failedEbpfPrograms = append(failedEbpfPrograms, ebpfProgram.Name)
			}
		}
	}

	if len(failedEbpfPrograms) != 0 {
		ReconcileError := metav1.Condition{
			Type:   string(ebpfProgConfigReconcileError),
			Status: metav1.ConditionTrue,
			Reason: "ReconcileError",
			Message: fmt.Sprintf("ebpfProgramReconciliation failed on the following ebpfProgram Objects: %v",
				failedEbpfPrograms),
		}

		updateStatusFunc(ReconcileError)
	} else {
		ReconcileSuccess := metav1.Condition{
			Type:    string(ebpfProgConfigReconcileSuccess),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileSuccess",
			Message: "ebpfProgramReconciliation Succeeded on all nodes",
		}

		updateStatusFunc(ReconcileSuccess)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EbpfProgramConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfdiov1alpha1.EbpfProgramConfig{}).
		// This only owns the bpfd-config configmap which should be created in the original deployment
		// Owns(&corev1.ConfigMap{}, builder.WithPredicates(configPredicate())).
		// This only owns the bpfd daemonset which should be created in the original
		// All we do in this configuration loop is make sure it still matches the ds on disk
		// eventually we may need to do some fancy reconciliation here but for now we don't
		Owns(&appsv1.DaemonSet{}, builder.WithPredicates(daemonPredicate())).
		Watches(
			&source.Kind{Type: &bpfdiov1alpha1.EbpfProgram{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, statusChangedPredicate())),
		).
		Complete(r)
}

// Only return node updates for our configMap
func statusChangedPredicate() predicate.Funcs {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObject := e.ObjectOld.(*bpfdiov1alpha1.EbpfProgram)
			newObject := e.ObjectNew.(*bpfdiov1alpha1.EbpfProgram)
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

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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/bpfman/bpfman/bpfman-operator/internal"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestBpfmanConfigReconcileAndDelete(t *testing.T) {
	var (
		name          = "bpfman-config"
		namespace     = "bpfman"
		staticDsPath  = "../../config/bpfman-deployment/daemonset.yaml"
		staticCsiPath = "../../config/bpfman-deployment/csidriverinfo.yaml"
		ctx           = context.TODO()
	)

	// A configMap for bpfman with metadata and spec.
	bpfmanConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			"bpfman.agent.image":     "BPFMAN_AGENT_IS_SCARY",
			"bpfman.image":           "FAKE-IMAGE",
			"bpfman.agent.log.level": "FAKE",
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{bpfmanConfig}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.ConfigMap{})
	s.AddKnownTypes(appsv1.SchemeGroupVersion, &appsv1.DaemonSet{})
	s.AddKnownTypes(storagev1.SchemeGroupVersion, &storagev1.CSIDriver{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	rc := ReconcilerCommon{
		Client: cl,
		Scheme: s,
	}

	// The expected bpfman daemonset
	expectedBpfmanDs := LoadAndConfigureBpfmanDs(bpfmanConfig, staticDsPath)

	// Set development Logger so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	// Create a ReconcileMemcached object with the scheme and fake client.
	r := &BpfmanConfigReconciler{ReconcilerCommon: rc, BpfmanStandardDeployment: staticDsPath, CsiDriverDeployment: staticCsiPath}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}

	// First reconcile will add bpfman-operator finalizer to bpfman configmap
	res, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check the BpfProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: bpfmanConfig.Name, Namespace: namespace}, bpfmanConfig)
	require.NoError(t, err)

	// Check the bpfman-operator finalizer was successfully added
	require.Contains(t, bpfmanConfig.GetFinalizers(), internal.BpfmanOperatorFinalizer)

	// Second reconcile will create bpfman daemonset
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check the bpfman daemonset was created successfully
	actualBpfmanDs := &appsv1.DaemonSet{}

	err = cl.Get(ctx, types.NamespacedName{Name: expectedBpfmanDs.Name, Namespace: namespace}, actualBpfmanDs)
	require.NoError(t, err)

	// Check the bpfman daemonset was created with the correct configuration.
	require.True(t, reflect.DeepEqual(actualBpfmanDs.Spec, expectedBpfmanDs.Spec))

	// Delete the bpfman configmap
	err = cl.Delete(ctx, bpfmanConfig)
	require.NoError(t, err)

	// Third reconcile will delete bpfman daemonset
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	err = cl.Get(ctx, types.NamespacedName{Name: expectedBpfmanDs.Name, Namespace: namespace}, actualBpfmanDs)
	require.True(t, errors.IsNotFound(err))

	err = cl.Get(ctx, types.NamespacedName{Name: bpfmanConfig.Name, Namespace: namespace}, bpfmanConfig)
	require.True(t, errors.IsNotFound(err))
}

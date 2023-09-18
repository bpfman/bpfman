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
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/bpfd-dev/bpfd/bpfd-operator/internal"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestBpfdConfigReconcileAndDelete(t *testing.T) {
	var (
		name         = "bpfd-config"
		namespace    = "bpfd"
		staticDsPath = "../../config/bpfd-deployment/daemonset.yaml"
		ctx          = context.TODO()
	)

	// A configMap for bpfd with metadata and spec.
	bpfdConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			"bpfd.agent.image":     "BPFD_AGENT_IS_SCARY",
			"bpfd.image":           "FAKE-IMAGE",
			"bpfd.agent.log.level": "FAKE",
			"bpfd.toml": `
[tls] # REQUIRED
ca_cert = "/etc/bpfd/certs/ca/ca.crt"
cert = "/etc/bpfd/certs/bpfd/tls.crt"
key = "/etc/bpfd/certs/bpfd/tls.key"
client_cert = "/etc/bpfd/certs/bpfd-client/tls.crt"
client_key = "/etc/bpfd/certs/bpfd-client/tls.key"
			`,
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{bpfdConfig}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.ConfigMap{})
	s.AddKnownTypes(appsv1.SchemeGroupVersion, &appsv1.DaemonSet{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	rc := ReconcilerCommon{
		Client: cl,
		Scheme: s,
	}

	// The expected bpfd daemonset
	expectedBpfdDs := LoadAndConfigureBpfdDs(bpfdConfig, staticDsPath)

	// Set development Logger so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	// Create a ReconcileMemcached object with the scheme and fake client.
	r := &BpfdConfigReconciler{ReconcilerCommon: rc, BpfdStandardDeployment: staticDsPath}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}

	// First reconcile will add bpfd-operator finalizer to bpfd configmap
	res, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check the BpfProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: bpfdConfig.Name, Namespace: namespace}, bpfdConfig)
	require.NoError(t, err)

	// Check the bpfd-operator finalizer was successfully added
	require.Contains(t, bpfdConfig.GetFinalizers(), internal.BpfdOperatorFinalizer)

	// Second reconcile will create bpfd daemonset
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check the bpfd daemonset was created successfully
	actualBpfdDs := &appsv1.DaemonSet{}

	err = cl.Get(ctx, types.NamespacedName{Name: expectedBpfdDs.Name, Namespace: namespace}, actualBpfdDs)
	require.NoError(t, err)

	// Check the bpfd daemonset was created with the correct configuration.
	require.True(t, reflect.DeepEqual(actualBpfdDs.Spec, expectedBpfdDs.Spec))

	// Delete the bpfd configmap
	err = cl.Delete(ctx, bpfdConfig)
	require.NoError(t, err)

	// Third reconcile will delete bpfd daemonset
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	err = cl.Get(ctx, types.NamespacedName{Name: expectedBpfdDs.Name, Namespace: namespace}, actualBpfdDs)
	require.True(t, errors.IsNotFound(err))

	err = cl.Get(ctx, types.NamespacedName{Name: bpfdConfig.Name, Namespace: namespace}, bpfdConfig)
	require.True(t, errors.IsNotFound(err))
}

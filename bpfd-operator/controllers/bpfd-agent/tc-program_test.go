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

package bpfdagent

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	bpfdiov1alpha1 "github.com/bpfd-dev/bpfd/bpfd-operator/apis/v1alpha1"
	bpfdagentinternal "github.com/bpfd-dev/bpfd/bpfd-operator/controllers/bpfd-agent/internal"
	agenttestutils "github.com/bpfd-dev/bpfd/bpfd-operator/controllers/bpfd-agent/internal/test-utils"
	testutils "github.com/bpfd-dev/bpfd/bpfd-operator/internal/test-utils"

	internal "github.com/bpfd-dev/bpfd/bpfd-operator/internal"

	gobpfd "github.com/bpfd-dev/bpfd/clients/gobpfd/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestTcProgramControllerCreate(t *testing.T) {
	var (
		name         = "fakeTcProgram"
		namespace    = "bpfd"
		bytecodePath = "/tmp/hello.o"
		sectionName  = "test"
		direction    = "ingress"
		fakeNode     = testutils.NewNode("fake-control-plane")
		fakeInt      = "eth0"
		ctx          = context.TODO()
		bpfProgName  = fmt.Sprintf("%s-%s", name, fakeNode.Name)
		bpfdProgId   = bpfdagentinternal.GenIdFromName(fmt.Sprintf("%s-%s", name, fakeInt))
		bpfProg      = &bpfdiov1alpha1.BpfProgram{}
	)
	// A TcProgram object with metadata and spec.
	tc := &bpfdiov1alpha1.TcProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: bpfdiov1alpha1.TcProgramSpec{
			BpfProgramCommon: bpfdiov1alpha1.BpfProgramCommon{
				SectionName:  sectionName,
				NodeSelector: metav1.LabelSelector{},
				ByteCode: bpfdiov1alpha1.BytecodeSelector{
					Path: &bytecodePath,
				},
			},
			InterfaceSelector: bpfdiov1alpha1.InterfaceSelector{
				Interface: &fakeInt,
			},
			Priority:  0,
			Direction: direction,
			ProceedOn: []bpfdiov1alpha1.TcProceedOnValue{
				bpfdiov1alpha1.TcProceedOnValue("pipe"),
				bpfdiov1alpha1.TcProceedOnValue("dispatcher_return"),
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, tc}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfdiov1alpha1.SchemeGroupVersion, tc)
	s.AddKnownTypes(bpfdiov1alpha1.SchemeGroupVersion, &bpfdiov1alpha1.TcProgramList{})
	s.AddKnownTypes(bpfdiov1alpha1.SchemeGroupVersion, &bpfdiov1alpha1.BpfProgram{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	cli := agenttestutils.NewBpfdClientFake()

	rc := ReconcilerCommon{
		Client:           cl,
		Scheme:           s,
		BpfdClient:       cli,
		NodeName:         fakeNode.Name,
		bpfProgram:       &bpfdiov1alpha1.BpfProgram{},
		expectedPrograms: map[string]map[string]string{},
	}

	// Set development Logger so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	// Create a ReconcileMemcached object with the scheme and fake client.
	r := &TcProgramReconciler{ReconcilerCommon: rc, ourNode: fakeNode}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}

	// First reconcile should create the bpf program object
	res, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Check the BpfProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName, Namespace: metav1.NamespaceAll}, bpfProg)
	require.NoError(t, err)

	require.NotEmpty(t, bpfProg)
	// Finalizer is written
	require.Equal(t, r.getFinalizer(), bpfProg.Finalizers[0])
	// Node is set
	require.Equal(t, fakeNode.Name, bpfProg.Spec.Node)
	// Type is set
	require.Equal(t, r.getRecType(), bpfProg.Spec.Type)
	// Require no requeue
	require.False(t, res.Requeue)

	// Second reconcile should create the bpfd Load Request and update the
	// BpfProgram object's 'Programs' field.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	expectedLoadReq := &gobpfd.LoadRequest{
		Common: &gobpfd.LoadRequestCommon{
			Location: &gobpfd.LoadRequestCommon_File{
				File: bytecodePath,
			},
			SectionName: sectionName,
			ProgramType: *internal.Tc.Int32(),
			Id:          &bpfdProgId,
		},
		AttachInfo: &gobpfd.LoadRequest_TcAttachInfo{
			TcAttachInfo: &gobpfd.TCAttachInfo{
				Iface:     fakeInt,
				Priority:  0,
				Direction: direction,
				ProceedOn: []int32{3, 30},
			},
		},
	}
	// Check the bpfLoadRequest was correctly Built
	if !cmp.Equal(expectedLoadReq, cli.LoadRequests[bpfdProgId], protocmp.Transform()) {
		t.Logf("Diff %v", cmp.Diff(expectedLoadReq, cli.LoadRequests[bpfdProgId], protocmp.Transform()))
		t.Fatal("Built bpfd LoadRequest does not match expected")
	}

	// Check that the bpfProgram's programs was correctly updated
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName, Namespace: metav1.NamespaceAll}, bpfProg)
	require.NoError(t, err)

	require.Equal(t, map[string]map[string]string{bpfdProgId: {}}, bpfProg.Spec.Programs)

	// Third reconcile should update the bpfPrograms status to loaded
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check that the bpfProgram's status was correctly updated
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName, Namespace: metav1.NamespaceAll}, bpfProg)
	require.NoError(t, err)

	require.Equal(t, string(bpfdiov1alpha1.BpfProgCondLoaded), bpfProg.Status.Conditions[0].Type)
}

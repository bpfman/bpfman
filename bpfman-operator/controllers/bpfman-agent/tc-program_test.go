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

package bpfmanagent

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanagentinternal "github.com/bpfman/bpfman/bpfman-operator/controllers/bpfman-agent/internal"
	agenttestutils "github.com/bpfman/bpfman/bpfman-operator/controllers/bpfman-agent/internal/test-utils"
	testutils "github.com/bpfman/bpfman/bpfman-operator/internal/test-utils"

	internal "github.com/bpfman/bpfman/bpfman-operator/internal"

	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
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
		name            = "fakeTcProgram"
		namespace       = "bpfman"
		bytecodePath    = "/tmp/hello.o"
		bpfFunctionName = "test"
		direction       = "ingress"
		fakeNode        = testutils.NewNode("fake-control-plane")
		fakeInt         = "eth0"
		ctx             = context.TODO()
		bpfProgName     = fmt.Sprintf("%s-%s-%s", name, fakeNode.Name, fakeInt)
		bpfProg         = &bpfmaniov1alpha1.BpfProgram{}
		fakeUID         = "ef71d42c-aa21-48e8-a697-82391d801a81"
	)
	// A TcProgram object with metadata and spec.
	tc := &bpfmaniov1alpha1.TcProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: bpfmaniov1alpha1.TcProgramSpec{
			BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
				BpfFunctionName: bpfFunctionName,
				NodeSelector:    metav1.LabelSelector{},
				ByteCode: bpfmaniov1alpha1.BytecodeSelector{
					Path: &bytecodePath,
				},
			},
			InterfaceSelector: bpfmaniov1alpha1.InterfaceSelector{
				Interfaces: &[]string{fakeInt},
			},
			Priority:  0,
			Direction: direction,
			ProceedOn: []bpfmaniov1alpha1.TcProceedOnValue{
				bpfmaniov1alpha1.TcProceedOnValue("pipe"),
				bpfmaniov1alpha1.TcProceedOnValue("dispatcher_return"),
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, tc}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, tc)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.TcProgramList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfProgram{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfProgramList{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	cli := agenttestutils.NewBpfmanClientFake()

	rc := ReconcilerCommon{
		Client:       cl,
		Scheme:       s,
		BpfmanClient: cli,
		NodeName:     fakeNode.Name,
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
	// owningConfig Label was correctly set
	require.Equal(t, bpfProg.Labels[internal.BpfProgramOwnerLabel], name)
	// node Label was correctly set
	require.Equal(t, bpfProg.Labels[internal.K8sHostLabel], fakeNode.Name)
	// Finalizer is written
	require.Equal(t, r.getFinalizer(), bpfProg.Finalizers[0])
	// Type is set
	require.Equal(t, r.getRecType(), bpfProg.Spec.Type)
	// Require no requeue
	require.False(t, res.Requeue)

	// Update UID of bpfProgram with Fake UID since the fake API server won't
	bpfProg.UID = types.UID(fakeUID)
	err = cl.Update(ctx, bpfProg)
	require.NoError(t, err)

	// Second reconcile should create the bpfman Load Request and update the
	// BpfProgram object's 'Programs' field.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)
	uuid := string(bpfProg.UID)

	expectedLoadReq := &gobpfman.LoadRequest{
		Bytecode: &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
		},
		Name:        bpfFunctionName,
		ProgramType: *internal.Tc.Uint32(),
		Metadata:    map[string]string{internal.UuidMetadataKey: string(uuid), internal.ProgramNameKey: name},
		MapOwnerId:  nil,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TcAttachInfo{
				TcAttachInfo: &gobpfman.TCAttachInfo{
					Iface:     fakeInt,
					Priority:  0,
					Direction: direction,
					ProceedOn: []int32{3, 30},
				},
			},
		},
	}

	// Check that the bpfProgram's programs was correctly updated
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName, Namespace: metav1.NamespaceAll}, bpfProg)
	require.NoError(t, err)

	// prog ID should already have been set
	id, err := bpfmanagentinternal.GetID(bpfProg)
	require.NoError(t, err)

	// Check the bpfLoadRequest was correctly built
	if !cmp.Equal(expectedLoadReq, cli.LoadRequests[int(*id)], protocmp.Transform()) {
		t.Logf("Diff %v", cmp.Diff(expectedLoadReq, cli.LoadRequests[int(*id)], protocmp.Transform()))
		t.Fatal("Built bpfman LoadRequest does not match expected")
	}

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

	require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), bpfProg.Status.Conditions[0].Type)
}

func TestTcProgramControllerCreateMultiIntf(t *testing.T) {
	var (
		name            = "fakeTcProgram"
		namespace       = "bpfman"
		bytecodePath    = "/tmp/hello.o"
		bpfFunctionName = "test"
		direction       = "ingress"
		fakeNode        = testutils.NewNode("fake-control-plane")
		fakeInts        = []string{"eth0", "eth1"}
		ctx             = context.TODO()
		bpfProgName0    = fmt.Sprintf("%s-%s-%s", name, fakeNode.Name, fakeInts[0])
		bpfProgName1    = fmt.Sprintf("%s-%s-%s", name, fakeNode.Name, fakeInts[1])
		bpfProgEth0     = &bpfmaniov1alpha1.BpfProgram{}
		bpfProgEth1     = &bpfmaniov1alpha1.BpfProgram{}
		fakeUID0        = "ef71d42c-aa21-48e8-a697-82391d801a80"
		fakeUID1        = "ef71d42c-aa21-48e8-a697-82391d801a81"
	)
	// A TcProgram object with metadata and spec.
	tc := &bpfmaniov1alpha1.TcProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: bpfmaniov1alpha1.TcProgramSpec{
			BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
				BpfFunctionName: bpfFunctionName,
				NodeSelector:    metav1.LabelSelector{},
				ByteCode: bpfmaniov1alpha1.BytecodeSelector{
					Path: &bytecodePath,
				},
			},
			InterfaceSelector: bpfmaniov1alpha1.InterfaceSelector{
				Interfaces: &fakeInts,
			},
			Priority:  0,
			Direction: direction,
			ProceedOn: []bpfmaniov1alpha1.TcProceedOnValue{
				bpfmaniov1alpha1.TcProceedOnValue("pipe"),
				bpfmaniov1alpha1.TcProceedOnValue("dispatcher_return"),
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, tc}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, tc)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.TcProgramList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfProgram{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfProgramList{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	cli := agenttestutils.NewBpfmanClientFake()

	rc := ReconcilerCommon{
		Client:       cl,
		Scheme:       s,
		BpfmanClient: cli,
		NodeName:     fakeNode.Name,
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

	// First reconcile should create the first bpf program object
	res, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Check the first BpfProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName0, Namespace: metav1.NamespaceAll}, bpfProgEth0)
	require.NoError(t, err)

	require.NotEmpty(t, bpfProgEth0)
	// owningConfig Label was correctly set
	require.Equal(t, bpfProgEth0.Labels[internal.BpfProgramOwnerLabel], name)
	// node Label was correctly set
	require.Equal(t, bpfProgEth0.Labels[internal.K8sHostLabel], fakeNode.Name)
	// Finalizer is written
	require.Equal(t, r.getFinalizer(), bpfProgEth0.Finalizers[0])
	// Type is set
	require.Equal(t, r.getRecType(), bpfProgEth0.Spec.Type)
	// Require no requeue
	require.False(t, res.Requeue)

	// Update UID of bpfProgram with Fake UID since the fake API server won't
	bpfProgEth0.UID = types.UID(fakeUID0)
	err = cl.Update(ctx, bpfProgEth0)
	require.NoError(t, err)

	// Second reconcile should create the bpfman Load Requests for the first bpfProgram and update the prog id.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Third reconcile should set the second bpf program object's status.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Fourth reconcile should create the bpfman Load Requests for the second bpfProgram.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Require no requeue
	require.False(t, res.Requeue)

	// Check the Second BpfProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName1, Namespace: metav1.NamespaceAll}, bpfProgEth1)
	require.NoError(t, err)

	require.NotEmpty(t, bpfProgEth1)
	// owningConfig Label was correctly set
	require.Equal(t, bpfProgEth1.Labels[internal.BpfProgramOwnerLabel], name)
	// node Label was correctly set
	require.Equal(t, bpfProgEth1.Labels[internal.K8sHostLabel], fakeNode.Name)
	// Finalizer is written
	require.Equal(t, r.getFinalizer(), bpfProgEth1.Finalizers[0])
	// Type is set
	require.Equal(t, r.getRecType(), bpfProgEth1.Spec.Type)

	// Update UID of bpfProgram with Fake UID since the fake API server won't
	bpfProgEth1.UID = types.UID(fakeUID1)
	err = cl.Update(ctx, bpfProgEth1)
	require.NoError(t, err)

	// Fifth reconcile should create the second bpfProgram.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Sixth reconcile should update the second bpfProgram's status.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	uuid0 := string(bpfProgEth0.UID)

	expectedLoadReq0 := &gobpfman.LoadRequest{
		Bytecode: &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
		},
		Name:        bpfFunctionName,
		ProgramType: *internal.Tc.Uint32(),
		Metadata:    map[string]string{internal.UuidMetadataKey: string(uuid0), internal.ProgramNameKey: name},
		MapOwnerId:  nil,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TcAttachInfo{
				TcAttachInfo: &gobpfman.TCAttachInfo{
					Iface:     fakeInts[0],
					Priority:  0,
					Direction: direction,
					ProceedOn: []int32{3, 30},
				},
			},
		},
	}

	uuid1 := string(bpfProgEth1.UID)

	expectedLoadReq1 := &gobpfman.LoadRequest{
		Bytecode: &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
		},
		Name:        bpfFunctionName,
		ProgramType: *internal.Tc.Uint32(),
		Metadata:    map[string]string{internal.UuidMetadataKey: string(uuid1), internal.ProgramNameKey: name},
		MapOwnerId:  nil,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TcAttachInfo{
				TcAttachInfo: &gobpfman.TCAttachInfo{
					Iface:     fakeInts[1],
					Priority:  0,
					Direction: direction,
					ProceedOn: []int32{3, 30},
				},
			},
		},
	}

	// Check that the bpfProgram's maps was correctly updated
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName0, Namespace: metav1.NamespaceAll}, bpfProgEth0)
	require.NoError(t, err)

	// prog ID should already have been set
	id0, err := bpfmanagentinternal.GetID(bpfProgEth0)
	require.NoError(t, err)

	// Check that the bpfProgram's maps was correctly updated
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName1, Namespace: metav1.NamespaceAll}, bpfProgEth1)
	require.NoError(t, err)

	// prog ID should already have been set
	id1, err := bpfmanagentinternal.GetID(bpfProgEth1)
	require.NoError(t, err)

	// Check the bpfLoadRequest was correctly built
	if !cmp.Equal(expectedLoadReq0, cli.LoadRequests[int(*id0)], protocmp.Transform()) {
		t.Logf("Diff %v", cmp.Diff(expectedLoadReq0, cli.LoadRequests[int(*id0)], protocmp.Transform()))
		t.Fatal("Built bpfman LoadRequest does not match expected")
	}

	// Check the bpfLoadRequest was correctly built
	if !cmp.Equal(expectedLoadReq1, cli.LoadRequests[int(*id1)], protocmp.Transform()) {
		t.Logf("Diff %v", cmp.Diff(expectedLoadReq1, cli.LoadRequests[int(*id1)], protocmp.Transform()))
		t.Fatal("Built bpfman LoadRequest does not match expected")
	}

	// Check that the bpfProgram's maps was correctly updated
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName0, Namespace: metav1.NamespaceAll}, bpfProgEth0)
	require.NoError(t, err)

	// Check that the bpfProgram's maps was correctly updated
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName1, Namespace: metav1.NamespaceAll}, bpfProgEth1)
	require.NoError(t, err)

	// Third reconcile should update the bpfPrograms status to loaded
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check that the bpfProgram's status was correctly updated
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName0, Namespace: metav1.NamespaceAll}, bpfProgEth0)
	require.NoError(t, err)

	require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), bpfProgEth0.Status.Conditions[0].Type)

	// Check that the bpfProgram's status was correctly updated
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName1, Namespace: metav1.NamespaceAll}, bpfProgEth1)
	require.NoError(t, err)

	require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), bpfProgEth1.Status.Conditions[0].Type)
}

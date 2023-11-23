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
	internal "github.com/bpfman/bpfman/bpfman-operator/internal"
	testutils "github.com/bpfman/bpfman/bpfman-operator/internal/test-utils"

	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Runs the XdpProgramControllerCreate test.  If multiInterface = true, it
// installs the program on two interfaces.  If multiCondition == true, it runs
// it with an error case in which the program object has multiple conditions.
func xdpProgramControllerCreate(t *testing.T, multiInterface bool, multiCondition bool) {
	var (
		name            = "fakeXdpProgram"
		namespace       = "bpfman"
		bytecodePath    = "/tmp/hello.o"
		bpfFunctionName = "test"
		fakeNode        = testutils.NewNode("fake-control-plane")
		fakeInt0        = "eth0"
		fakeInt1        = "eth1"
		ctx             = context.TODO()
		bpfProgName0    = fmt.Sprintf("%s-%s-%s", name, fakeNode.Name, fakeInt0)
		bpfProgName1    = fmt.Sprintf("%s-%s-%s", name, fakeNode.Name, fakeInt1)
		bpfProgEth0     = &bpfmaniov1alpha1.BpfProgram{}
		bpfProgEth1     = &bpfmaniov1alpha1.BpfProgram{}
		fakeUID0        = "ef71d42c-aa21-48e8-a697-82391d801a80"
		fakeUID1        = "ef71d42c-aa21-48e8-a697-82391d801a81"
	)

	var fakeInts []string
	if multiInterface {
		fakeInts = []string{fakeInt0, fakeInt1}
	} else {
		fakeInts = []string{fakeInt0}
	}

	// A XdpProgram object with metadata and spec.
	xdp := &bpfmaniov1alpha1.XdpProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: bpfmaniov1alpha1.XdpProgramSpec{
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
			Priority: 0,
			ProceedOn: []bpfmaniov1alpha1.XdpProceedOnValue{bpfmaniov1alpha1.XdpProceedOnValue("pass"),
				bpfmaniov1alpha1.XdpProceedOnValue("dispatcher_return"),
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, xdp}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, xdp)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.XdpProgramList{})
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
	r := &XdpProgramReconciler{ReconcilerCommon: rc, ourNode: fakeNode}

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

	// Second reconcile should create the bpfman Load Requests for the first bpfProgram.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	uuid0 := string(bpfProgEth0.UID)

	expectedLoadReq0 := &gobpfman.LoadRequest{
		Bytecode: &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
		},
		Name:        bpfFunctionName,
		ProgramType: *internal.Xdp.Uint32(),
		Metadata:    map[string]string{internal.UuidMetadataKey: uuid0, internal.ProgramNameKey: name},
		MapOwnerId:  nil,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_XdpAttachInfo{
				XdpAttachInfo: &gobpfman.XDPAttachInfo{
					Priority:  0,
					Iface:     fakeInts[0],
					ProceedOn: []int32{2, 31},
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

	// Check the bpfLoadRequest was correctly built
	if !cmp.Equal(expectedLoadReq0, cli.LoadRequests[int(*id0)], protocmp.Transform()) {
		t.Logf("Diff %v", cmp.Diff(expectedLoadReq0, cli.LoadRequests[int(*id0)], protocmp.Transform()))
		t.Fatal("Built bpfman LoadRequest does not match expected")
	}

	// NOTE: THIS IS A TEST FOR AN ERROR PATH. THERE SHOULD NEVER BE MORE THAN
	// ONE CONDITION.
	if multiCondition {
		// Add some random conditions and verify that the condition still gets
		// updated correctly.
		meta.SetStatusCondition(&bpfProgEth0.Status.Conditions, bpfmaniov1alpha1.BpfProgCondBytecodeSelectorError.Condition())
		if err := r.Status().Update(ctx, bpfProgEth0); err != nil {
			r.Logger.V(1).Info("failed to set KprobeProgram object status")
		}
		meta.SetStatusCondition(&bpfProgEth0.Status.Conditions, bpfmaniov1alpha1.BpfProgCondNotSelected.Condition())
		if err := r.Status().Update(ctx, bpfProgEth0); err != nil {
			r.Logger.V(1).Info("failed to set KprobeProgram object status")
		}
		// Make sure we have 2 conditions
		require.Equal(t, 2, len(bpfProgEth0.Status.Conditions))
	}

	// Third reconcile should update the bpfPrograms status to loaded
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Get program object
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName0, Namespace: metav1.NamespaceAll}, bpfProgEth0)
	require.NoError(t, err)

	// Check that the bpfProgram's status was correctly updated
	// Make sure we only have 1 condition now
	require.Equal(t, 1, len(bpfProgEth0.Status.Conditions))
	// Make sure it's the right one.
	require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), bpfProgEth0.Status.Conditions[0].Type)

	if multiInterface {
		// Fourth reconcile should create the second bpfProgram.
		res, err = r.Reconcile(ctx, req)
		if err != nil {
			t.Fatalf("reconcile: (%v)", err)
		}

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
		// Require no requeue
		require.False(t, res.Requeue)

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

		// Require no requeue
		require.False(t, res.Requeue)

		uuid1 := string(bpfProgEth1.UID)

		expectedLoadReq1 := &gobpfman.LoadRequest{
			Bytecode: &gobpfman.BytecodeLocation{
				Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
			},
			Name:        bpfFunctionName,
			ProgramType: *internal.Xdp.Uint32(),
			Metadata:    map[string]string{internal.UuidMetadataKey: uuid1, internal.ProgramNameKey: name},
			MapOwnerId:  nil,
			Attach: &gobpfman.AttachInfo{
				Info: &gobpfman.AttachInfo_XdpAttachInfo{
					XdpAttachInfo: &gobpfman.XDPAttachInfo{
						Priority:  0,
						Iface:     fakeInts[1],
						ProceedOn: []int32{2, 31},
					},
				},
			},
		}

		// Check that the bpfProgram's maps was correctly updated
		err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName1, Namespace: metav1.NamespaceAll}, bpfProgEth1)
		require.NoError(t, err)

		// prog ID should already have been set
		id1, err := bpfmanagentinternal.GetID(bpfProgEth1)
		require.NoError(t, err)

		// Check the bpfLoadRequest was correctly built
		if !cmp.Equal(expectedLoadReq1, cli.LoadRequests[int(*id1)], protocmp.Transform()) {
			t.Logf("Diff %v", cmp.Diff(expectedLoadReq1, cli.LoadRequests[int(*id1)], protocmp.Transform()))
			t.Fatal("Built bpfman LoadRequest does not match expected")
		}

		// Get program object
		err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName1, Namespace: metav1.NamespaceAll}, bpfProgEth1)
		require.NoError(t, err)

		// Check that the bpfProgram's status was correctly updated
		// Make sure we only have 1 condition now
		require.Equal(t, 1, len(bpfProgEth1.Status.Conditions))
		// Make sure it's the right one.
		require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), bpfProgEth1.Status.Conditions[0].Type)
	}
}

func TestXdpProgramControllerCreate(t *testing.T) {
	xdpProgramControllerCreate(t, false, false)
}

func TestXdpProgramControllerCreateMultiIntf(t *testing.T) {
	xdpProgramControllerCreate(t, true, false)
}

func TestUpdateStatus(t *testing.T) {
	xdpProgramControllerCreate(t, false, true)
}

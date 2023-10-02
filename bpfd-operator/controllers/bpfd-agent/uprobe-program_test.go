/*
Copyright 2023.

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
	internal "github.com/bpfd-dev/bpfd/bpfd-operator/internal"
	testutils "github.com/bpfd-dev/bpfd/bpfd-operator/internal/test-utils"

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

func TestUprobeProgramControllerCreate(t *testing.T) {
	var (
		name            = "fakeUprobeProgram"
		namespace       = "bpfd"
		bytecodePath    = "/tmp/hello.o"
		bpfFunctionName = "test"
		functionName    = "malloc"
		target          = "libc"
		offset          = 0
		retprobe        = false
		uprobenamespace = ""
		fakeNode        = testutils.NewNode("fake-control-plane")
		ctx             = context.TODO()
		bpfProgName     = fmt.Sprintf("%s-%s-%s", name, fakeNode.Name, "libc")
		bpfProg         = &bpfdiov1alpha1.BpfProgram{}
		fakeUID         = "ef71d42c-aa21-48e8-a697-82391d801a81"
	)
	// A UprobeProgram object with metadata and spec.
	Uprobe := &bpfdiov1alpha1.UprobeProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: bpfdiov1alpha1.UprobeProgramSpec{
			BpfProgramCommon: bpfdiov1alpha1.BpfProgramCommon{
				BpfFunctionName: bpfFunctionName,
				NodeSelector:    metav1.LabelSelector{},
				ByteCode: bpfdiov1alpha1.BytecodeSelector{
					Path: &bytecodePath,
				},
			},
			FunctionName: functionName,
			Targets:      []string{target},
			Offset:       uint64(offset),
			RetProbe:     retprobe,
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, Uprobe}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfdiov1alpha1.SchemeGroupVersion, Uprobe)
	s.AddKnownTypes(bpfdiov1alpha1.SchemeGroupVersion, &bpfdiov1alpha1.UprobeProgramList{})
	s.AddKnownTypes(bpfdiov1alpha1.SchemeGroupVersion, &bpfdiov1alpha1.BpfProgram{})
	s.AddKnownTypes(bpfdiov1alpha1.SchemeGroupVersion, &bpfdiov1alpha1.BpfProgramList{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	cli := agenttestutils.NewBpfdClientFake()

	rc := ReconcilerCommon{
		Client:     cl,
		Scheme:     s,
		BpfdClient: cli,
		NodeName:   fakeNode.Name,
	}

	// Set development Logger so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	// Create a ReconcileMemcached object with the scheme and fake client.
	r := &UprobeProgramReconciler{ReconcilerCommon: rc, ourNode: fakeNode}

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
	// owningConfig Label was correctly set
	require.Equal(t, bpfProg.Labels[internal.BpfProgramOwnerLabel], name)
	// node Label was correctly set
	require.Equal(t, bpfProg.Labels[internal.K8sHostLabel], fakeNode.Name)
	// uprobe function Annotation was correctly set
	require.Equal(t, bpfProg.Annotations[internal.UprobeProgramTarget], target)
	// Type is set
	require.Equal(t, r.getRecType(), bpfProg.Spec.Type)
	// Require no requeue
	require.False(t, res.Requeue)

	// Update UID of bpfProgram with Fake UID since the fake API server won't
	bpfProg.UID = types.UID(fakeUID)
	err = cl.Update(ctx, bpfProg)
	require.NoError(t, err)

	// Second reconcile should create the bpfd Load Request and update the
	// BpfProgram object's maps field and id annotation.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)
	expectedLoadReq := &gobpfd.LoadRequest{
		Bytecode: &gobpfd.BytecodeLocation{
			Location: &gobpfd.BytecodeLocation_File{File: bytecodePath},
		},
		Name:        bpfFunctionName,
		ProgramType: *internal.Kprobe.Uint32(),
		Metadata:    map[string]string{internal.UuidMetadataKey: string(bpfProg.UID), internal.ProgramNameKey: name},
		MapOwnerId:  nil,
		Attach: &gobpfd.AttachInfo{
			Info: &gobpfd.AttachInfo_UprobeAttachInfo{
				UprobeAttachInfo: &gobpfd.UprobeAttachInfo{
					FnName:    &functionName,
					Target:    target,
					Offset:    uint64(offset),
					Retprobe:  retprobe,
					Namespace: &uprobenamespace,
				},
			},
		},
	}

	// Check that the bpfProgram's programs was correctly updated
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName, Namespace: metav1.NamespaceAll}, bpfProg)
	require.NoError(t, err)

	// prog ID should already have been set
	id, err := bpfdagentinternal.GetID(bpfProg)
	require.NoError(t, err)

	// Check the bpfLoadRequest was correctly Built
	if !cmp.Equal(expectedLoadReq, cli.LoadRequests[int(*id)], protocmp.Transform()) {
		cmp.Diff(expectedLoadReq, cli.LoadRequests[int(*id)], protocmp.Transform())
		t.Logf("Diff %v", cmp.Diff(expectedLoadReq, cli.LoadRequests[int(*id)], protocmp.Transform()))
		t.Fatal("Built bpfd LoadRequest does not match expected")
	}

	require.Nil(t, bpfProg.Spec.Maps)

	// Third reconcile should set the status to loaded
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

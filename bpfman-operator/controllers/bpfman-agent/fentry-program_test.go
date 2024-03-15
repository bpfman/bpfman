/*
Copyright 2024.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestFentryProgramControllerCreate(t *testing.T) {
	var (
		name            = "fakeFentryProgram"
		namespace       = "bpfman"
		bytecodePath    = "/tmp/hello.o"
		bpfFunctionName = "test_fentry"
		functionName    = "do_unlinkat"
		fakeNode        = testutils.NewNode("fake-control-plane")
		ctx             = context.TODO()
		bpfProgName     = fmt.Sprintf("%s-%s-%s", name, fakeNode.Name, "do-unlinkat")
		bpfProg         = &bpfmaniov1alpha1.BpfProgram{}
		fakeUID         = "ef71d42c-aa21-48e8-a697-82391d801a81"
	)
	// A FentryProgram object with metadata and spec.
	Fentry := &bpfmaniov1alpha1.FentryProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: bpfmaniov1alpha1.FentryProgramSpec{
			BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
				BpfFunctionName: bpfFunctionName,
				NodeSelector:    metav1.LabelSelector{},
				ByteCode: bpfmaniov1alpha1.BytecodeSelector{
					Path: &bytecodePath,
				},
			},
			FunctionName: functionName,
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, Fentry}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, Fentry)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.FentryProgramList{})
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
	r := &FentryProgramReconciler{ReconcilerCommon: rc, ourNode: fakeNode}

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
	// fentry function Annotation was correctly set
	require.Equal(t, bpfProg.Annotations[internal.FentryProgramFunction], functionName)
	// Type is set
	require.Equal(t, r.getRecType(), bpfProg.Spec.Type)
	// Require no requeue
	require.False(t, res.Requeue)

	// Update UID of bpfProgram with Fake UID since the fake API server won't
	bpfProg.UID = types.UID(fakeUID)
	err = cl.Update(ctx, bpfProg)
	require.NoError(t, err)

	// Second reconcile should create the bpfman Load Request and update the
	// BpfProgram object's maps field and id annotation.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)
	expectedLoadReq := &gobpfman.LoadRequest{
		Bytecode: &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
		},
		Name:        bpfFunctionName,
		ProgramType: *internal.Tracing.Uint32(),
		Metadata:    map[string]string{internal.UuidMetadataKey: string(bpfProg.UID), internal.ProgramNameKey: name},
		MapOwnerId:  nil,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_FentryAttachInfo{
				FentryAttachInfo: &gobpfman.FentryAttachInfo{
					FnName: functionName,
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

	// Check the bpfLoadRequest was correctly Built
	if !cmp.Equal(expectedLoadReq, cli.LoadRequests[int(*id)], protocmp.Transform()) {
		cmp.Diff(expectedLoadReq, cli.LoadRequests[int(*id)], protocmp.Transform())
		t.Logf("Diff %v", cmp.Diff(expectedLoadReq, cli.LoadRequests[int(*id)], protocmp.Transform()))
		t.Fatal("Built bpfman LoadRequest does not match expected")
	}

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

	require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), bpfProg.Status.Conditions[0].Type)
}

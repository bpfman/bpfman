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

func TestDiscoveredProgramControllerCreate(t *testing.T) {
	var (
		namespace    = "bpfd"
		fakeNode     = testutils.NewNode("fake-control-plane")
		ctx          = context.TODO()
		bpfProgName0 = fmt.Sprintf("%s-%d-%s", "dump-bpf-map", 693, fakeNode.Name)
		bpfProgName1 = fmt.Sprintf("%s-%d-%s", "dump-bpf-prog", 694, fakeNode.Name)
		bpfProgName2 = fmt.Sprintf("%d-%s", 93149, fakeNode.Name)
		bpfProg      = &bpfdiov1alpha1.BpfProgram{}
		fakeUID      = "ef71d42c-aa21-48e8-a697-82391d801a81"
		programs     = map[string]*gobpfd.ListResponse_ListResult{
			"dump_bpf_map": {
				Name:          "dump_bpf_map",
				Location:      &gobpfd.ListResponse_ListResult_NoLocation{},
				ProgramType:   26,
				AttachInfo:    &gobpfd.ListResponse_ListResult_None{},
				BpfId:         693,
				LoadedAt:      "2023-03-02T18:15:06+0000",
				Tag:           "749172daffada61f",
				GplCompatible: true,
				MapIds:        []uint32{45},
				BtfId:         154,
				BytesXlated:   264,
				Jited:         true,
				BytesJited:    287,
				BytesMemlock:  4096,
				VerifiedInsns: 34,
			},
			"dump_bpf_prog": {
				Name:          "dump_bpf_prog",
				Location:      &gobpfd.ListResponse_ListResult_NoLocation{},
				ProgramType:   26,
				AttachInfo:    &gobpfd.ListResponse_ListResult_None{},
				BpfId:         694,
				LoadedAt:      "2023-03-02T18:15:06+0000",
				Tag:           "bc36dd738319ea32",
				GplCompatible: true,
				MapIds:        []uint32{45},
				BtfId:         154,
				BytesXlated:   528,
				Jited:         true,
				BytesJited:    715,
				BytesMemlock:  4096,
				VerifiedInsns: 84,
			},
			// test program with no name
			"": {
				Location:      &gobpfd.ListResponse_ListResult_NoLocation{},
				ProgramType:   8,
				AttachInfo:    &gobpfd.ListResponse_ListResult_None{},
				BpfId:         93149,
				LoadedAt:      "2023-07-20T19:11:11+0000",
				Tag:           "6deef7357e7b4530",
				GplCompatible: true,
				BytesXlated:   64,
				Jited:         true,
				BytesJited:    55,
				BytesMemlock:  4096,
				VerifiedInsns: 8,
			},
			// skip program loaded by bpfd
			"bpfd-prog": {
				Id:            &fakeUID,
				Location:      &gobpfd.ListResponse_ListResult_NoLocation{},
				ProgramType:   8,
				AttachInfo:    &gobpfd.ListResponse_ListResult_None{},
				BpfId:         93149,
				LoadedAt:      "2023-07-20T19:11:11+0000",
				Tag:           "6deef7357e7b4530",
				GplCompatible: true,
				BytesXlated:   64,
				Jited:         true,
				BytesJited:    55,
				BytesMemlock:  4096,
				VerifiedInsns: 8,
			},
		}
	)

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfdiov1alpha1.SchemeGroupVersion, &bpfdiov1alpha1.BpfProgram{})
	s.AddKnownTypes(bpfdiov1alpha1.SchemeGroupVersion, &bpfdiov1alpha1.BpfProgramList{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	cli := agenttestutils.NewBpfdClientFakeWithPrograms(programs)

	rc := ReconcilerCommon{
		Client:       cl,
		Scheme:       s,
		BpfdClient:   cli,
		NodeName:     fakeNode.Name,
		bpfPrograms:  map[string]bpfdiov1alpha1.BpfProgram{},
		expectedMaps: map[string]string{},
	}

	// Set development Logger so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	// Create a ReconcileMemcached object with the scheme and fake client.
	r := &DiscoveredProgramReconciler{ReconcilerCommon: rc}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "fake-control-plane",
			Namespace: namespace,
		},
	}

	// Three reconciles should create three bpf program objects
	res, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	require.False(t, res.Requeue)

	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	require.False(t, res.Requeue)

	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	require.False(t, res.Requeue)

	// Check the first discovered BpfProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName0, Namespace: metav1.NamespaceAll}, bpfProg)
	require.NoError(t, err)

	require.NotEmpty(t, bpfProg)
	// discovered Label is written
	require.Contains(t, bpfProg.Labels, internal.DiscoveredLabel)
	// node Label was correctly set
	require.Equal(t, bpfProg.Labels[internal.K8sHostLabel], fakeNode.Name)
	// ensure annotations were correct
	require.Equal(t, bpfProg.Annotations, bpfdagentinternal.Build_kernel_info_annotations(programs["dump_bpf_map"]))

	// Check the second discovered BpfProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName1, Namespace: metav1.NamespaceAll}, bpfProg)
	require.NoError(t, err)

	require.NotEmpty(t, bpfProg)
	// discovered Label is written
	require.Contains(t, bpfProg.Labels, internal.DiscoveredLabel)
	// node Label was correctly set
	require.Equal(t, bpfProg.Labels[internal.K8sHostLabel], fakeNode.Name)
	// ensure annotations were correct
	require.Equal(t, bpfProg.Annotations, bpfdagentinternal.Build_kernel_info_annotations(programs["dump_bpf_prog"]))

	// Check the third discovered BpfProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName2, Namespace: metav1.NamespaceAll}, bpfProg)
	require.NoError(t, err)

	require.NotEmpty(t, bpfProg)
	// discovered Label is written
	require.Contains(t, bpfProg.Labels, internal.DiscoveredLabel)
	// node Label was correctly set
	require.Equal(t, bpfProg.Labels[internal.K8sHostLabel], fakeNode.Name)
	// ensure annotations were correct
	require.Equal(t, bpfProg.Annotations, bpfdagentinternal.Build_kernel_info_annotations(programs[""]))

	// The fourth reconcile will end up exiting with a 30 second requeue
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require requeue
	require.True(t, res.Requeue)
}

func TestDiscoveredProgramControllerCreateAndDeleteStale(t *testing.T) {
	var (
		namespace    = "bpfd"
		fakeNode     = testutils.NewNode("fake-control-plane")
		ctx          = context.TODO()
		bpfProgName0 = fmt.Sprintf("%s-%d-%s", "dump-bpf-map", 693, fakeNode.Name)
		bpfProgName1 = fmt.Sprintf("%s-%d-%s", "dump-bpf-prog", 694, fakeNode.Name)
		bpfProgName2 = fmt.Sprintf("%d-%s", 93149, fakeNode.Name)
		bpfProg      = &bpfdiov1alpha1.BpfProgram{}
		fakeUID      = "ef71d42c-aa21-48e8-a697-82391d801a81"
		programs     = map[string]*gobpfd.ListResponse_ListResult{
			"dump_bpf_map": {
				Name:          "dump_bpf_map",
				Location:      &gobpfd.ListResponse_ListResult_NoLocation{},
				ProgramType:   26,
				AttachInfo:    &gobpfd.ListResponse_ListResult_None{},
				BpfId:         693,
				LoadedAt:      "2023-03-02T18:15:06+0000",
				Tag:           "749172daffada61f",
				GplCompatible: true,
				MapIds:        []uint32{45},
				BtfId:         154,
				BytesXlated:   264,
				Jited:         true,
				BytesJited:    287,
				BytesMemlock:  4096,
				VerifiedInsns: 34,
			},
			"dump_bpf_prog": {
				Name:          "dump_bpf_prog",
				Location:      &gobpfd.ListResponse_ListResult_NoLocation{},
				ProgramType:   26,
				AttachInfo:    &gobpfd.ListResponse_ListResult_None{},
				BpfId:         694,
				LoadedAt:      "2023-03-02T18:15:06+0000",
				Tag:           "bc36dd738319ea32",
				GplCompatible: true,
				MapIds:        []uint32{45},
				BtfId:         154,
				BytesXlated:   528,
				Jited:         true,
				BytesJited:    715,
				BytesMemlock:  4096,
				VerifiedInsns: 84,
			},
			// test program with no name
			"": {
				Location:      &gobpfd.ListResponse_ListResult_NoLocation{},
				ProgramType:   8,
				AttachInfo:    &gobpfd.ListResponse_ListResult_None{},
				BpfId:         93149,
				LoadedAt:      "2023-07-20T19:11:11+0000",
				Tag:           "6deef7357e7b4530",
				GplCompatible: true,
				BytesXlated:   64,
				Jited:         true,
				BytesJited:    55,
				BytesMemlock:  4096,
				VerifiedInsns: 8,
			},
			// skip program loaded by bpfd
			"bpfd-prog": {
				Id:            &fakeUID,
				Location:      &gobpfd.ListResponse_ListResult_NoLocation{},
				ProgramType:   8,
				AttachInfo:    &gobpfd.ListResponse_ListResult_None{},
				BpfId:         93149,
				LoadedAt:      "2023-07-20T19:11:11+0000",
				Tag:           "6deef7357e7b4530",
				GplCompatible: true,
				BytesXlated:   64,
				Jited:         true,
				BytesJited:    55,
				BytesMemlock:  4096,
				VerifiedInsns: 8,
			},
		}
	)

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfdiov1alpha1.SchemeGroupVersion, &bpfdiov1alpha1.BpfProgram{})
	s.AddKnownTypes(bpfdiov1alpha1.SchemeGroupVersion, &bpfdiov1alpha1.BpfProgramList{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	cli := agenttestutils.NewBpfdClientFakeWithPrograms(programs)

	rc := ReconcilerCommon{
		Client:       cl,
		Scheme:       s,
		BpfdClient:   cli,
		NodeName:     fakeNode.Name,
		bpfPrograms:  map[string]bpfdiov1alpha1.BpfProgram{},
		expectedMaps: map[string]string{},
	}

	// Set development Logger so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	// Create a ReconcileMemcached object with the scheme and fake client.
	r := &DiscoveredProgramReconciler{ReconcilerCommon: rc}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "fake-control-plane",
			Namespace: namespace,
		},
	}

	// Three reconciles should create three bpf program objects
	res, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	require.False(t, res.Requeue)

	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	require.False(t, res.Requeue)

	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	require.False(t, res.Requeue)

	// Check the first discovered BpfProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName0, Namespace: metav1.NamespaceAll}, bpfProg)
	require.NoError(t, err)

	require.NotEmpty(t, bpfProg)
	// discovered Label is written
	require.Contains(t, bpfProg.Labels, internal.DiscoveredLabel)
	// node Label was correctly set
	require.Equal(t, bpfProg.Labels[internal.K8sHostLabel], fakeNode.Name)
	// ensure annotations were correct
	require.Equal(t, bpfProg.Annotations, bpfdagentinternal.Build_kernel_info_annotations(programs["dump_bpf_map"]))

	// Check the second discovered BpfProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName1, Namespace: metav1.NamespaceAll}, bpfProg)
	require.NoError(t, err)

	require.NotEmpty(t, bpfProg)
	// discovered Label is written
	require.Contains(t, bpfProg.Labels, internal.DiscoveredLabel)
	// node Label was correctly set
	require.Equal(t, bpfProg.Labels[internal.K8sHostLabel], fakeNode.Name)
	// ensure annotations were correct
	require.Equal(t, bpfProg.Annotations, bpfdagentinternal.Build_kernel_info_annotations(programs["dump_bpf_prog"]))

	// Check the third discovered BpfProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName2, Namespace: metav1.NamespaceAll}, bpfProg)
	require.NoError(t, err)

	require.NotEmpty(t, bpfProg)
	// discovered Label is written
	require.Contains(t, bpfProg.Labels, internal.DiscoveredLabel)
	// node Label was correctly set
	require.Equal(t, bpfProg.Labels[internal.K8sHostLabel], fakeNode.Name)
	// ensure annotations were correct
	require.Equal(t, bpfProg.Annotations, bpfdagentinternal.Build_kernel_info_annotations(programs[""]))

	// delete program
	_, err = rc.BpfdClient.Unload(ctx, &gobpfd.UnloadRequest{Id: "dump_bpf_map"})
	require.NoError(t, err)

	// The fourth reconcile will end up deleting the extra bpfProgram
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	require.False(t, res.Requeue)

	// Check the first discovered BpfProgram Object was deleted successfully
	err = cl.Get(ctx, types.NamespacedName{Name: bpfProgName0, Namespace: metav1.NamespaceAll}, bpfProg)
	require.Error(t, err)

	// When all work is done make sure we will reconcile again soon.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	require.True(t, res.Requeue)
}

package sqlite_test

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/platform"
	"github.com/bpfman/bpfman/platform/store/sqlite"
)

// These tests cover the two-phase link write: CreatePendingLink
// inserts the row before the kernel attach, recording pin_path =
// {linksDir}/{link_id} in the same transaction so the id exists to
// name the pin, and FinaliseLink records the captured kernel link id
// afterwards.

const testLinksDir = "/run/bpfman/fs/links"

func TestLinkLifecycle_CreatePendingThenFinalise(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(42), testProgram()), "Save failed")

	details := bpfman.UprobeDetails{
		Target: "/usr/lib/libc.so.6",
		FnName: "main.getCount",
	}
	pending, err := store.CreatePendingLink(ctx, bpfman.LinkSpec{
		ProgramID: kernel.ProgramID(42),
		Kind:      details.Kind(),
		Details:   details,
	}, testLinksDir)
	require.NoError(t, err, "CreatePendingLink failed")
	require.NotZero(t, pending.ID, "pending link must carry an allocated id")
	assert.Nil(t, pending.KernelLinkID, "pending link has no kernel link id yet")
	require.NotNil(t, pending.PinPath, "pending link must record its pin path at creation")
	wantPin := filepath.Join(testLinksDir, strconv.FormatUint(uint64(pending.ID), 10))
	assert.Equal(t, wantPin, pending.PinPath.String(), "pin path must be the numeric link id under the links dir")

	// The pending row is durable and visible to readers between the
	// two phases: GetLink returns it with the pin path recorded and
	// a nil kernel link id. This documents the window a concurrent
	// `bpfman link get` (or a crash before FinaliseLink) observes --
	// crucially the pin path is already named, so cleanup can always
	// detach whatever the kernel attach may have pinned.
	got, err := store.GetLink(ctx, pending.ID)
	require.NoError(t, err, "GetLink on a pending link should succeed")
	assert.Nil(t, got.KernelLinkID)
	require.NotNil(t, got.PinPath)
	assert.Equal(t, wantPin, got.PinPath.String())

	kernelLinkID := kernel.LinkID(7777)
	finalised, err := store.FinaliseLink(ctx, pending.ID, &kernelLinkID)
	require.NoError(t, err, "FinaliseLink failed")
	assert.Equal(t, pending.ID, finalised.ID, "finalise must not change the id")
	require.NotNil(t, finalised.KernelLinkID)
	assert.Equal(t, kernelLinkID, *finalised.KernelLinkID)
	require.NotNil(t, finalised.PinPath)
	assert.Equal(t, wantPin, finalised.PinPath.String(), "finalise must not change the pin path")

	got, err = store.GetLink(ctx, pending.ID)
	require.NoError(t, err, "GetLink after finalise failed")
	require.NotNil(t, got.KernelLinkID)
	assert.Equal(t, kernelLinkID, *got.KernelLinkID)
	require.NotNil(t, got.PinPath)
	assert.Equal(t, wantPin, got.PinPath.String())
	uprobe, ok := got.Details.(bpfman.UprobeDetails)
	require.True(t, ok, "details must survive the two-phase write")
	assert.Equal(t, "main.getCount", uprobe.FnName)
}

func TestFinaliseLink_NotFound(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	kernelLinkID := kernel.LinkID(1)
	_, err = store.FinaliseLink(context.Background(), bpfman.LinkID(999), &kernelLinkID)
	require.Error(t, err)
	assert.ErrorIs(t, err, platform.ErrRecordNotFound)
}

func TestLinkLifecycle_DeletePendingLink(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err, "failed to create store")
	defer store.Close()

	ctx := context.Background()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(42), testProgram()), "Save failed")

	details := bpfman.KprobeDetails{FnName: "do_sys_open"}
	pending, err := store.CreatePendingLink(ctx, bpfman.LinkSpec{
		ProgramID: kernel.ProgramID(42),
		Kind:      details.Kind(),
		Details:   details,
	}, testLinksDir)
	require.NoError(t, err, "CreatePendingLink failed")

	// The attach-failure undo path: the pending row deletes cleanly.
	require.NoError(t, store.DeleteLink(ctx, pending.ID), "DeleteLink on pending link failed")
	_, err = store.GetLink(ctx, pending.ID)
	assert.ErrorIs(t, err, platform.ErrRecordNotFound)
}

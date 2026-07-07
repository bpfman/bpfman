package manager_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/platform"
)

type failProgramDeleteOnceStore struct {
	platform.Store
	err   error
	fired bool
}

func (s *failProgramDeleteOnceStore) Delete(ctx context.Context, programID kernel.ProgramID) error {
	if !s.fired {
		s.fired = true
		return s.err
	}
	return s.Store.Delete(ctx, programID)
}

// TestUnload_MapsPinsFailure_IsNonFatalAndStillCleansEmptyDispatcher
// pins the post-detach contract: once kernel detach succeeds, a
// transient bpffs failure on the program's map pins is warned and
// discarded, not joined. Surfacing it would put callers in a
// false-negative retry loop (the program is gone; a retry would see
// ErrProgramNotFound). Dispatcher removal has already happened during
// the dispatcher-aware link detach, so a hygiene warning cannot
// strand a dispatcher on the netdev.
func TestUnload_MapsPinsFailure_IsNonFatalAndStillCleansEmptyDispatcher(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("xdp.o"), "xdp_pass", bpfman.ProgramTypeXDP)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "Load should succeed")

	attachSpec, err := bpfman.NewXDPAttachSpec(prog.Record.ProgramID, "lo", 0)
	require.NoError(t, err)
	_, err = fix.Attach(ctx, attachSpec)
	require.NoError(t, err, "Attach should succeed")

	summaries, err := fix.Store.ListDispatcherSummaries(ctx)
	require.NoError(t, err)
	require.Len(t, summaries, 1, "expected exactly one dispatcher")

	// Inject a failure on the program's maps directory cleanup.
	// This is past the point of no return in the unload flow:
	// detachAllLinks and unloadKernelProgram have already run.
	mapsDir := fix.Layout.BPFFS().MapPinDir(prog.Record.ProgramID).String()
	hygieneErr := errors.New("simulated bpffs failure on maps cleanup")
	fix.Kernel.FailOnUnload(mapsDir, hygieneErr)

	err = fix.Unload(ctx, prog.Record.ProgramID)
	require.NoError(t, err, "maps-pin cleanup is orphan hygiene only")

	// Guard against the failure mode where the test's computed
	// mapsDir drifts from what production passes to kernel.Unload:
	// without this check, a path mismatch would silently no-op and
	// leave the test passing trivially.
	require.Equal(t, 1, fix.Kernel.UnloadFailureCount(mapsDir), "fault injection must have fired exactly once on the maps directory")

	// The contract: dispatcher removal runs before hygiene can fail.
	summaries, err = fix.Store.ListDispatcherSummaries(ctx)
	require.NoError(t, err)
	assert.Empty(t, summaries, "dispatcher must be cleaned up after the program's last link is detached, "+"even when post-detach hygiene fails")

	programs, err := fix.Store.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, programs, "program row should still be deleted")
}

func TestUnload_DeleteProgramFailure_RetryToleratesRemovedDispatcher(t *testing.T) {
	t.Parallel()

	deleteErr := errors.New("simulated program delete failure")
	fix := newTestFixtureWithStore(t, func(store platform.Store) platform.Store {
		return &failProgramDeleteOnceStore{Store: store, err: deleteErr}
	})
	ctx := context.Background()

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("xdp.o"), "xdp_pass", bpfman.ProgramTypeXDP)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err)

	attachSpec, err := bpfman.NewXDPAttachSpec(prog.Record.ProgramID, "lo", 0)
	require.NoError(t, err)
	_, err = fix.Attach(ctx, attachSpec)
	require.NoError(t, err)

	err = fix.Unload(ctx, prog.Record.ProgramID)
	require.ErrorIs(t, err, deleteErr)

	summaries, err := fix.Store.ListDispatcherSummaries(ctx)
	require.NoError(t, err)
	require.Empty(t, summaries, "first attempt should have removed the empty dispatcher")

	err = fix.Unload(ctx, prog.Record.ProgramID)
	require.NoError(t, err, "retry should tolerate the already-removed dispatcher")

	programs, err := fix.Store.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, programs, "retry should delete the remaining program row")
}

// TestUnload_BytecodeDirFailure_IsNonFatalAndStillCleansEmptyDispatcher
// pins the differentiated post-detach contract: once kernel state and
// store state are gone, a bytecode-directory removal failure leaves
// only a userland orphan and must not make Unload report failure.
//
// Fault injection here is via os.Chmod 0555 on the parent directory,
// which only fails the inner os.RemoveAll for non-root users; root
// bypasses DAC so the chmod is a no-op there. CI runs as root in the
// container, so the test is skipped there. Local developers exercise
// the path normally. The broader contract (Unload returns nil after
// post-detach hygiene failure, dispatcher cleanup still runs) is
// gated in CI by TestUnload_MapsPinsFailure_IsNonFatalAndStillCleansEmptyDispatcher,
// which uses fakeKernel-level fault injection that works under root.
func TestUnload_BytecodeDirFailure_IsNonFatalAndStillCleansEmptyDispatcher(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod-based fault injection requires non-root user; CI gating is via TestUnload_MapsPinsFailure")
	}
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("xdp.o"), "xdp_pass", bpfman.ProgramTypeXDP)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err)

	attachSpec, err := bpfman.NewXDPAttachSpec(prog.Record.ProgramID, "lo", 0)
	require.NoError(t, err)
	_, err = fix.Attach(ctx, attachSpec)
	require.NoError(t, err)

	summaries, err := fix.Store.ListDispatcherSummaries(ctx)
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	programDir := fix.Layout.Bytecode().ProgramDir(prog.Record.ProgramID)
	programsDir := filepath.Dir(programDir)

	info, err := os.Stat(programsDir)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(programsDir, 0555))
	t.Cleanup(func() {
		_ = os.Chmod(programsDir, info.Mode().Perm())
	})

	err = fix.Unload(ctx, prog.Record.ProgramID)
	require.NoError(t, err, "bytecode directory cleanup is orphan hygiene only")

	programs, err := fix.Store.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, programs, "program row should still be deleted")

	summaries, err = fix.Store.ListDispatcherSummaries(ctx)
	require.NoError(t, err)
	assert.Empty(t, summaries, "dispatcher cleanup must still run after bytecode dir warning")

	assert.Equal(t, 0, fix.Kernel.ProgramCount(), "kernel state should be fully removed")
	_, err = os.Stat(programDir)
	require.NoError(t, err, "failed bytecode cleanup should leave the orphaned directory behind")
}

// TestUnload_PreDetachFailure_LeavesDispatcherInPlace pins the
// symmetric contract: if the kernel-side detach itself fails, we are
// not past the point of no return and dispatcher cleanup must NOT
// run. Coherency, audit, and GC are responsible for repair when the
// destructive teardown is interrupted.
func TestUnload_PreDetachFailure_LeavesDispatcherInPlace(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("xdp.o"), "xdp_pass", bpfman.ProgramTypeXDP)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "Load should succeed")

	attachSpec, err := bpfman.NewXDPAttachSpec(prog.Record.ProgramID, "lo", 0)
	require.NoError(t, err)
	link, err := fix.Attach(ctx, attachSpec)
	require.NoError(t, err)

	summaries, err := fix.Store.ListDispatcherSummaries(ctx)
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	detachErr := errors.New("simulated detach failure")
	require.NotNil(t, link.Record.KernelLinkID)
	fix.Kernel.FailOnDetach(*link.Record.KernelLinkID, detachErr)

	err = fix.Unload(ctx, prog.Record.ProgramID)
	require.Error(t, err, "Unload should fail when kernel detach fails")
	assert.ErrorIs(t, err, detachErr)

	// Point of no return not crossed: dispatcher row stays.
	summaries, err = fix.Store.ListDispatcherSummaries(ctx)
	require.NoError(t, err)
	assert.Len(t, summaries, 1, "dispatcher must remain when kernel detach failed; coherency repairs")
}

// TestDetach_DispatcherRevisionDirFailure_IsNonFatal pins the same
// distinction on dispatcher teardown: once the dispatcher link and
// program pin are gone, failing to rmdir the now-empty revision
// directory leaves only a filesystem orphan and must not fail Detach.
//
// Fault injection here is via os.Chmod 0555 on the parent directory,
// which only fails the inner os.RemoveAll for non-root users; root
// bypasses DAC. CI runs as root and skips this test. The broader
// post-detach log-only contract is gated in CI by
// TestUnload_MapsPinsFailure_IsNonFatalAndStillCleansEmptyDispatcher.
func TestDetach_DispatcherRevisionDirFailure_IsNonFatal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod-based fault injection requires non-root user; CI gating is via TestUnload_MapsPinsFailure")
	}
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("xdp.o"), "xdp_pass", bpfman.ProgramTypeXDP)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err)

	attachSpec, err := bpfman.NewXDPAttachSpec(prog.Record.ProgramID, "lo", 0)
	require.NoError(t, err)
	link, err := fix.Attach(ctx, attachSpec)
	require.NoError(t, err)

	summaries, err := fix.Store.ListDispatcherSummaries(ctx)
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	snap, err := fix.Store.GetDispatcherSnapshot(ctx, summaries[0].Key)
	require.NoError(t, err)
	revDir := fix.Layout.BPFFS().DispatcherRevisionDir(snap.Key.Type, snap.Key.Nsid, snap.Key.Ifindex, snap.Revision).String()
	typeDir := filepath.Dir(revDir)

	info, err := os.Stat(typeDir)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(typeDir, 0555))
	t.Cleanup(func() {
		_ = os.Chmod(typeDir, info.Mode().Perm())
	})

	err = fix.Detach(ctx, link.Record.ID)
	require.NoError(t, err, "dispatcher revision directory cleanup is orphan hygiene only")

	summaries, err = fix.Store.ListDispatcherSummaries(ctx)
	require.NoError(t, err)
	assert.Empty(t, summaries, "dispatcher snapshot should still be deleted")

	_, err = os.Stat(revDir)
	require.NoError(t, err, "failed revision-dir cleanup should leave the orphaned directory behind")
}

package manager_test

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/platform"
)

// These tests pin down the link pin-naming contract: link pins use
// the numeric bpfman link id (links/{link_id}), not symbol-derived
// names (links/{program_id}/{fn_name}). bpffs rejects path
// components containing dots,
// so any naming scheme that lets a symbol reach the pin path fails
// at attach time for real-world Go targets like main.getCount. The
// shape assertions here are the unit-level guard; the e2e script
// TestUprobe_GoSymbolTraffic.bpfman proves the kernel accepts the
// result on a real bpffs.

// attachUprobe loads a uprobe program and attaches it to the given
// symbol, returning the link.
func attachUprobe(t *testing.T, fix *testFixture, progName, fnName string) bpfman.Link {
	t.Helper()
	ctx := context.Background()

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile(progName+".o"), progName, bpfman.ProgramTypeUprobe)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "Load should succeed")

	attachSpec, err := bpfman.NewUprobeAttachSpec(prog.Record.ProgramID, "/usr/lib/libc.so.6", 0, 0)
	require.NoError(t, err, "failed to create attach spec")
	attachSpec = attachSpec.WithFnName(fnName)

	link, err := fix.Attach(ctx, attachSpec)
	require.NoError(t, err, "Attach should succeed")
	return link
}

// requireNumericLinkPin asserts the link's pin path is
// {bpffs}/links/{link_id}: parent directory is the flat links dir
// and the final component is the decimal bpfman link id, regardless
// of the symbol or program the link was attached to.
func requireNumericLinkPin(t *testing.T, fix *testFixture, link bpfman.Link) {
	t.Helper()
	require.NotNil(t, link.Record.PinPath, "attached link should record a pin path")
	pin := link.Record.PinPath.String()
	assert.Equal(t, fix.Layout.BPFFS().Links(), filepath.Dir(pin), "link pin should live directly in the flat links dir")
	assert.Equal(t, strconv.FormatUint(uint64(link.Record.ID), 10), filepath.Base(pin), "link pin name should be the decimal bpfman link id")
}

func TestAttach_LinkPinPathIsNumericLinkID(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	link := attachUprobe(t, fix, "uprobe_prog", "malloc")
	requireNumericLinkPin(t, fix, link)
}

func TestAttach_TracepointLinkPinPathIsNumericLinkID(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("tp.o"), "tp_prog", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "Load should succeed")

	attachSpec, err := bpfman.NewTracepointAttachSpecFromString(prog.Record.ProgramID, "sched/sched_switch")
	require.NoError(t, err, "failed to create attach spec")

	link, err := fix.Attach(ctx, attachSpec)
	require.NoError(t, err, "Attach should succeed")
	requireNumericLinkPin(t, fix, link)
}

// TestAttach_DottedGoSymbolKeepsPinNumeric is the unit-level
// regression test for the bpffs dot rule: a fully qualified Go
// symbol must not leak into the pin path. The fn-name is stored in
// the link details; the pin name stays the numeric link id.
func TestAttach_DottedGoSymbolKeepsPinNumeric(t *testing.T) {
	t.Parallel()

	const goSymbol = "github.com/bpfman/bpfman/cmd/bpfman-shell/fixturemode.GoUprobeTarget"

	fix := newTestFixture(t)
	ctx := context.Background()

	link := attachUprobe(t, fix, "uprobe_prog", goSymbol)
	requireNumericLinkPin(t, fix, link)

	rel, err := filepath.Rel(fix.Layout.BPFFS().Links(), link.Record.PinPath.String())
	require.NoError(t, err)
	assert.NotContains(t, rel, ".", "no pin path component below links/ may contain a dot")
	assert.NotContains(t, rel, "/", "pin must be a direct child of links/, not nested")

	record, err := fix.Store.GetLink(ctx, link.Record.ID)
	require.NoError(t, err, "stored link should round-trip")
	details, ok := record.Details.(bpfman.UprobeDetails)
	require.True(t, ok, "expected UprobeDetails")
	assert.Equal(t, goSymbol, details.FnName, "the symbol lives in details, not in the pin name")
	require.NotNil(t, record.KernelLinkID, "finalised link should carry the kernel link id")
	require.NotNil(t, record.PinPath, "finalised link should carry the pin path")
}

func TestAttach_DistinctLinksGetDistinctNumericPins(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("uprobe_prog.o"), "uprobe_prog", bpfman.ProgramTypeUprobe)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "Load should succeed")

	var links []bpfman.Link
	for _, fn := range []string{"malloc", "free"} {
		attachSpec, err := bpfman.NewUprobeAttachSpec(prog.Record.ProgramID, "/usr/lib/libc.so.6", 0, 0)
		require.NoError(t, err)
		link, err := fix.Attach(ctx, attachSpec.WithFnName(fn))
		require.NoError(t, err, "Attach %s should succeed", fn)
		links = append(links, link)
	}

	assert.Less(t, links[0].Record.ID, links[1].Record.ID, "link ids should be allocated in increasing order")
	assert.NotEqual(t, links[0].Record.PinPath.String(), links[1].Record.PinPath.String(), "each link should get its own pin")
	for _, l := range links {
		requireNumericLinkPin(t, fix, l)
	}
}

// TestAttach_KernelFailureLeavesNoLinkRow verifies the
// create-then-attach ordering rolls the pending link row back when
// the kernel attach fails, mirroring Rust's delete-on-failure: no
// row may survive a failed attach.
func TestAttach_KernelFailureLeavesNoLinkRow(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("tp.o"), "tp_prog", bpfman.ProgramTypeTracepoint)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "Load should succeed")

	injected := errors.New("injected attach failure")
	fix.Kernel.FailOnAttach("tracepoint", injected)

	attachSpec, err := bpfman.NewTracepointAttachSpecFromString(prog.Record.ProgramID, "sched/sched_switch")
	require.NoError(t, err)
	_, err = fix.Attach(ctx, attachSpec)
	require.Error(t, err, "Attach should fail")
	require.ErrorIs(t, err, injected)

	linkRows, err := fix.Store.ListLinks(ctx)
	require.NoError(t, err)
	assert.Empty(t, linkRows, "a failed attach must not leave a link row (pending or otherwise)")
	assert.Equal(t, 0, fix.Kernel.LinkCount(), "no kernel link should exist")
}

// finaliseFailStore makes FinaliseLink fail while every other store
// operation behaves normally.
type finaliseFailStore struct {
	platform.Store
	err error
}

func (s *finaliseFailStore) FinaliseLink(ctx context.Context, linkID bpfman.LinkID, kernelLinkID *kernel.LinkID) (bpfman.LinkRecord, error) {
	return bpfman.LinkRecord{}, s.err
}

// TestAttach_FinaliseFailureRollsBackAttachAndRow verifies the undo
// chain when the final store write fails after a successful kernel
// attach: the kernel link is detached and the pending row deleted.
func TestAttach_FinaliseFailureRollsBackAttachAndRow(t *testing.T) {
	t.Parallel()

	injected := errors.New("injected finalise failure")
	fix := newTestFixtureWithStore(t, func(s platform.Store) platform.Store {
		return &finaliseFailStore{Store: s, err: injected}
	})
	ctx := context.Background()

	spec, err := bpfman.NewLoadSpec(fix.BytecodeFile("uprobe_prog.o"), "uprobe_prog", bpfman.ProgramTypeUprobe)
	require.NoError(t, err)
	prog, err := fix.Load(ctx, spec, manager.LoadOpts{})
	require.NoError(t, err, "Load should succeed")

	attachSpec, err := bpfman.NewUprobeAttachSpec(prog.Record.ProgramID, "/usr/lib/libc.so.6", 0, 0)
	require.NoError(t, err)
	_, err = fix.Attach(ctx, attachSpec.WithFnName("malloc"))
	require.Error(t, err, "Attach should fail when finalise fails")
	require.ErrorIs(t, err, injected)

	linkRows, err := fix.Store.ListLinks(ctx)
	require.NoError(t, err)
	assert.Empty(t, linkRows, "the pending link row must be rolled back")
	assert.Equal(t, 0, fix.Kernel.LinkCount(), "the kernel attach must be undone")
}

// TestAttach_DetachRemovesNumericPinRow covers the read-back half:
// detach resolves the link by its numeric id, removes the row, and
// the id is not reused for the next link (AUTOINCREMENT).
func TestAttach_DetachRemovesNumericPinRow(t *testing.T) {
	t.Parallel()

	fix := newTestFixture(t)
	ctx := context.Background()

	first := attachUprobe(t, fix, "uprobe_prog", "malloc")
	require.NoError(t, fix.Detach(ctx, first.Record.ID), "Detach should succeed")

	_, err := fix.Store.GetLink(ctx, first.Record.ID)
	require.Error(t, err, "detached link should be gone from the store")

	second := attachUprobe(t, fix, "uprobe_prog2", "free")
	assert.Greater(t, second.Record.ID, first.Record.ID, "link ids must not be reused after delete")
	requireNumericLinkPin(t, fix, second)
	if base := filepath.Base(second.Record.PinPath.String()); strings.Contains(base, ".") {
		t.Fatalf("pin name %q contains a dot", base)
	}
}

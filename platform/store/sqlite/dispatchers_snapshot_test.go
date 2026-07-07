package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/platform"
	"github.com/bpfman/bpfman/platform/store/sqlite"
)

// testXDPProgram returns a ProgramRecord for XDP testing.
func testXDPProgram(name string) bpfman.ProgramRecord {
	return bpfman.ProgramRecord{
		Load: bpfman.TestLoadSpecWithPath(bpfman.ProgramTypeXDP, "/test/path/"+name+".o"),
		Handles: bpfman.ProgramHandles{
			PinPath: bpfman.ProgPinPath("/sys/fs/bpf/" + name),
		},
		Meta: bpfman.ProgramMeta{
			Name: name,
		},
		CreatedAt: time.Now(),
	}
}

// testTCProgram returns a ProgramRecord for TC testing.
func testTCProgram(name string) bpfman.ProgramRecord {
	return bpfman.ProgramRecord{
		Load: bpfman.TestLoadSpecWithPath(bpfman.ProgramTypeTC, "/test/path/"+name+".o"),
		Handles: bpfman.ProgramHandles{
			PinPath: bpfman.ProgPinPath("/sys/fs/bpf/" + name),
		},
		Meta: bpfman.ProgramMeta{
			Name: name,
		},
		CreatedAt: time.Now(),
	}
}

const (
	testNsid    = uint64(4026531840)
	testIfindex = uint32(2)
)

func xdpKey() dispatcher.Key {
	return dispatcher.Key{
		Type:    dispatcher.DispatcherTypeXDP,
		Nsid:    testNsid,
		Ifindex: testIfindex,
	}
}

func tcIngressKey() dispatcher.Key {
	return dispatcher.Key{
		Type:    dispatcher.DispatcherTypeTCIngress,
		Nsid:    testNsid,
		Ifindex: testIfindex,
	}
}

// setupXDPSnapshot creates managed programs and returns a snapshot
// ready for ReplaceDispatcherSnapshot.
func setupXDPSnapshot(t *testing.T, ctx context.Context, store platform.Store) platform.DispatcherSnapshotSpec {
	t.Helper()

	// Create two managed programs that will be extensions.
	prog1ID := kernel.ProgramID(1001)
	prog2ID := kernel.ProgramID(1002)
	require.NoError(t, store.Save(ctx, prog1ID, testXDPProgram("xdp_prog1")))
	require.NoError(t, store.Save(ctx, prog2ID, testXDPProgram("xdp_prog2")))

	dispProgramID := kernel.ProgramID(500)
	linkID := kernel.LinkID(501)

	member1KernelLinkID := kernel.LinkID(701)
	member2KernelLinkID := kernel.LinkID(702)
	return platform.DispatcherSnapshotSpec{
		Key:      xdpKey(),
		Revision: 1,
		Runtime: platform.DispatcherRuntime{
			ProgramID:    dispProgramID,
			KernelLinkID: &linkID,
		},
		Members: []platform.DispatcherMemberSpec{
			{
				ProgramID:    prog1ID,
				ProgramName:  "xdp_prog1",
				ProgPinPath:  "/sys/fs/bpf/xdp_prog1",
				KernelLinkID: &member1KernelLinkID,
				LinkPinPath:  "/sys/fs/bpf/dispatch/r1/link0",
				Position:     0,
				Priority:     50,
				ProceedOn:    0x04, // XDP_PASS
				Ifname:       "eth0",
			},
			{
				ProgramID:    prog2ID,
				ProgramName:  "xdp_prog2",
				ProgPinPath:  "/sys/fs/bpf/xdp_prog2",
				KernelLinkID: &member2KernelLinkID,
				LinkPinPath:  "/sys/fs/bpf/dispatch/r1/link1",
				Position:     1,
				Priority:     100,
				ProceedOn:    0x06, // XDP_PASS | XDP_DROP
				Ifname:       "eth0",
			},
		},
	}
}

func TestSnapshotStore_ReplaceAndGet_XDP(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	snap := setupXDPSnapshot(t, ctx, store)

	// Replace (first time = create).
	completed, err := store.ReplaceDispatcherSnapshot(ctx, snap)
	require.NoError(t, err)

	// Get the snapshot back.
	got, err := store.GetDispatcherSnapshot(ctx, xdpKey())
	require.NoError(t, err)

	assert.Equal(t, snap.Key, got.Key)
	assert.Equal(t, snap.Revision, got.Revision)
	assert.Equal(t, snap.Runtime.ProgramID, got.Runtime.ProgramID)
	require.NotNil(t, got.Runtime.KernelLinkID)
	assert.Equal(t, *snap.Runtime.KernelLinkID, *got.Runtime.KernelLinkID)
	require.Len(t, got.Members, 2)
	require.Len(t, completed.Members, 2)
	assert.NotZero(t, completed.Members[0].LinkID)
	assert.NotZero(t, completed.Members[1].LinkID)

	// Members should be ordered by priority.
	assert.Equal(t, "xdp_prog1", got.Members[0].ProgramName)
	assert.Equal(t, 0, got.Members[0].Position)
	assert.Equal(t, 50, got.Members[0].Priority)
	assert.Equal(t, uint32(0x04), got.Members[0].ProceedOn)
	assert.Equal(t, "eth0", got.Members[0].Ifname)
	require.NotNil(t, got.Members[0].KernelLinkID)
	assert.Equal(t, kernel.LinkID(701), *got.Members[0].KernelLinkID)

	assert.Equal(t, "xdp_prog2", got.Members[1].ProgramName)
	assert.Equal(t, 1, got.Members[1].Position)
	assert.Equal(t, 100, got.Members[1].Priority)
}

func TestSnapshotStore_ReplaceRemovesOldMembership(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	snap := setupXDPSnapshot(t, ctx, store)

	// Initial replace with two members.
	completed, err := store.ReplaceDispatcherSnapshot(ctx, snap)
	require.NoError(t, err)

	got, err := store.GetDispatcherSnapshot(ctx, xdpKey())
	require.NoError(t, err)
	require.Len(t, got.Members, 2)

	// Replace again with only one member (simulating detach of prog2).
	// New dispatcher program ID from rebuild.
	newDispProgramID := kernel.ProgramID(600)
	newLinkID := kernel.LinkID(601)
	memberKernelLinkID := kernel.LinkID(703)
	snap2 := platform.DispatcherSnapshotSpec{
		Key:      xdpKey(),
		Revision: 2,
		Runtime: platform.DispatcherRuntime{
			ProgramID:    newDispProgramID,
			KernelLinkID: &newLinkID,
		},
		Members: []platform.DispatcherMemberSpec{
			{
				ExistingLinkID: &completed.Members[0].LinkID,
				ProgramID:      kernel.ProgramID(1001),
				ProgramName:    "xdp_prog1",
				ProgPinPath:    "/sys/fs/bpf/xdp_prog1",
				KernelLinkID:   &memberKernelLinkID,
				LinkPinPath:    "/sys/fs/bpf/dispatch/r2/link0",
				Position:       0,
				Priority:       50,
				ProceedOn:      0x04,
				Ifname:         "eth0",
			},
		},
	}

	completed2, err := store.ReplaceDispatcherSnapshot(ctx, snap2)
	require.NoError(t, err)

	got2, err := store.GetDispatcherSnapshot(ctx, xdpKey())
	require.NoError(t, err)
	assert.Equal(t, uint32(2), got2.Revision)
	assert.Equal(t, newDispProgramID, got2.Runtime.ProgramID)
	require.Len(t, got2.Members, 1)
	assert.Equal(t, "xdp_prog1", got2.Members[0].ProgramName)
	assert.Equal(t, completed.Members[0].LinkID, got2.Members[0].LinkID)
	assert.Equal(t, completed.Members[0].LinkID, completed2.Members[0].LinkID)

	// Old link records should be gone (the links table cascade
	// removed old detail rows).
	_, err = store.GetLink(ctx, completed.Members[1].LinkID)
	require.Error(t, err, "old link should be deleted")
}

func TestSnapshotStore_DeleteSnapshot(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	snap := setupXDPSnapshot(t, ctx, store)

	completed, err := store.ReplaceDispatcherSnapshot(ctx, snap)
	require.NoError(t, err)

	// Delete the snapshot.
	require.NoError(t, store.DeleteDispatcherSnapshot(ctx, xdpKey()))

	// Dispatcher should be gone.
	_, err = store.GetDispatcherSnapshot(ctx, xdpKey())
	require.Error(t, err)
	assert.True(t, errors.Is(err, platform.ErrRecordNotFound))

	// Extension links should be gone.
	_, err = store.GetLink(ctx, completed.Members[0].LinkID)
	require.Error(t, err)
	_, err = store.GetLink(ctx, completed.Members[1].LinkID)
	require.Error(t, err)
}

func TestSnapshotStore_DeleteNonExistent(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	err = store.DeleteDispatcherSnapshot(ctx, xdpKey())
	require.Error(t, err)
	assert.True(t, errors.Is(err, platform.ErrRecordNotFound))
}

func TestSnapshotStore_GetNonExistent(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	_, err = store.GetDispatcherSnapshot(ctx, xdpKey())
	require.Error(t, err)
	assert.True(t, errors.Is(err, platform.ErrRecordNotFound))
}

func TestSnapshotStore_TransactionRollback(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	snap := setupXDPSnapshot(t, ctx, store)

	// First, create the snapshot so we have known state.
	_, err = store.ReplaceDispatcherSnapshot(ctx, snap)
	require.NoError(t, err)

	// Attempt a replacement inside a transaction that rolls back.
	deliberateErr := errors.New("deliberate rollback")
	err = store.RunInTransaction(ctx, "test", func(txStore platform.Store) error {
		snap2 := snap
		snap2.Revision = 99
		snap2.Members = nil // empty members
		if _, err := txStore.ReplaceDispatcherSnapshot(ctx, snap2); err != nil {
			return err
		}
		return deliberateErr
	})
	require.ErrorIs(t, err, deliberateErr)

	// Original snapshot should be intact.
	got, err := store.GetDispatcherSnapshot(ctx, xdpKey())
	require.NoError(t, err)
	assert.Equal(t, uint32(1), got.Revision)
	require.Len(t, got.Members, 2)
}

func TestSnapshotStore_ListSummaries(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create programs for XDP.
	prog1ID := kernel.ProgramID(1001)
	prog2ID := kernel.ProgramID(1002)
	require.NoError(t, store.Save(ctx, prog1ID, testXDPProgram("xdp_prog1")))
	require.NoError(t, store.Save(ctx, prog2ID, testXDPProgram("xdp_prog2")))

	// Create programs for TC.
	prog3ID := kernel.ProgramID(2001)
	require.NoError(t, store.Save(ctx, prog3ID, testTCProgram("tc_prog1")))

	// XDP dispatcher with 2 members.
	xdpLinkID := kernel.LinkID(501)
	xdpMember1KernelLinkID := kernel.LinkID(801)
	xdpMember2KernelLinkID := kernel.LinkID(802)
	xdpSnap := platform.DispatcherSnapshotSpec{
		Key:      xdpKey(),
		Revision: 1,
		Runtime: platform.DispatcherRuntime{
			ProgramID:    kernel.ProgramID(500),
			KernelLinkID: &xdpLinkID,
		},
		Members: []platform.DispatcherMemberSpec{
			{
				ProgramID:    prog1ID,
				ProgramName:  "xdp_prog1",
				ProgPinPath:  "/sys/fs/bpf/xdp_prog1",
				KernelLinkID: &xdpMember1KernelLinkID,
				LinkPinPath:  "/sys/fs/bpf/dispatch/xdp/link0",
				Position:     0,
				Priority:     50,
				ProceedOn:    1 << 4,
				Ifname:       "eth0",
			},
			{
				ProgramID:    prog2ID,
				ProgramName:  "xdp_prog2",
				ProgPinPath:  "/sys/fs/bpf/xdp_prog2",
				KernelLinkID: &xdpMember2KernelLinkID,
				LinkPinPath:  "/sys/fs/bpf/dispatch/xdp/link1",
				Position:     1,
				Priority:     100,
				ProceedOn:    1 << 4,
				Ifname:       "eth0",
			},
		},
	}
	_, err = store.ReplaceDispatcherSnapshot(ctx, xdpSnap)
	require.NoError(t, err)

	// TC ingress dispatcher with 1 member.
	tcPriority := uint16(50)
	tcMemberKernelLinkID := kernel.LinkID(803)
	tcSnap := platform.DispatcherSnapshotSpec{
		Key:      tcIngressKey(),
		Revision: 1,
		Runtime: platform.DispatcherRuntime{
			ProgramID:      kernel.ProgramID(700),
			FilterPriority: &tcPriority,
			FilterHandle:   ptr(uint32(1)),
		},
		Members: []platform.DispatcherMemberSpec{
			{
				ProgramID:    prog3ID,
				ProgramName:  "tc_prog1",
				ProgPinPath:  "/sys/fs/bpf/tc_prog1",
				KernelLinkID: &tcMemberKernelLinkID,
				LinkPinPath:  "/sys/fs/bpf/dispatch/tc/link0",
				Position:     0,
				Priority:     50,
				ProceedOn:    1 << 4,
				Ifname:       "eth0",
			},
		},
	}
	_, err = store.ReplaceDispatcherSnapshot(ctx, tcSnap)
	require.NoError(t, err)

	// List summaries.
	summaries, err := store.ListDispatcherSummaries(ctx)
	require.NoError(t, err)
	require.Len(t, summaries, 2)

	// Build a map by dispatcher type for order-independent assertion.
	byType := map[string]platform.DispatcherSummary{}
	for _, s := range summaries {
		byType[s.Key.Type.String()] = s
	}

	xdpSummary := byType["xdp"]
	assert.Equal(t, 2, xdpSummary.MemberCount)
	assert.Equal(t, kernel.ProgramID(500), xdpSummary.Runtime.ProgramID)
	require.NotNil(t, xdpSummary.Runtime.KernelLinkID)
	assert.Equal(t, kernel.LinkID(501), *xdpSummary.Runtime.KernelLinkID)

	tcSummary := byType["tc-ingress"]
	assert.Equal(t, 1, tcSummary.MemberCount)
	assert.Equal(t, kernel.ProgramID(700), tcSummary.Runtime.ProgramID)
	assert.Nil(t, tcSummary.Runtime.KernelLinkID)
	require.NotNil(t, tcSummary.Runtime.FilterPriority)
	assert.Equal(t, uint16(50), *tcSummary.Runtime.FilterPriority)
}

func TestSnapshotStore_TC_NullKernelLinkID(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Create a TC program.
	progID := kernel.ProgramID(2001)
	require.NoError(t, store.Save(ctx, progID, testTCProgram("tc_prog")))

	tcPriority := uint16(50)
	memberKernelLinkID := kernel.LinkID(804)
	snap := platform.DispatcherSnapshotSpec{
		Key:      tcIngressKey(),
		Revision: 1,
		Runtime: platform.DispatcherRuntime{
			ProgramID:      kernel.ProgramID(700),
			FilterPriority: &tcPriority,
			FilterHandle:   ptr(uint32(1)),
		},
		Members: []platform.DispatcherMemberSpec{
			{
				ProgramID:    progID,
				ProgramName:  "tc_prog",
				ProgPinPath:  "/sys/fs/bpf/tc_prog",
				KernelLinkID: &memberKernelLinkID,
				LinkPinPath:  "/sys/fs/bpf/dispatch/tc/link0",
				Position:     0,
				Priority:     50,
				ProceedOn:    0x04,
				Ifname:       "eth0",
			},
		},
	}

	_, err = store.ReplaceDispatcherSnapshot(ctx, snap)
	require.NoError(t, err)

	// Verify via snapshot API.
	got, err := store.GetDispatcherSnapshot(ctx, tcIngressKey())
	require.NoError(t, err)
	assert.Nil(t, got.Runtime.KernelLinkID, "TC dispatchers should have nil KernelLinkID")
	require.NotNil(t, got.Runtime.FilterPriority)
	assert.Equal(t, uint16(50), *got.Runtime.FilterPriority)
	require.Len(t, got.Members, 1)

}

func TestSnapshotStore_ProceedOnEncodingRoundTrip(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	require.NoError(t, store.Save(ctx, kernel.ProgramID(1001), testXDPProgram("xdp_default")))
	require.NoError(t, store.Save(ctx, kernel.ProgramID(1002), testXDPProgram("xdp_pass")))
	require.NoError(t, store.Save(ctx, kernel.ProgramID(2001), testTCProgram("tc_unspec")))

	xdpKernelLinkID := kernel.LinkID(501)
	xdpMemberKernelLinkID := kernel.LinkID(601)
	xdpDefault, err := store.ReplaceDispatcherSnapshot(ctx, platform.DispatcherSnapshotSpec{
		Key:      xdpKey(),
		Revision: 1,
		Runtime:  platform.DispatcherRuntime{ProgramID: kernel.ProgramID(500), KernelLinkID: &xdpKernelLinkID},
		Members: []platform.DispatcherMemberSpec{
			{
				ProgramID:    kernel.ProgramID(1001),
				ProgramName:  "xdp_default",
				ProgPinPath:  "/sys/fs/bpf/xdp_default",
				KernelLinkID: &xdpMemberKernelLinkID,
				LinkPinPath:  "/sys/fs/bpf/xdp_default_link",
				Position:     0,
				Priority:     50,
				ProceedOn:    uint32(1<<2 | 1<<31),
				Ifname:       "eth0",
			},
		},
	})
	require.NoError(t, err)

	xdpLink, err := store.GetLink(ctx, xdpDefault.Members[0].LinkID)
	require.NoError(t, err)
	xdpDetails, ok := xdpLink.Details.(bpfman.XDPDetails)
	require.True(t, ok)
	assert.Equal(t, []int32{2, 31}, xdpDetails.ProceedOn)

	xdpExplicit, err := store.ReplaceDispatcherSnapshot(ctx, platform.DispatcherSnapshotSpec{
		Key:      xdpKey(),
		Revision: 2,
		Runtime:  platform.DispatcherRuntime{ProgramID: kernel.ProgramID(501), KernelLinkID: &xdpKernelLinkID},
		Members: []platform.DispatcherMemberSpec{
			{
				ProgramID:    kernel.ProgramID(1002),
				ProgramName:  "xdp_pass",
				ProgPinPath:  "/sys/fs/bpf/xdp_pass",
				KernelLinkID: &xdpMemberKernelLinkID,
				LinkPinPath:  "/sys/fs/bpf/xdp_pass_link",
				Position:     0,
				Priority:     50,
				ProceedOn:    1 << 2,
				Ifname:       "eth0",
			},
		},
	})
	require.NoError(t, err)
	gotXDP, err := store.GetDispatcherSnapshot(ctx, xdpKey())
	require.NoError(t, err)
	require.Len(t, gotXDP.Members, 1)
	assert.Equal(t, uint32(1<<2), gotXDP.Members[0].ProceedOn)

	xdpExplicitLink, err := store.GetLink(ctx, xdpExplicit.Members[0].LinkID)
	require.NoError(t, err)
	xdpExplicitDetails, ok := xdpExplicitLink.Details.(bpfman.XDPDetails)
	require.True(t, ok)
	assert.Equal(t, []int32{2}, xdpExplicitDetails.ProceedOn)

	tcPriority := uint16(50)
	tcMemberKernelLinkID := kernel.LinkID(701)
	tcCompleted, err := store.ReplaceDispatcherSnapshot(ctx, platform.DispatcherSnapshotSpec{
		Key:      tcIngressKey(),
		Revision: 1,
		Runtime:  platform.DispatcherRuntime{ProgramID: kernel.ProgramID(700), FilterPriority: &tcPriority, FilterHandle: ptr(uint32(1))},
		Members: []platform.DispatcherMemberSpec{
			{
				ProgramID:    kernel.ProgramID(2001),
				ProgramName:  "tc_unspec",
				ProgPinPath:  "/sys/fs/bpf/tc_unspec",
				KernelLinkID: &tcMemberKernelLinkID,
				LinkPinPath:  "/sys/fs/bpf/tc_unspec_link",
				Position:     0,
				Priority:     50,
				ProceedOn:    1 << 0,
				Ifname:       "eth0",
			},
		},
	})
	require.NoError(t, err)
	gotTC, err := store.GetDispatcherSnapshot(ctx, tcIngressKey())
	require.NoError(t, err)
	require.Len(t, gotTC.Members, 1)
	assert.Equal(t, uint32(1<<0), gotTC.Members[0].ProceedOn)

	tcLink, err := store.GetLink(ctx, tcCompleted.Members[0].LinkID)
	require.NoError(t, err)
	tcDetails, ok := tcLink.Details.(bpfman.TCDetails)
	require.True(t, ok)
	assert.Equal(t, []int32{-1}, tcDetails.ProceedOn)
}

func TestSnapshotStore_EmptyMembers(t *testing.T) {
	t.Parallel()

	store, err := sqlite.NewInMemory(context.Background(), testLogger())
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Replace with zero members (dispatcher exists but no extensions).
	linkID := kernel.LinkID(501)
	snap := platform.DispatcherSnapshotSpec{
		Key:      xdpKey(),
		Revision: 1,
		Runtime: platform.DispatcherRuntime{
			ProgramID:    kernel.ProgramID(500),
			KernelLinkID: &linkID,
		},
		Members: nil,
	}

	_, err = store.ReplaceDispatcherSnapshot(ctx, snap)
	require.NoError(t, err)

	got, err := store.GetDispatcherSnapshot(ctx, xdpKey())
	require.NoError(t, err)
	assert.Equal(t, uint32(1), got.Revision)
	assert.Empty(t, got.Members)
}

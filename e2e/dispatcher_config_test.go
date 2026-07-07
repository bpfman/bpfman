//go:build e2e

package e2e

import (
	"context"
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/e2e/testnet"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/platform"
)

// dispatcherTestHarness encapsulates TC-vs-XDP differences behind a
// uniform interface so that tests exercising the shared dispatcher
// machinery can be parameterised across both dispatcher types.
type dispatcherTestHarness struct {
	name     string
	env      *TestEnv
	dispType dispatcher.DispatcherType
	iface    testnet.TestInterface

	// loadProg loads the BPF object file and returns the kernel
	// program ID. The program is automatically unloaded on test
	// cleanup.
	loadProg func(t *testing.T) kernel.ProgramID

	// makeAttachSpec creates an attach spec for the given program,
	// interface, and priority. For XDP the priority is ignored.
	makeAttachSpec func(t *testing.T, progID kernel.ProgramID, iface testnet.TestInterface, priority int) bpfman.AttachSpec

	// verifyAttachPresent asserts the dispatcher's kernel-level
	// attachment exists (TC: tc filter present; XDP: link pin exists).
	verifyAttachPresent func(t *testing.T)

	// verifyAttachAbsent asserts the dispatcher's kernel-level
	// attachment has been cleaned up.
	verifyAttachAbsent func(t *testing.T)
}

// attach creates an attachment on the harness's default interface.
func (h *dispatcherTestHarness) attach(t *testing.T, progID kernel.ProgramID, priority int) bpfman.LinkRecord {
	t.Helper()
	return h.attachTo(t, progID, h.iface, priority)
}

// attachTo creates an attachment on a specific interface.
func (h *dispatcherTestHarness) attachTo(t *testing.T, progID kernel.ProgramID, iface testnet.TestInterface, priority int) bpfman.LinkRecord {
	t.Helper()
	spec := h.makeAttachSpec(t, progID, iface, priority)
	link, err := h.env.Attach(context.Background(), spec)
	require.NoError(t, err)
	return link
}

// tryAttach attempts an attachment and returns the error (if any)
// instead of failing the test. Used by tests that expect failure.
func (h *dispatcherTestHarness) tryAttach(t *testing.T, progID kernel.ProgramID, priority int) (bpfman.LinkRecord, error) {
	t.Helper()
	spec := h.makeAttachSpec(t, progID, h.iface, priority)
	return h.env.Attach(context.Background(), spec)
}

// memberCount returns the number of members attached to the
// dispatcher for the harness's default interface.
func (h *dispatcherTestHarness) memberCount(t *testing.T) int {
	t.Helper()
	snap, err := h.env.GetDispatcherSnapshot(context.Background(), dispatcher.Key{
		Type: h.dispType, Nsid: h.iface.Nsid, Ifindex: uint32(h.iface.Ifindex),
	})
	require.NoError(t, err, "dispatcher should exist")
	return len(snap.Members)
}

// linkPosition returns the dispatcher position stored in the link
// details for the given link ID.
func (h *dispatcherTestHarness) linkPosition(t *testing.T, linkID bpfman.LinkID) int32 {
	t.Helper()
	_, details, err := h.env.GetLink(context.Background(), linkID)
	require.NoError(t, err)
	switch d := details.(type) {
	case bpfman.XDPDetails:
		return d.Position
	case bpfman.TCDetails:
		return d.Position
	default:
		t.Fatalf("unexpected link details type %T", details)
		return -1
	}
}

// linkPriority returns the priority stored in the link details for the
// given link ID.
func (h *dispatcherTestHarness) linkPriority(t *testing.T, linkID bpfman.LinkID) int32 {
	t.Helper()
	_, details, err := h.env.GetLink(context.Background(), linkID)
	require.NoError(t, err)
	switch d := details.(type) {
	case bpfman.XDPDetails:
		return d.Priority
	case bpfman.TCDetails:
		return d.Priority
	default:
		t.Fatalf("unexpected link details type %T", details)
		return -1
	}
}

// newTCIngressHarness creates a harness for TC ingress dispatcher
// tests. It creates its own TestEnv and testnet.TestInterface for isolation.
func newTCIngressHarness(t *testing.T) dispatcherTestHarness {
	t.Helper()
	RequireTC(t)

	env := NewTestEnv(t)
	iface := testnet.NewTestInterface(t)

	return dispatcherTestHarness{
		name:     "tc-ingress",
		env:      env,
		dispType: dispatcher.DispatcherTypeTCIngress,
		iface:    iface,

		loadProg: func(t *testing.T) kernel.ProgramID {
			t.Helper()
			programs, err := env.LoadFile(context.Background(),
				"testdata/bpf/tc_counter_pinned.bpf.o", []manager.ProgramSpec{
					{Type: bpfman.ProgramTypeTC, Name: "stats"},
				}, manager.LoadOpts{})
			require.NoError(t, err)
			require.Len(t, programs, 1)
			t.Cleanup(func() {
				env.Unload(context.Background(), programs[0].Status.Kernel.ID)
			})
			return programs[0].Status.Kernel.ID
		},

		makeAttachSpec: func(t *testing.T, progID kernel.ProgramID, iface testnet.TestInterface, priority int) bpfman.AttachSpec {
			t.Helper()
			tcSpec, err := bpfman.NewTCAttachSpec(
				progID, iface.Name,
				bpfman.TCDirectionIngress,
				priority,
			)
			require.NoError(t, err)
			return tcSpec
		},

		verifyAttachPresent: func(t *testing.T) {
			t.Helper()
			filters := tcIngressFilters(t, iface.Name)
			require.NotEmpty(t, filters, "TC filter should be present")
		},

		verifyAttachAbsent: func(t *testing.T) {
			t.Helper()
			filters := tcIngressFilters(t, iface.Name)
			assert.Empty(t, filters, "TC filter should be removed")
		},
	}
}

// newXDPHarness creates a harness for XDP dispatcher tests. It
// creates its own TestEnv and testnet.TestInterface for isolation.
func newXDPHarness(t *testing.T) dispatcherTestHarness {
	t.Helper()

	env := NewTestEnv(t)
	iface := testnet.NewTestInterface(t)

	return dispatcherTestHarness{
		name:     "xdp",
		env:      env,
		dispType: dispatcher.DispatcherTypeXDP,
		iface:    iface,

		loadProg: func(t *testing.T) kernel.ProgramID {
			t.Helper()
			programs, err := env.LoadFile(context.Background(),
				"testdata/bpf/xdp_counter_pinned.bpf.o", []manager.ProgramSpec{
					{Type: bpfman.ProgramTypeXDP, Name: "xdp_stats"},
				}, manager.LoadOpts{})
			require.NoError(t, err)
			require.Len(t, programs, 1)
			t.Cleanup(func() {
				env.Unload(context.Background(), programs[0].Status.Kernel.ID)
			})
			return programs[0].Status.Kernel.ID
		},

		makeAttachSpec: func(t *testing.T, progID kernel.ProgramID, iface testnet.TestInterface, priority int) bpfman.AttachSpec {
			t.Helper()
			xdpSpec, err := bpfman.NewXDPAttachSpec(progID, iface.Name, priority)
			require.NoError(t, err)
			return xdpSpec
		},

		verifyAttachPresent: func(t *testing.T) {
			t.Helper()
			linkPin := env.Layout.BPFFS().DispatcherLinkPath(
				dispatcher.DispatcherTypeXDP, iface.Nsid, uint32(iface.Ifindex))
			_, err := os.Stat(linkPin.String())
			require.NoError(t, err, "XDP link pin should exist: %s", linkPin)
		},

		verifyAttachAbsent: func(t *testing.T) {
			t.Helper()
			linkPin := env.Layout.BPFFS().DispatcherLinkPath(
				dispatcher.DispatcherTypeXDP, iface.Nsid, uint32(iface.Ifindex))
			_, err := os.Stat(linkPin.String())
			assert.True(t, os.IsNotExist(err), "XDP link pin should not exist: %s", linkPin)
		},
	}
}

// eachDispatcherType returns harnesses for every dispatcher type that
// shares the dispatcher code path (TC ingress and XDP).
func eachDispatcherType(t *testing.T) []dispatcherTestHarness {
	t.Helper()
	return []dispatcherTestHarness{
		newTCIngressHarness(t),
		newXDPHarness(t),
	}
}

// TestDispatcher_PriorityOrderingTC verifies that filling all 10
// dispatcher slots with scrambled priorities produces positions that
// reflect the correct sorted order for TC ingress. Members are sorted
// by priority ascending; when priorities collide, the secondary sort
// is by program name.
func TestDispatcher_PriorityOrderingTC(t *testing.T) {
	t.Parallel()
	testPriorityOrdering(t, newTCIngressHarness(t))
}

// TestDispatcher_PriorityOrderingXDP verifies that filling all 10
// dispatcher slots with scrambled priorities produces positions that
// reflect the correct sorted order for XDP.
func TestDispatcher_PriorityOrderingXDP(t *testing.T) {
	t.Parallel()
	testPriorityOrdering(t, newXDPHarness(t))
}

func testPriorityOrdering(t *testing.T, h dispatcherTestHarness) {
	progID := h.loadProg(t)

	// Attach all 10 slots with scrambled priorities so the
	// dispatcher must reorder them.
	priorities := []int{500, 100, 800, 300, 900, 200, 700, 400, 600, 50}
	var links []bpfman.LinkRecord
	for _, prio := range priorities {
		link := h.attach(t, progID, prio)
		links = append(links, link)
	}

	t.Cleanup(func() {
		for _, link := range links {
			h.env.Detach(context.Background(), link.ID)
		}
	})

	require.Equal(t, 10, h.memberCount(t), "all 10 slots should be occupied")

	// Positions reflect priority ascending. Build the expected
	// position for each link by sorting priorities.
	sorted := make([]int, len(priorities))
	copy(sorted, priorities)
	sort.Ints(sorted)

	rankByPriority := make(map[int]int32, len(sorted))
	for i, p := range sorted {
		rankByPriority[p] = int32(i)
	}

	for i, link := range links {
		pos := h.linkPosition(t, link.ID)
		expected := rankByPriority[priorities[i]]
		assert.Equal(t, expected, pos, "link %d (priority %d): position should be %d, got %d", i, priorities[i], expected, pos)

		prio := h.linkPriority(t, link.ID)
		assert.Equal(t, int32(priorities[i]), prio, "link %d: stored priority should match requested priority %d, got %d", i, priorities[i], prio)
	}
}

// TestTCX_PriorityOrdering verifies that attaching multiple TCX
// programs with scrambled priorities produces positions that reflect
// the correct sorted order. TCX uses native kernel multi-program
// support rather than dispatchers, so this exercises a different code
// path from the TC/XDP dispatcher tests. Each program must be a
// distinct kernel instance because TCX does not allow the same
// program to be attached to the same interface+direction twice.
func TestTCX_PriorityOrdering(t *testing.T) {
	t.Parallel()
	RequireRoot(t)
	RequireKernelVersion(t, 6, 6)

	env := NewTestEnv(t)
	iface := testnet.NewTestInterface(t)
	ctx := context.Background()

	// Scrambled priorities matching the operator test pattern.
	priorities := []int{500, 100, 800, 300, 55}

	// Load a separate program instance for each priority. TCX
	// requires distinct kernel program IDs per attachment.
	type loaded struct {
		progID kernel.ProgramID
		link   bpfman.LinkRecord
	}
	var entries []loaded

	for _, prio := range priorities {
		programs, err := env.LoadFile(ctx, "testdata/bpf/tcx_counter.bpf.o", []manager.ProgramSpec{
			{Type: bpfman.ProgramTypeTCX, Name: "tcx_stats"},
		}, manager.LoadOpts{})
		require.NoError(t, err)
		require.Len(t, programs, 1)
		progID := programs[0].Status.Kernel.ID

		tcxSpec, err := bpfman.NewTCXAttachSpec(progID, iface.Name, bpfman.TCDirectionIngress, prio)
		require.NoError(t, err)
		link, err := env.Attach(ctx, tcxSpec)
		require.NoError(t, err)

		entries = append(entries, loaded{progID: progID, link: link})
	}

	t.Cleanup(func() {
		for i := len(entries) - 1; i >= 0; i-- {
			env.Detach(context.Background(), entries[i].link.ID)
			env.Unload(context.Background(), entries[i].progID)
		}
	})

	// Verify that positions reflect priority ascending.
	sorted := make([]int, len(priorities))
	copy(sorted, priorities)
	sort.Ints(sorted)

	rankByPriority := make(map[int]int32, len(sorted))
	for i, p := range sorted {
		rankByPriority[p] = int32(i)
	}

	// Diagnostic dump: log every link's full TCXDetails before
	// asserting on positions. The position formula in
	// link_tcx_details's GetTCXDetails subquery counts siblings
	// matching (nsid, ifindex, direction); a flake where one link
	// reports an off-by-one position is consistent with one row
	// drifting on one of those tuple fields. Surfacing the raw
	// tuple values for every entry makes the drift attributable
	// directly when the assertion fails.
	type linkDump struct {
		index     int
		linkID    bpfman.LinkID
		priority  int32
		ifindex   uint32
		direction bpfman.TCDirection
		nsid      uint64
		netns     string
		position  int32
	}
	dumps := make([]linkDump, 0, len(entries))
	for i, entry := range entries {
		_, details, err := env.GetLink(ctx, entry.link.ID)
		require.NoError(t, err)
		tcxDetails, ok := details.(bpfman.TCXDetails)
		require.True(t, ok, "expected TCXDetails, got %T", details)
		dumps = append(dumps, linkDump{
			index:     i,
			linkID:    entry.link.ID,
			priority:  tcxDetails.Priority,
			ifindex:   tcxDetails.Ifindex,
			direction: tcxDetails.Direction,
			nsid:      tcxDetails.Nsid,
			netns:     tcxDetails.Netns,
			position:  tcxDetails.Position,
		})
	}
	for _, d := range dumps {
		t.Logf("tcx-link dump: index=%d link_id=%d priority=%d ifindex=%d direction=%s nsid=%d netns=%q position=%d", d.index, d.linkID, d.priority, d.ifindex, d.direction, d.nsid, d.netns, d.position)
	}

	for i, entry := range entries {
		_, details, err := env.GetLink(ctx, entry.link.ID)
		require.NoError(t, err)
		tcxDetails, ok := details.(bpfman.TCXDetails)
		require.True(t, ok, "expected TCXDetails, got %T", details)

		expected := rankByPriority[priorities[i]]
		assert.Equal(t, expected, tcxDetails.Position, "link %d (priority %d): position should be %d, got %d", i, priorities[i], expected, tcxDetails.Position)

		assert.Equal(t, int32(priorities[i]), tcxDetails.Priority, "link %d: stored priority should match requested priority %d, got %d", i, priorities[i], tcxDetails.Priority)
	}
}

// TestDispatcher_ZeroPriorityOrdering verifies Rust parity for
// priority 0: it is stored verbatim and sorts before positive
// priorities.
func TestDispatcher_ZeroPriorityOrdering(t *testing.T) {
	t.Parallel()
	for _, h := range eachDispatcherType(t) {
		t.Run(h.name, func(t *testing.T) {
			t.Parallel()
			testZeroPriorityOrdering(t, h)
		})
	}
}

func testZeroPriorityOrdering(t *testing.T, h dispatcherTestHarness) {
	progID := h.loadProg(t)

	// Attach three programs: Rust bpfman stores the raw priority and
	// sorts ascending, so priority 0 must run first.
	link25 := h.attach(t, progID, 25)
	link0 := h.attach(t, progID, 0)
	link75 := h.attach(t, progID, 75)

	t.Cleanup(func() {
		h.env.Detach(context.Background(), link25.ID)
		h.env.Detach(context.Background(), link0.ID)
		h.env.Detach(context.Background(), link75.ID)
	})

	// The stored priority is the raw requested priority.
	assert.Equal(t, int32(25), h.linkPriority(t, link25.ID), "priority=25 should be stored as 25")
	assert.Equal(t, int32(0), h.linkPriority(t, link0.ID), "priority=0 should be stored as 0")
	assert.Equal(t, int32(75), h.linkPriority(t, link75.ID), "priority=75 should be stored as 75")

	// The ordering follows Rust's raw priority sort:
	// position 0: priority 0
	// position 1: priority 25
	// position 2: priority 75
	assert.Equal(t, int32(0), h.linkPosition(t, link0.ID), "priority=0 should be at position 0")
	assert.Equal(t, int32(1), h.linkPosition(t, link25.ID), "priority=25 should be at position 1")
	assert.Equal(t, int32(2), h.linkPosition(t, link75.ID), "priority=75 should be at position 2")
}

// TestDispatcher_AttachExceedsMaxPrograms verifies that attempting to
// attach an 11th program fails with a "no free slots" error.
func TestDispatcher_AttachExceedsMaxPrograms(t *testing.T) {
	t.Parallel()
	for _, h := range eachDispatcherType(t) {
		t.Run(h.name, func(t *testing.T) {
			t.Parallel()
			testAttachExceedsMaxPrograms(t, h)
		})
	}
}

func testAttachExceedsMaxPrograms(t *testing.T, h dispatcherTestHarness) {
	progID := h.loadProg(t)

	var links []bpfman.LinkRecord
	for i := range dispatcher.MaxPrograms {
		link := h.attach(t, progID, (i+1)*100)
		links = append(links, link)
	}

	t.Cleanup(func() {
		for _, link := range links {
			h.env.Detach(context.Background(), link.ID)
		}
	})

	_, err := h.tryAttach(t, progID, 1100)
	require.Error(t, err, "11th attach should fail")
	assert.Contains(t, err.Error(), "no free dispatcher slots")
}

// TestDispatcher_SlotReusedAfterDetach verifies that detaching a
// program frees a slot that can be reused, and that the reattached
// program lands at the correct position in the sorted order.
func TestDispatcher_SlotReusedAfterDetach(t *testing.T) {
	t.Parallel()
	for _, h := range eachDispatcherType(t) {
		t.Run(h.name, func(t *testing.T) {
			t.Parallel()
			testSlotReusedAfterDetach(t, h)
		})
	}
}

func testSlotReusedAfterDetach(t *testing.T, h dispatcherTestHarness) {
	progID := h.loadProg(t)

	// Fill all 10 slots with ascending priorities.
	// links[i] has priority (i+1)*100: 100, 200, ..., 1000.
	var links []bpfman.LinkRecord
	for i := range dispatcher.MaxPrograms {
		link := h.attach(t, progID, (i+1)*100)
		links = append(links, link)
	}

	require.Equal(t, dispatcher.MaxPrograms, h.memberCount(t), "all 10 slots should be occupied")

	// Detach the 4th attachment (priority 400).
	err := h.env.Detach(context.Background(), links[3].ID)
	require.NoError(t, err, "detach link at priority 400")

	assert.Equal(t, dispatcher.MaxPrograms-1, h.memberCount(t), "should have 9 programs after detach")

	// Re-attach at priority 350. This should slot between
	// priorities 300 (position 2) and 500 (position 4), landing
	// at position 3.
	newLink := h.attach(t, progID, 350)

	t.Cleanup(func() {
		for i, link := range links {
			if i == 3 {
				continue
			}
			h.env.Detach(context.Background(), link.ID)
		}
		h.env.Detach(context.Background(), newLink.ID)
	})

	require.Equal(t, 10, h.memberCount(t), "all 10 slots should be occupied again")

	newPos := h.linkPosition(t, newLink.ID)

	// Sorted: [100,200,300,350,500,600,700,800,900,1000]
	// The new link at priority 350 should have position 3.
	assert.Equal(t, int32(3), newPos, "reattached link (priority 350) should have position 3")
}

// TestDispatcher_LifecycleAfterLastDetach verifies that removing the
// last attached program tears down the dispatcher entirely (pins
// removed), and that a fresh attachment creates a new dispatcher.
func TestDispatcher_LifecycleAfterLastDetach(t *testing.T) {
	t.Parallel()
	for _, h := range eachDispatcherType(t) {
		t.Run(h.name, func(t *testing.T) {
			t.Parallel()
			testLifecycleAfterLastDetach(t, h)
		})
	}
}

func testLifecycleAfterLastDetach(t *testing.T, h dispatcherTestHarness) {
	progID := h.loadProg(t)

	ifindex := uint32(h.iface.Ifindex)

	dispKey := dispatcher.Key{Type: h.dispType, Nsid: h.iface.Nsid, Ifindex: ifindex}

	// Phase 1: attach a single program. A dispatcher should exist.
	link := h.attach(t, progID, 100)

	snap1, err := h.env.GetDispatcherSnapshot(context.Background(), dispKey)
	require.NoError(t, err, "dispatcher should exist after attach")
	h.verifyAttachPresent(t)

	require.Len(t, snap1.Members, 1, "should have 1 program")

	// Phase 2: detach the only program. The dispatcher should be
	// fully cleaned up.
	err = h.env.Detach(context.Background(), link.ID)
	require.NoError(t, err)

	_, err = h.env.GetDispatcherSnapshot(context.Background(), dispKey)
	require.ErrorIs(t, err, platform.ErrRecordNotFound, "dispatcher should be absent from store after last detach")

	h.verifyAttachAbsent(t)

	// Phase 3: attach again. A new dispatcher should be created
	// with a different program ID.
	newLink := h.attach(t, progID, 200)
	t.Cleanup(func() {
		h.env.Detach(context.Background(), newLink.ID)
	})

	snap2, err := h.env.GetDispatcherSnapshot(context.Background(), dispKey)
	require.NoError(t, err, "dispatcher should exist after second attach")
	assert.NotEqual(t, snap1.Runtime.ProgramID, snap2.Runtime.ProgramID, "second dispatcher should have a different program ID")

	h.verifyAttachPresent(t)
	require.Len(t, snap2.Members, 1, "should have 1 program after reattach")
}

// TestDispatcher_MultipleInterfacesIndependent verifies that
// dispatcher state on one interface is independent of another.
// Detaching from interface B must not affect interface A's member count.
func TestDispatcher_MultipleInterfacesIndependent(t *testing.T) {
	t.Parallel()
	for _, h := range eachDispatcherType(t) {
		t.Run(h.name, func(t *testing.T) {
			t.Parallel()
			testMultipleInterfacesIndependent(t, h)
		})
	}
}

func testMultipleInterfacesIndependent(t *testing.T, h dispatcherTestHarness) {
	progID := h.loadProg(t)

	ifaceB := testnet.NewTestInterface(t)

	// Attach 3 programs to default interface (A).
	var linksA []bpfman.LinkRecord
	for i := range 3 {
		link := h.attach(t, progID, (i+1)*100)
		linksA = append(linksA, link)
	}
	t.Cleanup(func() {
		for _, l := range linksA {
			h.env.Detach(context.Background(), l.ID)
		}
	})

	// Attach 2 programs to interface B.
	var linksB []bpfman.LinkRecord
	for i := range 2 {
		link := h.attachTo(t, progID, ifaceB, (i+1)*100)
		linksB = append(linksB, link)
	}

	keyA := dispatcher.Key{Type: h.dispType, Nsid: h.iface.Nsid, Ifindex: uint32(h.iface.Ifindex)}
	keyB := dispatcher.Key{Type: h.dispType, Nsid: ifaceB.Nsid, Ifindex: uint32(ifaceB.Ifindex)}

	// Verify A has 3 members.
	snapA, err := h.env.GetDispatcherSnapshot(context.Background(), keyA)
	require.NoError(t, err)
	require.Len(t, snapA.Members, 3, "interface A should have 3 programs")

	// Verify B has 2 members.
	snapB, err := h.env.GetDispatcherSnapshot(context.Background(), keyB)
	require.NoError(t, err)
	require.Len(t, snapB.Members, 2, "interface B should have 2 programs")

	// Detach all programs from B.
	for i, l := range linksB {
		err := h.env.Detach(context.Background(), l.ID)
		require.NoError(t, err, "ifaceB detach %d", i)
	}

	// B's dispatcher should be gone.
	_, err = h.env.GetDispatcherSnapshot(context.Background(), keyB)
	require.ErrorIs(t, err, platform.ErrRecordNotFound, "interface B dispatcher should be absent after detaching all links")

	// A's dispatcher should still exist with 3 members.
	snapAAfter, err := h.env.GetDispatcherSnapshot(context.Background(), keyA)
	require.NoError(t, err, "interface A dispatcher should still exist")

	assert.Len(t, snapAAfter.Members, len(snapA.Members), "A's program count should be unchanged")
}

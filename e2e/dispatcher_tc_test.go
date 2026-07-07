//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/e2e/testnet"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/platform"
)

// TestTC_IngressEgressIndependence verifies that ingress and egress
// dispatchers on the same interface are fully independent. Attaching
// programs to both directions, then detaching all egress links, must
// leave the ingress dispatcher unchanged. This exercises the
// (nsid, ifindex, direction) keying in the dispatcher store.
func TestTC_IngressEgressIndependence(t *testing.T) {
	t.Parallel()
	RequireRoot(t)
	RequireTC(t)

	env := NewTestEnv(t)
	iface := testnet.NewTestInterface(t)
	ctx := context.Background()

	programs, err := env.LoadFile(ctx, "testdata/bpf/tc_counter_pinned.bpf.o", []manager.ProgramSpec{
		{Type: bpfman.ProgramTypeTC, Name: "stats"},
	}, manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, programs, 1)

	prog := programs[0]
	t.Cleanup(func() { env.Unload(context.Background(), prog.Status.Kernel.ID) })

	ifindex := uint32(iface.Ifindex)

	// Attach 3 programs to ingress.
	var ingressLinks []bpfman.LinkRecord
	for i := range 3 {
		tcSpec, err := bpfman.NewTCAttachSpec(
			prog.Status.Kernel.ID, iface.Name,
			bpfman.TCDirectionIngress,
			i*100,
		)
		require.NoError(t, err)
		link, err := env.Attach(ctx, tcSpec)
		require.NoError(t, err, "ingress attach %d", i)
		ingressLinks = append(ingressLinks, link)
	}
	t.Cleanup(func() {
		for _, l := range ingressLinks {
			env.Detach(context.Background(), l.ID)
		}
	})

	ingressKey := dispatcher.Key{Type: dispatcher.DispatcherTypeTCIngress, Nsid: iface.Nsid, Ifindex: ifindex}
	egressKey := dispatcher.Key{Type: dispatcher.DispatcherTypeTCEgress, Nsid: iface.Nsid, Ifindex: ifindex}

	// Verify ingress has 3 members.
	ingressSnap, err := env.GetDispatcherSnapshot(ctx, ingressKey)
	require.NoError(t, err)
	require.Len(t, ingressSnap.Members, 3, "ingress should have 3 programs")

	// Attach 2 programs to egress.
	var egressLinks []bpfman.LinkRecord
	for i := range 2 {
		tcSpec, err := bpfman.NewTCAttachSpec(
			prog.Status.Kernel.ID, iface.Name,
			bpfman.TCDirectionEgress,
			i*100,
		)
		require.NoError(t, err)
		link, err := env.Attach(ctx, tcSpec)
		require.NoError(t, err, "egress attach %d", i)
		egressLinks = append(egressLinks, link)
	}

	// Verify both dispatchers exist.
	_, err = env.GetDispatcherSnapshot(ctx, ingressKey)
	require.NoError(t, err, "ingress dispatcher should exist")
	egressSnap, err := env.GetDispatcherSnapshot(ctx, egressKey)
	require.NoError(t, err, "egress dispatcher should exist")

	require.Len(t, egressSnap.Members, 2, "egress should have 2 programs")

	// Detach all egress links.
	for i, l := range egressLinks {
		err := env.Detach(ctx, l.ID)
		require.NoError(t, err, "egress detach %d", i)
	}

	// Egress dispatcher should be gone.
	_, err = env.GetDispatcherSnapshot(ctx, egressKey)
	require.ErrorIs(t, err, platform.ErrRecordNotFound, "egress dispatcher should be absent after detaching all egress links")

	// Ingress dispatcher should be unaffected.
	ingressSnapAfter, err := env.GetDispatcherSnapshot(ctx, ingressKey)
	require.NoError(t, err, "ingress dispatcher should still exist")

	assert.Len(t, ingressSnapAfter.Members, len(ingressSnap.Members), "ingress program count should be unchanged")
}

// TestTC_DispatcherPriorityTieBreakByName verifies that when two
// programs share the same priority, the dispatcher orders them
// alphabetically by program name. "beta" is attached first (slot 0),
// "alpha" second (slot 1), both at priority 100. Alpha should have a
// lower position than beta, proving the secondary sort.
func TestTC_DispatcherPriorityTieBreakByName(t *testing.T) {
	t.Parallel()
	RequireRoot(t)
	RequireTC(t)

	env := NewTestEnv(t)
	iface := testnet.NewTestInterface(t)
	ctx := context.Background()

	// Load "beta" and "alpha" as separate programs so they have
	// distinct Meta.Name values in the dispatcher slot records.
	progsBeta, err := env.LoadFile(ctx, "testdata/bpf/tc_pass.bpf.o", []manager.ProgramSpec{
		{Type: bpfman.ProgramTypeTC, Name: "beta"},
	}, manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, progsBeta, 1)
	beta := progsBeta[0]
	t.Cleanup(func() { env.Unload(context.Background(), beta.Status.Kernel.ID) })

	progsAlpha, err := env.LoadFile(ctx, "testdata/bpf/tc_pass.bpf.o", []manager.ProgramSpec{
		{Type: bpfman.ProgramTypeTC, Name: "alpha"},
	}, manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, progsAlpha, 1)
	alpha := progsAlpha[0]
	t.Cleanup(func() { env.Unload(context.Background(), alpha.Status.Kernel.ID) })

	// Attach beta first (gets slot 0), then alpha (gets slot 1).
	// Both at the same priority so the tie-break decides ordering.
	tcBeta, err := bpfman.NewTCAttachSpec(
		beta.Status.Kernel.ID, iface.Name,
		bpfman.TCDirectionIngress,
		100,
	)
	require.NoError(t, err)
	linkBeta, err := env.Attach(ctx, tcBeta)
	require.NoError(t, err)
	t.Cleanup(func() { env.Detach(context.Background(), linkBeta.ID) })

	tcAlpha, err := bpfman.NewTCAttachSpec(
		alpha.Status.Kernel.ID, iface.Name,
		bpfman.TCDirectionIngress,
		100,
	)
	require.NoError(t, err)
	linkAlpha, err := env.Attach(ctx, tcAlpha)
	require.NoError(t, err)
	t.Cleanup(func() { env.Detach(context.Background(), linkAlpha.ID) })

	// Verify via link details that alpha has a lower position than
	// beta, proving the alphabetical tie-break. Position reflects
	// the sorted execution order (priority ASC, name ASC) and is
	// recomputed on each dispatcher rebuild.
	_, alphaDetails, err := env.GetLink(ctx, linkAlpha.ID)
	require.NoError(t, err)
	alphaTCDetails, ok := alphaDetails.(bpfman.TCDetails)
	require.True(t, ok, "alpha link should have TC details")

	_, betaDetails, err := env.GetLink(ctx, linkBeta.ID)
	require.NoError(t, err)
	betaTCDetails, ok := betaDetails.(bpfman.TCDetails)
	require.True(t, ok, "beta link should have TC details")

	assert.Less(t, alphaTCDetails.Position, betaTCDetails.Position, "alpha (position=%d) should precede beta (position=%d)", alphaTCDetails.Position, betaTCDetails.Position)
}

// TestTC_DispatcherFillDrainRefill exercises repeated fill-drain-refill
// cycles where the drain boundary shifts each oscillation, verifying
// that slot reuse, member count tracking, and traffic delivery
// remain correct throughout.
//
// The test performs three oscillations. Each trough drains one more
// slot than the previous (6, 7, 8) and the drain region alternates
// between the low and high ends of the slot space:
//
//	Peak 0:   fill all 10 slots                     [0-9 occupied]
//	Trough 1: drain first 6    (slots 0-5 freed)    [6-9 survive]
//	Peak 1:   refill 6         (slots 0-5 reused)   [0-9 occupied]
//	Trough 2: drain last 7     (slots 3-9 freed)    [0-2 survive]
//	Peak 2:   refill 7         (slots 3-9 reused)   [0-9 occupied]
//	Trough 3: drain first 8    (slots 0-7 freed)    [8-9 survive]
//	Peak 3:   refill 8         (slots 0-7 reused)   [0-9 occupied]
//
// The shifting drain boundary ensures that every physical slot
// position is both vacated and reused at least once.
//
// At each peak the test asserts:
//   - Member count equals dispatcher.MaxPrograms (10).
//   - Every program (including newly attached ones) receives new
//     traffic: packet counts are recorded before a ping burst and
//     verified to have increased afterwards.
//
// At each trough the test asserts:
//   - Member count equals the surviving program count.
//
// Programs are loaded as separate instances (one LoadImage per
// program) so that each has an independent tc_stats_map for traffic
// verification. Proceed-on is set to TC_ACT_OK|TC_ACT_PIPE|
// DispatcherReturn on every attachment so the full chain executes.
func TestTC_DispatcherFillDrainRefill(t *testing.T) {
	t.Parallel()
	RequireRoot(t)
	RequireTC(t)

	env := NewTestEnv(t)
	veth := testnet.NewTestVethPair(t)
	ctx := context.Background()

	objFile := "testdata/bpf/tc_counter.bpf.o"
	proceedOn := []int32{0, 3, 30} // TC_ACT_OK, TC_ACT_PIPE, DispatcherReturn

	type prog struct {
		kernelID   kernel.ProgramID
		mapPinPath string
	}

	loadProg := func() prog {
		programs, err := env.LoadFile(ctx, objFile, []manager.ProgramSpec{
			{Type: bpfman.ProgramTypeTC, Name: "stats"},
		}, manager.LoadOpts{})
		require.NoError(t, err)
		require.Len(t, programs, 1)
		p := programs[0]
		t.Cleanup(func() { env.Unload(context.Background(), p.Status.Kernel.ID) })
		return prog{p.Status.Kernel.ID, p.Record.Handles.MapsDir.String()}
	}

	attachProg := func(p prog, priority int) bpfman.LinkRecord {
		tcSpec, err := bpfman.NewTCAttachSpec(
			p.kernelID, veth.A.Name,
			bpfman.TCDirectionIngress,
			priority,
		)
		require.NoError(t, err)
		tcSpec, err = tcSpec.WithProceedOnCodes(proceedOn)
		require.NoError(t, err)
		link, err := env.Attach(ctx, tcSpec)
		require.NoError(t, err, "attach at priority %d", priority)
		return link
	}

	type slotEntry struct {
		prog     prog
		link     bpfman.LinkRecord
		priority int
	}

	var slots [dispatcher.MaxPrograms]*slotEntry

	t.Cleanup(func() {
		for _, s := range slots {
			if s != nil {
				env.Detach(context.Background(), s.link.ID)
			}
		}
	})

	// fill loads and attaches len(priorities) new programs into
	// the first available free slots.
	fill := func(priorities []int) {
		t.Helper()
		start := time.Now()
		j := 0
		for i := range dispatcher.MaxPrograms {
			if slots[i] != nil || j >= len(priorities) {
				continue
			}
			p := loadProg()
			link := attachProg(p, priorities[j])
			slots[i] = &slotEntry{p, link, priorities[j]}
			j++
		}
		require.Equal(t, len(priorities), j, "fill: not enough free slots")
		t.Logf("fill %d programs: %v", len(priorities), time.Since(start))
	}

	// drain detaches programs in slots [lo, hi).
	drain := func(lo, hi int) {
		t.Helper()
		start := time.Now()
		for i := lo; i < hi; i++ {
			require.NotNilf(t, slots[i], "drain: slot %d already empty", i)
			detachStart := time.Now()
			err := env.Detach(ctx, slots[i].link.ID)
			require.NoError(t, err, "drain: detach slot %d", i)
			t.Logf("drain: detach slot %d: %v", i, time.Since(detachStart))
			slots[i] = nil
		}
		t.Logf("drain %d programs [%d,%d): %v", hi-lo, lo, hi, time.Since(start))
	}

	// occupiedCount returns the number of non-nil slots.
	occupiedCount := func() int {
		var n int
		for _, s := range slots {
			if s != nil {
				n++
			}
		}
		return n
	}

	// verifyMemberCount reads the dispatcher member count and
	// asserts it matches the expected state derived from the slot
	// array.
	verifyMemberCount := func(phase string) {
		t.Helper()
		snap, err := env.GetDispatcherSnapshot(ctx, dispatcher.Key{
			Type: dispatcher.DispatcherTypeTCIngress, Nsid: veth.A.Nsid, Ifindex: uint32(veth.A.Ifindex),
		})
		require.NoError(t, err)
		expected := occupiedCount()
		assert.Equal(t, expected, len(snap.Members), "%s: member count", phase)
	}

	// verifyTraffic sends traffic and asserts every active
	// program's packet count increased.
	verifyTraffic := func(phase string) {
		t.Helper()
		var active []prog
		for _, s := range slots {
			if s != nil {
				active = append(active, s.prog)
			}
		}
		before := make([]uint64, len(active))
		for i, p := range active {
			before[i] = readStatsMap(t, filepath.Join(p.mapPinPath, "tc_stats_map"))
		}
		veth.Ping(t, 20)
		for i, p := range active {
			after := readStatsMap(t, filepath.Join(p.mapPinPath, "tc_stats_map"))
			assert.Greater(t, after, before[i], "%s: program %d (kernel_id=%d) should have received new traffic", phase, i, p.kernelID)
		}
	}

	// -- Peak 0: fill all 10 slots
	initialPriorities := make([]int, dispatcher.MaxPrograms)
	for i := range initialPriorities {
		initialPriorities[i] = i * 100 // 0, 100, 200, ..., 900
	}
	fill(initialPriorities)
	verifyMemberCount("peak 0")
	verifyTraffic("peak 0")

	// -- Trough 1: drain first 6
	// Slots 0-5 freed; slots 6-9 survive (priorities 600-900).
	drain(0, 6)
	verifyMemberCount("trough 1")

	// -- Peak 1: refill 6
	// Priorities interleave with surviving 600, 700, 800, 900.
	fill([]int{950, 850, 750, 650, 550, 450})
	verifyMemberCount("peak 1")
	verifyTraffic("peak 1")

	// -- Trough 2: drain last 7
	// Slots 3-9 freed; slots 0-2 survive (priorities 950, 850,
	// 750 from wave 1).
	drain(3, 10)
	verifyMemberCount("trough 2")

	// -- Peak 2: refill 7
	// Priorities interleave with surviving 950, 850, 750.
	fill([]int{25, 125, 225, 325, 425, 525, 625})
	verifyMemberCount("peak 2")
	verifyTraffic("peak 2")

	// -- Trough 3: drain first 8
	// Slots 0-7 freed; slots 8-9 survive (priorities 525, 625
	// from wave 2).
	drain(0, 8)
	verifyMemberCount("trough 3")

	// -- Peak 3: refill 8
	// Priorities interleave with surviving 525, 625.
	fill([]int{62, 162, 262, 362, 462, 562, 662, 762})
	verifyMemberCount("peak 3")
	verifyTraffic("peak 3")
}

// tcStatsEntry matches the BPF struct used by go-tc-counter.
type tcStatsEntry struct {
	Packets uint64
	Bytes   uint64
}

// readStatsMap loads a pinned tc_stats_map (PerCPUArray) and returns
// the total packet count summed across all CPUs. The go-tc-counter
// program stores a tcStatsEntry per CPU at key 0.
func readStatsMap(t *testing.T, mapPinPath string) uint64 {
	t.Helper()

	m, err := ebpf.LoadPinnedMap(mapPinPath, nil)
	require.NoError(t, err, "load pinned tc_stats_map at %s", mapPinPath)
	defer m.Close()

	var perCPU []tcStatsEntry
	err = m.Lookup(uint32(0), &perCPU)
	require.NoError(t, err, "lookup key 0 in tc_stats_map")

	var total uint64
	for _, entry := range perCPU {
		total += entry.Packets
	}
	return total
}

// TestTC_DispatcherChainExecution verifies that all programs in a TC
// dispatch chain actually execute when real traffic flows through the
// interface. Five separate programs are loaded and attached at
// different priorities; after sending traffic through a veth pair,
// each program's independent counter map must show a non-zero packet
// count.
func TestTC_DispatcherChainExecution(t *testing.T) {
	t.Parallel()
	RequireRoot(t)
	RequireTC(t)

	env := NewTestEnv(t)
	veth := testnet.NewTestVethPair(t)
	ctx := context.Background()

	objFile := "testdata/bpf/tc_counter.bpf.o"

	// Load 5 separate instances so each gets independent maps.
	type loadedProg struct {
		kernelID   kernel.ProgramID
		mapPinPath string
	}

	var progs []loadedProg
	for i := range 5 {
		programs, err := env.LoadFile(ctx, objFile, []manager.ProgramSpec{
			{Type: bpfman.ProgramTypeTC, Name: "stats"},
		}, manager.LoadOpts{})
		require.NoError(t, err, "load %d", i)
		require.Len(t, programs, 1)

		prog := programs[0]
		t.Cleanup(func() { env.Unload(context.Background(), prog.Status.Kernel.ID) })

		progs = append(progs, loadedProg{
			kernelID:   prog.Status.Kernel.ID,
			mapPinPath: prog.Record.Handles.MapsDir.String(),
		})
	}

	// Attach each at a different priority with proceed-on
	// OK|Pipe|DispatcherReturn so the chain continues through all
	// programs.
	priorities := []int{500, 100, 300, 200, 400}
	proceedOn := []int32{0, 3, 30} // TC_ACT_OK, TC_ACT_PIPE, bpfman dispatcher return

	var linkIDs []bpfman.LinkRecord
	for i, prio := range priorities {
		tcSpec, err := bpfman.NewTCAttachSpec(
			progs[i].kernelID, veth.A.Name,
			bpfman.TCDirectionIngress,
			prio,
		)
		require.NoError(t, err)
		tcSpec, err = tcSpec.WithProceedOnCodes(proceedOn)
		require.NoError(t, err)
		link, err := env.Attach(ctx, tcSpec)
		require.NoError(t, err, "attach %d at priority %d", i, prio)
		linkIDs = append(linkIDs, link)
	}

	t.Cleanup(func() {
		for _, link := range linkIDs {
			env.Detach(context.Background(), link.ID)
		}
	})

	// Send traffic through the veth pair.
	veth.Ping(t, 20)

	// Read each program's counter map and verify non-zero counts.
	for i, prog := range progs {
		statsPath := filepath.Join(prog.mapPinPath, "tc_stats_map")
		packets := readStatsMap(t, statsPath)
		t.Logf("program %d (kernel_id=%d, priority=%d): %d packets", i, prog.kernelID, priorities[i], packets)
		assert.Greater(t, packets, uint64(0), "program %d (priority %d) should have counted packets", i, priorities[i])
	}
}

// TestTC_DispatcherChainProceedOn verifies that the TC dispatcher
// chain-break logic works correctly. When a program's proceed-on
// configuration excludes the action returned by that program
// (TC_ACT_OK), the dispatcher must stop the chain: programs after
// the break point must see exactly zero packets.
func TestTC_DispatcherChainProceedOn(t *testing.T) {
	t.Parallel()
	RequireRoot(t)
	RequireTC(t)

	// proceed-on that includes TC_ACT_OK (0): chain continues.
	proceedOnContinue := []int32{0, 3, 30} // OK, Pipe, DispatcherReturn

	// proceed-on that excludes TC_ACT_OK: chain stops here.
	// go-tc-counter always returns TC_ACT_OK, so requiring only
	// TC_ACT_SHOT (2) causes the dispatcher to halt the chain.
	proceedOnStop := []int32{2} // TC_ACT_SHOT only

	tests := []struct {
		name    string
		n       int
		breakAt int // execution position where chain stops; -1 = all proceed
	}{
		{"single program", 1, -1},
		{"3 programs, all proceed", 3, -1},
		{"3 programs, break after first", 3, 0},
		{"3 programs, break after second", 3, 1},
		{"3 programs, break after third", 3, 2},
		{"5 programs, all proceed", 5, -1},
		{"5 programs, break after first", 5, 0},
		{"5 programs, break after third", 5, 2},
		{"5 programs, break after fifth", 5, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := NewTestEnv(t)
			veth := testnet.NewTestVethPair(t)
			ctx := context.Background()

			objFile := "testdata/bpf/tc_counter.bpf.o"

			type loadedProg struct {
				kernelID   kernel.ProgramID
				mapPinPath string
			}

			var progs []loadedProg
			for i := 0; i < tt.n; i++ {
				programs, err := env.LoadFile(ctx, objFile, []manager.ProgramSpec{
					{Type: bpfman.ProgramTypeTC, Name: "stats"},
				}, manager.LoadOpts{})
				require.NoError(t, err, "load %d", i)
				require.Len(t, programs, 1)

				prog := programs[0]
				t.Cleanup(func() { env.Unload(context.Background(), prog.Status.Kernel.ID) })

				progs = append(progs, loadedProg{
					kernelID:   prog.Status.Kernel.ID,
					mapPinPath: prog.Record.Handles.MapsDir.String(),
				})
			}

			// Attach each program at ascending priorities so
			// attachment order equals execution order.
			var linkIDs []bpfman.LinkRecord
			for i := 0; i < tt.n; i++ {
				tcSpec, err := bpfman.NewTCAttachSpec(
					progs[i].kernelID, veth.A.Name,
					bpfman.TCDirectionIngress,
					(i+1)*100,
				)
				require.NoError(t, err)

				po := proceedOnContinue
				if tt.breakAt >= 0 && i == tt.breakAt {
					po = proceedOnStop
				}

				tcSpec, err = tcSpec.WithProceedOnCodes(po)
				require.NoError(t, err)
				link, err := env.Attach(ctx, tcSpec)
				require.NoError(t, err, "attach %d at priority %d", i, (i+1)*100)
				linkIDs = append(linkIDs, link)
			}

			t.Cleanup(func() {
				for _, link := range linkIDs {
					env.Detach(context.Background(), link.ID)
				}
			})

			// Send traffic through the veth pair.
			veth.Ping(t, 20)

			// Verify packet counts for each program.
			for i, prog := range progs {
				statsPath := filepath.Join(prog.mapPinPath, "tc_stats_map")
				packets := readStatsMap(t, statsPath)
				t.Logf("program %d (kernel_id=%d): %d packets", i, prog.kernelID, packets)

				if tt.breakAt == -1 || i <= tt.breakAt {
					assert.Greater(t, packets, uint64(0), "program %d should have counted packets (at or before break point)", i)
				} else {
					assert.Equal(t, uint64(0), packets, "program %d should have zero packets (after break point at position %d)", i, tt.breakAt)
				}
			}
		})
	}
}

// TestTC_EgressTrafficCounting verifies that TC programs attached in
// the egress direction see real traffic. Three programs are loaded
// separately (so each has an independent stats map), attached to
// egress on a veth interface, and then traffic is generated by
// pinging from the peer namespace to the root namespace. The ICMP
// replies leaving through A's egress must be counted by all three
// programs.
func TestTC_EgressTrafficCounting(t *testing.T) {
	t.Parallel()
	RequireRoot(t)
	RequireTC(t)

	env := NewTestEnv(t)
	veth := testnet.NewTestVethPair(t)
	ctx := context.Background()

	objFile := "testdata/bpf/tc_counter.bpf.o"

	type loadedProg struct {
		kernelID   kernel.ProgramID
		mapPinPath string
	}

	// Load 3 separate instances so each gets independent maps.
	var progs []loadedProg
	for i := range 3 {
		programs, err := env.LoadFile(ctx, objFile, []manager.ProgramSpec{
			{Type: bpfman.ProgramTypeTC, Name: "stats"},
		}, manager.LoadOpts{})
		require.NoError(t, err, "load %d", i)
		require.Len(t, programs, 1)

		prog := programs[0]
		t.Cleanup(func() { env.Unload(context.Background(), prog.Status.Kernel.ID) })

		progs = append(progs, loadedProg{
			kernelID:   prog.Status.Kernel.ID,
			mapPinPath: prog.Record.Handles.MapsDir.String(),
		})
	}

	// Attach each at a different priority on egress with
	// proceed-on OK|Pipe|DispatcherReturn so the full chain
	// executes.
	proceedOn := []int32{0, 3, 30}
	var linkIDs []bpfman.LinkRecord
	for i, prio := range []int{100, 200, 300} {
		tcSpec, err := bpfman.NewTCAttachSpec(
			progs[i].kernelID, veth.A.Name,
			bpfman.TCDirectionEgress,
			prio,
		)
		require.NoError(t, err)
		tcSpec, err = tcSpec.WithProceedOnCodes(proceedOn)
		require.NoError(t, err)
		link, err := env.Attach(ctx, tcSpec)
		require.NoError(t, err, "attach %d at priority %d", i, prio)
		linkIDs = append(linkIDs, link)
	}

	t.Cleanup(func() {
		for _, link := range linkIDs {
			env.Detach(context.Background(), link.ID)
		}
	})

	// Send traffic: ping from B to A. The ICMP replies leave
	// through A's egress, where the programs are attached.
	veth.Ping(t, 20)

	// Each program's stats map must show non-zero packet counts.
	for i, prog := range progs {
		statsPath := filepath.Join(prog.mapPinPath, "tc_stats_map")
		packets := readStatsMap(t, statsPath)
		t.Logf("egress program %d (kernel_id=%d): %d packets", i, prog.kernelID, packets)
		assert.Greater(t, packets, uint64(0), "egress program %d should have counted packets", i)
	}
}

// TestTC_DefaultProceedOnRebuild reproduces the operator integration
// test failure. Two TC programs with the same priority and default
// proceed-on (Pipe|DispatcherReturn, no TC_ACT_OK) share a
// dispatcher. The BPF program returns TC_ACT_OK, so the chain stops
// after position 0.
//
// Rust bpfman sorts new (unattached) programs before existing ones
// at the same priority, giving the newly-added program position 0.
// The sort here must match so the newly-attached program executes.
func TestTC_DefaultProceedOnRebuild(t *testing.T) {
	t.Parallel()
	RequireRoot(t)
	RequireTC(t)

	env := NewTestEnv(t)
	veth := testnet.NewTestVethPair(t)
	ctx := context.Background()

	objFile := "testdata/bpf/tc_counter.bpf.o"

	// Default proceed-on matching the operator CRD default.
	defaultProceedOn := []int32{3, 30} // Pipe, DispatcherReturn

	type prog struct {
		id         kernel.ProgramID
		mapPinPath string
	}

	load := func() prog {
		programs, err := env.LoadFile(ctx, objFile, []manager.ProgramSpec{
			{Type: bpfman.ProgramTypeTC, Name: "stats"},
		}, manager.LoadOpts{})
		require.NoError(t, err)
		require.Len(t, programs, 1)
		p := programs[0]
		t.Cleanup(func() { env.Unload(context.Background(), p.Status.Kernel.ID) })
		return prog{id: p.Status.Kernel.ID, mapPinPath: p.Record.Handles.MapsDir.String()}
	}

	attach := func(p prog, priority int) {
		spec, err := bpfman.NewTCAttachSpec(p.id, veth.A.Name, bpfman.TCDirectionIngress, priority)
		require.NoError(t, err)
		spec, err = spec.WithProceedOnCodes(defaultProceedOn)
		require.NoError(t, err)
		link, err := env.Attach(ctx, spec)
		require.NoError(t, err)
		t.Cleanup(func() { env.Detach(context.Background(), link.ID) })
	}

	packets := func(p prog) uint64 {
		return readStatsMap(t, filepath.Join(p.mapPinPath, "tc_stats_map"))
	}

	existing := load()
	incoming := load()

	// Attach existing program first (simulates the app-counter
	// that is already on eth0 from a prior test).
	attach(existing, 55)

	// Attach incoming program at the same priority (simulates the
	// tc-counter being deployed by the operator). This triggers a
	// dispatcher rebuild.
	attach(incoming, 55)

	// Dump snapshot for diagnostics.
	snap, err := env.GetDispatcherSnapshot(ctx, dispatcher.Key{
		Type: dispatcher.DispatcherTypeTCIngress, Nsid: veth.A.Nsid, Ifindex: uint32(veth.A.Ifindex),
	})
	require.NoError(t, err)
	for _, m := range snap.Members {
		t.Logf("position=%d program_id=%d name=%q priority=%d proceed_on=%#x", m.Position, m.ProgramID, m.ProgramName, m.Priority, m.ProceedOn)
	}

	// The newly-attached program must be at position 0, matching
	// Rust bpfman behaviour. When two programs have the same
	// priority and name, the new (unattached) program sorts before
	// the existing one.
	require.Len(t, snap.Members, 2)
	memberByPos := make(map[int]platform.DispatcherMember)
	for _, m := range snap.Members {
		memberByPos[m.Position] = m
	}
	assert.Equal(t, incoming.id, memberByPos[0].ProgramID, "newly-attached program should be at position 0")
	assert.Equal(t, existing.id, memberByPos[1].ProgramID, "previously-attached program should be at position 1")

	// With the incoming program at position 0, it must see traffic.
	veth.Ping(t, 20)
	t.Logf("existing=%d packets, incoming=%d packets", packets(existing), packets(incoming))
	assert.Greater(t, packets(incoming), uint64(0), "incoming program at position 0 should count packets")
}

// TestTC_MultiPriorityChainDefaultProceedOn verifies that the default
// TC proceed-on (Pipe|DispatcherReturn, matching Rust bpfman) stops
// the chain when a program returns TC_ACT_OK. Only position 0
// executes; programs at higher positions see zero packets.
//
// This matches the behaviour observed in the operator integration
// test TestTcGoCounterLinkPriority, where Rust appears to pass only
// because the counter pod carries cumulative counts from the
// preceding TestTcGoCounter test.
func TestTC_MultiPriorityChainDefaultProceedOn(t *testing.T) {
	t.Parallel()
	RequireRoot(t)
	RequireTC(t)

	env := NewTestEnv(t)
	veth := testnet.NewTestVethPair(t)
	ctx := context.Background()

	objFile := "testdata/bpf/tc_counter.bpf.o"

	type prog struct {
		id         kernel.ProgramID
		mapPinPath string
	}

	load := func() prog {
		programs, err := env.LoadFile(ctx, objFile, []manager.ProgramSpec{
			{Type: bpfman.ProgramTypeTC, Name: "stats"},
		}, manager.LoadOpts{})
		require.NoError(t, err)
		require.Len(t, programs, 1)
		p := programs[0]
		t.Cleanup(func() { env.Unload(context.Background(), p.Status.Kernel.ID) })
		return prog{id: p.Status.Kernel.ID, mapPinPath: p.Record.Handles.MapsDir.String()}
	}

	// Attach WITHOUT specifying proceed-on, relying on the manager
	// default (DefaultTCProceedOn = Pipe|DispatcherReturn).
	attach := func(p prog, priority int) {
		spec, err := bpfman.NewTCAttachSpec(p.id, veth.A.Name, bpfman.TCDirectionIngress, priority)
		require.NoError(t, err)
		link, err := env.Attach(ctx, spec)
		require.NoError(t, err)
		t.Cleanup(func() { env.Detach(context.Background(), link.ID) })
	}

	packets := func(p prog) uint64 {
		return readStatsMap(t, filepath.Join(p.mapPinPath, "tc_stats_map"))
	}

	// Three programs at different priorities. All return TC_ACT_OK.
	progs := make([]prog, 3)
	priorities := []int{0, 55, 100}
	for i := range progs {
		progs[i] = load()
	}
	for i, p := range progs {
		attach(p, priorities[i])
	}

	veth.Ping(t, 20)

	// Default proceed-on excludes TC_ACT_OK, so the chain stops
	// after position 0. Only the lowest-priority program counts.
	for i, p := range progs {
		count := packets(p)
		t.Logf("priority=%d program_id=%d packets=%d", priorities[i], p.id, count)
		if i == 0 {
			assert.Greater(t, count, uint64(0), "position 0 (priority %d) should count packets", priorities[i])
		} else {
			assert.Equal(t, uint64(0), count, "position %d (priority %d) should NOT count packets with default proceed-on", i, priorities[i])
		}
	}
}

// TestTC_MultiPriorityChainWithOKProceedOn verifies that when
// TC_ACT_OK is explicitly included in proceed-on, the chain
// continues past each position and all programs count packets.
func TestTC_MultiPriorityChainWithOKProceedOn(t *testing.T) {
	t.Parallel()
	RequireRoot(t)
	RequireTC(t)

	env := NewTestEnv(t)
	veth := testnet.NewTestVethPair(t)
	ctx := context.Background()

	objFile := "testdata/bpf/tc_counter.bpf.o"

	// Proceed-on including TC_ACT_OK so the chain continues.
	proceedOn := []int32{0, 3, 30} // OK, Pipe, DispatcherReturn

	type prog struct {
		id         kernel.ProgramID
		mapPinPath string
	}

	load := func() prog {
		programs, err := env.LoadFile(ctx, objFile, []manager.ProgramSpec{
			{Type: bpfman.ProgramTypeTC, Name: "stats"},
		}, manager.LoadOpts{})
		require.NoError(t, err)
		require.Len(t, programs, 1)
		p := programs[0]
		t.Cleanup(func() { env.Unload(context.Background(), p.Status.Kernel.ID) })
		return prog{id: p.Status.Kernel.ID, mapPinPath: p.Record.Handles.MapsDir.String()}
	}

	attach := func(p prog, priority int) {
		spec, err := bpfman.NewTCAttachSpec(p.id, veth.A.Name, bpfman.TCDirectionIngress, priority)
		require.NoError(t, err)
		spec, err = spec.WithProceedOnCodes(proceedOn)
		require.NoError(t, err)
		link, err := env.Attach(ctx, spec)
		require.NoError(t, err)
		t.Cleanup(func() { env.Detach(context.Background(), link.ID) })
	}

	packets := func(p prog) uint64 {
		return readStatsMap(t, filepath.Join(p.mapPinPath, "tc_stats_map"))
	}

	progs := make([]prog, 3)
	priorities := []int{0, 55, 100}
	for i := range progs {
		progs[i] = load()
	}
	for i, p := range progs {
		attach(p, priorities[i])
	}

	veth.Ping(t, 20)

	// With TC_ACT_OK in proceed-on, every program counts packets.
	for i, p := range progs {
		count := packets(p)
		t.Logf("priority=%d program_id=%d packets=%d", priorities[i], p.id, count)
		assert.Greater(t, count, uint64(0), "program at priority %d should count packets", priorities[i])
	}
}

// TestTC_PinByNameMapSharing verifies that loading the same BPF
// object file twice (without an explicit MapOwnerID)
// produces programs that share kernel maps declared with
// LIBBPF_PIN_BY_NAME. This matches aya/Rust-bpfman behaviour where
// PinByName maps are implicitly shared via a well-known pin path.
//
// The test:
//  1. Loads tc_counter_pinned.bpf.o twice as separate LoadFile calls.
//  2. Verifies both programs reference the same kernel map ID for
//     tc_stats_map (proving the map is shared, not duplicated).
//  3. Attaches both programs with proceed-on including TC_ACT_OK so
//     both execute in the dispatch chain.
//  4. Sends traffic and verifies both per-program map pins report
//     the same non-zero packet count (proving writes go to the same
//     underlying kernel map).
func TestTC_PinByNameMapSharing(t *testing.T) {
	t.Parallel()
	RequireRoot(t)
	RequireTC(t)
	RequireIsolatedRuntime(t,
		"asserts the shared tc_stats_map pin is removed after the last "+
			"user unloads -- a global property that only holds when this "+
			"test owns the whole bpffs; under shared mode other tests "+
			"legitimately keep the same shared map alive")

	env := NewTestEnv(t)
	veth := testnet.NewTestVethPair(t)
	ctx := context.Background()

	objFile := "testdata/bpf/tc_counter_pinned.bpf.o"

	type prog struct {
		kernelID   kernel.ProgramID
		mapPinPath string
	}

	load := func() prog {
		programs, err := env.LoadFile(ctx, objFile, []manager.ProgramSpec{
			{Type: bpfman.ProgramTypeTC, Name: "stats"},
		}, manager.LoadOpts{})
		require.NoError(t, err)
		require.Len(t, programs, 1)
		p := programs[0]
		return prog{p.Status.Kernel.ID, p.Record.Handles.MapsDir.String()}
	}

	progA := load()
	progB := load()

	// Each program should have a distinct kernel ID (they are
	// separate program loads).
	require.NotEqual(t, progA.kernelID, progB.kernelID, "programs should have distinct kernel IDs")

	// Verify map sharing: load the pinned tc_stats_map from each
	// program's per-program directory and compare kernel map IDs.
	mapIDFor := func(p prog) ebpf.MapID {
		t.Helper()
		statsPath := filepath.Join(p.mapPinPath, "tc_stats_map")
		m, err := ebpf.LoadPinnedMap(statsPath, nil)
		require.NoError(t, err, "load pinned tc_stats_map for program %d", p.kernelID)
		defer m.Close()
		info, err := m.Info()
		require.NoError(t, err, "get map info for program %d", p.kernelID)
		id, ok := info.ID()
		require.True(t, ok, "map ID should be available")
		return id
	}

	mapIDA := mapIDFor(progA)
	mapIDB := mapIDFor(progB)

	t.Logf("program A (kernel_id=%d): tc_stats_map ID=%d", progA.kernelID, mapIDA)
	t.Logf("program B (kernel_id=%d): tc_stats_map ID=%d", progB.kernelID, mapIDB)

	require.Equal(t, mapIDA, mapIDB, "both programs should share the same kernel map for tc_stats_map (PinByName sharing)")

	// Attach both programs at different priorities with
	// proceed-on including TC_ACT_OK so both execute.
	proceedOn := []int32{0, 3, 30} // TC_ACT_OK, TC_ACT_PIPE, DispatcherReturn

	var linkIDs []bpfman.LinkID
	for i, p := range []prog{progA, progB} {
		tcSpec, err := bpfman.NewTCAttachSpec(
			p.kernelID, veth.A.Name,
			bpfman.TCDirectionIngress,
			(i+1)*100,
		)
		require.NoError(t, err)
		tcSpec, err = tcSpec.WithProceedOnCodes(proceedOn)
		require.NoError(t, err)
		link, err := env.Attach(ctx, tcSpec)
		require.NoError(t, err, "attach program %d", i)
		linkIDs = append(linkIDs, link.ID)
	}

	// Send traffic through the veth pair.
	veth.Ping(t, 20)

	// Detach both programs before reading the map, so the
	// per-CPU array is frozen while we sample it. Reading a
	// live counter via two sequential Lookups would race
	// against in-flight packets (ARP, NAPI batching) and
	// give different totals even though both pins reference
	// the same kernel map. Quiescence makes the equality
	// assertion deterministic and turns it into a real
	// functional check: writes via either pin must be
	// observable through both.
	for _, id := range linkIDs {
		require.NoError(t, env.Detach(ctx, id), "detach link %d", id)
	}

	countA := readStatsMap(t, filepath.Join(progA.mapPinPath, "tc_stats_map"))
	countB := readStatsMap(t, filepath.Join(progB.mapPinPath, "tc_stats_map"))

	t.Logf("program A packets=%d, program B packets=%d", countA, countB)

	assert.Greater(t, countA, uint64(0), "shared tc_stats_map should have counted packets")
	assert.Equal(t, countA, countB, "shared map: writes via either pin must be observable through both")

	// Verify shared pin cleanup on unload.
	sharedPinPath := env.Layout.BPFFS().SharedMapPin("tc_stats_map")

	// The shared pin should exist while both programs are loaded.
	_, err := os.Stat(sharedPinPath.String())
	require.NoError(t, err, "shared map pin should exist while programs are loaded")

	// Detach links before unloading (unload requires no active links
	// for dispatcher-backed programs, but the Unload path handles
	// link cleanup internally).
	// Unload program A. The shared pin should still exist because
	// program B still references it.
	require.NoError(t, env.Unload(ctx, progA.kernelID), "unload program A")

	_, err = os.Stat(sharedPinPath.String())
	require.NoError(t, err, "shared map pin should still exist after unloading A (B still uses it)")

	// Unload program B (last user). The shared pin should now be
	// removed.
	require.NoError(t, env.Unload(ctx, progB.kernelID), "unload program B")

	_, err = os.Stat(sharedPinPath.String())
	require.True(t, os.IsNotExist(err), "shared map pin should be removed after last user unloads: %v", err)
}

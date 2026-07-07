//go:build e2e

package e2e

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cilium/ebpf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/e2e/testnet"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/manager"
)

// TestXDP_DispatcherConfigAfterDetach verifies that filling all 10
// XDP dispatcher slots then detaching them one at a time correctly
// updates the member count at each step.
func TestXDP_DispatcherConfigAfterDetach(t *testing.T) {
	t.Parallel()
	RequireRoot(t)

	env := NewTestEnv(t)
	iface := testnet.NewTestInterface(t)
	ctx := context.Background()

	programs, err := env.LoadFile(ctx, "testdata/bpf/xdp_pass_pinned.bpf.o", []manager.ProgramSpec{
		{Type: bpfman.ProgramTypeXDP, Name: "pass"},
	}, manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, programs, 1)

	prog := programs[0]
	t.Cleanup(func() { env.Unload(context.Background(), prog.Status.Kernel.ID) })

	// Fill all 10 slots
	xdpSpec, err := bpfman.NewXDPAttachSpec(prog.Status.Kernel.ID, iface.Name, 0)
	require.NoError(t, err)

	var linkIDs []bpfman.LinkRecord
	for i := range 10 {
		link, err := env.Attach(ctx, xdpSpec)
		require.NoError(t, err, "attach %d should succeed", i)
		linkIDs = append(linkIDs, link)
	}

	// Keep the last link alive for cleanup
	t.Cleanup(func() {
		env.Detach(context.Background(), linkIDs[9].ID)
	})

	dispKey := dispatcher.Key{Type: dispatcher.DispatcherTypeXDP, Nsid: iface.Nsid, Ifindex: uint32(iface.Ifindex)}

	// Verify 10 members before detach.
	snap, err := env.GetDispatcherSnapshot(ctx, dispKey)
	require.NoError(t, err)
	require.Len(t, snap.Members, 10, "should have 10 programs before detach")

	// Detach first 9 links one at a time, verifying count decreases.
	for i := range 9 {
		err = env.Detach(ctx, linkIDs[i].ID)
		require.NoError(t, err, "detach %d should succeed", i)

		snap, err = env.GetDispatcherSnapshot(ctx, dispKey)
		require.NoError(t, err, "dispatcher should still exist after detach %d", i)
		assert.Len(t, snap.Members, 9-i, "should have %d programs after detaching %d", 9-i, i+1)
	}
}

// xdpStatsEntry matches the BPF datarec struct used by xdp_counter.
type xdpStatsEntry struct {
	RxPackets uint64
	RxBytes   uint64
}

// readXDPStatsMap loads a pinned xdp_stats_map (PerCPUArray) and
// returns the total packet count summed across all CPUs. The
// xdp_counter program records stats at key 2 (XDP_PASS).
func readXDPStatsMap(t *testing.T, mapPinPath string) uint64 {
	t.Helper()

	m, err := ebpf.LoadPinnedMap(mapPinPath, nil)
	require.NoError(t, err, "load pinned xdp_stats_map at %s", mapPinPath)
	defer m.Close()

	const xdpPassKey = uint32(2) // XDP_PASS
	var perCPU []xdpStatsEntry
	err = m.Lookup(xdpPassKey, &perCPU)
	require.NoError(t, err, "lookup key %d in xdp_stats_map", xdpPassKey)

	var total uint64
	for _, entry := range perCPU {
		total += entry.RxPackets
	}
	return total
}

// TestXDP_DispatcherChainExecution verifies that all programs in an
// XDP dispatch chain actually execute when real traffic flows through
// the interface. Multiple xdp_counter programs are loaded and
// attached; after sending traffic through a veth pair, each program's
// independent xdp_stats_map must show a non-zero packet count at the
// XDP_PASS key. The test is parameterised across chain lengths to
// exercise single-program, small-chain, and full-chain scenarios.
func TestXDP_DispatcherChainExecution(t *testing.T) {
	t.Parallel()
	RequireRoot(t)

	tests := []struct {
		name string
		n    int
	}{
		{"single program", 1},
		{"3 programs", 3},
		{"5 programs", 5},
		{"10 programs (full)", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := NewTestEnv(t)
			veth := testnet.NewTestVethPair(t)
			ctx := context.Background()

			objFile := "testdata/bpf/xdp_counter.bpf.o"

			type loadedProg struct {
				kernelID   kernel.ProgramID
				mapPinPath string
			}

			var progs []loadedProg
			for i := 0; i < tt.n; i++ {
				programs, err := env.LoadFile(ctx, objFile, []manager.ProgramSpec{
					{Type: bpfman.ProgramTypeXDP, Name: "xdp_stats"},
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

			// Attach each program. XDP has no user-facing priority
			// or proceed-on control; the dispatcher always uses
			// proceed-on XDP_PASS so the chain continues through
			// all programs.
			var linkIDs []bpfman.LinkRecord
			for i := range progs {
				xdpSpec, err := bpfman.NewXDPAttachSpec(
					progs[i].kernelID, veth.A.Name, 0,
				)
				require.NoError(t, err)
				link, err := env.Attach(ctx, xdpSpec)
				require.NoError(t, err, "attach %d", i)
				linkIDs = append(linkIDs, link)
			}

			t.Cleanup(func() {
				for _, link := range linkIDs {
					env.Detach(context.Background(), link.ID)
				}
			})

			// Send traffic through the veth pair.
			veth.Ping(t, 20)

			// Read each program's xdp_stats_map and verify
			// non-zero counts.
			for i, prog := range progs {
				statsPath := filepath.Join(prog.mapPinPath, "xdp_stats_map")
				packets := readXDPStatsMap(t, statsPath)
				t.Logf("program %d (kernel_id=%d): %d packets", i, prog.kernelID, packets)
				assert.Greater(t, packets, uint64(0), "program %d should have counted packets", i)
			}
		})
	}
}

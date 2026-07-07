//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/cilium/ebpf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/e2e/testnet"
	"github.com/bpfman/bpfman/manager"
)

// listIngressBPFFilters returns the cls_bpf filters at the given
// priority on the interface's ingress parent.
func listIngressBPFFilters(t *testing.T, ifindex int, priority uint16) []netlink.BpfFilter {
	t.Helper()
	link, err := netlink.LinkByIndex(ifindex)
	require.NoError(t, err)
	filters, err := netlink.FilterList(link, netlink.HANDLE_MIN_INGRESS)
	require.NoError(t, err)
	var out []netlink.BpfFilter
	for _, f := range filters {
		bpf, ok := f.(*netlink.BpfFilter)
		if !ok || bpf.Priority != priority {
			continue
		}
		out = append(out, *bpf)
	}
	return out
}

// hasFilterWithProgID reports whether any filter points at the given
// kernel program id.
func hasFilterWithProgID(filters []netlink.BpfFilter, progID int) bool {
	for _, f := range filters {
		if f.Id == progID {
			return true
		}
	}
	return false
}

// hasFilterNamed reports whether any filter carries the given name.
func hasFilterNamed(filters []netlink.BpfFilter, name string) bool {
	for _, f := range filters {
		if f.Name == name {
			return true
		}
	}
	return false
}

// plantForeignTCFilter installs a cls_bpf filter at the given ingress
// priority pointing at the named program from tc_pass.bpf.o, modelling
// a filter another tool put on the same parent/priority. It returns
// the filter's kernel program id; the collection is closed and the
// filter best-effort removed at test end.
func plantForeignTCFilter(t *testing.T, ifindex int, priority uint16, progName string) int {
	t.Helper()
	obj, err := os.ReadFile(BytecodePath("testdata/bpf/tc_pass.bpf.o"))
	require.NoError(t, err)
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(obj))
	require.NoError(t, err)
	coll, err := ebpf.NewCollection(spec)
	require.NoError(t, err)
	t.Cleanup(coll.Close)
	prog := coll.Programs[progName]
	require.NotNil(t, prog, "program %q in tc_pass.bpf.o", progName)
	info, err := prog.Info()
	require.NoError(t, err)
	id, ok := info.ID()
	require.True(t, ok)
	filter := &netlink.BpfFilter{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: ifindex,
			Parent:    netlink.HANDLE_MIN_INGRESS,
			Priority:  priority,
			Protocol:  unix.ETH_P_ALL,
		},
		Fd:           prog.FD(),
		Name:         "foreign_" + progName,
		DirectAction: true,
	}
	require.NoError(t, netlink.FilterAdd(filter))
	t.Cleanup(func() { _ = netlink.FilterDel(filter) })
	return int(id)
}

// TestTC_DetachDeletesOwnFilterNotForeign proves that detaching a
// bpfman TC dispatcher removes bpfman's own cls_bpf filter and leaves
// an unrelated filter that happens to share the dispatcher priority
// untouched. The dispatcher filter always lands at priority 50, so a
// foreign cls_bpf filter at priority 50 on the same parent collides;
// identifying the filter to delete by priority alone (rather than the
// exact kernel handle bpfman created) deletes the foreign filter and
// orphans bpfman's own. Rust stores and deletes by the exact handle;
// this is the parity the test pins.
func TestTC_DetachDeletesOwnFilterNotForeign(t *testing.T) {
	t.Parallel()
	RequireRoot(t)
	RequireTC(t)

	env := NewTestEnv(t)
	veth := testnet.NewTestVethPair(t)
	ctx := context.Background()

	// Load and attach the bpfman TC dispatcher (its filter lands at
	// priority 50).
	programs, err := env.LoadFile(ctx, "testdata/bpf/tc_counter.bpf.o", []manager.ProgramSpec{
		{Type: bpfman.ProgramTypeTC, Name: "stats"},
	}, manager.LoadOpts{})
	require.NoError(t, err)
	require.Len(t, programs, 1)
	dispProgID := programs[0].Status.Kernel.ID
	t.Cleanup(func() { env.Unload(context.Background(), dispProgID) })

	spec, err := bpfman.NewTCAttachSpec(dispProgID, veth.A.Name, bpfman.TCDirectionIngress, 100)
	require.NoError(t, err)
	link, err := env.Attach(ctx, spec)
	require.NoError(t, err)

	// Plant two foreign cls_bpf filters at the dispatcher priority,
	// each pointing at an unrelated program. Three coats now share
	// priority 50 (bpfman's dispatcher plus two foreigns), so a
	// correct detach must target bpfman's exact handle, not merely
	// "the filter that is not mine". Added after bpfman's filter so
	// they are what a priority-only lookup returns first.
	var foreignIDs []int
	for _, progName := range []string{"alpha", "beta"} {
		foreignIDs = append(foreignIDs, plantForeignTCFilter(t, veth.A.Ifindex, 50, progName))
	}

	// Sanity: bpfman's dispatcher filter and both foreign filters are
	// present at priority 50. The cls_bpf filter points at the
	// generated tc_dispatcher program (the loaded program is a
	// freplace extension in a slot, not the filter target).
	before := listIngressBPFFilters(t, veth.A.Ifindex, 50)
	for _, id := range foreignIDs {
		require.True(t, hasFilterWithProgID(before, id), "foreign filter (prog %d) should be present before detach", id)
	}
	require.True(t, hasFilterNamed(before, "tc_dispatcher"), "dispatcher filter should be present before detach")

	// Detach the bpfman dispatcher. Only bpfman's own filter must go;
	// both foreign filters must remain.
	require.NoError(t, env.Detach(ctx, link.ID))

	after := listIngressBPFFilters(t, veth.A.Ifindex, 50)
	for _, id := range foreignIDs {
		assert.True(t, hasFilterWithProgID(after, id), "foreign filter (prog %d) must survive bpfman detach (deleting it by priority-only lookup is the bug)", id)
	}
	assert.False(t, hasFilterNamed(after, "tc_dispatcher"), "bpfman's own dispatcher filter must be removed (leaving it orphaned is the bug)")
}

// TestTC_RebuildSwapPreservesForeignFilter exercises the second site
// where filter removal must use the exact handle, not a priority-only
// lookup: the dispatcher
// rebuild a second attach triggers (the filter swap). The swap must
// remove only bpfman's old filter, by its exact handle, leaving a
// foreign filter at the same priority untouched. Distinct from the
// last-member detach path in TestTC_DetachDeletesOwnFilterNotForeign.
func TestTC_RebuildSwapPreservesForeignFilter(t *testing.T) {
	t.Parallel()
	RequireRoot(t)
	RequireTC(t)

	env := NewTestEnv(t)
	veth := testnet.NewTestVethPair(t)
	ctx := context.Background()

	attachNew := func(priority int) {
		programs, err := env.LoadFile(ctx, "testdata/bpf/tc_counter.bpf.o", []manager.ProgramSpec{
			{Type: bpfman.ProgramTypeTC, Name: "stats"},
		}, manager.LoadOpts{})
		require.NoError(t, err)
		require.Len(t, programs, 1)
		id := programs[0].Status.Kernel.ID
		t.Cleanup(func() { env.Unload(context.Background(), id) })

		spec, err := bpfman.NewTCAttachSpec(id, veth.A.Name, bpfman.TCDirectionIngress, priority)
		require.NoError(t, err)
		link, err := env.Attach(ctx, spec)
		require.NoError(t, err)
		t.Cleanup(func() { env.Detach(context.Background(), link.ID) })
	}

	// First attach installs bpfman's filter at priority 50.
	attachNew(100)

	// A foreign filter joins at the same priority, after bpfman's.
	foreignID := plantForeignTCFilter(t, veth.A.Ifindex, 50, "alpha")

	// Second attach triggers a dispatcher rebuild: a new filter is
	// created and bpfman's old filter removed by its exact handle.
	attachNew(200)

	filters := listIngressBPFFilters(t, veth.A.Ifindex, 50)
	assert.True(t, hasFilterWithProgID(filters, foreignID), "foreign filter must survive the dispatcher rebuild swap")
	assert.True(t, hasFilterNamed(filters, "tc_dispatcher"), "bpfman's dispatcher filter must remain after the swap")
}

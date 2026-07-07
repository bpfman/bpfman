package builtins

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// neverExists is the default existence-check stub: nothing the
// previous tenant left behind is visible in the kernel. Used by
// tests that exercise the acquire/release ordering rather than
// the leak path itself.
func neverExists(string) bool { return false }

// seedPoolForSlotOne writes bodies for every slot so the FIFO
// scan has a definite ordering. Slot 1 gets seedSlot1's body
// (typically representing a leaked previous tenant);
// slots 2..poolSize get a "just released" body so they sort
// strictly after slot 1. Without this scaffold the unused-slot
// zero-time tier wins first and the leak-detection path never
// runs.
func seedPoolForSlotOne(t *testing.T, root string, seedSlot1 provenance) {
	t.Helper()
	rawLeak, err := json.Marshal(seedSlot1)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(slotLockPath(root, 1), rawLeak, 0o600))
	fresh := provenance{ReleasedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	rawFresh, err := json.Marshal(fresh)
	require.NoError(t, err)
	for slot := uint32(2); slot <= poolSize; slot++ {
		require.NoError(t, os.WriteFile(slotLockPath(root, slot), rawFresh, 0o600))
	}
}

// poolReq builds a poolAcquireRequest pre-populated with the
// per-test root and the neverExists stubs. Tests that want a
// non-default existence behaviour override the field in the
// returned struct.
func poolReq(root, origin, ns, link string) poolAcquireRequest {
	return poolAcquireRequest{
		root:        root,
		origin:      origin,
		nsName:      ns,
		linkAName:   link,
		linkExists:  neverExists,
		netnsExists: neverExists,
	}
}

func TestSlotAddrs_Boundaries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		slot       uint32
		host, peer string
	}{
		{1, "198.51.100.1", "198.51.100.2"},
		{2, "198.51.100.5", "198.51.100.6"},
		{3, "198.51.100.9", "198.51.100.10"},
		{64, "198.51.100.253", "198.51.100.254"},
	}
	for _, c := range cases {
		h, p := slotAddrs(c.slot)
		assert.Equalf(t, c.host, h, "slot %d host", c.slot)
		assert.Equalf(t, c.peer, p, "slot %d peer", c.slot)
	}
}

// TestAcquirePoolSlot_AssignsSlotOneFirst confirms the empty-pool
// case: with no lockfiles in place, slot 1 wins because its zero-
// time sort key ties at the front and sort.SliceStable preserves
// scan order.
func TestAcquirePoolSlot_AssignsSlotOneFirst(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	lease, err := acquirePoolSlot(poolReq(root, "test.bpfman:1", "ns0", "vea0"))
	require.NoError(t, err)
	defer releasePoolSlot(lease, "ns0", "", "vea0")
	assert.Equal(t, uint32(1), lease.slot)
	assert.Equal(t, "198.51.100.1/30", lease.hostCIDR)
	assert.Equal(t, "198.51.100.2/30", lease.peerCIDR)
	assert.Equal(t, "198.51.100.1", lease.hostAddr)
	assert.Equal(t, "198.51.100.2", lease.peerAddr)
}

// TestAcquirePoolSlot_DistinctSlotsWhileHeld confirms that
// holding a slot prevents the next acquire from taking it.
// Sequence: acquire slot 1, hold it, acquire again -- second
// call must get slot 2.
func TestAcquirePoolSlot_DistinctSlotsWhileHeld(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	first, err := acquirePoolSlot(poolReq(root, "a", "ns-a", "vea-a"))
	require.NoError(t, err)
	defer releasePoolSlot(first, "ns-a", "", "vea-a")

	second, err := acquirePoolSlot(poolReq(root, "b", "ns-b", "vea-b"))
	require.NoError(t, err)
	defer releasePoolSlot(second, "ns-b", "", "vea-b")

	assert.Equal(t, uint32(1), first.slot)
	assert.Equal(t, uint32(2), second.slot)
}

// TestAcquirePoolSlot_FIFOOnRelease is the cooldown invariant:
// after acquiring slots 1 and 2 and then releasing 1, the next
// acquire should NOT pick 1 (its released_at is more recent than
// the zero sort key on unused slots 3..64). The next free slot
// is 3.
func TestAcquirePoolSlot_FIFOOnRelease(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	a, err := acquirePoolSlot(poolReq(root, "a", "ns-a", "vea-a"))
	require.NoError(t, err)

	b, err := acquirePoolSlot(poolReq(root, "b", "ns-b", "vea-b"))
	require.NoError(t, err)
	defer releasePoolSlot(b, "ns-b", "", "vea-b")

	require.NoError(t, releasePoolSlot(a, "ns-a", "", "vea-a"))

	c, err := acquirePoolSlot(poolReq(root, "c", "ns-c", "vea-c"))
	require.NoError(t, err)
	defer releasePoolSlot(c, "ns-c", "", "vea-c")

	assert.Equal(t, uint32(1), a.slot)
	assert.Equal(t, uint32(2), b.slot)
	assert.Equalf(t, uint32(3), c.slot, "released slot 1 should sort after the unused slots; got slot %d", c.slot)
}

// TestAcquirePoolSlot_PreferReleasedOverHeld covers the case
// where every slot has a lockfile body: the released ones
// (sorted by released_at) should be picked before any unused or
// held slot. We acquire the full pool so there are no unused
// slots to win the FIFO race, release two slots with a known
// ordering, and check the next acquire picks the longest-
// released one.
func TestAcquirePoolSlot_PreferReleasedOverHeld(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	held := make([]*poolLease, poolSize)
	defer func() {
		for _, l := range held {
			if l != nil {
				_ = releasePoolSlot(l, "", "", "")
			}
		}
	}()
	for i := range uint32(poolSize) {
		l, err := acquirePoolSlot(poolReq(root, fmt.Sprintf("init-%d", i), "", ""))
		require.NoError(t, err)
		held[i] = l
	}
	// held[i] holds slot i+1 since acquires are assigned in
	// scan order from an empty pool. Release slot 3 first, then
	// slot 2; slot 3 ends up with the older released_at.
	require.NoError(t, releasePoolSlot(held[2], "", "", ""))
	held[2] = nil
	time.Sleep(2 * time.Millisecond)
	require.NoError(t, releasePoolSlot(held[1], "", "", ""))
	held[1] = nil

	next, err := acquirePoolSlot(poolReq(root, "reuse", "", ""))
	require.NoError(t, err)
	defer releasePoolSlot(next, "", "", "")
	assert.Equalf(t, uint32(3), next.slot, "expected the longest-released slot (3); got %d", next.slot)
}

// TestAcquirePoolSlot_Exhaustion acquires all slots and confirms
// the next attempt returns the typed exhaustion error once the
// caller's wait budget expires rather than retrying forever or
// returning a stale slot.
func TestAcquirePoolSlot_Exhaustion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	held := make([]*poolLease, 0, poolSize)
	defer func() {
		for _, l := range held {
			_ = releasePoolSlot(l, "", "", "")
		}
	}()
	for i := uint32(1); i <= poolSize; i++ {
		l, err := acquirePoolSlot(poolReq(root, fmt.Sprintf("slot%d", i), "", ""))
		require.NoError(t, err)
		held = append(held, l)
	}

	req := poolReq(root, "overflow", "", "")
	req.waitTimeout = time.Millisecond
	_, err := acquirePoolSlot(req)
	require.Error(t, err)
	assert.ErrorIs(t, err, errNetPoolExhausted)
}

// TestAcquirePoolSlot_ReleasedSlotIgnoresExtantLink confirms that
// a slot whose previous tenant wrote released_at is trusted to
// have cleaned up, even when an extant link of the same name still
// exists in the kernel.
// That extant link is presumed to belong to a later unrelated
// process (typically another concurrent run of a script with
// deterministic names) and must not be flagged as a leak this
// acquirer attributes. Without this carve-out, parallel runs of
// the same fixture script reliably produce false positives.
func TestAcquirePoolSlot_ReleasedSlotIgnoresExtantLink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	seedPoolForSlotOne(t, root, provenance{
		Origin:     "previous_test.bpfman:42",
		NsName:     "ns-leaker",
		LinkAName:  "vea-leaker",
		AcquiredAt: "2020-01-01T00:00:00Z",
		ReleasedAt: "2020-01-01T00:00:01Z",
	})

	req := poolReq(root, "next_test.bpfman:1", "ns-next", "vea-next")
	req.linkExists = func(name string) bool { return name == "vea-leaker" }
	lease, err := acquirePoolSlot(req)
	require.NoError(t, err, "released_at should suppress the existence check")
	defer releasePoolSlot(lease, "ns-next", "", "vea-next")
	assert.Equal(t, uint32(1), lease.slot)
}

// TestAcquirePoolSlot_ReleasedSlotIgnoresExtantNetns is the
// netns sibling of the above. Same reasoning: a released slot
// trusts its released_at promise; any extant netns of the
// previous name is attributed to an unrelated concurrent
// process.
func TestAcquirePoolSlot_ReleasedSlotIgnoresExtantNetns(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	seedPoolForSlotOne(t, root, provenance{
		Origin:     "previous_test.bpfman:7",
		NsName:     "ns-leaker",
		LinkAName:  "vea-leaker",
		AcquiredAt: "2020-01-01T00:00:00Z",
		ReleasedAt: "2020-01-01T00:00:01Z",
	})

	req := poolReq(root, "next_test.bpfman:1", "ns-next", "vea-next")
	req.netnsExists = func(name string) bool { return name == "ns-leaker" }
	lease, err := acquirePoolSlot(req)
	require.NoError(t, err, "released_at should suppress the existence check")
	defer releasePoolSlot(lease, "ns-next", "", "vea-next")
	assert.Equal(t, uint32(1), lease.slot)
}

// TestAcquirePoolSlot_CrashLeakedLinkAttributed exercises the
// remaining attribution path: the previous tenant crashed
// before writing released_at and left its link behind. This
// is the scenario assertSlotClean is now scoped to, and the
// failure must name the previous origin and the lingering link.
func TestAcquirePoolSlot_CrashLeakedLinkAttributed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Slot 1 has an acquire-time body with no released_at; mtime
	// is forced into the past so it sorts ahead of the freshly-
	// seeded slots 2..poolSize.
	body := provenance{
		Origin:     "crashed_test.bpfman:13",
		NsName:     "ns-leaker",
		LinkAName:  "vea-leaker",
		AcquiredAt: "2020-01-01T00:00:00Z",
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	slot1 := slotLockPath(root, 1)
	require.NoError(t, os.WriteFile(slot1, raw, 0o600))
	pastTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(slot1, pastTime, pastTime))
	fresh := provenance{ReleasedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	rawFresh, err := json.Marshal(fresh)
	require.NoError(t, err)
	for slot := uint32(2); slot <= poolSize; slot++ {
		require.NoError(t, os.WriteFile(slotLockPath(root, slot), rawFresh, 0o600))
	}

	req := poolReq(root, "next_test.bpfman:1", "ns-next", "vea-next")
	req.linkExists = func(name string) bool { return name == "vea-leaker" }
	_, err = acquirePoolSlot(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slot 1")
	assert.Contains(t, err.Error(), `link "vea-leaker"`)
	assert.Contains(t, err.Error(), "crashed_test.bpfman:13")
	assert.Contains(t, err.Error(), "never released")
}

// TestAcquirePoolSlot_StaleLeak_NeverReleased is the crash-
// before-release case: the previous tenant wrote an acquire-
// time body but no released_at. The slot must still get picked
// (via mtime fallback ordering, oldest first) and attributed
// with "never released" wording.
func TestAcquirePoolSlot_StaleLeak_NeverReleased(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Slot 1 with no released_at; the scan falls back to
	// mtime, which we then make oldest by touching the file
	// in the past. Slots 2..poolSize get a freshly-released
	// body so they sort strictly after slot 1.
	body := provenance{
		Origin:     "crashed_test.bpfman:99",
		NsName:     "ns-crasher",
		LinkAName:  "vea-crasher",
		AcquiredAt: "2020-01-01T00:00:00Z",
	}
	raw, _ := json.Marshal(body)
	slot1 := slotLockPath(root, 1)
	require.NoError(t, os.WriteFile(slot1, raw, 0o600))
	pastTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(slot1, pastTime, pastTime))
	fresh := provenance{ReleasedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	rawFresh, err := json.Marshal(fresh)
	require.NoError(t, err)
	for slot := uint32(2); slot <= poolSize; slot++ {
		require.NoError(t, os.WriteFile(slotLockPath(root, slot), rawFresh, 0o600))
	}

	req := poolReq(root, "next_test.bpfman:1", "ns-next", "vea-next")
	req.linkExists = func(name string) bool { return name == "vea-crasher" }
	_, err = acquirePoolSlot(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "never released")
	assert.Contains(t, err.Error(), "crashed_test.bpfman:99")
}

// TestReleasePoolSlot_WritesReleasedAt confirms the on-disk
// invariant: after a clean release the lockfile body carries a
// released_at the next acquirer can sort on. Without this the
// FIFO ordering degenerates to mtime, which is the fallback.
func TestReleasePoolSlot_WritesReleasedAt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	lease, err := acquirePoolSlot(poolReq(root, "release_test.bpfman:5", "ns-rel", "vea-rel"))
	require.NoError(t, err)
	require.NoError(t, releasePoolSlot(lease, "ns-rel", "", "vea-rel"))

	body, err := os.ReadFile(slotLockPath(root, lease.slot))
	require.NoError(t, err)
	var p provenance
	require.NoError(t, json.Unmarshal(body, &p))
	assert.Equal(t, "release_test.bpfman:5", p.Origin)
	assert.Equal(t, "ns-rel", p.NsName)
	assert.Equal(t, "vea-rel", p.LinkAName)
	assert.NotEmpty(t, p.AcquiredAt, "acquired_at should survive into the released body for post-mortem")
	assert.NotEmpty(t, p.ReleasedAt, "released_at is the FIFO sort key and must be present after clean release")
}

// TestReleasePoolSlot_NilOrZeroIsNoOp guards the explicit-mode
// path that does not lease a slot: calling release on a nil
// lease or one with Slot == 0 must not error and must not touch
// the filesystem.
func TestReleasePoolSlot_NilOrZeroIsNoOp(t *testing.T) {
	t.Parallel()
	require.NoError(t, releasePoolSlot(nil, "", "", ""))
	require.NoError(t, releasePoolSlot(&poolLease{}, "", "", ""))
}

// TestAcquirePoolSlot_ConcurrentDistinctSlots is the in-process
// concurrency invariant: N goroutines racing to AcquirePoolSlot
// must end up holding N distinct slots. Linux flock(2) is per
// open-file-description, so a same-process double-open does
// exclude correctly; this test guards against future regressions
// (e.g. if the implementation switched to a shared fd).
func TestAcquirePoolSlot_ConcurrentDistinctSlots(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	const n = 16
	results := make([]*poolLease, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = acquirePoolSlot(poolReq(root, fmt.Sprintf("goroutine-%d", i), "", ""))
		}(i)
	}
	wg.Wait()
	defer func() {
		for _, l := range results {
			if l != nil {
				_ = releasePoolSlot(l, "", "", "")
			}
		}
	}()

	slots := make([]int, 0, n)
	for i, err := range errs {
		require.NoErrorf(t, err, "goroutine %d", i)
		slots = append(slots, int(results[i].slot))
	}
	sort.Ints(slots)
	for i := range n - 1 {
		assert.NotEqualf(t, slots[i], slots[i+1], "two goroutines got the same slot %d", slots[i])
	}
}

// TestAcquirePoolSlot_RecoversFromUnparseableBody guards the
// legacy-body fallback: if a lockfile contains garbage, the
// acquire must not panic and must treat the slot as available
// (mtime-based ordering, no leak attribution against junk
// fields).
func TestAcquirePoolSlot_RecoversFromUnparseableBody(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	require.NoError(t, os.WriteFile(slotLockPath(root, 1), []byte("not json {{{"), 0o600))

	lease, err := acquirePoolSlot(poolReq(root, "ok", "", ""))
	require.NoError(t, err)
	defer releasePoolSlot(lease, "", "", "")
	// Slot 1's mtime is "now" so it sorts after the unused
	// slots 2..64; we expect slot 2.
	assert.Equalf(t, uint32(2), lease.slot, "garbage body should fall back to mtime ordering, putting slot 1 last; got %d", lease.slot)
}

// TestAcquirePoolSlot_MkdirFailure surfaces a permissions-style
// error at the pool root level rather than blowing up later on
// the first OpenFile. We point Root at a path under a non-writable
// parent and expect a clear mkdir error.
func TestAcquirePoolSlot_MkdirFailure(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions do not apply")
	}
	parent := t.TempDir()
	require.NoError(t, os.Chmod(parent, 0o500))
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	_, err := acquirePoolSlot(poolReq(parent+"/forbidden/pool", "x", "", ""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir")
}

// TestProvenance_SortKeyParseFailureFallsBackToModTime guards
// the case where released_at is present but unparseable (truncated
// write, future schema): the slot must still rank by mtime rather
// than landing at zero-time (which would make it win first and
// defeat the cooldown).
func TestProvenance_SortKeyParseFailureFallsBackToModTime(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	body := []byte(`{"origin":"x","released_at":"not-a-timestamp"}`)
	require.NoError(t, os.WriteFile(slotLockPath(root, 1), body, 0o600))
	cands, err := scanSlots(root)
	require.NoError(t, err)
	var got slotCandidate
	for _, c := range cands {
		if c.slot == 1 {
			got = c
			break
		}
	}
	assert.Falsef(t, got.sortKey.IsZero(), "slot 1 should fall back to mtime, not zero time")
}

// TestLeakError_FormatVariants covers the formatLeak wording
// matrix: released vs never-released vs unknown-origin.
func TestLeakError_FormatVariants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		prev        provenance
		kind, what  string
		mustContain []string
	}{
		{
			name: "released",
			prev: provenance{Origin: "a.bpfman:1", ReleasedAt: "2026-05-11T00:00:00Z"},
			kind: "link", what: "vea0",
			mustContain: []string{"a.bpfman:1", "released 2026-05-11T00:00:00Z", `link "vea0"`},
		},
		{
			name: "never_released_with_acquire",
			prev: provenance{Origin: "a.bpfman:1", AcquiredAt: "2026-05-11T00:00:00Z"},
			kind: "netns", what: "ns0",
			mustContain: []string{"a.bpfman:1", "never released", "acquired 2026-05-11T00:00:00Z", `netns "ns0"`},
		},
		{
			name: "unknown_origin",
			prev: provenance{},
			kind: "link", what: "vea0",
			mustContain: []string{"<unknown caller>", `link "vea0"`, "never released"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := leakError(7, c.prev, c.kind, c.what)
			require.Error(t, err)
			msg := err.Error()
			for _, s := range c.mustContain {
				assert.Containsf(t, msg, s, "missing %q in %q", s, msg)
			}
			assert.Contains(t, msg, "slot 7")
		})
	}
}

// TestProvenance_JSONRoundTrip is a sanity check on the on-disk
// schema: provenance round-trips through json.Marshal/Unmarshal
// so a malformed edit by a future change to the struct shape
// surfaces here, not at runtime under load.
func TestProvenance_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	in := provenance{
		Origin:     "rt.bpfman:1",
		NsName:     "ns",
		LinkAName:  "vea",
		AcquiredAt: "2026-05-11T00:00:00Z",
		ReleasedAt: "2026-05-11T00:00:01Z",
	}
	body, err := json.Marshal(in)
	require.NoError(t, err)
	var out provenance
	require.NoError(t, json.Unmarshal(body, &out))
	assert.Equal(t, in, out)
}

// TestPoolAcquireRequest_DefaultsApplyToEmptyFields confirms the
// fallback chain: Root="" -> defaultPoolRoot, linkExists=nil ->
// defaultLinkCheck, netnsExists=nil -> defaultNetnsCheck. We
// exercise the linkExists/netnsExists defaults (Root would write
// to /run/bpfman-net-pool so we skip that branch) by seeding a
// slot with a real-but-not-present link name and a real-but-not-
// present netns name and confirming the default checkers report
// "absent". A passing test means the defaults were wired in.
func TestPoolAcquireRequest_DefaultsApplyToEmptyFields(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Slot 1 references plausible-but-nonexistent kernel
	// resources. Default checkers must answer false; assertSlotClean
	// must return nil; the acquire must succeed.
	seedPoolForSlotOne(t, root, provenance{
		Origin:     "previous.bpfman:1",
		NsName:     "ns-nope-xxxxx",
		LinkAName:  "vea-nope-xxxxx",
		AcquiredAt: "2020-01-01T00:00:00Z",
		ReleasedAt: "2020-01-01T00:00:01Z",
	})
	// Request with Root populated but linkExists / netnsExists
	// nil: the defaults should run.
	lease, err := acquirePoolSlot(poolAcquireRequest{
		root:      root,
		origin:    "next.bpfman:1",
		nsName:    "ns-next",
		linkAName: "vea-next",
	})
	require.NoError(t, err, "default checkers should report absent for nonexistent kernel resources")
	defer releasePoolSlot(lease, "ns-next", "", "vea-next")
	assert.Equal(t, uint32(1), lease.slot)
}

// TestAcquirePoolSlot_CrashLeakedSecondNetnsAttributed covers the
// isolated builder's second tenant: a crash-leaked slot whose
// ns_b_name still exists in the kernel must fail the acquire with
// the leak attributed, exactly as ns_name does.
func TestAcquirePoolSlot_CrashLeakedSecondNetnsAttributed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	body := provenance{
		Origin:     "crashed_test.bpfman:7",
		NsName:     "B0000000000a1Na",
		NsBName:    "B0000000000a1Nb",
		LinkAName:  "B0000000000a1Na",
		AcquiredAt: "2020-01-01T00:00:00Z",
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	slot1 := slotLockPath(root, 1)
	require.NoError(t, os.WriteFile(slot1, raw, 0o600))
	pastTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(slot1, pastTime, pastTime))
	fresh := provenance{ReleasedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	rawFresh, err := json.Marshal(fresh)
	require.NoError(t, err)
	for slot := uint32(2); slot <= poolSize; slot++ {
		require.NoError(t, os.WriteFile(slotLockPath(root, slot), rawFresh, 0o600))
	}

	req := poolReq(root, "next_test.bpfman:1", "ns-next", "vea-next")
	req.netnsExists = func(name string) bool { return name == "B0000000000a1Nb" }
	_, err = acquirePoolSlot(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slot 1")
	assert.Contains(t, err.Error(), `netns "B0000000000a1Nb"`)
	assert.Contains(t, err.Error(), "crashed_test.bpfman:7")
	assert.Contains(t, err.Error(), "never released")
}

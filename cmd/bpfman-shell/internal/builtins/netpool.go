// netpool is the cross-process slot allocator backing
// `net veth-pair`'s auto-address mode. Each running bpfman-shell
// process competes for one of 64 /30 subnets carved out of
// 198.51.100.0/24 (TEST-NET-2); exclusion is via flock on a
// per-slot lockfile under /run/bpfman-net-pool/. The kernel
// releases the flock when the holder exits, so the pool is
// self-cleaning against crashes without a daemon.
package builtins

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

// poolSize is the number of /30 subnets the pool carves out of
// 198.51.100.0/24. Sized well above realistic parallelism for
// the dispatcher corpus; widening to a second /24 doubles the
// address management cost for no near-term gain.
const poolSize = 64

// defaultPoolAcquireTimeout bounds how long an auto-address
// `net veth-pair` waits for another process to release a pool
// slot. Stress runs can legitimately dispatch more net-using
// scripts than the pool has slots; that should back-pressure the
// caller rather than fail a script that would have run correctly
// once a lease became available.
const defaultPoolAcquireTimeout = 5 * time.Minute

const poolAcquirePollInterval = 25 * time.Millisecond

// defaultPoolRoot is the production location of the pool, sibling
// of `/run/bpfman`. Callers that pass an empty Root in
// poolAcquireRequest get this; tests pass a t.TempDir() so the
// production path is never written to by accident and so tests
// can run in parallel.
const defaultPoolRoot = "/run/bpfman-net-pool"

// poolSubnetPrefix is the network prefix of the TEST-NET-2 /24 the pool
// allocates inside. Slot n occupies n's /30 at base 4*(n-1):
// host = base+1, peer = base+2, broadcast = base+3.
const poolSubnetPrefix = "198.51.100."

// linkExistsFn / netnsExistsFn are the assertSlotClean
// primitives. Callers normally leave them nil so the defaults
// (netlink.LinkByName / netns.GetFromName) run; tests pass stubs
// to model a leaked tenant without needing real kernel state.
//
// The defaults use vishvananda/netlink and vishvananda/netns
// (both already direct module deps): netlink.LinkByName issues a
// single RTM_GETLINK with NLM_F_REQUEST (targeted lookup, no
// dump, so NLM_F_DUMP_INTR cannot strike); netns.GetFromName
// opens the named netns file under /var/run/netns/. Both
// definitively distinguish presence from absence without parsing
// iproute2 output.
type linkExistsFn func(name string) bool
type netnsExistsFn func(name string) bool

// poolLease is the handle returned by acquirePoolSlot. The flock
// is held until releasePoolSlot closes the lockfile, so callers
// must arrange a paired release on every code path that consumes
// a lease.
//
// slot, hostCIDR, peerCIDR, hostAddr, and peerAddr are carried
// only inside the net builtin. The user-visible NetPair handle
// deliberately does not retain the lease.
type poolLease struct {
	// slot is the 1-indexed slot number. The lockfile path is
	// derived from this in slotLockPath.
	slot uint32

	// hostCIDR and peerCIDR are the /30 prefix forms to pass to
	// `ip addr add`. The kernel installs the connected route
	// automatically; the pool does not manage explicit routes,
	// though pair creation does ensure the harness-wide TEST-NET-2
	// policy rule (see internal/testnetroute) so host policy
	// routing cannot hijack the reply path.
	hostCIDR string
	peerCIDR string

	// hostAddr and peerAddr are the bare host-side and peer-side
	// addresses (no /CIDR suffix), suitable for handing to ping
	// or for exposing on $pair.host_addr / $pair.peer_addr.
	hostAddr string
	peerAddr string

	lockFile *os.File
	origin   string
}

// poolAcquireRequest is the call-site context recorded in the
// slot's provenance body, plus optional overrides for the pool
// root and the two cleanliness-check functions. Production
// callers leave root, linkExists, and netnsExists at their
// zero values; tests pass a t.TempDir() and stubs so they can
// run in parallel without sharing package-level state.
type poolAcquireRequest struct {
	// root is the on-disk pool directory. Empty defaults to
	// defaultPoolRoot.
	root string

	// origin is a free-form attribution string, typically the
	// source location of the `net veth-pair` invocation
	// (file:line[:col]). Empty is tolerated but discouraged.
	origin string

	// nsName is the netns name the caller will pass to
	// `ip netns add`. Used by assertSlotClean's netns check.
	nsName string

	// nsBName is the second netns name for the isolated
	// netns-veth-pair builder; empty for the host-end builder.
	// Recorded in provenance so the next acquirer's leak check
	// validates both tenants.
	nsBName string

	// linkAName is the host-side veth name the caller will pass
	// to `ip link add`. Used by assertSlotClean's link check.
	linkAName string

	// linkExists and netnsExists override the assertSlotClean
	// primitives. Nil means "use the default kernel-truth
	// implementation". Set by tests to inject a known leak
	// without needing CAP_NET_ADMIN.
	linkExists  linkExistsFn
	netnsExists netnsExistsFn

	// waitTimeout bounds how long acquirePoolSlot waits for an
	// in-flight lease to be released when every slot is held.
	// Zero uses defaultPoolAcquireTimeout. Tests set a tiny value
	// when they need to exercise exhaustion without sleeping for
	// the production budget.
	waitTimeout time.Duration
}

var (
	netPairLeaseMu      sync.Mutex
	netPairLeases       = map[*runtime.NetPair]*poolLease{}
	netnsVethPairLeases = map[*runtime.NetnsVethPair]*poolLease{}
)

// provenance is the JSON body written to a slot's lockfile. It
// records who last held the slot and when, so the next acquirer
// can attribute a leaked resource and so FIFO ordering has an
// explicit timestamp to sort on.
type provenance struct {
	Origin string `json:"origin,omitempty"`
	NsName string `json:"ns_name,omitempty"`
	// NsBName is the second netns name when the tenant was a
	// `net netns-veth-pair` (both ends namespaced). Empty for the
	// host-end builder, which owns only one namespace.
	NsBName    string `json:"ns_b_name,omitempty"`
	LinkAName  string `json:"link_a_name,omitempty"`
	AcquiredAt string `json:"acquired_at,omitempty"`
	ReleasedAt string `json:"released_at,omitempty"`
}

// acquirePoolSlot leases a slot under flock. The algorithm:
//
//  1. Scan slots [1, poolSize], computing a sort key per slot
//     (zero time if no lockfile; ReleasedAt from the body if
//     present; mtime otherwise).
//  2. Sort ascending so unused and longest-released slots win
//     first.
//  3. Walk the sorted candidates trying flock(LOCK_EX|LOCK_NB)
//     on each. Skip any already-held slot.
//  4. Under the lock, re-read the body (state may have advanced
//     since the scan), validate cleanliness, and write a fresh
//     acquire-time body before returning.
//
// The mkdir is best-effort: a pre-existing pool root is fine; a
// permissions failure surfaces here rather than during the first
// flock.
var errNetPoolExhausted = errors.New("net pool exhausted")

func acquirePoolSlot(req poolAcquireRequest) (*poolLease, error) {
	timeout := req.waitTimeout
	if timeout == 0 {
		timeout = defaultPoolAcquireTimeout
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	var lastErr error
	for {
		lease, err := tryAcquirePoolSlot(req)
		if err == nil {
			return lease, nil
		}
		if !errors.Is(err, errNetPoolExhausted) {
			return nil, err
		}
		lastErr = err

		select {
		case <-deadline.C:
			return nil, lastErr
		case <-time.After(poolAcquirePollInterval):
		}
	}
}

func tryAcquirePoolSlot(req poolAcquireRequest) (*poolLease, error) {
	root := req.root
	if root == "" {
		root = defaultPoolRoot
	}
	linkCheck := req.linkExists
	if linkCheck == nil {
		linkCheck = defaultLinkCheck
	}
	netnsCheck := req.netnsExists
	if netnsCheck == nil {
		netnsCheck = defaultNetnsCheck
	}

	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("net pool: mkdir %s: %w", root, err)
	}

	cands, err := scanSlots(root)
	if err != nil {
		return nil, err
	}

	slices.SortStableFunc(cands, func(a, b slotCandidate) int {
		return a.sortKey.Compare(b.sortKey)
	})

	for _, c := range cands {
		path := slotLockPath(root, c.slot)
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			return nil, fmt.Errorf("net pool: open %s: %w", path, err)
		}

		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			f.Close()
			if errors.Is(err, syscall.EWOULDBLOCK) {
				continue
			}
			return nil, fmt.Errorf("net pool: flock %s: %w", path, err)
		}

		// Re-read under the lock; the scan-time body is potentially
		// stale because another process may have released and
		// re-acquired between the scan and the flock.
		prev, err := readProvenance(path)
		if err != nil {
			f.Close()
			return nil, err
		}

		if err := assertSlotClean(c.slot, prev, linkCheck, netnsCheck); err != nil {
			f.Close()
			return nil, err
		}

		now := time.Now().UTC()
		fresh := provenance{
			Origin:     req.origin,
			NsName:     req.nsName,
			NsBName:    req.nsBName,
			LinkAName:  req.linkAName,
			AcquiredAt: now.Format(time.RFC3339Nano),
		}
		if err := writeProvenance(f, fresh); err != nil {
			f.Close()
			return nil, fmt.Errorf("net pool: write %s: %w", path, err)
		}

		host, peer := slotAddrs(c.slot)
		return &poolLease{
			slot:     c.slot,
			hostCIDR: fmt.Sprintf("%s/30", host),
			peerCIDR: fmt.Sprintf("%s/30", peer),
			hostAddr: host,
			peerAddr: peer,
			lockFile: f,
			origin:   req.origin,
		}, nil
	}
	return nil, fmt.Errorf("%w: more than %d concurrent pairs in flight", errNetPoolExhausted, poolSize)
}

// releasePoolSlot writes a final provenance body carrying
// released_at, then closes the lockfile (which releases the
// flock). The teardown order is deliberate: the body is the
// canonical "what was released" payload, and the flock release
// must happen before any subsequent operation on the local handle
// short-circuits.
//
// nsName, nsBName, and linkAName are passed in so the released
// body carries the names the next acquirer will validate against;
// they should match what the caller installed in the kernel under
// this slot. nsBName is empty for the host-end builder.
//
// A nil lease or a lease with slot == 0 is a no-op (the explicit-
// address path on `net veth-pair` does not lease a slot).
func releasePoolSlot(lease *poolLease, nsName, nsBName, linkAName string) error {
	if lease == nil || lease.slot == 0 || lease.lockFile == nil {
		return nil
	}
	final := provenance{
		Origin:     lease.origin,
		NsName:     nsName,
		NsBName:    nsBName,
		LinkAName:  linkAName,
		ReleasedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	// Preserve AcquiredAt from the on-disk body so the released
	// record carries both timestamps for post-mortem inspection.
	if prev, err := readProvenanceFromFile(lease.lockFile); err == nil {
		final.AcquiredAt = prev.AcquiredAt
	}

	if err := writeProvenance(lease.lockFile, final); err != nil {
		lease.lockFile.Close()
		lease.lockFile = nil
		return fmt.Errorf("net pool: write final provenance: %w", err)
	}

	err := lease.lockFile.Close()
	lease.lockFile = nil
	if err != nil {
		return fmt.Errorf("net pool: close slot %d lockfile: %w", lease.slot, err)
	}
	return nil
}

func rememberNetPairLease(pair *runtime.NetPair, lease *poolLease) {
	if pair == nil || lease == nil {
		return
	}
	netPairLeaseMu.Lock()
	netPairLeases[pair] = lease
	netPairLeaseMu.Unlock()
}

func takeNetPairLease(pair *runtime.NetPair) *poolLease {
	if pair == nil {
		return nil
	}
	netPairLeaseMu.Lock()
	lease := netPairLeases[pair]
	delete(netPairLeases, pair)
	netPairLeaseMu.Unlock()
	return lease
}

func rememberNetnsVethPairLease(pair *runtime.NetnsVethPair, lease *poolLease) {
	if pair == nil || lease == nil {
		return
	}
	netPairLeaseMu.Lock()
	netnsVethPairLeases[pair] = lease
	netPairLeaseMu.Unlock()
}

func takeNetnsVethPairLease(pair *runtime.NetnsVethPair) *poolLease {
	if pair == nil {
		return nil
	}
	netPairLeaseMu.Lock()
	lease := netnsVethPairLeases[pair]
	delete(netnsVethPairLeases, pair)
	netPairLeaseMu.Unlock()
	return lease
}

// defaultLinkCheck issues a single RTM_GETLINK and reports
// whether the kernel returned a link. Any error -- LinkNotFound,
// permission, transport -- degrades to "absent" so the next
// setup step surfaces the real problem rather than letting an
// unrelated environment fault masquerade as a leak.
func defaultLinkCheck(name string) bool {
	_, err := netlink.LinkByName(name)
	return err == nil
}

// defaultNetnsCheck opens /var/run/netns/NAME; the open succeeds
// iff the named netns is currently mounted. The fd is closed
// immediately because the netns is only needed for the existence
// signal.
func defaultNetnsCheck(name string) bool {
	h, err := netns.GetFromName(name)
	if err != nil {
		return false
	}
	h.Close()
	return true
}

// slotCandidate carries the sort key and previous provenance for
// a slot during the acquire scan.
type slotCandidate struct {
	slot    uint32
	sortKey time.Time
	prev    provenance
}

// scanSlots inspects every slot lockfile and returns one
// slotCandidate per slot. Missing files use the zero time (oldest
// possible, so unused slots win first). Files with a parseable
// ReleasedAt use that timestamp. Everything else falls back to
// mtime, covering legacy bodies, crash-leaked bodies, and bodies
// written by a process that never made it past AcquiredAt.
func scanSlots(root string) ([]slotCandidate, error) {
	out := make([]slotCandidate, 0, poolSize)
	for slot := uint32(1); slot <= poolSize; slot++ {
		path := slotLockPath(root, slot)
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			out = append(out, slotCandidate{slot: slot})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("net pool: stat %s: %w", path, err)
		}

		prev, _ := readProvenance(path)
		key := time.Time{}
		if prev.ReleasedAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, prev.ReleasedAt); err == nil {
				key = t
			}
		}
		if key.IsZero() {
			key = info.ModTime()
		}
		out = append(out, slotCandidate{slot: slot, sortKey: key, prev: prev})
	}
	return out, nil
}

// assertSlotClean fails the acquire if the previous tenant
// crashed before releasing the slot AND its kernel artefacts are
// still present. The check is scoped to the crash case
// (prev.ReleasedAt == "") because a slot that recorded
// released_at is a strong promise that the release path deleted
// the named resources before writing the timestamp; any extant
// resource with the same name today was created by a later
// unrelated process (e.g. a concurrent run of a deterministically-
// named script in the test corpus) and is not a leak this
// acquirer should attribute. Treating that as a leak produces
// false positives whenever the same script runs back-to-back on
// the same host.
//
// Both checks remain name-targeted (no link-table dumps) so
// NLM_F_DUMP_INTR cannot strike under parallel churn.
func assertSlotClean(slot uint32, prev provenance, linkCheck linkExistsFn, netnsCheck netnsExistsFn) error {
	if prev.ReleasedAt != "" {
		return nil
	}
	if prev.LinkAName != "" && linkCheck(prev.LinkAName) {
		return leakError(slot, prev, "link", prev.LinkAName)
	}
	if prev.NsName != "" && netnsCheck(prev.NsName) {
		return leakError(slot, prev, "netns", prev.NsName)
	}
	if prev.NsBName != "" && netnsCheck(prev.NsBName) {
		return leakError(slot, prev, "netns", prev.NsBName)
	}
	return nil
}

// leakError formats an attributed leak message naming the previous
// tenant, when the slot was last released (or that it never was),
// and which resource is still present. The next test fails as a
// canary rather than letting the leak propagate into a mystery
// EEXIST elsewhere.
func leakError(slot uint32, prev provenance, kind, name string) error {
	when := prev.ReleasedAt
	if when == "" {
		if prev.AcquiredAt != "" {
			when = "never released (acquired " + prev.AcquiredAt + ")"
		} else {
			when = "never released"
		}
	} else {
		when = "released " + when
	}
	origin := prev.Origin
	if origin == "" {
		origin = "<unknown caller>"
	}
	return fmt.Errorf("net pool: slot %d still has %s %q from previous tenant %s (%s)", slot, kind, name, origin, when)
}

// slotLockPath returns the on-disk lockfile path for a slot,
// zero-padded to two digits so `ls` orders the files numerically.
func slotLockPath(root string, slot uint32) string {
	return filepath.Join(root, fmt.Sprintf("%02d.lock", slot))
}

// slotAddrs returns the host and peer bare-address strings for a
// slot. Layout: slot n occupies the /30 at base 4*(n-1); host is
// base+1, peer is base+2.
func slotAddrs(slot uint32) (host, peer string) {
	base := 4 * (slot - 1)
	return fmt.Sprintf("%s%d", poolSubnetPrefix, base+1), fmt.Sprintf("%s%d", poolSubnetPrefix, base+2)
}

// readProvenance loads the slot body from path, ignoring parse
// errors so a malformed legacy body is treated as empty and falls
// back to mtime-based ordering.
func readProvenance(path string) (provenance, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return provenance{}, fmt.Errorf("net pool: read %s: %w", path, err)
	}

	var p provenance
	if len(body) > 0 {
		_ = json.Unmarshal(body, &p)
	}
	return p, nil
}

// readProvenanceFromFile is the under-the-lock variant: the caller
// already holds the open fd, so we seek to start and decode
// without reopening the file. Parse failures degrade to the empty
// provenance the same way readProvenance does.
func readProvenanceFromFile(f *os.File) (provenance, error) {
	if _, err := f.Seek(0, 0); err != nil {
		return provenance{}, err
	}

	body, err := readAll(f)
	if err != nil {
		return provenance{}, err
	}

	var p provenance
	if len(body) > 0 {
		_ = json.Unmarshal(body, &p)
	}
	return p, nil
}

// readAll drains the file into a byte slice. It exists as a small
// helper so writeProvenance and readProvenanceFromFile do not
// reach for io.ReadAll directly and bring an extra import.
func readAll(f *os.File) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	buf := make([]byte, info.Size())
	_, err = f.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, fs.ErrClosed) {
		return buf, err
	}
	return buf, nil
}

// writeProvenance truncates the lockfile, rewinds, and writes the
// JSON body. The fd is held under flock so no concurrent writer
// can interleave; the truncate+rewrite is atomic from external
// observers' perspective.
func writeProvenance(f *os.File, p provenance) error {
	if err := f.Truncate(0); err != nil {
		return err
	}

	if _, err := f.Seek(0, 0); err != nil {
		return err
	}

	enc := json.NewEncoder(f)
	return enc.Encode(p)
}

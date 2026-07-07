//go:build e2e

// Package testnet provides shared network primitives -- veth
// pairs, dummy interfaces, and the address-pool plumbing they
// rest on -- for bpfman e2e tests. Each helper creates a
// uniquely-named primitive, registers t.Cleanup, and enforces the
// invariants that matter under heavy parallel test execution:
// stable MACs that the kernel never regenerates from a peer
// teardown, address-index reuse cooldown so a fresh test never
// inherits in-flight kernel state from the slot's previous
// occupant, and leak attribution to the prior test if the
// invariants do break.
//
// Lives in its own package so test binaries outside the e2e
// package (e.g. e2e/grpc) can share the same veth helpers instead
// of growing parallel thinner versions.
package testnet

import (
	"bytes"
	"context"
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vishvananda/netlink"

	"github.com/vishvananda/netns"

	"github.com/bpfman/bpfman/internal/testnetroute"

	bpfnetns "github.com/bpfman/bpfman/ns/netns"
)

// RootNetns is the name under /run/netns/ that the e2e suite
// bind-mounts at TestMain entry so test code can wrap kernel
// operations in `ip netns exec <RootNetns> ...` and have them
// target the root netns regardless of which Go thread happens to
// run the command. NewTestVethPair uses this for the `ip monitor`
// it starts; callers that need a stable root-netns handle for
// their own wrapping read it from here.
//
// The mount itself is the e2e suite's responsibility (see
// e2e/netns_root_mount_test.go). When the mount is not present
// (e.g. the gRPC parallel test which does not set it up),
// commands that depend on it fail and the call site logs the
// failure instead of treating it as fatal -- the diagnostic
// wrapper is best-effort.
const RootNetns = "root"

// TestInterface is a kernel interface created for a single test.
// Programs attach by Name (the kernel-visible name) or Ifindex,
// and the namespace identity captured here is the netns the
// interface was created in -- not whichever netns the test
// goroutine happens to be running in at any given moment.
//
// We capture Nsid at creation time rather than reading it later
// because Go's runtime moves goroutines between OS threads, and a
// thread's netns is per-thread state in the kernel. CurrentNSID
// reads the calling thread's /proc/self/ns/net at call time, so
// the answer drifts under concurrent test goroutines that switch
// netns and don't restore. The construction-time capture happens
// on whichever goroutine created the interface, which by
// definition is the goroutine that knows what netns it intended.
type TestInterface struct {
	// Name is the kernel-visible interface name programs attach by.
	Name string

	// Ifindex is the kernel interface index programs attach by.
	Ifindex int

	// Nsid is the inode of the netns the interface was created
	// in, captured at construction so it does not drift when the
	// test goroutine later moves between OS threads or switches
	// netns.
	Nsid uint64
}

var testNameSeq atomic.Uint64

// uniqueTestName generates a unique name for test network
// interfaces and namespaces. The name starts with "B" and ends
// with "N" (the first and last letters of "bpfman" upper-cased),
// with 12 hex characters between them derived from hashing the
// PID and an atomic counter. The result is 14 characters,
// leaving room for a single veth suffix within the IFNAMSIZ
// limit of 15.
func uniqueTestName() string {
	n := testNameSeq.Add(1)
	h := fnv.New64a()
	fmt.Fprintf(h, "%d:%d", os.Getpid(), n)
	return fmt.Sprintf("B%012xN", h.Sum64()&0xffffffffffff)
}

// slotProvenance records the most recent occupant of a pair
// index slot so a stale-state check on the next acquire can
// attribute any leaked kernel state to a specific test.
type slotProvenance struct {
	testName   string
	nsName     string
	linkAName  string // name of A-side veth in root namespace
	releasedAt time.Time
}

// vethAddrPool tracks which pair indices in [1, 127] are
// currently allocated. Indices map to /32 addresses inside RFC
// 5737 TEST-NET-2 (198.51.100.0/24) via vethAddrsForIndex. The
// pool is sized for peak concurrent veth pairs across parallel
// tests, not the cumulative total over the lifetime of the
// process: NewTestVethPair acquires an index, the t.Cleanup
// releases it.
//
// Allocation order is FIFO: the oldest released index is handed
// out first. Each pair index pins a (deterministic MAC, IP)
// pair; reusing the most recently released index immediately
// would put a fresh test on top of kernel state (ARP entries,
// refcount-pending XDP links on the just-deleted veth's
// ifindex) that has not finished tearing down. FIFO maximises
// the cooldown each freed index gets before reuse without
// growing the pool.
//
// last[idx] keeps the forensic breadcrumb of the slot's most
// recent occupant so acquire-time stale-state checks can name
// the test that leaked.
var vethAddrPool = struct {
	mu   sync.Mutex
	used [128]bool // index 0 unused; valid range is [1, 127]
	free []uint32  // FIFO queue of free indices; head = oldest
	last [128]slotProvenance
}{
	free: func() []uint32 {
		q := make([]uint32, 0, 127)
		for i := uint32(1); i <= 127; i++ {
			q = append(q, i)
		}
		return q
	}(),
}

// acquireVethAddrs takes the oldest free index from
// vethAddrPool, asserts that no kernel state from the slot's
// previous occupant is still present, and returns the slot's
// /32 addresses. Panics if the pool is exhausted -- that means
// more than 127 veth pairs are alive concurrently, which is well
// past expected parallelism and indicates either a leak
// (releaseVethAddrs not called) or genuinely too many parallel
// tests for this address range.
//
// nsName and linkAName are the names the caller is about to
// bind to this slot. They are recorded so that the *next*
// acquire of this slot can verify the previous occupant
// actually deleted them. testName is t.Name() captured before
// any failure path so attribution in panics or fatals is always
// accurate.
func acquireVethAddrs(t *testing.T, nsName, linkAName string) (addrA, addrB, pingTarget string, pairIndex uint32) {
	t.Helper()
	vethAddrPool.mu.Lock()
	defer vethAddrPool.mu.Unlock()
	if len(vethAddrPool.free) == 0 {
		panic("veth address pool exhausted: more than 127 concurrent veth pairs in flight (leak or excessive parallelism)")
	}
	pairIndex = vethAddrPool.free[0]
	vethAddrPool.free = vethAddrPool.free[1:]

	addrA, addrB, pingTarget = vethAddrsForIndex(pairIndex)

	if err := assertSlotClean(pairIndex, vethAddrPool.last[pairIndex]); err != nil {
		t.Fatalf("acquireVethAddrs: %v", err)
	}

	vethAddrPool.used[pairIndex] = true
	vethAddrPool.last[pairIndex] = slotProvenance{
		testName:  t.Name(),
		nsName:    nsName,
		linkAName: linkAName,
	}
	return addrA, addrB, pingTarget, pairIndex
}

// assertSlotClean verifies that no kernel state attributable to
// the slot's previous occupant is still present. Checks the two
// resources the previous occupant owned by name:
//
//   - the A-side veth in root namespace (LinkByName)
//   - the netns the B-side lived in (netns.GetFromName)
//
// Both must be absent; if either remains, the previous
// occupant's t.Cleanup did not finish. On a leak the error
// names the test that previously held the slot, the kind of
// leak, and how long ago the slot was released. The caller
// raises this via t.Fatalf so the leak fails the *next* test
// as a canary, surfaced loudly with attribution rather than
// silently propagating into mysterious EBUSY/EEXIST further
// down the line.
//
// Targeted lookups (rather than dumping the whole link table)
// avoid NLM_F_DUMP_INTR under heavy parallel churn and answer
// the exact "did the previous tenant clean up?" question.
func assertSlotClean(idx uint32, prev slotProvenance) error {
	if prev.linkAName != "" {
		if lnk, err := netlink.LinkByName(prev.linkAName); err == nil {
			attrs := lnk.Attrs()
			return fmt.Errorf("pair index %d: root-ns interface %q (ifindex %d) from previous tenant test=%q still exists (released %s ago); cleanup did not delete it", idx, attrs.Name, attrs.Index, prev.testName, time.Since(prev.releasedAt))
		}
	}
	if prev.nsName != "" {
		if h, err := netns.GetFromName(prev.nsName); err == nil {
			h.Close()
			return fmt.Errorf("pair index %d: netns %q from previous tenant test=%q still exists (released %s ago); cleanup did not delete it", idx, prev.nsName, prev.testName, time.Since(prev.releasedAt))
		}
	}
	return nil
}

// releaseVethAddrs returns an index to vethAddrPool so the next
// acquireVethAddrs can reuse its addresses. Released indices go
// to the tail of the FIFO queue, ensuring maximum cooldown
// before reuse. Idempotent for the no-op case (already-free
// index) so a double-cleanup doesn't crash; the index becoming
// free twice is benign and the second release is dropped.
func releaseVethAddrs(pairIndex uint32) {
	vethAddrPool.mu.Lock()
	defer vethAddrPool.mu.Unlock()
	if pairIndex < 1 || pairIndex > 127 {
		return
	}
	if !vethAddrPool.used[pairIndex] {
		return
	}
	vethAddrPool.used[pairIndex] = false
	vethAddrPool.last[pairIndex].releasedAt = time.Now()
	vethAddrPool.free = append(vethAddrPool.free, pairIndex)
}

// vethAddrsForIndex returns unique /32 addresses for the given
// pair index. The index must be in [1, 127].
func vethAddrsForIndex(n uint32) (addrA, addrB, pingTarget string) {
	if n < 1 || n > 127 {
		panic(fmt.Sprintf("veth pair index %d out of range [1, 127]", n))
	}
	hostA := n*2 + 1 // 3, 5, 7, ...
	hostB := n * 2   // 2, 4, 6, ...
	addrA = fmt.Sprintf("198.51.100.%d/32", hostA)
	addrB = fmt.Sprintf("198.51.100.%d/32", hostB)
	pingTarget = fmt.Sprintf("198.51.100.%d", hostA)
	return
}

// NewTestInterface creates a dummy network interface for testing.
// The interface is automatically deleted via t.Cleanup().
// Each test gets a unique interface, enabling parallel execution.
func NewTestInterface(t *testing.T) TestInterface {
	t.Helper()

	name := uniqueTestName()

	t.Logf("creating interface %s", name)

	// Fail if interface already exists - indicates a leak from a previous test.
	if _, err := netlink.LinkByName(name); err == nil {
		t.Fatalf("interface %s already exists (leaked from previous test?)", name)
	}

	dummy := &netlink.Dummy{
		LinkAttrs: netlink.LinkAttrs{Name: name, TxQLen: 1000},
	}

	if err := netlink.LinkAdd(dummy); err != nil {
		t.Fatalf("failed to create dummy interface %s: %v", name, err)
	}

	t.Cleanup(func() {
		// Best effort cleanup - interface may already be gone
		if link, err := netlink.LinkByName(name); err == nil {
			netlink.LinkDel(link)
		}
	})

	link, err := netlink.LinkByName(name)
	if err != nil {
		t.Fatalf("failed to find dummy interface %s: %v", name, err)
	}

	if err := netlink.LinkSetUp(link); err != nil {
		t.Fatalf("failed to bring up interface %s: %v", name, err)
	}

	rootNsid, err := bpfnetns.CurrentNSID()
	if err != nil {
		t.Fatalf("get root nsid: %v", err)
	}

	return TestInterface{
		Name:    name,
		Ifindex: link.Attrs().Index,
		Nsid:    rootNsid,
	}
}

// TestVethPair holds information about a veth pair where one end
// is in the root namespace and the other is in a test network
// namespace. Programs are attached to interface A (root
// namespace); traffic is generated from interface B (test
// namespace).
type TestVethPair struct {
	// A is the root-namespace end of the pair; attach programs here.
	A TestInterface

	// B is the test-namespace end of the pair; generate traffic here.
	B TestInterface

	// Netns is the name of the test network namespace holding B.
	Netns string

	// PingTarget is A's IP address, the ping destination from B.
	PingTarget string
}

// vethConfig captures NewTestVethPair's tunable behaviour.
// Fields default to the values most callers want (warmup on,
// ip monitor on).
type vethConfig struct {
	waitForConnectivity bool
	startIPMonitor      bool
}

// VethOption tunes NewTestVethPair. Pass zero or more of these
// to opt out of work the caller does not need.
type VethOption func(*vethConfig)

// WithoutConnectivityWarmup skips the ARP warmup ping (and the
// associated ip-monitor diagnostic stream, since it exists to
// debug traffic-flow failures the caller is no longer asking
// about). Use when the caller only needs a netif handle to
// attach a BPF program to and does not generate traffic through
// the veth path -- e.g. an XDP/TC/TCX program-attach
// correctness test whose subsequent assertions never put a
// packet on the wire.
//
// The warmup is what makes Ping / PingExact / PingExpectDrop
// deterministic: it proves end-to-end ARP has resolved before
// the test's real packet burst starts, so a single lost packet
// from an unresolved-ARP race never fails an exact-count
// assertion. Callers that DO ping (the in-process e2e suite's
// dispatcher / multi-prog / chain-stops tests) must leave the
// warmup on. The default is on; this option is the explicit
// opt-out.
//
// Skipping the warmup is also the only way to push concurrent
// veth setup past N ~= 40 on a busy machine: the warmup ping's
// 30-second budget is the dominant per-veth setup cost and is
// the first thing that fails when the kernel cannot service
// concurrent namespace creation + ping warmup fast enough.
func WithoutConnectivityWarmup() VethOption {
	return func(c *vethConfig) {
		c.waitForConnectivity = false
		c.startIPMonitor = false
	}
}

// NewTestVethPair creates a veth pair with one end in a dedicated
// network namespace for generating real traffic through TC hooks.
//
// A unique name and unique /32 addresses from RFC 5737 TEST-NET-2
// (198.51.100.0/24) are generated automatically. Interface A
// stays in the root namespace; interface B is moved to a new
// network namespace. Peer routes ensure each pair has its own
// distinct routing entry, avoiding conflicts when multiple pairs
// coexist.
//
// Both interfaces and the namespace are cleaned up via
// t.Cleanup().
//
// By default the helper warms ARP with a ping and starts an
// ip-monitor for diagnostic logging. Callers that do not generate
// traffic through the pair can opt out with
// WithoutConnectivityWarmup.
func NewTestVethPair(t *testing.T, opts ...VethOption) TestVethPair {
	t.Helper()

	cfg := vethConfig{waitForConnectivity: true, startIPMonitor: true}
	for _, opt := range opts {
		opt(&cfg)
	}

	base := uniqueTestName()
	nameA := base + "a"
	nameB := base + "b"
	nsName := base

	// The host end stays in the root namespace, so replies to the
	// pair's TEST-NET-2 addresses resolve through host policy
	// routing, which a VPN can hijack. Establish the harness's
	// bypass rule before building any topology; the per-test
	// host-route precheck then verifies the invariant holds.
	if err := testnetroute.Ensure(); err != nil {
		t.Fatalf("ensure test-net policy rule: %v", err)
	}

	// Fail if interfaces already exist.
	for _, name := range []string{nameA, nameB} {
		if _, err := netlink.LinkByName(name); err == nil {
			t.Fatalf("interface %s already exists (leaked from previous test?)", name)
		}
	}

	// Fail if namespace already exists.
	if _, err := netns.GetFromName(nsName); err == nil {
		t.Fatalf("namespace %s already exists (leaked from previous test?)", nsName)
	}

	// Acquire the address index first and register its release
	// cleanup before any kernel artefacts (namespace, veth) so
	// LIFO cleanup order is: delete veth -> delete namespace ->
	// release index. The release must run *after* the kernel
	// has removed the interface (and with it the addresses and
	// routes) so a concurrent acquirer reusing the same index
	// doesn't try to add the still-present /32 to a new veth.
	ipA, ipB, pingTarget, pairIdx := acquireVethAddrs(t, nsName, nameA)
	t.Cleanup(func() { releaseVethAddrs(pairIdx) })

	// Create the named netns. CreateNamed handles the
	// LockOSThread / NewNamed / restore / UnlockOSThread
	// dance correctly: on any error after the new netns is
	// created the OS thread is left locked so that t.Fatalf
	// retires it rather than returning a poisoned thread
	// (still in the named netns) to Go's scheduler.
	if err := bpfnetns.CreateNamed(nsName); err != nil {
		t.Fatalf("%v", err)
	}

	t.Cleanup(func() {
		netns.DeleteNamed(nsName)
	})

	t.Logf("creating veth pair %s/%s in namespace %s", nameA, nameB, nsName)

	// Compute deterministic MACs and pass them at create time
	// rather than overwriting after creation. See the long
	// comment further down for the format. Setting
	// the address at LinkAdd time (via LinkAttrs.HardwareAddr /
	// PeerHardwareAddr) means the kernel never assigns a random
	// MAC in the first place; subsequent NETDEV_CHANGE storms
	// from sibling subtests' teardown cannot regenerate an
	// address that wasn't kernel-generated.
	pid := os.Getpid()
	macA, _ := net.ParseMAC(fmt.Sprintf("02:%02x:%02x:00:%02x:01", (pid>>8)&0xff, pid&0xff, pairIdx))
	macB, _ := net.ParseMAC(fmt.Sprintf("02:%02x:%02x:00:%02x:02", (pid>>8)&0xff, pid&0xff, pairIdx))

	// Create veth pair in root namespace with MACs baked in.
	veth := &netlink.Veth{
		LinkAttrs:        netlink.LinkAttrs{Name: nameA, TxQLen: 1000, HardwareAddr: macA},
		PeerName:         nameB,
		PeerHardwareAddr: macB,
	}
	if err := netlink.LinkAdd(veth); err != nil {
		t.Fatalf("failed to create veth pair %s/%s: %v", nameA, nameB, err)
	}
	t.Cleanup(func() {
		if link, err := netlink.LinkByName(nameA); err == nil {
			netlink.LinkDel(link)
		}
	})

	// Set TxQLen on peer before moving it into the namespace.
	linkB, err := netlink.LinkByName(nameB)
	if err != nil {
		t.Fatalf("failed to find interface %s: %v", nameB, err)
	}

	if err := netlink.LinkSetTxQLen(linkB, 1000); err != nil {
		t.Fatalf("failed to set txqlen on %s: %v", nameB, err)
	}

	// Move B into the namespace via netlink.
	nsHandleForMove, err := netns.GetFromName(nsName)
	if err != nil {
		t.Fatalf("failed to get ns handle for %s: %v", nsName, err)
	}

	if err := netlink.LinkSetNsFd(linkB, int(nsHandleForMove)); err != nil {
		nsHandleForMove.Close()
		t.Fatalf("failed to move %s to namespace %s: %v", nameB, nsName, err)
	}

	nsHandleForMove.Close()

	// Configure A in root namespace with a peer route to B.
	linkA, err := netlink.LinkByName(nameA)
	if err != nil {
		t.Fatalf("failed to find interface %s: %v", nameA, err)
	}

	// Set deterministic, locally administered MAC addresses on
	// both veth ends. This is essential for parallel test
	// stability.
	//
	// The kernel auto-assigns random MACs at veth creation time
	// and marks them NET_ADDR_RANDOM internally. Under
	// concurrent veth creation and deletion (as happens with
	// parallel subtests whose t.Cleanup tears down finished
	// pairs while other pairs are still live), the kernel can
	// regenerate MACs marked NET_ADDR_RANDOM on unrelated
	// interfaces. This invalidates ARP caches and causes 100%
	// ping packet loss.
	//
	// Explicitly setting the MAC via LinkSetHardwareAddr changes
	// the kernel's addr_assign_type to NET_ADDR_SET, which the
	// kernel treats as sacrosanct and never regenerates.
	//
	// We use the IEEE 802 locally administered address space.
	// The first octet's two least-significant bits control the
	// address type:
	//
	//   First octet: 0x02
	//
	//     bit 7   bit 0
	//      |       |
	//      v       v
	//      0 0 0 0 0 0 1 0
	//                  | |
	//                  | +-- 0 = unicast (vs 1 = multicast)
	//                  +---- 1 = locally administered (vs 0 = OUI/global)
	//
	// The "locally administered" bit (bit 1) indicates the
	// address is not from a globally unique OUI allocation but
	// a locally scoped assignment, analogous to RFC 1918 for IP
	// addresses. Any MAC with this bit set is guaranteed never
	// to collide with a manufacturer-assigned address.
	//
	// The full format is:
	//
	//   02:<pid_hi>:<pid_lo>:00:<pair>:<end>
	//
	// where <pid_hi>:<pid_lo> are the two least-significant
	// bytes of the process ID (ensuring uniqueness across
	// concurrent stress test processes), <pair> is the veth
	// pair index returned by acquireVethAddrs (ensuring
	// uniqueness within a process and avoiding a race between
	// Add and Load on the atomic counter), and <end> is 01 for
	// the A side (root namespace) or 02 for the B side (test
	// namespace).
	//
	// Parallel subtests flake without explicit MACs: a live
	// veth interface's MAC can change mid-test (same ifindex,
	// different MAC), immediately followed by the kernel
	// flushing ARP neighbour entries and regenerating the IPv6
	// link-local address (derived via EUI-64 from the new MAC).
	// The MAC change correlates with another subtest's t.Cleanup
	// deleting its own (unrelated) veth pair, and the MAC
	// regeneration is kernel-internal and asynchronous, so
	// serialising setup and teardown under a mutex does not help.
	// The strategy is to assign explicit MACs at LinkAdd time and
	// disable IPv6 on both ends so parallel subtests do not flake.
	//
	// In the kernel 6.12 source the chain of events is: when a
	// veth peer is deleted,
	// veth_dellink triggers carrier loss on the surviving end
	// via netif_carrier_off, which fires a NETDEV_CHANGE
	// notification through the linkwatch subsystem. The IPv6
	// addrconf_notify handler (net/ipv6/addrconf.c) processes
	// NETDEV_CHANGE and calls addrconf_dev_config, which
	// triggers EUI-64 link-local address generation from the
	// device's MAC. This processing chain appears to cause MAC
	// regeneration on other veth interfaces as a side effect.
	//
	// The current strategy passes the deterministic MAC at
	// LinkAdd time via LinkAttrs.HardwareAddr and
	// PeerHardwareAddr, so the kernel never assigns a random MAC
	// in the first place.
	// There is no "original" address for any later code path
	// to regenerate back to. disableIPv6 is retained as a
	// secondary defence and to avoid wasting cycles on
	// IPv6 link-local plumbing the tests do not use.

	// Disable IPv6 on A before link-up. We only need IPv4 for
	// the ping traffic, and IPv6 link-local address generation
	// triggers kernel code paths that can regenerate MAC
	// addresses on veth interfaces under concurrent load.
	disableIPv6(t, nameA)

	addrA, _ := netlink.ParseAddr(ipA)
	peerOfA, _ := netlink.ParseAddr(ipB)
	addrA.Peer = peerOfA.IPNet
	if err := netlink.AddrAdd(linkA, addrA); err != nil {
		t.Fatalf("failed to add address to %s: %v", nameA, err)
	}

	if err := netlink.LinkSetUp(linkA); err != nil {
		t.Fatalf("failed to bring up %s: %v", nameA, err)
	}

	// Configure B inside the namespace via a netlink handle.
	nsHandleForConfig, err := netns.GetFromName(nsName)
	if err != nil {
		t.Fatalf("failed to get ns handle for config: %v", err)
	}

	nlh, err := netlink.NewHandleAt(nsHandleForConfig)
	nsHandleForConfig.Close()
	if err != nil {
		t.Fatalf("failed to create netlink handle in namespace %s: %v", nsName, err)
	}
	defer nlh.Close()

	nsLinkB, err := nlh.LinkByName(nameB)
	if err != nil {
		t.Fatalf("failed to find %s in namespace: %v", nameB, err)
	}
	// B's MAC was set at LinkAdd time via PeerHardwareAddr.

	// Disable IPv6 on B inside the namespace.
	disableIPv6InNs(t, nsName, nameB)

	addrB, _ := netlink.ParseAddr(ipB)
	peerOfB, _ := netlink.ParseAddr(ipA)
	addrB.Peer = peerOfB.IPNet
	if err := nlh.AddrAdd(nsLinkB, addrB); err != nil {
		t.Fatalf("failed to add address to %s: %v", nameB, err)
	}

	if err := nlh.LinkSetUp(nsLinkB); err != nil {
		t.Fatalf("failed to bring up %s: %v", nameB, err)
	}

	// Bring up loopback in the namespace.
	lo, err := nlh.LinkByName("lo")
	if err != nil {
		t.Fatalf("failed to find lo in namespace: %v", err)
	}

	if err := nlh.LinkSetUp(lo); err != nil {
		t.Fatalf("failed to bring up lo in namespace: %v", err)
	}

	// Wait for both veth ends to reach OperUp. Veth interfaces
	// transition to OperUp once both peers are up, but there
	// can be a brief kernel event propagation delay under load.
	waitLinkOperUp(t, nil, nameA, 5*time.Second)
	waitLinkOperUp(t, nlh, nameB, 5*time.Second)

	if cfg.waitForConnectivity {
		// Verify end-to-end connectivity with a warmup ping.
		// Under heavy parallel load ARP resolution can lag
		// behind link-up.
		waitConnectivity(t, nsName, pingTarget, 30*time.Second)

		// Verify ARP consistency: B's cached MAC for A must
		// match A's actual MAC. Log both for debugging
		// intermittent failures.
		linkARefresh, _ := netlink.LinkByName(nameA)
		aMac := linkARefresh.Attrs().HardwareAddr.String()
		aIdx := linkARefresh.Attrs().Index
		arpOut, _ := exec.Command("ip", "netns", "exec", nsName, "ip", "neigh", "show", "dev", nameB, pingTarget).CombinedOutput()
		t.Logf("post-warmup: A=%s ifindex=%d MAC=%s, B's ARP: %s", nameA, aIdx, aMac, strings.TrimSpace(string(arpOut)))
	}

	if cfg.startIPMonitor {
		// Start ip monitor to capture link state events for
		// this veth pair. Output is logged on test
		// completion. Wrapped via `ip netns exec <RootNetns>`
		// so monitor is bound to the bind-mounted root netns
		// regardless of caller thread state. If the root-netns
		// mount isn't present (e.g. the gRPC parallel test
		// which doesn't set it up), the command fails to start
		// and we log instead of fataling -- the monitor is
		// best-effort diagnostic only.
		var monBuf bytes.Buffer
		monCmd := exec.Command("ip", "netns", "exec", RootNetns, "ip", "monitor", "link", "address", "route", "neigh")
		monCmd.Stdout = &monBuf
		monCmd.Stderr = &monBuf
		if err := monCmd.Start(); err != nil {
			t.Logf("ip monitor failed to start: %v", err)
		} else {
			t.Cleanup(func() {
				monCmd.Process.Kill()
				monCmd.Wait()
				// Filter output to only show events for our interfaces.
				for line := range strings.SplitSeq(monBuf.String(), "\n") {
					if strings.Contains(line, nameA) || strings.Contains(line, nameB) || strings.Contains(line, nsName) {
						t.Logf("[ip-monitor %s] %s", base, line)
					}
				}
			})
		}
	}

	rootNsid, err := bpfnetns.CurrentNSID()
	if err != nil {
		t.Fatalf("get root nsid: %v", err)
	}

	bNsid, err := bpfnetns.NSID("/var/run/netns/" + nsName)
	if err != nil {
		t.Fatalf("get nsid for test netns %s: %v", nsName, err)
	}

	return TestVethPair{
		A: TestInterface{
			Name:    nameA,
			Ifindex: linkA.Attrs().Index,
			Nsid:    rootNsid,
		},
		B: TestInterface{
			Name: nameB,
			Nsid: bNsid,
		},
		Netns:      nsName,
		PingTarget: pingTarget,
	}
}

// pingMode selects ping behaviour for the private ping helper.
// The three Ping* public methods each pick one mode; the modes
// aren't combinatorial flags, they're discrete choices, so an
// enum reads better than a pair of bools at the helper's call
// sites.
type pingMode int

const (
	// pingDefensive re-verifies connectivity before the burst
	// (one extra pre-burst ping) and fails on any reply loss.
	// Default for non-exact-equality tests under heavy parallel
	// load -- the re-verify re-warms ARP that other tests'
	// cleanup may have evicted.
	pingDefensive pingMode = iota

	// pingExactCount skips the re-verify so the burst is
	// exactly N ICMP echo requests on A's ingress, and fails on
	// any reply loss. For exact-equality counter assertions.
	pingExactCount

	// pingExpectDrop skips the re-verify and tolerates 100%
	// reply loss. For chain-stops tests where an attached BPF
	// program drops packets at A's ingress (e.g. a multi-prog
	// XDP chain whose middle program returns XDP_DROP).
	pingExpectDrop
)

// Ping sends count ICMP echo requests from the veth pair's B
// interface (inside the test namespace) to A's IP address. This
// generates real ingress traffic on A, triggering any attached
// TC programs. Does a defensive re-verify before the burst,
// which adds one extra echo request to A's ingress -- non-exact
// tests use this form; tests that assert exact counts should use
// PingExact.
func (v TestVethPair) Ping(t *testing.T, count int) {
	t.Helper()
	v.ping(t, count, pingDefensive)
}

// PingExact is Ping without the pre-burst re-verify. Use for
// exact-equality counter assertions, where the extra ping
// inflates the count by one. Relies on the initial
// waitConnectivity at veth creation having warmed ARP; do not
// call across long pauses where concurrent cleanup elsewhere
// could disrupt link state.
func (v TestVethPair) PingExact(t *testing.T, count int) {
	t.Helper()
	v.ping(t, count, pingExactCount)
}

// PingExpectDrop fires count ICMP echo requests but tolerates
// 100% reply loss. Use when an attached BPF program is expected
// to drop packets at A's ingress (e.g. a multi-program XDP chain
// where the middle program returns XDP_DROP to terminate the
// chain). The kernel still sends the N requests from B; the
// counter at A's BPF program advances exactly N times even
// though the kernel ICMP responder never gets them.
func (v TestVethPair) PingExpectDrop(t *testing.T, count int) {
	t.Helper()
	v.ping(t, count, pingExpectDrop)
}

func (v TestVethPair) ping(t *testing.T, count int, mode pingMode) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pre-ping MAC/ifindex check for debugging.
	linkA, err := netlink.LinkByName(v.A.Name)
	if err != nil {
		t.Fatalf("pre-ping: cannot find %s: %v", v.A.Name, err)
	}

	t.Logf("pre-ping: A=%s ifindex=%d MAC=%s (expected ifindex=%d)", v.A.Name, linkA.Attrs().Index, linkA.Attrs().HardwareAddr, v.A.Ifindex)
	if linkA.Attrs().Index != v.A.Ifindex {
		t.Errorf("IFINDEX CHANGED: was %d at creation, now %d -- interface was recreated!", v.A.Ifindex, linkA.Attrs().Index)
	}

	if mode == pingDefensive {
		// Re-verify connectivity before the test burst. Under
		// heavy parallel load, concurrent veth cleanup from
		// other tests can disrupt link state. This re-
		// establishes ARP entries.
		waitConnectivity(t, v.Netns, v.PingTarget, 30*time.Second)
	}

	cmd := exec.CommandContext(ctx, "ip", "netns", "exec", v.Netns, "ping", "-c", strconv.Itoa(count), "-i", "0.1", "-W", "1", v.PingTarget)
	out, err := cmd.CombinedOutput()
	if err != nil && mode != pingExpectDrop {
		v.dumpNetworkState(t, "ping-failure")
		t.Fatalf("ping failed: %v\n%s", err, out)
	}
}

// dumpNetworkState logs diagnostic information about the veth
// pair to help debug connectivity failures.
func (v TestVethPair) dumpNetworkState(t *testing.T, label string) {
	t.Helper()

	// Root namespace: interface A state, addresses, routes,
	// ARP, TC filters.
	for _, args := range [][]string{
		{"ip", "link", "show", v.A.Name},
		{"ip", "addr", "show", v.A.Name},
		{"ip", "route", "show", "dev", v.A.Name},
		{"ip", "neigh", "show", "dev", v.A.Name},
		{"tc", "filter", "show", "dev", v.A.Name, "ingress"},
	} {
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		if err != nil {
			t.Logf("[%s root] %s: error: %v", label, strings.Join(args, " "), err)
		} else {
			t.Logf("[%s root] %s:\n%s", label, strings.Join(args, " "), out)
		}
	}

	// Test namespace: interface B state, addresses, routes, ARP.
	for _, args := range [][]string{
		{"ip", "netns", "exec", v.Netns, "ip", "link", "show", v.B.Name},
		{"ip", "netns", "exec", v.Netns, "ip", "addr", "show", v.B.Name},
		{"ip", "netns", "exec", v.Netns, "ip", "route", "show", "dev", v.B.Name},
		{"ip", "netns", "exec", v.Netns, "ip", "neigh", "show", "dev", v.B.Name},
	} {
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		if err != nil {
			t.Logf("[%s ns %s] %s: error: %v", label, v.Netns, strings.Join(args, " "), err)
		} else {
			t.Logf("[%s ns %s] %s:\n%s", label, v.Netns, strings.Join(args, " "), out)
		}
	}
}

// waitLinkOperUp polls until the named interface reports OperUp.
// Pass a nil handle to query the root network namespace.
func waitLinkOperUp(t *testing.T, h *netlink.Handle, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var link netlink.Link
		var err error
		if h != nil {
			link, err = h.LinkByName(name)
		} else {
			link, err = netlink.LinkByName(name)
		}
		if err == nil && link.Attrs().OperState == netlink.OperUp {
			return
		}
		if time.Now().After(deadline) {
			state := "unknown"
			if err == nil {
				state = link.Attrs().OperState.String()
			}
			t.Fatalf("interface %s did not reach OperUp within %v (current state: %s)", name, timeout, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitConnectivity sends single pings with retries until one
// succeeds, proving the veth path is ready for traffic.
func waitConnectivity(t *testing.T, nsName, target string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		cmd := exec.CommandContext(ctx, "ip", "netns", "exec", nsName,
			"ping", "-c", "1", "-W", "1", target)
		err := cmd.Run()
		cancel()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("veth pair connectivity not established within %v", timeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// disableIPv6 disables IPv6 on an interface in the root namespace
// via sysctl. This must be called before LinkSetUp to prevent the
// kernel from generating an IPv6 link-local address.
func disableIPv6(t *testing.T, ifaceName string) {
	t.Helper()
	path := fmt.Sprintf("/proc/sys/net/ipv6/conf/%s/disable_ipv6", ifaceName)
	if err := os.WriteFile(path, []byte("1"), 0644); err != nil {
		t.Fatalf("failed to disable IPv6 on %s: %v", ifaceName, err)
	}
}

// disableIPv6InNs disables IPv6 on an interface inside a named
// network namespace via ip netns exec.
func disableIPv6InNs(t *testing.T, nsName, ifaceName string) {
	t.Helper()
	sysctl := fmt.Sprintf("net.ipv6.conf.%s.disable_ipv6=1", ifaceName)
	out, err := exec.Command("ip", "netns", "exec", nsName, "sysctl", "-w", sysctl).CombinedOutput()
	if err != nil {
		t.Fatalf("failed to disable IPv6 on %s in ns %s: %v\n%s", ifaceName, nsName, err, out)
	}
}

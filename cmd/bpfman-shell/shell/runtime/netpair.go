package runtime

import (
	"encoding/json"
	"strconv"
	"sync"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

// NetPair is the user-visible handle for a paired-veth single-netns
// topology built by `net veth-pair`. The identity fields (five
// strings naming the netns / interfaces / addresses, plus the
// host-side ifindex and netns inode) are immutable -- set once at
// construction, never rewritten -- and Released is the lifecycle
// latch: net release flips it on the first call; subsequent
// net exec / net start against the same handle reject the call
// as "use after release". Field reads remain valid after release
// because the identity is still a historical description of what
// existed.
//
// Concurrency: Mu guards Released. The identity fields are
// read-only after construction so they need no lock.
type NetPair struct {
	// Ns is the netns name the peer-side veth lives in.
	Ns string

	// HostLink is the host-side veth interface name (the side the
	// BPF programs attach to).
	HostLink string

	// PeerLink is the peer-side veth interface name (lives inside
	// Ns; the side the traffic driver originates from).
	PeerLink string

	// HostAddr is the host-side IPv4 address without a /CIDR
	// suffix, suitable for handing to commands like ping. The
	// CIDR form passed to `ip addr add` is reconstructed inside
	// the builtin and not exposed on the handle.
	HostAddr string

	// PeerAddr is the peer-side IPv4 address without a /CIDR
	// suffix.
	PeerAddr string

	// HostIfindex is the host-side veth interface index, captured
	// at construction time so scripts that need to address the
	// dispatcher attach point (e.g. `bpfman dispatcher get
	// tc-ingress $pair.host_nsid $pair.host_ifindex`) do not have
	// to round-trip through `ip link show`. Zero when the pair
	// was constructed in a path that skipped the ifindex lookup
	// (test fixtures); the runtime path always populates it.
	HostIfindex uint32

	// HostNsid is the inode number of the network namespace the
	// host-side veth lives in -- always the process's own netns
	// for a `net veth-pair` handle. Captured at construction
	// time alongside HostIfindex; same gap rule applies for
	// tests that omit it.
	HostNsid uint64

	// Mu guards Released.
	Mu sync.Mutex

	// Released is true once net release has consumed the
	// topology. Subsequent net exec / net start error; subsequent
	// net release is a no-op (idempotent cleanup).
	Released bool
}

// MarkReleased sets the lifecycle latch and reports whether it was
// already set. The first caller observes false and flips it,
// proceeding with the teardown; subsequent callers observe true and
// can short-circuit the teardown.
func (p *NetPair) MarkReleased() (wasReleased bool) {
	p.Mu.Lock()
	defer p.Mu.Unlock()
	if p.Released {
		return true
	}
	p.Released = true
	return false
}

// IsReleased reports whether the handle has been consumed.
func (p *NetPair) IsReleased() bool {
	p.Mu.Lock()
	defer p.Mu.Unlock()
	return p.Released
}

// ValueFromNetPair wraps p as a Value with semantics.OriginNetPair. The path
// machinery resolves $pair.host_link / $pair.peer_addr / ... through
// the JSON-tree mirror; the underlying *NetPair is recoverable via
// Value.Origin() so the net release / exec / start builtins reach
// the lifecycle latch directly.
func ValueFromNetPair(p *NetPair) Value {
	mirror := map[string]any{
		"ns":           p.Ns,
		"host_link":    p.HostLink,
		"peer_link":    p.PeerLink,
		"host_addr":    p.HostAddr,
		"peer_addr":    p.PeerAddr,
		"host_ifindex": json.Number(strconv.FormatUint(uint64(p.HostIfindex), 10)),
		"host_nsid":    json.Number(strconv.FormatUint(p.HostNsid, 10)),
	}
	return Value{v: mirror, origin: p, kind: semantics.OriginNetPair}
}

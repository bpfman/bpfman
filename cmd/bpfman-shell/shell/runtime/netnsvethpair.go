package runtime

import (
	"encoding/json"
	"strconv"
	"sync"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

// NetnsVethPair is the user-visible handle for the isolated
// topology built by `net netns-veth-pair`: a veth pair whose two
// ends live in owned, named network namespaces. Unlike NetPair
// there is no privileged host side; the two endpoints are
// symmetric and the record exposes them as $pair.a / $pair.b.
//
// The release unit is the pair. An endpoint is a capability for
// execution and field access, not an ownership boundary, so the
// lifecycle latch lives here and governs both sides: after `net
// release $pair`, operational use of either endpoint rejects as
// released. Field reads remain valid after release because the
// identity is still a historical description of what existed.
//
// Concurrency: Mu guards Released. The endpoint identity fields
// are read-only after construction so they need no lock.
type NetnsVethPair struct {
	// A is the first endpoint of the pair. Set once by
	// NewNetnsVethPair, which also installs its back-pointer; never
	// rewritten.
	A *NetnsVethEndpoint

	// B is the second endpoint of the pair. Set once by
	// NewNetnsVethPair, which also installs its back-pointer; never
	// rewritten.
	B *NetnsVethEndpoint

	// Mu guards Released.
	Mu sync.Mutex

	// Released is true once net release has consumed the
	// topology. Subsequent net exec / net start against either
	// endpoint error; subsequent net release is a no-op
	// (idempotent cleanup).
	Released bool
}

// NetnsVethEndpoint is one side of a NetnsVethPair: the netns the
// veth end lives in, the interface name and address inside that
// namespace, and the per-side ifindex and netns inode needed for
// attach assertions and dispatcher scoping. The identity fields
// are immutable after construction.
type NetnsVethEndpoint struct {
	// Ns is the named network namespace this end lives in.
	Ns string

	// Link is the veth interface name inside Ns.
	Link string

	// Addr is the IPv4 address without a /CIDR suffix, suitable
	// for handing to commands like ping.
	Addr string

	// Ifindex is the veth interface index inside Ns, captured at
	// construction time. Zero when the pair was constructed in a
	// path that skipped the lookup (test fixtures); the runtime
	// path always populates it.
	Ifindex uint32

	// Nsid is the inode number of Ns, captured at construction
	// time alongside Ifindex; same gap rule applies for tests
	// that omit it.
	Nsid uint64

	// Pair points back at the owning NetnsVethPair so `net exec
	// $pair.a` can consult the pair's release latch and `net
	// release $pair.a` can name the pair in its rejection.
	// Installed by NewNetnsVethPair; never rewritten.
	Pair *NetnsVethPair
}

// NewNetnsVethPair builds the pair from two endpoint identities
// and installs the back-pointers, so a constructed pair can never
// carry an endpoint that does not point back at it.
func NewNetnsVethPair(a, b NetnsVethEndpoint) *NetnsVethPair {
	pair := &NetnsVethPair{A: &a, B: &b}
	pair.A.Pair = pair
	pair.B.Pair = pair
	return pair
}

// MarkReleased sets the lifecycle latch and reports whether it was
// already set. The first caller observes false and proceeds with
// the teardown; subsequent callers observe true and short-circuit.
func (p *NetnsVethPair) MarkReleased() (wasReleased bool) {
	p.Mu.Lock()
	defer p.Mu.Unlock()
	if p.Released {
		return true
	}
	p.Released = true
	return false
}

// IsReleased reports whether the handle has been consumed.
func (p *NetnsVethPair) IsReleased() bool {
	p.Mu.Lock()
	defer p.Mu.Unlock()
	return p.Released
}

// valueFromNetnsVethEndpoint wraps e as a Value with
// semantics.OriginNetnsVethEndpoint. The path machinery resolves
// $pair.a.ns / $pair.a.addr / ... through the JSON-tree mirror;
// the underlying *NetnsVethEndpoint is recoverable via
// Value.Origin() so net exec / net start reach the namespace name
// and the pair's release latch directly.
func valueFromNetnsVethEndpoint(e *NetnsVethEndpoint) Value {
	mirror := map[string]any{
		"ns":      e.Ns,
		"link":    e.Link,
		"addr":    e.Addr,
		"ifindex": json.Number(strconv.FormatUint(uint64(e.Ifindex), 10)),
		"nsid":    json.Number(strconv.FormatUint(e.Nsid, 10)),
	}
	return Value{v: mirror, origin: e, kind: semantics.OriginNetnsVethEndpoint}
}

// ValueFromNetnsVethPair wraps p as a Value with
// semantics.OriginNetnsVethPair. The pair is a record of the two
// endpoint capabilities: ValueFromRecord's side table preserves
// the typed endpoint Values, so $pair.a recovers the endpoint
// capability (origin *NetnsVethEndpoint) rather than an origin-
// less object, and $pair.a.ns walks the endpoint's mirror as
// usual. The underlying *NetnsVethPair is recoverable via
// Value.Origin() so net release reaches the lifecycle latch.
func ValueFromNetnsVethPair(p *NetnsVethPair) Value {
	return ValueFromRecord(map[string]Value{
		"a": valueFromNetnsVethEndpoint(p.A),
		"b": valueFromNetnsVethEndpoint(p.B),
	}).withOrigin(p, semantics.OriginNetnsVethPair)
}

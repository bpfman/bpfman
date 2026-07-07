package dispatcher

import "github.com/bpfman/bpfman/kernel"

// Key uniquely identifies a dispatcher by its type, network namespace,
// and interface index.
type Key struct {
	// Type is the dispatcher type (xdp, tc-ingress, tc-egress).
	Type DispatcherType `json:"type"`

	// Nsid is the network namespace inode number.
	Nsid uint64 `json:"nsid"`

	// Ifindex is the network interface index.
	Ifindex uint32 `json:"ifindex"`
}

// NewKey constructs a dispatcher key from already parsed domain
// values.
func NewKey(typ DispatcherType, nsid uint64, ifindex uint32) Key {
	return Key{
		Type:    typ,
		Nsid:    nsid,
		Ifindex: ifindex,
	}
}

// KeyFilter selects dispatcher keys by any combination of type,
// nsid, and ifindex. Zero values mean unfiltered: nsid 0 and
// ifindex 0 never identify a real dispatcher (the kernel allocates
// namespace inodes and interface indices from 1), matching the zero
// DispatcherType sentinel.
type KeyFilter struct {
	// Type filters by dispatcher type; the zero DispatcherType matches
	// any type.
	Type DispatcherType

	// Nsid filters by network namespace inode number; 0 matches any
	// namespace.
	Nsid uint64

	// Ifindex filters by network interface index; 0 matches any
	// interface.
	Ifindex uint32
}

// Matches reports whether the key passes every set filter field.
func (f KeyFilter) Matches(k Key) bool {
	if f.Type != (DispatcherType{}) && k.Type != f.Type {
		return false
	}
	if f.Nsid != 0 && k.Nsid != f.Nsid {
		return false
	}
	if f.Ifindex != 0 && k.Ifindex != f.Ifindex {
		return false
	}
	return true
}

// State represents the persistent state of a dispatcher.
// A dispatcher manages multi-program chaining for XDP or TC attachments.
type State struct {
	// Type is the dispatcher type (xdp, tc-ingress, tc-egress).
	Type DispatcherType `json:"type"`

	// Nsid is the network namespace inode number.
	// This uniquely identifies the network namespace.
	Nsid uint64 `json:"nsid"`

	// Ifindex is the network interface index.
	Ifindex uint32 `json:"ifindex"`

	// Revision is the current dispatcher revision.
	// Incremented on each atomic update, wraps at MaxUint32.
	Revision uint32 `json:"revision"`

	// ProgramID is the kernel program ID of the dispatcher.
	ProgramID kernel.ProgramID `json:"program_id"`

	// KernelLinkID is the kernel link ID (XDP link for XDP dispatchers).
	// Zero for TC dispatchers which use legacy netlink instead of BPF links.
	KernelLinkID kernel.LinkID `json:"kernel_link_id"`

	// Priority is the tc filter priority.
	// Only set for TC dispatchers (legacy netlink). Zero for XDP.
	Priority uint16 `json:"priority"`
}

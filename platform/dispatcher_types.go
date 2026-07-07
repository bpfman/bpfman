package platform

import (
	bpfman "github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/kernel"
)

// DispatcherMember describes an extension program attached to a
// dispatcher slot, as read back from the store. Each member occupies a
// unique position in the dispatcher's slot array.
type DispatcherMember struct {
	// ProgramID is the kernel program ID of the extension (freplace)
	// program occupying this slot.
	ProgramID kernel.ProgramID `json:"program_id"`

	// ProgramName is the name of the extension program.
	ProgramName string `json:"program_name"`

	// ProgPinPath is the bpffs path at which the extension program is
	// pinned.
	ProgPinPath bpfman.ProgPinPath `json:"prog_pin_path"`

	// LinkID is the bpfman-allocated identifier for this member's
	// extension link record in the store.
	LinkID bpfman.LinkID `json:"link_id"`

	// KernelLinkID is the kernel link ID of the freplace extension
	// link. It is nil until the extension has been attached in the
	// kernel.
	KernelLinkID *kernel.LinkID `json:"kernel_link_id"`

	// LinkPinPath is the bpffs path at which the freplace extension
	// link is pinned.
	LinkPinPath bpfman.LinkPath `json:"link_pin_path"`

	// Position is the 0-based slot index within the dispatcher chain,
	// assigned in priority order.
	Position int `json:"position"`

	// Priority is the caller-requested ordering priority. Lower values
	// sort earlier in the chain (priority 0 first); negatives are
	// rejected at spec construction.
	Priority int `json:"priority"`

	// ProceedOn is the dispatcher-ABI bitmask of kernel return codes
	// on which the dispatcher proceeds to the next slot. This is the
	// encoded mask, not a []int32 list of codes; see
	// dispatcher.ProceedOnMask and dispatcher.ProceedOnActions for the
	// encoding.
	ProceedOn uint32 `json:"proceed_on"`

	// Ifname is the network interface name the dispatcher attaches to.
	Ifname string `json:"ifname"`

	// Metadata is the caller-supplied key/value metadata recorded with
	// the link.
	Metadata map[string]string `json:"metadata"`
}

// DispatcherMemberSpec describes an extension program that should be
// persisted as part of a dispatcher snapshot but does not yet have a bpfman
// LinkID allocated by the store.
type DispatcherMemberSpec struct {
	// ExistingLinkID, when non-nil, asks the store to reuse this
	// already-allocated bpfman link ID for the member rather than
	// allocating a fresh one. nil requests a new allocation. It is set
	// to preserve a surviving member's link ID across a snapshot
	// rebuild.
	ExistingLinkID *bpfman.LinkID `json:"existing_link_id,omitempty"`

	// ProgramID is the kernel program ID of the extension (freplace)
	// program for this slot.
	ProgramID kernel.ProgramID `json:"program_id"`

	// ProgramName is the name of the extension program.
	ProgramName string `json:"program_name"`

	// ProgPinPath is the bpffs path at which the extension program is
	// pinned.
	ProgPinPath bpfman.ProgPinPath `json:"prog_pin_path"`

	// KernelLinkID is the kernel link ID of the freplace extension
	// link. It is nil until the extension has been attached in the
	// kernel.
	KernelLinkID *kernel.LinkID `json:"kernel_link_id"`

	// LinkPinPath is the bpffs path at which the freplace extension
	// link is pinned.
	LinkPinPath bpfman.LinkPath `json:"link_pin_path"`

	// Position is the 0-based slot index within the dispatcher chain,
	// assigned in priority order.
	Position int `json:"position"`

	// Priority is the caller-requested ordering priority. Lower values
	// sort earlier in the chain (priority 0 first); negatives are
	// rejected at spec construction.
	Priority int `json:"priority"`

	// ProceedOn is the dispatcher-ABI bitmask of kernel return codes
	// on which the dispatcher proceeds to the next slot. This is the
	// encoded mask, not a []int32 list of codes; see
	// dispatcher.ProceedOnMask and dispatcher.ProceedOnActions for the
	// encoding.
	ProceedOn uint32 `json:"proceed_on"`

	// Ifname is the network interface name the dispatcher attaches to.
	Ifname string `json:"ifname"`

	// Metadata is the caller-supplied key/value metadata to record
	// with the link.
	Metadata map[string]string `json:"metadata"`
}

// DispatcherRuntime holds the kernel-assigned identifiers for the
// dispatcher program itself. KernelLinkID is nil for TC dispatchers which
// use netlink filters rather than BPF links. FilterPriority and
// FilterHandle are the TC filter's priority and exact kernel-assigned
// handle, both nil for XDP. The handle is recorded at create (echoed
// back by the kernel) so detach deletes bpfman's own filter rather than
// rediscovering one by priority alone.
type DispatcherRuntime struct {
	// ProgramID is the kernel program ID of the dispatcher program
	// itself.
	ProgramID kernel.ProgramID `json:"program_id"`

	// KernelLinkID is the kernel link ID of the dispatcher's XDP link.
	// It is nil for TC dispatchers, which use netlink filters rather
	// than BPF links.
	KernelLinkID *kernel.LinkID `json:"kernel_link_id,omitempty"`

	// FilterPriority is the TC filter's priority. It is nil for XDP.
	FilterPriority *uint16 `json:"filter_priority,omitempty"`

	// FilterHandle is the exact kernel-assigned TC filter handle,
	// recorded at create (echoed back by the kernel) so detach deletes
	// bpfman's own filter rather than rediscovering one by priority
	// alone. It is nil for XDP.
	FilterHandle *uint32 `json:"filter_handle,omitempty"`

	// NetnsPath is the path to the network namespace the dispatcher is
	// attached in (for example /proc/<pid>/ns/net). An empty path
	// means the daemon's own namespace.
	NetnsPath string `json:"netns_path"`
}

// DispatcherSnapshot is a complete point-in-time view of a dispatcher
// and all its extension members. The snapshot is the unit of
// replacement: callers build a complete snapshot and pass it to
// ReplaceDispatcherSnapshot, which atomically replaces all persisted
// state for the dispatcher's attach point.
type DispatcherSnapshot struct {
	// Key is the attach-point key (type, namespace, interface)
	// identifying the dispatcher.
	Key dispatcher.Key `json:"key"`

	// Revision is the dispatcher's revision counter, incremented on
	// each rebuild. It distinguishes successive dispatcher programs so
	// the old and new pins coexist during the atomic swap; pin paths
	// are derived from it.
	Revision uint32 `json:"revision"`

	// Runtime holds the kernel-assigned identifiers for the dispatcher
	// program (and, for TC, its filter handle).
	Runtime DispatcherRuntime `json:"runtime"`

	// Members are the extension programs currently attached to the
	// dispatcher, in slot order (ascending Position).
	Members []DispatcherMember `json:"members"`
}

// DispatcherSnapshotSpec is the requested replacement state for a dispatcher.
// Members may refer to existing bpfman link handles or ask the store to
// allocate new ones.
type DispatcherSnapshotSpec struct {
	// Key is the attach-point key (type, namespace, interface)
	// identifying the dispatcher to replace.
	Key dispatcher.Key `json:"key"`

	// Revision is the dispatcher's revision counter for the requested
	// state (see DispatcherSnapshot.Revision).
	Revision uint32 `json:"revision"`

	// Runtime holds the kernel-assigned identifiers for the dispatcher
	// program (and, for TC, its filter handle).
	Runtime DispatcherRuntime `json:"runtime"`

	// Members are the requested extension members, each either reusing
	// an existing bpfman link handle (ExistingLinkID set) or asking the
	// store to allocate a new one.
	Members []DispatcherMemberSpec `json:"members"`
}

// DispatcherSummary is a lightweight view of a dispatcher suitable
// for listing. It carries the member count rather than the full
// member list, avoiding the cost of joining detail tables when only
// aggregate information is needed.
type DispatcherSummary struct {
	// Key is the attach-point key (type, namespace, interface)
	// identifying the dispatcher.
	Key dispatcher.Key `json:"key"`

	// Revision is the dispatcher's revision counter (see
	// DispatcherSnapshot.Revision).
	Revision uint32 `json:"revision"`

	// Runtime holds the kernel-assigned identifiers for the dispatcher
	// program (and, for TC, its filter handle).
	Runtime DispatcherRuntime `json:"runtime"`

	// MemberCount is the number of extension members attached to the
	// dispatcher.
	MemberCount int `json:"member_count"`
}

// DispatcherListResult wraps dispatcher list output for consistent
// JSON structure, mirroring LinkListResult.
type DispatcherListResult struct {
	// Dispatchers is the list of dispatcher summaries, one per
	// dispatcher.
	Dispatchers []DispatcherSummary `json:"dispatchers"`
}

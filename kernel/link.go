package kernel

// Link represents a BPF link in the kernel.
// All fields from cilium/ebpf's link.Info are captured here, including
// type-specific information extracted from the various *Info subtypes.
//
// Fields are emitted explicitly; consumers use LinkType as the
// discriminator for which type-specific fields are meaningful. Zero on
// a field that does not apply to this link's LinkType is not a
// meaningful observation, and zero on a field that does apply is.
type Link struct {
	// ID is the kernel-assigned bpf_link ID, unique while the link is
	// alive.
	ID LinkID `json:"id"`

	// ProgramID is the kernel ID of the program this link attaches.
	ProgramID ProgramID `json:"program_id"`

	// LinkType is the kernel link-type name (for example "xdp", "tcx",
	// "tracing", "perf_event", "kprobe_multi", "netfilter"), or
	// "unknown(N)" for a type this package does not name. It discriminates
	// which type-specific fields below are meaningful.
	LinkType string `json:"link_type"`

	// AttachType is the kernel attach-type enum rendered as a decimal
	// string, set for tracing, cgroup, netns, tcx, and netkit links and
	// empty otherwise.
	AttachType string `json:"attach_type"`

	// Ifindex is the network interface index, set for xdp, tcx, and netkit
	// links.
	Ifindex uint32 `json:"ifindex"`

	// TargetObjID is the kernel ID of the object a tracing link attaches
	// to.
	TargetObjID uint32 `json:"target_obj_id"`

	// TargetBTFId is the BTF type ID of the attach target for a tracing
	// link.
	TargetBTFId uint32 `json:"target_btf_id"`

	// CgroupID is the ID of the cgroup a cgroup link attaches to.
	CgroupID uint64 `json:"cgroup_id"`

	// NetnsIno is the inode number of the network namespace a netns link
	// attaches to.
	NetnsIno uint32 `json:"netns_ino"`

	// NetfilterPf is the netfilter protocol family (for example PF_INET)
	// the hook is registered for.
	NetfilterPf uint32 `json:"netfilter_pf"`

	// NetfilterHooknum is the netfilter hook point within the protocol
	// family.
	NetfilterHooknum uint32 `json:"netfilter_hooknum"`

	// NetfilterPriority orders the hook relative to other netfilter hooks;
	// lower values run first.
	NetfilterPriority int32 `json:"netfilter_priority"`

	// NetfilterFlags is the netfilter link flags bitmask.
	NetfilterFlags uint32 `json:"netfilter_flags"`

	// KprobeMultiCount is the number of addresses a kprobe.multi link
	// attaches to.
	KprobeMultiCount uint32 `json:"kprobe_multi_count"`

	// KprobeMultiFlags is the kprobe.multi flags bitmask (for example the
	// return-probe flag).
	KprobeMultiFlags uint32 `json:"kprobe_multi_flags"`

	// KprobeMultiMissed is the number of times the kprobe.multi probe was
	// skipped (for example due to recursion).
	KprobeMultiMissed uint64 `json:"kprobe_multi_missed"`

	// KprobeAddress is the kernel address a single perf-event kprobe is
	// attached to.
	KprobeAddress uint64 `json:"kprobe_address"`

	// KprobeMissed is the number of times the single perf-event kprobe was
	// skipped (for example due to recursion).
	KprobeMissed uint64 `json:"kprobe_missed"`
}

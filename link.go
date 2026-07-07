package bpfman

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/bpfman/bpfman/kernel"
)

// LinkID uniquely identifies a bpfman-managed attachment.
type LinkID uint64

// TCXAttachOrder specifies where to insert a TCX program in the chain.
// Programs are ordered by priority, with lower priority values running first.
// This type maps to cilium/ebpf's link.Anchor for kernel attachment.
type TCXAttachOrder struct {
	// First attaches at the head of the chain (runs before all others).
	First bool `json:"first"`

	// BeforeProgID attaches before the program with this kernel ID. Zero means not set.
	BeforeProgID kernel.ProgramID `json:"before_prog_id"`

	// AfterProgID attaches after the program with this kernel ID. Zero means not set.
	AfterProgID kernel.ProgramID `json:"after_prog_id"`
}

// TCXAttachFirst returns an order that attaches at the head of the chain.
func TCXAttachFirst() TCXAttachOrder {
	return TCXAttachOrder{First: true}
}

// TCXAttachBefore returns an order that attaches before the given program.
func TCXAttachBefore(progID kernel.ProgramID) TCXAttachOrder {
	return TCXAttachOrder{BeforeProgID: progID}
}

// TCXAttachAfter returns an order that attaches after the given program.
func TCXAttachAfter(progID kernel.ProgramID) TCXAttachOrder {
	return TCXAttachOrder{AfterProgID: progID}
}

// LinkPath represents a pinned link path within a bpffs.
// This is a newtype to prevent accidentally passing arbitrary strings
// where a validated link pin path is expected.
type LinkPath string

// String returns the path as a string.
func (p LinkPath) String() string { return string(p) }

// ProgPinPath represents a pinned program path within a bpffs. The
// newtype prevents feeding a path of another domain (a link pin, a
// map pin, an arbitrary string) to a primitive that expects a
// program pin -- in particular, ebpf.LoadPinnedProgram and the kernel
// adapter's program-side actions.
type ProgPinPath string

// String returns the path as a string.
func (p ProgPinPath) String() string { return string(p) }

// MapPinPath represents a pinned map path within a bpffs. The newtype
// prevents feeding a path of another domain to map-side primitives
// (ebpf.LoadPinnedMap, shared-map pin tracking).
type MapPinPath string

// String returns the path as a string.
func (p MapPinPath) String() string { return string(p) }

// MapDir is a per-program maps directory within a bpffs. The newtype
// distinguishes it from DispatcherRevDir and other bpffs directory
// domains so that directory-removal actions cannot be fed a path of
// the wrong domain (e.g. RemoveMapDir on a dispatcher dir).
type MapDir string

// String returns the path as a string.
func (d MapDir) String() string { return string(d) }

// DispatcherRevDir is a per-revision dispatcher directory within a
// bpffs (containing the dispatcher program pin and extension link
// pins for one revision). The newtype prevents confusion with the
// per-program MapDir.
type DispatcherRevDir string

// String returns the path as a string.
func (d DispatcherRevDir) String() string { return string(d) }

// NewLinkPath wraps a string-derived path (LinkPath or plain string)
// into a *LinkPath, returning nil if empty. The generic constraint
// lets callers pass the already-typed bpfman.LinkPath without a cast,
// while keeping the SQLite-boundary case ergonomic where rows arrive
// as plain strings.
func NewLinkPath[P ~string](s P) *LinkPath {
	if s == "" {
		return nil
	}
	p := LinkPath(s)
	return &p
}

// LinkDetails is a sealed interface for type-specific link details.
// Use type assertion or type switch to access the concrete type.
// The interface is sealed via the unexported linkDetails() method -
// only types in this package can implement it.
type LinkDetails interface {
	// linkDetails is an unexported marker; only types in this package
	// can implement LinkDetails.
	linkDetails()

	// Kind returns the LinkKind for this detail type.
	Kind() LinkKind
}

// TracepointDetails contains fields specific to tracepoint attachments.
type TracepointDetails struct {
	// Group is the tracepoint group (the directory under events/).
	Group string `json:"group"`

	// Name is the tracepoint name within the group.
	Name string `json:"name"`
}

func (TracepointDetails) linkDetails() {}

// Kind returns LinkKindTracepoint.
func (TracepointDetails) Kind() LinkKind { return LinkKindTracepoint }

// KprobeDetails contains fields specific to kprobe/kretprobe attachments.
type KprobeDetails struct {
	// FnName is the kernel function the probe attaches to.
	FnName string `json:"fn_name"`

	// Offset is the byte offset into the function at which to attach.
	Offset uint64 `json:"offset"`

	// Retprobe selects the return variant (kretprobe) when set.
	Retprobe bool `json:"retprobe"`
}

func (KprobeDetails) linkDetails() {}

// Kind returns LinkKindKretprobe when Retprobe is set, otherwise LinkKindKprobe.
func (d KprobeDetails) Kind() LinkKind {
	if d.Retprobe {
		return LinkKindKretprobe
	}
	return LinkKindKprobe
}

// UprobeDetails contains fields specific to uprobe/uretprobe attachments.
// FnName and Offset form two attach modes: a non-empty FnName attaches by
// symbol; an empty FnName with a non-zero Offset attaches at that offset
// within Target. PID 0 means attach system-wide; a non-zero PID restricts
// the probe to that process. ContainerPid 0 means not container-scoped.
type UprobeDetails struct {
	// Target is the executable or library the probe attaches to.
	Target string `json:"target"`

	// FnName is the symbol to attach by; empty selects offset-based attachment.
	FnName string `json:"fn_name"`

	// Offset is the byte offset within Target to attach at when FnName is empty.
	Offset uint64 `json:"offset"`

	// PID restricts the probe to that process; 0 means attach system-wide.
	PID int32 `json:"pid"`

	// Retprobe selects the return variant (uretprobe) when set.
	Retprobe bool `json:"retprobe"`

	// ContainerPid scopes the probe to a container; 0 means not container-scoped.
	ContainerPid int32 `json:"container_pid"`
}

func (UprobeDetails) linkDetails() {}

// Kind returns LinkKindUretprobe when Retprobe is set, otherwise LinkKindUprobe.
func (d UprobeDetails) Kind() LinkKind {
	if d.Retprobe {
		return LinkKindUretprobe
	}
	return LinkKindUprobe
}

// FentryDetails contains fields specific to fentry attachments.
type FentryDetails struct {
	// FnName is the kernel function the fentry program traces.
	FnName string `json:"fn_name"`
}

func (FentryDetails) linkDetails() {}

// Kind returns LinkKindFentry.
func (FentryDetails) Kind() LinkKind { return LinkKindFentry }

// FexitDetails contains fields specific to fexit attachments.
type FexitDetails struct {
	// FnName is the kernel function the fexit program traces.
	FnName string `json:"fn_name"`
}

func (FexitDetails) linkDetails() {}

// Kind returns LinkKindFexit.
func (FexitDetails) Kind() LinkKind { return LinkKindFexit }

// XDPDetails contains fields specific to XDP attachments.
// Netns empty means the root network namespace.
type XDPDetails struct {
	// Interface is the name of the network interface the program attaches to.
	Interface string `json:"interface"`

	// Ifindex is the index of that network interface.
	Ifindex uint32 `json:"ifindex"`

	// Priority is the attach priority used to order the program within the
	// dispatcher chain.
	Priority int32 `json:"priority"`

	// Position is the 0-based slot index within the dispatcher chain.
	Position int32 `json:"position"`

	// ProceedOn is the set of kernel return codes on which the dispatcher
	// proceeds to the next program.
	ProceedOn []int32 `json:"proceed_on"`

	// Netns is the network namespace path; empty means the root network namespace.
	Netns string `json:"netns"`

	// Nsid is the network namespace ID the dispatcher runs in.
	Nsid uint64 `json:"nsid"`

	// DispatcherID is the kernel ID of the owning dispatcher program.
	DispatcherID kernel.ProgramID `json:"dispatcher_id"`

	// Revision is the dispatcher revision this link belongs to.
	Revision uint32 `json:"revision"`
}

func (XDPDetails) linkDetails() {}

// Kind returns LinkKindXDP.
func (XDPDetails) Kind() LinkKind { return LinkKindXDP }

// TCDirection represents the direction of TC traffic (ingress or egress).
//
// It is a plain string enum: a value carries no proof of validity, so
// validity is enforced at the boundaries. ParseTCDirection is the strict
// parser for external input (case-insensitive), and Valid reports
// membership of the known set. JSON decoding is permissive, trusting
// bpfman's own stored records.
type TCDirection string

// The known TC attach directions.
const (
	TCDirectionIngress TCDirection = "ingress"
	TCDirectionEgress  TCDirection = "egress"
)

// Valid reports whether d is one of the known directions. Strict
// membership: the zero value and unrecognised values are not valid.
func (d TCDirection) Valid() bool {
	switch d {
	case TCDirectionIngress, TCDirectionEgress:
		return true
	default:
		return false
	}
}

// ParseTCDirection parses a string into a TCDirection.
// Returns an error if the string is not "ingress" or "egress".
func ParseTCDirection(s string) (TCDirection, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "ingress":
		return TCDirectionIngress, nil
	case "egress":
		return TCDirectionEgress, nil
	default:
		return "", fmt.Errorf("invalid TC direction %q: must be 'ingress' or 'egress'", s)
	}
}

// String returns the direction as a string.
func (d TCDirection) String() string { return string(d) }

// TCDetails contains fields specific to TC attachments.
// Netns empty means the root network namespace.
type TCDetails struct {
	// Interface is the name of the network interface the program attaches to.
	Interface string `json:"interface"`

	// Ifindex is the index of that network interface.
	Ifindex uint32 `json:"ifindex"`

	// Direction is the traffic direction (ingress or egress).
	Direction TCDirection `json:"direction"`

	// Priority is the attach priority used to order the program within the
	// dispatcher chain.
	Priority int32 `json:"priority"`

	// Position is the 0-based slot index within the dispatcher chain.
	Position int32 `json:"position"`

	// ProceedOn is the set of kernel return codes on which the dispatcher
	// proceeds to the next program.
	ProceedOn []int32 `json:"proceed_on"`

	// Netns is the network namespace path; empty means the root network namespace.
	Netns string `json:"netns"`

	// Nsid is the network namespace ID the dispatcher runs in.
	Nsid uint64 `json:"nsid"`

	// DispatcherID is the kernel ID of the owning dispatcher program.
	DispatcherID kernel.ProgramID `json:"dispatcher_id"`

	// Revision is the dispatcher revision this link belongs to.
	Revision uint32 `json:"revision"`
}

func (TCDetails) linkDetails() {}

// Kind returns LinkKindTC.
func (TCDetails) Kind() LinkKind { return LinkKindTC }

// TCXDetails contains fields specific to TCX attachments.
// Netns empty means the root network namespace.
type TCXDetails struct {
	// Interface is the name of the network interface the program attaches to.
	Interface string `json:"interface"`

	// Ifindex is the index of that network interface.
	Ifindex uint32 `json:"ifindex"`

	// Direction is the traffic direction (ingress or egress).
	Direction TCDirection `json:"direction"`

	// Priority is the attach priority used to order the program in the
	// kernel's TCX chain.
	Priority int32 `json:"priority"`

	// Position is the 0-based position in the kernel's TCX program chain.
	Position int32 `json:"position"`

	// Netns is the network namespace path; empty means the root network namespace.
	Netns string `json:"netns"`

	// Nsid is the network namespace ID the program runs in.
	Nsid uint64 `json:"nsid"`
}

func (TCXDetails) linkDetails() {}

// Kind returns LinkKindTCX.
func (TCXDetails) Kind() LinkKind { return LinkKindTCX }

// TCXLinkInfo combines link summary with TCX-specific details.
// Used for computing attach order based on priority, and for
// naming the offending link when a duplicate attach is rejected.
type TCXLinkInfo struct {
	// LinkID is the bpfman management handle for the link.
	LinkID LinkID `json:"link_id"`

	// KernelLinkID is the kernel bpf_link ID for the attachment.
	KernelLinkID kernel.LinkID `json:"kernel_link_id"`

	// KernelProgramID is the kernel ID of the attached program.
	KernelProgramID kernel.ProgramID `json:"kernel_program_id"`

	// Priority is the attach priority, used to compute attach order.
	Priority int32 `json:"priority"`
}

// LinkKind is bpfman's discriminator for link types. It is distinct
// from kernel.Link.LinkType, which is kernel-reported.
//
// It is a plain string enum: a value carries no proof of validity, so
// validity is enforced at the boundaries. ParseLinkKind is the strict
// parser for external input; JSON decoding is permissive, trusting
// bpfman's own stored records.
type LinkKind string

// The known link kinds.
const (
	LinkKindTracepoint LinkKind = "tracepoint"
	LinkKindKprobe     LinkKind = "kprobe"
	LinkKindKretprobe  LinkKind = "kretprobe"
	LinkKindUprobe     LinkKind = "uprobe"
	LinkKindUretprobe  LinkKind = "uretprobe"
	LinkKindFentry     LinkKind = "fentry"
	LinkKindFexit      LinkKind = "fexit"
	LinkKindXDP        LinkKind = "xdp"
	LinkKindTC         LinkKind = "tc"
	LinkKindTCX        LinkKind = "tcx"
)

// allLinkKinds is the canonical list of valid link kinds.
var allLinkKinds = []LinkKind{
	LinkKindTracepoint,
	LinkKindKprobe,
	LinkKindKretprobe,
	LinkKindUprobe,
	LinkKindUretprobe,
	LinkKindFentry,
	LinkKindFexit,
	LinkKindXDP,
	LinkKindTC,
	LinkKindTCX,
}

// AllLinkKinds returns all valid link kinds.
func AllLinkKinds() []LinkKind {
	return allLinkKinds
}

// LinkKindNames returns all valid link kind names as strings.
func LinkKindNames() []string {
	names := make([]string, len(allLinkKinds))
	for i, k := range allLinkKinds {
		names[i] = k.String()
	}
	return names
}

// String returns the link kind as a string.
func (k LinkKind) String() string { return string(k) }

// Valid reports whether k is one of the known link kinds. Strict
// membership backed by ParseLinkKind: the zero value and unrecognised
// values are not valid.
func (k LinkKind) Valid() bool {
	_, err := ParseLinkKind(string(k))
	return err == nil
}

// ParseLinkKind parses a string into a LinkKind.
// Returns the LinkKind and a nil error if valid, or the zero value and
// an error if unrecognised.
func ParseLinkKind(s string) (LinkKind, error) {
	switch s {
	case "tracepoint":
		return LinkKindTracepoint, nil
	case "kprobe":
		return LinkKindKprobe, nil
	case "kretprobe":
		return LinkKindKretprobe, nil
	case "uprobe":
		return LinkKindUprobe, nil
	case "uretprobe":
		return LinkKindUretprobe, nil
	case "fentry":
		return LinkKindFentry, nil
	case "fexit":
		return LinkKindFexit, nil
	case "xdp":
		return LinkKindXDP, nil
	case "tc":
		return LinkKindTC, nil
	case "tcx":
		return LinkKindTCX, nil
	default:
		return "", fmt.Errorf("unknown link kind %q", s)
	}
}

// LinkSpec is the requested managed-link row before the store has allocated a
// bpfman LinkID.
type LinkSpec struct {
	// ProgramID is the kernel ID of the program this link attaches to.
	ProgramID kernel.ProgramID `json:"program_id"`

	// KernelLinkID is the captured kernel bpf_link ID, nil when none was observed.
	KernelLinkID *kernel.LinkID `json:"kernel_link_id"`

	// Kind is the link kind; it must equal Details.Kind() when Details is set.
	Kind LinkKind `json:"kind"`

	// PinPath is the bpffs link pin, nil for an ephemeral link.
	PinPath *LinkPath `json:"pin_path"`

	// Details holds the type-specific link details.
	Details LinkDetails `json:"details"`

	// Metadata holds user key/value labels for selection; nil means none.
	Metadata map[string]string `json:"metadata"`
}

// LinkRecord is the stored record of an attached link. ID is the bpfman-owned
// management handle. KernelLinkID is the captured kernel bpf_link ID, if the
// attach path observed one.
type LinkRecord struct {
	// ID is the bpfman-owned management handle for this link.
	ID LinkID `json:"id"`

	// ProgramID is the kernel ID of the program this link attaches to.
	ProgramID kernel.ProgramID `json:"program_id"`

	// KernelLinkID is the captured kernel bpf_link ID, nil when the attach
	// path observed none.
	KernelLinkID *kernel.LinkID `json:"kernel_link_id"`

	// Kind is the link kind. When Details is non-nil it must equal
	// Details.Kind(); constructors enforce this.
	Kind LinkKind `json:"kind"`

	// PinPath nil distinguishes an ephemeral link from one with a pin. Always
	// emitted as JSON null in the ephemeral case so the consumer schema is stable.
	PinPath *LinkPath `json:"pin_path"`

	// Details nil means "no per-kind detail available"; always emitted as JSON
	// null in that case.
	Details LinkDetails `json:"details"`

	// CreatedAt is when the link record was created.
	CreatedAt time.Time `json:"created_at"`

	// Metadata holds user-supplied key/value labels attached at attach time,
	// used for selection by `link list`. Empty (or nil) means none.
	Metadata map[string]string `json:"metadata"`
}

// newLinkDetails returns a fresh pointer to the concrete
// LinkDetails implementer associated with kind, or nil if kind is
// unrecognised. Used by LinkRecord.UnmarshalJSON to pick the
// json.Unmarshal target for the polymorphic Details field, and
// indirectly by external tooling reflecting over each kind's
// JSON schema (the bpfman-shell static checker derives its
// per-kind details Shape registry from LinkAttachKindDetailsType,
// which shares this dispatch).
//
// kprobe / kretprobe share KprobeDetails and uprobe / uretprobe
// share UprobeDetails; the Retprobe field inside each struct
// distinguishes the paired LinkKinds.
func newLinkDetails(kind LinkKind) LinkDetails {
	switch kind {
	case LinkKindXDP:
		return &XDPDetails{}
	case LinkKindTC:
		return &TCDetails{}
	case LinkKindTCX:
		return &TCXDetails{}
	case LinkKindTracepoint:
		return &TracepointDetails{}
	case LinkKindKprobe, LinkKindKretprobe:
		return &KprobeDetails{}
	case LinkKindUprobe, LinkKindUretprobe:
		return &UprobeDetails{}
	case LinkKindFentry:
		return &FentryDetails{}
	case LinkKindFexit:
		return &FexitDetails{}
	}
	return nil
}

// LinkAttachKinds returns the attach subcommand keywords the CLI
// exposes (xdp, tc, tcx, tracepoint, kprobe, uprobe, fentry,
// fexit). Eight entries, not ten -- kprobe / kretprobe and uprobe
// / uretprobe collapse into the kprobe and uprobe attach
// subcommands respectively, with --retprobe selecting the paired
// LinkKind at attach time.
func LinkAttachKinds() []string {
	return []string{
		"xdp",
		"tc",
		"tcx",
		"tracepoint",
		"kprobe",
		"uprobe",
		"fentry",
		"fexit",
	}
}

// LinkAttachKindDetailsType returns the reflect.Type of the
// LinkDetails implementer for the named attach subcommand, or
// nil if attachKind is not in LinkAttachKinds. External tooling
// (notably the bpfman-shell static checker) reflects over the
// returned type to derive per-subcommand JSON schemas without
// duplicating the dispatch table.
func LinkAttachKindDetailsType(attachKind string) reflect.Type {
	switch attachKind {
	case "xdp":
		return reflect.TypeFor[XDPDetails]()
	case "tc":
		return reflect.TypeFor[TCDetails]()
	case "tcx":
		return reflect.TypeFor[TCXDetails]()
	case "tracepoint":
		return reflect.TypeFor[TracepointDetails]()
	case "kprobe":
		return reflect.TypeFor[KprobeDetails]()
	case "uprobe":
		return reflect.TypeFor[UprobeDetails]()
	case "fentry":
		return reflect.TypeFor[FentryDetails]()
	case "fexit":
		return reflect.TypeFor[FexitDetails]()
	}
	return nil
}

// UnmarshalJSON decodes a LinkRecord from JSON. The Details field
// is a sealed interface (LinkDetails), so encoding/json's default
// mechanism cannot pick a concrete type for it. This unmarshaler
// reads Kind first and then dispatches to newLinkDetails for the
// right json.Unmarshal target; every other field uses the
// default JSON mapping. Details is stored as a value type rather
// than as a pointer to match the rest of the package's
// convention (existing call sites type-switch on bpfman.TCDetails,
// not *bpfman.TCDetails).
func (r *LinkRecord) UnmarshalJSON(data []byte) error {
	type alias struct {
		ID           LinkID            `json:"id"`
		ProgramID    kernel.ProgramID  `json:"program_id"`
		KernelLinkID *kernel.LinkID    `json:"kernel_link_id"`
		Kind         LinkKind          `json:"kind"`
		PinPath      *LinkPath         `json:"pin_path"`
		Details      json.RawMessage   `json:"details"`
		CreatedAt    time.Time         `json:"created_at"`
		Metadata     map[string]string `json:"metadata"`
	}
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}

	r.ID = a.ID
	r.ProgramID = a.ProgramID
	r.KernelLinkID = a.KernelLinkID
	r.Kind = a.Kind
	r.PinPath = a.PinPath
	r.CreatedAt = a.CreatedAt
	// Canonical in-memory form is nil for "no metadata". There is
	// deliberately no custom LinkRecord marshaler: a json.Marshaler would
	// unseal this shell-visible record in the bpfman-shell shape deriver and
	// weaken static field checking, so absent metadata encodes as
	// "metadata": null under standard encoding. An empty object decodes back
	// to nil here so a metadata-less record round-trips identically.
	r.Metadata = a.Metadata
	if len(r.Metadata) == 0 {
		r.Metadata = nil
	}
	r.Details = nil
	if len(a.Details) == 0 || bytes.Equal(a.Details, []byte("null")) {
		return nil
	}
	target := newLinkDetails(a.Kind)
	if target == nil {
		return fmt.Errorf("LinkRecord.UnmarshalJSON: no LinkDetails type registered for kind %q", a.Kind)
	}

	if err := json.Unmarshal(a.Details, target); err != nil {
		return fmt.Errorf("LinkRecord.UnmarshalJSON: decode %s details: %w", a.Kind, err)
	}

	r.Details = reflect.ValueOf(target).Elem().Interface().(LinkDetails)
	return nil
}

// LinkListResult wraps link list output for consistent JSON structure.
type LinkListResult struct {
	// Links is the list of link records.
	Links []LinkRecord `json:"links"`
}

// LinkListOption configures link list filtering.
type LinkListOption func(*linkListOptions)

// linkListOptions holds the accumulated filter state.
type linkListOptions struct {
	kinds     map[LinkKind]struct{}
	programID *kernel.ProgramID
}

// Matches returns true if the link matches all filter criteria.
func (o *linkListOptions) Matches(link *LinkRecord) bool {
	return o.matchesKind(link) && o.matchesProgramID(link)
}

func (o *linkListOptions) matchesKind(link *LinkRecord) bool {
	if len(o.kinds) == 0 {
		return true
	}
	_, ok := o.kinds[link.Kind]
	return ok
}

func (o *linkListOptions) matchesProgramID(link *LinkRecord) bool {
	if o.programID == nil {
		return true
	}
	return link.ProgramID == *o.programID
}

// ApplyLinkListOptions applies the given options and returns the configured filter.
func ApplyLinkListOptions(opts ...LinkListOption) *linkListOptions {
	o := &linkListOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithKinds filters to links of the specified kinds.
func WithKinds(kinds ...LinkKind) LinkListOption {
	return func(o *linkListOptions) {
		if o.kinds == nil {
			o.kinds = make(map[LinkKind]struct{})
		}
		for _, k := range kinds {
			o.kinds[k] = struct{}{}
		}
	}
}

// WithProgramID filters to links attached to the given program.
func WithProgramID(id kernel.ProgramID) LinkListOption {
	return func(o *linkListOptions) {
		o.programID = &id
	}
}

// HasPin returns true if this link has a pin path.
func (r LinkRecord) HasPin() bool { return r.PinPath != nil }

// LinkStatus is observed state (kernel + fs).
// This is "what actually exists right now".
type LinkStatus struct {
	// Kernel nil means the link is not currently in the kernel's link list, or
	// bpfman did not capture a kernel link ID for this managed attachment.
	// Always emitted as JSON null in that case; a present pointer carries the
	// kernel-reported view.
	Kernel *kernel.Link `json:"kernel"`

	// KernelSeen is true if kernel enumeration succeeded, distinguishing
	// "not found" from "unknown".
	KernelSeen bool `json:"kernel_seen"`

	// PinPresent is true if the pin path exists on the filesystem.
	PinPresent bool `json:"pin_present"`
}

// HasLinkID is a capability interface for domain objects that carry a bpfman
// management handle.
type HasLinkID interface {
	// LinkID returns the bpfman management handle.
	LinkID() LinkID
}

// Compile-time interface assertions.
var (
	_ HasLinkID = Link{}
	_ HasLinkID = LinkRecord{}
)

// Link is the canonical domain object combining record and status.
// Record comes from the store (what bpfman manages).
// Status comes from observation (kernel enumeration + filesystem checks).
type Link struct {
	// Record is the stored link record (what bpfman manages).
	Record LinkRecord `json:"record"`

	// Status is the observed link state (kernel enumeration + filesystem checks).
	Status LinkStatus `json:"status"`
}

// LinkID returns the link's bpfman management handle.
func (l Link) LinkID() LinkID { return l.Record.ID }

// LinkID returns the record's bpfman management handle.
func (r LinkRecord) LinkID() LinkID { return r.ID }

// AttachOutput is the raw result of a kernel attach operation.
// This is transient I/O boundary data - the manager uses it along with
// the AttachSpec to construct the stored LinkSpec.
//
// AttachOutput parallels LoadOutput for programs: it captures what the
// kernel returned without mixing in user-provided metadata.
type AttachOutput struct {
	// KernelLinkID is the captured kernel link ID, if observed.
	KernelLinkID *kernel.LinkID

	// KernelLink is the kernel link info, nil when no kernel ID was captured.
	KernelLink *kernel.Link

	// PinPath is the actual bpffs link pin, empty if none was created.
	PinPath LinkPath
}

// NewPinnedLinkSpec creates a fully-detailed spec for a pinned link.
// Kind is derived from details to enforce the invariant.
func NewPinnedLinkSpec(programID kernel.ProgramID, kernelLinkID *kernel.LinkID, details LinkDetails, pin LinkPath) LinkSpec {
	return LinkSpec{
		ProgramID:    programID,
		KernelLinkID: kernelLinkID,
		Kind:         details.Kind(),
		PinPath:      &pin,
		Details:      details,
	}
}

// NewEphemeralLinkSpec creates a fully-detailed spec for an unpinned link.
// Kind is derived from details to enforce the invariant.
func NewEphemeralLinkSpec(programID kernel.ProgramID, kernelLinkID *kernel.LinkID, details LinkDetails) LinkSpec {
	return LinkSpec{
		ProgramID:    programID,
		KernelLinkID: kernelLinkID,
		Kind:         details.Kind(),
		Details:      details,
	}
}

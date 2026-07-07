package bpfman

import (
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"k8s.io/apimachinery/pkg/labels"

	"github.com/bpfman/bpfman/kernel"
)

// ProgramType is bpfman's discriminator for BPF program types. It is
// distinct from kernel.ProgramType: the latter carries the coarser
// kernel taxonomy (tc and tcx both map to the kernel's schedcls, the
// kprobe family to kprobe, fentry/fexit to tracing), so the two cannot
// be unified without losing the attach distinction.
//
// It is a plain string enum, so a ProgramType value carries no proof of
// validity -- ProgramType("bogus") is representable. Validity is
// therefore enforced at the points that matter, not by the type:
// ParseProgramType is the strict boundary parser (CLI and gRPC route
// through it), the NewLoadSpec / NewAttachLoadSpec constructors reject
// unknown values, and Valid reports membership of the known set. JSON
// decoding is deliberately permissive, trusting bpfman's own stored
// records rather than revalidating them.
type ProgramType string

// The known program types.
const (
	ProgramTypeXDP        ProgramType = "xdp"
	ProgramTypeTC         ProgramType = "tc"
	ProgramTypeTCX        ProgramType = "tcx"
	ProgramTypeTracepoint ProgramType = "tracepoint"
	ProgramTypeKprobe     ProgramType = "kprobe"
	ProgramTypeKretprobe  ProgramType = "kretprobe"
	ProgramTypeUprobe     ProgramType = "uprobe"
	ProgramTypeUretprobe  ProgramType = "uretprobe"
	ProgramTypeFentry     ProgramType = "fentry"
	ProgramTypeFexit      ProgramType = "fexit"
)

// allProgramTypes is the canonical list of valid program types.
// ParseProgramType and ProgramTypeNames derive from this.
var allProgramTypes = []ProgramType{
	ProgramTypeXDP,
	ProgramTypeTC,
	ProgramTypeTCX,
	ProgramTypeTracepoint,
	ProgramTypeKprobe,
	ProgramTypeKretprobe,
	ProgramTypeUprobe,
	ProgramTypeUretprobe,
	ProgramTypeFentry,
	ProgramTypeFexit,
}

// AllProgramTypes returns all valid program types.
func AllProgramTypes() []ProgramType {
	return allProgramTypes
}

// ProgramTypeNames returns all valid program type names as strings.
func ProgramTypeNames() []string {
	names := make([]string, len(allProgramTypes))
	for i, t := range allProgramTypes {
		names[i] = t.String()
	}
	return names
}

// String returns the string representation of the program type.
func (t ProgramType) String() string { return string(t) }

// KernelType projects bpfman's attach-oriented program type onto the
// coarser kernel taxonomy (cilium/ebpf's lowercased type names). The
// mapping is many-to-one and lossy: tc and tcx both load as the
// kernel's schedcls, the kprobe family as kprobe, and fentry/fexit as
// tracing. It exists to filter kernel-only (unmanaged) programs, which
// carry only the kernel-reported type and none of bpfman's finer
// metadata; managed programs are filtered on their bpfman type
// directly, so they keep the finer distinction.
func (t ProgramType) KernelType() kernel.ProgramType {
	switch t {
	case ProgramTypeTC, ProgramTypeTCX:
		return kernel.NewProgramType("schedcls")
	case ProgramTypeKprobe, ProgramTypeKretprobe, ProgramTypeUprobe, ProgramTypeUretprobe:
		return kernel.NewProgramType("kprobe")
	case ProgramTypeFentry, ProgramTypeFexit:
		return kernel.NewProgramType("tracing")
	case ProgramTypeXDP:
		return kernel.NewProgramType("xdp")
	case ProgramTypeTracepoint:
		return kernel.NewProgramType("tracepoint")
	default:
		return ""
	}
}

// ParseProgramType parses a string into a ProgramType.
// Returns the ProgramType and a nil error if valid, or the zero value
// and an error if not recognised.
func ParseProgramType(s string) (ProgramType, error) {
	switch s {
	case "xdp":
		return ProgramTypeXDP, nil
	case "tc":
		return ProgramTypeTC, nil
	case "tcx":
		return ProgramTypeTCX, nil
	case "tracepoint":
		return ProgramTypeTracepoint, nil
	case "kprobe":
		return ProgramTypeKprobe, nil
	case "kretprobe":
		return ProgramTypeKretprobe, nil
	case "uprobe":
		return ProgramTypeUprobe, nil
	case "uretprobe":
		return ProgramTypeUretprobe, nil
	case "fentry":
		return ProgramTypeFentry, nil
	case "fexit":
		return ProgramTypeFexit, nil
	default:
		return "", fmt.Errorf("unknown program type %q", s)
	}
}

// ProgramHandles contains stable filesystem handles for management.
// These are outputs of load, used for lifecycle operations.
type ProgramHandles struct {
	// PinPath is where the program is pinned in the bpffs.
	PinPath ProgPinPath `json:"pin_path"`

	// MapsDir is the directory where this program's maps are pinned
	// (per-program when the program owns its maps, the owner's
	// MapsDir when the program shares maps via map_owner_id).
	// JSON tag preserved as map_pin_path for compatibility with
	// existing on-disk records.
	MapsDir MapDir `json:"map_pin_path"`

	// MapOwnerID nil means this program is not a shared-map consumer of another
	// program; emitted as JSON null in that case so the consumer schema is stable.
	MapOwnerID *kernel.ProgramID `json:"map_owner_id"`
}

// ProgramMeta contains operator-facing management metadata.
// Searchable/editable without affecting the loaded program.
type ProgramMeta struct {
	// Name is a human-readable label for the program.
	Name string `json:"name"`

	// Owner is who manages this program; empty means unassigned.
	Owner string `json:"owner"`

	// Description is a free-text description; empty means none.
	Description string `json:"description"`

	// Metadata holds arbitrary key/value pairs for selection. It is always
	// emitted: {} when the operator supplied none, otherwise the user's
	// key/value pairs. nil and empty map collapse to the empty map at marshal
	// time so consumers see a stable shape.
	Metadata map[string]string `json:"metadata"`
}

// ProgramRecord is the stored record of a loaded program (DB-backed).
// ProgramID is the DB primary key and user-facing identity.
//
// Note: ProgramRecord is distinct from LoadSpec. LoadSpec describes how to load
// a program (validated input), while ProgramRecord describes a loaded program's
// stored state (output). They share some fields but serve different purposes.
type ProgramRecord struct {
	// ProgramID is the kernel program ID: the DB primary key and the
	// user-facing identity.
	ProgramID kernel.ProgramID `json:"program_id"`

	// Load is the validated load request the program was loaded from.
	Load LoadSpec `json:"load"`

	// License is the program licence discovered at load time from the ELF.
	// It lives on ProgramRecord (not LoadSpec) because it is a property of
	// the loaded program, not part of the load request. Empty when not
	// discovered (enumerated rather than loaded).
	License string `json:"license"`

	// GPLCompatible reports whether License is GPL-compatible, discovered at
	// load time from the ELF. It lives on ProgramRecord (not LoadSpec)
	// because it is a property of the loaded program, not the load request.
	GPLCompatible bool `json:"gpl_compatible"`

	// Handles holds the stable filesystem handles used for lifecycle operations.
	Handles ProgramHandles `json:"handles"`

	// Meta holds operator-facing management metadata.
	Meta ProgramMeta `json:"meta"`

	// CreatedAt is when the program record was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is nil when the record has never been updated
	// since creation, distinct from CreatedAt. The pointer +
	// JSON null encoding keeps "created at T, never updated" and
	// "created at T, updated at T'" distinguishable on the wire
	// without conflating the two timestamps.
	UpdatedAt *time.Time `json:"updated_at"`
}

// ProgramStatus is observed state (kernel + filesystem-derived paths).
//
// Path fields (ProgPin, MapDir, Bytecode) carry the
// canonical filesystem locations bpfman would write to or read
// from for this program. They are derived from the program ID and
// runtime layout, not stat'd: a populated path is a claim about
// where the file would live, not a claim that it currently does.
// Callers that need "does this path actually exist" do their own
// stat -- the wire shape does not encode presence.
//
// Kernel and Stats are the kernel-observation half. Kernel is nil
// when the program is not loaded; Stats is nil when stats were
// not collected (kernel.bpf_stats_enabled=0, observation skipped,
// or fetch failed).
type ProgramStatus struct {
	// Kernel is the kernel-observed program info, nil when the program is
	// not loaded.
	Kernel *kernel.Program `json:"kernel"`

	// Stats is the kernel-observed run statistics, nil when stats were not
	// collected.
	Stats *kernel.ProgramStats `json:"stats"`

	// ProgPin is the canonical bpffs path the program would be pinned at.
	ProgPin ProgPinPath `json:"prog_pin"`

	// MapDir is the canonical bpffs directory the program's maps would be
	// pinned under.
	MapDir MapDir `json:"map_dir"`

	// Bytecode is the canonical filesystem path of the program's stored
	// bytecode.
	Bytecode string `json:"bytecode"`

	// Links is the program's links; [] when none.
	Links []Link `json:"links"`

	// Maps is the program's maps; [] when none.
	Maps []MapStatus `json:"maps"`

	// MapUsedBy is the set of managed kernel program ids whose records point
	// at this program's map set, including this program when it is live.
	MapUsedBy []kernel.ProgramID `json:"map_used_by"`
}

// HasKernelProgramID is a capability interface for domain objects
// that carry a kernel-assigned program ID. The typed argument
// parsers use this to extract a program ID from an origin-backed
// structured value without depending on a concrete type.
type HasKernelProgramID interface {
	// KernelProgramID returns the kernel-assigned program ID.
	KernelProgramID() kernel.ProgramID
}

// Compile-time interface assertions.
var (
	_ HasKernelProgramID = Program{}
	_ HasKernelProgramID = ProgramRecord{}
)

// Program is the canonical domain object combining record and status.
// Record comes from the store (what bpfman manages).
// Status comes from observation (kernel enumeration + filesystem checks).
type Program struct {
	// Record is the stored program record (what bpfman manages).
	Record ProgramRecord `json:"record"`

	// Status is the observed program state (kernel enumeration + filesystem checks).
	Status ProgramStatus `json:"status"`
}

// MarshalJSON for ProgramMeta coerces a nil Metadata map to the empty
// map so JSON consumers always see "metadata": {} rather than null or
// field absence. Always-emit is the contract; the construction path
// that left Metadata nil is therefore invisible to consumers.
func (m ProgramMeta) MarshalJSON() ([]byte, error) {
	type alias ProgramMeta
	a := alias(m)
	if a.Metadata == nil {
		a.Metadata = map[string]string{}
	}
	return json.Marshal(a)
}

// MarshalJSON for ProgramStatus coerces nil slices to empty slices so JSON
// consumers always see collection fields as [] rather than null.
func (s ProgramStatus) MarshalJSON() ([]byte, error) {
	type alias ProgramStatus
	a := alias(s)
	if a.Links == nil {
		a.Links = []Link{}
	}
	if a.Maps == nil {
		a.Maps = []MapStatus{}
	}
	if a.MapUsedBy == nil {
		a.MapUsedBy = []kernel.ProgramID{}
	}
	return json.Marshal(a)
}

// KernelProgramID returns the program's kernel-assigned ID.
func (p Program) KernelProgramID() kernel.ProgramID { return p.Record.ProgramID }

// KernelProgramID returns the record's kernel-assigned program ID.
func (r ProgramRecord) KernelProgramID() kernel.ProgramID { return r.ProgramID }

// MapStatus represents observed map state: kernel info plus
// filesystem pin path and presence.
type MapStatus struct {
	// Map is the kernel-reported map info.
	kernel.Map

	// PinPath is the map's bpffs pin path.
	PinPath MapPinPath `json:"pin_path"`

	// Present is true when the pin path exists on the filesystem.
	Present bool `json:"present"`
}

// ToMapStatus converts kernel maps to MapStatus values with zero-
// valued pin fields. Use this at construction sites that only have
// kernel maps and no filesystem context.
func ToMapStatus(maps []kernel.Map) []MapStatus {
	result := make([]MapStatus, len(maps))
	for i, m := range maps {
		result[i] = MapStatus{Map: m}
	}
	return result
}

// WithDescription returns a new ProgramRecord with the description set.
func (p ProgramRecord) WithDescription(desc string) ProgramRecord {
	cp := p
	cp.Meta.Description = desc
	cp.Meta.Metadata = maps.Clone(p.Meta.Metadata)
	// Clone global data by reconstructing the LoadSpec with cloned data
	cp.Load = cp.Load.WithGlobalData(maps.Clone(p.Load.GlobalData()))
	return cp
}

// LoadOutput is the raw result of kernel.Load().
// This is transient I/O boundary data, not stored in the DB.
type LoadOutput struct {
	// PinPath is where the program was pinned.
	PinPath ProgPinPath

	// MapsDir is where the program's maps were pinned.
	MapsDir MapDir

	// Program is the kernel-reported program info (ID, MapIDs, etc).
	Program *kernel.Program

	// License is the licence string from the ELF, for the GPL check.
	License string

	// InferredType is the program type inferred from the ELF when the user
	// did not specify one.
	InferredType ProgramType

	// SharedMapNames is the list of PinByName map names, for reference counting.
	SharedMapNames []string
}

// IsGPLCompatible checks if a license string is GPL compatible.
// This matches the kernel's license_is_gpl_compatible() function.
func IsGPLCompatible(license string) bool {
	switch license {
	case "GPL", "GPL v2", "GPL and additional rights",
		"Dual BSD/GPL", "Dual MIT/GPL", "Dual MPL/GPL":
		return true
	default:
		return false
	}
}

// TestLoadSpec creates a LoadSpec with the given program type.
// This is a convenience constructor for tests.
func TestLoadSpec(programType ProgramType) LoadSpec {
	return LoadSpec{}.WithProgramType(programType)
}

// TestLoadSpecWithPath creates a LoadSpec with the given program type and object path.
// This is a convenience constructor for tests.
func TestLoadSpecWithPath(programType ProgramType, objectPath string) LoadSpec {
	return LoadSpec{}.
		WithProgramType(programType).
		WithObjectPath(objectPath)
}

// ProgramListEntry is one row of `program list`. It summarises a
// program rather than carrying the full managed Program: the common
// columns are top-level fields, so a kernel-only program -- one loaded
// in the kernel but not managed by bpfman, surfaced by `program list
// --all` -- is represented honestly without a synthetic Record. Record
// is present only for managed programs; Kernel is present whenever the
// program was observed in the kernel.
type ProgramListEntry struct {
	// ProgramID is the kernel program ID.
	ProgramID kernel.ProgramID `json:"program_id"`

	// Managed reports whether bpfman manages this program.
	Managed bool `json:"managed"`

	// Application is the application label from the program's metadata;
	// empty when unset.
	Application string `json:"application"`

	// Type is the program type name.
	Type string `json:"type"`

	// FunctionName is the program's entry function name (the stored ELF name
	// for managed programs, the kernel name otherwise).
	FunctionName string `json:"function_name"`

	// Links is the bpfman link handles attached to this program; [] when none.
	Links []LinkID `json:"links"`

	// Record is the managed store record, non-nil only when Managed is
	// true; it is null for kernel-only programs.
	Record *ProgramRecord `json:"record"`

	// Kernel is the kernel observation, non-nil when the program is
	// loaded in the kernel.
	Kernel *kernel.Program `json:"kernel"`
}

// ProgramListResult is the result of `program list`: summary entries
// wrapped in a stable top-level `programs` key, matching LoadResult
// and LinkListResult. It carries summary entries rather than full
// managed programs so the listing can include kernel-only rows (under
// --all) without a synthetic Program.
type ProgramListResult struct {
	// Programs is the list of program summary entries.
	Programs []ProgramListEntry `json:"programs"`
}

// LoadResult wraps the programs returned by Manager.Load. The
// wrapper exists so CLI JSON output exposes a stable top-level
// `programs` key matching ProgramListResult and LinkListResult.
//
// Programs are returned in the same order as the input ProgramSpec
// slice. Tests rely on this ordering contract; do not break it.
type LoadResult struct {
	// Programs holds the loaded programs, in the same order as the input
	// ProgramSpec slice.
	Programs []Program `json:"programs"`
}

// ListOption configures program list filtering.
type ListOption func(*listOptions)

// listOptions holds the accumulated filter state.
type listOptions struct {
	attached         *bool // nil = don't filter, true = attached only, false = unattached only
	types            map[ProgramType]struct{}
	selector         labels.Selector
	includeUnmanaged bool
}

// Matches returns true if the program matches all filter criteria.
func (o *listOptions) Matches(prog *Program) bool {
	return o.matchesAttachment(prog) &&
		o.matchesType(prog) &&
		o.matchesLabels(prog)
}

func (o *listOptions) matchesAttachment(prog *Program) bool {
	if o.attached == nil {
		return true
	}
	hasLinks := hasActiveLinks(prog)
	if *o.attached {
		return hasLinks
	}
	return !hasLinks
}

func (o *listOptions) matchesType(prog *Program) bool {
	if len(o.types) == 0 {
		return true
	}
	_, ok := o.types[prog.Record.Load.ProgramType()]
	return ok
}

func (o *listOptions) matchesLabels(prog *Program) bool {
	if o.selector == nil {
		return true
	}
	return o.selector.Matches(labels.Set(prog.Record.Meta.Metadata))
}

// hasActiveLinks returns true if the program has at least one link
// with kernel presence (actually attached, not just a DB record).
func hasActiveLinks(prog *Program) bool {
	for _, link := range prog.Status.Links {
		if link.Status.Kernel != nil {
			return true
		}
	}
	return false
}

// ApplyListOptions applies the given options and returns the configured listOptions.
func ApplyListOptions(opts ...ListOption) *listOptions {
	o := &listOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithAttached filters to programs with active kernel links.
func WithAttached() ListOption {
	return func(o *listOptions) {
		t := true
		o.attached = &t
	}
}

// WithUnattached filters to programs without active kernel links.
func WithUnattached() ListOption {
	return func(o *listOptions) {
		f := false
		o.attached = &f
	}
}

// WithTypes filters to programs of the specified types.
func WithTypes(types ...ProgramType) ListOption {
	return func(o *listOptions) {
		if o.types == nil {
			o.types = make(map[ProgramType]struct{})
		}
		for _, t := range types {
			o.types[t] = struct{}{}
		}
	}
}

// MatchingLabels filters to programs with matching label key-value pairs.
func MatchingLabels(lbls map[string]string) ListOption {
	return func(o *listOptions) {
		o.selector = labels.SelectorFromSet(labels.Set(lbls))
	}
}

// MatchingSelector filters to programs matching the label selector.
func MatchingSelector(sel labels.Selector) ListOption {
	return func(o *listOptions) {
		o.selector = sel
	}
}

// WithIncludeUnmanaged includes kernel-only programs in the listing --
// programs loaded in the kernel that bpfman does not manage. This is
// the `program list --all` surface. Without it, only managed programs
// are listed.
func WithIncludeUnmanaged() ListOption {
	return func(o *listOptions) {
		o.includeUnmanaged = true
	}
}

// IncludeUnmanaged reports whether kernel-only programs should be listed.
func (o *listOptions) IncludeUnmanaged() bool {
	return o.includeUnmanaged
}

// MatchesKernelOnly reports whether a kernel-only program of the given
// kernel program type passes the filter. Kernel-only programs carry no
// bpfman link or metadata state, so an attachment filter
// (--attached/--unattached) or any label/metadata selector excludes
// them; only the program-type filter applies.
//
// The filter holds bpfman types but a kernel-only program reports only
// its kernel type, so each filter type is projected onto the kernel
// taxonomy (see ProgramType.KernelType) before comparison. The match is
// therefore necessarily coarse: --type=tc and --type=tcx both match a
// kernel-only schedcls program, the probe variants all match a
// kernel-only kprobe, and fentry/fexit both match a kernel-only
// tracing, because the kernel does not record bpfman's finer intent.
func (o *listOptions) MatchesKernelOnly(kernelType kernel.ProgramType) bool {
	if o.attached != nil {
		return false
	}
	if o.selector != nil && !o.selector.Empty() {
		return false
	}
	if len(o.types) == 0 {
		return true
	}
	for t := range o.types {
		if t.KernelType() == kernelType {
			return true
		}
	}
	return false
}

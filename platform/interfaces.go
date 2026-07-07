package platform

import (
	"context"
	"errors"
	"io"
	"iter"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/lock"
)

// ErrRecordNotFound is returned when a store lookup by ID finds no
// matching row.
var ErrRecordNotFound = errors.New("record not found")

// ErrMapSetIDReused is returned when a self-owned load would create a map
// set whose id is already held by a surviving map set: the kernel reused
// a program id while an older map set keyed by that id is still in use.
// The load is refused rather than silently joining or overwriting the
// surviving set, so the failure is diagnosable instead of surfacing as a
// bare constraint violation.
var ErrMapSetIDReused = errors.New("reused kernel program id collided with a surviving map set")

// ErrMapOwnerNotFound is returned when a load requests an explicit map owner
// whose map set does not exist. Callers wrap it with the offending owner id so
// front-ends can report a useful invalid-argument error.
var ErrMapOwnerNotFound = errors.New("map owner not found")

// ErrInterfaceNotFound marks a failure to resolve a network interface
// name (in its netns) to an ifindex -- an unknown interface or an
// unreachable netns path. Both stem from caller-supplied input, so a
// front-end can map it to an invalid-argument status rather than an
// internal error. InterfaceResolver implementations wrap it.
var ErrInterfaceNotFound = errors.New("interface not found")

// LinkWriter writes standalone link metadata to the store.
// Dispatcher-backed XDP/TC member links are persisted and removed
// through DispatcherStore snapshot operations, not through LinkWriter.
type LinkWriter interface {
	// CreateLink persists a standalone link record and its
	// type-specific details, allocating a bpfman LinkID.
	// Dispatcher-backed XDP/TC member links are persisted through
	// DispatcherStore.ReplaceDispatcherSnapshot, not through CreateLink.
	CreateLink(ctx context.Context, spec bpfman.LinkSpec) (bpfman.LinkRecord, error)

	// DeleteLink removes a standalone link record. Returns an
	// error if the link is dispatcher-backed (XDP/TC); those must
	// be removed via DispatcherStore lifecycle operations.
	DeleteLink(ctx context.Context, linkID bpfman.LinkID) error

	// CreatePendingLink persists a standalone link record before
	// the kernel attach happens, allocating a bpfman LinkID and --
	// in the same transaction -- recording the link's pin path as
	// {linksDir}/{link_id}. Writing the pin path at creation means
	// no observable state has a bpffs pin that the store does not
	// name: a crash between pin and finalise still leaves a row
	// whose pin path cleanup can detach. The returned record
	// carries the pin path; KernelLinkID is nil until
	// FinaliseLink.
	CreatePendingLink(ctx context.Context, spec bpfman.LinkSpec, linksDir string) (bpfman.LinkRecord, error)

	// FinaliseLink records the captured kernel link ID on a
	// pending link row created by CreatePendingLink. Returns the
	// updated record without details.
	FinaliseLink(ctx context.Context, linkID bpfman.LinkID, kernelLinkID *kernel.LinkID) (bpfman.LinkRecord, error)
}

// LinkReader reads link metadata from the store.
type LinkReader interface {
	// GetLink returns the link record for linkID with its
	// type-specific details populated. It performs a two-phase
	// lookup -- link registry, then the per-kind detail table --
	// and returns ErrRecordNotFound if no link has that ID.
	GetLink(ctx context.Context, linkID bpfman.LinkID) (bpfman.LinkRecord, error)
}

// LinkLister lists links from the store.
type LinkLister interface {
	// ListLinks returns every link record with its type-specific
	// details populated. The iteration order is unspecified.
	ListLinks(ctx context.Context) ([]bpfman.LinkRecord, error)

	// ListLinksByProgram returns every link attached to programID,
	// with type-specific details populated.
	ListLinksByProgram(ctx context.Context, programID kernel.ProgramID) ([]bpfman.LinkRecord, error)

	// ListTCXLinksByInterface returns all TCX links for a given interface/direction/namespace.
	// Used for computing attach order based on priority.
	ListTCXLinksByInterface(ctx context.Context, nsid uint64, ifindex uint32, direction string) ([]bpfman.TCXLinkInfo, error)
}

// LinkStore combines all link store operations.
type LinkStore interface {
	LinkWriter
	LinkReader
	LinkLister
}

// DispatcherStore manages dispatcher state.
type DispatcherStore interface {
	// GetDispatcherSnapshot retrieves a complete snapshot of a
	// dispatcher and all its extension members, identified by key.
	// Returns ErrRecordNotFound if the dispatcher does not exist.
	GetDispatcherSnapshot(ctx context.Context, key dispatcher.Key) (DispatcherSnapshot, error)

	// ListDispatcherSummaries returns lightweight summaries of all
	// dispatchers, including member counts. This replaces the N+1
	// pattern of ListDispatchers + CountDispatcherLinks per dispatcher.
	ListDispatcherSummaries(ctx context.Context) ([]DispatcherSummary, error)

	// ReplaceDispatcherSnapshot atomically replaces all persisted
	// state for a dispatcher's attach point. The snapshot must
	// contain all members (existing and new). Old extension link
	// records for the attach point are removed and replaced with
	// the snapshot's members in a single transaction. The returned
	// snapshot is the completed persisted form, including store-
	// allocated LinkIDs for new members.
	ReplaceDispatcherSnapshot(ctx context.Context, snap DispatcherSnapshotSpec) (DispatcherSnapshot, error)

	// DeleteDispatcherSnapshot removes a dispatcher and all its
	// extension link records by attach point key.
	DeleteDispatcherSnapshot(ctx context.Context, key dispatcher.Key) error
}

// SharedMapPinStore tracks which programs reference shared PinByName
// map pins, enabling reference-counted cleanup on unload.
type SharedMapPinStore interface {
	// SaveSharedMapPins records that the given program uses the
	// named shared maps. Duplicate entries are silently ignored.
	SaveSharedMapPins(ctx context.Context, programID kernel.ProgramID, mapNames []string) error

	// DeleteSharedMapPins removes a program's shared map pin
	// entries and returns the map names that are no longer
	// referenced by any program (orphaned). The caller is
	// responsible for removing the corresponding filesystem pins.
	DeleteSharedMapPins(ctx context.Context, programID kernel.ProgramID) (orphanedMaps []string, err error)

	// ListReferencedSharedMaps returns all shared map names that
	// are still referenced by at least one program. Used by the
	// GC gather phase to detect filesystem orphans.
	ListReferencedSharedMaps(ctx context.Context) ([]string, error)
}

// Store combines program, link, dispatcher, and shared map pin store operations.
type Store interface {
	io.Closer
	ProgramStore
	LinkStore
	DispatcherStore
	SharedMapPinStore
	Transactional
}

// Transactional composes several store operations into one larger
// atomic unit. Store methods are themselves atomic domain primitives
// (multi-statement methods own a transaction internally), so reach
// for RunInTransaction only when a caller needs several of them to
// commit together. The callback receives a Store that participates
// in the transaction. If the callback returns nil, the transaction
// commits. If the callback returns an error, the transaction rolls
// back. A primitive's self-owned transaction entered from inside the
// callback flattens into the caller's transaction, so the two
// conventions compose.
//
// name identifies the transaction class for instrumentation: the
// store-side timing logs (wait_ms, held_ms) carry it as the tx
// field so log queries can group throughput and tail latency by
// transaction kind ("load", "cleanup_shared_map_pins", etc.)
// rather than seeing every transaction as a single anonymous
// workload. Use snake_case names that describe what the
// transaction does, not the calling context's internal phase
// organisation.
type Transactional interface {
	// RunInTransaction runs fn inside a single transaction,
	// committing when fn returns nil and rolling back when it
	// returns an error. The Store passed to fn participates in that
	// transaction. name classifies the transaction for the store's
	// timing instrumentation.
	RunInTransaction(ctx context.Context, name string, fn func(Store) error) error
}

// ProgramReader reads program metadata from the store.
type ProgramReader interface {
	// Get returns the program record for programID, or
	// ErrRecordNotFound if no such program exists.
	Get(ctx context.Context, programID kernel.ProgramID) (bpfman.ProgramRecord, error)
}

// ProgramWriter writes program metadata to the store.
type ProgramWriter interface {
	// Save persists program metadata for programID with
	// last-write-wins upsert semantics: an existing row is
	// overwritten rather than rejected, because the kernel recycles
	// program IDs aggressively after unload.
	Save(ctx context.Context, programID kernel.ProgramID, metadata bpfman.ProgramRecord) error

	// Delete removes the program record for programID, returning
	// ErrRecordNotFound if no such program exists.
	Delete(ctx context.Context, programID kernel.ProgramID) error
}

// ProgramLister lists all program metadata from the store.
type ProgramLister interface {
	// List returns every program record keyed by kernel ID. The
	// map's iteration order is unspecified.
	List(ctx context.Context) (map[kernel.ProgramID]bpfman.ProgramRecord, error)
}

// ProgramFinder finds programs by criteria.
type ProgramFinder any

// MapOwnershipReader queries map-sharing relationships. A map set is
// the group of pinned maps owned by one program and identified by that
// program's kernel ID; other programs that share those maps reference
// the same set.
type MapOwnershipReader interface {
	// CountMapSets returns the number of map sets currently recorded.
	CountMapSets(ctx context.Context) (int, error)

	// CountMapSetUsers returns the number of programs that reference
	// the map set identified by mapSetID, including its owner.
	CountMapSetUsers(ctx context.Context, mapSetID kernel.ProgramID) (int, error)

	// ListMapSetUsers returns the kernel IDs of the programs that
	// reference the map set identified by mapSetID, in ascending
	// order.
	ListMapSetUsers(ctx context.Context, mapSetID kernel.ProgramID) ([]kernel.ProgramID, error)

	// MapSetExists reports whether a map set with the given ID exists.
	MapSetExists(ctx context.Context, mapSetID kernel.ProgramID) (bool, error)

	// DeleteMapSet removes the map set identified by mapSetID,
	// returning ErrRecordNotFound if no such map set exists.
	DeleteMapSet(ctx context.Context, mapSetID kernel.ProgramID) error
}

// ProgramStore combines all store operations.
type ProgramStore interface {
	ProgramReader
	ProgramWriter
	ProgramLister
	ProgramFinder
	MapOwnershipReader
}

// KernelSource provides access to kernel BPF objects.
type KernelSource interface {
	// Programs returns an iterator over all BPF programs currently
	// loaded in the kernel. Each step yields a program or an
	// enumeration error.
	Programs(ctx context.Context) iter.Seq2[kernel.Program, error]

	// GetProgramByID returns the kernel-reported information for the
	// program with the given ID.
	GetProgramByID(ctx context.Context, id kernel.ProgramID) (kernel.Program, error)

	// GetProgramStatsByID returns runtime statistics for the program
	// with the given ID, or nil if statistics are unavailable (for
	// example when kernel.bpf_stats_enabled is 0).
	GetProgramStatsByID(ctx context.Context, id kernel.ProgramID) (*kernel.ProgramStats, error)

	// GetLinkByID returns the kernel-reported information for the
	// link with the given ID.
	GetLinkByID(ctx context.Context, id kernel.LinkID) (kernel.Link, error)

	// GetMapByID returns the kernel-reported information for the map
	// with the given ID.
	GetMapByID(ctx context.Context, id kernel.MapID) (kernel.Map, error)

	// Maps returns an iterator over all BPF maps currently loaded in
	// the kernel.
	Maps(ctx context.Context) iter.Seq2[kernel.Map, error]

	// Links returns an iterator over all BPF links currently present
	// in the kernel.
	Links(ctx context.Context) iter.Seq2[kernel.Link, error]
}

// ProgramLoader loads BPF programs into the kernel.
type ProgramLoader interface {
	// Load loads a BPF program and pins it using the bpffs paths.
	// Pin paths are computed from the kernel ID using bpffs methods:
	//   - Program: bpffs.ProgPinPath(program_id)
	//   - Maps: bpffs.MapPinDir(program_id) / <map_name>
	Load(ctx context.Context, spec bpfman.LoadSpec, bpffs fs.BPFFS) (bpfman.LoadOutput, error)

	// HasPinByName reports whether the bytecode referenced by spec
	// declares any LIBBPF_PIN_BY_NAME maps. The manager calls this
	// before the per-program load loop to decide whether to
	// acquire the cross-process writer lock for the load: shared
	// pin paths are the one resource that two concurrent loaders
	// can race on, so loads that touch them serialise while loads
	// without them stay lockless.
	HasPinByName(spec bpfman.LoadSpec) (bool, error)
}

// ProgramUnloader removes BPF programs from the kernel.
type ProgramUnloader interface {
	// Unload removes a BPF program from the kernel by unpinning it
	// at pinPath. It handles both a single-directory pin layout and
	// a split layout (separate program pin and maps directory), and
	// treats a missing path as success.
	Unload(ctx context.Context, pinPath string) error

	// UnloadProgram removes a program and its maps using the upstream pin layout.
	UnloadProgram(ctx context.Context, progPinPath bpfman.ProgPinPath, mapsDir string) error
}

// PinInspector provides raw inspection of bpffs pins.
type PinInspector interface {
	// ListPinDir scans a bpffs directory and returns its contents.
	ListPinDir(ctx context.Context, pinDir string, includeMaps bool) (*kernel.PinDirContents, error)

	// GetPinned loads and returns info about a pinned program.
	GetPinned(ctx context.Context, pinPath string) (*kernel.PinnedProgram, error)
}

// ProgramAttacher attaches programs to hooks.
// All methods return AttachOutput (raw kernel result) rather than Link,
// allowing the manager to construct LinkRecord from AttachSpec + AttachOutput.
type ProgramAttacher interface {
	// AttachTracepoint attaches a pinned program to a tracepoint.
	AttachTracepoint(ctx context.Context, progPinPath bpfman.ProgPinPath, group, name string, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error)

	// AttachXDP attaches a pinned XDP program to a network interface.
	AttachXDP(ctx context.Context, progPinPath bpfman.ProgPinPath, ifindex int, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error)

	// AttachKprobe attaches a pinned program to a kernel function.
	// If retprobe is true, attaches as a kretprobe instead of kprobe.
	AttachKprobe(ctx context.Context, progPinPath bpfman.ProgPinPath, fnName string, offset uint64, retprobe bool, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error)

	// AttachUprobeLocal attaches a pinned program to a user-space function
	// in the current namespace. Does not spawn a helper, so no lock scope needed.
	// target is the path to the binary or library (e.g., /usr/lib/libc.so.6).
	// pid > 0 scopes the probe to that process; 0 traces all processes.
	// If retprobe is true, attaches as a uretprobe instead of uprobe.
	AttachUprobeLocal(ctx context.Context, progPinPath bpfman.ProgPinPath, target, fnName string, offset uint64, pid int32, retprobe bool, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error)

	// AttachUprobeContainer attaches a pinned program to a user-space function
	// in a container's mount namespace. Spawns bpfman-ns helper, so requires
	// lock scope to pass fd.
	// target is the path to the binary or library (resolved in the container's namespace).
	// pid > 0 scopes the probe to that process; 0 traces all processes.
	// If retprobe is true, attaches as a uretprobe instead of uprobe.
	// containerPid identifies the target container.
	AttachUprobeContainer(ctx context.Context, scope lock.WriterScope, progPinPath bpfman.ProgPinPath, target, fnName string, offset uint64, pid int32, retprobe bool, linkPinPath bpfman.LinkPath, containerPid int32) (bpfman.AttachOutput, error)

	// AttachFentry attaches a pinned program to a kernel function entry point.
	// The fnName was specified at load time and stored with the program.
	AttachFentry(ctx context.Context, progPinPath bpfman.ProgPinPath, fnName string, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error)

	// AttachFexit attaches a pinned program to a kernel function exit point.
	// The fnName was specified at load time and stored with the program.
	AttachFexit(ctx context.Context, progPinPath bpfman.ProgPinPath, fnName string, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error)
}

// XDPDispatcherResult holds the result of loading an XDP dispatcher.
type XDPDispatcherResult struct {
	// DispatcherID is the kernel program ID of the loaded dispatcher
	// program.
	DispatcherID kernel.ProgramID

	// KernelLinkID is the kernel link ID of the XDP link binding the
	// dispatcher to the interface.
	KernelLinkID kernel.LinkID

	// DispatcherPin is the bpffs path at which the dispatcher program
	// is pinned.
	DispatcherPin bpfman.ProgPinPath

	// LinkPin is the bpffs path at which the XDP link is pinned.
	LinkPin bpfman.LinkPath
}

// TCDispatcherResult holds the result of loading a TC dispatcher.
// Legacy TC uses netlink (clsact qdisc + tc filter) rather than BPF
// links, so there is no link ID or link pin. Instead the kernel
// assigns a handle that identifies the filter for later removal.
type TCDispatcherResult struct {
	// DispatcherID is the kernel program ID of the loaded dispatcher
	// program.
	DispatcherID kernel.ProgramID

	// DispatcherPin is the bpffs path at which the dispatcher program
	// is pinned.
	DispatcherPin bpfman.ProgPinPath

	// Handle is the exact tc filter handle the kernel assigned, used
	// to target this filter for later removal.
	Handle uint32

	// Priority is the tc filter priority (typically 50).
	Priority uint16
}

// ExtensionLinkInfo is the kernel-reported state of a pinned freplace
// extension link, read via BPF_LINK_GET_INFO_BY_FD. Diagnostic; used
// to verify each freplace's trampoline is observably installed before
// the dispatcher swap.
type ExtensionLinkInfo struct {
	// KernelLinkID is the kernel link ID of the freplace extension
	// link.
	KernelLinkID kernel.LinkID

	// TargetProgID is the kernel program ID of the dispatcher program
	// the extension is replacing into.
	TargetProgID kernel.ProgramID

	// TargetBtfID is the BTF type ID of the dispatcher stub function
	// being replaced.
	TargetBtfID uint32

	// AttachType is the kernel attach type reported for the link.
	AttachType uint32
}

// DispatcherAttacher attaches dispatcher programs for multi-program chaining.
type DispatcherAttacher interface {
	// AttachXDPExtension attaches a pinned Extension program to a
	// dispatcher slot via freplace link.
	AttachXDPExtension(ctx context.Context, spec dispatcher.XDPExtensionAttachSpec) (bpfman.AttachOutput, error)

	// AttachTCExtension attaches a pinned Extension program to a TC
	// dispatcher slot via freplace link.
	AttachTCExtension(ctx context.Context, spec dispatcher.TCExtensionAttachSpec) (bpfman.AttachOutput, error)

	// ExtensionLinkInfo reads BPF_LINK_GET_INFO_BY_FD on a pinned
	// freplace extension link and returns the kernel-reported
	// trampoline target. Diagnostic; used to verify each freplace
	// is observably installed before swapping the dispatcher.
	ExtensionLinkInfo(ctx context.Context, linkPinPath bpfman.LinkPath) (ExtensionLinkInfo, error)

	// UpdateXDPDispatcherLink atomically updates an existing XDP
	// dispatcher BPF link to point to a new dispatcher program.
	// Used during rebuild to swap from old to new dispatcher.
	UpdateXDPDispatcherLink(ctx context.Context, linkPinPath bpfman.LinkPath, newProgPinPath bpfman.ProgPinPath) error

	// LoadAndPinXDPDispatcher loads an XDP dispatcher program with
	// the given .rodata config and pins it at progPinPath. Does not
	// create an XDP link. Returns the kernel program ID.
	LoadAndPinXDPDispatcher(ctx context.Context, cfg dispatcher.XDPConfig, progPinPath bpfman.ProgPinPath) (kernel.ProgramID, error)

	// LoadAndPinTCDispatcher loads a TC dispatcher program with
	// the given .rodata config and pins it at progPinPath. Does not
	// create a TC filter. Returns the kernel program ID.
	LoadAndPinTCDispatcher(ctx context.Context, cfg dispatcher.TCConfig, progPinPath bpfman.ProgPinPath) (kernel.ProgramID, error)

	// CreateXDPLink creates an XDP link from a pinned dispatcher
	// program to a network interface, optionally in a specific
	// network namespace. Returns the link info.
	CreateXDPLink(ctx context.Context, progPinPath bpfman.ProgPinPath, ifindex int, linkPinPath bpfman.LinkPath, netnsPath string) (*XDPDispatcherResult, error)

	// CreateTCFilter creates a TC filter from a pinned dispatcher
	// program on a network interface, optionally in a specific
	// network namespace. Creates the clsact qdisc if needed.
	// desiredHandle of 0 lets the kernel assign the handle (the normal
	// path); a non-zero value requests that exact handle, used by
	// rollback to restore a filter under the handle the snapshot still
	// records. The result carries the handle actually installed.
	CreateTCFilter(ctx context.Context, progPinPath bpfman.ProgPinPath, ifindex int, ifname string, direction bpfman.TCDirection, netnsPath string, desiredHandle uint32) (*TCDispatcherResult, error)

	// AttachTCX attaches a loaded program directly to an interface using TCX link.
	// Unlike TC which uses dispatchers, TCX uses native kernel multi-program support.
	// The program must already be pinned at programPinPath.
	//
	// Parameters:
	//   - ifindex: Network interface index
	//   - direction: "ingress" or "egress"
	//   - programPinPath: Path where the program is pinned
	//   - linkPinPath: Path to pin the TCX link
	//   - netns: Optional network namespace path. If non-empty, attachment is performed in that namespace.
	//   - order: Specifies where to insert the program in the TCX chain based on priority.
	AttachTCX(ctx context.Context, ifindex int, direction string, programPinPath bpfman.ProgPinPath, linkPinPath bpfman.LinkPath, netns string, order bpfman.TCXAttachOrder) (bpfman.AttachOutput, error)
}

// LinkDetacher detaches links from hooks.
type LinkDetacher interface {
	// DetachLink removes a pinned link by deleting its pin from bpffs.
	// This releases the kernel link if it was the last reference.
	DetachLink(ctx context.Context, linkPinPath bpfman.LinkPath) error
}

// PinRemover removes program pins from bpffs.
type PinRemover interface {
	// RemovePin removes a program pin from bpffs. The bpfman.ProgPinPath
	// type ensures only program pin paths -- not link pins, map pins,
	// or arbitrary strings -- can be passed in. For a kernel-attached
	// BPF link, DetachLink is required because dropping the userland
	// reference does not synchronously detach the link from its
	// attach point. Returns nil if the path does not exist.
	RemovePin(ctx context.Context, p bpfman.ProgPinPath) error
}

// TCFilterDetacher removes legacy TC BPF filters via netlink.
type TCFilterDetacher interface {
	// DetachTCFilter removes a tc filter identified by ifindex, parent,
	// priority, handle, and network namespace. This is the counterpart
	// to the netlink-based attachment performed by CreateTCFilter. The
	// handle is the exact kernel-assigned value CreateTCFilter echoed
	// back and the snapshot persisted, so the delete targets bpfman's
	// own filter rather than any other filter sharing the priority.
	DetachTCFilter(ctx context.Context, ifindex int, ifname string, parent uint32, priority uint16, handle uint32, netnsPath string) error

	// RemoveTCClsactIfUnused reclaims the clsact qdisc bpfman created on
	// an interface once both its ingress and egress filter blocks are
	// empty. Called on the last detach so bpfman owns the qdisc's full
	// lifecycle rather than leaking it. It leaves the qdisc in place when
	// any filter remains (a co-resident direction or a foreign owner) or
	// when no clsact is present, and treats a deleted netns as success.
	RemoveTCClsactIfUnused(ctx context.Context, ifindex int, ifname string, netnsPath string) error
}

// MapRepinner re-pins maps to new locations.
type MapRepinner interface {
	// RepinMap loads a pinned map and re-pins it to a new path.
	// Used by CSI to expose maps to per-pod bpffs.
	RepinMap(ctx context.Context, srcPath, dstPath string) error
}

// TracepointLister enumerates kernel tracepoints visible via tracefs.
type TracepointLister interface {
	// ListTracepoints returns all tracepoints as "group/name" strings
	// read from /sys/kernel/tracing/events/. Hidden tracefs metadata
	// files (enable, filter, header_page, etc.) are skipped. Returns
	// an empty slice if tracefs is unavailable; callers should treat
	// that as "cannot validate" rather than "no tracepoints exist".
	ListTracepoints(ctx context.Context) ([]string, error)
}

// KernelOperations combines all kernel operations.
type KernelOperations interface {
	KernelSource
	ProgramLoader
	ProgramUnloader
	PinInspector
	ProgramAttacher
	DispatcherAttacher
	LinkDetacher
	PinRemover
	MapRepinner
	TCFilterDetacher
	TracepointLister
	InterfaceResolver
}

// InterfaceResolver resolves a network interface name to its kernel
// ifindex within a network namespace. netnsPath is the path to the
// target namespace (for example /proc/<pid>/ns/net); an empty path
// resolves in the daemon's own namespace. Resolution must happen
// inside the target namespace because a name like a pod's "eth0"
// exists only there, not in the host. This is the single resolution
// boundary: the manager owns it, and the gRPC server and CLI pass
// interface names through untouched.
type InterfaceResolver interface {
	// InterfaceByName resolves the interface name to its kernel
	// ifindex within the namespace at netnsPath, returning
	// ErrInterfaceNotFound when the name or namespace cannot be
	// resolved.
	InterfaceByName(ctx context.Context, name, netnsPath string) (ifindex int, err error)
}

// ImageRef describes an OCI image to pull.
type ImageRef struct {
	// URL is the OCI image reference to pull (for example
	// quay.io/org/repo:tag).
	URL string

	// PullPolicy governs whether the image is fetched from the
	// registry or served from the local cache (Always, IfNotPresent,
	// or Never).
	PullPolicy bpfman.ImagePullPolicy

	// Auth carries registry credentials, or nil for anonymous access.
	Auth *ImageAuth
}

// ImageAuth contains credentials for authenticating to an OCI registry.
type ImageAuth struct {
	// Username is the registry account name for basic auth.
	Username string

	// Password is the registry password or token for basic auth.
	Password string
}

// Complete reports whether the credentials are usable for basic auth.
func (a *ImageAuth) Complete() bool {
	return a != nil && a.Username != "" && a.Password != ""
}

// PulledImage is the result of successfully pulling an OCI image.
type PulledImage struct {
	// ObjectPath is the path to the extracted ELF bytecode file.
	ObjectPath string

	// Programs maps program names to their types from the io.ebpf.programs label.
	Programs map[string]string

	// Maps maps map names to their types from the io.ebpf.maps label.
	Maps map[string]string

	// URL is the OCI image reference that was pulled.
	URL string

	// Digest is the resolved image digest.
	Digest string

	// PullPolicy is the policy that was used when pulling.
	PullPolicy bpfman.ImagePullPolicy
}

// ImagePuller fetches BPF bytecode from OCI images.
type ImagePuller interface {
	// Pull downloads an image and returns the extracted bytecode.
	// The returned ObjectPath is valid until the puller is closed or
	// the cache is cleaned.
	Pull(ctx context.Context, ref ImageRef) (PulledImage, error)
}

// SignatureVerificationStatus describes how an image satisfied signature
// policy.
type SignatureVerificationStatus string

// The signature-verification outcomes: verification disabled, the
// signature verified, or an unsigned image accepted by policy.
const (
	SignatureVerificationDisabled         SignatureVerificationStatus = "disabled"
	SignatureVerificationVerified         SignatureVerificationStatus = "verified"
	SignatureVerificationUnsignedAccepted SignatureVerificationStatus = "unsigned_accepted"
)

// SignatureVerification is the result of a successful signature policy
// decision.
type SignatureVerification struct {
	// Status describes how the image satisfied signature policy
	// (verification disabled, signature verified, or unsigned image
	// accepted).
	Status SignatureVerificationStatus
}

// SignatureVerificationRequest describes an OCI image signature policy
// check.
type SignatureVerificationRequest struct {
	// ImageRef is the OCI image reference whose signature is checked.
	ImageRef string

	// Auth carries registry credentials, or nil for
	// anonymous/default registry access.
	Auth *ImageAuth
}

// SignatureVerifier verifies OCI image signatures.
type SignatureVerifier interface {
	// Verify checks that the image satisfies signature policy.
	// Returns a result describing how the image was accepted.
	// Returns an error if the image signature is invalid or missing
	// (when unsigned images are not allowed).
	Verify(ctx context.Context, req SignatureVerificationRequest) (SignatureVerification, error)
}

// ProgramValidator validates requested program names against BPF
// object files. Loads name every program explicitly; there is no
// whole-object discovery (section names cannot distinguish tc from
// tcx and carry no fentry/fexit attach function, so a discovered
// type would be a guess).
type ProgramValidator interface {
	// ValidatePrograms checks that all specified program names exist in
	// the object file. Returns an error listing missing programs.
	ValidatePrograms(objectPath string, programNames []string) error
}

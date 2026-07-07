package inspect

import (
	"cmp"
	"context"
	"errors"
	"iter"
	"slices"
	"strconv"
	"time"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/platform"
)

// ErrNotFound is returned when a program is not found in any source.
var ErrNotFound = errors.New("not found")

// StoreLister is the subset of platform.Store needed by Snapshot.
type StoreLister interface {
	// List returns every program record keyed by kernel ID.
	List(ctx context.Context) (map[kernel.ProgramID]bpfman.ProgramRecord, error)

	// ListLinks returns every link record with its type-specific details populated.
	ListLinks(ctx context.Context) ([]bpfman.LinkRecord, error)

	// ListDispatcherSummaries returns lightweight summaries of all dispatchers, including member counts.
	ListDispatcherSummaries(ctx context.Context) ([]platform.DispatcherSummary, error)
}

// KernelLister is the subset of platform.KernelSource needed by Snapshot.
type KernelLister interface {
	// Programs returns an iterator over all BPF programs currently loaded in the kernel.
	Programs(ctx context.Context) iter.Seq2[kernel.Program, error]

	// Links returns an iterator over all BPF links currently present in the kernel.
	Links(ctx context.Context) iter.Seq2[kernel.Link, error]
}

// LinkGetter is the subset of platform.Store needed by GetLink.
type LinkGetter interface {
	// GetLink returns the link record for linkID with its type-specific details populated, or platform.ErrRecordNotFound if no link has that ID.
	GetLink(ctx context.Context, linkID bpfman.LinkID) (bpfman.LinkRecord, error)
}

// KernelLinkGetter is the subset of platform.KernelSource needed by GetLink.
type KernelLinkGetter interface {
	// GetLinkByID returns the kernel-reported information for the link with the given ID.
	GetLinkByID(ctx context.Context, id kernel.LinkID) (kernel.Link, error)
}

// LinkInfo is the result of GetLink, containing record and presence.
type LinkInfo struct {
	// Record is the store-recorded link record, with its type-specific
	// details populated.
	Record bpfman.LinkRecord `json:"record"`

	// Kernel nil means this link is not present in the kernel's link list;
	// pointer + omitempty encodes that absence.
	Kernel *kernel.Link `json:"kernel,omitempty"`

	// Presence reports which of store, kernel, and filesystem the link
	// was found in.
	Presence Presence `json:"presence"`
}

// Presence indicates where an object exists across the three sources.
type Presence struct {
	// InStore reports whether bpfman has a stored record for the object.
	InStore bool `json:"in_store"`

	// InKernel reports whether the object is currently present in the
	// kernel.
	InKernel bool `json:"in_kernel"`

	// InFS reports whether the object has a backing pin or directory on
	// bpffs.
	InFS bool `json:"in_fs"`
}

// Managed returns true if the object is tracked in the store.
func (p Presence) Managed() bool { return p.InStore }

// OrphanFS returns true if the object exists only on the filesystem.
func (p Presence) OrphanFS() bool { return p.InFS && !p.InStore && !p.InKernel }

// KernelOnly returns true if the object exists only in the kernel.
func (p Presence) KernelOnly() bool { return p.InKernel && !p.InStore }

// ProgramView is a correlation view of a program across store, kernel, and FS.
type ProgramView struct {
	// ProgramID is the kernel program ID that correlates this view's
	// store, kernel, and FS observations.
	ProgramID kernel.ProgramID `json:"program_id"`

	// Managed nil means the program is not recorded in the store (kernel-only or
	// FS-only observation). Pointer + omitempty encodes that absence.
	Managed *bpfman.ProgramRecord `json:"managed,omitempty"`

	// Kernel nil means the program is not currently loaded in the kernel.
	// Pointer + omitempty encodes that absence.
	Kernel *kernel.Program `json:"kernel,omitempty"`

	// FSPinPath is the bpffs program pin backing this program; empty
	// when no pin was found.
	FSPinPath string `json:"fs_pin_path"`

	// MapsPresent reports whether a map pin directory exists for this
	// program on bpffs.
	MapsPresent bool `json:"maps_present"`

	// Links are the links attached to this program, correlated from
	// Observation.Links; empty when the program has none.
	Links []LinkRow `json:"links"`

	// MapUsedBy is the sorted set of managed program ids sharing this
	// program's map set, derived once over the store records (see
	// MapSetMembers). Nil for kernel-only and FS-only rows, which are
	// not store-managed and so belong to no map set.
	MapUsedBy []kernel.ProgramID `json:"map_used_by"`

	// Presence reports which of store, kernel, and filesystem the
	// program was found in.
	Presence Presence `json:"presence"`
}

// AsProgram constructs a bpfman.Program composite from a store-managed program.
// Returns (Program, true) when Managed != nil (even if kernel/fs are missing).
// Status reflects what's actually present vs missing.
func (v ProgramView) AsProgram() (bpfman.Program, bool) {
	if v.Managed == nil {
		return bpfman.Program{}, false // not store-managed, can't construct
	}

	// Convert links
	var links []bpfman.Link
	for _, lr := range v.Links {
		if link, ok := lr.AsLink(); ok {
			links = append(links, link)
		}
	}

	return bpfman.Program{
		Record: *v.Managed,
		Status: bpfman.ProgramStatus{
			Kernel:    v.Kernel, // may be nil
			Links:     links,
			MapUsedBy: v.MapUsedBy,
		},
	}, true
}

// Name returns the program name (from store if available, else kernel).
func (v ProgramView) Name() string {
	if v.Managed != nil {
		return v.Managed.Meta.Name
	}
	if v.Kernel != nil {
		return v.Kernel.Name
	}
	return ""
}

// Type returns the program type (from store if available, else kernel).
func (v ProgramView) Type() string {
	if v.Managed != nil {
		return v.Managed.Load.ProgramType().String()
	}
	if v.Kernel != nil {
		return v.Kernel.ProgramType.String()
	}
	return ""
}

// PinPath returns the pin path (from store if available, else FS).
func (v ProgramView) PinPath() string {
	if v.Managed != nil && v.Managed.Handles.PinPath != "" {
		return v.Managed.Handles.PinPath.String()
	}
	return v.FSPinPath
}

// LinkRow is a store-first view of a link with presence annotations.
type LinkRow struct {
	// Managed nil means the link is not recorded in the store (kernel-only
	// observation). Pointer + omitempty encodes that absence.
	Managed *bpfman.LinkRecord `json:"managed,omitempty"`

	// Kernel nil means the link is not present in the kernel's link list.
	// Pointer + omitempty encodes that absence.
	Kernel *kernel.Link `json:"kernel,omitempty"`

	// FSPinPath is the bpf fs pin file backing this link, when one
	// is found. Populated from a walk of bpfman's bpf fs subtree
	// that loads each file as a bpf_link to read its ID, so links
	// pinned under dispatcher or TCX subtrees surface their pin
	// here alongside links/{link_id} pins. Empty when no pin was
	// located.
	FSPinPath string `json:"fs_pin_path,omitempty"`

	// Presence reports which of store, kernel, and filesystem the link
	// was found in.
	Presence Presence `json:"presence"`
}

// ID returns the link's durable bpfman ID.
func (r LinkRow) ID() bpfman.LinkID {
	if r.Managed != nil {
		return r.Managed.ID
	}
	return 0
}

// KernelLinkID returns the kernel link ID if available.
func (r LinkRow) KernelLinkID() *kernel.LinkID {
	if r.Managed != nil {
		return r.Managed.KernelLinkID
	}
	if r.Kernel != nil {
		return &r.Kernel.ID
	}
	return nil
}

// Kind returns the link kind (from store if available).
func (r LinkRow) Kind() bpfman.LinkKind {
	if r.Managed != nil {
		return r.Managed.Kind
	}
	return ""
}

// PinPath returns the pin path (from store if available).
func (r LinkRow) PinPath() string {
	if r.Managed != nil && r.Managed.PinPath != nil {
		return r.Managed.PinPath.String()
	}
	return ""
}

// HasPin returns true if this link has a pin path.
func (r LinkRow) HasPin() bool {
	if r.Managed != nil {
		return r.Managed.HasPin()
	}
	return false
}

// AsLink constructs a bpfman.Link composite from a store-managed link.
// Returns (Link, true) when Managed != nil.
func (r LinkRow) AsLink() (bpfman.Link, bool) {
	if r.Managed == nil {
		return bpfman.Link{}, false
	}
	return bpfman.Link{
		Record: *r.Managed,
		Status: bpfman.LinkStatus{
			Kernel:     r.Kernel,
			KernelSeen: r.Presence.InKernel,
			PinPresent: r.Presence.InFS,
		},
	}, true
}

// DispatcherRow is a store-first view of a dispatcher with presence annotations.
type DispatcherRow struct {
	// DispType is the dispatcher type: "xdp", "tc-ingress", or
	// "tc-egress". Together with Nsid and Ifindex it forms the
	// dispatcher's identity key.
	DispType string `json:"disp_type"`

	// Nsid is the network namespace ID the dispatcher belongs to.
	Nsid uint64 `json:"nsid"`

	// Ifindex is the index of the interface the dispatcher is attached
	// to.
	Ifindex uint32 `json:"ifindex"`

	// Managed nil means the dispatcher is not recorded in the store (an orphan
	// dispatcher observed only on the filesystem). Pointer + omitempty encodes
	// that absence.
	Managed *platform.DispatcherSummary `json:"managed,omitempty"`

	// Revision is the dispatcher's current revision number.
	Revision uint32 `json:"revision"`

	// ProgramID is the kernel program ID of the dispatcher program.
	ProgramID kernel.ProgramID `json:"program_id"`

	// KernelLinkID is the XDP dispatcher's kernel link ID; 0 for TC
	// dispatchers.
	KernelLinkID kernel.LinkID `json:"kernel_link_id"`

	// Priority is the TC filter priority; 0 for XDP dispatchers.
	Priority uint32 `json:"priority"`

	// ProgPresence reports which of store, kernel, and filesystem the
	// dispatcher program was found in.
	ProgPresence Presence `json:"prog_presence"`

	// LinkPresence reports which of store, kernel, and filesystem the
	// XDP link was found in (for XDP dispatchers).
	LinkPresence Presence `json:"link_presence"`

	// FSLinkCount is the count of link_* files in the revision
	// directory, or -1 when unknown (no dispatcher directory on bpffs).
	FSLinkCount int `json:"fs_link_count"`
}

// SnapshotMeta contains metadata about the snapshot.
type SnapshotMeta struct {
	// ObservedAt is when the snapshot was taken.
	ObservedAt time.Time `json:"observed_at"`

	// Errors are the non-fatal errors encountered while taking the
	// snapshot. They are excluded from JSON output (errors do not
	// serialise meaningfully).
	Errors []error `json:"-"`

	// ProgramEnumErrors counts errors during kernel program enumeration.
	ProgramEnumErrors int `json:"program_enum_errors"`

	// LinkEnumErrors counts errors during kernel link enumeration.
	LinkEnumErrors int `json:"link_enum_errors"`
}

// Observation is a point-in-time correlated view of bpfman's state across all sources.
type Observation struct {
	// Programs are the correlated program views, sorted by program ID.
	Programs []ProgramView `json:"programs"`

	// Links are the correlated link rows, sorted by bpfman link ID.
	Links []LinkRow `json:"links"`

	// Dispatchers are the correlated dispatcher rows, sorted by type,
	// namespace, then interface index.
	Dispatchers []DispatcherRow `json:"dispatchers"`

	// Meta carries the snapshot timestamp and any non-fatal enumeration
	// errors.
	Meta SnapshotMeta `json:"meta"`
}

// ManagedPrograms returns only store-managed programs.
func (o *Observation) ManagedPrograms() []ProgramView {
	var out []ProgramView
	for _, r := range o.Programs {
		if r.Presence.InStore {
			out = append(out, r)
		}
	}
	return out
}

// ManagedLinks returns only store-managed links.
func (o *Observation) ManagedLinks() []LinkRow {
	var out []LinkRow
	for _, r := range o.Links {
		if r.Presence.InStore {
			out = append(out, r)
		}
	}
	return out
}

// ManagedDispatchers returns only store-managed dispatchers.
func (o *Observation) ManagedDispatchers() []DispatcherRow {
	var out []DispatcherRow
	for _, r := range o.Dispatchers {
		if r.ProgPresence.InStore {
			out = append(out, r)
		}
	}
	return out
}

// Snapshot builds an Observation by reading from store, kernel, and filesystem.
// The returned Observation contains all objects from all sources, correlated
// by kernel ID. Use ManagedPrograms() etc. for the default store-first view.
func Snapshot(
	ctx context.Context,
	store StoreLister,
	kern KernelLister,
	scanner *fs.Scanner,
) (*Observation, error) {
	obs := &Observation{
		Meta: SnapshotMeta{
			ObservedAt: time.Now(),
		},
	}

	// Phase 1: Build indexes from kernel and filesystem.
	// We store full kernel.Link objects so that Phase 3 (link
	// correlation) can reuse them without a second enumeration.
	kernelProgs := make(map[kernel.ProgramID]kernel.Program)
	kernelLinks := make(map[kernel.LinkID]kernel.Link)

	for kp, err := range kern.Programs(ctx) {
		if err != nil {
			obs.Meta.Errors = append(obs.Meta.Errors, err)
			obs.Meta.ProgramEnumErrors++
			continue
		}

		kernelProgs[kp.ID] = kp
	}

	for kl, err := range kern.Links(ctx) {
		if err != nil {
			obs.Meta.Errors = append(obs.Meta.Errors, err)
			obs.Meta.LinkEnumErrors++
			continue
		}

		kernelLinks[kl.ID] = kl
	}

	// FS indexes
	fsProgPins := make(map[kernel.ProgramID]string)  // programID -> path
	fsMapDirs := make(map[kernel.ProgramID]string)   // programID -> path
	fsDispDirs := make(map[string]*fs.DispatcherDir) // "type/nsid/ifindex" -> dir
	fsDispLinks := make(map[string]string)           // "type/nsid/ifindex" -> path

	// Comprehensive link-pin index built by walking the entire
	// bpf fs subtree and loading each candidate as a bpf_link.
	// Used to set FSPinPath on every LinkRow below, including
	// kernel-only links whose pin lives outside {fs}/links/
	// (extension link slots, TCX, dispatcher stable links).
	fsLinkPins, err := scanLinkPins(ctx, scanner)
	if err != nil {
		obs.Meta.Errors = append(obs.Meta.Errors, err)
	}

	for pin, err := range scanner.ProgPins(ctx) {
		if err != nil {
			obs.Meta.Errors = append(obs.Meta.Errors, err)
			continue
		}

		fsProgPins[pin.ProgramID] = pin.Path
	}

	for dir, err := range scanner.MapDirs(ctx) {
		if err != nil {
			obs.Meta.Errors = append(obs.Meta.Errors, err)
			continue
		}

		fsMapDirs[dir.ProgramID] = dir.Path
	}

	for dir, err := range scanner.DispatcherDirs(ctx) {
		if err != nil {
			obs.Meta.Errors = append(obs.Meta.Errors, err)
			continue
		}

		key := dispatcherKey(dir.DispType, dir.Nsid, dir.Ifindex)
		d := dir // copy
		fsDispDirs[key] = &d
	}

	for pin, err := range scanner.DispatcherLinkPins(ctx) {
		if err != nil {
			obs.Meta.Errors = append(obs.Meta.Errors, err)
			continue
		}

		key := dispatcherKey(pin.DispType, pin.Nsid, pin.Ifindex)
		fsDispLinks[key] = pin.Path
	}

	// Phase 2: Build program rows (store-first)
	storeProgs, err := store.List(ctx)
	if err != nil {
		return nil, err
	}

	// Derive map-set membership once over the full store record set,
	// so every store-managed row carries its map-used-by set without a
	// per-program store query.
	mapUsedBy := MapSetMembers(storeProgs)

	seenProgIDs := make(map[kernel.ProgramID]bool)
	for programID, prog := range storeProgs {
		seenProgIDs[programID] = true
		fsPath, inFS := fsProgPins[programID]
		kp, inKernel := kernelProgs[programID]
		_, mapsPresent := fsMapDirs[programID]

		row := ProgramView{
			ProgramID:   programID,
			Managed:     &prog,
			FSPinPath:   fsPath,
			MapsPresent: mapsPresent,
			MapUsedBy:   mapUsedBy[programID],
			Presence: Presence{
				InStore:  true,
				InKernel: inKernel,
				InFS:     inFS,
			},
		}
		if inKernel {
			row.Kernel = &kp
		}
		obs.Programs = append(obs.Programs, row)
	}

	// Add kernel-only programs (not in store)
	for programID, kp := range kernelProgs {
		if seenProgIDs[programID] {
			continue
		}
		fsPath, inFS := fsProgPins[programID]
		row := ProgramView{
			ProgramID: programID,
			Kernel:    &kp,
			FSPinPath: fsPath,
			Presence: Presence{
				InStore:  false,
				InKernel: true,
				InFS:     inFS,
			},
		}
		obs.Programs = append(obs.Programs, row)
		seenProgIDs[programID] = true
	}

	// Add FS-only programs (not in store, not in kernel)
	for programID, fsPath := range fsProgPins {
		if seenProgIDs[programID] {
			continue
		}
		row := ProgramView{
			ProgramID: programID,
			FSPinPath: fsPath,
			Presence: Presence{
				InStore:  false,
				InKernel: false,
				InFS:     true,
			},
		}
		obs.Programs = append(obs.Programs, row)
	}

	// Phase 3: Build link rows (store-first).
	// kernelLinks (built in Phase 1) already contains full
	// kernel.Link objects, so no second enumeration is needed.
	storeLinks, err := store.ListLinks(ctx)
	if err != nil {
		return nil, err
	}

	seenKernelLinkIDs := make(map[kernel.LinkID]bool)
	for _, link := range storeLinks {
		// Track kernel link IDs we've seen from store.
		if link.KernelLinkID != nil {
			seenKernelLinkIDs[*link.KernelLinkID] = true
		}

		// Check kernel presence
		var kernelLink *kernel.Link
		if link.KernelLinkID != nil {
			if kl, ok := kernelLinks[*link.KernelLinkID]; ok {
				kernelLink = &kl
			}
		}

		// Resolve the pin path: prefer the bpf-fs walk index
		// (covers extension links and TCX pins), then fall
		// back to the store's recorded PinPath. The InFS flag
		// tracks whether either source confirmed a live pin.
		var fsPinPath string
		var inFS bool
		if link.KernelLinkID != nil {
			if pin, ok := fsLinkPins[*link.KernelLinkID]; ok {
				fsPinPath = pin
				inFS = true
			}
		}
		if fsPinPath == "" && link.PinPath != nil {
			storePath := link.PinPath.String()
			if scanner.PathExists(storePath) {
				fsPinPath = storePath
				inFS = true
			}
		}
		row := LinkRow{
			Managed:   &link,
			Kernel:    kernelLink,
			FSPinPath: fsPinPath,
			Presence: Presence{
				InStore:  true,
				InKernel: kernelLink != nil,
				InFS:     inFS,
			},
		}
		obs.Links = append(obs.Links, row)
	}

	// Add kernel-only links (not in store).
	for kernelLinkID, kl := range kernelLinks {
		if seenKernelLinkIDs[kernelLinkID] {
			continue
		}
		pin, inFS := fsLinkPins[kernelLinkID]
		row := LinkRow{
			Kernel:    &kl,
			FSPinPath: pin,
			Presence: Presence{
				InStore:  false,
				InKernel: true,
				InFS:     inFS,
			},
		}
		obs.Links = append(obs.Links, row)
	}

	// Phase 4: Build dispatcher rows (store-first)
	storeDisps, err := store.ListDispatcherSummaries(ctx)
	if err != nil {
		return nil, err
	}

	seenDispKeys := make(map[string]bool)
	for _, disp := range storeDisps {
		key := dispatcherKey(disp.Key.Type.String(), disp.Key.Nsid, disp.Key.Ifindex)
		seenDispKeys[key] = true

		fsDir := fsDispDirs[key]
		_, linkPinExists := fsDispLinks[key]

		fsLinkCount := -1
		progInFS := false
		if fsDir != nil {
			fsLinkCount = fsDir.LinkCount
			progInFS = true
		}

		var linkID kernel.LinkID
		if disp.Runtime.KernelLinkID != nil {
			linkID = *disp.Runtime.KernelLinkID
		}
		var priority uint32
		if disp.Runtime.FilterPriority != nil {
			priority = uint32(*disp.Runtime.FilterPriority)
		}

		_, progInKernel := kernelProgs[disp.Runtime.ProgramID]
		_, linkInKernel := kernelLinks[linkID]
		d := disp // copy for pointer
		row := DispatcherRow{
			DispType:     disp.Key.Type.String(),
			Nsid:         disp.Key.Nsid,
			Ifindex:      disp.Key.Ifindex,
			Managed:      &d,
			Revision:     disp.Revision,
			ProgramID:    disp.Runtime.ProgramID,
			KernelLinkID: linkID,
			Priority:     priority,
			FSLinkCount:  fsLinkCount,
			ProgPresence: Presence{
				InStore:  true,
				InKernel: progInKernel,
				InFS:     progInFS,
			},
			LinkPresence: Presence{
				InStore:  linkID != 0,
				InKernel: linkID != 0 && linkInKernel,
				InFS:     linkPinExists,
			},
		}
		obs.Dispatchers = append(obs.Dispatchers, row)
	}

	// Add FS-only dispatchers (orphan dirs)
	for key, fsDir := range fsDispDirs {
		if seenDispKeys[key] {
			continue
		}
		_, linkPinExists := fsDispLinks[key]
		row := DispatcherRow{
			DispType:    fsDir.DispType,
			Nsid:        fsDir.Nsid,
			Ifindex:     fsDir.Ifindex,
			Revision:    fsDir.Revision,
			FSLinkCount: fsDir.LinkCount,
			ProgPresence: Presence{
				InStore:  false,
				InKernel: false,
				InFS:     true,
			},
			LinkPresence: Presence{
				InStore:  false,
				InKernel: false,
				InFS:     linkPinExists,
			},
		}
		obs.Dispatchers = append(obs.Dispatchers, row)
	}

	// Correlate links to programs by ProgramID
	programIndex := make(map[kernel.ProgramID]int, len(obs.Programs))
	for i := range obs.Programs {
		programIndex[obs.Programs[i].ProgramID] = i
	}
	for _, link := range obs.Links {
		if link.Managed == nil {
			continue
		}
		if idx, ok := programIndex[link.Managed.ProgramID]; ok {
			obs.Programs[idx].Links = append(obs.Programs[idx].Links, link)
		}
	}

	// Sort all slices for deterministic output
	slices.SortFunc(obs.Programs, func(a, b ProgramView) int {
		return cmp.Compare(a.ProgramID, b.ProgramID)
	})
	slices.SortFunc(obs.Links, func(a, b LinkRow) int {
		return cmp.Compare(a.ID(), b.ID())
	})
	slices.SortFunc(obs.Dispatchers, func(a, b DispatcherRow) int {
		if c := cmp.Compare(a.DispType, b.DispType); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Nsid, b.Nsid); c != 0 {
			return c
		}
		return cmp.Compare(a.Ifindex, b.Ifindex)
	})

	return obs, nil
}

// GetLink retrieves a single link by its durable bpfman ID, correlating state
// from store, kernel, and filesystem. This is more efficient than Snapshot
// for single-link lookups as it performs targeted queries rather than
// enumerating everything.
//
// Returns ErrNotFound if the link does not exist in any source.
func GetLink(
	ctx context.Context,
	linkGetter LinkGetter,
	kern KernelLinkGetter,
	scanner *fs.Scanner,
	linkID bpfman.LinkID,
) (LinkInfo, error) {
	info := LinkInfo{}

	// Try store - this returns the full record with details
	record, err := linkGetter.GetLink(ctx, linkID)
	if err == nil {
		info.Record = record
		info.Presence.InStore = true
	} else if !errors.Is(err, platform.ErrRecordNotFound) {
		// Real error (not just "not found")
		return LinkInfo{}, err
	}

	// Try kernel when bpfman captured a kernel link ID.
	if info.Presence.InStore && record.KernelLinkID != nil {
		kl, err := kern.GetLinkByID(ctx, *record.KernelLinkID)
		if err == nil {
			info.Kernel = &kl
			info.Presence.InKernel = true
		}
		// Kernel errors (link not found) are not fatal - just means not in kernel
	}

	// Try filesystem - check if pin path exists
	if info.Presence.InStore && record.PinPath != nil {
		if scanner.PathExists(record.PinPath.String()) {
			info.Presence.InFS = true
		}
	}

	// If not found in store, return error (links are store-first)
	if !info.Presence.InStore {
		return LinkInfo{}, ErrNotFound
	}

	return info, nil
}

func dispatcherKey(dispType string, nsid uint64, ifindex uint32) string {
	return dispType + "/" + strconv.FormatUint(nsid, 10) + "/" + strconv.FormatUint(uint64(ifindex), 10)
}

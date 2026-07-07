package manager

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/inspect"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/lock"
	"github.com/bpfman/bpfman/platform"
)

// ErrProgramRequiresReconciliation is returned when a program has a
// store record but the corresponding kernel object is absent.
type ErrProgramRequiresReconciliation struct {
	// ProgramID is the program present in the store but missing from
	// the kernel.
	ProgramID kernel.ProgramID

	// Cause is the underlying kernel-lookup error that revealed the
	// absence; returned by Unwrap.
	Cause error
}

// Error reports that the program exists in the store but not in the
// kernel, and so requires reconciliation.
func (e ErrProgramRequiresReconciliation) Error() string {
	return fmt.Sprintf("program %d exists in store but not in kernel (requires reconciliation)", e.ProgramID)
}

// Unwrap returns the underlying kernel-lookup error that revealed the
// missing program.
func (e ErrProgramRequiresReconciliation) Unwrap() error {
	return e.Cause
}

// Get retrieves a managed program by its kernel ID with full
// filesystem enrichment. Returns the canonical bpfman.Program type
// with Record (from store) and Status (from kernel enumeration,
// filesystem checks, links, and maps with pin correlation).
func (m *Manager) Get(ctx context.Context, programID kernel.ProgramID) (bpfman.Program, error) {
	// Fetch program from store
	metadata, err := m.store.Get(ctx, programID)
	if err != nil {
		return bpfman.Program{}, err
	}

	// Fetch program from kernel
	kp, err := m.kernel.GetProgramByID(ctx, programID)
	if err != nil {
		return bpfman.Program{}, ErrProgramRequiresReconciliation{
			ProgramID: programID,
			Cause:     err,
		}
	}

	// Fetch links from store (records with details)
	storedLinks, err := m.store.ListLinksByProgram(ctx, programID)
	if err != nil {
		return bpfman.Program{}, fmt.Errorf("list links: %w", err)
	}

	bpffs := m.rt.BPFFS()
	scanner := bpffs.Scanner()
	bc := m.rt.Bytecode()

	// Build links with spec + status
	var links []bpfman.Link
	for _, sl := range storedLinks {
		// Fetch full record with details for this link
		record, err := m.store.GetLink(ctx, sl.ID)
		if err != nil {
			m.logger.WarnContext(ctx, "failed to get link details", "link_id", sl.ID, "error", err)
			record = sl // Use summary record without details
		}

		link := bpfman.Link{
			Record: record,
		}

		// Check pin presence from filesystem, not from record
		if record.PinPath != nil {
			link.Status.PinPresent = scanner.PathExists(record.PinPath.String())
		}

		// Fetch kernel link if bpfman captured a kernel link ID.
		if record.KernelLinkID != nil {
			kl, err := m.kernel.GetLinkByID(ctx, *record.KernelLinkID)
			if err == nil {
				link.Status.Kernel = &kl
				link.Status.KernelSeen = true
			}
		}

		links = append(links, link)
	}

	// Fetch each map from kernel using the program's map IDs
	var kernelMaps []kernel.Map
	for _, mapID := range kp.MapIDs {
		km, err := m.kernel.GetMapByID(ctx, mapID)
		if err != nil {
			// The map is in the program's map-id set but could not be
			// read back (revoked fd, permissions, transient). Omit it
			// rather than failing the whole get, but leave a breadcrumb
			// so a short map list is diagnosable.
			m.logger.DebugContext(ctx, "kernel map lookup failed, omitting from program maps", "map_id", mapID, "error", err)
			continue
		}
		kernelMaps = append(kernelMaps, km)
	}

	// Fetch stats (best-effort, don't fail if unavailable)
	var stats *kernel.ProgramStats
	if s, err := m.kernel.GetProgramStatsByID(ctx, programID); err == nil {
		stats = s
	}

	// Determine map owner for map path construction.
	mapOwner := programID
	if metadata.Handles.MapOwnerID != nil {
		mapOwner = *metadata.Handles.MapOwnerID
	}

	// Build map status with pin correlation. Derive map pins from
	// the filesystem directory rather than constructing paths from
	// kernel-truncated names. The kernel truncates map names to 15
	// characters, but pins use the full ELF section name.
	var mapStatuses []bpfman.MapStatus
	mapDir := bpffs.MapPinDir(mapOwner)
	if entries, err := os.ReadDir(mapDir.String()); err == nil {
		matched := make(map[int]bool)
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			pinPath := filepath.Join(mapDir.String(), name)

			ms := bpfman.MapStatus{
				PinPath: bpfman.MapPinPath(pinPath),
				Present: true,
			}

			// Correlate with kernel map: the kernel truncates
			// names to 15 chars, so the kernel name is a prefix
			// of the full ELF name.
			for i, km := range kernelMaps {
				if matched[i] {
					continue
				}
				if name == km.Name || strings.HasPrefix(name, km.Name) {
					ms.Map = km
					matched[i] = true
					break
				}
			}

			mapStatuses = append(mapStatuses, ms)
		}

		// Report kernel maps with no corresponding pin. Global-data
		// maps carry no pin path rather than an absent one reported as
		// missing: the load path never pins them, so there is nothing
		// to be missing.
		for i, km := range kernelMaps {
			if matched[i] {
				continue
			}
			if kernel.IsInternalMapName(km.Name) {
				mapStatuses = append(mapStatuses, bpfman.MapStatus{Map: km})
				continue
			}

			pinPath := bpffs.MapPinPath(mapOwner, km.Name)
			mapStatuses = append(mapStatuses, bpfman.MapStatus{
				Map:     km,
				PinPath: pinPath,
				Present: false,
			})
		}
	} else {
		// Directory unreadable or absent: fall back to constructing
		// paths from kernel names, with the same global-data exception.
		for _, km := range kernelMaps {
			if kernel.IsInternalMapName(km.Name) {
				mapStatuses = append(mapStatuses, bpfman.MapStatus{Map: km})
				continue
			}

			pinPath := bpffs.MapPinPath(mapOwner, km.Name)
			mapStatuses = append(mapStatuses, bpfman.MapStatus{
				Map:     km,
				PinPath: pinPath,
				Present: scanner.PathExists(pinPath.String()),
			})
		}
	}

	prog := bpfman.Program{
		Record: metadata,
		Status: bpfman.ProgramStatus{
			Kernel:   &kp,
			Stats:    stats,
			ProgPin:  bpffs.ProgPinPath(programID),
			MapDir:   bpffs.MapPinDir(mapOwner),
			Bytecode: bc.ProgramBytecodePath(programID),
			Links:    links,
			Maps:     mapStatuses,
		},
	}
	records, err := m.store.List(ctx)
	if err != nil {
		return bpfman.Program{}, fmt.Errorf("list programs for map users: %w", err)
	}

	prog.Status.MapUsedBy = inspect.MapSetMembers(records)[programID]
	return prog, nil
}

// Snapshot returns a point-in-time correlated view of every
// managed program, link, and dispatcher across the store,
// kernel, and bpf fs. Each row's Presence flags expose where
// the object is currently observable, so callers can detect
// orphans (InFS without InStore, etc.) without re-running the
// three enumerations themselves.
func (m *Manager) Snapshot(ctx context.Context) (*inspect.Observation, error) {
	scanner := m.rt.BPFFS().Scanner()
	return inspect.Snapshot(ctx, m.store, m.kernel, scanner)
}

// ListLinks returns all managed links (records only).
// Optional LinkListOption arguments filter the results.
func (m *Manager) ListLinks(ctx context.Context, opts ...bpfman.LinkListOption) ([]bpfman.LinkRecord, error) {
	links, err := m.store.ListLinks(ctx)
	if err != nil {
		return nil, err
	}

	filter := bpfman.ApplyLinkListOptions(opts...)

	// Initialise non-nil so an empty result is len()==0 rather than
	// nil. The shell binds this slice through ValueFromStruct, which
	// json.Marshals it; a nil slice would serialise as `null` and a
	// jq filter like `.links[]` would error on iteration. The wire
	// contract is "empty collection", not "absent".
	result := []bpfman.LinkRecord{}
	for _, link := range links {
		l := link // explicit copy
		if filter.Matches(&l) {
			result = append(result, link)
		}
	}
	return result, nil
}

// ListLinksScopedToPrograms lists links whose owning program matches the
// given program list options, then applies the link options. This is the
// Rust-faithful program-scoped link filter: --program-type, --application
// and --metadata-selector on `link list` select the owning program (its
// type and load-time metadata), and the result is that program's links.
// The link's own attach-time metadata is not the filter key.
func (m *Manager) ListLinksScopedToPrograms(ctx context.Context, programOpts []bpfman.ListOption, linkOpts []bpfman.LinkListOption) ([]bpfman.LinkRecord, error) {
	progs, err := m.ListPrograms(ctx, programOpts...)
	if err != nil {
		return nil, err
	}

	allowed := make(map[kernel.ProgramID]struct{}, len(progs))
	for _, p := range progs {
		allowed[p.Record.ProgramID] = struct{}{}
	}

	links, err := m.ListLinks(ctx, linkOpts...)
	if err != nil {
		return nil, err
	}

	result := []bpfman.LinkRecord{}
	for _, l := range links {
		if _, ok := allowed[l.ProgramID]; ok {
			result = append(result, l)
		}
	}
	return result, nil
}

// ListLinksByProgram returns all links for a given program.
func (m *Manager) ListLinksByProgram(ctx context.Context, programID kernel.ProgramID) ([]bpfman.LinkRecord, error) {
	return m.store.ListLinksByProgram(ctx, programID)
}

// ListDispatcherSummaries returns lightweight summaries of all dispatchers.
func (m *Manager) ListDispatcherSummaries(ctx context.Context) ([]platform.DispatcherSummary, error) {
	return m.store.ListDispatcherSummaries(ctx)
}

// GetDispatcherSnapshot retrieves the full dispatcher snapshot for the
// given key.
func (m *Manager) GetDispatcherSnapshot(ctx context.Context, key dispatcher.Key) (platform.DispatcherSnapshot, error) {
	return m.store.GetDispatcherSnapshot(ctx, key)
}

// DeleteDispatcherSnapshot removes a dispatcher and all its extension
// link records by attach point key.
func (m *Manager) DeleteDispatcherSnapshot(ctx context.Context, writeLock lock.WriterScope, key dispatcher.Key) error {
	return m.store.DeleteDispatcherSnapshot(ctx, key)
}

// GetLink retrieves a link by link ID, returning the full record with details.
func (m *Manager) GetLink(ctx context.Context, linkID bpfman.LinkID) (bpfman.LinkRecord, error) {
	record, err := m.getLink(ctx, linkID)
	if err != nil {
		return bpfman.LinkRecord{}, err
	}
	return record, nil
}

// GetLinkInfo retrieves a link with presence information across store, kernel, and filesystem.
func (m *Manager) GetLinkInfo(ctx context.Context, linkID bpfman.LinkID) (inspect.LinkInfo, error) {
	scanner := m.rt.BPFFS().Scanner()
	info, err := inspect.GetLink(ctx, m.store, m.kernel, scanner, linkID)
	if err != nil {
		return info, err
	}
	return info, nil
}

// ProgramName returns the stored name of a program by id. It reads only
// the store record, unlike Get, so presentation joins do not depend on
// live kernel state.
func (m *Manager) ProgramName(ctx context.Context, programID kernel.ProgramID) (string, error) {
	rec, err := m.store.Get(ctx, programID)
	if err != nil {
		return "", err
	}
	return rec.Meta.Name, nil
}

// FindLoadedProgramByMetadata finds a program by metadata key/value from
// the reconciled list of loaded programs (those in both DB and kernel).
//
// When multiple programs match (e.g. multi-program applications), this
// returns the match with the lowest program ID. The CSI publishes all
// requested maps from that one program's MapPinPath; missing maps are a
// clean CSI error, not a reason to infer a synthetic shared owner.
//
// The lowest-ID tiebreak is owned here rather than inherited from the
// snapshot's ordering, so CSI selection cannot silently change if that
// ordering ever does. It also matches Rust bpfman, whose CSI resolves
// the first match over the kernel's ascending-ID program iteration.
func (m *Manager) FindLoadedProgramByMetadata(ctx context.Context, key, value string) (bpfman.ProgramRecord, kernel.ProgramID, error) {
	scanner := m.rt.BPFFS().Scanner()
	obs, err := inspect.Snapshot(ctx, m.store, m.kernel, scanner)
	if err != nil {
		return bpfman.ProgramRecord{}, 0, fmt.Errorf("snapshot: %w", err)
	}

	// Find managed programs that are also in kernel and match the metadata
	var matches []inspect.ProgramView
	for _, row := range obs.Programs {
		if !row.Presence.InStore || !row.Presence.InKernel {
			continue
		}
		if row.Managed.Meta.Metadata[key] == value {
			matches = append(matches, row)
		}
	}

	switch len(matches) {
	case 0:
		return bpfman.ProgramRecord{}, 0, fmt.Errorf("program with %s=%s: %w", key, value, platform.ErrRecordNotFound)
	default:
		slices.SortFunc(matches, func(a, b inspect.ProgramView) int {
			return cmp.Compare(a.ProgramID, b.ProgramID)
		})
		m.logger.DebugContext(ctx, "found metadata match", "key", key, "value", value, "total_matches", len(matches), "program_id", matches[0].ProgramID, "program_name", matches[0].Managed.Meta.Name)
		return *matches[0].Managed, matches[0].ProgramID, nil
	}
}

// ListPrograms returns all managed programs with full spec and status,
// ordered deterministically by kernel ID (then type+name for ties).
// Optional ListOption arguments filter the results. This is the
// internal primitive used by delete, the link-scope filter and the
// gRPC server; the user-facing `program list` uses ListProgramEntries.
func (m *Manager) ListPrograms(ctx context.Context, opts ...bpfman.ListOption) ([]bpfman.Program, error) {
	filter := bpfman.ApplyListOptions(opts...)

	scanner := m.rt.BPFFS().Scanner()
	obs, err := inspect.Snapshot(ctx, m.store, m.kernel, scanner)
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}

	// Initialise non-nil so an empty result is len()==0 rather than
	// nil. Same wire-contract reasoning as ListLinks above.
	programs := []bpfman.Program{}
	for _, row := range obs.ManagedPrograms() {
		if prog, ok := row.AsProgram(); ok {
			p := prog // explicit copy for clarity
			if filter.Matches(&p) {
				// Enrich Status.Maps with kernel-side map metadata
				// (id, name, type, sizes). Mirrors what Manager.Load
				// does -- no filesystem pin correlation, that is
				// Manager.Get's job. A map that cannot be read back is
				// logged and dropped, same as Get.
				if p.Status.Kernel != nil {
					var kernelMaps []kernel.Map
					for _, mapID := range p.Status.Kernel.MapIDs {
						km, err := m.kernel.GetMapByID(ctx, mapID)
						if err != nil {
							m.logger.DebugContext(ctx, "kernel map lookup failed, omitting from program maps", "map_id", mapID, "error", err)
							continue
						}
						kernelMaps = append(kernelMaps, km)
					}
					p.Status.Maps = bpfman.ToMapStatus(kernelMaps)
				}
				programs = append(programs, p)
			}
		}
	}

	// Deterministic output ordering: by kernel ID, then by type+name for ties
	slices.SortFunc(programs, func(a, b bpfman.Program) int {
		if c := cmp.Compare(a.Record.ProgramID, b.Record.ProgramID); c != 0 {
			return c
		}
		// Fallback for zero IDs: sort by type, then name
		if c := cmp.Compare(a.Record.Load.ProgramType().String(), b.Record.Load.ProgramType().String()); c != 0 {
			return c
		}
		return cmp.Compare(a.Record.Meta.Name, b.Record.Meta.Name)
	})

	return programs, nil
}

// ListProgramEntries lists programs as summary entries for the user-
// facing `program list` command. Managed programs are always included;
// kernel-only programs (loaded in the kernel but not managed by
// bpfman) are included only when WithIncludeUnmanaged is set -- the
// `--all` surface. Each entry carries the common columns as top-level
// fields; managed entries also carry the full store Record, while
// kernel-only entries carry only the kernel observation and no Record.
//
// The snapshot already enumerates every kernel program and tags its
// presence, so including kernel-only rows reuses data already
// collected: there is no second kernel walk and no per-program lookup.
func (m *Manager) ListProgramEntries(ctx context.Context, opts ...bpfman.ListOption) (bpfman.ProgramListResult, error) {
	filter := bpfman.ApplyListOptions(opts...)

	scanner := m.rt.BPFFS().Scanner()
	obs, err := inspect.Snapshot(ctx, m.store, m.kernel, scanner)
	if err != nil {
		return bpfman.ProgramListResult{}, fmt.Errorf("snapshot: %w", err)
	}

	entries := []bpfman.ProgramListEntry{}
	for _, row := range obs.Programs {
		switch {
		case row.Presence.InStore:
			prog, ok := row.AsProgram()
			if !ok {
				continue
			}
			if filter.Matches(&prog) {
				entries = append(entries, managedProgramEntry(prog))
			}
		case filter.IncludeUnmanaged() && row.Presence.KernelOnly():
			if row.Kernel == nil {
				continue
			}
			if filter.MatchesKernelOnly(row.Kernel.ProgramType) {
				entries = append(entries, kernelOnlyProgramEntry(*row.Kernel))
			}
		}
	}

	slices.SortFunc(entries, func(a, b bpfman.ProgramListEntry) int {
		return cmp.Compare(a.ProgramID, b.ProgramID)
	})

	return bpfman.ProgramListResult{Programs: entries}, nil
}

// managedProgramEntry builds a list entry for a bpfman-managed program.
func managedProgramEntry(p bpfman.Program) bpfman.ProgramListEntry {
	rec := p.Record
	return bpfman.ProgramListEntry{
		ProgramID:    p.Record.ProgramID,
		Managed:      true,
		Application:  p.Record.Meta.Metadata[ApplicationMetadataKey],
		Type:         p.Record.Load.ProgramType().String(),
		FunctionName: programEntryFunctionName(p.Record.Meta.Name, p.Status.Kernel),
		Links:        linkIDs(p.Status.Links),
		Record:       &rec,
		Kernel:       p.Status.Kernel,
	}
}

// kernelOnlyProgramEntry builds a list entry for a kernel program that
// bpfman does not manage. It carries only the kernel observation; the
// managed Record is nil and there are no bpfman-tracked links.
func kernelOnlyProgramEntry(kp kernel.Program) bpfman.ProgramListEntry {
	k := kp
	return bpfman.ProgramListEntry{
		ProgramID:    kp.ID,
		Managed:      false,
		Type:         kp.ProgramType.String(),
		FunctionName: kp.Name,
		Links:        []bpfman.LinkID{},
		Record:       nil,
		Kernel:       &k,
	}
}

// programEntryFunctionName is the function name for a list entry: the
// stored ELF name when present (managed programs), falling back to the
// kernel name (the only value available for kernel-only programs).
func programEntryFunctionName(metaName string, k *kernel.Program) string {
	if metaName != "" {
		return metaName
	}
	if k != nil {
		return k.Name
	}
	return ""
}

// linkIDs extracts the bpfman link IDs from a program's links. It
// returns a non-nil empty slice when there are none so the wire shape
// is always a JSON array.
func linkIDs(links []bpfman.Link) []bpfman.LinkID {
	ids := make([]bpfman.LinkID, 0, len(links))
	for _, l := range links {
		ids = append(ids, l.Record.ID)
	}
	return ids
}

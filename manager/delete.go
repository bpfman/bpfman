package manager

import (
	"context"
	"fmt"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/lock"
)

// DeleteProgramsOpts configures program deletion.
type DeleteProgramsOpts struct {
	// Recursive, when true, also deletes the map-owner dependants of
	// each target program (the programs that share its maps via
	// map_owner_id), ordered dependant-first.
	Recursive bool

	// All, when true, selects every managed program as a target; the
	// explicit id list is ignored. It also implies dependant expansion.
	All bool
}

// DeleteProgramResult records the outcome for one requested program.
type DeleteProgramResult struct {
	// ProgramID is the program the result refers to.
	ProgramID kernel.ProgramID

	// Err is nil when the program was deleted (or was already removed
	// earlier in the batch as a dependant), otherwise it carries the
	// failure for this program.
	Err error
}

// DeleteLinksOpts configures link deletion.
type DeleteLinksOpts struct {
	// Recursive, when true and the detach leaves the owning program
	// without links, also deletes that orphaned program's map-owner
	// dependants before unloading it.
	Recursive bool
}

// DeleteLinkResult records the outcome for one requested link.
type DeleteLinkResult struct {
	// LinkID is the link the result refers to.
	LinkID bpfman.LinkID

	// Err is nil when the link was detached (or was already removed
	// earlier in the batch), otherwise it carries the failure for this
	// link.
	Err error
}

// ResolveDeleteProgramIDs resolves the user-facing delete target into
// concrete program IDs. When all is true, every managed program is selected.
func (m *Manager) ResolveDeleteProgramIDs(ctx context.Context, all bool, explicit []kernel.ProgramID) ([]kernel.ProgramID, error) {
	if !all {
		return append([]kernel.ProgramID(nil), explicit...), nil
	}

	result, err := m.ListPrograms(ctx)
	if err != nil {
		return nil, fmt.Errorf("list programs: %w", err)
	}

	ids := make([]kernel.ProgramID, len(result))
	for i, prog := range result {
		ids[i] = prog.Record.ProgramID
	}
	return ids, nil
}

// DeletePrograms detaches each target program's links and unloads the
// program. Selected programmes are ordered dependant-first so an owner and
// one of its selected dependants can be deleted in either argument order. With
// Recursive or All, unnamed dependants are also selected before their owners.
func (m *Manager) DeletePrograms(ctx context.Context, writeLock lock.WriterScope, ids []kernel.ProgramID, opts DeleteProgramsOpts) []DeleteProgramResult {
	results := make([]DeleteProgramResult, 0, len(ids))
	expandDependents := opts.Recursive || opts.All
	orderDependentsFirst := expandDependents || len(ids) > 1
	dependentsByOwner, err := m.programDependentsByOwner(ctx, orderDependentsFirst)
	if err != nil {
		for _, id := range ids {
			results = append(results, DeleteProgramResult{ProgramID: id, Err: err})
		}
		return results
	}

	batch := deleteBatch{
		dependentsByOwner: dependentsByOwner,
		removedPrograms:   make(map[kernel.ProgramID]bool, len(ids)),
		removedLinks:      make(map[bpfman.LinkID]bool),
	}

	for _, id := range orderProgramDeletes(ids, dependentsByOwner) {
		if batch.removedPrograms[id] {
			results = append(results, DeleteProgramResult{ProgramID: id})
			continue
		}
		_, err := m.deleteProgram(ctx, writeLock, id, expandDependents, batch)
		results = append(results, DeleteProgramResult{ProgramID: id, Err: err})
	}
	return results
}

// DeleteLinks detaches each link and unloads the owning program if the
// detach leaves it without remaining links. With Recursive, map-owner
// dependants of the orphaned program are deleted first.
func (m *Manager) DeleteLinks(ctx context.Context, writeLock lock.WriterScope, ids []bpfman.LinkID, opts DeleteLinksOpts) []DeleteLinkResult {
	results := make([]DeleteLinkResult, 0, len(ids))
	dependentsByOwner, err := m.programDependentsByOwner(ctx, opts.Recursive)
	if err != nil {
		for _, id := range ids {
			results = append(results, DeleteLinkResult{LinkID: id, Err: err})
		}
		return results
	}

	batch := deleteBatch{
		dependentsByOwner: dependentsByOwner,
		removedPrograms:   make(map[kernel.ProgramID]bool),
		removedLinks:      make(map[bpfman.LinkID]bool, len(ids)),
	}

	for _, id := range ids {
		if batch.removedLinks[id] {
			results = append(results, DeleteLinkResult{LinkID: id})
			continue
		}
		_, err := m.deleteLink(ctx, writeLock, id, opts.Recursive, batch)
		results = append(results, DeleteLinkResult{LinkID: id, Err: err})
	}
	return results
}

type deleteBatch struct {
	dependentsByOwner map[kernel.ProgramID][]kernel.ProgramID
	removedPrograms   map[kernel.ProgramID]bool
	removedLinks      map[bpfman.LinkID]bool
}

func (m *Manager) deleteLink(ctx context.Context, writeLock lock.WriterScope, linkID bpfman.LinkID, recursive bool, batch deleteBatch) (deleteOutcome, error) {
	if batch.removedLinks[linkID] {
		return deleteOutcome{}, nil
	}

	link, err := m.GetLink(ctx, linkID)
	if err != nil {
		return deleteOutcome{}, fmt.Errorf("get link: %w", err)
	}

	programID := link.ProgramID

	if err := m.Detach(ctx, writeLock, linkID); err != nil {
		return deleteOutcome{}, fmt.Errorf("detach: %w", err)
	}

	batch.removedLinks[linkID] = true

	links, err := m.ListLinksByProgram(ctx, programID)
	if err != nil {
		return deleteOutcome{}, fmt.Errorf("list links for program %d: %w", programID, err)
	}

	deleted := deleteOutcome{links: []bpfman.LinkID{linkID}}
	if len(links) == 0 {
		orphaned, err := m.deleteProgram(ctx, writeLock, programID, recursive, batch)
		deleted = deleted.merge(orphaned)
		if err != nil {
			return deleted, fmt.Errorf("delete orphaned program %d: %w", programID, err)
		}
	}

	return deleted, nil
}

type deleteOutcome struct {
	programs []kernel.ProgramID
	links    []bpfman.LinkID
}

func (d deleteOutcome) merge(other deleteOutcome) deleteOutcome {
	d.programs = append(d.programs, other.programs...)
	d.links = append(d.links, other.links...)
	return d
}

func (m *Manager) deleteProgram(ctx context.Context, writeLock lock.WriterScope, programID kernel.ProgramID, recursive bool, batch deleteBatch) (deleteOutcome, error) {
	if batch.removedPrograms[programID] {
		return deleteOutcome{}, nil
	}

	var deleted deleteOutcome
	if recursive {
		dependents, err := m.deleteDependents(ctx, writeLock, programID, batch)
		deleted = deleted.merge(dependents)
		if err != nil {
			return deleted, err
		}
	}

	links, err := m.ListLinksByProgram(ctx, programID)
	if err != nil {
		return deleted, fmt.Errorf("list links: %w", err)
	}

	for _, link := range links {
		if err := m.Detach(ctx, writeLock, link.ID); err != nil {
			return deleted, fmt.Errorf("detach link %d: %w", link.ID, err)
		}

		batch.removedLinks[link.ID] = true
		deleted.links = append(deleted.links, link.ID)
	}

	if err := m.Unload(ctx, writeLock, programID); err != nil {
		return deleted, fmt.Errorf("unload: %w", err)
	}

	batch.removedPrograms[programID] = true
	deleted.programs = append(deleted.programs, programID)
	return deleted, nil
}

func (m *Manager) programDependentsByOwner(ctx context.Context, recursive bool) (map[kernel.ProgramID][]kernel.ProgramID, error) {
	if !recursive {
		return nil, nil
	}

	result, err := m.ListPrograms(ctx)
	if err != nil {
		return nil, fmt.Errorf("list programs: %w", err)
	}

	dependentsByOwner := make(map[kernel.ProgramID][]kernel.ProgramID)
	for _, prog := range result {
		if prog.Record.Handles.MapOwnerID != nil {
			ownerID := *prog.Record.Handles.MapOwnerID
			dependentsByOwner[ownerID] = append(dependentsByOwner[ownerID], prog.Record.ProgramID)
		}
	}
	return dependentsByOwner, nil
}

func orderProgramDeletes(ids []kernel.ProgramID, dependentsByOwner map[kernel.ProgramID][]kernel.ProgramID) []kernel.ProgramID {
	if len(ids) <= 1 || len(dependentsByOwner) == 0 {
		return append([]kernel.ProgramID(nil), ids...)
	}

	selected := make(map[kernel.ProgramID]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}

	ordered := make([]kernel.ProgramID, 0, len(ids))
	seen := make(map[kernel.ProgramID]bool, len(ids))
	var visit func(kernel.ProgramID)
	visit = func(id kernel.ProgramID) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, dependentID := range dependentsByOwner[id] {
			if selected[dependentID] {
				visit(dependentID)
			}
		}
		ordered = append(ordered, id)
	}

	for _, id := range ids {
		visit(id)
	}
	return ordered
}

func (m *Manager) deleteDependents(ctx context.Context, writeLock lock.WriterScope, ownerID kernel.ProgramID, batch deleteBatch) (deleteOutcome, error) {
	var deleted deleteOutcome
	for _, dependentID := range batch.dependentsByOwner[ownerID] {
		if batch.removedPrograms[dependentID] {
			continue
		}
		dependents, err := m.deleteProgram(ctx, writeLock, dependentID, true, batch)
		deleted = deleted.merge(dependents)
		if err != nil {
			return deleted, fmt.Errorf("delete dependent program %d: %w", dependentID, err)
		}
	}

	return deleted, nil
}

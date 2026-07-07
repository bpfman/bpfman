package manager

import (
	"context"
	"fmt"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/lock"
	"github.com/bpfman/bpfman/manager/action"
	"github.com/bpfman/bpfman/manager/operation"
)

// Detach removes a link by link ID.
//
// This detaches the link from the kernel (if pinned) and removes it
// from the store. The associated program remains loaded.
//
// For XDP and TC links attached via dispatchers, the dispatcher link
// count is queried after the link is removed. If no extensions
// remain, the dispatcher is cleaned up automatically (pins removed,
// deleted from store).
//
// Preflight failures (store lookup, not-managed check, dispatcher key
// extraction) return plain errors.
func (m *Manager) Detach(ctx context.Context, writeLock lock.WriterScope, linkID bpfman.LinkID) error {
	_ = writeLock // reserved for symmetry with other mutating methods

	// Preflight: get link record.
	record, err := m.getLink(ctx, linkID)
	if err != nil {
		return err
	}

	// Preflight: extract dispatcher key for post-detach cleanup.
	var dispKey *dispatcher.Key
	if record.Kind == bpfman.LinkKindXDP || record.Kind == bpfman.LinkKindTC {
		dispType, nsid, ifindex, err := extractDispatcherKey(record.Details)
		if err != nil {
			return fmt.Errorf("extract dispatcher key: %w", err)
		}

		if dispType != (dispatcher.DispatcherType{}) {
			dispKey = &dispatcher.Key{Type: dispType, Nsid: nsid, Ifindex: ifindex}
		}
	}

	m.logger.InfoContext(ctx, "detaching link", "link_id", linkID, "kind", record.Kind, "pin_path", record.PinPath)

	plan := m.detachPlan(record, dispKey)
	if err := operation.Run0(ctx, m.logger, m.executor, plan); err != nil {
		return err
	}

	m.logger.InfoContext(ctx, "removed link", "link_id", linkID, "kind", record.Kind)
	return nil
}

// detachPlan builds the operation plan for detaching a single link.
//
// For non-dispatcher links:
//  1. (conditional) Do "detach-link" -- kernel detach via DetachLink.
//  2. Do "delete-link" -- store delete via DeleteLink.
//
// For dispatcher-backed links (XDP/TC):
//  1. (conditional) Do "detach-link" -- kernel detach via DetachLink.
//  2. Do "dispatcher-rebuild" -- rebuild dispatcher membership
//     excluding the detached link. The rebuild atomically replaces the
//     snapshot, which removes the old member rows as a side effect.
//
// No undo entries on any node. Detach is destructive and
// non-reversible.
func (m *Manager) detachPlan(
	record bpfman.LinkRecord, dispKey *dispatcher.Key,
) operation.Plan {
	var nodes []operation.Node
	target := fmt.Sprintf("%d", record.ID)

	if record.PinPath != nil {
		nodes = append(nodes, operation.DoAction("detach-link", target, action.DetachLink{PinPath: *record.PinPath}))
	}

	if dispKey != nil {
		// Dispatcher-backed link: route through dispatcher
		// rebuild which handles member removal atomically.
		nodes = append(nodes, operation.DoAction(
			"dispatcher-rebuild",
			fmt.Sprintf("%s:%d:%d", dispKey.Type, dispKey.Nsid, dispKey.Ifindex),
			action.RebuildDispatcherForDetach{Key: *dispKey, ExcludeLinkID: record.ID},
		))
	} else {
		// Non-dispatcher link: generic store deletion.
		nodes = append(nodes, operation.DoAction("delete-link", target, action.DeleteLink{LinkID: record.ID}))
	}

	return operation.Build(nodes...)
}

// extractDispatcherKey extracts dispatcher identification from link details.
// Returns empty dispType if the link type doesn't use dispatchers.
func extractDispatcherKey(details bpfman.LinkDetails) (dispType dispatcher.DispatcherType, nsid uint64, ifindex uint32, err error) {
	switch d := details.(type) {
	case bpfman.XDPDetails:
		return dispatcher.DispatcherTypeXDP, d.Nsid, d.Ifindex, nil
	case bpfman.TCDetails:
		switch d.Direction {
		case bpfman.TCDirectionIngress:
			return dispatcher.DispatcherTypeTCIngress, d.Nsid, d.Ifindex, nil
		case bpfman.TCDirectionEgress:
			return dispatcher.DispatcherTypeTCEgress, d.Nsid, d.Ifindex, nil
		default:
			return dispatcher.DispatcherType{}, 0, 0, fmt.Errorf("unknown TC direction: %s", d.Direction)
		}
	default:
		return dispatcher.DispatcherType{}, 0, 0, nil
	}
}

// collectDispatcherKeys examines links for dispatcher associations and
// returns a deduplicated set of dispatcher keys. The links must have
// their Details populated (as returned by ListLinksByProgram).
func collectDispatcherKeys(links []bpfman.LinkRecord) map[dispatcher.Key]struct{} {
	keys := make(map[dispatcher.Key]struct{})
	for _, link := range links {
		if link.Kind != bpfman.LinkKindTC && link.Kind != bpfman.LinkKindXDP {
			continue
		}
		dispType, nsid, ifindex, err := extractDispatcherKey(link.Details)
		if err != nil || dispType == (dispatcher.DispatcherType{}) {
			continue
		}
		keys[dispatcher.Key{Type: dispType, Nsid: nsid, Ifindex: ifindex}] = struct{}{}
	}
	return keys
}

// cleanupEmptyDispatchers checks each dispatcher in the set and
// removes any that no longer have extension links. Errors are logged
// but do not prevent cleanup of remaining dispatchers.
func (m *Manager) cleanupEmptyDispatchers(ctx context.Context, dispatchers map[dispatcher.Key]struct{}) {
	for key := range dispatchers {
		if err := m.executor.Execute(ctx, action.RemoveDispatcher{Key: key}); err != nil {
			m.logger.WarnContext(ctx, "dispatcher cleanup failed", "type", key.Type, "nsid", key.Nsid, "ifindex", key.Ifindex, "error", err)
		}
	}
}

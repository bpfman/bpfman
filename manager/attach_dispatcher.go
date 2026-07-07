package manager

import (
	"context"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/manager/action"
	"github.com/bpfman/bpfman/manager/operation"
)

// dispatcherAttachParams describes a dispatcher-based attach operation
// (XDP or TC). The closure constructs the type-specific rebuild
// action while the shared skeleton handles the plan structure and
// result extraction.
type dispatcherAttachParams struct {
	programID kernel.ProgramID
	ifindex   int
	ifname    string
	netnsPath string
	target    string // target (e.g., "eth0:xdp", "eth0:ingress")
	dispType  dispatcher.DispatcherType

	// rebuildAction constructs the Rebuild action given the
	// program record.
	rebuildAction func(prog bpfman.ProgramRecord) action.Action
}

// extensionResult bundles the completed link with dispatcher
// metadata from the rebuild that produced it. The link record has
// already been persisted by ReplaceDispatcherSnapshot.
type extensionResult struct {
	link     bpfman.Link
	key      dispatcher.Key
	revision uint32
	position int
	pinPath  bpfman.LinkPath
}

// Binding keys for dispatcherAttach plan nodes.
var (
	dispPreparedKey = operation.NewKey[dispPrepared]("disp-prepared")
	extResultKey    = operation.NewKey[extensionResult]("extension-result")
)

// dispPrepared bundles the program record fetched in node 1 for use
// by subsequent nodes.
type dispPrepared struct {
	prog bpfman.ProgramRecord
}

// dispatcherAttach implements the common skeleton for dispatcher-based
// attach types (XDP, TC).
//
// The operation triggers a full dispatcher rebuild (creating the
// dispatcher if none exists), attaches the user program as an
// extension, and persists the link metadata. All cross-subsystem
// complexity lives behind the rebuild executor action.
func (m *Manager) dispatcherAttach(ctx context.Context, p dispatcherAttachParams) (bpfman.Link, error) {
	plan := m.dispatcherAttachPlan(p)
	b, err := operation.Run(ctx, m.logger, m.executor, plan)
	if err != nil {
		return bpfman.Link{}, err
	}

	r := operation.Get(b, extResultKey)
	m.logger.InfoContext(ctx, "attached via dispatcher", "type", p.dispType, "link_id", r.link.Record.ID, "program_id", p.programID, "interface", p.ifname, "ifindex", p.ifindex, "nsid", r.key.Nsid, "position", r.position, "revision", r.revision, "pin_path", r.pinPath)

	return r.link, nil
}

// dispatcherAttachPlan builds the operation plan for a
// dispatcher-based attach.
//
// Nodes:
//  1. Produce dispPreparedKey -- fetch program record via executor.
//  2. Produce extResultKey -- RebuildXDPDispatcher or
//     RebuildTCDispatcher via executor, with undo that detaches the
//     link on failure. The rebuild saves all member links (including
//     the new extension) via ReplaceDispatcherSnapshot, so no
//     separate saveLinkNode is needed.
func (m *Manager) dispatcherAttachPlan(p dispatcherAttachParams) operation.Plan {
	return operation.Build(
		// Node 1: Fetch program record.
		operation.Produce(dispPreparedKey, p.target,
			func(ctx context.Context, exec action.Executor, _ *operation.Bindings) (dispPrepared, error) {
				prog, err := action.Produce[bpfman.ProgramRecord](ctx, exec, action.GetProgramFromStore{ProgramID: p.programID})
				if err != nil {
					return dispPrepared{}, err
				}
				return dispPrepared{prog: prog}, nil
			},
		),

		// Node 2: Rebuild dispatcher (creates if needed, attaches extension).
		operation.Produce(extResultKey, p.target,
			func(ctx context.Context, exec action.Executor, b *operation.Bindings) (extensionResult, error) {
				dp := operation.Get(b, dispPreparedKey)
				return action.Produce[extensionResult](ctx, exec, p.rebuildAction(dp.prog))
			},
			operation.UndoFrom(func(b *operation.Bindings) []action.Action {
				r := operation.Get(b, extResultKey)
				return []action.Action{
					action.DetachLink{PinPath: r.pinPath},
				}
			}),
		),
	)
}

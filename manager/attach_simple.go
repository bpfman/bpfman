package manager

import (
	"context"
	"fmt"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/manager/action"
	"github.com/bpfman/bpfman/manager/operation"
)

// Binding keys for simpleAttach plan nodes.
var (
	preparedKey    = operation.NewKey[attachPlan]("prepared")
	pendingLinkKey = operation.NewKey[bpfman.LinkRecord]("pending-link")
	attachOutKey   = operation.NewKey[bpfman.AttachOutput]("kernel-attach")
	linkKey        = operation.NewKey[bpfman.Link]("link")
)

// attachPlan captures the variable parts of a simple attach operation.
// Returned by the prepare closure after inspecting the program record.
type attachPlan struct {
	// target is the recording target (e.g., "sched/sched_switch").
	target string
	// details is the sealed LinkDetails value for the link record.
	details bpfman.LinkDetails
	// attachAction constructs the kernel attach action for the given link pin path.
	attachAction func(linkPinPath bpfman.LinkPath) action.Action
}

// attachParams describes a non-dispatcher attach operation.
type attachParams struct {
	// programID is the kernel ID of the program to attach.
	programID kernel.ProgramID
	// metadata holds user-supplied key/value link labels to persist on
	// the link record. nil when the caller supplied none.
	metadata map[string]string
	// defaultTarget is used for plan node labels. The actual target
	// may differ once the program record is fetched (e.g., fentry
	// resolves the function name from the record).
	defaultTarget string
	// prepare inspects the program record and returns the plan.
	// progPinPath is the program's bpffs pin path, precomputed
	// from the kernel ID.
	prepare func(prog bpfman.ProgramRecord, progPinPath bpfman.ProgPinPath) (attachPlan, error)
}

// simpleAttach implements the common skeleton for non-dispatcher attach
// types (tracepoint, kprobe, uprobe, fentry, fexit).
//
// It builds a plan via simpleAttachPlan and delegates to
// operation.Run. The plan interpreter walks each node in sequence,
// executing I/O through the executor as reified actions and
// accumulating undo actions for automatic rollback on failure. Node
// closures never call store or kernel methods directly; they
// construct action values and hand them to the executor.
func (m *Manager) simpleAttach(ctx context.Context, p attachParams) (bpfman.Link, error) {
	plan := m.simpleAttachPlan(p)
	b, err := operation.Run(ctx, m.logger, m.executor, plan)
	if err != nil {
		return bpfman.Link{}, err
	}

	pa := operation.Get(b, preparedKey)
	link := operation.Get(b, linkKey)
	m.logger.InfoContext(ctx, "attached", "link_id", link.Record.ID, "program_id", p.programID, "target", pa.target, "pin_path", link.Record.PinPath)

	return link, nil
}

// simpleAttachPlan builds the operation plan for a simple attach.
//
// Nodes:
//  1. Produce preparedKey -- fetch program record, run prepare.
//  2. Produce pendingLinkKey -- create the link record first,
//     allocating the bpfman link ID and recording the pin path
//     links/{link_id} in the same transaction; undo deletes the
//     record. The pin name is the numeric link ID because bpffs
//     rejects dots in path components and symbol-derived names
//     contain them. Recording the pin path before the kernel attach
//     means a crash at any point leaves a row whose pin path
//     cleanup can detach -- never a pinned link the store does not
//     name.
//  3. Produce attachOutKey -- kernel attach via action, pinning at
//     the recorded path, with undo that detaches.
//  4. Produce linkKey -- finalise the record with the captured
//     kernel link ID.
func (m *Manager) simpleAttachPlan(p attachParams) operation.Plan {
	return operation.Build(
		operation.Produce(preparedKey, p.defaultTarget,
			func(ctx context.Context, exec action.Executor, _ *operation.Bindings) (attachPlan, error) {
				prog, err := action.Produce[bpfman.ProgramRecord](ctx, exec, action.GetProgramFromStore{ProgramID: p.programID})
				if err != nil {
					return attachPlan{}, err
				}

				progPinPath := m.rt.BPFFS().ProgPinPath(p.programID)
				return p.prepare(prog, progPinPath)
			},
		),

		operation.Produce(pendingLinkKey, p.defaultTarget,
			func(ctx context.Context, exec action.Executor, b *operation.Bindings) (bpfman.LinkRecord, error) {
				pa := operation.Get(b, preparedKey)
				spec := bpfman.LinkSpec{
					ProgramID: p.programID,
					Kind:      pa.details.Kind(),
					Details:   pa.details,
					Metadata:  p.metadata,
				}
				return action.Produce[bpfman.LinkRecord](ctx, exec, action.CreatePendingLink{
					Spec:     spec,
					LinksDir: m.rt.BPFFS().Links(),
				})
			},
			operation.UndoFrom(func(b *operation.Bindings) []action.Action {
				record := operation.Get(b, pendingLinkKey)
				return []action.Action{
					action.DeleteLink{LinkID: record.ID},
				}
			}),
		),

		operation.Produce(attachOutKey, p.defaultTarget,
			func(ctx context.Context, exec action.Executor, b *operation.Bindings) (bpfman.AttachOutput, error) {
				pa := operation.Get(b, preparedKey)
				record := operation.Get(b, pendingLinkKey)
				return action.Produce[bpfman.AttachOutput](ctx, exec, pa.attachAction(*record.PinPath))
			},
			operation.UndoFrom(func(b *operation.Bindings) []action.Action {
				record := operation.Get(b, pendingLinkKey)
				return []action.Action{
					action.DetachLink{PinPath: *record.PinPath},
				}
			}),
		),

		operation.Produce(linkKey, p.defaultTarget,
			func(ctx context.Context, exec action.Executor, b *operation.Bindings) (bpfman.Link, error) {
				pa := operation.Get(b, preparedKey)
				record := operation.Get(b, pendingLinkKey)
				out := operation.Get(b, attachOutKey)
				finalised, err := action.Produce[bpfman.LinkRecord](ctx, exec, action.FinaliseLink{
					LinkID:       record.ID,
					KernelLinkID: out.KernelLinkID,
				})
				if err != nil {
					return bpfman.Link{}, fmt.Errorf("save link metadata: %w", err)
				}

				finalised.Details = pa.details
				return bpfman.Link{
					Record: finalised,
					Status: bpfman.LinkStatus{
						Kernel:     out.KernelLink,
						KernelSeen: out.KernelLink != nil,
						PinPresent: out.PinPath != "",
					},
				}, nil
			},
		),
	)
}

// saveLinkNode builds a Produce node that constructs a LinkSpec,
// persists it via CreateLink, and returns the completed bpfman.Link.
// The extract closure reads bindings to supply the two variable parts
// -- the link details and the kernel attach output -- from which the
// LinkSpec takes the kernel link ID, kind, and pin path.
func saveLinkNode(
	programID kernel.ProgramID,
	metadata map[string]string,
	target string,
	extract func(*operation.Bindings) (bpfman.LinkDetails, bpfman.AttachOutput),
) operation.Node {
	return operation.Produce(linkKey, target,
		func(ctx context.Context, exec action.Executor, b *operation.Bindings) (bpfman.Link, error) {
			details, out := extract(b)
			spec := bpfman.LinkSpec{
				ProgramID:    programID,
				KernelLinkID: out.KernelLinkID,
				Kind:         details.Kind(),
				PinPath:      bpfman.NewLinkPath(out.PinPath),
				Details:      details,
				Metadata:     metadata,
			}
			record, err := action.Produce[bpfman.LinkRecord](ctx, exec, action.CreateLink{Spec: spec})
			if err != nil {
				return bpfman.Link{}, fmt.Errorf("save link metadata: %w", err)
			}

			link := bpfman.Link{
				Record: record,
				Status: bpfman.LinkStatus{
					Kernel:     out.KernelLink,
					KernelSeen: out.KernelLink != nil,
					PinPresent: out.PinPath != "",
				},
			}
			return link, nil
		},
	)
}

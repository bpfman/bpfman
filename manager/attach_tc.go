package manager

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/manager/action"
	"github.com/bpfman/bpfman/manager/operation"
	"github.com/bpfman/bpfman/ns/netns"
)

// DefaultTCProceedOn is the default bitmask for TC proceed-on actions.
// This matches the Rust bpfman default: Pipe and DispatcherReturn.
// TC_ACT_OK is deliberately excluded because it means "accept and
// stop" in standard TC semantics; programs that want chain
// continuation should return TC_ACT_PIPE.
var DefaultTCProceedOn = mustProceedOnMask(dispatcher.DispatcherTypeTCIngress, bpfman.TCActionPipe.Int32(), bpfman.TCActionDispatcherReturn.Int32())

func mustProceedOnMask(dt dispatcher.DispatcherType, codes ...int32) uint32 {
	mask, err := dispatcher.ProceedOnMask(dt, codes...)
	if err != nil {
		panic(err)
	}
	return mask
}

// attachTC attaches a TC program to a network interface using the
// dispatcher model for multi-program chaining.
//
// Every attach triggers a full dispatcher rebuild: a new dispatcher
// is loaded with updated .rodata config, all extensions are re-attached,
// and the TC filter is atomically swapped (or created for first attach).
//
// Pin paths follow the Rust bpfman convention:
//   - Dispatcher prog: /sys/fs/bpf/bpfman/tc-{direction}/dispatcher_{nsid}_{ifindex}_{revision}/dispatcher
//   - Extension links: /sys/fs/bpf/bpfman/tc-{direction}/dispatcher_{nsid}_{ifindex}_{revision}/link_{position}
func (m *Manager) attachTC(ctx context.Context, spec bpfman.TCAttachSpec) (bpfman.Link, error) {
	ifname := spec.Ifname()
	direction := spec.Direction()
	priority := spec.Priority()
	proceedOn := spec.ProceedOn()
	netnsPath := spec.Netns()
	ifindex, err := m.kernel.InterfaceByName(ctx, ifname, netnsPath)
	if err != nil {
		return bpfman.Link{}, err
	}

	var dispType dispatcher.DispatcherType
	if direction == bpfman.TCDirectionIngress {
		dispType = dispatcher.DispatcherTypeTCIngress
	} else {
		dispType = dispatcher.DispatcherTypeTCEgress
	}

	if len(proceedOn) == 0 {
		proceedOn = []int32{
			bpfman.TCActionPipe.Int32(),
			bpfman.TCActionDispatcherReturn.Int32(),
		}
	}
	proceedOnMask, err := dispatcher.ProceedOnMask(dispType, proceedOn...)
	if err != nil {
		return bpfman.Link{}, fmt.Errorf("encode TC proceed-on: %w", err)
	}

	return m.dispatcherAttach(ctx, dispatcherAttachParams{
		programID: spec.ProgramID(),
		ifindex:   ifindex,
		ifname:    ifname,
		netnsPath: netnsPath,
		target:    ifname + ":" + direction.String(),
		dispType:  dispType,
		rebuildAction: func(prog bpfman.ProgramRecord) action.Action {
			return action.RebuildTCDispatcher{
				ProgramID:   spec.ProgramID(),
				Ifindex:     uint32(ifindex),
				Ifname:      ifname,
				Direction:   direction,
				DispType:    dispType,
				NetnsPath:   netnsPath,
				ProgPinPath: prog.Handles.PinPath,
				ProgramName: prog.Meta.Name,
				Priority:    priority,
				ProceedOn:   proceedOnMask,
				Metadata:    spec.Metadata(),
			}
		},
	})
}

// attachTCX attaches a TCX program to a network interface using native
// kernel multi-program support. Unlike TC, TCX doesn't use dispatchers.
//
// Pin paths follow the convention:
//   - Link: /sys/fs/bpf/bpfman/tcx-{direction}/link_{nsid}_{ifindex}_{linkid}
//
// Preflight failures (getProgram, type check, NSID, stale pin
// removal, link listing) return plain errors.

func (m *Manager) attachTCX(ctx context.Context, spec bpfman.TCXAttachSpec) (bpfman.Link, error) {
	// --- Preflight (outside plan, plain errors) ---
	programID := spec.ProgramID()
	ifname := spec.Ifname()
	direction := spec.Direction()
	priority := spec.Priority()
	netnsPath := spec.Netns()
	target := ifname + ":" + direction.String()

	ifindex, err := m.kernel.InterfaceByName(ctx, ifname, netnsPath)
	if err != nil {
		return bpfman.Link{}, err
	}

	// Manager.Attach has already verified the program is a tcx program
	// (before any side effect); here we only need its pin path.
	prog, err := m.getProgram(ctx, programID)
	if err != nil {
		return bpfman.Link{}, err
	}

	nsid, err := netns.NSID(netnsPath)
	if err != nil {
		return bpfman.Link{}, fmt.Errorf("get nsid: %w", err)
	}

	linkPinPath := m.rt.BPFFS().TCXLinkPath(direction.String(), nsid, uint32(ifindex), programID)

	progPinPath := prog.Handles.PinPath
	existingLinks, err := m.store.ListTCXLinksByInterface(ctx, nsid, uint32(ifindex), direction.String())
	if err != nil {
		return bpfman.Link{}, fmt.Errorf("list existing TCX links: %w", err)
	}

	// Duplicate attach is rejected, not replaced. The pin path is
	// keyed by (direction, nsid, ifindex, program), so a second
	// attach of the same program to the same hook would share the
	// first's pin: the stale-pin preflight below would detach the
	// LIVE kernel link, and the two store records would cross-wire
	// every later detach. A store record for this program on this
	// hook is the proof of a live managed attachment; the kernel's
	// mprog layer gives Rust the equivalent rejection via EEXIST.
	for _, l := range existingLinks {
		if l.KernelProgramID == programID {
			return bpfman.Link{}, fmt.Errorf("program %d is already attached to %s %s as link %d; detach it first",
				programID, ifname, direction, l.LinkID)
		}
	}

	// Stale pin removal (preflight I/O). The duplicate check above
	// proves no managed attachment owns this pin, so anything at the
	// path is crash residue. Use DetachLink, not RemovePin: if a
	// previous attach crashed leaving a live kernel link behind
	// the bpffs pin, raw os.Remove drops only the userland reference
	// and the netdev keeps running the program until RCU teardown
	// completes. DetachLink performs BPF_LINK_DETACH first, so the
	// kernel link is provably gone before we touch the pin file.
	if err := m.executor.Execute(ctx, action.DetachLink{PinPath: linkPinPath}); err != nil {
		return bpfman.Link{}, fmt.Errorf("detach stale TCX link %s: %w", linkPinPath, err)
	}

	// The attach order anchors the new program relative to an existing
	// one by kernel program ID. The store can outlive the kernel objects
	// it records -- a daemon restart, an external unload, or a
	// ClusterBpfApplication deleted and recreated all leave link rows
	// whose programs are gone. Anchoring against such a dead program ID
	// makes the kernel reject the attach with ENOENT, and every reconcile
	// recomputes the same dead anchor, so the interface never recovers.
	// Treat the kernel as the source of truth and the store as a hint:
	// drop links whose program is no longer live before computing the
	// order. computeTCXAttachOrder falls back to Head when none remain.
	liveLinks := filterLiveTCXLinks(existingLinks, func(id kernel.ProgramID) bool {
		_, kerr := m.kernel.GetProgramByID(ctx, id)
		switch {
		case kerr == nil:
			return true // live anchor; keep
		case errors.Is(kerr, os.ErrNotExist):
			m.logger.WarnContext(ctx, "ignoring stale TCX link whose anchor program is not in the kernel", "interface", ifname, "direction", direction.String(), "dead_program_id", id)
			return false // confirmed dead; drop
		default:
			// Inconclusive lookup (transient, EPERM, fd exhaustion).
			// Do not drop a possibly-live anchor on a kernel hiccup;
			// keep it and let the attach surface any real failure.
			m.logger.WarnContext(ctx, "inconclusive kernel lookup for TCX anchor; keeping link", "interface", ifname, "direction", direction.String(), "program_id", id, "error", kerr)
			return true
		}
	})
	order := computeTCXAttachOrder(liveLinks, int32(priority))

	m.logger.DebugContext(ctx, "computed TCX attach order", "program_id", programID, "priority", priority, "existing_links", len(existingLinks), "live_links", len(liveLinks), "order", order)

	// --- Build and execute plan ---
	plan := m.attachTCXPlan(programID, spec.Metadata(), ifindex, ifname, direction, priority, nsid, netnsPath, linkPinPath, progPinPath, target, order)
	b, err := operation.Run(ctx, m.logger, m.executor, plan)
	if err != nil {
		return bpfman.Link{}, err
	}

	link := operation.Get(b, linkKey)
	m.logger.InfoContext(ctx, "attached TCX program", "link_id", link.Record.ID, "program_id", programID, "interface", ifname, "direction", direction, "ifindex", ifindex, "nsid", nsid, "priority", priority, "pin_path", linkPinPath)

	return link, nil
}

// attachTCXPlan builds the operation plan for a TCX attach.
//
// Nodes:
//  1. Produce attachOutKey -- kernel attach via AttachTCX, with undo
//     that detaches the link on failure.
//  2. Produce linkKey -- construct link record, save to store.
func (m *Manager) attachTCXPlan(
	programID kernel.ProgramID, metadata map[string]string, ifindex int, ifname string,
	direction bpfman.TCDirection, priority int, nsid uint64,
	netnsPath string, linkPinPath bpfman.LinkPath, progPinPath bpfman.ProgPinPath, target string,
	order bpfman.TCXAttachOrder,
) operation.Plan {
	return operation.Build(
		operation.Produce(attachOutKey, target,
			func(ctx context.Context, exec action.Executor, _ *operation.Bindings) (bpfman.AttachOutput, error) {
				return action.Produce[bpfman.AttachOutput](ctx, exec, action.AttachTCX{
					Ifindex:     ifindex,
					Direction:   direction.String(),
					ProgPinPath: progPinPath,
					LinkPinPath: linkPinPath,
					NetnsPath:   netnsPath,
					Order:       order,
				})
			},
			operation.UndoFrom(func(_ *operation.Bindings) []action.Action {
				return []action.Action{
					action.DetachLink{PinPath: linkPinPath},
				}
			}),
		),

		saveLinkNode(programID, metadata, target, func(b *operation.Bindings) (bpfman.LinkDetails, bpfman.AttachOutput) {
			out := operation.Get(b, attachOutKey)
			return bpfman.TCXDetails{
				Interface: ifname,
				Ifindex:   uint32(ifindex),
				Direction: direction,
				Priority:  int32(priority),
				Netns:     netnsPath,
				Nsid:      nsid,
			}, out
		}),
	)
}

// filterLiveTCXLinks returns the subset of links whose program is still
// live in the kernel, as reported by isLive. isLive is queried at most
// once per distinct kernel program ID. It exists because the link store
// can outlive the kernel objects it records: anchoring a new TCX attach
// against a program that has since been unloaded makes the kernel reject
// the attach with ENOENT. See attachTCX for the full rationale.
func filterLiveTCXLinks(links []bpfman.TCXLinkInfo, isLive func(kernel.ProgramID) bool) []bpfman.TCXLinkInfo {
	live := make([]bpfman.TCXLinkInfo, 0, len(links))
	checked := make(map[kernel.ProgramID]bool, len(links))
	for _, l := range links {
		ok, seen := checked[l.KernelProgramID]
		if !seen {
			ok = isLive(l.KernelProgramID)
			checked[l.KernelProgramID] = ok
		}
		if ok {
			live = append(live, l)
		}
	}
	return live
}

// computeTCXAttachOrder determines where to insert a new TCX program in the chain
// based on its priority relative to existing programs. Lower priority values run first.
//
// The algorithm:
// 1. If no existing links, attach at head (first)
// 2. Find the first existing link with priority > newPriority, attach before it
// 3. If all existing links have priority <= newPriority, attach after the last one
//
// This ensures programs are ordered by priority, with ties broken by insertion order.
func computeTCXAttachOrder(existingLinks []bpfman.TCXLinkInfo, newPriority int32) bpfman.TCXAttachOrder {
	if len(existingLinks) == 0 {
		// No existing links, attach at head
		return bpfman.TCXAttachFirst()
	}

	// Links are already sorted by priority ASC from the query
	// Find the first link with higher priority (should come after us)
	for _, link := range existingLinks {
		if link.Priority > newPriority {
			// This link has higher priority (runs later), we should attach before it
			return bpfman.TCXAttachBefore(link.KernelProgramID)
		}
	}

	// All existing links have priority <= ours, attach after the last one
	lastLink := existingLinks[len(existingLinks)-1]
	return bpfman.TCXAttachAfter(lastLink.KernelProgramID)
}

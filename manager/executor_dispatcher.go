// executor_dispatcher.go contains the full-rebuild dispatcher logic.
//
// Every XDP/TC attach or detach that changes the set of extensions
// triggers a full dispatcher rebuild: a new dispatcher program is
// loaded with updated .rodata config, all extensions are re-attached
// to it, and the link (XDP) or filter (TC) is atomically swapped.
// This matches the upstream Rust bpfman approach.

package manager

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/internal/tcpolicy"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/manager/action"
	"github.com/bpfman/bpfman/ns/netns"
	"github.com/bpfman/bpfman/platform"
)

// rebuildSlot carries per-extension data for the rebuild.
type rebuildSlot struct {
	ProgPinPath    bpfman.ProgPinPath
	ProgramName    string
	Priority       int // user-specified priority (may be 0 for unspecified)
	ProceedOn      uint32
	ExistingLinkID *bpfman.LinkID
	ProgramID      kernel.ProgramID  // managed program's kernel ID
	Ifname         string            // interface name from detail record
	Metadata       map[string]string // user link labels; new slot from spec, existing slots preserved from the snapshot
}

// xdpRebuildOps returns the type-specific operations for XDP
// dispatcher rebuild.
type xdpRebuildOps struct {
	ifindex   uint32
	ifname    string
	netnsPath string
}

// tcRebuildOps returns the type-specific operations for TC
// dispatcher rebuild.
type tcRebuildOps struct {
	ifindex   uint32
	ifname    string
	direction bpfman.TCDirection
	dispType  dispatcher.DispatcherType
	netnsPath string
}

// attachedExt records one extension attached during a rebuild: its
// attach output, chain position, and link pin path. The rebuild
// functions collect these to build the new snapshot's members and to
// roll back on failure.
type attachedExt struct {
	out      bpfman.AttachOutput
	position int
	pinPath  bpfman.LinkPath
}

// dispatcherMembers builds the snapshot member list from the rebuilt
// slots and the extensions attached for them, pairing each slot with the
// kernel link and pin from its attach at the same index.
func dispatcherMembers(slots []rebuildSlot, attached []attachedExt) []platform.DispatcherMemberSpec {
	members := make([]platform.DispatcherMemberSpec, len(slots))
	for i, slot := range slots {
		members[i] = platform.DispatcherMemberSpec{
			ExistingLinkID: slot.ExistingLinkID,
			ProgramID:      slot.ProgramID,
			ProgramName:    slot.ProgramName,
			ProgPinPath:    slot.ProgPinPath,
			KernelLinkID:   attached[i].out.KernelLinkID,
			LinkPinPath:    attached[i].pinPath,
			Position:       i,
			Priority:       slot.Priority,
			ProceedOn:      slot.ProceedOn,
			Ifname:         slot.Ifname,
			Metadata:       slot.Metadata,
		}
	}
	return members
}

// rebuildXDPDispatcher performs a full XDP dispatcher rebuild.
// It handles both first-attach (no dispatcher exists) and
// subsequent-attach (dispatcher exists, rebuild all extensions).
// The managedProgramID identifies the user program being attached.
func (e *executor) rebuildXDPDispatcher(
	ctx context.Context,
	managedProgramID kernel.ProgramID,
	ops xdpRebuildOps,
	progPinPath bpfman.ProgPinPath,
	programName string,
	priority int,
	proceedOn uint32,
	metadata map[string]string,
) (extensionResult, error) {
	newSlot := rebuildSlot{
		ProgPinPath: progPinPath,
		ProgramName: programName,
		Priority:    priority,
		ProceedOn:   proceedOn,
		ProgramID:   managedProgramID,
		Ifname:      ops.ifname,
		Metadata:    metadata,
	}

	nsid, err := netns.NSID(ops.netnsPath)
	if err != nil {
		return extensionResult{}, fmt.Errorf("get nsid: %w", err)
	}

	dispType := dispatcher.DispatcherTypeXDP
	key := dispatcher.Key{Type: dispType, Nsid: nsid, Ifindex: ops.ifindex}

	// Query existing dispatcher snapshot (may not exist).
	snap, err := e.store.GetDispatcherSnapshot(ctx, key)
	firstAttach := false
	if err != nil {
		if !isNotFound(err) {
			return extensionResult{}, fmt.Errorf("get dispatcher: %w", err)
		}
		firstAttach = true
	}

	// Build the full set of extensions (existing + new).
	allSlots := make([]rebuildSlot, 0, len(snap.Members)+1)
	for _, m := range snap.Members {
		linkID := m.LinkID
		allSlots = append(allSlots, rebuildSlot{
			ProgPinPath:    m.ProgPinPath,
			ProgramName:    m.ProgramName,
			Priority:       m.Priority,
			ProceedOn:      m.ProceedOn,
			ExistingLinkID: &linkID,
			ProgramID:      m.ProgramID,
			Ifname:         m.Ifname,
			Metadata:       m.Metadata,
		})
	}
	allSlots = append(allSlots, newSlot)

	if len(allSlots) > dispatcher.MaxPrograms {
		return extensionResult{}, fmt.Errorf("no free dispatcher slots (all %d occupied)", dispatcher.MaxPrograms)
	}

	// Sort by (priority, programName) to determine positions.
	sortRebuildSlots(allSlots)

	// Compute .rodata config.
	cfg, err := dispatcher.NewXDPConfig(len(allSlots))
	if err != nil {
		return extensionResult{}, fmt.Errorf("create XDP dispatcher config: %w", err)
	}
	for i, slot := range allSlots {
		cfg.ChainCallActions[i] = slot.ProceedOn
	}

	// Compute new revision.
	var revision uint32
	if firstAttach {
		revision = 1
	} else {
		revision = snap.Revision + 1
	}

	dispProgPinPath := e.bpffs.DispatcherProgPath(dispType, nsid, ops.ifindex, revision)

	e.logger.InfoContext(ctx, "rebuilding XDP dispatcher", "nsid", nsid, "ifindex", ops.ifindex, "revision", revision, "num_extensions", len(allSlots), "first_attach", firstAttach)

	// Load new dispatcher with .rodata config.
	dispatcherID, err := e.kernel.LoadAndPinXDPDispatcher(ctx, cfg, dispProgPinPath)
	if err != nil {
		return extensionResult{}, fmt.Errorf("load XDP dispatcher: %w", err)
	}

	// Track cleanup for rollback on failure.
	cleanupNewDispatcher := func() {
		if rbErr := e.kernel.RemovePin(ctx, dispProgPinPath); rbErr != nil {
			e.logger.ErrorContext(ctx, "rollback: remove new dispatcher pin failed", "path", dispProgPinPath, "error", rbErr)
		}
	}

	// Attach all extensions to the new dispatcher.
	attached := make([]attachedExt, 0, len(allSlots))

	cleanupExtensions := func() {
		for _, ext := range attached {
			if ext.pinPath != "" {
				if rbErr := e.kernel.DetachLink(ctx, ext.pinPath); rbErr != nil {
					e.logger.ErrorContext(ctx, "rollback: detach extension link failed", "path", ext.pinPath, "error", rbErr)
				}
			}
		}
	}

	// Find which slot is the new one (the one we're adding).
	newSlotPosition := -1
	for i, slot := range allSlots {
		linkPinPath := e.bpffs.ExtensionLinkPath(dispType, nsid, ops.ifindex, revision, i)

		out, err := e.kernel.AttachXDPExtension(ctx, dispatcher.XDPExtensionAttachSpec{
			DispatcherPinPath: dispProgPinPath,
			ProgPinPath:       slot.ProgPinPath,
			ProgramName:       slot.ProgramName,
			Position:          i,
			LinkPinPath:       linkPinPath,
		})
		if err != nil {
			cleanupExtensions()
			cleanupNewDispatcher()
			return extensionResult{}, fmt.Errorf("attach XDP extension %s at position %d: %w", slot.ProgramName, i, err)
		}

		attached = append(attached, attachedExt{out: out, position: i, pinPath: linkPinPath})

		if slot.ExistingLinkID == nil {
			newSlotPosition = i
		}
	}

	// Diagnostic: read each just-pinned freplace link back via
	// BPF_LINK_GET_INFO_BY_FD. This forces a syscall round-trip per
	// link before the dispatcher swap, giving the kernel another
	// chance to publish trampoline state, and surfaces any slot
	// whose target_obj_id does not match the new dispatcher (which
	// would mean the freplace is bound to a stale program). Errors
	// are logged but do not abort the rebuild.
	for _, ext := range attached {
		info, infoErr := e.kernel.ExtensionLinkInfo(ctx, ext.pinPath)
		if infoErr != nil {
			e.logger.WarnContext(ctx, "verify: extension link info failed", "type", dispType.String(), "ifindex", ops.ifindex, "revision", revision, "position", ext.position, "path", ext.pinPath, "error", infoErr)
			continue
		}

		e.logger.InfoContext(ctx, "verify: extension link", "type", dispType.String(), "ifindex", ops.ifindex, "revision", revision, "position", ext.position, "kernel_link_id", uint64(info.KernelLinkID), "target_prog_id", uint64(info.TargetProgID), "target_btf_id", info.TargetBtfID, "attach_type", info.AttachType, "matches_dispatcher", uint64(info.TargetProgID) == uint64(dispatcherID))
	}

	// Atomic swap: create link (first-attach) or update existing link.
	dispLinkPinPath := e.bpffs.DispatcherLinkPath(dispType, nsid, ops.ifindex)
	var linkID kernel.LinkID

	if firstAttach {
		result, err := e.kernel.CreateXDPLink(ctx, dispProgPinPath, int(ops.ifindex), dispLinkPinPath, ops.netnsPath)
		if err != nil {
			cleanupExtensions()
			cleanupNewDispatcher()
			return extensionResult{}, fmt.Errorf("create XDP link: %w", err)
		}
		linkID = result.KernelLinkID
	} else {
		if err := e.kernel.UpdateXDPDispatcherLink(ctx, dispLinkPinPath, dispProgPinPath); err != nil {
			cleanupExtensions()
			cleanupNewDispatcher()
			return extensionResult{}, fmt.Errorf("update XDP dispatcher link: %w", err)
		}
		if snap.Runtime.KernelLinkID != nil {
			linkID = *snap.Runtime.KernelLinkID
		}
	}

	// Build snapshot with all members and persist atomically.
	newSnap := platform.DispatcherSnapshotSpec{
		Key:      key,
		Revision: revision,
		Runtime: platform.DispatcherRuntime{
			ProgramID:    dispatcherID,
			KernelLinkID: &linkID,
			NetnsPath:    ops.netnsPath,
		},
	}
	newSnap.Members = dispatcherMembers(allSlots, attached)

	completed, err := e.store.ReplaceDispatcherSnapshot(ctx, newSnap)
	if err != nil {
		e.logger.ErrorContext(ctx, "persist failed, rolling back XDP dispatcher", "ifindex", ops.ifindex, "error", err)
		if firstAttach {
			if rbErr := e.kernel.DetachLink(ctx, dispLinkPinPath); rbErr != nil {
				e.logger.ErrorContext(ctx, "rollback: detach dispatcher link failed", "path", dispLinkPinPath, "error", rbErr)
			}
		} else {
			oldDispProgPinPath := e.bpffs.DispatcherProgPath(dispType, nsid, ops.ifindex, snap.Revision)
			if rbErr := e.kernel.UpdateXDPDispatcherLink(ctx, dispLinkPinPath, oldDispProgPinPath); rbErr != nil {
				e.logger.ErrorContext(ctx, "rollback: restore XDP dispatcher link failed", "link_path", dispLinkPinPath, "old_dispatcher_path", oldDispProgPinPath, "error", rbErr)
			}
		}
		cleanupExtensions()
		cleanupNewDispatcher()
		return extensionResult{}, err
	}

	// Clean up old revision directory (if not first-attach).
	if !firstAttach {
		oldRevDir := e.bpffs.DispatcherRevisionDir(dispType, nsid, ops.ifindex, snap.Revision)
		if err := e.Execute(ctx, action.RemoveDispatcherRevDir{Path: oldRevDir}); err != nil {
			e.logger.WarnContext(ctx, "failed to remove old revision directory", "path", oldRevDir, "error", err)
		}
	}

	// Find the new extension's attach output.
	if newSlotPosition < 0 {
		newSlotPosition = len(attached) - 1
	}
	newExt := attached[newSlotPosition]

	e.logger.InfoContext(ctx, "rebuilt XDP dispatcher", "nsid", nsid, "ifindex", ops.ifindex, "revision", revision, "dispatcher_id", dispatcherID, "num_extensions", len(allSlots), "new_position", newSlotPosition)

	proceedOnActions, err := dispatcher.ProceedOnActions(dispType, newSlot.ProceedOn)
	if err != nil {
		return extensionResult{}, fmt.Errorf("decode XDP proceed-on: %w", err)
	}

	// Construct the bpfman.Link for the new extension.
	newExtLinkRecord := completed.Members[newSlotPosition]
	newExtRecord := bpfman.LinkRecord{
		ID:           newExtLinkRecord.LinkID,
		ProgramID:    managedProgramID,
		KernelLinkID: newExtLinkRecord.KernelLinkID,
		Kind:         bpfman.LinkKindXDP,
		PinPath:      bpfman.NewLinkPath(newExt.pinPath),
		Metadata:     newExtLinkRecord.Metadata,
		Details: bpfman.XDPDetails{
			Interface:    newSlot.Ifname,
			Ifindex:      ops.ifindex,
			Priority:     int32(newSlot.Priority),
			Position:     int32(newSlotPosition),
			ProceedOn:    proceedOnActions,
			Nsid:         nsid,
			DispatcherID: dispatcherID,
			Revision:     revision,
		},
	}

	return extensionResult{
		link: bpfman.Link{
			Record: newExtRecord,
			Status: bpfman.LinkStatus{
				Kernel:     newExt.out.KernelLink,
				KernelSeen: newExt.out.KernelLink != nil,
				PinPresent: newExt.pinPath != "",
			},
		},
		key:      key,
		revision: revision,
		position: newSlotPosition,
		pinPath:  newExt.pinPath,
	}, nil
}

// rebuildTCDispatcher performs a full TC dispatcher rebuild.
// Same semantics as rebuildXDPDispatcher but for TC dispatchers.
// The managedProgramID identifies the user program being attached.
func (e *executor) rebuildTCDispatcher(
	ctx context.Context,
	managedProgramID kernel.ProgramID,
	ops tcRebuildOps,
	progPinPath bpfman.ProgPinPath,
	programName string,
	priority int,
	proceedOn uint32,
	metadata map[string]string,
) (extensionResult, error) {
	newSlot := rebuildSlot{
		ProgPinPath: progPinPath,
		ProgramName: programName,
		Priority:    priority,
		ProceedOn:   proceedOn,
		ProgramID:   managedProgramID,
		Ifname:      ops.ifname,
		Metadata:    metadata,
	}

	nsid, err := netns.NSID(ops.netnsPath)
	if err != nil {
		return extensionResult{}, fmt.Errorf("get nsid: %w", err)
	}

	dispType := ops.dispType
	key := dispatcher.Key{Type: dispType, Nsid: nsid, Ifindex: ops.ifindex}

	// Query existing dispatcher snapshot (may not exist).
	snap, err := e.store.GetDispatcherSnapshot(ctx, key)
	firstAttach := false
	if err != nil {
		if !isNotFound(err) {
			return extensionResult{}, fmt.Errorf("get dispatcher: %w", err)
		}
		firstAttach = true
	}

	// Build the full set of extensions (existing + new).
	allSlots := make([]rebuildSlot, 0, len(snap.Members)+1)
	for _, m := range snap.Members {
		linkID := m.LinkID
		allSlots = append(allSlots, rebuildSlot{
			ProgPinPath:    m.ProgPinPath,
			ProgramName:    m.ProgramName,
			Priority:       m.Priority,
			ProceedOn:      m.ProceedOn,
			ExistingLinkID: &linkID,
			ProgramID:      m.ProgramID,
			Ifname:         m.Ifname,
			Metadata:       m.Metadata,
		})
	}
	allSlots = append(allSlots, newSlot)

	if len(allSlots) > dispatcher.MaxPrograms {
		return extensionResult{}, fmt.Errorf("no free dispatcher slots (all %d occupied)", dispatcher.MaxPrograms)
	}

	// Sort by (priority, programName) to determine positions.
	sortRebuildSlots(allSlots)

	// Compute .rodata config.
	cfg, err := dispatcher.NewTCConfig(len(allSlots))
	if err != nil {
		return extensionResult{}, fmt.Errorf("create TC dispatcher config: %w", err)
	}
	for i, slot := range allSlots {
		cfg.ChainCallActions[i] = slot.ProceedOn
	}

	// Compute new revision.
	var revision uint32
	if firstAttach {
		revision = 1
	} else {
		revision = snap.Revision + 1
	}

	dispProgPinPath := e.bpffs.DispatcherProgPath(dispType, nsid, ops.ifindex, revision)

	e.logger.InfoContext(ctx, "rebuilding TC dispatcher", "nsid", nsid, "ifindex", ops.ifindex, "ifname", ops.ifname, "direction", ops.direction, "revision", revision, "num_extensions", len(allSlots), "first_attach", firstAttach)

	// Load new dispatcher with .rodata config.
	dispatcherID, err := e.kernel.LoadAndPinTCDispatcher(ctx, cfg, dispProgPinPath)
	if err != nil {
		return extensionResult{}, fmt.Errorf("load TC dispatcher: %w", err)
	}

	cleanupNewDispatcher := func() {
		if rbErr := e.kernel.RemovePin(ctx, dispProgPinPath); rbErr != nil {
			e.logger.ErrorContext(ctx, "rollback: remove new TC dispatcher pin failed", "path", dispProgPinPath, "error", rbErr)
		}
	}

	// Attach all extensions to the new dispatcher.
	attached := make([]attachedExt, 0, len(allSlots))

	cleanupExtensions := func() {
		for _, ext := range attached {
			if ext.pinPath != "" {
				if rbErr := e.kernel.DetachLink(ctx, ext.pinPath); rbErr != nil {
					e.logger.ErrorContext(ctx, "rollback: detach TC extension link failed", "path", ext.pinPath, "error", rbErr)
				}
			}
		}
	}

	newSlotPosition := -1
	for i, slot := range allSlots {
		linkPinPath := e.bpffs.ExtensionLinkPath(dispType, nsid, ops.ifindex, revision, i)

		out, err := e.kernel.AttachTCExtension(ctx, dispatcher.TCExtensionAttachSpec{
			DispatcherPinPath: dispProgPinPath,
			ProgPinPath:       slot.ProgPinPath,
			ProgramName:       slot.ProgramName,
			Position:          i,
			LinkPinPath:       linkPinPath,
		})
		if err != nil {
			cleanupExtensions()
			cleanupNewDispatcher()
			return extensionResult{}, fmt.Errorf("attach TC extension %s at position %d: %w", slot.ProgramName, i, err)
		}

		attached = append(attached, attachedExt{out: out, position: i, pinPath: linkPinPath})

		if slot.ExistingLinkID == nil {
			newSlotPosition = i
		}
	}

	// Diagnostic: read each just-pinned freplace link back via
	// BPF_LINK_GET_INFO_BY_FD. This forces a syscall round-trip per
	// link before the dispatcher swap, giving the kernel another
	// chance to publish trampoline state, and surfaces any slot
	// whose target_obj_id does not match the new dispatcher (which
	// would mean the freplace is bound to a stale program). Errors
	// are logged but do not abort the rebuild.
	for _, ext := range attached {
		info, infoErr := e.kernel.ExtensionLinkInfo(ctx, ext.pinPath)
		if infoErr != nil {
			e.logger.WarnContext(ctx, "verify: extension link info failed", "type", dispType.String(), "ifindex", ops.ifindex, "revision", revision, "position", ext.position, "path", ext.pinPath, "error", infoErr)
			continue
		}

		e.logger.InfoContext(ctx, "verify: extension link", "type", dispType.String(), "ifindex", ops.ifindex, "revision", revision, "position", ext.position, "kernel_link_id", uint64(info.KernelLinkID), "target_prog_id", uint64(info.TargetProgID), "target_btf_id", info.TargetBtfID, "attach_type", info.AttachType, "matches_dispatcher", uint64(info.TargetProgID) == uint64(dispatcherID))
	}

	// Atomic swap: create filter (first-attach) or swap (add new, remove old).
	// The old filter's exact handle was recorded at its create and
	// persisted in the snapshot, so the swap removes bpfman's own
	// filter rather than rediscovering one by priority.
	var oldHandle uint32
	var oldPriority uint16
	if !firstAttach && snap.Runtime.FilterPriority != nil {
		oldPriority = *snap.Runtime.FilterPriority
		if snap.Runtime.FilterHandle != nil {
			oldHandle = *snap.Runtime.FilterHandle
		}
	}

	result, err := e.kernel.CreateTCFilter(ctx, dispProgPinPath, int(ops.ifindex), ops.ifname, ops.direction, ops.netnsPath, 0)
	if err != nil {
		cleanupExtensions()
		cleanupNewDispatcher()
		return extensionResult{}, fmt.Errorf("create TC filter: %w", err)
	}

	e.logger.InfoContext(ctx, "TC filter swap: new filter created", "new_handle", fmt.Sprintf("%x", result.Handle), "new_priority", result.Priority, "new_dispatcher_id", result.DispatcherID, "old_handle", fmt.Sprintf("%x", oldHandle), "handles_match", result.Handle == oldHandle)

	// Remove old filter after new one is in place.
	if !firstAttach && oldHandle != 0 {
		parent := dispatcher.TCParentHandle(dispType)
		if err := e.kernel.DetachTCFilter(ctx, int(ops.ifindex), ops.ifname, parent, oldPriority, oldHandle, ops.netnsPath); err != nil {
			e.logger.WarnContext(ctx, "failed to remove old TC filter", "handle", fmt.Sprintf("%x", oldHandle), "error", err)
		} else {
			e.logger.InfoContext(ctx, "TC filter swap: removed old filter", "removed_handle", fmt.Sprintf("%x", oldHandle))
		}
	}

	// Build snapshot with all members and persist atomically.
	newSnap := platform.DispatcherSnapshotSpec{
		Key:      key,
		Revision: revision,
		Runtime: platform.DispatcherRuntime{
			ProgramID:      dispatcherID,
			FilterPriority: &result.Priority,
			FilterHandle:   &result.Handle,
			NetnsPath:      ops.netnsPath,
		},
	}
	newSnap.Members = dispatcherMembers(allSlots, attached)

	completed, err := e.store.ReplaceDispatcherSnapshot(ctx, newSnap)
	if err != nil {
		e.logger.ErrorContext(ctx, "persist failed, rolling back TC dispatcher", "ifindex", ops.ifindex, "error", err)
		parent := dispatcher.TCParentHandle(dispType)
		if rbErr := e.kernel.DetachTCFilter(ctx, int(ops.ifindex), ops.ifname, parent, result.Priority, result.Handle, ops.netnsPath); rbErr != nil {
			e.logger.ErrorContext(ctx, "rollback: remove new TC filter failed", "handle", fmt.Sprintf("%x", result.Handle), "priority", result.Priority, "error", rbErr)
		}

		if !firstAttach && oldHandle != 0 {
			oldDispProgPinPath := e.bpffs.DispatcherProgPath(dispType, nsid, ops.ifindex, snap.Revision)
			if _, rbErr := e.kernel.CreateTCFilter(ctx, oldDispProgPinPath, int(ops.ifindex), ops.ifname, ops.direction, ops.netnsPath, oldHandle); rbErr != nil {
				e.logger.ErrorContext(ctx, "rollback: restore old TC filter failed", "path", oldDispProgPinPath, "old_priority", oldPriority, "old_handle", fmt.Sprintf("%x", oldHandle), "error", rbErr)
			}
		}
		cleanupExtensions()
		cleanupNewDispatcher()
		return extensionResult{}, err
	}

	// Clean up old revision directory.
	if !firstAttach {
		oldRevDir := e.bpffs.DispatcherRevisionDir(dispType, nsid, ops.ifindex, snap.Revision)
		if err := e.Execute(ctx, action.RemoveDispatcherRevDir{Path: oldRevDir}); err != nil {
			e.logger.WarnContext(ctx, "failed to remove old TC revision directory", "path", oldRevDir, "error", err)
		}
	}

	if newSlotPosition < 0 {
		newSlotPosition = len(attached) - 1
	}
	newExt := attached[newSlotPosition]

	e.logger.InfoContext(ctx, "rebuilt TC dispatcher", "nsid", nsid, "ifindex", ops.ifindex, "ifname", ops.ifname, "direction", ops.direction, "revision", revision, "dispatcher_id", dispatcherID, "num_extensions", len(allSlots), "new_position", newSlotPosition)

	proceedOnActions, err := dispatcher.ProceedOnActions(dispType, newSlot.ProceedOn)
	if err != nil {
		return extensionResult{}, fmt.Errorf("decode TC proceed-on: %w", err)
	}

	// Construct the bpfman.Link for the new extension.
	newExtLinkRecord := completed.Members[newSlotPosition]
	newExtRecord := bpfman.LinkRecord{
		ID:           newExtLinkRecord.LinkID,
		ProgramID:    managedProgramID,
		KernelLinkID: newExtLinkRecord.KernelLinkID,
		Kind:         bpfman.LinkKindTC,
		PinPath:      bpfman.NewLinkPath(newExt.pinPath),
		Metadata:     newExtLinkRecord.Metadata,
		Details: bpfman.TCDetails{
			Interface:    newSlot.Ifname,
			Ifindex:      ops.ifindex,
			Direction:    ops.direction,
			Priority:     int32(newSlot.Priority),
			Position:     int32(newSlotPosition),
			ProceedOn:    proceedOnActions,
			Nsid:         nsid,
			DispatcherID: dispatcherID,
			Revision:     revision,
		},
	}

	return extensionResult{
		link: bpfman.Link{
			Record: newExtRecord,
			Status: bpfman.LinkStatus{
				Kernel:     newExt.out.KernelLink,
				KernelSeen: newExt.out.KernelLink != nil,
				PinPresent: newExt.pinPath != "",
			},
		},
		key:      key,
		revision: revision,
		position: newSlotPosition,
		pinPath:  newExt.pinPath,
	}, nil
}

// removeDispatcherIfEmpty removes a dispatcher when no extension
// links remain in its snapshot, and is a no-op otherwise. This is
// the implementation of action.RemoveDispatcher -- the manager's
// single domain intent for nominal empty-dispatcher teardown.
//
// It is intentionally *not* the same as
// rebuildDispatcherForDetach(ctx, key, 0): that helper rebuilds
// the dispatcher with all current members when none are excluded,
// which would be a wasteful no-op behavioural surprise for an
// action whose contract is "remove if empty".
func (e *executor) removeDispatcherIfEmpty(ctx context.Context, key dispatcher.Key) error {
	snap, err := e.store.GetDispatcherSnapshot(ctx, key)
	if err != nil {
		return fmt.Errorf("get dispatcher snapshot: %w", err)
	}

	if len(snap.Members) != 0 {
		return nil
	}
	return e.removeEmptyDispatcher(ctx, snap)
}

// rebuildDispatcherForDetach rebuilds the dispatcher after an
// extension has been detached. If no extensions remain, the
// dispatcher is removed entirely.
func (e *executor) rebuildDispatcherForDetach(ctx context.Context, key dispatcher.Key, excludeLinkID bpfman.LinkID) error {
	snap, err := e.store.GetDispatcherSnapshot(ctx, key)
	if err != nil {
		return fmt.Errorf("get dispatcher snapshot: %w", err)
	}

	// Filter out the excluded member.
	filtered := snap.Members[:0]
	for _, m := range snap.Members {
		if m.LinkID != excludeLinkID {
			filtered = append(filtered, m)
		}
	}
	snap.Members = filtered

	if len(snap.Members) == 0 {
		return e.removeEmptyDispatcher(ctx, snap)
	}
	if snap.Runtime.NetnsPath != "" {
		if _, err := os.Stat(snap.Runtime.NetnsPath); errors.Is(err, os.ErrNotExist) {
			return e.removeEmptyDispatcher(ctx, snap)
		}
	}

	// Extensions remain: rebuild with the remaining members.
	rebuildSlots := make([]rebuildSlot, len(snap.Members))
	for i, m := range snap.Members {
		linkID := m.LinkID
		rebuildSlots[i] = rebuildSlot{
			ProgPinPath:    m.ProgPinPath,
			ProgramName:    m.ProgramName,
			Priority:       m.Priority,
			ProceedOn:      m.ProceedOn,
			ExistingLinkID: &linkID,
			ProgramID:      m.ProgramID,
			Ifname:         m.Ifname,
		}
	}
	sortRebuildSlots(rebuildSlots)

	// Compute new revision.
	revision := snap.Revision + 1
	progPinPath := e.bpffs.DispatcherProgPath(key.Type, key.Nsid, key.Ifindex, revision)

	e.logger.InfoContext(ctx, "rebuilding dispatcher for detach", "type", key.Type, "nsid", key.Nsid, "ifindex", key.Ifindex, "revision", revision, "remaining", len(rebuildSlots))

	if key.Type == dispatcher.DispatcherTypeXDP {
		return e.rebuildXDPForDetach(ctx, snap, rebuildSlots, revision, progPinPath)
	}
	return e.rebuildTCForDetach(ctx, snap, rebuildSlots, revision, progPinPath)
}

// rebuildXDPForDetach handles the XDP-specific rebuild after detach.
func (e *executor) rebuildXDPForDetach(
	ctx context.Context,
	snap platform.DispatcherSnapshot,
	slots []rebuildSlot,
	revision uint32,
	progPinPath bpfman.ProgPinPath,
) error {
	key := snap.Key

	cfg, err := dispatcher.NewXDPConfig(len(slots))
	if err != nil {
		return fmt.Errorf("create XDP dispatcher config for detach rebuild: %w", err)
	}
	for i, slot := range slots {
		cfg.ChainCallActions[i] = slot.ProceedOn
	}

	dispatcherID, err := e.kernel.LoadAndPinXDPDispatcher(ctx, cfg, progPinPath)
	if err != nil {
		return fmt.Errorf("load XDP dispatcher for detach rebuild: %w", err)
	}

	cleanupNewRevision := func() {
		if rbErr := e.removeDispatcherRevisionDir(ctx, key, revision); rbErr != nil {
			e.logger.ErrorContext(ctx, "rollback: remove XDP detach-rebuild revision failed", "path", e.bpffs.DispatcherRevisionDir(key.Type, key.Nsid, key.Ifindex, revision), "error", rbErr)
		}
	}

	// Attach remaining extensions.
	attached := make([]attachedExt, 0, len(slots))
	cleanupExtensions := func() {
		for _, ext := range attached {
			if ext.pinPath != "" {
				if rbErr := e.kernel.DetachLink(ctx, ext.pinPath); rbErr != nil {
					e.logger.ErrorContext(ctx, "rollback: detach XDP detach-rebuild extension failed", "path", ext.pinPath, "error", rbErr)
				}
			}
		}
	}
	for i, slot := range slots {
		linkPinPath := e.bpffs.ExtensionLinkPath(key.Type, key.Nsid, key.Ifindex, revision, i)
		out, err := e.kernel.AttachXDPExtension(ctx, dispatcher.XDPExtensionAttachSpec{
			DispatcherPinPath: progPinPath,
			ProgPinPath:       slot.ProgPinPath,
			ProgramName:       slot.ProgramName,
			Position:          i,
			LinkPinPath:       linkPinPath,
		})
		if err != nil {
			cleanupExtensions()
			cleanupNewRevision()
			return fmt.Errorf("re-attach XDP extension %s at position %d: %w", slot.ProgramName, i, err)
		}
		attached = append(attached, attachedExt{out: out, pinPath: linkPinPath})
	}

	// Diagnostic: see rebuildXDPDispatcher for rationale.
	for i, ext := range attached {
		info, infoErr := e.kernel.ExtensionLinkInfo(ctx, ext.pinPath)
		if infoErr != nil {
			e.logger.WarnContext(ctx, "verify: extension link info failed (detach rebuild)", "type", key.Type.String(), "ifindex", key.Ifindex, "revision", revision, "position", i, "path", ext.pinPath, "error", infoErr)
			continue
		}

		e.logger.InfoContext(ctx, "verify: extension link (detach rebuild)", "type", key.Type.String(), "ifindex", key.Ifindex, "revision", revision, "position", i, "kernel_link_id", uint64(info.KernelLinkID), "target_prog_id", uint64(info.TargetProgID), "target_btf_id", info.TargetBtfID, "attach_type", info.AttachType, "matches_dispatcher", uint64(info.TargetProgID) == uint64(dispatcherID))
	}

	// Swap link.
	linkPinPath := e.bpffs.DispatcherLinkPath(key.Type, key.Nsid, key.Ifindex)
	if err := e.kernel.UpdateXDPDispatcherLink(ctx, linkPinPath, progPinPath); err != nil {
		cleanupExtensions()
		cleanupNewRevision()
		return fmt.Errorf("update XDP dispatcher link: %w", err)
	}

	// Build new snapshot and persist.
	newSnap := platform.DispatcherSnapshotSpec{
		Key:      key,
		Revision: revision,
		Runtime: platform.DispatcherRuntime{
			ProgramID:    dispatcherID,
			KernelLinkID: snap.Runtime.KernelLinkID,
			NetnsPath:    snap.Runtime.NetnsPath,
		},
	}
	newSnap.Members = dispatcherMembers(slots, attached)

	if _, err := e.store.ReplaceDispatcherSnapshot(ctx, newSnap); err != nil {
		oldProgPinPath := e.bpffs.DispatcherProgPath(key.Type, key.Nsid, key.Ifindex, snap.Revision)
		if rbErr := e.kernel.UpdateXDPDispatcherLink(ctx, linkPinPath, oldProgPinPath); rbErr != nil {
			e.logger.ErrorContext(ctx, "rollback: restore XDP detach-rebuild dispatcher link failed", "link_path", linkPinPath, "old_dispatcher_path", oldProgPinPath, "error", rbErr)
		}
		cleanupExtensions()
		cleanupNewRevision()
		return err
	}

	// Clean up old revision.
	oldRevDir := e.bpffs.DispatcherRevisionDir(key.Type, key.Nsid, key.Ifindex, snap.Revision)
	if err := e.Execute(ctx, action.RemoveDispatcherRevDir{Path: oldRevDir}); err != nil {
		e.logger.WarnContext(ctx, "failed to remove old revision directory", "path", oldRevDir, "error", err)
	}

	return nil
}

// rebuildTCForDetach handles the TC-specific rebuild after detach.
func (e *executor) rebuildTCForDetach(
	ctx context.Context,
	snap platform.DispatcherSnapshot,
	slots []rebuildSlot,
	revision uint32,
	progPinPath bpfman.ProgPinPath,
) error {
	key := snap.Key
	dispType := key.Type

	cfg, err := dispatcher.NewTCConfig(len(slots))
	if err != nil {
		return fmt.Errorf("create TC dispatcher config for detach rebuild: %w", err)
	}
	for i, slot := range slots {
		cfg.ChainCallActions[i] = slot.ProceedOn
	}

	dispatcherID, err := e.kernel.LoadAndPinTCDispatcher(ctx, cfg, progPinPath)
	if err != nil {
		return fmt.Errorf("load TC dispatcher for detach rebuild: %w", err)
	}

	cleanupNewRevision := func() {
		if rbErr := e.removeDispatcherRevisionDir(ctx, key, revision); rbErr != nil {
			e.logger.ErrorContext(ctx, "rollback: remove TC detach-rebuild revision failed", "path", e.bpffs.DispatcherRevisionDir(key.Type, key.Nsid, key.Ifindex, revision), "error", rbErr)
		}
	}

	// Attach remaining extensions.
	attached := make([]attachedExt, 0, len(slots))
	cleanupExtensions := func() {
		for _, ext := range attached {
			if ext.pinPath != "" {
				if rbErr := e.kernel.DetachLink(ctx, ext.pinPath); rbErr != nil {
					e.logger.ErrorContext(ctx, "rollback: detach TC detach-rebuild extension failed", "path", ext.pinPath, "error", rbErr)
				}
			}
		}
	}
	for i, slot := range slots {
		linkPinPath := e.bpffs.ExtensionLinkPath(key.Type, key.Nsid, key.Ifindex, revision, i)
		out, err := e.kernel.AttachTCExtension(ctx, dispatcher.TCExtensionAttachSpec{
			DispatcherPinPath: progPinPath,
			ProgPinPath:       slot.ProgPinPath,
			ProgramName:       slot.ProgramName,
			Position:          i,
			LinkPinPath:       linkPinPath,
		})
		if err != nil {
			cleanupExtensions()
			cleanupNewRevision()
			return fmt.Errorf("re-attach TC extension %s at position %d: %w", slot.ProgramName, i, err)
		}
		attached = append(attached, attachedExt{out: out, pinPath: linkPinPath})
	}

	// Diagnostic: see rebuildTCDispatcher for rationale.
	for i, ext := range attached {
		info, infoErr := e.kernel.ExtensionLinkInfo(ctx, ext.pinPath)
		if infoErr != nil {
			e.logger.WarnContext(ctx, "verify: extension link info failed (detach rebuild)", "type", key.Type.String(), "ifindex", key.Ifindex, "revision", revision, "position", i, "path", ext.pinPath, "error", infoErr)
			continue
		}

		e.logger.InfoContext(ctx, "verify: extension link (detach rebuild)", "type", key.Type.String(), "ifindex", key.Ifindex, "revision", revision, "position", i, "kernel_link_id", uint64(info.KernelLinkID), "target_prog_id", uint64(info.TargetProgID), "target_btf_id", info.TargetBtfID, "attach_type", info.AttachType, "matches_dispatcher", uint64(info.TargetProgID) == uint64(dispatcherID))
	}

	// Record old handle before swap, from the exact handle persisted
	// at its create.
	var oldHandle uint32
	var oldPriority uint16
	if snap.Runtime.FilterPriority != nil {
		oldPriority = *snap.Runtime.FilterPriority
		if snap.Runtime.FilterHandle != nil {
			oldHandle = *snap.Runtime.FilterHandle
		}
	}

	// Determine direction from dispatcher type.
	var direction bpfman.TCDirection
	if dispType == dispatcher.DispatcherTypeTCIngress {
		direction = bpfman.TCDirectionIngress
	} else {
		direction = bpfman.TCDirectionEgress
	}

	// Create new filter.
	result, err := e.kernel.CreateTCFilter(ctx, progPinPath, int(key.Ifindex), snapInterfaceName(snap), direction, snap.Runtime.NetnsPath, 0)
	if err != nil {
		cleanupExtensions()
		cleanupNewRevision()
		return fmt.Errorf("create TC filter for detach rebuild: %w", err)
	}

	// Remove old filter.
	if oldHandle != 0 {
		parent := dispatcher.TCParentHandle(dispType)
		if err := e.kernel.DetachTCFilter(ctx, int(key.Ifindex), snapInterfaceName(snap), parent, oldPriority, oldHandle, snap.Runtime.NetnsPath); err != nil {
			e.logger.WarnContext(ctx, "failed to remove old TC filter after detach rebuild", "handle", fmt.Sprintf("%x", oldHandle), "error", err)
		}
	}

	// Build new snapshot and persist.
	newSnap := platform.DispatcherSnapshotSpec{
		Key:      key,
		Revision: revision,
		Runtime: platform.DispatcherRuntime{
			ProgramID:      result.DispatcherID,
			FilterPriority: &result.Priority,
			FilterHandle:   &result.Handle,
			NetnsPath:      snap.Runtime.NetnsPath,
		},
	}
	newSnap.Members = dispatcherMembers(slots, attached)

	if _, err := e.store.ReplaceDispatcherSnapshot(ctx, newSnap); err != nil {
		parent := dispatcher.TCParentHandle(dispType)
		if rbErr := e.kernel.DetachTCFilter(ctx, int(key.Ifindex), snapInterfaceName(snap), parent, result.Priority, result.Handle, snap.Runtime.NetnsPath); rbErr != nil {
			e.logger.ErrorContext(ctx, "rollback: remove TC detach-rebuild filter failed", "handle", fmt.Sprintf("%x", result.Handle), "priority", result.Priority, "error", rbErr)
		}

		if oldHandle != 0 {
			oldProgPinPath := e.bpffs.DispatcherProgPath(key.Type, key.Nsid, key.Ifindex, snap.Revision)
			if _, rbErr := e.kernel.CreateTCFilter(ctx, oldProgPinPath, int(key.Ifindex), snapInterfaceName(snap), direction, snap.Runtime.NetnsPath, oldHandle); rbErr != nil {
				e.logger.ErrorContext(ctx, "rollback: restore TC detach-rebuild filter failed", "path", oldProgPinPath, "old_priority", oldPriority, "old_handle", fmt.Sprintf("%x", oldHandle), "error", rbErr)
			}
		}
		cleanupExtensions()
		cleanupNewRevision()
		return err
	}

	// Clean up old revision.
	oldRevDir := e.bpffs.DispatcherRevisionDir(key.Type, key.Nsid, key.Ifindex, snap.Revision)
	if err := e.Execute(ctx, action.RemoveDispatcherRevDir{Path: oldRevDir}); err != nil {
		e.logger.WarnContext(ctx, "failed to remove old TC revision directory", "path", oldRevDir, "error", err)
	}

	return nil
}

func snapInterfaceName(snap platform.DispatcherSnapshot) string {
	for _, member := range snap.Members {
		if member.Ifname != "" {
			return member.Ifname
		}
	}
	return ""
}

// removeEmptyDispatcher removes a dispatcher when no extensions
// remain. It orchestrates a fixed sequence of named lifecycle
// operations directly, rather than building a generic action list
// and handing it to ExecuteAll.
//
// Failure contract for destructive teardown:
//
//  1. Kernel-side detach (XDP outer link or TC filter) is the point
//     of no return. If it fails, packets can still reach the
//     dispatcher program; the function aborts teardown and surfaces
//     the error so the caller can retry or hand off to repair.
//
//  2. Once kernel detach succeeds, the user-visible state is "the
//     dispatcher is gone". The contract from here is two-state:
//     either the kernel detach above failed (dispatcher still live,
//     retry safe) or it succeeded and the caller never sees a
//     residue error. Only deleteDispatcherSnapshot joins into the
//     returned error: a phantom row is the worst residue class and,
//     unlike the other failures, retries cleanly without a
//     false-negative. Removing the dispatcher program pin and the
//     per-revision directory are warned and discarded; both leave
//     only userland orphans that coherency, audit, and GC repair.
//     This mirrors the post-detach contract on Manager.unload.
//
// Ordering is also load-bearing for safety. The lifecycle methods
// are private to the executor; nothing outside this file can
// compose them differently.
func (e *executor) removeEmptyDispatcher(ctx context.Context, snap platform.DispatcherSnapshot) error {
	key := snap.Key
	e.logger.DebugContext(ctx, "removing empty dispatcher", "type", key.Type, "nsid", key.Nsid, "ifindex", key.Ifindex, "program_id", snap.Runtime.ProgramID, "kernel_link_id", snap.Runtime.KernelLinkID)

	// Point of no return.
	if isTCDispatcherType(key.Type) {
		if err := e.detachTCDispatcherFilter(ctx, snap); err != nil {
			return fmt.Errorf("detach TC dispatcher filter: %w", err)
		}
	} else {
		if err := e.detachXDPOuterLink(ctx, key); err != nil {
			return fmt.Errorf("detach XDP outer link: %w", err)
		}
	}

	// Post-detach cleanup. Each step is independent; later steps
	// run even if earlier steps fail. Only deleteDispatcherSnapshot
	// joins into the returned error (see the failure contract
	// above): every other step is warned and left for
	// coherency/audit/GC.
	var errs []error
	if err := e.removeDispatcherProgPin(ctx, key, snap.Revision); err != nil {
		e.logger.WarnContext(ctx, "failed to remove orphaned dispatcher program pin", "type", key.Type, "nsid", key.Nsid, "ifindex", key.Ifindex, "revision", snap.Revision, "path", e.bpffs.DispatcherProgPath(key.Type, key.Nsid, key.Ifindex, snap.Revision), "error", err)
	}

	if err := e.removeDispatcherRevisionDir(ctx, key, snap.Revision); err != nil {
		e.logger.WarnContext(ctx, "failed to remove orphaned dispatcher revision directory", "type", key.Type, "nsid", key.Nsid, "ifindex", key.Ifindex, "revision", snap.Revision, "path", e.bpffs.DispatcherRevisionDir(key.Type, key.Nsid, key.Ifindex, snap.Revision), "error", err)
	}

	if err := e.deleteDispatcherSnapshot(ctx, key); err != nil {
		errs = append(errs, fmt.Errorf("delete dispatcher snapshot: %w", err))
	}
	return errors.Join(errs...)
}

// detachXDPOuterLink performs BPF_LINK_DETACH on the dispatcher's
// outer XDP link, then unpins it. The kernel detaches the link from
// the netdev synchronously before this returns, which is what
// stops packets reaching the dispatcher program. RemovePin alone
// is not equivalent: dropping the userland reference does not
// detach the link, and the netdev continues to run the dispatcher
// until RCU grace and deferred work complete.
func (e *executor) detachXDPOuterLink(ctx context.Context, key dispatcher.Key) error {
	linkPinPath := e.bpffs.DispatcherLinkPath(key.Type, key.Nsid, key.Ifindex)
	return e.kernel.DetachLink(ctx, linkPinPath)
}

// detachTCDispatcherFilter removes the dispatcher's TC filter via
// RTM_DELTFILTER. TC dispatchers predate BPF links and are managed
// through legacy netlink. The filter's exact kernel handle was echoed
// back at create and persisted in the snapshot, so the delete targets
// bpfman's own filter by (parent, priority, handle) -- never a foreign
// filter that happens to share the dispatcher priority. A snapshot
// without a recorded handle (or handle 0) means there is nothing for
// bpfman to remove. DetachTCFilter treats an already-absent filter as
// success, so a retried teardown is idempotent.
//
// This is the last-member teardown. Whether bpfman also reclaims the
// clsact qdisc it created is governed by tcpolicy.ReclaimClsactOnDetach.
func (e *executor) detachTCDispatcherFilter(ctx context.Context, snap platform.DispatcherSnapshot) error {
	key := snap.Key
	if snap.Runtime.FilterHandle != nil && *snap.Runtime.FilterHandle != 0 {
		var priority uint16
		if snap.Runtime.FilterPriority != nil {
			priority = *snap.Runtime.FilterPriority
		}
		parent := dispatcher.TCParentHandle(key.Type)
		if err := e.kernel.DetachTCFilter(ctx, int(key.Ifindex), snapInterfaceName(snap), parent, priority, *snap.Runtime.FilterHandle, snap.Runtime.NetnsPath); err != nil {
			return err
		}
	}
	if tcpolicy.ReclaimClsactOnDetach {
		return e.kernel.RemoveTCClsactIfUnused(ctx, int(key.Ifindex), snapInterfaceName(snap), snap.Runtime.NetnsPath)
	}
	return nil
}

// removeDispatcherProgPin unpins the dispatcher program. After the
// outer link or TC filter is gone the kernel program has no
// attachment, and dropping the bpffs pin lets the kernel reclaim
// it once the userland refcount hits zero.
func (e *executor) removeDispatcherProgPin(ctx context.Context, key dispatcher.Key, revision uint32) error {
	progPinPath := e.bpffs.DispatcherProgPath(key.Type, key.Nsid, key.Ifindex, revision)
	return e.kernel.RemovePin(ctx, progPinPath)
}

// removeDispatcherRevisionDir removes the per-revision directory
// containing the extension link pins.
func (e *executor) removeDispatcherRevisionDir(_ context.Context, key dispatcher.Key, revision uint32) error {
	revDir := e.bpffs.DispatcherRevisionDir(key.Type, key.Nsid, key.Ifindex, revision)
	return e.bpffs.RemoveDispatcherRevDir(revDir)
}

// deleteDispatcherSnapshot removes the dispatcher row from the
// store. This is the last step: by the time it runs, the kernel
// attachment is gone and the bpffs is clean, so a failure here
// only leaves a stale row that coherency can repair.
func (e *executor) deleteDispatcherSnapshot(ctx context.Context, key dispatcher.Key) error {
	return e.store.DeleteDispatcherSnapshot(ctx, key)
}

// isTCDispatcherType reports whether the dispatcher type is one of
// the TC variants (ingress or egress).
func isTCDispatcherType(t dispatcher.DispatcherType) bool {
	return t == dispatcher.DispatcherTypeTCIngress || t == dispatcher.DispatcherTypeTCEgress
}

// isNotFound returns true if the error wraps platform.ErrRecordNotFound.
func isNotFound(err error) bool {
	return errors.Is(err, platform.ErrRecordNotFound)
}

// sortRebuildSlots sorts rebuild slots by
// (priority ASC, attached ASC, programName ASC). Priorities are
// validated at spec construction (negatives rejected) and used here
// as stored, so priority 0 sorts first.
//
// The attached tie-breaker matches Rust bpfman: new (unattached)
// programs sort before existing (attached) ones at the same priority.
// This matters when the default proceed-on excludes TC_ACT_OK - only
// position 0 executes, so the newly-added program must land there.
func sortRebuildSlots(slots []rebuildSlot) {
	for i := 1; i < len(slots); i++ {
		for j := i; j > 0; j-- {
			pi := slots[j].Priority
			pj := slots[j-1].Priority
			ai := slots[j].ExistingLinkID != nil   // attached = true
			aj := slots[j-1].ExistingLinkID != nil // attached = true
			if pi < pj ||
				(pi == pj && !ai && aj) ||
				(pi == pj && ai == aj && slots[j].ProgramName < slots[j-1].ProgramName) {
				slots[j], slots[j-1] = slots[j-1], slots[j]
			} else {
				break
			}
		}
	}
}

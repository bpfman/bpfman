package ebpf

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/ns/netns"
	"github.com/bpfman/bpfman/platform"
)

// tcDispatcherPriority is the default TC priority for the dispatcher
// filter, matching the upstream Rust bpfman value.
const tcDispatcherPriority = 50

// addTCBpfFilterWithEcho creates a cls_bpf filter on the given
// parent/priority and returns the kernel-assigned handle.
//
// vishvananda/netlink's FilterAdd does not surface the handle the
// kernel allocates, so we issue the RTM_NEWTFILTER request directly
// with NLM_F_ECHO and read the handle out of the echoed reply -- the
// mechanism aya (and thus Rust bpfman) uses. Recording the exact
// handle lets later detach delete by it, rather than rediscovering a
// filter by priority alone, which can match an unrelated filter that
// happens to share the dispatcher priority on the same parent.
//
// The request mirrors the cls_bpf serialisation vishvananda builds for
// a BpfFilter: TCA_KIND "bpf", then TCA_OPTIONS carrying the program
// fd, the filter name, and the direct-action flag. A desiredHandle of
// 0 lets the kernel assign one; a non-zero value requests that exact
// handle, used by rollback to restore a filter under the handle the
// snapshot still records. The caller is responsible for running in the
// target network namespace.
func addTCBpfFilterWithEcho(ifindex int, parent uint32, priority uint16, progFD int, name string, desiredHandle uint32) (uint32, error) {
	req := nl.NewNetlinkRequest(unix.RTM_NEWTFILTER, unix.NLM_F_CREATE|unix.NLM_F_EXCL|unix.NLM_F_ECHO|unix.NLM_F_ACK)
	req.AddData(&nl.TcMsg{
		Family:  nl.FAMILY_ALL,
		Ifindex: int32(ifindex),
		Handle:  desiredHandle,
		Parent:  parent,
		Info:    netlink.MakeHandle(priority, nl.Swap16(unix.ETH_P_ALL)),
	})
	req.AddData(nl.NewRtAttr(nl.TCA_KIND, nl.ZeroTerminated("bpf")))

	options := nl.NewRtAttr(nl.TCA_OPTIONS, nil)
	options.AddRtAttr(nl.TCA_BPF_FD, nl.Uint32Attr(uint32(progFD)))
	if name != "" {
		options.AddRtAttr(nl.TCA_BPF_NAME, nl.ZeroTerminated(name))
	}
	options.AddRtAttr(nl.TCA_BPF_FLAGS, nl.Uint32Attr(nl.TCA_BPF_FLAG_ACT_DIRECT))
	req.AddData(options)

	msgs, err := req.Execute(unix.NETLINK_ROUTE, 0)
	if err != nil {
		return 0, err
	}
	for _, m := range msgs {
		if handle := nl.DeserializeTcMsg(m).Handle; handle != 0 {
			return handle, nil
		}
	}
	return 0, fmt.Errorf("no handle echoed back from TC filter creation on ifindex %d parent %x", ifindex, parent)
}

// DetachTCFilter removes a legacy TC BPF filter via netlink.
func (k *kernelAdapter) DetachTCFilter(ctx context.Context, ifindex int, ifname string, parent uint32, priority uint16, handle uint32, netnsPath string) error {
	err := netns.Run(netnsPath, func() error {
		filter := &netlink.BpfFilter{
			FilterAttrs: netlink.FilterAttrs{
				LinkIndex: ifindex,
				Parent:    parent,
				Handle:    handle,
				Priority:  priority,
				Protocol:  unix.ETH_P_ALL,
			},
		}
		if err := netlink.FilterDel(filter); err != nil {
			// An already-absent filter is success: teardown retries
			// and coherency repair must be idempotent.
			if errors.Is(err, unix.ENOENT) {
				return nil
			}
			return fmt.Errorf("delete TC filter (ifindex=%d parent=%x prio=%d handle=%x): %w", ifindex, parent, priority, handle, err)
		}
		return nil
	})
	if err != nil {
		// A deleted target netns means the kernel already tore down the
		// interface and its filters when the namespace went away, so
		// there is nothing left to remove. Mirror Rust, which skips the
		// detach when enter_netns fails ("the netns may have been
		// deleted").
		if errors.Is(err, os.ErrNotExist) {
			k.logger.Debug("target netns gone; TC filter already removed with it", "netns", netnsPath, "ifindex", ifindex, "priority", priority)
			return nil
		}
		return err
	}

	k.logger.Debug("detached TC filter", "ifindex", ifindex, "ifname", ifname, "netns", netnsPath, "parent", fmt.Sprintf("%x", parent), "priority", priority, "handle", fmt.Sprintf("%x", handle))
	return nil
}

// RemoveTCClsactIfUnused reclaims the clsact qdisc bpfman created on an
// interface once nothing uses it any more. It is called on the last
// detach: bpfman creates the clsact on first attach, so leaving it
// behind leaks a qdisc and lets stale clsacts accumulate on churned or
// reused interfaces. It is removed only when a clsact is actually
// present and both its ingress (ffff:fff2) and egress (ffff:fff3) filter
// blocks are empty, so a co-resident egress dispatcher -- or a foreign
// tool's filters on the same clsact -- is never torn out from under its
// owner. A deleted target netns is treated as success: the qdisc went
// with the namespace.
func (k *kernelAdapter) RemoveTCClsactIfUnused(ctx context.Context, ifindex int, ifname, netnsPath string) error {
	err := netns.Run(netnsPath, func() error {
		netlinkLink, err := netlink.LinkByIndex(ifindex)
		if err != nil {
			return fmt.Errorf("look up interface %s (ifindex %d): %w", ifname, ifindex, err)
		}

		qdiscs, err := netlink.QdiscList(netlinkLink)
		if err != nil {
			return fmt.Errorf("list qdiscs on %s (ifindex %d): %w", ifname, ifindex, err)
		}

		hasClsact := false
		for _, q := range qdiscs {
			if q.Type() == "clsact" {
				hasClsact = true
				break
			}
		}
		if !hasClsact {
			return nil
		}
		for _, parent := range []uint32{netlink.HANDLE_MIN_INGRESS, netlink.HANDLE_MIN_EGRESS} {
			filters, err := netlink.FilterList(netlinkLink, parent)
			if err != nil {
				return fmt.Errorf("list filters on %s (ifindex %d) parent %x: %w", ifname, ifindex, parent, err)
			}
			if len(filters) > 0 {
				// Still in use by some direction; leave the clsact.
				return nil
			}
		}
		qdisc := &netlink.Clsact{
			QdiscAttrs: netlink.QdiscAttrs{
				LinkIndex: ifindex,
				Handle:    netlink.MakeHandle(0xffff, 0),
				Parent:    netlink.HANDLE_INGRESS,
			},
		}
		if err := netlink.QdiscDel(qdisc); err != nil && !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("delete clsact qdisc on %s (ifindex %d): %w", ifname, ifindex, err)
		}

		k.logger.Debug("reclaimed unused clsact qdisc", "ifindex", ifindex, "ifname", ifname, "netns", netnsPath)
		return nil
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}

// LoadAndPinTCDispatcher loads a TC dispatcher program with .rodata config
// and pins it at progPinPath without creating a TC filter. Used during
// rebuild to prepare a new dispatcher before atomically swapping.
func (k *kernelAdapter) LoadAndPinTCDispatcher(ctx context.Context, cfg dispatcher.TCConfig, progPinPath bpfman.ProgPinPath) (kernel.ProgramID, error) {
	collSpec, err := dispatcher.LoadTCDispatcher(cfg)
	if err != nil {
		return 0, fmt.Errorf("load TC dispatcher spec: %w", err)
	}

	coll, err := ebpf.NewCollection(collSpec)
	if err != nil {
		return 0, fmt.Errorf("create TC dispatcher collection: %w", err)
	}
	defer coll.Close()

	dispatcherProg := coll.Programs["tc_dispatcher"]
	if dispatcherProg == nil {
		return 0, fmt.Errorf("tc_dispatcher program not found in collection")
	}

	progInfo, err := dispatcherProg.Info()
	if err != nil {
		return 0, fmt.Errorf("get TC dispatcher program info: %w", err)
	}

	progID, ok := progInfo.ID()
	if !ok {
		return 0, fmt.Errorf("failed to get TC dispatcher program ID from kernel")
	}

	if err := pinWithRetry(progPinPath, dispatcherProg.Pin); err != nil {
		return 0, fmt.Errorf("pin TC dispatcher program to %s: %w", progPinPath, err)
	}

	k.logger.Debug("loaded and pinned TC dispatcher", "program_id", progID, "prog_pin_path", progPinPath, "num_progs", cfg.NumProgsEnabled)
	return kernel.ProgramID(progID), nil
}

// CreateTCFilter creates a TC filter from a pinned dispatcher program
// on a network interface, optionally in a specific network namespace.
// Creates the clsact qdisc if needed. desiredHandle of 0 lets the
// kernel assign the filter handle (the normal attach path); a non-zero
// value requests that exact handle, used by rollback to restore a
// filter under the handle the persisted snapshot still records. The
// returned result carries the handle that was actually installed.
func (k *kernelAdapter) CreateTCFilter(ctx context.Context, progPinPath bpfman.ProgPinPath, ifindex int, ifname string, direction bpfman.TCDirection, netnsPath string, desiredHandle uint32) (*platform.TCDispatcherResult, error) {
	prog, err := ebpf.LoadPinnedProgram(progPinPath.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("load pinned program %s: %w", progPinPath, err)
	}
	defer prog.Close()

	var parent uint32
	switch direction {
	case bpfman.TCDirectionIngress:
		parent = netlink.HANDLE_MIN_INGRESS
	case bpfman.TCDirectionEgress:
		parent = netlink.HANDLE_MIN_EGRESS
	default:
		return nil, fmt.Errorf("invalid TC direction %q: must be ingress or egress", direction)
	}

	if netnsPath != "" {
		k.logger.Debug("entering network namespace for TC filter creation", "netns", netnsPath, "ifname", ifname, "ifindex", ifindex, "direction", direction)
	}

	var result *platform.TCDispatcherResult
	err = netns.Run(netnsPath, func() error {
		// The dispatcher needs a clsact qdisc: clsact provides both the
		// ingress (ffff:fff2) and egress (ffff:fff3) attach points, and
		// bpfman owns its lifecycle. Inspect the ingress qdisc slot
		// rather than blindly tolerating EEXIST from QdiscAdd. Reuse an
		// existing clsact; refuse if a non-clsact qdisc occupies the
		// slot. A classic `ingress` qdisc has only an ingress block, so
		// attaching the egress dispatcher there silently mis-wires it
		// (the filter never sees egress traffic), and bpfman must not
		// parasitise or tear down a qdisc another owner installed.
		netlinkLink, err := netlink.LinkByIndex(ifindex)
		if err != nil {
			return fmt.Errorf("look up interface %s (ifindex %d): %w", ifname, ifindex, err)
		}

		qdiscs, err := netlink.QdiscList(netlinkLink)
		if err != nil {
			return fmt.Errorf("list qdiscs on %s (ifindex %d): %w", ifname, ifindex, err)
		}

		hasClsact := false
		conflictKind := ""
		for _, q := range qdiscs {
			switch {
			case q.Type() == "clsact":
				hasClsact = true
			case q.Attrs().Parent == netlink.HANDLE_INGRESS:
				conflictKind = q.Type()
			}
		}
		if !hasClsact && conflictKind != "" {
			return fmt.Errorf("cannot attach TC program to %s: a %q qdisc already occupies the ingress qdisc slot; bpfman requires clsact -- remove it (tc qdisc del dev %s %s) or detach the conflicting program first", ifname, conflictKind, ifname, conflictKind)
		}
		if !hasClsact {
			qdisc := &netlink.Clsact{
				QdiscAttrs: netlink.QdiscAttrs{
					LinkIndex: ifindex,
					Handle:    netlink.MakeHandle(0xffff, 0),
					Parent:    netlink.HANDLE_INGRESS,
				},
			}
			// A clsact already present (EEXIST) is fine to reuse: the
			// check above already refused any foreign qdisc in the slot,
			// so an EEXIST here can only be a clsact whose dump lagged its
			// creation.
			if err := netlink.QdiscAdd(qdisc); err != nil && !errors.Is(err, unix.EEXIST) {
				return fmt.Errorf("add clsact qdisc to %s (ifindex %d): %w", ifname, ifindex, err)
			}
		}

		handle, err := addTCBpfFilterWithEcho(ifindex, parent, tcDispatcherPriority, prog.FD(), "tc_dispatcher", desiredHandle)
		if err != nil {
			return fmt.Errorf("add TC BPF filter to %s (ifindex %d) %s: %w", ifname, ifindex, direction, err)
		}

		progInfo, err := prog.Info()
		if err != nil {
			return fmt.Errorf("get TC dispatcher program info: %w", err)
		}

		progID, ok := progInfo.ID()
		if !ok {
			return fmt.Errorf("failed to get TC dispatcher program ID from kernel")
		}

		result = &platform.TCDispatcherResult{
			DispatcherID:  kernel.ProgramID(progID),
			DispatcherPin: progPinPath,
			Handle:        handle,
			Priority:      tcDispatcherPriority,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// AttachTCExtension loads a pinned extension program and attaches it
// to a TC dispatcher slot via freplace. The extension was already
// loaded as BPF_PROG_TYPE_EXT during the initial Load, so no ELF
// re-read or map replacement is needed.
func (k *kernelAdapter) AttachTCExtension(ctx context.Context, spec dispatcher.TCExtensionAttachSpec) (bpfman.AttachOutput, error) {
	if err := spec.Validate(); err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("invalid spec: %w", err)
	}

	// Load the pinned dispatcher to use as attach target.
	dispatcherProg, err := ebpf.LoadPinnedProgram(spec.DispatcherPinPath.String(), nil)
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("load pinned TC dispatcher %s: %w", spec.DispatcherPinPath, err)
	}
	defer dispatcherProg.Close()

	// Load the pinned extension program.
	extensionProg, err := ebpf.LoadPinnedProgram(spec.ProgPinPath.String(), nil)
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("load pinned TC extension %s: %w", spec.ProgPinPath, err)
	}
	defer extensionProg.Close()

	// Attach the extension using freplace link.
	slotName, err := dispatcher.SlotName(spec.Position)
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("slot name for position %d: %w", spec.Position, err)
	}

	lnk, err := link.AttachFreplace(dispatcherProg, slotName, extensionProg)
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("attach TC freplace to %s: %w", slotName, err)
	}

	return k.finishPinnedAttach(lnk, spec.LinkPinPath, "TC extension link")
}

// AttachTCX attaches a loaded program directly to an interface using TCX link.
// Unlike TC which uses dispatchers, TCX uses native kernel multi-program support.
// The order parameter specifies where to insert the program in the TCX chain.
func (k *kernelAdapter) AttachTCX(ctx context.Context, ifindex int, direction string, programPinPath bpfman.ProgPinPath, linkPinPath bpfman.LinkPath, netnsPath string, order bpfman.TCXAttachOrder) (bpfman.AttachOutput, error) {
	// Load the pinned program
	prog, err := ebpf.LoadPinnedProgram(programPinPath.String(), nil)
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("load pinned program %s: %w", programPinPath, err)
	}
	defer prog.Close()

	// Determine attach type based on direction
	var attachType ebpf.AttachType
	switch direction {
	case "ingress":
		attachType = ebpf.AttachTCXIngress
	case "egress":
		attachType = ebpf.AttachTCXEgress
	default:
		return bpfman.AttachOutput{}, fmt.Errorf("invalid TCX direction %q: must be ingress or egress", direction)
	}

	// Convert TCXAttachOrder to cilium/ebpf link.Anchor
	var anchor link.Anchor
	switch {
	case order.First:
		anchor = link.Head()
	case order.BeforeProgID != 0:
		anchor = link.BeforeProgramByID(ebpf.ProgramID(order.BeforeProgID))
	case order.AfterProgID != 0:
		anchor = link.AfterProgramByID(ebpf.ProgramID(order.AfterProgID))
	default:
		// Default to head for safety - ensures new programs run before existing ones
		anchor = link.Head()
	}

	// Attach and pin in target namespace (if specified)
	if netnsPath != "" {
		k.logger.Debug("entering network namespace for TCX attachment", "netns", netnsPath, "ifindex", ifindex, "direction", direction)
	}

	var result bpfman.AttachOutput
	err = netns.Run(netnsPath, func() error {
		// Attach using TCX link with ordering anchor
		lnk, err := link.AttachTCX(link.TCXOptions{
			Interface: ifindex,
			Program:   prog,
			Attach:    attachType,
			Anchor:    anchor,
		})
		if err != nil {
			return fmt.Errorf("attach TCX to ifindex %d %s: %w", ifindex, direction, err)
		}

		out, ferr := k.finishPinnedAttach(lnk, linkPinPath, "TCX link")
		if ferr != nil {
			return ferr
		}
		result = out
		return nil
	})
	if err != nil {
		return bpfman.AttachOutput{}, err
	}

	return result, nil
}

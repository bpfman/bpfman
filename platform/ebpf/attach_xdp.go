package ebpf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/ns/netns"
	"github.com/bpfman/bpfman/platform"
)

// attachXDPWithRetry retries link.AttachXDP on EBUSY. Removing a
// pinned XDP link drops the last kernel reference, but the kernel
// releases the XDP hook asynchronously. A brief retry avoids
// spurious failures when re-attaching to the same interface
// immediately after detach.
func attachXDPWithRetry(opts link.XDPOptions) (link.Link, error) {
	const (
		maxAttempts = 5
		baseDelay   = 50 * time.Millisecond
	)
	var lnk link.Link
	var err error
	for i := range maxAttempts {
		lnk, err = link.AttachXDP(opts)
		if err == nil || !errors.Is(err, syscall.EBUSY) {
			return lnk, err
		}
		time.Sleep(baseDelay << i)
	}
	return nil, err
}

// AttachXDP attaches a pinned XDP program to a network interface.
func (k *kernelAdapter) AttachXDP(ctx context.Context, progPinPath bpfman.ProgPinPath, ifindex int, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error) {
	prog, err := ebpf.LoadPinnedProgram(progPinPath.String(), nil)
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("load pinned program %s: %w", progPinPath, err)
	}
	defer prog.Close()

	lnk, err := attachXDPWithRetry(link.XDPOptions{
		Program:   prog,
		Interface: ifindex,
	})
	if err != nil {
		if errors.Is(err, syscall.EBUSY) {
			return bpfman.AttachOutput{}, fmt.Errorf("attach XDP to ifindex %d: interface already has an XDP program attached: %w", ifindex, err)
		}
		return bpfman.AttachOutput{}, fmt.Errorf("attach XDP to ifindex %d: %w", ifindex, err)
	}

	return k.finishPinnedAttach(lnk, linkPinPath, "link")
}

// finishPinnedAttach is the shared tail of the pinned/dispatcher
// attaches (XDP, the XDP and TC freplace extensions, TCX). It pins the
// link, reads its info, and closes the fd: the pin keeps the kernel link
// alive, and leaking the fd would stop DetachLink -- which only removes
// the pin -- from releasing the link. On any failure before the pin is
// durable it removes the pin and closes the link. The noun labels the
// link in the pin and cleanup diagnostics ("link", "extension link",
// "TC extension link", "TCX link").
func (k *kernelAdapter) finishPinnedAttach(lnk link.Link, linkPinPath bpfman.LinkPath, noun string) (bpfman.AttachOutput, error) {
	linkPin := linkPinPath.String()

	success := false
	cleanup := func() {
		if !success {
			lnk.Close()
			if linkPin != "" {
				if err := os.Remove(linkPin); err != nil && !os.IsNotExist(err) {
					k.logger.Warn(fmt.Sprintf("failed to remove pinned %s during cleanup", noun), "path", linkPin, "error", err)
				}
			}
		}
	}
	defer cleanup()

	if linkPin != "" {
		if err := pinWithRetry(linkPin, lnk.Pin); err != nil {
			return bpfman.AttachOutput{}, fmt.Errorf("pin %s to %s: %w", noun, linkPin, err)
		}
	}

	linkInfo, err := lnk.Info()
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("get link info: %w", err)
	}

	lnk.Close()
	success = true

	kernelLinkID := kernel.LinkID(linkInfo.ID)
	return bpfman.AttachOutput{
		KernelLinkID: &kernelLinkID,
		KernelLink:   ToKernelLink(linkInfo),
		PinPath:      linkPinPath,
	}, nil
}

// UpdateXDPDispatcherLink atomically updates an existing XDP
// dispatcher's BPF link to point to a new dispatcher program.
// This is used during rebuild to swap from old to new dispatcher.
func (k *kernelAdapter) UpdateXDPDispatcherLink(ctx context.Context, linkPinPath bpfman.LinkPath, newProgPinPath bpfman.ProgPinPath) error {
	lnk, err := link.LoadPinnedLink(linkPinPath.String(), nil)
	if err != nil {
		return fmt.Errorf("load pinned link %s: %w", linkPinPath, err)
	}
	defer lnk.Close()

	newProg, err := ebpf.LoadPinnedProgram(newProgPinPath.String(), nil)
	if err != nil {
		return fmt.Errorf("load pinned program %s: %w", newProgPinPath, err)
	}
	defer newProg.Close()

	if err := lnk.Update(newProg); err != nil {
		return fmt.Errorf("update XDP link to new dispatcher: %w", err)
	}

	k.logger.Debug("updated XDP dispatcher link", "link_pin", linkPinPath, "new_prog_pin", newProgPinPath)
	return nil
}

// LoadAndPinXDPDispatcher loads an XDP dispatcher program with .rodata
// config and pins it at progPinPath without creating an XDP link.
// Used during rebuild to prepare a new dispatcher before atomically
// swapping the link.
func (k *kernelAdapter) LoadAndPinXDPDispatcher(ctx context.Context, cfg dispatcher.XDPConfig, progPinPath bpfman.ProgPinPath) (kernel.ProgramID, error) {
	collSpec, err := dispatcher.LoadXDPDispatcher(cfg)
	if err != nil {
		return 0, fmt.Errorf("load XDP dispatcher spec: %w", err)
	}

	coll, err := ebpf.NewCollection(collSpec)
	if err != nil {
		return 0, fmt.Errorf("create XDP dispatcher collection: %w", err)
	}
	defer coll.Close()

	dispatcherProg := coll.Programs["xdp_dispatcher"]
	if dispatcherProg == nil {
		return 0, fmt.Errorf("xdp_dispatcher program not found in collection")
	}

	progInfo, err := dispatcherProg.Info()
	if err != nil {
		return 0, fmt.Errorf("get dispatcher program info: %w", err)
	}

	progID, ok := progInfo.ID()
	if !ok {
		return 0, fmt.Errorf("failed to get dispatcher program ID from kernel")
	}

	if err := pinWithRetry(progPinPath, dispatcherProg.Pin); err != nil {
		return 0, fmt.Errorf("pin dispatcher program to %s: %w", progPinPath, err)
	}

	k.logger.Debug("loaded and pinned XDP dispatcher", "program_id", progID, "prog_pin_path", progPinPath, "num_progs", cfg.NumProgsEnabled)
	return kernel.ProgramID(progID), nil
}

// CreateXDPLink creates an XDP link from a pinned dispatcher program
// to a network interface, optionally in a specific network namespace.
func (k *kernelAdapter) CreateXDPLink(ctx context.Context, progPinPath bpfman.ProgPinPath, ifindex int, linkPinPath bpfman.LinkPath, netnsPath string) (*platform.XDPDispatcherResult, error) {
	linkPin := linkPinPath.String()
	prog, err := ebpf.LoadPinnedProgram(progPinPath.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("load pinned program %s: %w", progPinPath, err)
	}
	defer prog.Close()

	progInfo, err := prog.Info()
	if err != nil {
		return nil, fmt.Errorf("get program info: %w", err)
	}

	progID, ok := progInfo.ID()
	if !ok {
		return nil, fmt.Errorf("failed to get program ID from kernel")
	}

	if netnsPath != "" {
		k.logger.Debug("entering network namespace for XDP link creation", "netns", netnsPath, "ifindex", ifindex)
	}

	var result *platform.XDPDispatcherResult
	err = netns.Run(netnsPath, func() error {
		k.logger.Debug("creating XDP link", "ifindex", ifindex, "prog_pin_path", progPinPath, "link_pin_path", linkPinPath, "netns", netnsPath)
		lnk, err := attachXDPWithRetry(link.XDPOptions{
			Program:   prog,
			Interface: ifindex,
		})
		if err != nil {
			k.logger.Debug("XDP link creation failed", "ifindex", ifindex, "error", err, "is_ebusy", errors.Is(err, syscall.EBUSY))
			if errors.Is(err, syscall.EBUSY) {
				return fmt.Errorf("attach XDP to ifindex %d: interface already has an XDP program attached: %w", ifindex, err)
			}
			return fmt.Errorf("attach XDP to ifindex %d: %w", ifindex, err)
		}

		linkInfo, err := lnk.Info()
		if err != nil {
			lnk.Close()
			return fmt.Errorf("get link info: %w", err)
		}

		if err := pinWithRetry(linkPin, lnk.Pin); err != nil {
			lnk.Close()
			return fmt.Errorf("pin link to %s: %w", linkPin, err)
		}

		lnk.Close()
		result = &platform.XDPDispatcherResult{
			DispatcherID:  kernel.ProgramID(progID),
			KernelLinkID:  kernel.LinkID(linkInfo.ID),
			DispatcherPin: progPinPath,
			LinkPin:       linkPinPath,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// AttachXDPExtension loads a pinned extension program and attaches it
// to a dispatcher slot via freplace. The extension was already loaded
// as BPF_PROG_TYPE_EXT during the initial Load, so no ELF re-read or
// map replacement is needed.
func (k *kernelAdapter) AttachXDPExtension(ctx context.Context, spec dispatcher.XDPExtensionAttachSpec) (bpfman.AttachOutput, error) {
	if err := spec.Validate(); err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("invalid spec: %w", err)
	}

	// Load the pinned dispatcher to use as attach target.
	dispatcherProg, err := ebpf.LoadPinnedProgram(spec.DispatcherPinPath.String(), nil)
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("load pinned dispatcher %s: %w", spec.DispatcherPinPath, err)
	}
	defer dispatcherProg.Close()

	// Load the pinned extension program.
	extensionProg, err := ebpf.LoadPinnedProgram(spec.ProgPinPath.String(), nil)
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("load pinned extension %s: %w", spec.ProgPinPath, err)
	}
	defer extensionProg.Close()

	// Attach the extension using freplace link.
	slotName, err := dispatcher.SlotName(spec.Position)
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("slot name for position %d: %w", spec.Position, err)
	}

	lnk, err := link.AttachFreplace(dispatcherProg, slotName, extensionProg)
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("attach freplace to %s: %w", slotName, err)
	}

	return k.finishPinnedAttach(lnk, spec.LinkPinPath, "extension link")
}

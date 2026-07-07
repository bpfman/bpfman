package ebpf

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
)

// AttachTracepoint attaches a pinned program to a tracepoint.
func (k *kernelAdapter) AttachTracepoint(ctx context.Context, progPinPath bpfman.ProgPinPath, group, name string, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error) {
	prog, err := ebpf.LoadPinnedProgram(progPinPath.String(), nil)
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("load pinned program %s: %w", progPinPath, err)
	}
	defer prog.Close()

	lnk, err := link.Tracepoint(group, name, prog, nil)
	if err != nil {
		// Preserve the domain-level not-found error when tracefs
		// enumeration was unavailable and manager preflight skipped.
		if isTracepointNotFoundError(err) {
			return bpfman.AttachOutput{}, bpfman.ErrTracepointNotFound{Group: group, Name: name}
		}
		return bpfman.AttachOutput{}, fmt.Errorf("attach to tracepoint %s/%s: %w", group, name, err)
	}

	return k.finishProbeAttach(lnk, linkPinPath)
}

// finishProbeAttach is the shared tail of the probe-style attaches
// (tracepoint, kprobe, fentry/fexit). It pins the link when linkPinPath
// is set, reads its info, and then either hands the live link to the
// adapter for a later Close or closes it. Probe-style links need an
// explicit Close after unpinning: pin-removal alone does not run
// perf_event_free_bpf_prog, so the program stays attached to the
// perf_event until the link object is Closed.
func (k *kernelAdapter) finishProbeAttach(lnk link.Link, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error) {
	linkPin := linkPinPath.String()

	if linkPin != "" {
		if err := pinWithRetry(linkPin, lnk.Pin); err != nil {
			lnk.Close()
			return bpfman.AttachOutput{}, fmt.Errorf("pin link to %s: %w", linkPin, err)
		}
	}

	linkInfo, err := lnk.Info()
	if err != nil {
		lnk.Close()
		return bpfman.AttachOutput{}, fmt.Errorf("get link info: %w", err)
	}

	if linkPin != "" {
		k.trackLink(linkPin, lnk)
	} else {
		lnk.Close()
	}

	kernelLinkID := kernel.LinkID(linkInfo.ID)
	return bpfman.AttachOutput{
		KernelLinkID: &kernelLinkID,
		KernelLink:   ToKernelLink(linkInfo),
		PinPath:      linkPinPath,
	}, nil
}

func isTracepointNotFoundError(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

// AttachKprobe attaches a pinned program to a kernel function.
// If retprobe is true, attaches as a kretprobe instead of kprobe.
func (k *kernelAdapter) AttachKprobe(ctx context.Context, progPinPath bpfman.ProgPinPath, fnName string, offset uint64, retprobe bool, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error) {
	prog, err := ebpf.LoadPinnedProgram(progPinPath.String(), nil)
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("load pinned program %s: %w", progPinPath, err)
	}
	defer prog.Close()

	// Build kprobe options
	opts := &link.KprobeOptions{
		Offset: offset,
	}

	// Attach as kprobe or kretprobe
	var lnk link.Link
	if retprobe {
		lnk, err = link.Kretprobe(fnName, prog, opts)
	} else {
		lnk, err = link.Kprobe(fnName, prog, opts)
	}
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("attach kprobe to %s: %w", fnName, err)
	}

	return k.finishProbeAttach(lnk, linkPinPath)
}

// AttachFentry attaches a pinned fentry program to a kernel function.
// The target function was specified at load time and is stored in the program.
func (k *kernelAdapter) AttachFentry(ctx context.Context, progPinPath bpfman.ProgPinPath, fnName string, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error) {
	return k.attachTracing(ctx, progPinPath, fnName, linkPinPath)
}

// AttachFexit attaches a pinned fexit program to a kernel function.
// The target function was specified at load time and is stored in the program.
func (k *kernelAdapter) AttachFexit(ctx context.Context, progPinPath bpfman.ProgPinPath, fnName string, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error) {
	return k.attachTracing(ctx, progPinPath, fnName, linkPinPath)
}

// attachTracing is the shared implementation for fentry and fexit attachment.
func (k *kernelAdapter) attachTracing(ctx context.Context, progPinPath bpfman.ProgPinPath, fnName string, linkPinPath bpfman.LinkPath) (bpfman.AttachOutput, error) {
	prog, err := ebpf.LoadPinnedProgram(progPinPath.String(), nil)
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("load pinned program %s: %w", progPinPath, err)
	}
	defer prog.Close()

	// Attach using link.AttachTracing - the program already has the target
	// function and attach type set from load time (via ELF section name).
	lnk, err := link.AttachTracing(link.TracingOptions{
		Program: prog,
	})
	if err != nil {
		return bpfman.AttachOutput{}, fmt.Errorf("attach tracing to %s: %w", fnName, err)
	}

	return k.finishProbeAttach(lnk, linkPinPath)
}

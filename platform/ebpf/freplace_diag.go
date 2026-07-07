package ebpf

import (
	"context"
	"fmt"

	"github.com/cilium/ebpf/link"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/platform"
)

// ExtensionLinkInfo loads a pinned freplace link, calls
// BPF_LINK_GET_INFO_BY_FD, and returns the kernel-reported tracing
// target. Diagnostic only: callers use this to verify each
// just-attached freplace is observably installed in the kernel before
// committing the dispatcher swap.
func (k *kernelAdapter) ExtensionLinkInfo(ctx context.Context, linkPinPath bpfman.LinkPath) (platform.ExtensionLinkInfo, error) {
	lnk, err := link.LoadPinnedLink(linkPinPath.String(), nil)
	if err != nil {
		return platform.ExtensionLinkInfo{}, fmt.Errorf("load pinned extension link %s: %w", linkPinPath, err)
	}
	defer lnk.Close()

	info, err := lnk.Info()
	if err != nil {
		return platform.ExtensionLinkInfo{}, fmt.Errorf("get link info for %s: %w", linkPinPath, err)
	}

	out := platform.ExtensionLinkInfo{
		KernelLinkID: kernel.LinkID(info.ID),
	}
	if t := info.Tracing(); t != nil {
		out.TargetProgID = kernel.ProgramID(t.TargetObjId)
		out.TargetBtfID = uint32(t.TargetBtfId)
		out.AttachType = uint32(t.AttachType)
	}
	return out, nil
}

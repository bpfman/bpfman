package ebpf

import (
	"fmt"

	"github.com/cilium/ebpf/link"

	"github.com/bpfman/bpfman/kernel"
)

// ToKernelLink converts a cilium/ebpf link.Info to a kernel.Link.
func ToKernelLink(info *link.Info) *kernel.Link {
	if info == nil {
		return nil
	}

	kl := &kernel.Link{
		ID:        kernel.LinkID(info.ID),
		ProgramID: kernel.ProgramID(info.Program),
		LinkType:  linkTypeString(info.Type),
	}

	// Extract type-specific fields
	if tracing := info.Tracing(); tracing != nil {
		kl.AttachType = fmt.Sprintf("%d", tracing.AttachType)
		kl.TargetObjID = tracing.TargetObjId
		kl.TargetBTFId = uint32(tracing.TargetBtfId)
	}
	if xdp := info.XDP(); xdp != nil {
		kl.Ifindex = xdp.Ifindex
	}
	if tcx := info.TCX(); tcx != nil {
		kl.AttachType = fmt.Sprintf("%d", tcx.AttachType)
		kl.Ifindex = tcx.Ifindex
	}
	if cgroup := info.Cgroup(); cgroup != nil {
		kl.AttachType = fmt.Sprintf("%d", cgroup.AttachType)
		kl.CgroupID = cgroup.CgroupId
	}
	if netns := info.NetNs(); netns != nil {
		kl.AttachType = fmt.Sprintf("%d", netns.AttachType)
		kl.NetnsIno = netns.NetnsIno
	}
	if netkit := info.Netkit(); netkit != nil {
		kl.AttachType = fmt.Sprintf("%d", netkit.AttachType)
		kl.Ifindex = netkit.Ifindex
	}
	if kprobeMulti := info.KprobeMulti(); kprobeMulti != nil {
		// KprobeMulti fields are accessed via methods
		if count, ok := kprobeMulti.AddressCount(); ok {
			kl.KprobeMultiCount = count
		}
		if flags, ok := kprobeMulti.Flags(); ok {
			kl.KprobeMultiFlags = flags
		}
		if missed, ok := kprobeMulti.Missed(); ok {
			kl.KprobeMultiMissed = missed
		}
	}
	if perf := info.PerfEvent(); perf != nil {
		// PerfEvent fields are accessed via methods
		if kprobe := perf.Kprobe(); kprobe != nil {
			if addr, ok := kprobe.Address(); ok {
				kl.KprobeAddress = addr
			}
			if missed, ok := kprobe.Missed(); ok {
				kl.KprobeMissed = missed
			}
		}
	}
	if netfilter := info.Netfilter(); netfilter != nil {
		kl.NetfilterPf = netfilter.Pf
		kl.NetfilterHooknum = netfilter.Hooknum
		kl.NetfilterPriority = netfilter.Priority
		kl.NetfilterFlags = netfilter.Flags
	}

	return kl
}

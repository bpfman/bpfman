package ebpf

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
)

// inferProgramType returns the program type based on the ELF section name.
// This follows the Rust bpfman approach of deriving the type from bytecode
// metadata rather than relying on user-specified types.
//
// Section name patterns (from cilium/ebpf elf_sections.go):
//   - kprobe/*, kprobe.multi/* -> kprobe
//   - kretprobe/*, kretprobe.multi/* -> kretprobe
//   - uprobe/*, uprobe.multi/* -> uprobe
//   - uretprobe/*, uretprobe.multi/* -> uretprobe
//   - tracepoint/* -> tracepoint
//   - xdp*, xdp.frags* -> xdp
//   - tc, classifier/* -> tc
//   - tcx/* -> tcx
//   - fentry/* -> fentry
//   - fexit/* -> fexit
func inferProgramType(sectionName string) bpfman.ProgramType {
	// Remove optional program marking prefix
	sectionName = strings.TrimPrefix(sectionName, "?")

	switch {
	case strings.HasPrefix(sectionName, "kretprobe"):
		return bpfman.ProgramTypeKretprobe
	case strings.HasPrefix(sectionName, "kprobe"):
		return bpfman.ProgramTypeKprobe
	case strings.HasPrefix(sectionName, "uretprobe"):
		return bpfman.ProgramTypeUretprobe
	case strings.HasPrefix(sectionName, "uprobe"):
		return bpfman.ProgramTypeUprobe
	case strings.HasPrefix(sectionName, "tracepoint"):
		return bpfman.ProgramTypeTracepoint
	case strings.HasPrefix(sectionName, "fentry"):
		return bpfman.ProgramTypeFentry
	case strings.HasPrefix(sectionName, "fexit"):
		return bpfman.ProgramTypeFexit
	case strings.HasPrefix(sectionName, "xdp"):
		return bpfman.ProgramTypeXDP
	case strings.HasPrefix(sectionName, "tcx"):
		return bpfman.ProgramTypeTCX
	case strings.HasPrefix(sectionName, "tc") || strings.HasPrefix(sectionName, "classifier"):
		return bpfman.ProgramTypeTC
	default:
		return ""
	}
}

// sectionFamily groups the bpfman program types that may legitimately
// be loaded from the same ELF section. Members of a family share a
// kernel program type and a section-name prefix; the remaining
// distinction is settled after load rather than encoded in the section:
//
//   - kprobe/kretprobe and uprobe/uretprobe: both halves are
//     BPF_PROG_TYPE_KPROBE; entry vs return is chosen at
//     perf_event_open, not load. kprobe and uprobe are kept in
//     separate families: they share a kernel type but the section
//     encodes distinct intent (kernel function vs user-space binary),
//     and the attach layer treats them as distinct verbs.
//   - tc/tcx: both BPF_PROG_TYPE_SCHED_CLS. tcx objects are compiled
//     with the classifier SEC, so the section infers tc; loading such
//     an object as tcx is routine and must be allowed.
//
// Every other type forms its own singleton family, so a declared type
// that contradicts the section (e.g. xdp from a kprobe SEC, or fentry
// from a fexit SEC) is a genuine mismatch.
func sectionFamily(t bpfman.ProgramType) string {
	switch t {
	case bpfman.ProgramTypeKprobe, bpfman.ProgramTypeKretprobe:
		return "kprobe"
	case bpfman.ProgramTypeUprobe, bpfman.ProgramTypeUretprobe:
		return "uprobe"
	case bpfman.ProgramTypeTC, bpfman.ProgramTypeTCX:
		return "schedcls"
	default:
		return string(t)
	}
}

// declaredTypeMatchesSection reports whether a program declared as
// `declared` may be loaded from an ELF section that infers `inferred`.
// They match when they belong to the same section family.
func declaredTypeMatchesSection(declared, inferred bpfman.ProgramType) bool {
	return sectionFamily(declared) == sectionFamily(inferred)
}

// bootTime returns the system boot time by reading /proc/stat.
// Falls back to time.Now() if /proc/stat cannot be read.
func bootTime() time.Time {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Now()
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "btime ") {
			var btime int64
			if _, err := fmt.Sscanf(line, "btime %d", &btime); err == nil {
				return time.Unix(btime, 0)
			}
		}
	}
	return time.Now()
}

func infoToMap(info *ebpf.MapInfo, id kernel.MapID) kernel.Map {
	km := kernel.Map{
		ID:         id,
		Name:       info.Name,
		MapType:    kernel.NewMapType(info.Type.String()),
		KeySize:    info.KeySize,
		ValueSize:  info.ValueSize,
		MaxEntries: info.MaxEntries,
		Flags:      info.Flags,
		Frozen:     info.Frozen(),
	}

	// BTF ID (available from kernel 4.18)
	if btfID, ok := info.BTFID(); ok {
		km.BTFId = uint32(btfID)
		km.HasBTFId = true
	}

	// MapExtra (available from kernel 5.16)
	if mapExtra, ok := info.MapExtra(); ok {
		km.MapExtra = mapExtra
		km.HasMapExtra = true
	}

	// Memlock (available from kernel 4.10)
	if memlock, ok := info.Memlock(); ok {
		km.Memlock = memlock
		km.HasMemlock = true
	}

	return km
}

// infoToLink converts a cilium/ebpf link.Info to a kernel.Link value.
// It delegates to the canonical ToKernelLink so the two conversions
// cannot drift; callers here want a value rather than a pointer.
func infoToLink(info *link.Info) kernel.Link {
	return *ToKernelLink(info)
}

// linkTypeString converts a link.Type to a human-readable string.
func linkTypeString(t link.Type) string {
	// These values come from include/uapi/linux/bpf.h (BPF_LINK_TYPE_*)
	names := map[link.Type]string{
		0:  "unspec",
		1:  "raw_tracepoint",
		2:  "tracing",
		3:  "cgroup",
		4:  "iter",
		5:  "netns",
		6:  "xdp",
		7:  "perf_event",
		8:  "kprobe_multi",
		9:  "struct_ops",
		10: "netfilter",
		11: "tcx",
		12: "uprobe_multi",
		13: "netkit",
	}
	if name, ok := names[t]; ok {
		return name
	}
	return fmt.Sprintf("unknown(%d)", t)
}

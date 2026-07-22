package ebpf

import (
	"errors"
	"sync"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"golang.org/x/sys/unix"
)

var (
	xdpFragsOnce      sync.Once
	xdpFragsSupported bool
)

// haveXDPFrags reports whether the running kernel accepts
// BPF_F_XDP_HAS_FRAGS at program load. The flag was added in Linux 5.18;
// on older kernels BPF_PROG_LOAD rejects the unknown prog_flags bit with
// EINVAL. cilium/ebpf has no feature probe for program flags, so this
// probes directly, once, by loading a trivial XDP program with the flag
// set and caching the outcome. A load probe rather than a version check
// so it stays correct across backports.
func haveXDPFrags() bool {
	xdpFragsOnce.Do(func() {
		prog, err := ebpf.NewProgram(&ebpf.ProgramSpec{
			Name:    "probe_xdp_frags",
			Type:    ebpf.XDP,
			Flags:   unix.BPF_F_XDP_HAS_FRAGS,
			License: "GPL",
			Instructions: asm.Instructions{
				asm.Mov.Imm(asm.R0, 2), // XDP_PASS
				asm.Return(),
			},
		})

		switch {
		case err == nil:
			prog.Close()
			xdpFragsSupported = true
		case errors.Is(err, unix.EINVAL):
			// The kernel rejected the flag itself: frags
			// is genuinely unsupported (pre-5.18). Cache
			// the negative.
			xdpFragsSupported = false
		default:
			// The probe failed for a reason unrelated to
			// the flag -- a kernel that lacks frags
			// returns EINVAL. Assume supported rather
			// than caching a false negative that would
			// silently strip frags from every program for
			// the rest of the process; if the kernel
			// really lacks frags the real load is still
			// rejected.
			xdpFragsSupported = true
		}
	})

	return xdpFragsSupported
}

// resolveXDPFrags returns the program flags to load with and whether the
// program is frags-capable, given the flags the spec declares (the loader
// sets BPF_F_XDP_HAS_FRAGS from an xdp.frags section) and whether the
// running kernel supports the flag. On a kernel without frags support the
// flag is stripped so the program loads as non-frags rather than being
// rejected at load. This is the pure decision behind the probe; Load
// feeds haveXDPFrags() in as kernelHasFrags.
func resolveXDPFrags(specFlags uint32, kernelHasFrags bool) (flags uint32, hasFrags bool) {
	hasFrags = specFlags&unix.BPF_F_XDP_HAS_FRAGS != 0
	if hasFrags && !kernelHasFrags {
		return specFlags &^ unix.BPF_F_XDP_HAS_FRAGS, false
	}
	return specFlags, hasFrags
}

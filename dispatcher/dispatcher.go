package dispatcher

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/cilium/ebpf"
	"golang.org/x/sys/unix"

	"github.com/bpfman/bpfman"
)

// MaxPrograms is the maximum number of programs that can be chained.
const MaxPrograms = 10

// XDP dispatcher bytecode - compiled from xdp_dispatcher_v2.bpf.c
//
//go:embed xdp_dispatcher_v2.bpf.o
var xdpDispatcherBytes []byte

// TC dispatcher bytecode - compiled from tc_dispatcher.bpf.c
//
//go:embed tc_dispatcher.bpf.o
var tcDispatcherBytes []byte

// XDPConfig configures the XDP dispatcher.
// This must match struct xdp_dispatcher_conf in xdp_dispatcher_v2.bpf.c:
// the value is copied verbatim into the dispatcher program's "conf"
// variable, so field order, widths, and padding are load-bearing.
type XDPConfig struct {
	// Magic is the ABI tag the dispatcher program checks to confirm the
	// config layout it was handed. NewXDPConfig sets it to 236
	// (xdpDispatcherMagic).
	Magic uint8

	// DispatcherVersion is the dispatcher ABI version. NewXDPConfig sets
	// it to 2 (xdpDispatcherVersion).
	DispatcherVersion uint8

	// NumProgsEnabled is the number of chained extension programs the
	// dispatcher runs, in the range [1, MaxPrograms]. Only the first
	// NumProgsEnabled slots of the per-slot arrays are meaningful.
	NumProgsEnabled uint8

	// IsXDPFrags, when nonzero, marks the dispatcher as XDP-fragments
	// aware.
	IsXDPFrags uint8

	// ChainCallActions holds the per-slot proceed-on bitmask: element i
	// applies to slot i, and when the program in that slot returns one
	// of the encoded actions the dispatcher proceeds to the next slot.
	// See ProceedOnMask for the bit encoding.
	ChainCallActions [MaxPrograms]uint32

	// RunPrios holds the per-slot run priority. NewXDPConfig fills every
	// element with DispatcherRunPriority. It mirrors Rust bpfman's
	// config but does not determine chain order: slots are ordered by
	// stored priority and position before the config is built.
	RunPrios [MaxPrograms]uint32

	// ProgramFlags holds the per-slot program flags.
	ProgramFlags [MaxPrograms]uint32
}

// TCConfig configures the TC dispatcher.
// This must match struct tc_dispatcher_config in tc_dispatcher.bpf.c:
// the value is copied verbatim into the dispatcher program's "CONFIG"
// variable, so field order, widths, and padding are load-bearing.
type TCConfig struct {
	// NumProgsEnabled is the number of chained extension programs the
	// dispatcher runs, in the range [1, MaxPrograms]. Only the first
	// NumProgsEnabled slots of the per-slot arrays are meaningful.
	NumProgsEnabled uint8

	// _ pads the struct so ChainCallActions is 4-byte aligned, matching
	// the C layout.
	_ [3]uint8

	// ChainCallActions holds the per-slot proceed-on bitmask: element i
	// applies to slot i, and when the program in that slot returns one
	// of the encoded actions the dispatcher proceeds to the next slot.
	// See ProceedOnMask for the bit encoding.
	ChainCallActions [MaxPrograms]uint32

	// RunPrios holds the per-slot run priority. NewTCConfig fills every
	// element with DispatcherRunPriority. It mirrors Rust bpfman's
	// config but does not determine chain order: slots are ordered by
	// stored priority and position before the config is built.
	RunPrios [MaxPrograms]uint32
}

const (
	// XDP dispatcher constants from xdp_dispatcher_v2.bpf.c
	xdpDispatcherMagic   = 236
	xdpDispatcherVersion = 2

	// DispatcherRunPriority is the value written into the dispatcher
	// run_prios array. It mirrors Rust bpfman's dispatcher config but
	// does not determine chain order; user links are ordered by their
	// stored priority and position.
	DispatcherRunPriority = 50
)

// XDPFragsMode controls whether an XDP dispatcher is loaded as
// fragments-aware. It is a defined bool so call sites read
// XDPFragsEnabled/XDPFragsDisabled rather than a bare bool, while the
// type can hold no value other than the two below.
type XDPFragsMode bool

const (
	XDPFragsDisabled XDPFragsMode = false
	XDPFragsEnabled  XDPFragsMode = true
)

// FragsEligible reports whether an XDP dispatcher built from these
// members should be loaded frags-aware: the set is non-empty and every
// member is frags-capable. Empty, or any non-frags member, yields false.
// TC members are never frags-capable, so this returns false for a TC
// dispatcher. It is the single definition of the frags-eligibility rule,
// shared by the load decision and the reported dispatcher snapshot.
//
// A member is frags-capable when its XDP program is compiled with a
// SEC("xdp.frags") section: cilium/ebpf reads that section and sets
// BPF_F_XDP_HAS_FRAGS at load, which bpfman records per member and passes
// here as memberFrags. A plain SEC("xdp") program never carries the flag.
//
// The all-or-nothing rule is the libxdp dispatcher policy, not a kernel
// constraint. BPF_F_XDP_HAS_FRAGS is a property of the dispatcher as a
// whole -- it cannot be set per freplace member -- so it is only safe to
// set when every member is written to handle fragmented packets; a
// non-frags member exposed to frags can silently misbehave, and the
// kernel cannot detect this. See lib/libxdp/protocol.org, "Supporting XDP
// programs with frags support", in xdp-project/xdp-tools.
func FragsEligible(memberFrags []bool) bool {
	if len(memberFrags) == 0 {
		return false
	}

	for _, frags := range memberFrags {
		if !frags {
			return false
		}
	}

	return true
}

func proceedOnOffset(dt DispatcherType) (int32, error) {
	switch dt {
	case DispatcherTypeXDP:
		return 0, nil
	case DispatcherTypeTCIngress, DispatcherTypeTCEgress:
		return 1, nil
	default:
		return 0, fmt.Errorf("unknown dispatcher type %q", dt)
	}
}

func validateProceedOnCode(dt DispatcherType, code int32) error {
	switch dt {
	case DispatcherTypeXDP:
		_, err := bpfman.XDPActionFromInt32(code)
		return err
	case DispatcherTypeTCIngress, DispatcherTypeTCEgress:
		_, err := bpfman.TCActionFromInt32(code)
		return err
	default:
		return fmt.Errorf("unknown dispatcher type %q", dt)
	}
}

// ProceedOnMask returns the final dispatcher ABI bitmask for the given
// action codes. If a program returns one of these actions, the
// dispatcher continues to the next program in the chain.
func ProceedOnMask(dt DispatcherType, codes ...int32) (uint32, error) {
	offset, err := proceedOnOffset(dt)
	if err != nil {
		return 0, err
	}
	var mask uint32
	for _, code := range codes {
		if err := validateProceedOnCode(dt, code); err != nil {
			return 0, err
		}
		bit := code + offset
		if bit < 0 || bit >= 32 {
			return 0, fmt.Errorf("proceed-on action code %d maps to invalid bit %d", code, bit)
		}
		mask |= 1 << uint(bit)
	}
	return mask, nil
}

// ProceedOnActions decodes a dispatcher ABI bitmask into action codes.
func ProceedOnActions(dt DispatcherType, mask uint32) ([]int32, error) {
	offset, err := proceedOnOffset(dt)
	if err != nil {
		return nil, err
	}
	var actions []int32
	for bit := range 32 {
		if mask&(1<<uint(bit)) == 0 {
			continue
		}
		code := int32(bit) - offset
		if err := validateProceedOnCode(dt, code); err != nil {
			return nil, err
		}
		actions = append(actions, code)
	}
	return actions, nil
}

// NewXDPConfig creates a default XDP dispatcher config. numProgs
// must be in the range [1, MaxPrograms].
func NewXDPConfig(numProgs int, fragsMode XDPFragsMode) (XDPConfig, error) {
	if numProgs < 1 || numProgs > MaxPrograms {
		return XDPConfig{}, fmt.Errorf("numProgs %d out of range [1, %d]", numProgs, MaxPrograms)
	}
	cfg := XDPConfig{
		Magic:             xdpDispatcherMagic,
		DispatcherVersion: xdpDispatcherVersion,
		NumProgsEnabled:   uint8(numProgs),
	}
	if fragsMode == XDPFragsEnabled {
		cfg.IsXDPFrags = 1
	}
	for i := range MaxPrograms {
		cfg.RunPrios[i] = DispatcherRunPriority
		if fragsMode == XDPFragsEnabled && i < numProgs {
			cfg.ProgramFlags[i] = unix.BPF_F_XDP_HAS_FRAGS
		}
	}
	return cfg, nil
}

// NewTCConfig creates a default TC dispatcher config. numProgs must
// be in the range [1, MaxPrograms].
func NewTCConfig(numProgs int) (TCConfig, error) {
	if numProgs < 1 || numProgs > MaxPrograms {
		return TCConfig{}, fmt.Errorf("numProgs %d out of range [1, %d]", numProgs, MaxPrograms)
	}
	cfg := TCConfig{
		NumProgsEnabled: uint8(numProgs),
	}
	for i := range MaxPrograms {
		cfg.RunPrios[i] = DispatcherRunPriority
	}
	return cfg, nil
}

// LoadXDPDispatcher loads the XDP dispatcher with the given config.
func LoadXDPDispatcher(cfg XDPConfig) (*ebpf.CollectionSpec, error) {
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(xdpDispatcherBytes))
	if err != nil {
		return nil, fmt.Errorf("load XDP dispatcher spec: %w", err)
	}

	confVar, ok := spec.Variables["conf"]
	if !ok {
		return nil, fmt.Errorf("XDP dispatcher missing 'conf' variable")
	}
	if err := confVar.Set(cfg); err != nil {
		return nil, fmt.Errorf("set XDP dispatcher config: %w", err)
	}

	return spec, nil
}

// LoadTCDispatcher loads the TC dispatcher with the given config.
func LoadTCDispatcher(cfg TCConfig) (*ebpf.CollectionSpec, error) {
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(tcDispatcherBytes))
	if err != nil {
		return nil, fmt.Errorf("load TC dispatcher spec: %w", err)
	}

	confVar, ok := spec.Variables["CONFIG"]
	if !ok {
		return nil, fmt.Errorf("TC dispatcher missing 'CONFIG' variable")
	}
	if err := confVar.Set(cfg); err != nil {
		return nil, fmt.Errorf("set TC dispatcher config: %w", err)
	}

	return spec, nil
}

// SlotName returns the function name for a dispatcher slot. Position
// must be in the range [0, MaxPrograms). This is the target function
// name used for BPF extension attachment.
func SlotName(position int) (string, error) {
	if position < 0 || position >= MaxPrograms {
		return "", fmt.Errorf("position %d out of range [0, %d)", position, MaxPrograms)
	}
	return fmt.Sprintf("prog%d", position), nil
}

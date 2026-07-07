// Little-endian hex builtins for the bpfman global-data injection
// flow. `bpfman -g NAME=HEX` takes a hex byte string; tests that
// inject runtime-computed values (a worker PID, a per-program
// weight) need to convert decimal integers to LE-byte hex without
// shelling out to printf for the bit-twiddling.
//
// One named builtin per width, matching the C declarations users
// see in their .bpf.c (volatile const __u32, __u64).
package builtins

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

// HandleU32LE parses args[0] as a non-negative integer that fits
// in a uint32 and returns its little-endian 4-byte hex string (8
// hex characters, lowercase, no 0x prefix).
//
// Examples:
//
//	u32le 0          -> "00000000"
//	u32le 1          -> "01000000"
//	u32le 12345      -> "39300000"
//	u32le 4294967295 -> "ffffffff"
func HandleU32LE(c driver.Ctx) (runtime.Value, error) {
	n, err := singleUintArg("u32le", c.Args, math.MaxUint32)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.ValueFromAny(formatLE(n, 4)).WithKind(semantics.OriginScalar), nil
}

// HandleU64LE parses args[0] as a non-negative integer that fits
// in a uint64 and returns its little-endian 8-byte hex string (16
// hex characters, lowercase, no 0x prefix).
//
// Examples:
//
//	u64le 0    -> "0000000000000000"
//	u64le 1    -> "0100000000000000"
//	u64le 42   -> "2a00000000000000"
func HandleU64LE(c driver.Ctx) (runtime.Value, error) {
	n, err := singleUintArg("u64le", c.Args, math.MaxUint64)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.ValueFromAny(formatLE(n, 8)).WithKind(semantics.OriginScalar), nil
}

// singleUintArg parses exactly one positional argument as a
// non-negative integer no larger than max. The verb name is used
// only for error-message context.
func singleUintArg(verb string, args []runtime.Arg, max uint64) (uint64, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("%s: expected exactly 1 argument, got %d", verb, len(args))
	}
	text := strings.TrimSpace(driver.ArgText(args[0]))
	if text == "" {
		return 0, fmt.Errorf("%s: empty argument", verb)
	}
	if strings.HasPrefix(text, "-") {
		return 0, fmt.Errorf("%s: negative values are not representable (got %q)", verb, text)
	}
	n, err := strconv.ParseUint(text, 0, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q: %w", verb, text, err)
	}

	if n > max {
		return 0, fmt.Errorf("%s: value %d does not fit in %d bits", verb, n, bitsForMax(max))
	}
	return n, nil
}

// formatLE writes n as a width-byte little-endian hex string. The
// caller has already range-checked n against the width's maximum,
// so any high bits that exceed `width` bytes are silently dropped
// here (they cannot exist in a value singleUintArg let through).
func formatLE(n uint64, width int) string {
	var sb strings.Builder
	sb.Grow(width * 2)
	for i := range width {
		fmt.Fprintf(&sb, "%02x", byte(n>>(8*i)))
	}
	return sb.String()
}

// bitsForMax returns 32 for math.MaxUint32 and 64 for
// math.MaxUint64. Used only for error messages.
func bitsForMax(max uint64) int {
	if max == math.MaxUint32 {
		return 32
	}
	return 64
}

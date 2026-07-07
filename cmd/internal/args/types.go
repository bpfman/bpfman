package args

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
)

// ParseProgramID parses a decimal kernel program ID. It is the Kong
// mapper for kernel.ProgramID arguments and flags.
func ParseProgramID(s string) (kernel.ProgramID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("program ID cannot be empty")
	}

	val, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid program ID %q: must be a decimal number", s)
	}

	return kernel.ProgramID(val), nil
}

// ParseLinkID parses a decimal link ID. It is the Kong mapper for
// bpfman.LinkID arguments and flags.
func ParseLinkID(s string) (bpfman.LinkID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("link ID cannot be empty")
	}

	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid link ID %q: must be a decimal number", s)
	}

	return bpfman.LinkID(val), nil
}

// KeyValue represents a KEY=VALUE metadata pair.
type KeyValue struct {
	// Key is the metadata key, trimmed of surrounding whitespace.
	Key string

	// Value is the metadata value, taken verbatim from after the first "=".
	Value string
}

// ParseKeyValue parses a KEY=VALUE string.
func ParseKeyValue(s string) (KeyValue, error) {
	idx := strings.Index(s, "=")
	if idx <= 0 {
		return KeyValue{}, fmt.Errorf("invalid format %q: expected KEY=VALUE", s)
	}

	key := strings.TrimSpace(s[:idx])
	if key == "" {
		return KeyValue{}, fmt.Errorf("invalid format %q: key cannot be empty", s)
	}

	return KeyValue{
		Key:   key,
		Value: s[idx+1:],
	}, nil
}

// GlobalData represents a NAME=HEX global data pair.
type GlobalData struct {
	// Name is the global variable name, trimmed of surrounding whitespace.
	Name string

	// Data is the decoded bytes parsed from the hex value (an optional 0x prefix is stripped before decoding).
	Data []byte
}

// ParseGlobalData parses a NAME=HEX string.
func ParseGlobalData(s string) (GlobalData, error) {
	idx := strings.Index(s, "=")
	if idx <= 0 {
		return GlobalData{}, fmt.Errorf("invalid format %q: expected NAME=HEX", s)
	}

	name := strings.TrimSpace(s[:idx])
	if name == "" {
		return GlobalData{}, fmt.Errorf("invalid format %q: name cannot be empty", s)
	}

	hexStr := strings.TrimSpace(s[idx+1:])
	// Remove optional 0x prefix
	hexStr = strings.TrimPrefix(hexStr, "0x")
	hexStr = strings.TrimPrefix(hexStr, "0X")

	data, err := hex.DecodeString(hexStr)
	if err != nil {
		return GlobalData{}, fmt.Errorf("invalid hex data for %q: %w", name, err)
	}

	return GlobalData{
		Name: name,
		Data: data,
	}, nil
}

// ParseObjectPath validates that s names an existing, non-directory
// file and returns the path.
func ParseObjectPath(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("object path cannot be empty")
	}

	info, err := os.Stat(s)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("object file %q does not exist", s)
		}
		return "", fmt.Errorf("cannot access object file %q: %w", s, err)
	}

	if info.IsDir() {
		return "", fmt.Errorf("object path %q is a directory, not a file", s)
	}

	return s, nil
}

// MetadataMap converts a slice of KeyValue to a map.
func MetadataMap(kvs []KeyValue) map[string]string {
	if len(kvs) == 0 {
		return nil
	}
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		m[kv.Key] = kv.Value
	}
	return m
}

// GlobalDataMap converts a slice of GlobalData to a map.
func GlobalDataMap(gds []GlobalData) map[string][]byte {
	if len(gds) == 0 {
		return nil
	}
	m := make(map[string][]byte, len(gds))
	for _, gd := range gds {
		m[gd.Name] = gd.Data
	}
	return m
}

// ProgramSpec represents a TYPE:NAME[:ATTACH_FUNC] program specification for loading.
// For fentry and fexit, the attach function is required.
type ProgramSpec struct {
	// Type is the validated program type parsed from the spec's first component.
	Type bpfman.ProgramType

	// Name is the program name within the ELF object, taken from the spec's second component.
	Name string

	// AttachFunc is the optional attach function from the spec's third component; it is required for fentry and fexit.
	AttachFunc string
}

// ParseProgramSpec parses a TYPE:NAME or TYPE:NAME:ATTACH_FUNC string.
// Examples:
//   - "xdp:xdp_pass"
//   - "tc:stats"
//   - "fentry:test_fentry:do_unlinkat"
//
// The type is validated against known program types at parse time.
// For fentry and fexit, the attach function (third component) is required.
func ParseProgramSpec(s string) (ProgramSpec, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return ProgramSpec{}, fmt.Errorf("program spec cannot be empty")
	}

	parts := strings.SplitN(s, ":", 3)
	if len(parts) < 2 {
		return ProgramSpec{}, fmt.Errorf("invalid program spec %q: expected TYPE:NAME format (e.g., xdp:my_prog)", s)
	}

	typeStr := strings.TrimSpace(parts[0])
	progName := strings.TrimSpace(parts[1])
	var attachFunc string
	if len(parts) == 3 {
		attachFunc = strings.TrimSpace(parts[2])
	}

	if typeStr == "" {
		return ProgramSpec{}, fmt.Errorf("invalid program spec %q: type cannot be empty", s)
	}
	if progName == "" {
		return ProgramSpec{}, fmt.Errorf("invalid program spec %q: name cannot be empty", s)
	}

	progType, err := bpfman.ParseProgramType(typeStr)
	if err != nil {
		return ProgramSpec{}, fmt.Errorf("invalid program spec %q: unknown type %q (valid: xdp, tc, tcx, tracepoint, kprobe, kretprobe, uprobe, uretprobe, fentry, fexit)", s, typeStr)
	}

	// Validate fentry/fexit require attach function
	if (progType == bpfman.ProgramTypeFentry || progType == bpfman.ProgramTypeFexit) && attachFunc == "" {
		return ProgramSpec{}, fmt.Errorf("invalid program spec %q: %s requires attach function (format: %s:FUNC_NAME:ATTACH_FUNC)", s, typeStr, typeStr)
	}

	return ProgramSpec{
		Type:       progType,
		Name:       progName,
		AttachFunc: attachFunc,
	}, nil
}

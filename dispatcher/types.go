package dispatcher

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

// DispatcherType represents the type of dispatcher (XDP or TC).
// The unexported field prevents construction of invalid values; use the
// package-level variables or ParseDispatcherType.
type DispatcherType struct{ v string }

// The three dispatcher types. XDP dispatchers attach via a BPF link;
// the TC variants attach via a netlink tc filter on the named
// direction.
var (
	DispatcherTypeXDP       = DispatcherType{"xdp"}
	DispatcherTypeTCIngress = DispatcherType{"tc-ingress"}
	DispatcherTypeTCEgress  = DispatcherType{"tc-egress"}
)

// String returns the dispatcher type's canonical name ("xdp",
// "tc-ingress", or "tc-egress").
func (d DispatcherType) String() string { return d.v }

// MarshalText implements encoding.TextMarshaler, encoding the type as its name.
func (d DispatcherType) MarshalText() ([]byte, error) { return []byte(d.v), nil }

// UnmarshalText implements encoding.TextUnmarshaler, parsing the type
// name via ParseDispatcherType.
func (d *DispatcherType) UnmarshalText(b []byte) error {
	parsed, err := ParseDispatcherType(string(b))
	if err != nil {
		return err
	}

	*d = parsed
	return nil
}

// TCParentHandle returns the netlink parent handle for a TC
// dispatcher type.
func TCParentHandle(dispType DispatcherType) uint32 {
	switch dispType {
	case DispatcherTypeTCIngress:
		return netlink.HANDLE_MIN_INGRESS
	case DispatcherTypeTCEgress:
		return netlink.HANDLE_MIN_EGRESS
	default:
		return 0
	}
}

// ParseDispatcherType parses a string into a DispatcherType.
func ParseDispatcherType(s string) (DispatcherType, error) {
	switch s {
	case "xdp":
		return DispatcherTypeXDP, nil
	case "tc-ingress":
		return DispatcherTypeTCIngress, nil
	case "tc-egress":
		return DispatcherTypeTCEgress, nil
	default:
		return DispatcherType{}, fmt.Errorf("unknown dispatcher type %q", s)
	}
}

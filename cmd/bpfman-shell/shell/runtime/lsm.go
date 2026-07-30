package runtime

import (
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

// LsmProbe is the user-visible handle for one LSM fixture probe,
// produced by `lsm probe` and consumed by `lsm fire`. It owns a unique
// marker comm and a target file: `lsm fire` opens the target under the
// marker comm so an LSM program filtering file_open by that comm counts
// exactly the fixture's opens, isolated from the host's own file
// activity. The marker is carried into the program at load time via the
// marker_hex global-data value.
type LsmProbe struct {
	// Marker is the 8-byte comm the fire worker sets on itself before
	// opening the target; the attach point for the program's filter.
	Marker string

	// MarkerHex is Marker's bytes in order as hex, for the load-time
	// `-g target_comm=0x<hex>` global that seeds the program's filter.
	MarkerHex string

	// File is the target file the fire worker opens; each open is one
	// file_open event carrying the marker comm.
	File string

	// Dir is the tempdir owning File, for the script to remove on
	// cleanup (`defer exec rm -rf $probe.dir`).
	Dir string
}

// ValueFromLsmProbe wraps p as a Value with semantics.OriginLsm.
func ValueFromLsmProbe(p *LsmProbe) Value {
	mirror := map[string]any{
		"marker":     p.Marker,
		"marker_hex": p.MarkerHex,
		"file":       p.File,
		"dir":        p.Dir,
	}
	return Value{v: mirror, origin: p, kind: semantics.OriginLsm}
}

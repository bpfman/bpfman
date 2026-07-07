package residue

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// E2EUnmanagedPinPrefix is the basename prefix the e2e scripts give to
// programs they load out-of-band with bpftool and pin on the system
// bpffs, so bpfman observes them as kernel-only. The scripts remove
// these pins with a defer on the happy path; this prefix lets the
// residue sweep reclaim ones left behind when a script aborts (e.g.
// failfast) before its defer runs.
//
// The pins are namespaced by the kernel program ID, so a leak never
// blocks a re-run -- the path is never reused -- it only accumulates,
// and this sweep is what reclaims the accumulation between reboots.
//
// Kept in sync by convention with the pin paths in e2e/scripts/*.bpfman,
// which hardcode the literal prefix.
const E2EUnmanagedPinPrefix = "bpfman_e2e_unmanaged_"

// findE2EUnmanagedProgramPins returns the paths of top-level pins under
// bpffsRoot whose basename carries E2EUnmanagedPinPrefix. These are
// program pins the e2e scripts create out-of-band; ScanE2EResidue's
// link-pin index skips non-link pins, so they are collected here by
// name instead. A missing bpffsRoot is treated as no residue.
func findE2EUnmanagedProgramPins(bpffsRoot string) ([]string, error) {
	entries, err := os.ReadDir(bpffsRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var pins []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), E2EUnmanagedPinPrefix) {
			pins = append(pins, filepath.Join(bpffsRoot, entry.Name()))
		}
	}
	return pins, nil
}

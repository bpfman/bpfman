package residue

import (
	"fmt"
	"os"

	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/fs/runtime"
)

// ScanWipe returns a Plan that returns bpfman's runtime directory
// to a fresh-box state: unmount the bpffs at the runtime root if
// present, then remove the runtime root wholesale. The next
// `bpfman` invocation recreates the lock file, the runtime
// subdirectories, and re-mounts bpffs from scratch -- which is
// exactly the first-touch path the lock-on-startup code now
// relies on.
//
// Unlike ScanE2EResidue and PlanFromObservation, ScanWipe does
// not consult the store. It is the escape hatch for when the
// store and the bpf fs have drifted out of sync to a state where
// the normal cleanup flows cannot reconcile them.
func ScanWipe(layout fs.Layout) (Plan, error) {
	var plan Plan

	bpffsRoot := layout.BPFFS().MountPoint()
	if mounted, err := runtime.IsBpffsMounted(runtime.DefaultMountInfoPath, bpffsRoot); err != nil {
		return nil, fmt.Errorf("check bpffs mount %s: %w", bpffsRoot, err)
	} else if mounted {
		plan = append(plan, UnmountBPFFS{Path: bpffsRoot})
	}

	base := layout.Base()
	if _, err := os.Stat(base); err == nil {
		plan = append(plan, RemoveTree{Path: base})
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat %s: %w", base, err)
	}

	return plan, nil
}

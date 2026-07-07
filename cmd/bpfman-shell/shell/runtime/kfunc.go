package runtime

import (
	"encoding/json"
	"strconv"
	"sync"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

// Kfunc is the user-visible handle for one leased kernel-function slot
// exported by the bpfman_e2e_targets module. The handle is produced
// by `kfunc acquire` and consumed by `kfunc release`; `kfunc fire`
// writes the trigger file to invoke the function.
type Kfunc struct {
	// Index is the leased slot number within the bpfman_e2e_targets
	// module's pool of kernel-function targets.
	Index uint32

	// Name is the kernel-function symbol exposed by the leased slot,
	// the attach target for fentry/fexit and kprobe/kretprobe
	// programs.
	Name string

	// Trigger is the path to the debugfs file that `kfunc fire`
	// writes to invoke the function.
	Trigger string

	// Count is the path to the debugfs file reporting how many times
	// the function has fired.
	Count string

	// Mu guards Released.
	Mu sync.Mutex

	// Released is the lifecycle latch; `kfunc release` sets it and
	// returns the slot to the cross-process pool.
	Released bool
}

// MarkReleased sets the lifecycle latch and reports whether this
// call observed an already-released handle.
func (f *Kfunc) MarkReleased() (wasReleased bool) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.Released {
		return true
	}
	f.Released = true
	return false
}

// IsReleased reports whether the handle has been consumed.
func (f *Kfunc) IsReleased() bool {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	return f.Released
}

// ValueFromKfunc wraps f as a Value with semantics.OriginKfunc.
func ValueFromKfunc(f *Kfunc) Value {
	mirror := map[string]any{
		"index":   json.Number(strconv.FormatUint(uint64(f.Index), 10)),
		"name":    f.Name,
		"trigger": f.Trigger,
		"count":   f.Count,
	}
	return Value{v: mirror, origin: f, kind: semantics.OriginKfunc}
}

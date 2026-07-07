package runtime

import (
	"sync"
	"time"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

// Job is the user-visible handle for a background process started
// by 'start COMMAND ARGS'. The PID is set at start time; the
// captured streams, exit code, and lifecycle flags settle when
// the underlying process exits or is killed. The driver owns the
// actual *exec.Cmd and the goroutine that copies stdout/stderr
// into Stdout/Stderr; the shell layer only sees the data the
// script can read or write to.
//
// A Job is not an immutable Value: it is an execution capability
// whose internal state evolves over the job's lifetime. Wrap it
// with ValueFromJob and the script can read $job.pid via the
// standard path-walker; the remaining state (Stdout, Stderr,
// ExitCode, Killed) flows back to the script through 'wait',
// which constructs a captured-result Envelope from the job's
// settled fields.
//
// Concurrency: Done acts as the synchronisation barrier. The
// driver writes Stdout/Stderr/ExitCode/Killed before closing Done;
// readers (wait) are guaranteed to see the final values once Done
// is closed. Mu protects the lifecycle flags (Managed, Killed)
// against concurrent updates from 'wait' and 'kill' running on
// the same handle.
type Job struct {
	// PID is the process group leader's PID, suitable for
	// /proc/<pid>/... lookups, bpftool PID filters, and any
	// other tool that takes a PID. Set once at start; never
	// mutated.
	PID int

	// Done is closed when the underlying process has exited and
	// the captured streams are final. wait blocks on it.
	Done chan struct{}

	// Mu guards Managed and Killed. Stdout/Stderr/ExitCode are
	// written before Done is closed and read after, so they need
	// no separate lock.
	Mu sync.Mutex

	// Stdout holds the captured standard output. Settled before
	// Done is closed.
	Stdout string

	// Stderr holds the captured standard error. Settled before Done
	// is closed.
	Stderr string

	// ExitCode is the process exit status. Settled before Done
	// is closed. -1 indicates the job was killed before it set
	// an exit status of its own.
	ExitCode int

	// Killed is true if 'kill' has been invoked against this
	// job. Distinguishes "user terminated" from "process exited
	// on its own".
	Killed bool

	// Signal is the short name of the signal that ended the
	// process (e.g. "TERM", "USR1", "KILL"), or empty when the
	// process exited normally. The kill builtin sets it from
	// its own --signal flag; the reaper goroutine sets it for
	// processes signalled by anything else (an external kill,
	// a parent SIGTERM during shutdown).
	Signal string

	// Managed is true once wait or kill has run against the job.
	// An unmanaged job at scope exit is a script failure: the
	// lifecycle leaked.
	Managed bool

	// Args is the resolved argv used to spawn the process. The
	// renderer reads it when reporting an unmanaged-job failure
	// or a non-ok wait result so the user can locate the
	// offending start in the script.
	Args []string

	// Origin is a human-readable source citation captured at
	// start time ('file:line' in scripts, empty for stdin-driven
	// runs). The scope-exit leak diagnostic prepends
	// it so the user can locate the offending start, even when
	// the leak is detected in a different part of the script
	// from where the job was launched.
	Origin string

	// Started is the wall-clock instant at which the spawn
	// returned successfully. Set once at construction; never
	// mutated. Renderers (the 'jobs' listing) format this for
	// display and consumers that care about correlation with
	// other logs read it directly. Wall clock rather than
	// monotonic because the primary use is "what time did
	// this start?", not "how long has this been running?".
	Started time.Time

	// TargetBinary is populated when the launched job corresponds
	// to a stable executable image that kernel attachment APIs
	// may target (uprobes, symbol resolution, etc.). It is not
	// intended as a general process-inspection surface.
	//
	// fire kinds with NeedsBinary == true populate this with the
	// running bpfman-shell ELF (/proc/self/exe) so the script can
	// pass it to `bpfman link attach uprobe <program-id> ...`. Plain
	// start populates it with argv[0] as best-effort identity; the
	// semantic guarantee belongs only to fire kinds.
	//
	// Empty when the producer did not publish a target binary.
	// Path-walking the absent field is a runtime error, not a
	// silent empty string, so a typo cannot flow into a downstream
	// empty target operands undetected.
	TargetBinary string
}

// MarkManaged records that the script has acknowledged this
// job's lifecycle (via wait or kill). Used by the scope-exit
// check to distinguish leaked jobs from properly handled ones.
func (j *Job) MarkManaged() {
	j.Mu.Lock()
	j.Managed = true
	j.Mu.Unlock()
}

// IsManaged reports whether the script has handled this job.
func (j *Job) IsManaged() bool {
	j.Mu.Lock()
	defer j.Mu.Unlock()
	return j.Managed
}

// ValueFromJob wraps j as a Value with semantics.OriginJob. The path
// machinery resolves $job.pid through the JSON-tree mirror; the
// underlying *Job is recoverable via Value.Origin() so the wait
// and kill builtins reach the channels and lifecycle state
// directly.
func ValueFromJob(j *Job) Value {
	mirror := map[string]any{
		"pid": numFromInt(j.PID),
	}
	if j.TargetBinary != "" {
		mirror["target_binary"] = j.TargetBinary
	}
	return Value{v: mirror, origin: j, kind: semantics.OriginJob}
}

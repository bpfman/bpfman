// Job-leak policy for scripts and 'bpfman-shell FILE' contracts
// that treat a leaked job as a failure.

package driver

import (
	"strings"
	"syscall"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/internal/cli"
)

// StrictJobLeakHandler is the script-mode policy: an unmanaged
// job is a contract violation. Renders '[job] FAIL <origin>:
// never waited or killed: argv', SIGKILLs the process group so
// 'bpfman-shell script.bpfman' never leaves stray processes,
// and bumps the session leak counter so the caller surfaces a
// non-zero exit. Scripts are a reproducible test contract;
// leaking a job is a bug worth failing the run for.
//
// ESRCH (the process exited on its own between leak detection
// and signal delivery) is silently fine; permission errors
// fall through and would print, but in practice we sent the
// job's own SIGTERM-able signal earlier or could not have
// spawned it in the first place.
func StrictJobLeakHandler(cli *cli.CLI, session *runtime.Session) func(*runtime.Job) {
	return func(j *runtime.Job) {
		origin := j.Origin
		if origin == "" {
			origin = "<stdin>"
		}
		argv := strings.Join(j.Args, " ")
		_ = cli.PrintErrf("[job] FAIL %s: never waited or killed: %s\n", origin, argv)
		if j.PID > 0 {
			_ = syscall.Kill(-j.PID, syscall.SIGKILL)
		}
		session.RecordJobLeak()
	}
}

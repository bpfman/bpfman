// Package execcancel wires cooperative cancellation onto an
// exec.Cmd built with exec.CommandContext. On context cancellation
// the command's process group is sent SIGINT -- the shell interrupt
// -- and os/exec's WaitDelay bounds how long Run/Wait may spend on
// a command that does not exit or whose output pipes remain open.
//
// The child is placed in its own process group so a single signal
// to -pid reaches the child and any descendants it spawned. This
// inherits the usual Unix process-group precision caveat: a child
// that starts its own session or re-parents can escape the group.
// For the lifecycle scripts this drives, group signalling is the
// pragmatic choice.
package execcancel

import (
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
)

// Grace bounds how long a signalled process group may take to exit
// before os/exec applies its fallback handling. The cooperative
// SIGINT is group-wide; os/exec's forced process kill, if needed,
// targets the direct child process.
const Grace = 2 * time.Second

// ensureProcessGroup requests that cmd's child lead its own process
// group, so a signal to -pid reaches the child and its descendants.
// Must be called before Start.
func ensureProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// Configure makes cmd cancel cooperatively: it places the child in
// its own process group, overrides cmd.Cancel to SIGINT that group,
// and sets cmd.WaitDelay to Grace. cmd must be built with
// exec.CommandContext for Cancel to fire. The returned flag reads
// true once cancellation has actually signalled the group, so the
// caller can substitute context.Cause(ctx) for the synthetic wait
// error.
func Configure(cmd *exec.Cmd) *atomic.Bool {
	ensureProcessGroup(cmd)
	var cancelled atomic.Bool
	cmd.Cancel = func() error {
		err := signalGroup(cmd.Process.Pid, syscall.SIGINT)
		if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
			return os.ErrProcessDone
		}
		if err == nil {
			cancelled.Store(true)
		}
		return err
	}
	cmd.WaitDelay = Grace
	return &cancelled
}

func signalGroup(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	return syscall.Kill(-pid, sig)
}

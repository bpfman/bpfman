// Foreground job control for bpfman-shell. When the shell is
// running attached to a TTY and launches an external program
// with stdio inherited (vi, less, top, ssh, gdb), we want the
// child to be the one the user's keystrokes target: ^C should
// reach only the child, not the shell, and the shell should
// reclaim the terminal cleanly when the child exits. Without
// this dance, the shell and the child share a foreground
// process group: a ^C the user meant for the child also tears
// the shell down with it.
//
// The Unix recipe is well-trodden. Block SIGTTOU at startup so
// the parent can manipulate the controlling-terminal foreground
// group without being suspended. Launch the child with
// Setpgid so the kernel atomically places it in its own
// process group as part of fork. tcsetpgrp the TTY's foreground
// group to the child's PID. Wait. tcsetpgrp the TTY back to
// the shell's group. The TTY-sensitive part is conditional on
// stdin actually being a TTY: under stdin pipes, scripts, and
// CI we have nothing to manage and the dance is a no-op.

package driver

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// FgJob holds the state needed to give and reclaim the
// terminal's foreground group around a single child invocation.
// The disabled value (ttyFD == -1) means "no TTY in play"; every
// method short-circuits and the call sites do not need to
// special-case the non-TTY path.
type FgJob struct {
	ttyFD     int // -1 if not running on a TTY
	shellPgid int // shell's process group, restored after the child exits
}

// Enabled reports whether foreground TTY job control is active.
func (f FgJob) Enabled() bool {
	return f.ttyFD >= 0
}

// NewFgJob inspects stdin. If it is a TTY, returns an FgJob
// ready to grant the terminal to a child via Grant. Otherwise
// returns the disabled value: every FgJob method becomes
// a no-op so the caller writes the same code path for both
// the TTY and the non-TTY case.
func NewFgJob() FgJob {
	fd := int(os.Stdin.Fd())
	if !TermIsTTY(fd) {
		return FgJob{ttyFD: -1}
	}
	pgid, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	if err != nil {
		return FgJob{ttyFD: -1}
	}

	return FgJob{ttyFD: fd, shellPgid: pgid}
}

// SysProcAttr returns the SysProcAttr needed to launch a child
// in its own process group when the shell is running on a TTY.
// Setpgid is set so the kernel places the child in pgid=child
// atomically with fork; without it there is a race between
// fork return and our subsequent tcsetpgrp. Off-TTY callers
// receive nil so default exec.Cmd behaviour applies.
func (f FgJob) SysProcAttr() *syscall.SysProcAttr {
	if f.ttyFD < 0 {
		return nil
	}
	return &syscall.SysProcAttr{Setpgid: true}
}

// Grant gives the terminal's foreground group to childPid,
// which the kernel guarantees is also the child's pgid because
// of the Setpgid request above. After this call the child
// owns the TTY: typed characters reach the child, and ^C
// generates a SIGINT delivered only to the child's process
// group (not to the shell). SIGTTOU is masked at process
// startup so this call from the shell (now a background
// process relative to the TTY) is not suspended. A nil error
// means the child is now the foreground group; any error
// leaves the foreground unchanged and the caller treats the
// child as not-job-controlled.
func (f FgJob) Grant(childPid int) error {
	if f.ttyFD < 0 {
		return nil
	}
	return unix.IoctlSetPointerInt(f.ttyFD, unix.TIOCSPGRP, childPid)
}

// Reclaim restores the shell's process group as the
// terminal's foreground group. Called after the child has
// exited and been waited on, so the previous foreground
// group (the child's) is gone and the kernel will accept the
// switch. Errors are returned for logging but the shell
// continues; even if the ioctl fails cooked-mode line
// discipline tends to forgive a stale foreground group when
// the only candidate is the shell that drains stdin.
func (f FgJob) Reclaim() error {
	if f.ttyFD < 0 {
		return nil
	}
	return unix.IoctlSetPointerInt(f.ttyFD, unix.TIOCSPGRP, f.shellPgid)
}

// TermIsTTY reports whether fd refers to a terminal. We do
// our own check rather than pulling in golang.org/x/term to
// avoid a circular import via the line-reader; the x/sys
// ioctl is what x/term uses internally anyway.
func TermIsTTY(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	return err == nil
}

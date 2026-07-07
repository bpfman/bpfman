// Process-startup signal ignores for foreground job control.
// SIGTTOU and SIGTTIN are sent to a background process that
// attempts to write to or read from the controlling terminal.
// While the shell hands the terminal off to a child via
// tcsetpgrp (see FgJob.Grant), the shell itself is briefly a
// background process relative to the TTY: TIOCSPGRP from a
// background process is exactly the kind of operation the
// kernel suspends with SIGTTOU. Ignoring SIGTTOU at startup
// turns that suspension into a successful ioctl and lets the
// shell drive the foreground-group dance. SIGTTIN is ignored
// for symmetry; cooked-mode line discipline can deliver it in
// background-read scenarios that we do not drive but which
// would otherwise stop the shell.
//
// The default Go signal set includes SIGTTOU under signal.Ignore
// for any process that uses the runtime's default behaviour, but
// bpfman-shell installs explicit signal notification at process
// startup. Ignore these stop signals explicitly so job control
// remains stable regardless of the process signal wiring.

package driver

import (
	"os/signal"
	"syscall"
)

func init() {
	signal.Ignore(syscall.SIGTTOU, syscall.SIGTTIN)
}

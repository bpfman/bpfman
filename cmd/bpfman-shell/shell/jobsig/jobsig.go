// Package jobsig owns the set of signals the shell's job-control
// builtins understand, mapping between the bare 'TERM' / 'USR1'
// spelling a script writes and the syscall.Signal the runtime
// delivers. The static checker (which only needs to know whether a
// name is valid) and the runtime kill builtin (which needs the
// signal value and the inverse spelling for the result envelope)
// both derive from the one table here, so the accepted set cannot
// drift between preflight and execution.
package jobsig

import (
	"strconv"
	"strings"
	"syscall"
)

// table is the single source of truth: every accepted signal in the
// bare spelling paired with the syscall.Signal it stands for. Both
// directions and the known-name test below read from this list.
var table = []struct {
	name string
	sig  syscall.Signal
}{
	{"TERM", syscall.SIGTERM},
	{"KILL", syscall.SIGKILL},
	{"INT", syscall.SIGINT},
	{"QUIT", syscall.SIGQUIT},
	{"HUP", syscall.SIGHUP},
	{"USR1", syscall.SIGUSR1},
	{"USR2", syscall.SIGUSR2},
	{"STOP", syscall.SIGSTOP},
	{"CONT", syscall.SIGCONT},
}

// normalise folds a user-written signal name to the bare spelling
// used as the table key: case-insensitive, surrounding space
// trimmed, optional 'SIG' prefix dropped. 'sigusr1', ' USR1 ' and
// 'SIGUSR1' all normalise to 'USR1'.
func normalise(name string) string {
	upper := strings.ToUpper(strings.TrimSpace(name))
	return strings.TrimPrefix(upper, "SIG")
}

// FromName maps a signal name to its syscall.Signal. Both the
// 'SIGNAME' and 'NAME' spellings are accepted. The bool is false
// for an unrecognised name; the caller decides how to report it.
func FromName(name string) (syscall.Signal, bool) {
	n := normalise(name)
	for _, e := range table {
		if e.name == n {
			return e.sig, true
		}
	}
	return 0, false
}

// ShortName is the inverse of FromName: it maps a syscall.Signal to
// the bare spelling so a result envelope reads naturally regardless
// of which signal the process ended on. An unrecognised signal
// falls back to its numeric form.
func ShortName(sig syscall.Signal) string {
	for _, e := range table {
		if e.sig == sig {
			return e.name
		}
	}
	return strconv.Itoa(int(sig))
}

// KnownName reports whether name matches an accepted signal, using
// the same normalisation as FromName. The static checker uses this
// to flag a bad 'kill --signal=...' before the runtime would.
func KnownName(name string) bool {
	_, ok := FromName(name)
	return ok
}

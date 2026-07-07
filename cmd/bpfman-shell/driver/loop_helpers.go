// Small driver-side helpers: the two halt-the-script sentinels
// and the captured-result failure renderer. None of them are
// big or feature-aware.

package driver

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/internal/cli"
)

// ErrScriptError is the sentinel the driver returns when a
// script emitted a runtime error that has already been printed
// with a file:line: prefix. The embedding binary translates it
// into a silent non-zero exit so Kong does not print a second
// error message.
var ErrScriptError = errors.New("script error")

// makeDeferOutputFlusher returns a runtime.Env.RenderDeferOutput
// hook that writes the captured stdout/stderr of a successful
// defer through to the driver's terminal. Defers go through
// ExecBind, which captures the deferred command's output into
// the result envelope; without this flush the captured bytes
// would be dropped and `defer print "trace"` would produce no
// visible output. Failure-path defers still render through
// RenderDeferFailure's labelled block (which already shows the
// captured streams), so this hook only fires on success.
//
// Output is forwarded verbatim. Builtins like print append a
// trailing newline themselves, so this code adds no extra
// formatting; subprocess output that lacks a trailing newline
// will appear without one, matching how the same command would
// look when run directly on stdout/stderr.
func makeDeferOutputFlusher(cli *cli.CLI) func(args []runtime.Arg, rc runtime.Envelope) {
	return func(_ []runtime.Arg, rc runtime.Envelope) {
		if rc.Stdout != "" {
			_ = cli.PrintOut(rc.Stdout)
		}
		if rc.Stderr != "" {
			_ = cli.PrintErr(rc.Stderr)
		}
	}
}

// RenderEnvelopeFailure prints a captured-result failure as a
// labelled block: the verb header (guard, require, assert,
// defer), the source position of the failing statement, the
// resolved command line, the exit code, and any captured stdout
// and stderr. Empty stdout/stderr emit just the label;
// multi-line text is indented two spaces per line.
//
// Positions on statements are expected to carry their own source
// file; fallbackFile is only for fileless or synthetic spans.
func RenderEnvelopeFailure(cli *cli.CLI, verb string, fallbackFile string, stmtLoc source.Pos, args []runtime.Arg, env runtime.Envelope) {
	file := stmtLoc.File
	if file == "" {
		file = fallbackFile
	}
	if file == "" {
		file = "<stdin>"
	}
	cite := SourceLoc{File: file, Line: stmtLoc.Line, Col: stmtLoc.Col}.Cite()
	if cite == "" {
		cite = file
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] FAIL at %s\n", verb, cite)
	b.WriteString("command:\n")
	if argv := ArgTexts(args); len(argv) > 0 {
		fmt.Fprintf(&b, "  %s\n", strings.Join(argv, " "))
	}
	fmt.Fprintf(&b, "exit:\n  %d\n", env.ExitCode)
	b.WriteString("stdout:\n")
	writeIndented(&b, env.Stdout)
	b.WriteString("stderr:\n")
	writeIndented(&b, env.Stderr)
	_ = cli.PrintErrf("%s", b.String())
}

// writeIndented appends s to b with each line prefixed by two
// spaces. A trailing newline on s is dropped before splitting so
// a captured stdout that already ended in '\n' does not produce
// a blank indented line at the end.
func writeIndented(b *strings.Builder, s string) {
	if s == "" {
		return
	}
	for line := range strings.SplitSeq(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

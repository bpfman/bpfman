// External-process primitives: spawn a command and capture its
// output, or hand stdio to the parent so tty-bound programs
// work. Two paths share a single argv-resolution helper that
// flattens runtime.Arg values (including file:$var adapters) into
// argv strings.
//
// These are pure mechanism: no knowledge of the builtin registry,
// no knowledge of bpfman. The `exec` builtin and the loop's
// fall-through to external commands both call into them.

package driver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/internal/cli"
	"github.com/bpfman/bpfman/internal/execcancel"
)

// ExecFailure is the typed error returned by RunExecStatement when
// an external subprocess at top-level statement position exits
// non-zero. It deliberately is not a *runtime.SyntaxError: nothing in
// the source is malformed, the child simply reported a non-zero exit.
// SourceSpan satisfies syntax's internal span-carrying error contract
// so frameAtSpan leaves these
// values untouched; the renderer routes them to a citation shape
// (file:line: argv: exit N) instead of the rust-compiler frame
// reserved for parser/checker diagnostics.
type ExecFailure struct {
	// Argv is the resolved command and arguments as spawned.
	Argv []string

	// ExitCode is the non-zero status the subprocess reported.
	ExitCode int

	// Span is the source extent of the statement that ran the
	// subprocess, used to cite the failing line.
	Span source.Span

	// Stdout holds the subprocess's standard output when the failing
	// path captured it; empty when stdio was inherited.
	Stdout string

	// Stderr holds the subprocess's standard error when the failing
	// path captured it; empty when stdio was inherited.
	Stderr string
}

// Error returns the argv joined by spaces followed by the non-zero
// exit status.
func (e *ExecFailure) Error() string {
	return fmt.Sprintf("%s: exit status %d", strings.Join(e.Argv, " "), e.ExitCode)
}

// SourceSpan implements the internal span-carrying error contract.
func (e *ExecFailure) SourceSpan() source.Span { return e.Span }

// CommandNotFound is the typed error returned at the subprocess
// fallthrough when the first word resolves to no executable on
// $PATH. Detected before argument resolution so a script that
// names a non-existent command reports the missing-command failure
// first, rather than a downstream argument-flatten error caused by
// the later arguments. The span-carrying error contract keeps it out of the syntax-error
// frame: the source is well-formed, the name just does not resolve.
type CommandNotFound struct {
	// Name is the first word that resolved to no executable on
	// $PATH.
	Name string

	// Span is the source extent of the command, used to cite the
	// failing line.
	Span source.Span
}

// Error reports that the named command was not found.
func (e *CommandNotFound) Error() string {
	return fmt.Sprintf("%s: command not found", e.Name)
}

// SourceSpan implements the internal span-carrying error contract.
func (e *CommandNotFound) SourceSpan() source.Span { return e.Span }

// ExecArgError is the typed error returned by ResolveExternalArgs
// when an argument cannot be flattened into argv text -- a
// structured value passed where the spawned process expects a
// scalar, or an unrecognised adapter form. The source construct is
// well-formed; the runtime value just does not compose with what
// the executor needs.
type ExecArgError struct {
	// Msg describes why an argument could not be flattened into
	// argv text.
	Msg string

	// Span is the source extent of the offending argument.
	Span source.Span
}

// Error returns the argument-flatten failure message.
func (e *ExecArgError) Error() string { return e.Msg }

// SourceSpan implements the internal span-carrying error contract.
func (e *ExecArgError) SourceSpan() source.Span { return e.Span }

// RuntimeError is the typed error a handler returns when the
// failure is a runtime outcome on a syntactically well-formed
// construct, not a malformed region of source. The in-process
// bpfman dispatcher wraps its returned errors in this type;
// individual shell builtins can opt in when their failure is
// genuinely runtime-outcome rather than usage-error.
type RuntimeError struct {
	// Msg is the runtime failure description.
	Msg string

	// Span is the source extent of the construct that failed.
	Span source.Span
}

// Error returns the runtime failure message.
func (e *RuntimeError) Error() string { return e.Msg }

// SourceSpan implements the internal span-carrying error contract.
func (e *RuntimeError) SourceSpan() source.Span { return e.Span }

// ResolveCommandPath returns nil if name names an executable
// reachable from the current process: an absolute path, a relative
// path containing a slash, or a bare name that exec.LookPath finds
// on $PATH. Otherwise it returns a *CommandNotFound carrying the
// originating Span so the renderer can cite the source line. No
// caching: exec.LookPath rescans $PATH on every call.
func ResolveCommandPath(name string, span source.Span) error {
	if _, err := exec.LookPath(name); err != nil {
		return &CommandNotFound{Name: name, Span: span}
	}
	return nil
}

// RunExecStatement is the shared exec-as-statement implementation
// used by the `exec` builtin handler and by the loop's fallthrough
// for unknown first words. span identifies the originating statement
// so failures cite the right source location without a
// syntax-error frame.
func RunExecStatement(ctx context.Context, cli *cli.CLI, args []runtime.Arg, span source.Span) (runtime.Value, error) {
	argv, exitCode, err := RunExternalInherit(ctx, cli, args)
	if err != nil {
		return runtime.Value{}, err
	}

	if exitCode != 0 {
		return runtime.Value{}, &ExecFailure{
			Argv:     argv,
			ExitCode: exitCode,
			Span:     span,
		}
	}

	return runtime.Value{}, nil
}

// ExecCapture is the result of running an external command
// without any policy applied: argv as constructed, captured stdout
// and stderr, and the actual exit code. Launch failure is reported
// as a Go error from RunExternal and never appears as an
// ExecCapture.
type ExecCapture struct {
	// Argv is the command and arguments as constructed and run.
	Argv []string

	// Stdout is the captured standard output.
	Stdout string

	// Stderr is the captured standard error.
	Stderr string

	// ExitCode is the process exit status; 0 on success.
	ExitCode int
}

// ResolveExternalArgs walks args, resolving file: adapter values
// to temp files and rejecting structured-value args that cannot
// flatten into argv text. Returned tempFiles are the caller's to
// remove (typically via defer); they outlive the resolve call so
// the spawned process can read them. Shared between RunExternal
// and RunExternalInherit.
func ResolveExternalArgs(args []runtime.Arg) (argv []string, tempFiles []string, err error) {
	resolved := make([]runtime.Arg, len(args))
	for i, a := range args {
		switch aa := a.(type) {
		case runtime.AdapterArg:
			if aa.Adapter != "file" {
				for _, f := range tempFiles {
					os.Remove(f)
				}
				return nil, nil, &ExecArgError{
					Msg:  fmt.Sprintf("argument %d: unknown adapter %q", i+1, aa.Adapter),
					Span: aa.Span,
				}
			}
			path, terr := WriteValueToTemp(aa.Value)
			if terr != nil {
				for _, f := range tempFiles {
					os.Remove(f)
				}
				return nil, nil, &ExecArgError{
					Msg:  fmt.Sprintf("argument %d: file adapter: %v", i+1, terr),
					Span: aa.Span,
				}
			}
			tempFiles = append(tempFiles, path)
			resolved[i] = runtime.ScalarValueArg{Text: path, Span: aa.Span}
		case runtime.StructuredValueArg:
			for _, f := range tempFiles {
				os.Remove(f)
			}
			return nil, nil, &ExecArgError{
				Msg: fmt.Sprintf(
					"argument %d: cannot pass a %s value to an external command; use a scalar field (e.g. $name.field) or the file adapter (file:$name)",
					i+1, aa.Value.Kind()),
				Span: aa.Span,
			}
		default:
			resolved[i] = a
		}
	}
	return ArgTexts(resolved), tempFiles, nil
}

// RunExternal runs an external command and captures its output.
// Inline adapter arguments (e.g. file:$var.path) are resolved to
// temporary files before the command runs and removed
// unconditionally after. Structured-value arguments are rejected
// because they cannot be flattened into argv text. Non-zero exit
// is reported via ExecCapture.ExitCode, not as an error: callers
// decide whether non-zero is fatal.
func RunExternal(ctx context.Context, args []runtime.Arg) (ExecCapture, error) {
	if len(args) == 0 {
		return ExecCapture{}, fmt.Errorf("exec requires at least one argument")
	}
	argv, tempFiles, err := ResolveExternalArgs(args)
	if err != nil {
		return ExecCapture{}, err
	}

	defer func() {
		for _, f := range tempFiles {
			os.Remove(f)
		}
	}()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cancelled := execcancel.Configure(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cap := ExecCapture{Argv: argv}
	err = cmd.Run()
	cap.Stdout = stdout.String()
	cap.Stderr = stderr.String()
	if cancelled.Load() {
		return cap, context.Cause(ctx)
	}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return ExecCapture{}, fmt.Errorf("exec %s: %w", argv[0], err)
		}
		cap.ExitCode = exitErr.ExitCode()
	}
	return cap, nil
}

// RunExternalInherit runs an external command with stdio
// connected to the parent: stdin from os.Stdin, stdout/stderr to
// the cli's writers, and (when stdin is a TTY) full foreground
// job control so the child owns the terminal for the duration of
// the call. Interactive programs (vi, less, htop) get a real TTY.
//
// Cancellation is split by whether we are on a TTY. On a TTY the
// child owns the foreground group, so the terminal delivers a ^C
// straight to it; ctx is left out of the spawn so that cancelling
// the shell's root ctx does not tear down a foreground program the
// user is still driving. Off a TTY (script mode, stdin pipe, CI)
// there is no foreground group to route signals, so the child is
// spawned through exec.CommandContext with cooperative
// cancellation: ctx cancellation SIGINTs the child's process group
// and WaitDelay escalates to SIGKILL after a grace period.
func RunExternalInherit(ctx context.Context, cli *cli.CLI, args []runtime.Arg) (argv []string, exitCode int, err error) {
	if len(args) == 0 {
		return nil, 0, fmt.Errorf("exec requires at least one argument")
	}
	argv, tempFiles, err := ResolveExternalArgs(args)
	if err != nil {
		return nil, 0, err
	}

	defer func() {
		for _, f := range tempFiles {
			os.Remove(f)
		}
	}()

	fg := NewFgJob()
	var cmd *exec.Cmd
	cancelled := &atomic.Bool{}
	if fg.Enabled() {
		cmd = exec.Command(argv[0], argv[1:]...)
		cmd.SysProcAttr = fg.SysProcAttr()
	} else {
		cmd = exec.CommandContext(ctx, argv[0], argv[1:]...)
		cancelled = execcancel.Configure(cmd)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = cli.Out
	cmd.Stderr = cli.Err

	if rerr := cmd.Start(); rerr != nil {
		return argv, 0, fmt.Errorf("exec %s: %w", argv[0], rerr)
	}

	// Hand the terminal to the child. SIGTTOU is masked at process
	// startup so this ioctl from a now-background process does not
	// stop us. A failure here means the child runs without owning
	// the foreground group; the user may see ^C affect the shell
	// rather than the child, but the run still completes.
	_ = fg.Grant(cmd.Process.Pid)
	defer func() { _ = fg.Reclaim() }()

	rerr := cmd.Wait()
	if rerr != nil {
		var exitErr *exec.ExitError
		if !errors.As(rerr, &exitErr) {
			return argv, 0, fmt.Errorf("exec %s: %w", argv[0], rerr)
		}
		if cancelled.Load() {
			return argv, exitErr.ExitCode(), context.Cause(ctx)
		}
		return argv, exitErr.ExitCode(), nil
	}
	if cancelled.Load() {
		return argv, 0, context.Cause(ctx)
	}
	return argv, 0, nil
}

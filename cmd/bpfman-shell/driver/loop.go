// Whole-program script runner and the Env-callback factories
// that bridge the shell evaluator to the runtime. Pure
// mechanism: the embedding binary plugs in its handlers via
// RegisterBuiltin and its bpfman bridge via Config.Fallback /
// Config.BindFallback.

package driver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/lower"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
	"github.com/bpfman/bpfman/cmd/internal/cli"
)

// RunHooks bundles the Env callbacks the runner installs when
// it evaluates a parsed program.
// Callers pass the same set the outer Loop wires through
// Config.Fallback / BindFallback / MakeAssert / Now / Sleep; the
// framework has no use for the hooks outside the program-execution
// path, so they are not stored on the context.
type RunHooks struct {
	// Fallback dispatches a statement-position command that no
	// registered builtin matched.
	Fallback FallbackFunc

	// BindFallback dispatches a bind-position ("<-") command that no
	// registered builtin matched.
	BindFallback BindFallbackFunc

	// MakeAssert builds the lowered-assert evaluator wired into
	// Env.ExecAssert; nil disables runtime assert evaluation.
	MakeAssert MakeAssertFunc

	// Now overrides the clock used for poll deadlines; nil means
	// real wall-clock time.
	Now func() time.Time

	// Sleep overrides the poll cadence sleep; nil means real time.
	Sleep func(time.Duration)
}

// Config bundles the call-site options Run needs. The embedding
// binary fills it from its Kong-parsed CLI struct (or equivalent)
// and passes it to Run; the driver package owns everything past
// that.
type Config struct {
	// CLI is the CLI handle used for writers and logger
	// access.
	CLI *cli.CLI

	// LineReader is the input source the runner reads from.
	LineReader LineReader

	// Session is the shell session the runner evaluates against.
	Session *runtime.Session

	// File is the diagnostic name for the input ("script.bpfman",
	// "<stdin>", ""). Loop uses it for source-location prefixes.
	File string

	// NoCheck disables the static pre-flight pass for script
	// mode. Used by tests that exercise runtime behaviour on
	// inputs the static checker would otherwise reject.
	NoCheck bool

	// Fallback is consulted when no registered builtin matches
	// the first token of a dispatched command. Embedders use
	// this to wire in a domain-command bridge (the bpfman
	// dispatcher). Return handled == false to let the loop
	// fall through to external-command execution.
	Fallback FallbackFunc

	// BindFallback is the equivalent of Fallback for the
	// `<- name args` bind path. Embedders use it to special-
	// case wait/net-exec (where the bind's Rc must reflect the
	// captured inner outcome) and to dispatch the bpfman
	// bridge. Return handled == false to let the loop fall
	// through to external-command execution.
	BindFallback BindFallbackFunc

	// MakeAssert builds the lowered-assert evaluator the
	// runtime.Env wires into Env.ExecAssert. The embedding
	// binary owns the actual verb dispatch and reporting policy.
	// nil disables lowered assert evaluation at runtime.
	MakeAssert MakeAssertFunc

	// Now overrides the clock the runtime uses for poll deadlines.
	// nil (the production default) means real wall-clock time; tests
	// inject a fake clock so poll is deterministic regardless of
	// host load.
	Now func() time.Time

	// Sleep overrides the poll cadence sleep. nil (the production
	// default) means real time; tests inject a fake sleep so poll is
	// deterministic regardless of host load.
	Sleep func(time.Duration)
}

// FallbackFunc dispatches unhandled commands (statement
// position). Returning handled == false means "fall through to
// external command".
type FallbackFunc func(ctx context.Context, cli *cli.CLI, args []runtime.Arg, loc SourceLoc, span source.Span) (handled bool, val runtime.Value, err error)

// BindFallbackFunc dispatches unhandled commands on the right
// of `<-`. Returning handled == false means "fall through to
// external command".
type BindFallbackFunc func(ctx context.Context, cli *cli.CLI, session *runtime.Session, env *runtime.Env, args []runtime.Arg, loc SourceLoc, span source.Span) (handled bool, br runtime.BindResult, err error)

// MakeAssertFunc builds the Env.ExecAssert callback for one
// program execution. Returning nil disables lowered assert
// evaluation in that run.
type MakeAssertFunc func(cli *cli.CLI, session *runtime.Session) func(*ir.Assert, *runtime.Env) error

// Run drives one whole-program execution end-to-end and returns
// the session-aggregated outcome: ErrSilent for script-error / require-fail
// paths the caller has already cited, a wrapped error for
// assertion / defer / job-leak counters, or nil on clean exit.
//
// The session counters drive the post-loop summary; assertion
// failures surface a non-zero exit even when the program ran
// without aborting.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Session == nil {
		cfg.Session = runtime.NewSession()
	}

	loopErr := Loop(ctx, cfg)

	if isContextCancellation(ctx, loopErr) {
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		return loopErr
	}
	if errors.Is(loopErr, runtime.ErrRequireFailed) || errors.Is(loopErr, ErrScriptError) {
		return ErrSilent
	}
	if loopErr != nil {
		return loopErr
	}

	if n := cfg.Session.AssertFailures(); n > 0 {
		_ = cfg.CLI.PrintErrf("%d assertion(s) failed\n", n)
		return fmt.Errorf("%d assertion(s) failed", n)
	}

	if n := cfg.Session.DeferFailures(); n > 0 {
		_ = cfg.CLI.PrintErrf("%d defer(s) failed\n", n)
		return fmt.Errorf("%d defer(s) failed", n)
	}

	if n := cfg.Session.JobLeaks(); n > 0 {
		_ = cfg.CLI.PrintErrf("%d job(s) leaked\n", n)
		return fmt.Errorf("%d job(s) leaked", n)
	}

	return nil
}

// Loop reads one whole program from cfg.LineReader and executes
// it inside one outer job scope and one outer defer scope.
// `defer cleanup` therefore fires at script end and any
// unmanaged job is reported as `[job] FAIL ...`, killed, and
// counted towards the final non-zero exit.
func Loop(ctx context.Context, cfg Config) error {
	return scriptLoop(ctx, cfg)
}

// deferDrainBudget bounds the cleanup context that defer drains run
// under once the run's root context has been cancelled. It is the
// whole drain's budget, not per-defer: a deferred command that
// blocks eats the remainder and later defers fail fast. A var, not
// a const, so tests can shrink it. Note the e2e script runner's
// execcancel.Grace (2s) SIGKILLs a signalled bpfman-shell before
// this budget expires; typical drains (a handful of unloads) finish
// in well under either bound.
var deferDrainBudget = 5 * time.Second

// dispatchContext returns a per-dispatch context selector and a stop
// function releasing the cleanup context's resources. While the
// run's root context is alive every dispatch uses it. Once the root
// is cancelled (operator interrupt, runner failfast abort, script
// timeout), defer drains get a fresh context detached from the
// cancellation and bounded by deferDrainBudget, so deferred cleanup
// actually executes instead of dying on the dead root. Non-drain
// dispatches keep the cancelled root, so a statement racing the
// unwind still aborts.
func dispatchContext(root context.Context, env *runtime.Env) (ctxFor func() context.Context, stop func()) {
	var mu sync.Mutex
	var cleanup context.Context
	var cancel context.CancelFunc
	ctxFor = func() context.Context {
		if root.Err() == nil || !env.Draining {
			return root
		}
		mu.Lock()
		defer mu.Unlock()
		if cleanup == nil {
			cleanup, cancel = context.WithTimeout(context.WithoutCancel(root), deferDrainBudget)
		}
		return cleanup
	}
	stop = func() {
		mu.Lock()
		defer mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
	return ctxFor, stop
}

// wireEnvForRun installs the per-run Env callbacks the
// evaluator needs: command dispatch, bind dispatch, lowered assert
// dispatch, and the trace hook. The returned stop function releases
// the defer-drain cleanup context and must be called when the run
// ends.
func wireEnvForRun(ctx context.Context, cli *cli.CLI, session *runtime.Session, env *runtime.Env, loc SourceLoc, hooks RunHooks) (stop func()) {
	ctxFor, stop := dispatchContext(ctx, env)
	env.ExecCommand = makeExecCommand(ctxFor, cli, session, env, loc, hooks.Fallback)
	env.ExecBind = makeExecBind(ctxFor, cli, session, env, loc, hooks.BindFallback)
	if hooks.MakeAssert != nil {
		env.ExecAssert = hooks.MakeAssert(cli, session)
	}
	env.Now = hooks.Now
	env.Sleep = hooks.Sleep
	env.Trace = makeTraceHook(cli, session)
	env.RenderPollFailure = makeRenderPollFailure(cli)
	return stop
}

// makeRenderPollFailure builds the callback the shell evaluator
// invokes when a poll runs out of retry budget.
func makeRenderPollFailure(cli *cli.CLI) func(source.Span, time.Duration, time.Duration, int, string) {
	return func(span source.Span, timeout, every time.Duration, attempts int, lastRetry string) {
		loc := SourceLoc{File: span.Pos.File, Line: span.Pos.Line, Col: span.Pos.Col}
		if lastRetry == "" {
			_ = cli.PrintErrf("%s[poll] FAIL: timed out after %s every %s across %d attempt(s)\n", loc, timeout, every, attempts)
			return
		}
		_ = cli.PrintErrf("%s[poll] FAIL: timed out after %s every %s across %d attempt(s): %s\n", loc, timeout, every, attempts, lastRetry)
	}
}

// configHooks extracts the RunHooks set from the loop's
// Config so the same env-wiring helper serves both the CLI and
// the test.
func configHooks(cfg Config) RunHooks {
	return RunHooks{
		Fallback:     cfg.Fallback,
		BindFallback: cfg.BindFallback,
		MakeAssert:   cfg.MakeAssert,
		Now:          cfg.Now,
		Sleep:        cfg.Sleep,
	}
}

// scriptLoop drives one whole-program evaluation in script mode.
// runtime.Exec owns the outer job scope for one program run, and the
// lowered IR owns program/def/poll defer scopes directly.
//
// The caller owns cancellation. The bpfman-shell binary passes a
// signal-aware root context from main; tests and other embedders pass
// their own context.
func scriptLoop(ctx context.Context, cfg Config) error {
	cli := cfg.CLI
	lr := cfg.LineReader
	session := cfg.Session
	file := cfg.File

	src, slurpErr := SlurpReader(lr)
	if slurpErr != nil {
		_ = cli.PrintErrf("%s: %v\n", file, slurpErr)
		return ErrScriptError
	}
	if !cfg.NoCheck {
		if hadIssues := PreflightCheck(cli.Err, file, src); hadIssues {
			return ErrScriptError
		}
	}
	baseDir := sourceBaseDir(file)

	env := &runtime.Env{
		Session: session,
		PrintResult: func(v runtime.Value) error {
			return WriteValue(cli, v)
		},
		RenderDeferFailure: func(stmtLoc source.Pos, args []runtime.Arg, rc runtime.Envelope) {
			RenderEnvelopeFailure(cli, "defer", file, stmtLoc, args, rc)
		},
		RenderDeferOutput: makeDeferOutputFlusher(cli),
		HandleJobLeak:     StrictJobLeakHandler(cli, session),
	}
	hooks := configHooks(cfg)
	loc := SourceLoc{File: file, Line: 1}
	stop := wireEnvForRun(ctx, cli, session, env, loc, hooks)
	defer stop()
	return runProgramSource(ctx, cli, env, src, loc, baseDir)
}

func sourceBaseDir(file string) string {
	if file != "" && file != "-" && file != "<stdin>" {
		return ""
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	return cwd
}

// runProgramSource tokenises, parses, and executes one whole program.
// Typed errors with a Span are rendered as rust-style frames against the
// source text.
func runProgramSource(ctx context.Context, cli *cli.CLI, env *runtime.Env, input string, loc SourceLoc, baseDir string) error {
	emitFrame := func(span source.Span, msg string) {
		_ = cli.PrintErr(renderDiagnostic(input, loc.File, diagnostic{
			Span: span,
			Msg:  msg,
		}))
	}
	cite := func(span source.Span, body string) {
		_ = cli.PrintErrf("%s%s\n", loc.WithSpan(span).String(), body)
	}
	report := func(err error) error {
		if isContextCancellation(ctx, err) {
			return err
		}

		var re *RuntimeError
		if errors.As(err, &re) {
			cite(re.Span, re.Msg)
			return ErrScriptError
		}

		var ae *ExecArgError
		if errors.As(err, &ae) {
			cite(ae.Span, ae.Msg)
			return ErrScriptError
		}

		var cnf *CommandNotFound
		if errors.As(err, &cnf) {
			cite(cnf.Span, cnf.Name+": command not found")
			return ErrScriptError
		}

		var ef *ExecFailure
		if errors.As(err, &ef) {
			if loc.File != "" {
				cite(ef.Span, fmt.Sprintf("%s: exit %d", strings.Join(ef.Argv, " "), ef.ExitCode))
				var b strings.Builder
				if ef.Stdout != "" {
					b.WriteString("stdout:\n")
					writeIndented(&b, ef.Stdout)
				}
				if ef.Stderr != "" {
					b.WriteString("stderr:\n")
					writeIndented(&b, ef.Stderr)
				}
				if b.Len() > 0 {
					_ = cli.PrintErr(b.String())
				}
			}
			return ErrScriptError
		}

		var se *syntax.SyntaxError
		if errors.As(err, &se) && se.Span.Pos.Line > 0 {
			// A *SyntaxError that escaped through callDef has
			// already been decorated with its def's registration
			// File and an absolute Span; the source text we are
			// currently rendering is the caller's, not the def's,
			// so the rust-style frame source-text would be the
			// wrong file. Cite by location alone for cross-file
			// errors; the rust-style frame is reserved for
			// same-file diagnostics where the local source still
			// lines up.
			if se.Span.Pos.File != "" && se.Span.Pos.File != loc.File {
				absLoc := SourceLoc{File: se.Span.Pos.File, Line: se.Span.Pos.Line, Col: se.Span.Pos.Col}
				_ = cli.PrintErrf("%s%s\n", absLoc.String(), se.Msg)
				return ErrScriptError
			}
			emitFrame(se.Span, se.Msg)
			return ErrScriptError
		}

		_ = cli.PrintErrf("%serror: %v\n", loc, err)
		return ErrScriptError
	}
	if strings.TrimSpace(input) == "" {
		return nil
	}
	prog, err := parseAndExpandWithBaseTrace(loc.File, baseDir, input, loc.Line, nil, env.Trace)
	if err != nil {
		return report(err)
	}

	evalErr := execProgram(prog, env)
	if evalErr != nil {
		if errors.Is(evalErr, runtime.ErrRequireFailed) {
			return evalErr
		}
		if errors.Is(evalErr, ErrScriptError) {
			return evalErr
		}
		var gf *runtime.GuardFailure
		if errors.As(evalErr, &gf) {
			stmtLoc := gf.Pos
			var se *syntax.SyntaxError
			if errors.As(evalErr, &se) && se.Span.Pos.Line > 0 {
				stmtLoc = se.Span.Pos
			}
			RenderEnvelopeFailure(cli, "guard", loc.File, stmtLoc, gf.Args, gf.Envelope)
			return ErrScriptError
		}

		return report(evalErr)
	}

	return nil
}

func execProgram(prog *syntax.Program, env *runtime.Env) error {
	lp, err := lower.Lower(prog)
	if err != nil {
		return err
	}

	return runtime.Exec(lp, env)
}

func isContextCancellation(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if ctx.Err() == nil {
		return false
	}
	cause := context.Cause(ctx)
	return cause != nil && errors.Is(err, cause)
}

// Dispatch looks the first token of args up in the builtin
// registry and invokes its handler. Returns
// (true, value, err) when the registry has an entry; the value
// is the assignable primary for builtins that produce one,
// runtime.Value{} for builtins that bind nothing. Returns
// (false, runtime.Value{}, nil) when no builtin matches.
func Dispatch(ctx context.Context, cli *cli.CLI, session *runtime.Session, env *runtime.Env, args []runtime.Arg, loc SourceLoc, span source.Span) (bool, runtime.Value, error) {
	if len(args) == 0 {
		return false, runtime.Value{}, nil
	}
	cmd := ArgText(args[0])
	b, ok := LookupBuiltin(cmd)
	if !ok {
		return false, runtime.Value{}, nil
	}

	callLoc := dispatchSourceLoc(loc, env, span)
	c := Ctx{
		Ctx:  ctx,
		CLI:  cli,
		Env:  env,
		Cmd:  cmd,
		Args: args[1:],
		Pos:  callLoc,
		Span: span,
	}
	val, err := b.Handler(c)
	return true, val, syntax.FrameAt(span, err)
}

// makeExecCommand bridges the evaluator's top-level CommandStmt
// dispatch into the loop pipeline. Output is visible on the CLI.
// Dispatch order: registered builtins handle their own names;
// the embedder's Fallback handles domain commands; an
// unrecognised first word runs as an external subprocess.
func makeExecCommand(ctxFor func() context.Context, cli *cli.CLI, session *runtime.Session, env *runtime.Env, loc SourceLoc, fallback FallbackFunc) func([]runtime.Arg, source.Span) (runtime.Value, error) {
	return func(args []runtime.Arg, span source.Span) (runtime.Value, error) {
		if len(args) == 0 {
			return runtime.Value{}, nil
		}
		ctx := ctxFor()
		callLoc := dispatchSourceLoc(loc, env, span)
		handled, val, err := Dispatch(ctx, cli, session, env, args, loc, span)
		if err != nil {
			return runtime.Value{}, err
		}

		if handled {
			return val, nil
		}
		if fallback != nil {
			handled, val, err = fallback(ctx, cli, args, callLoc, span)
			if handled {
				return val, err
			}
		}
		first := ArgText(args[0])
		if err := ResolveCommandPath(first, span); err != nil {
			return runtime.Value{}, err
		}

		val, err = RunExecStatement(ctx, cli, args, span)
		return val, syntax.FrameAt(span, err)
	}
}

// makeExecBind bridges the evaluator's BindStmt dispatch into the
// loop pipeline. Output is suppressed. Dispatch order:
// `exec NAME` always runs as a subprocess; the embedder's
// BindFallback handles special-case bind paths (wait, net exec,
// domain dispatch); registered builtins handle their own names;
// an unrecognised first word runs as an external subprocess.
func makeExecBind(ctxFor func() context.Context, cli *cli.CLI, session *runtime.Session, env *runtime.Env, loc SourceLoc, fallback BindFallbackFunc) func([]runtime.Arg, source.Span) (runtime.BindResult, error) {
	return func(args []runtime.Arg, span source.Span) (runtime.BindResult, error) {
		if len(args) == 0 {
			return runtime.BindResult{}, syntax.SpanErrorf(span, "empty command form on '<-' RHS")
		}
		ctx := ctxFor()
		callLoc := dispatchSourceLoc(loc, env, span)

		if ArgText(args[0]) == "exec" {
			return runExternalAsBind(ctx, args[1:], span)
		}

		if fallback != nil {
			handled, br, err := fallback(ctx, cli, session, env, args, callLoc, span)
			if handled {
				return br, err
			}
		}

		// Capture the in-process Dispatch path's stdout / stderr
		// into a buffer rather than discarding it. Capturing puts
		// them on the rc envelope alongside what runExternalAsBind
		// already produces for
		// subprocesses, so both halves of the bind family populate
		// rc.Stdout / rc.Stderr uniformly. The bytes flow from
		// there: ordinary bind callers read them via $v.stdout;
		// the RenderDeferOutput hook flushes them to the
		// terminal on successful defers; the RenderDeferFailure
		// path already showed them in its labelled block on
		// failure.
		captured := cli.WithCaptureOutput()
		handled, val, err := Dispatch(ctx, captured.CLI, session, env, args, loc, span)
		if handled {
			stdout := captured.Stdout()
			stderr := captured.Stderr()
			if err != nil {
				rc := runtime.FailEnvelope()
				rc.Stdout = stdout
				if stderr != "" {
					rc.Stderr = stderr
				} else {
					rc.Stderr = err.Error()
				}
				return runtime.BindResult{Rc: rc, Primary: runtime.ValueFromEnvelope(rc)}, nil
			}
			rc := runtime.OkEnvelope()
			rc.Stdout = stdout
			rc.Stderr = stderr
			primary := val
			if primary.IsNil() {
				primary = runtime.ValueFromEnvelope(rc)
			}
			return runtime.BindResult{Rc: rc, Primary: primary}, nil
		}

		return runExternalAsBind(ctx, args, span)
	}
}

func dispatchSourceLoc(base SourceLoc, _ *runtime.Env, span source.Span) SourceLoc {
	if span.Pos.Line > 0 {
		file := span.Pos.File
		if file == "" {
			file = base.File
		}
		return SourceLoc{File: file, Line: span.Pos.Line, Col: span.Pos.Col}
	}
	return base
}

// runExternalAsBind runs args as a subprocess and packages the
// outcome as a BindResult. A launch failure returns a Go error;
// a non-zero exit is captured into the rc envelope so the bind
// caller can inspect it.
func runExternalAsBind(ctx context.Context, args []runtime.Arg, span source.Span) (runtime.BindResult, error) {
	if len(args) > 0 {
		if err := ResolveCommandPath(ArgText(args[0]), span); err != nil {
			return runtime.BindResult{}, err
		}
	}
	cap, err := RunExternal(ctx, args)
	if err != nil {
		return runtime.BindResult{}, err
	}

	rc := runtime.Envelope{
		ExitCode: cap.ExitCode,
		Stdout:   cap.Stdout,
		Stderr:   cap.Stderr,
	}
	return runtime.BindResult{Rc: rc, Primary: runtime.ValueFromEnvelope(rc)}, nil
}

// makeTraceHook builds the Env.Trace closure for one program run. The
// closure consults session.TraceEnabled() on every invocation
// so `trace on` / `trace off` can toggle tracing mid-script
// without rebuilding the Env.
//
// Line translation uses the def's registration source when we
// are inside a def body, falling back to the executing
// program's loc.Line otherwise. Without this, trace lines
// emitted from a def body get shifted by the top-level source
// start, and a nested call renders both the body's call and the
// top-level statement as if they lived on the same line.
func makeTracePrinter(cli *cli.CLI, session *runtime.Session) func(source.Pos, string) {
	return func(pos source.Pos, rendered string) {
		if !session.TraceEnabled() {
			return
		}
		file := pos.File
		if file == "" {
			file = "<stdin>"
		}
		_ = cli.PrintErrf("+ %s:%d: %s\n", file, pos.Line, rendered)
	}
}

func makeTraceHook(cli *cli.CLI, session *runtime.Session) func(source.Pos, string) {
	return makeTracePrinter(cli, session)
}

package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// Env is the execution environment for the evaluator. Session is
// the variable and def store; ExecCommand dispatches top-level
// commands to the shell's command and domain pipelines; ExecBind
// dispatches command forms on the right of a '<-' bind.
//
// A nil ExecCommand makes any top-level syntax.CommandStmt a runtime
// error; a nil ExecBind makes any syntax.BindStmt a runtime error.
// Tests that only exercise expression evaluation can leave both
// unset.
type Env struct {
	// Session is the variable and def store the evaluator reads and
	// writes throughout a run.
	Session *Session

	// ExecCommand runs a top-level syntax.CommandStmt. span is the
	// originating statement's source extent so handlers (and any
	// errors they emit) can frame diagnostics at the failing
	// command. The returned Value may be nil; any output is
	// visible on the CLI.
	ExecCommand func(args []Arg, span source.Span) (Value, error)

	// ExecBind runs a command form on the right of a '<-' bind.
	// span is the bind statement's source extent. The returned
	// BindResult carries the result envelope (Rc) and the
	// provider's primary result (Primary). Command failure
	// (non-zero exit, in-process error) is encoded on Rc as a
	// non-zero code with stdout and stderr set, not as a Go
	// error. A Go error is reserved for structural failures
	// (empty argv, malformed adapter, no provider for this
	// hook). Set by the shell runner; nil makes any syntax.BindStmt a
	// runtime error.
	ExecBind func(args []Arg, span source.Span) (BindResult, error)

	// ExecAssert runs a lowered Assert instruction. Set by the
	// shell runner; nil makes any lowered Assert a runtime error.
	ExecAssert func(*ir.Assert, *Env) error

	// PrintResult is called when a top-level syntax.ExprStmt produces a
	// value. It is the shell auto-print hook: a top-level "$x"
	// or "$x == 5" lands here. A nil callback
	// discards the value silently, which is the right behaviour
	// for embedded evaluators and for tests that do not care
	// about side output.
	PrintResult func(Value) error

	// RenderDeferFailure formats a defer-failure for the user.
	// The shell layer evaluates the deferred command via
	// ExecBind and, when the rc is not ok, calls this callback
	// so the driver can emit the labelled-block diagnostic. A
	// nil callback discards the rendering; the failure still
	// counts towards the script's exit code via Session.
	RenderDeferFailure func(stmtLoc source.Pos, args []Arg, rc Envelope)

	// Draining is true while runDefers is executing a defer
	// stack. The driver's dispatch closures read it to decide
	// which context a dispatch runs under: when the run's root
	// context has been cancelled (operator interrupt, runner
	// abort, timeout), defer drains get a fresh bounded cleanup
	// context instead of the dead root, so deferred cleanup
	// actually executes. runDefers saves and restores the flag,
	// so nested drains (a deferred def whose body has its own
	// defers) keep it set.
	Draining bool

	// RenderDeferOutput fires after every defer dispatch, win or
	// lose, so the driver can flush the deferred command's
	// captured stdout/stderr to its terminal. Defers go through
	// ExecBind, which captures output into the rc envelope (rc.
	// Stdout / rc.Stderr); without this hook the captured bytes
	// are dropped on the floor and `defer print "trace"` is
	// silent. A nil callback preserves the historical drop-the-
	// output behaviour for tests and embedders that do not want
	// side output during cleanup. The failure-path rendering in
	// RenderDeferFailure still shows the captured streams in
	// its labelled block, but the standalone success-output flow
	// is the job of this hook.
	RenderDeferOutput func(args []Arg, rc Envelope)

	// HandleJobLeak is called once per unmanaged job at scope
	// exit. The driver renders the diagnostic ('[job] FAIL at
	// file:line: argv') and is responsible for any cleanup
	// signal (typically SIGKILL so a leaked background process
	// does not survive the script). The handler owns counting
	// policy too: strict drivers call Session.RecordJobLeak, while
	// embedders with a nil handler deliberately let leaks pass
	// silently.
	HandleJobLeak func(*Job)

	// defers is the active defer scope's stack. execRegisterDefer
	// appends; runDefers drains LIFO at scope exit. The
	// top-level program and def bodies establish new scopes by
	// saving and replacing the field; if/foreach/retry blocks
	// share the enclosing scope.
	defers *[]deferEntry

	// jobs is the active scope's started-job registry. start
	// appends via RegisterJob; the scope-exit leak check walks
	// the slice after defers have run (so 'defer kill $job' has
	// the chance to mark Managed first) and reports any
	// unmanaged entries. Saved/restored alongside defers so
	// nested scopes compose.
	jobs *[]*Job

	// Trace, when non-nil, is invoked just before a statement
	// executes (and again when a deferred command fires at scope
	// exit). pos identifies the statement's source position;
	// rendered is a one-line summary of the statement with
	// interpolations resolved, e.g. an argv with `$prog`
	// substituted by its compact-JSON form, or `let x = <value>`
	// after the RHS has evaluated. Drivers typically prepend
	// `file:line:` and write the result to stderr. shell/ never
	// decides whether to trace; it only emits when the callback
	// is non-nil, so policy (a `trace on` toggle, a CLI flag)
	// lives in the driver-side installer.
	Trace func(pos source.Pos, rendered string)

	// RenderPollFailure, when set, is invoked when a poll runs
	// out of retry budget. The per-attempt retry reasons are
	// suppressed during the construct, so this is the single
	// place the user sees the timeout summary.
	RenderPollFailure func(span source.Span, timeout, every time.Duration, attempts int, lastRetry string)

	// Now is the clock hook the retrying constructs consult.
	// Drivers leave it nil to use time.Now; tests override it to
	// make timeout boundaries deterministic without package-global
	// state.
	Now func() time.Time

	// Sleep is the delay hook the retrying constructs consult.
	// Drivers leave it nil to use time.Sleep; tests override it
	// alongside Now so timeout boundaries stay deterministic.
	Sleep func(time.Duration)

	// defCallDepth counts the def-call frames currently active
	// on the evaluator's stack. runDefCall increments on entry
	// and decrements on exit; a value over MaxDefCallDepth is a
	// clean failure rather than a Go-runtime stack overflow.
	// Runaway recursion -- the natural shape of a value-returning
	// helper that forgets its base case -- otherwise dumps pages
	// of goroutine traces, which is unkind. The cap is far below
	// Go's stack limit so the diagnostic always wins.
	defCallDepth int

	// activePolls counts the poll constructs currently executing.
	// Helper bodies can observe this even when the helper text is
	// not itself nested directly inside a poll block.
	activePolls int
}

func (e *Env) now() time.Time {
	if e != nil && e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Env) sleep(d time.Duration) {
	if e != nil && e.Sleep != nil {
		e.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (e *Env) enterPoll() {
	if e != nil {
		e.activePolls++
	}
}

func (e *Env) exitPoll() {
	if e != nil && e.activePolls > 0 {
		e.activePolls--
	}
}

// InPoll reports whether a poll construct is currently executing.
// Helper bodies can observe this even when the helper text is not
// itself nested directly inside a poll block.
func (e *Env) InPoll() bool {
	return e != nil && e.activePolls > 0
}

// MaxDefCallDepth bounds how deep def calls can nest before
// the evaluator surfaces a clean recursion-limit diagnostic.
// The number is deliberately a few orders of magnitude smaller
// than Go's default per-goroutine stack ceiling so the
// diagnostic fires before the runtime panics. Real corpus
// patterns nest a handful of frames at most (a load_xxx helper
// calling guard_attach_yyy, say); 256 leaves abundant slack
// while still catching the textbook "forgot the base case"
// mistake within a fraction of a second.
const MaxDefCallDepth = 256

// callDef binds def parameters from args and runs the def's
// lowered entry block in env. Each call runs in its own session frame: parameters bind
// into the call frame, body-level `let` lives there too, and
// everything disappears when the call returns. Recursion works
// naturally because each call gets its own frame. Arity is
// checked against len(def.Params) and a mismatch yields a
// runtime error citing both the call site and the def's
// declaration site.
//
// Defs do not capture variable frames: the body resolves
// references against the caller's frame stack at call time plus
// its own call frame. Definition-time bindings are not part of
// the closure. If lexical capture becomes a need, that is a
// separate design.
func callDef(def *defValue, args []Arg, callLoc source.Pos, env *Env) error {
	_, _, _, err := runDefCall(def, args, callLoc, env)
	if err != nil {
		return decorateDefError(err, def, callLoc)
	}
	// A `return EXPR` inside the body short-circuits the body
	// loop via returnSignal. At command-form position the value
	// is discarded; the early exit itself is the only observable
	// effect. callDefAsBind handles the bind-position case and
	// keeps the Value. Defer failures still increment the
	// session counter via runDefers so the script's exit code
	// reflects the failure even when the call discards the
	// value -- that view is global by design.
	return nil
}

// callDefAsBind runs def in bind position and packages the
// outcome as a BindResult. The body's `return EXPR` becomes
// Primary; a body that runs to completion without `return`
// produces Primary = ValueFromEnvelope(Rc), matching the
// no-payload command-bind family (exec, bpftool, wait). The Rc
// is successful by default; a failure from a defer registered in THIS
// def's body marks Rc failed so a `guard p <- f` halts and
// a `let r <- f` lets the caller inspect the cleanup outcome.
//
// The local-cleanup view is load-bearing: a nested helper
// invoked at command form during the body has already run its
// own defers and left any failures on the session counter; that
// counter is the global exit-code view, not "did this def's
// cleanup fail". runDefCall threads runDefers's local return up
// to here so the flip reflects only defers belonging to this
// def's body, matching the def-local cleanup contract.
//
// A non-return error from the body (unbound variable, type
// error, guard halt inside the body, parse error from a
// dynamic source, etc.) propagates as a Go error; the bind
// path then frames it and the calling script halts. No
// bindings happen in that case.
func callDefAsBind(def *defValue, args []Arg, callLoc source.Pos, env *Env) (BindResult, error) {
	returned, hasReturn, localDeferFailures, err := runDefCall(def, args, callLoc, env)
	if err != nil {
		return BindResult{}, decorateDefError(err, def, callLoc)
	}

	rc := OkEnvelope()
	if localDeferFailures > 0 {
		rc = FailEnvelope()
	}
	primary := returned
	if !hasReturn {
		primary = ValueFromEnvelope(rc)
	}
	return BindResult{Rc: rc, Primary: primary}, nil
}

// runDefCall is the shared body of callDef and callDefAsBind. It
// checks arity, enforces the recursion limit, and dispatches into
// the lowered def body. The returned tuple matches callDefAsBind's
// needs: returned Value, hasReturn flag, def-local defer failure
// count, and any escaping runtime error.
func runDefCall(def *defValue, args []Arg, callLoc source.Pos, env *Env) (Value, bool, int, error) {
	if len(args) != len(def.Params) {
		return Value{}, false, 0, syntax.LocErrorf(callLoc, "%s: expected %d argument(s), got %d (def declared at %d:%d)", def.Name, len(def.Params), len(args), def.Pos.Line, def.Pos.Col)
	}
	// Catch runaway recursion before Go's stack does. The cap
	// is far below Go's per-goroutine stack ceiling so the
	// clean diagnostic wins over a runtime panic; the count is
	// pushed / popped around the body so unrelated calls do not
	// accumulate against the limit and a backtrack out of a
	// recursive helper resumes with the right depth.
	if env.defCallDepth >= MaxDefCallDepth {
		return Value{}, false, 0, syntax.LocErrorf(callLoc, "in def %s: recursion depth limit exceeded (%d)", def.Name, MaxDefCallDepth)
	}
	env.defCallDepth++
	defer func() { env.defCallDepth-- }()
	if def.Entry == nil {
		return Value{}, false, 0, syntax.LocErrorf(callLoc, "def %s has no IR body", def.Name)
	}
	return runLoweredDefCall(def, args, env)
}

// decorateDefError annotates a *syntax.SyntaxError escaping a def body
// with the def name and the caller's line/col. Positions are
// already absolute, so the helper only needs to preserve the
// innermost source span and add call-site context to the message.
//
// Independently of the source coordinates, the error's message
// gains a leading "in def NAME (called at L:C): " annotation
// so a runtime error escaping a value-returning helper is no
// longer ambiguous about which call site produced it.
// Decoration is suppressed when the error
// already carries an inner def's annotation (the innermost
// callDef has decorated first), so propagation preserves the
// closest-to-the-failure call rather than over-attributing to
// every wrapping caller.
func decorateDefError(err error, def *defValue, callLoc source.Pos) error {
	if err == nil {
		return err
	}

	var se *syntax.SyntaxError
	if !errors.As(err, &se) {
		return err
	}

	// Annotate the message with the def name plus the caller's
	// line:col. An inner annotation -- recognised by the
	// "in def " prefix -- leaves the message alone so the
	// innermost def wins the attribution.
	if !strings.HasPrefix(se.Msg, "in def ") {
		callLine := callLoc.Line
		callCol := callLoc.Col
		if callLine > 0 {
			se.Msg = fmt.Sprintf("in def %s (called at %d:%d): %s", def.Name, callLine, callCol, se.Msg)
		} else {
			se.Msg = fmt.Sprintf("in def %s: %s", def.Name, se.Msg)
		}
	}
	return err
}

// bindDefArg converts one call argument into the value bound to the
// def's i-th parameter. Untyped parameters keep argToValue's
// baseline rule. An annotated parameter is a declared input
// boundary: it parses bare words (the genuinely untyped form) into
// the declared type, and requires every already-typed argument to
// match -- a quoted literal asserts string, a typed variable asserts
// its kind, and the annotation never coerces across an asserted
// kind. Failures cite the argument's span and name the def and
// parameter.
func bindDefArg(def *defValue, i int, arg Arg) (Value, error) {
	p := def.Params[i]
	if p.Type == "" {
		return argToValue(arg), nil
	}
	fail := func(got string) error {
		return syntax.SpanErrorf(ArgSpan(arg), "def %s: parameter %q: expected %s, got %s", def.Name, p.Name, p.Type, got)
	}
	switch a := arg.(type) {
	case WordArg:
		return parseWordAs(p.Type, a.Text, fail)
	case QuotedArg:
		if p.Type == "string" {
			return StringValue(a.Text), nil
		}
		return Value{}, fail(fmt.Sprintf("the quoted string %q (quoting asserts string; drop the quotes to parse it)", a.Text))
	default:
		v := argToValue(arg)
		if got := scalarKind(v); got != p.Type {
			if got == "" {
				return Value{}, fail("a non-scalar value")
			}
			return Value{}, fail("a " + got)
		}
		return v, nil
	}
}

// parseWordAs parses a bare word into the declared parameter type.
func parseWordAs(typ, text string, fail func(string) error) (Value, error) {
	switch typ {
	case "number":
		if !syntax.IsJSONNumber(text) {
			return Value{}, fail(fmt.Sprintf("%q", text))
		}
		return ValueFromAny(json.Number(text)), nil
	case "bool":
		switch text {
		case "true":
			return BoolValue(true), nil
		case "false":
			return BoolValue(false), nil
		}
		return Value{}, fail(fmt.Sprintf("%q", text))
	default: // "string"
		return StringValue(text), nil
	}
}

// scalarKind names a Value's scalar kind in annotation vocabulary:
// "number", "string", or "bool". Structured, null, and absent
// values return "".
func scalarKind(v Value) string {
	switch v.v.(type) {
	case string:
		return "string"
	case json.Number, float64:
		return "number"
	case bool:
		return "bool"
	default:
		return ""
	}
}

// argToValue converts a post-expansion Arg into a Value suitable for
// binding to a def parameter. The typing rule at the call boundary:
// variables keep their value kinds (a number-valued $n arrives in
// the def as a number), while bare and quoted literals are words by
// shell convention and bind as strings. Structured and adapter args
// carry their already-resolved Value through.
func argToValue(a Arg) Value {
	switch v := a.(type) {
	case WordArg:
		return StringValue(v.Text)
	case QuotedArg:
		return StringValue(v.Text)
	case ScalarValueArg:
		if v.HasValue {
			return v.Value
		}
		return StringValue(v.Text)
	case StructuredValueArg:
		return v.Value
	case AdapterArg:
		return v.Value
	default:
		return Value{}
	}
}

// deferEntry is one captured invocation in a defer scope. Args
// are evaluated at register time and frozen onto the entry; Cmd
// holds the original command form so the diagnostic renderer can
// cite the source location of the defer statement.
type deferEntry struct {
	source.Span
	Args   []Arg
	policy ir.DispatchPolicy

	// trace is the Env.Trace callback captured at registration
	// time, or nil if tracing was not active when defer ran.
	// runDefers invokes this saved callback (not the current
	// env.Trace) so the fire trace cites the file:line of the
	// REGISTRATION site, not whatever source happens to be
	// unwinding. Without this, a script-scope defer registered
	// in one source unit but fired much later would inherit the
	// later source's shift and cite the wrong line.
	trace func(pos source.Pos, rendered string)
}

// runDefers drains the active defer scope's stack in LIFO order,
// dispatching each entry via env.ExecBind. A non-ok rc is rendered
// through RenderDeferFailure (when set) and counted via Session so
// the script's exit code reflects the failure; cleanup continues
// across failures. Structural errors from ExecBind are rare;
// they are rendered with an empty-rc envelope so the user still
// sees a labelled block.
//
// The return value is the count of failures observed in THIS
// scope only -- the local-scope view a caller needs when it has
// to react to its own cleanup result. Nested scopes have already
// run their own defers by the time their stacks reach a caller
// of this function, so their failures land on the session
// counter (global view, used for exit-code accounting) but never
// in the local count returned here. callDefAsBind uses the local
// count to decide whether to mark the bind-position Rc failed, so
// the def-local cleanup contract does not silently broaden into "anything
// that failed during this call's dynamic extent".
func runDefers(env *Env, stack []deferEntry) int {
	if env.ExecBind == nil {
		return 0
	}
	prev := env.Draining
	env.Draining = true
	defer func() { env.Draining = prev }()
	failures := 0
	for i := len(stack) - 1; i >= 0; i-- {
		entry := stack[i]
		// Use the trace callback captured at registration so
		// the fire's file:line cites where defer was written,
		// not where the surrounding scope happens to be
		// unwinding. Fall back to the current env.Trace only
		// when tracing was off at registration time but is on
		// now -- still useful, even if the line is approximate.
		traceFn := entry.trace
		if traceFn == nil {
			traceFn = env.Trace
		}
		if traceFn != nil {
			traceFn(entry.Pos, "defer fire: "+renderArgvTrace(entry.Args))
		}
		// Defer dispatches through the shared bind-style
		// helper so the def-vs-external precedence matches the
		// other bind-position sites. Failure rendering and
		// session counter accounting stay in this loop because
		// they are defer-specific (RenderDeferFailure block,
		// RecordDeferFailure tick); the helper only handles
		// the head resolution.
		result, err := dispatchBindByPolicy(entry.policy, entry.Args, entry.Pos, entry.Span, env)
		if err != nil {
			rc := FailEnvelopeFromError(err)
			if env.RenderDeferFailure != nil {
				env.RenderDeferFailure(entry.Pos, entry.Args, rc)
			}
			env.Session.RecordDeferFailure()
			failures++
			continue
		}

		// Flush the captured stdout/stderr through the driver
		// before the failure-path branch decides whether to
		// also render a labelled block: a successful defer's
		// output would otherwise be dropped, and a failing
		// defer's output is included in the failure block below
		// so the success-output hook only carries the
		// non-failure case.
		if result.Rc.OK() && env.RenderDeferOutput != nil {
			env.RenderDeferOutput(entry.Args, result.Rc)
		}
		if !result.Rc.OK() {
			if env.RenderDeferFailure != nil {
				env.RenderDeferFailure(entry.Pos, entry.Args, result.Rc)
			}
			env.Session.RecordDeferFailure()
			failures++
		}
	}
	return failures
}

// RegisterJob appends a started Job to the active job scope's
// registry so the scope-exit leak check can detect an unmanaged
// lifecycle. Outside any job scope (no driver-established
// withJobScope) the call is a no-op: there is nothing to leak
// from. j must be non-nil; nil is reserved for "no job" rather
// than used as a sentinel here.
func (e *Env) RegisterJob(j *Job) {
	if e.jobs == nil {
		return
	}
	*e.jobs = append(*e.jobs, j)
}

// ActiveJobs returns a snapshot of the jobs registered in the
// innermost active job scope, in registration order. The slice
// is a fresh copy so callers may sort or filter without
// disturbing the registry. Outside any job scope the result is
// nil. Used by the 'jobs' builtin to list everything alive in
// the current session.
func (e *Env) ActiveJobs() []*Job {
	if e.jobs == nil {
		return nil
	}
	out := make([]*Job, len(*e.jobs))
	copy(out, *e.jobs)
	return out
}

// ReapJobs removes every job from the active job scope's
// registry for which shouldReap returns true. Registration
// order is preserved for survivors. Outside any job scope
// the call is a no-op. Used by the 'reap' builtin to drop
// completed entries while leaving running jobs alone; the
// predicate shape lets callers choose their own definition
// of 'done' (closed Done channel, Managed flag, both, ...).
func (e *Env) ReapJobs(shouldReap func(*Job) bool) {
	if e.jobs == nil {
		return
	}
	src := *e.jobs
	dst := src[:0]
	for _, j := range src {
		if !shouldReap(j) {
			dst = append(dst, j)
		}
	}
	// Clear any tail references so reaped jobs are not held
	// alive by the underlying array.
	for i := len(dst); i < len(src); i++ {
		src[i] = nil
	}
	*e.jobs = dst
}

// withJobScope establishes a job scope around fn: any Job that
// fn registers via Env.RegisterJob is tracked in this scope's
// registry, and on exit each unmanaged entry is reported through
// HandleJobLeak and counted on the session.
//
// Drivers open exactly one job scope per session unit: the whole
// script in script mode, the whole imported file in import mode,
// the whole interactive session in interactive mode. Inner
// blocks (def bodies, foreach, retry) deliberately do not open
// new job scopes, so a job started inside a def joins the
// caller's registry and survives the def's return without
// being flagged.
//
// Job leak reporting runs after fn returns. When the driver
// composes withJobScope around an outer withDeferScope (the
// usual shape: defers nest inside jobs), the outer defers run
// before the leak walk, so 'defer kill $job' marks a job
// Managed before the leak walk sees it.
func withJobScope(env *Env, fn func() error) error {
	saved := env.jobs
	var jobs []*Job
	env.jobs = &jobs
	bodyErr := fn()
	env.jobs = saved
	reportJobLeaks(env, jobs)
	return bodyErr
}

// reportJobLeaks walks the scope's registered jobs and invokes
// HandleJobLeak on any the script never marked Managed (via
// wait or kill). The handler owns the policy: a strict driver
// renders a diagnostic, kills the process, and records the
// leak on the session so the run exits non-zero; a friendly
// driver kills silently and leaves the session counter
// untouched. The shell layer takes no opinion -- a nil handler
// means "nothing to do; the leak passes silently".
func reportJobLeaks(env *Env, jobs []*Job) {
	if env.HandleJobLeak == nil {
		return
	}
	for _, j := range jobs {
		if j.IsManaged() {
			continue
		}
		env.HandleJobLeak(j)
	}
}

// dispatchBindByPolicy runs the shell-level head-resolution rule
// named by policy. Today the only bind-position policy is
// def-first fallback to ExecBind, but keeping the policy
// explicit lets the lowered IR expose the rule and keeps future
// dispatch growth honest.
func dispatchBindByPolicy(policy ir.DispatchPolicy, args []Arg, callLoc source.Pos, span source.Span, env *Env) (BindResult, error) {
	switch policy {
	case ir.DispatchPolicyDefThenExecBind:
		if def, ok := lookupDefHead(args, env); ok {
			return callDefAsBind(def, args[1:], callLoc, env)
		}
		return env.ExecBind(args, span)
	default:
		return BindResult{}, syntax.SpanErrorf(span, "unsupported bind dispatch policy %s", dispatchPolicyName(policy))
	}
}

// dispatchCommandHead is the command-style sibling: defs first,
// then env.ExecCommand.
func dispatchCommandByPolicy(policy ir.DispatchPolicy, args []Arg, callLoc source.Pos, span source.Span, env *Env) error {
	switch policy {
	case ir.DispatchPolicyDefThenExecCommand:
		if def, ok := lookupDefHead(args, env); ok {
			return callDef(def, args[1:], callLoc, env)
		}
		if env.ExecCommand == nil {
			return syntax.SpanErrorf(span, "command execution is not configured")
		}
		_, err := env.ExecCommand(args, span)
		return syntax.FrameAt(span, err)
	default:
		return syntax.SpanErrorf(span, "unsupported command dispatch policy %s", dispatchPolicyName(policy))
	}
}

func dispatchPolicyName(policy ir.DispatchPolicy) string {
	switch policy {
	case ir.DispatchPolicyDefThenExecBind:
		return "def-then-exec-bind"
	case ir.DispatchPolicyDefThenExecCommand:
		return "def-then-exec-command"
	default:
		return fmt.Sprintf("<unknown:%d>", int(policy))
	}
}

// lookupDefHead returns the def value when args[0] names a
// registered def. Used by both dispatch helpers above.
func lookupDefHead(args []Arg, env *Env) (*defValue, bool) {
	if len(args) == 0 {
		return nil, false
	}
	name, ok := commandHeadName(args[0])
	if !ok {
		return nil, false
	}
	return env.Session.getDef(name)
}

// ErrRequireFailed is the sentinel error chained under a
// *RequireFailure so `errors.Is(err, ErrRequireFailed)` at
// script-loop boundaries recognises a failed `require`. The
// driver layer re-exports this value so callers reading driver
// import paths see the same sentinel.
var ErrRequireFailed = errors.New("require failed")

// GuardFailure is the error type a 'guard ... <- CMD' statement
// returns when the captured rc is not ok. The driver formats the
// failure through its renderer; the language layer carries the
// envelope so the renderer has the captured stdout, stderr, exit
// code, and the offending bind's source location, plus the
// resolved Args so the renderer can show the command line that
// failed and the Primary name (the bind target the user wrote)
// for the diagnostic.
type GuardFailure struct {
	// Span is the source extent of the offending guard bind, so the
	// renderer can cite the failing statement.
	source.Span

	// Primary is the bind target the user wrote ("_" for a
	// throwaway bind), named in the diagnostic.
	Primary string

	// Args is the resolved argument list of the failed command, so
	// the renderer can show the command line that failed.
	Args []Arg

	// Envelope is the captured non-ok result, carrying the exit
	// code, stdout, and stderr.
	Envelope Envelope
}

// Error renders the guard failure as "guard <target>: command failed
// (exit N)", appending the captured stderr when present.
func (e *GuardFailure) Error() string {
	target := e.Primary
	if target == "" || target == "_" {
		target = "_"
	}
	if e.Envelope.Stderr != "" {
		return fmt.Sprintf("guard %s: command failed (exit %d): %s",
			target, e.Envelope.ExitCode, e.Envelope.Stderr)
	}
	return fmt.Sprintf("guard %s: command failed (exit %d)", target, e.Envelope.ExitCode)
}

// RequireFailure is the typed-error form of a `require`
// predicate that did not hold. Unwrapping yields
// ErrRequireFailed so existing `errors.Is(err, ErrRequireFailed)`
// halts at the same script-loop boundaries that already check
// for the sentinel.
type RequireFailure struct {
	// Span is the source extent of the failing require statement.
	source.Span

	// Expr is the rendered predicate expression, or empty when the
	// caller supplied no expression text.
	Expr string
}

// Error renders the failure as "require failed", appending the
// expression text when Expr is non-empty.
func (e *RequireFailure) Error() string {
	if e.Expr == "" {
		return "require failed"
	}
	return "require failed: " + e.Expr
}

// Unwrap returns ErrRequireFailed so errors.Is(err, ErrRequireFailed)
// halts at the same script-loop boundaries that already check for the
// sentinel.
func (e *RequireFailure) Unwrap() error { return ErrRequireFailed }

// commandHeadName extracts the command name from the first argument
// of a command call, but only when the first argument is a literal
// word (the syntactic shape that names a command). Quoted strings,
// resolved scalars, and structured values do not name commands and
// are not eligible to dispatch as defs.
func commandHeadName(a Arg) (string, bool) {
	w, ok := a.(WordArg)
	if !ok {
		return "", false
	}
	return w.Text, true
}

// structuredShape returns a short description of a structured
// Value suitable for error messages. The declared semantics.OriginKind is
// used when it is anything other than semantics.OriginUnknown (so "program"
// or "exec.result" read as such); otherwise the raw Go shape is
// inspected so an untagged record or array still reports
// meaningfully as "object" or "array" rather than the useless
// "unknown".
func structuredShape(v Value) string {
	if k := v.Kind(); k != semantics.OriginUnknown && k != semantics.OriginScalar {
		return k.String()
	}
	switch v.Raw().(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	default:
		return "structured"
	}
}

// RenderCompact renders a Value to a single-line string form.
// Scalars (including semantics.OriginNull, which renders as "null") use
// their text form; structured values marshal to compact JSON;
// an absent Value renders as "null" so a missing slot surfaces as
// visible "null" rather than silently vanishing or erroring.
// Used wherever a Value must flatten onto a single line -- string
// interpolation and multi-argument print both feed through it
// so formatting stays consistent across those paths.
func RenderCompact(v Value) (string, error) {
	if v.IsNil() {
		return "null", nil
	}
	if v.IsStructured() {
		b, err := json.Marshal(v.Raw())
		if err != nil {
			return "", fmt.Errorf("render %s: %v", structuredShape(v), err)
		}
		return string(b), nil
	}
	return v.Scalar()
}

// argTraceText renders a single Arg as text suitable for an
// execution trace. Scalars and word forms emit their resolved
// text; structured values render as compact JSON so the user can
// see the value that flowed into the call rather than the bare
// `$name` placeholder; adapter args keep their `adapter:$var.path`
// form because the temp-file backing path is uninteresting for
// debugging. Mirrors the cmd-side argText spelling, with the
// deliberate difference that StructuredValueArg yields the value
// not the variable name.
func argTraceText(a Arg) string {
	switch v := a.(type) {
	case WordArg:
		return v.Text
	case QuotedArg:
		return v.Text
	case ScalarValueArg:
		return v.Text
	case StructuredValueArg:
		s, err := RenderCompact(v.Value)
		if err != nil {
			return "$" + v.Name
		}
		return s
	case AdapterArg:
		if v.Path != "" {
			return fmt.Sprintf("%s:$%s.%s", v.Adapter, v.Name, v.Path)
		}
		return fmt.Sprintf("%s:$%s", v.Adapter, v.Name)
	default:
		return ""
	}
}

// renderArgvTrace joins the resolved Arg list as a single line for
// the trace hook. Whitespace inside a scalar is left as-is; the
// trace is for human reading, not for re-parsing, so re-quoting
// every value would obscure more than it clarifies.
func renderArgvTrace(args []Arg) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = argTraceText(a)
	}
	return strings.Join(parts, " ")
}

func resolveVarRefValueParts(name, path string, span source.Span, env *Env) (Value, error) {
	v, ok := env.Session.Get(name)
	if !ok {
		return Value{}, syntax.SpanErrorf(span, "undefined variable %q", name)
	}

	if path == "" {
		return v, nil
	}
	path, err := resolveDynamicPath(path, env, span)
	if err != nil {
		return Value{}, err
	}

	lv, err := v.LookupValue(name, path)
	if err != nil {
		return Value{}, syntax.SpanErrorf(span, "%v", err)
	}
	return lv, nil
}

func resolveVarRefArgParts(name, path string, span source.Span, env *Env) (Arg, error) {
	v, ok := env.Session.Get(name)
	if !ok {
		return nil, syntax.SpanErrorf(span, "undefined variable: %s", name)
	}

	resolved := v
	if path != "" {
		path, err := resolveDynamicPath(path, env, span)
		if err != nil {
			return nil, err
		}

		// Soft lookup at the arg boundary: absent paths surface
		// as MissingArg so the shape-test predicates
		// (present / missing / strict null) can distinguish
		// "field not in the value tree" from "field present and
		// null". Hard lookup errors (malformed path,
		// non-traversable intermediate) still propagate.
		presence, err := v.LookupPresence(name, path)
		if err != nil {
			return nil, syntax.SpanErrorf(span, "%v", err)
		}

		if presence.IsMissing() {
			return MissingArg{Name: name, Path: path, Span: span}, nil
		}
		resolved = presence.Value()
	}
	if resolved.IsNil() || resolved.IsNull() {
		// Terminal null is a value. Surface it as NilArg so
		// downstream consumers (print, jq, the null/present
		// predicates) can decide how to handle it. Commands
		// that need a non-null arg surface their own clearer
		// diagnostic when they encounter NilArg.
		return NilArg{Span: span}, nil
	}
	if resolved.IsStructured() {
		return StructuredValueArg{Name: name, Value: resolved, Span: span}, nil
	}
	s, err := resolved.Scalar()
	if err != nil {
		return nil, syntax.SpanErrorf(span, "variable %s: %v", qualify(name, path), err)
	}
	return ScalarValueArg{Text: s, Value: resolved, HasValue: true, Span: span}, nil
}

// qualify produces a "name.path" string for error messages, or
// just "name" when path is empty.
func qualify(name, path string) string {
	if path == "" {
		return name
	}
	return name + "." + path
}

func resolveAdapterArgParts(adapter, name, path string, span source.Span, env *Env) (Arg, error) {
	v, ok := env.Session.Get(name)
	if !ok {
		return nil, syntax.SpanErrorf(span, "undefined variable: %s", name)
	}

	resolved := v
	if path != "" {
		var err error
		path, err = resolveDynamicPath(path, env, span)
		if err != nil {
			return nil, err
		}

		lv, err := v.LookupValue(name, path)
		if err != nil {
			return nil, syntax.SpanErrorf(span, "%v", err)
		}

		resolved = lv
	}
	if resolved.IsNil() || resolved.IsNull() {
		return nil, syntax.SpanErrorf(span, "adapter %s: variable %s is null", adapter, name)
	}
	return AdapterArg{
		Adapter: adapter,
		Name:    name,
		Path:    path,
		Value:   resolved,
		Span:    span,
	}, nil
}

// resolveDynamicPath rewrites "[$ident]" segments in path to "[N]"
// using the current session bindings. The tokeniser accepts the
// "[$ident]" form alongside literal "[digits]", deferring the
// integer resolution here so a single foreach can index parallel
// lists without round-tripping through jq. Segments that are not
// "[$ident]" pass through unchanged; downstream parsePath only ever
// sees the digit form.
//
// Errors cite the host span (the syntax.VarRefExpr's `$xs[$i]`), not the
// inner `[$i]` position, because the path text in syntax.VarRefExpr is
// stored without per-segment offsets. The index variable must
// resolve to a scalar integer; strings parsable as an integer are
// accepted (jq -r round-trips produce string scalars), booleans and
// nulls are rejected.
func resolveDynamicPath(path string, env *Env, span source.Span) (string, error) {
	if !strings.Contains(path, "[$") {
		return path, nil
	}
	var b strings.Builder
	b.Grow(len(path))
	i := 0
	for i < len(path) {
		if path[i] != '[' || i+1 >= len(path) || path[i+1] != '$' {
			b.WriteByte(path[i])
			i++
			continue
		}
		nameStart := i + 2
		j := nameStart
		for j < len(path) && (isIdentStartByte(path[j]) || (j > nameStart && isIdentContinueByte(path[j]))) {
			j++
		}
		if j == nameStart || j >= len(path) || path[j] != ']' {
			// Should not be reachable: lexPathIndex would have
			// rejected this at tokenisation time. Surface the
			// state defensively rather than silently writing
			// out malformed text.
			return "", syntax.SpanErrorf(span, "malformed dynamic index in path %q", path)
		}
		name := path[nameStart:j]
		v, ok := env.Session.Get(name)
		if !ok {
			return "", syntax.SpanErrorf(span, "index variable $%s is not defined", name)
		}

		n, err := valueToIndex(v)
		if err != nil {
			return "", syntax.SpanErrorf(span, "index variable $%s: %v", name, err)
		}

		b.WriteByte('[')
		b.WriteString(strconv.Itoa(n))
		b.WriteByte(']')
		i = j + 1
	}
	return b.String(), nil
}

// valueToIndex converts a scalar Value to an int suitable for array
// indexing. Accepts json.Number (the shape `range N` produces),
// float64 (only if integral), and strings parseable as a base-10
// integer. Booleans, nulls, and structured values are rejected.
// Negative integers are returned as-is; range validation lives in
// walkPath's indexStep handler.
func valueToIndex(v Value) (int, error) {
	switch x := v.Raw().(type) {
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			return 0, fmt.Errorf("index must be an integer, got %q", x)
		}
		return int(n), nil
	case float64:
		if x != math.Trunc(x) {
			return 0, fmt.Errorf("index must be an integer, got %v", x)
		}
		return int(x), nil
	case string:
		n, err := strconv.Atoi(x)
		if err != nil {
			return 0, fmt.Errorf("index must be an integer, got %q", x)
		}
		return n, nil
	case bool:
		return 0, fmt.Errorf("index must be an integer, got bool")
	case nil:
		return 0, fmt.Errorf("index is null")
	default:
		return 0, fmt.Errorf("index must be a scalar integer, got %s", v.Kind())
	}
}

// valueToArg wraps a Value in the most specific Arg variant for
// the dispatch boundary: structured values stay structured,
// scalars become ScalarValueArg, nil becomes NilArg so the
// receiving command can decide whether to accept null at its
// own input boundary (jq, print, the strict-null and present
// predicates) rather than blanket-erroring at resolution time.
// span is attached to the resulting Arg so command-handler
// parsers can frame argument-position errors at the originating
// expression.
func valueToArg(v Value, span source.Span) (Arg, error) {
	if v.IsNil() || v.IsNull() {
		return NilArg{Span: span}, nil
	}
	if v.IsStructured() {
		return StructuredValueArg{Value: v, Span: span}, nil
	}
	s, err := v.Scalar()
	if err != nil {
		return nil, err
	}
	return ScalarValueArg{Text: s, Value: v, HasValue: true, Span: span}, nil
}

// compareKind classifies a Value for the purpose of strict
// comparison dispatch. Numbers (json.Number, float64) are
// "number"; plain strings are "string"; booleans are "bool";
// explicit JSON null is "null" -- a first-class comparable
// value with the equality rules `null == null` true and `null
// == X` false for any non-null X (no ordering). Anything else
// (map, slice, absent values) returns "" and is rejected by
// evalCompare with an error citing the actual underlying type
// so users see why the operands are incomparable.
func compareKind(v Value) string {
	if v.IsNull() {
		return "null"
	}
	switch v.Raw().(type) {
	case json.Number, float64:
		return "number"
	case string:
		return "string"
	case bool:
		return "bool"
	}
	return ""
}

// evalCompare performs a strict, type-aware comparison. Both
// operands must classify as the same compareKind: number-vs-number
// compares as floats, string-vs-string as text, bool-vs-bool only
// supports == and != (booleans have no defined ordering). Cross-type
// comparisons are an error rather than a silent false, matching
// jq's strict equality and surfacing operator misuse loudly. To
// compare stringy numeric input (e.g. exec stdout) against a
// number, coerce explicitly via "$x |> jq tonumber" first.
func evalCompare(op string, l, r Value, span source.Span) (Value, error) {
	lk := compareKind(l)
	rk := compareKind(r)
	if lk == "" {
		return Value{}, syntax.SpanErrorf(span, "%s: left side is a %s; only scalars (numbers, strings, booleans) can be compared with %s", op, l.Kind(), op)
	}
	if rk == "" {
		return Value{}, syntax.SpanErrorf(span, "%s: right side is a %s; only scalars (numbers, strings, booleans) can be compared with %s", op, r.Kind(), op)
	}
	// Null comparisons are special: `null == null` is true,
	// `null == X` (X non-null) is false, and the cross-kind
	// case is well-defined rather than an error. The language
	// supports null as a first-class comparable value, so any
	// op that does not need ordering (== and !=) bypasses the
	// strict same-kind rule when at least one operand is null.
	// Ordering operators (<, <=, >, >=) are not defined for
	// null and surface as an explicit error.
	if lk == "null" || rk == "null" {
		if op != "==" && op != "!=" {
			return Value{}, syntax.SpanErrorf(span, "binary %s: null supports only == and != (no ordering)", op)
		}
		bothNull := lk == "null" && rk == "null"
		pass := (op == "==") == bothNull
		return BoolValue(pass), nil
	}
	if lk != rk {
		return Value{}, syntax.SpanErrorf(span, "binary %s: cannot compare %s to %s; coerce explicitly (e.g. \"$x |> jq tonumber\" for stringy numeric input)", op, lk, rk)
	}
	left, err := l.Scalar()
	if err != nil {
		return Value{}, syntax.SpanErrorf(span, "binary %s: left: %v", op, err)
	}

	right, err := r.Scalar()
	if err != nil {
		return Value{}, syntax.SpanErrorf(span, "binary %s: right: %v", op, err)
	}

	switch lk {
	case "number":
		v, err := evalNumericComparison(op, left, right)
		if err != nil {
			return Value{}, syntax.SpanErrorf(span, "%v", err)
		}
		return v, nil
	case "bool":
		if op != "==" && op != "!=" {
			return Value{}, syntax.SpanErrorf(span, "binary %s: booleans support only == and !=", op)
		}
		v, err := evalSymbolicTextComparison(op, left, right)
		if err != nil {
			return Value{}, syntax.SpanErrorf(span, "%v", err)
		}
		return v, nil
	default:
		v, err := evalSymbolicTextComparison(op, left, right)
		if err != nil {
			return Value{}, syntax.SpanErrorf(span, "%v", err)
		}
		return v, nil
	}
}

// isArithmeticOpText reports whether op is one of the five
// arithmetic operators. Separate from isArithmeticOp (which
// operates on a syntax.Token) because the evaluator works with the
// already-extracted Op string on syntax.BinaryExpr.
func isArithmeticOpText(op string) bool {
	switch op {
	case "+", "-", "*", "/", "%":
		return true
	}
	return false
}

type shellNumber struct {
	text     string
	integral bool
	i        *big.Int
	f        float64
}

func parseShellNumber(text, side string) (shellNumber, error) {
	if !syntax.IsJSONNumber(text) {
		return shellNumber{}, fmt.Errorf("%s operand %q is not numeric", side, text)
	}
	n := shellNumber{text: text, integral: syntax.IsIntegralJSONNumber(text)}
	if n.integral {
		i, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return shellNumber{}, fmt.Errorf("%s operand %q is not numeric", side, text)
		}

		n.i = i
		return n, nil
	}
	f, err := json.Number(text).Float64()
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return shellNumber{}, fmt.Errorf("%s operand %q exceeds the representable range", side, text)
	}

	n.f = f
	return n, nil
}

func (n shellNumber) float64(side string) (float64, error) {
	if !n.integral {
		return n.f, nil
	}
	f, _ := new(big.Float).SetInt(n.i).Float64()
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, fmt.Errorf("%s operand %q exceeds the representable range", side, n.text)
	}
	return f, nil
}

func numericValueFromInt(i *big.Int) Value {
	return Value{v: json.Number(i.String()), kind: semantics.OriginScalar}
}

// evalArithmetic performs exact integer arithmetic for integral
// operands where the operator can stay integral (+, -, *, %).
// Division and any mixed/fractional operation promote to finite
// float64. Division and modulo by zero are runtime errors.
func evalArithmetic(op, left, right string) (Value, error) {
	a, err := parseShellNumber(left, "left")
	if err != nil {
		return Value{}, fmt.Errorf("arithmetic %s: %w", op, err)
	}

	b, err := parseShellNumber(right, "right")
	if err != nil {
		return Value{}, fmt.Errorf("arithmetic %s: %w", op, err)
	}

	if a.integral && b.integral && op != "/" {
		if op == "%" && b.i.Sign() == 0 {
			return Value{}, fmt.Errorf("division by zero")
		}
		r := new(big.Int)
		switch op {
		case "+":
			r.Add(a.i, b.i)
		case "-":
			r.Sub(a.i, b.i)
		case "*":
			r.Mul(a.i, b.i)
		case "%":
			r.Rem(a.i, b.i)
		default:
			return Value{}, fmt.Errorf("unknown arithmetic operator %q", op)
		}
		return numericValueFromInt(r), nil
	}
	af, err := a.float64("left")
	if err != nil {
		return Value{}, fmt.Errorf("arithmetic %s: %w", op, err)
	}

	bf, err := b.float64("right")
	if err != nil {
		return Value{}, fmt.Errorf("arithmetic %s: %w", op, err)
	}

	var r float64
	switch op {
	case "+":
		r = af + bf
	case "-":
		r = af - bf
	case "*":
		r = af * bf
	case "/":
		if bf == 0 {
			return Value{}, fmt.Errorf("division by zero")
		}
		r = af / bf
	case "%":
		if bf == 0 {
			return Value{}, fmt.Errorf("division by zero")
		}
		r = math.Mod(af, bf)
	default:
		return Value{}, fmt.Errorf("unknown arithmetic operator %q", op)
	}
	if math.IsInf(r, 0) || math.IsNaN(r) {
		return Value{}, fmt.Errorf("arithmetic %s: result exceeds the representable range", op)
	}
	return numericValue(r), nil
}

// numericValue wraps a float64 result as a Value whose raw
// representation is a json.Number. That matches how jq-produced
// numbers land in the session and keeps Value.Scalar() on a common
// rendering path: integer-valued results print without a trailing
// ".0".
func numericValue(x float64) Value {
	text := strconv.FormatFloat(x, 'f', -1, 64)
	return Value{v: json.Number(text), kind: semantics.OriginScalar}
}

func literalValueParts(text string, quoted bool) (Value, error) {
	if quoted {
		return StringValue(text), nil
	}
	switch text {
	case "true":
		return BoolValue(true), nil
	case "false":
		return BoolValue(false), nil
	case "null":
		return NullValue(), nil
	}
	if syntax.IsJSONNumber(text) {
		return Value{v: json.Number(text), kind: semantics.OriginScalar}, nil
	}
	if looksNumeric(text) {
		// Two distinguishable failures deserve different advice:
		// valid JSON grammar that overflows float64 (1e309) cannot
		// be fixed by quoting, while a malformed spelling (5s,
		// 2024-01-01) is usually an intended string.
		if json.Valid([]byte(text)) {
			return Value{}, fmt.Errorf("numeric literal %q exceeds the representable range", text)
		}
		return Value{}, fmt.Errorf("numeric literal %q is not a valid JSON number; quote it if you meant a string", text)
	}
	return StringValue(text), nil
}

func looksNumeric(text string) bool {
	if text == "" {
		return false
	}
	first := text[0]
	if first >= '0' && first <= '9' {
		return true
	}
	if (first == '-' || first == '+') && len(text) > 1 {
		second := text[1]
		return second >= '0' && second <= '9'
	}
	return false
}

// evalNotEmpty implements the not-empty unary predicate. "Empty"
// is applied uniformly under the Go zero-value convention -- null
// is empty, "" is empty, [] / nil-slice is empty, {} / nil-map is
// empty, numeric 0 is empty, false is empty -- so the predicate
// reads the same inline (`assert not-empty $xs`) and inside a
// matches block (`field: not-empty`).
func evalNotEmpty(operand Value, span source.Span) (Value, error) {
	if operand.IsNil() || operand.IsNull() {
		return BoolValue(false), nil
	}
	switch x := operand.Raw().(type) {
	case string:
		return BoolValue(x != ""), nil
	case []any:
		return BoolValue(len(x) > 0), nil
	case map[string]any:
		return BoolValue(len(x) > 0), nil
	case json.Number:
		f, ferr := x.Float64()
		if ferr != nil {
			return Value{}, syntax.SpanErrorf(span, "not-empty: %v", ferr)
		}
		return BoolValue(f != 0), nil
	case float64:
		return BoolValue(x != 0), nil
	case bool:
		return BoolValue(x), nil
	default:
		// Any carrier outside the documented vocabulary
		// (string / []any / map[string]any / json.Number /
		// float64 / bool / nil) is a misuse of the Value
		// API: ValueFromAny's doc lists those types, and
		// anything else is a programmer error. Fall back to
		// the scalar conversion so the diagnostic identifies
		// the unsupported carrier rather than silently
		// declaring it truthy.
		s, err := operand.Scalar()
		if err != nil {
			return Value{}, syntax.SpanErrorf(span, "not-empty: %v", err)
		}
		return BoolValue(s != ""), nil
	}
}

// evalSymbolicTextComparison compares two strings under the
// canonical symbol-form operator. Both operands have already been
// reduced to their scalar text by evalCompare; this function is the
// single textual-compare path for strings and (after the canonical
// "true"/"false" rendering) booleans.
func evalSymbolicTextComparison(op, left, right string) (Value, error) {
	var pass bool
	switch op {
	case "==":
		pass = left == right
	case "!=":
		pass = left != right
	case "<":
		pass = left < right
	case "<=":
		pass = left <= right
	case ">":
		pass = left > right
	case ">=":
		pass = left >= right
	default:
		return Value{}, fmt.Errorf("unknown textual operator %q", op)
	}
	return BoolValue(pass), nil
}

func evalNumericComparison(op, left, right string) (Value, error) {
	a, err := parseShellNumber(left, "left")
	if err != nil {
		return Value{}, err
	}

	b, err := parseShellNumber(right, "right")
	if err != nil {
		return Value{}, err
	}

	var cmp int
	if a.integral && b.integral {
		cmp = a.i.Cmp(b.i)
	} else {
		af, err := a.float64("left")
		if err != nil {
			return Value{}, err
		}

		bf, err := b.float64("right")
		if err != nil {
			return Value{}, err
		}

		switch {
		case af < bf:
			cmp = -1
		case af > bf:
			cmp = 1
		default:
			cmp = 0
		}
	}
	var pass bool
	switch op {
	case "==":
		pass = cmp == 0
	case "!=":
		pass = cmp != 0
	case "<":
		pass = cmp < 0
	case "<=":
		pass = cmp <= 0
	case ">":
		pass = cmp > 0
	case ">=":
		pass = cmp >= 0
	default:
		return Value{}, fmt.Errorf("unknown numeric operator %q", op)
	}
	return BoolValue(pass), nil
}

func isIdentStartByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentContinueByte(b byte) bool {
	return isIdentStartByte(b) || (b >= '0' && b <= '9')
}

// AsBool extracts a boolean from a Value. It succeeds when the
// underlying raw value is a Go bool, regardless of the semantics.OriginKind
// tag: comparison results carry semantics.OriginBool explicitly, while a
// path lookup that lands on a JSON boolean field arrives with
// kind semantics.OriginUnknown but raw type bool. Both should drive
// if/assert truthiness without forcing the caller to add a
// redundant "== true". Anything else returns a type error.
func AsBool(v Value) (bool, error) {
	if b, ok := v.Raw().(bool); ok {
		return b, nil
	}
	if v.Kind() == semantics.OriginBool {
		return false, fmt.Errorf("condition has boolean origin but non-boolean value %T", v.Raw())
	}
	return false, fmt.Errorf("condition is a %s; use a comparison like '$x == 5' or a check like 'not-empty $x' to produce a boolean", v.Kind())
}

// Interpreter for the executable IR.
//
// State per running unit lives on an *executor. Temps are stored in a
// slice of any so the same table can carry Values (from ir.Eval),
// argv lists (from ir.BuildArgs), and bind results (from ir.DispatchBind)
// without a tagged-union wrapper. Frame and defer-scope management
// reuse the shared Session / Env primitives so the interpreter owns
// control flow while the scope mechanics stay centralised.
//
// execInstr's switch handles the IR instruction set.

package runtime

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// Exec runs lp against env. Top-level defs in lp are registered into the
// session before any body instruction runs.
//
// Exec owns the outer job scope for one program run; defer scope handling
// lives in ir.EnterDeferScope and ir.RunDefers so nested scopes (a
// poll attempt inside the program body, a def call inside any of
// them) compose naturally. The trailing unwind drains any scopes left open
// by an error so cleanup still runs on failure.
func Exec(lp *ir.Program, env *Env) error {
	return withJobScope(env, func() error {
		return execProgram(lp, env, false)
	})
}

func execProgram(lp *ir.Program, env *Env, skipProgramScope bool) error {
	if lp == nil || lp.Body == nil {
		return nil
	}
	registerLoweredDefs(lp.Defs, env)
	ex := newExecutor(env, lp.NumTemps)
	ex.skipProgramScope = skipProgramScope
	runErr := ex.runUnit(lp.Body)
	// Frame an escaping bare *GuardFailure at its own source.Span so a
	// top-level bind failure cites the bind site directly.
	// Errors that already carry a frame (because they crossed a
	// ir.DispatchCommand / ir.DispatchBind on the way out) are
	// idempotent under syntax.FrameAt and pass through unchanged.
	if gf := (*GuardFailure)(nil); runErr != nil && errors.As(runErr, &gf) {
		var se *syntax.SyntaxError
		if !errors.As(runErr, &se) {
			runErr = syntax.FrameAt(gf.Span, runErr)
		}
	}

	ex.unwindOnExit()
	return runErr
}

// runLoweredDefCall runs a def whose body is the IR Blocks
// rooted at def.Entry. The caller (runDefCall) has
// already done arity checking and recursion-depth bookkeeping;
// this function owns frame, defer-scope, and return-slot
// management via the def's own ir.EnterFrame / ir.EnterDeferScope /
// ir.RunDefers / ir.ReturnValue instructions.
//
// Returns match runDefCall's contract: (returned value,
// hasReturn flag, local defer-failure count, error). The
// returned value is the def's published primary when a
// syntax.ReturnStmt fired; hasReturn=false means the body fell
// through without returning. localDeferFailures counts
// failures observed by ir.RunDefers with policy=def-local in
// THIS call only, so callDefAsBind can mark Rc failed on a
// failed def-local cleanup without folding in failures from
// nested calls.
func runLoweredDefCall(def *defValue, args []Arg, env *Env) (Value, bool, int, error) {
	ex := newExecutor(env, def.NumTemps)
	ex.inDef = true
	for i := range args {
		v, err := bindDefArg(def, i, args[i])
		if err != nil {
			return Value{}, false, 0, err
		}

		ex.temps[i] = v
	}
	err := ex.runUnit(def.Entry)
	ex.unwindOnExit()
	return ex.returnValue, ex.hasReturn, ex.localDeferFailures, err
}

// executor is the per-unit interpretation state. A unit is the
// program body or a def body; each gets its own executor because
// temps and frame state do not flow across unit boundaries.
type executor struct {
	env                *Env
	temps              []any
	bindArgs           map[ir.Temp][]Arg
	pendingGuardFail   *pendingGuardFail // set by ir.ApplyBind on guard fail, drained by ir.PropagateError
	loops              []*loopState
	scopes             []*[]deferEntry // saved env.defers, restored on ir.RunDefers
	polls              []*pollState
	returnValue        Value // populated by ir.ReturnValue; read by runLoweredDefCall
	hasReturn          bool
	localDeferFailures int // accumulated def-local defer failures observed by ir.RunDefers
	entryFrameDepth    int // Session.FrameDepth() at executor start; unwindOnExit pops back to here
	inDef              bool

	// skipProgramScope tells the ir.EnterDeferScope / ir.RunDefers
	// handlers to skip the body's program-level scope dance
	// when the caller already opened a long-lived scope (the
	// driver-owned program scope case). Poll-attempt and def-local
	// scopes still nest normally; only program-policy gets
	// the no-op treatment.
	skipProgramScope bool
}

// pendingGuardFail carries the payload ir.PropagateError needs to
// raise a *GuardFailure: Envelope from the failing bind, Primary
// name from the bind statement, and Args from the dispatch's argv.
type pendingGuardFail struct {
	Envelope Envelope
	Primary  string
	Args     []Arg
	Span     source.Span
}

type pollTimeoutError struct {
	Timeout   time.Duration
	Every     time.Duration
	Attempts  int
	LastRetry string
}

// Error renders the poll-timeout message, citing the configured
// timeout, the number of attempts, and the last retry reason when one
// was recorded.
func (e *pollTimeoutError) Error() string {
	if e == nil {
		return "poll timed out"
	}
	if e.LastRetry == "" {
		return fmt.Sprintf("poll timed out after %s across %d attempt(s)", e.Timeout, e.Attempts)
	}
	return fmt.Sprintf("poll timed out after %s across %d attempt(s): %s", e.Timeout, e.Attempts, e.LastRetry)
}

// pollRetrySignal is an internal control-flow carrier used when a
// helper def executes `retry` while a caller-owned poll is active.
// The helper executor cannot jump into the caller's IR blocks
// directly, so it raises this signal and lets the enclosing poll
// executor translate it into continueOrTimeout. LastRetry is
// eagerly rendered to string here because the helper and caller do
// not share temp slots or value storage.
type pollRetrySignal struct {
	LastRetry string
}

// Error renders the retry signal's message, appending the last retry
// reason when one was recorded.
func (s *pollRetrySignal) Error() string {
	if s == nil || s.LastRetry == "" {
		return "poll retry"
	}
	return "poll retry: " + s.LastRetry
}

// pollState tracks one active poll region. The interpreter
// pushes a state on ir.BeginPoll and pops it when control
// reaches OnSuccess or when timeout is rendered.
type pollState struct {
	Timeout      time.Duration
	Every        time.Duration
	Deadline     time.Time
	Attempt      *ir.BasicBlock
	OnSuccess    *ir.BasicBlock
	OnTimeout    *ir.BasicBlock
	StartTime    time.Time
	AttemptCount int
	LastRetry    string
	Span         source.Span

	// Attempt-entry depth snapshots. The retry path (lexical
	// ir.RetryPoll) drains executor state back to these depths
	// before re-entering the Attempt block; without them the iter
	// frames, defer scopes, and loop states opened during a
	// failed attempt would survive into the retry.
	AttemptFrameDepth int
	AttemptScopeDepth int
	AttemptLoopDepth  int

	// Failed is set once a transfer to OnTimeout has been made
	// so the timeout block can render once and escape cleanly.
	Failed   bool
	TimedOut bool
}

// loopState tracks one active foreach (or future foreach-collect)
// loop. The interpreter pushes a loopState on ir.ForEach, mutates Index
// on each iteration, and pops on natural exhaustion or ir.ExitLoop.
// EntryFrameDepth records Session frame count at loop start so
// ir.ForEachContinue and ir.ExitLoop can close every frame opened during
// an iteration -- the iter frame itself plus any intermediate ones
// (if-branch, nested foreach, etc.) -- without the lowerer having
// to emit explicit ir.ExitFrame markers along every early-exit path.
type loopState struct {
	List            []any
	Index           int
	Names           []string
	Body            *ir.BasicBlock
	Exit            *ir.BasicBlock
	EntryFrameDepth int
	Origin          Value // wrapped list value; IndexValue preserves origin
	Span            source.Span

	// Collect-only fields. Populated for bind-collect loops
	// (ir.ForEachCollect); zero values otherwise. The accumulators
	// preserve the documented list result shape: Results holds one
	// outcome per produced iteration, Values holds successful
	// unwrapped values, and CollectOK is true iff every produced
	// iteration succeeded.
	Collect   bool
	Primary   string
	Guard     bool
	Results   []Value
	Values    []Value
	CollectOK bool
}

func newExecutor(env *Env, numTemps int) *executor {
	return &executor{
		env:             env,
		bindArgs:        make(map[ir.Temp][]Arg),
		temps:           make([]any, numTemps),
		entryFrameDepth: env.Session.FrameDepth(),
	}
}

// registerLoweredDefs installs the program's top-level defs into
// the session before any body instruction executes. Later defs
// overwrite earlier ones in source order.
func registerLoweredDefs(defs []*ir.Def, env *Env) {
	for _, def := range defs {
		env.Session.setDef(&defValue{
			Name:      def.Name,
			Params:    def.Params,
			HasReturn: def.HasReturn,
			Entry:     def.Entry,
			NumTemps:  def.NumTemps,
			Span:      def.Span,
		})
		traceRendered(env, def.Span.Pos, defTraceText(def.Name, def.Params))
	}
}

// unwindOnExit restores the session and the env's defer-scope
// pointer to what this executor inherited. Used by both
// Exec and runLoweredDefCall after runUnit returns; an
// error escape can leave frames, defer scopes, and loop states
// open mid-iteration, so explicit drain logic is required.
//
// The pointer this executor inherited (env.defers as it stood
// at the matching ir.EnterDeferScope, or nil if none ran) is left
// in place. The program driver opens one long-lived scope via
// withDeferScope and threads it through repeated
// execInScope calls in the package tests; touching
// env.defers here would destroy that outer scope after the
// first source unit.
func (ex *executor) unwindOnExit() {
	// Loop states own nothing the frame / scope drain below
	// will not also catch; clearing the slice is enough.
	ex.loops = ex.loops[:0]
	for range ex.polls {
		ex.env.exitPoll()
	}
	ex.polls = nil
	for len(ex.scopes) > 0 {
		if ex.env.defers != nil {
			runDefers(ex.env, *ex.env.defers)
		}
		n := len(ex.scopes)
		ex.env.defers = ex.scopes[n-1]
		ex.scopes = ex.scopes[:n-1]
	}
	for ex.env.Session.FrameDepth() > ex.entryFrameDepth {
		ex.env.Session.PopFrame()
	}
}

// runUnit walks blocks starting at entry until a terminator halts
// the unit or transfers control off-graph. Each instruction is
// dispatched through execInstr, which reports whether the unit
// should halt and which block to enter next.
func (ex *executor) runUnit(entry *ir.BasicBlock) error {
	cur := entry
	for cur != nil {
		if n := len(ex.polls); n > 0 {
			top := ex.polls[n-1]
			if cur == top.OnSuccess && !top.Failed {
				traceNote(ex.env, top.Span, "poll success")
				ex.env.exitPoll()
				ex.polls = ex.polls[:n-1]
			}
		}
		next, halt, err := ex.runBlock(cur)
		if err != nil {
			return err
		}

		if halt {
			return nil
		}

		cur = next
	}
	return nil
}

// continueOrTimeout advances a poll after an explicit retry:
// drain attempt-local scopes and either re-enter Attempt or
// transfer to OnTimeout when the deadline has elapsed.
func (ex *executor) continueOrTimeout(top *pollState) (*ir.BasicBlock, error) {
	ex.drainToAttemptStart(top)
	if !ex.env.now().Before(top.Deadline) {
		top.Failed = true
		top.TimedOut = true
		return top.OnTimeout, nil
	}
	top.AttemptCount++
	traceNote(ex.env, top.Span, "poll retry")
	if top.Every > 0 {
		ex.env.sleep(top.Every)
	}
	return top.Attempt, nil
}

func renderPollRetryMessage(v Value) string {
	if v.IsNil() {
		return ""
	}
	if v.IsScalar() {
		if raw := v.Raw(); raw != nil {
			return fmt.Sprint(raw)
		}
		if v.IsNull() {
			return "null"
		}
	}
	rendered, err := RenderCompact(v)
	if err != nil {
		return "<unrenderable retry message>"
	}

	return rendered
}

// drainToAttemptStart closes every loop, defer scope, and frame
// opened during the attempt so a retry re-entering the attempt
// block starts from a clean slate. The attempt block's own
// ir.EnterFrame and ir.EnterDeferScope re-open them when control
// returns.
func (ex *executor) drainToAttemptStart(state *pollState) {
	for len(ex.loops) > state.AttemptLoopDepth {
		ex.loops = ex.loops[:len(ex.loops)-1]
	}
	for len(ex.scopes) > state.AttemptScopeDepth {
		if ex.env.defers != nil {
			runDefers(ex.env, *ex.env.defers)
		}
		n := len(ex.scopes)
		ex.env.defers = ex.scopes[n-1]
		ex.scopes = ex.scopes[:n-1]
	}
	for ex.env.Session.FrameDepth() > state.AttemptFrameDepth {
		ex.env.Session.PopFrame()
	}
}

// runBlock executes each instruction in blk in order. Non-terminator
// instructions return (nil, false, nil) and runBlock continues to
// the next instruction. Terminators return either a non-nil next
// block (ir.Jump, ir.Branch on later slices) or halt=true (ir.Stop) or an
// error (ir.PropagateError, ir.Fail). The block's last instruction is
// always a terminator; non-terminators that appear in tail position
// would leave next=nil and halt=false, which runUnit treats as a
// silent halt -- the lowerer's invariant prevents this in practice.
func (ex *executor) runBlock(blk *ir.BasicBlock) (*ir.BasicBlock, bool, error) {
	for _, ins := range blk.Instrs {
		next, halt, err := ex.execInstr(ins)
		if err != nil {
			return nil, false, err
		}
		if halt {
			return nil, true, nil
		}
		if next != nil {
			return next, false, nil
		}
	}
	return nil, false, nil
}

// execInstr dispatches one instruction. Returns (next, halt, err):
// next is the block to enter when the instruction transfers
// control; halt signals ir.Stop; err signals a propagated failure or a
// not-yet-supported construct.
func (ex *executor) execInstr(ins ir.Instr) (*ir.BasicBlock, bool, error) {
	switch v := ins.(type) {
	case *ir.EnterFrame:
		ex.env.Session.PushFrame()
		return nil, false, nil
	case *ir.ExitFrame:
		ex.env.Session.PopFrame()
		return nil, false, nil
	case *ir.EnterDeferScope:
		// Save the current scope so the matching ir.RunDefers can
		// restore it, then install a fresh stack. The IR opens
		// at least one scope (the program body) and may nest
		// (one extra per poll attempt, future def call).
		// In skipProgramScope mode the program-level open is a
		// no-op: the driver's outer scope already spans the
		// whole program, so the body should append to it rather
		// than opening a fresh stack that gets drained too early.
		if v.Kind == ir.DeferScopeProgram && ex.skipProgramScope {
			return nil, false, nil
		}
		ex.scopes = append(ex.scopes, ex.env.defers)
		fresh := []deferEntry{}
		ex.env.defers = &fresh
		return nil, false, nil
	case *ir.RegisterDefer:
		return nil, false, ex.execRegisterDefer(v)
	case *ir.RegisterDef:
		// Install a defValue whose Entry / NumTemps
		// point at the IR for the def body. runDefCall picks that
		// lane and drives the body via runUnit.
		ex.env.Session.setDef(&defValue{
			Name:      v.Def.Name,
			Params:    v.Def.Params,
			HasReturn: v.Def.HasReturn,
			Entry:     v.Def.Entry,
			NumTemps:  v.Def.NumTemps,
			Span:      v.Def.Span,
		})
		traceRendered(ex.env, v.Def.Span.Pos, defTraceText(v.Def.Name, v.Def.Params))
		return nil, false, nil
	case *ir.RunDefers:
		// Match the ir.EnterDeferScope skip: when the driver owns
		// the program-level scope, the body's program-
		// policy ir.RunDefers must not drain it; the outer scope
		// fires at program exit. Other policies (def-local,
		// attempt-fatal) still drain their own pushed scopes.
		if v.Policy == ir.RunDefersProgram && ex.skipProgramScope {
			return nil, false, nil
		}
		var failed int
		if ex.env.defers != nil {
			failed = runDefers(ex.env, *ex.env.defers)
		}
		if v.Policy == ir.RunDefersDefLocal {
			ex.localDeferFailures += failed
		}
		if n := len(ex.scopes); n > 0 {
			ex.env.defers = ex.scopes[n-1]
			ex.scopes = ex.scopes[:n-1]
		} else {
			ex.env.defers = nil
		}
		// Attempt-local cleanup failure is fatal to the enclosing
		// poll: a defer registered during an attempt body returned
		// a non-ok envelope while unwinding, and retrying compounds
		// leaks rather than reaching success. The lexical case
		// finds the poll on this executor's stack; the helper
		// case runs on its own executor (ex.polls empty) but is
		// still part of the caller's attempt -- env.InPoll() is
		// true and emitRetryStmt is the only emitter of
		// AttemptFatal inside a helper, so the failure has the
		// same meaning. Returning a regular error (not a retry
		// signal) lets the caller's DispatchBind propagate the
		// fatal diagnostic instead of treating the retry as in-
		// progress.
		if v.Policy == ir.RunDefersAttemptFatal && failed > 0 {
			if len(ex.polls) > 0 {
				top := ex.polls[len(ex.polls)-1]
				top.Failed = true
				traceNote(ex.env, top.Span, "poll fatal")
				return nil, false, syntax.SpanErrorf(top.Span, "poll: attempt-local defer failed during unwind (%d cleanup failure(s)); halting the construct", failed)
			}
			if ex.inDef && ex.env.InPoll() {
				return nil, false, syntax.SpanErrorf(v.Span, "poll: helper defer failed during retry unwind (%d cleanup failure(s)); halting the construct", failed)
			}
		}
		return nil, false, nil
	case *ir.Eval:
		val, err := evalExpr(v.Expr, ex.env)
		if err != nil {
			return nil, false, err
		}

		ex.temps[v.Dst] = val
		return nil, false, nil
	case *ir.BuildArgs:
		args, err := evalArgs(v.Args, ex.env)
		if err != nil {
			return nil, false, err
		}

		ex.temps[v.Dst] = args
		return nil, false, nil
	case *ir.DispatchCommand:
		args, err := ex.argvAt(v.Argv, v.Span)
		if err != nil {
			return nil, false, err
		}

		if v.Trace && ex.env.Trace != nil {
			ex.env.Trace(v.Span.Pos, renderArgvTrace(args))
		}
		// Route through the policy-carried dispatcher so the
		// lowered form, the executor, and the shared shell helpers
		// all agree on command-position head resolution.
		if err := dispatchCommandByPolicy(v.Policy, args, v.Span.Pos, v.Span, ex.env); err != nil {
			var retry *pollRetrySignal
			if errors.As(err, &retry) && len(ex.polls) > 0 {
				top := ex.polls[len(ex.polls)-1]
				top.LastRetry = retry.LastRetry
				next, rerr := ex.continueOrTimeout(top)
				if rerr != nil {
					return nil, false, rerr
				}
				return next, false, nil
			}
			// Inside def bodies a command-position def call
			// propagates its error raw so outer bind/program
			// boundaries choose the framing span; at the top level
			// the statement boundary itself is the frame site.
			if ex.inDef {
				return nil, false, err
			}
			return nil, false, syntax.FrameAt(v.Span, err)
		}
		return nil, false, nil
	case *ir.BindName:
		val, err := ex.valueAt(v.Src, v.Span)
		if err != nil {
			return nil, false, err
		}

		if v.TracePrefix != "" && ex.env.Trace != nil {
			rendered, rerr := RenderCompact(val)
			if rerr != nil {
				rendered = fmt.Sprintf("<unrenderable %s>", val.Kind())
			}
			ex.env.Trace(v.Span.Pos, v.TracePrefix+rendered)
		}
		ex.env.Session.Set(v.Name, val)
		return nil, false, nil
	case *ir.DispatchBind:
		args, err := ex.argvAt(v.Argv, v.Span)
		if err != nil {
			return nil, false, err
		}

		if v.TraceHeader != "" && ex.env.Trace != nil {
			ex.env.Trace(v.Span.Pos, fmt.Sprintf("%s <- %s", v.TraceHeader, renderArgvTrace(args)))
		}
		if ex.env.ExecBind == nil {
			return nil, false, syntax.SpanErrorf(v.Span, "no bind handler installed")
		}
		callPos := v.CallPos
		if callPos.Line == 0 {
			// Fall back to the statement span when CallPos is unset.
			callPos = v.Span.Pos
		}
		result, err := dispatchBindByPolicy(v.Policy, args, callPos, v.Span, ex.env)
		if err != nil {
			var retry *pollRetrySignal
			if errors.As(err, &retry) && len(ex.polls) > 0 {
				top := ex.polls[len(ex.polls)-1]
				top.LastRetry = retry.LastRetry
				next, rerr := ex.continueOrTimeout(top)
				if rerr != nil {
					return nil, false, rerr
				}
				return next, false, nil
			}
			// Frame at the bind instruction's span so an error
			// escaping a bind-position callee carries the
			// caller's stmt span, mirroring evalBindStmt's
			// syntax.FrameAt(s.Span, err) on the AST side.
			return nil, false, syntax.FrameAt(v.Span, err)
		}

		ex.bindArgs[v.Dst] = args
		ex.temps[v.Dst] = result
		return nil, false, nil
	case *ir.ApplyBind:
		res, ok := ex.temps[v.Src].(BindResult)
		if !ok {
			return nil, false, syntax.SpanErrorf(v.Span, "exec: t%d expected BindResult, got %T", v.Src, ex.temps[v.Src])
		}

		if v.Guard && !res.Rc.OK() {
			if v.OnFail == nil {
				return nil, false, syntax.SpanErrorf(v.Span, "exec: guard fail has no OnFail block")
			}
			// Stash the failure payload so the OnFail block's
			// ir.PropagateError can raise a *GuardFailure that
			// matches applyBindResult's shape. Argv is the same
			// temp ir.DispatchBind read; re-reading it here keeps
			// the engines in sync on Args even when the original
			// dispatch happened many instructions ago.
			args, _ := ex.temps[v.Argv].([]Arg)
			ex.pendingGuardFail = &pendingGuardFail{
				Envelope: res.Rc,
				Primary:  v.Target,
				Args:     args,
				Span:     v.Span,
			}
			return v.OnFail, false, nil
		}
		if v.Target != "" && v.Target != "_" {
			if v.Guard {
				ex.env.Session.Set(v.Target, res.Primary)
			} else {
				ex.env.Session.Set(v.Target, ValueFromOutcome(res))
			}
		}
		return nil, false, nil
	case *ir.BindDestructure:
		val, err := ex.valueAt(v.Src, v.Span)
		if err != nil {
			return nil, false, err
		}

		if err := ex.bindDestructure(v.Names, val, v.Span); err != nil {
			return nil, false, err
		}

		if v.Trace && ex.env.Trace != nil {
			ex.emitDestructureTrace(v.Names, val, v.Span)
		}
		return nil, false, nil
	case *ir.BuildEnvelope:
		// ir.BuildEnvelope.Err carries a diagnostic string; it lands
		// in Envelope.Stderr because Envelope reserves Stderr for
		// the human-readable failure reason (see envelope.go).
		env := Envelope{ExitCode: v.ExitCode, Stderr: v.Err}
		ex.temps[v.Dst] = ValueFromEnvelope(env)
		return nil, false, nil
	case *ir.EmitBindResult:
		// Bind-result emission is meaningful only for callable
		// units (defs). At the program
		// body level there is no caller frame to receive the
		// result; treat it as a no-op so the lowerer can emit it
		// unconditionally in unreachable trailing blocks.
		return nil, false, nil
	case *ir.EmitResult:
		val, err := ex.valueAt(v.Src, v.Span)
		if err != nil {
			return nil, false, err
		}

		if v.Trace {
			traceValue(ex.env, v.Span, "expr ", val)
		}
		if ex.env.PrintResult == nil {
			return nil, false, nil
		}
		return nil, false, ex.env.PrintResult(val)
	case *ir.TraceNote:
		traceNote(ex.env, v.Span, v.Msg)
		return nil, false, nil
	case *ir.Branch:
		val, err := ex.valueAt(v.Cond, v.Span)
		if err != nil {
			return nil, false, err
		}

		cond, err := AsBool(val)
		if err != nil {
			return nil, false, syntax.SpanErrorf(v.Span, "exec: branch condition: %v", err)
		}

		if cond {
			return v.True, false, nil
		}
		return v.False, false, nil
	case *ir.Jump:
		return v.Target, false, nil
	case *ir.Stop:
		return nil, true, nil
	case *ir.ReturnValue:
		val, err := ex.valueAt(v.Src, v.Span)
		if err != nil {
			return nil, false, err
		}

		if v.Trace && ex.env.Trace != nil {
			rendered, rerr := RenderCompact(val)
			if rerr != nil {
				rendered = fmt.Sprintf("<unrenderable %s>", val.Kind())
			}
			ex.env.Trace(v.Span.Pos, fmt.Sprintf("return %s", rendered))
		}
		ex.returnValue = val
		ex.hasReturn = true
		return v.To, false, nil
	case *ir.PropagateError:
		if ex.pendingGuardFail != nil {
			pf := ex.pendingGuardFail
			ex.pendingGuardFail = nil
			// Raise *GuardFailure unframed so it can escape a
			// nested def call without triggering
			// decorateDefError; the outer ir.DispatchCommand /
			// ir.DispatchBind instructions apply the matching
			// syntax.FrameAt when this error bubbles out of a def
			// call.
			return nil, false, &GuardFailure{
				Span:     pf.Span,
				Primary:  pf.Primary,
				Args:     pf.Args,
				Envelope: pf.Envelope,
			}
		}
		if n := len(ex.polls); n > 0 {
			top := ex.polls[n-1]
			if top.Failed && top.TimedOut {
				if ex.env.RenderPollFailure != nil {
					ex.env.RenderPollFailure(v.Span, top.Timeout, top.Every, top.AttemptCount, top.LastRetry)
				}
				timeoutErr := &pollTimeoutError{
					Timeout:   top.Timeout,
					Every:     top.Every,
					Attempts:  top.AttemptCount,
					LastRetry: top.LastRetry,
				}
				ex.env.exitPoll()
				ex.polls = ex.polls[:n-1]
				if ex.inDef {
					return nil, false, timeoutErr
				}
				return nil, false, syntax.FrameAt(v.Span, timeoutErr)
			}
		}
		return nil, false, syntax.SpanErrorf(v.Span, "propagated error")
	case *ir.PropagateGuardFailure:
		return nil, false, &GuardFailure{
			Span:    v.Span,
			Primary: v.Primary,
			Args:    []Arg{WordArg{Text: v.Head, Span: v.ArgSpan}},
			Envelope: Envelope{
				ExitCode: v.ExitCode,
				Stdout:   v.Stdout,
				Stderr:   v.Stderr,
				Killed:   v.Killed,
				Signal:   v.Signal,
				HasPID:   v.HasPID,
				PID:      v.PID,
			},
		}
	case *ir.Fail:
		return nil, false, syntax.SpanErrorf(v.Span, "%s", v.Msg)
	case *ir.Assert:
		if ex.env.ExecAssert == nil {
			return nil, false, syntax.SpanErrorf(v.Span, "assert: handler not installed")
		}
		return nil, false, ex.env.ExecAssert(v, ex.env)
	case *ir.ForEach:
		return ex.execForEach(v)
	case *ir.ForEachContinue:
		return ex.execForEachContinue(v.Span)
	case *ir.ExitLoop:
		return ex.execExitLoop(v.Span)
	case *ir.BeginPoll:
		now := ex.env.now()
		state := &pollState{
			Timeout:           v.Timeout,
			Every:             v.Every,
			Deadline:          now.Add(v.Timeout),
			Attempt:           v.Attempt,
			OnSuccess:         v.OnSuccess,
			OnTimeout:         v.OnTimeout,
			StartTime:         now,
			AttemptCount:      1,
			Span:              v.Span,
			AttemptFrameDepth: ex.env.Session.FrameDepth(),
			AttemptScopeDepth: len(ex.scopes),
			AttemptLoopDepth:  len(ex.loops),
		}
		ex.env.enterPoll()
		ex.polls = append(ex.polls, state)
		return v.Attempt, false, nil
	case *ir.RetryPoll:
		lastRetry := ""
		if v.Message != nil {
			val, err := ex.valueAt(*v.Message, v.Span)
			if err != nil {
				return nil, false, err
			}

			lastRetry = renderPollRetryMessage(val)
		}
		if len(ex.polls) == 0 {
			if ex.inDef && ex.env.InPoll() {
				return nil, false, &pollRetrySignal{LastRetry: lastRetry}
			}
			return nil, false, syntax.SpanErrorf(v.Span, "retry: outside any poll")
		}
		top := ex.polls[len(ex.polls)-1]
		top.LastRetry = lastRetry
		next, err := ex.continueOrTimeout(top)
		if err != nil {
			return nil, false, err
		}

		return next, false, nil
	case *ir.ForEachCollect:
		return ex.execForEachCollect(v)
	case *ir.CollectProduce:
		return ex.execCollectProduce(v)
	default:
		return nil, false, fmt.Errorf("exec: %T not yet supported in this slice", ins)
	}
}

// execRegisterDefer queues an argv (built earlier via ir.BuildArgs)
// onto the active defer scope. The argv lives in temps as []Arg so
// the registration shape matches what downstream runDefers
// expects.
func (ex *executor) execRegisterDefer(v *ir.RegisterDefer) error {
	if ex.env.defers == nil {
		return syntax.SpanErrorf(v.Span, "defer outside any defer scope")
	}
	args, err := ex.argvAt(v.Argv, v.Span)
	if err != nil {
		return err
	}

	if v.Trace && ex.env.Trace != nil {
		ex.env.Trace(v.Span.Pos, "defer "+renderArgvTrace(args))
	}
	*ex.env.defers = append(*ex.env.defers, deferEntry{
		Span:   v.Span,
		Args:   args,
		policy: v.Policy,
		trace:  ex.env.Trace,
	})
	return nil
}

// execForEach opens a foreach loop: read the list, push a
// loopState, and either set up the first iteration's iter frame
// and name bindings or transfer to Exit when the list is empty.
// The iter frame is pushed here rather than in the body block so
// every iter-bound name is local to its iteration; the matching
// pop happens in execForEachContinue (or execExitLoop).
func (ex *executor) execForEach(v *ir.ForEach) (*ir.BasicBlock, bool, error) {
	val, err := ex.valueAt(v.List, v.Span)
	if err != nil {
		return nil, false, err
	}

	if val.IsNil() {
		return nil, false, syntax.SpanErrorf(v.Span, "foreach: list expression is null")
	}
	raw, ok := val.Raw().([]any)
	if !ok {
		return nil, false, syntax.SpanErrorf(v.Span, "foreach: expected a list, got %s", val.Kind())
	}

	state := &loopState{
		List:            raw,
		Index:           0,
		Names:           v.Names,
		Body:            v.Body,
		Exit:            v.Exit,
		EntryFrameDepth: ex.env.Session.FrameDepth(),
		Origin:          val,
		Span:            v.Span,
	}
	if len(raw) == 0 {
		return state.Exit, false, nil
	}
	ex.loops = append(ex.loops, state)
	if err := ex.beginIteration(state); err != nil {
		return nil, false, err
	}

	return state.Body, false, nil
}

// beginIteration pushes a fresh iter frame and binds Names against
// state.List[state.Index]. Used for the first iteration and after
// each ir.ForEachContinue advance.
func (ex *executor) beginIteration(state *loopState) error {
	ex.env.Session.PushFrame()
	elem := state.Origin.IndexValue(state.Index)
	if err := ex.bindForEachNames(state, elem); err != nil {
		return err
	}

	if ex.env.Trace != nil {
		ex.emitForEachTrace(state, elem)
	}
	return nil
}

// bindForEachNames installs the iter element into the iter frame:
// single-var form binds verbatim; multi-var form destructures the
// element as a list of matching length; "_" entries discard.
func (ex *executor) bindForEachNames(state *loopState, elem Value) error {
	if len(state.Names) == 1 {
		if state.Names[0] != "_" {
			ex.env.Session.Set(state.Names[0], elem)
		}
		return nil
	}
	sub, ok := elem.Raw().([]any)
	if !ok {
		return syntax.SpanErrorf(state.Span, "foreach: element %d is not a list, cannot destructure into %d names", state.Index, len(state.Names))
	}

	if len(sub) != len(state.Names) {
		return syntax.SpanErrorf(state.Span, "foreach: element %d has %d sub-elements, cannot destructure into %d names", state.Index, len(sub), len(state.Names))
	}
	for j, name := range state.Names {
		if name == "_" {
			continue
		}
		ex.env.Session.Set(name, elem.IndexValue(j))
	}
	return nil
}

func (ex *executor) emitForEachTrace(state *loopState, elem Value) {
	if len(state.Names) == 1 {
		rendered, err := RenderCompact(elem)
		if err != nil {
			rendered = fmt.Sprintf("<unrenderable %T>", state.List[state.Index])
		}
		ex.env.Trace(state.Span.Pos, fmt.Sprintf("foreach %s = %s", state.Names[0], rendered))
		return
	}
	parts := make([]string, 0, len(state.Names))
	for j, name := range state.Names {
		sub := elem.IndexValue(j)
		rendered, err := RenderCompact(sub)
		if err != nil {
			rendered = fmt.Sprintf("<unrenderable %T>", sub.Raw())
		}
		parts = append(parts, fmt.Sprintf("%s=%s", name, rendered))
	}
	ex.env.Trace(state.Span.Pos, "foreach "+strings.Join(parts, " "))
}

func (ex *executor) emitDestructureTrace(names []string, val Value, sp source.Span) {
	parts := make([]string, 0, len(names))
	for j, name := range names {
		rendered, err := RenderCompact(val.IndexValue(j))
		if err != nil {
			rendered = fmt.Sprintf("<unrenderable %T>", val.Raw())
		}
		parts = append(parts, fmt.Sprintf("%s=%s", name, rendered))
	}
	ex.env.Trace(sp.Pos, "let "+strings.Join(parts, " "))
}

// execForEachContinue closes every frame opened during the current
// iteration (the iter frame plus any nested ones the body left
// open via a continue mid-block), advances the index, and either
// re-enters the body with a fresh iter frame and bindings or
// transfers to the loop's Exit when the list is exhausted. Inside
// a bind-collect, this is the "continue" semantic: the
// iteration's producer value is not accumulated.
func (ex *executor) execForEachContinue(sp source.Span) (*ir.BasicBlock, bool, error) {
	if len(ex.loops) == 0 {
		return nil, false, syntax.SpanErrorf(sp, "foreach-continue: outside any loop")
	}
	state := ex.loops[len(ex.loops)-1]
	return ex.advanceLoop(state)
}

// execExitLoop ends the innermost foreach via break: close every
// frame opened during the iteration and transfer to the loop's
// Exit. Unlike ir.ForEachContinue it does not advance the index.
// Inside a bind-collect, break also publishes the partial
// collection so the bound name carries every accumulated value
// from before the break.
func (ex *executor) execExitLoop(sp source.Span) (*ir.BasicBlock, bool, error) {
	if len(ex.loops) == 0 {
		return nil, false, syntax.SpanErrorf(sp, "exit-loop: outside any loop")
	}
	state := ex.loops[len(ex.loops)-1]
	ex.popToDepth(state.EntryFrameDepth)
	ex.loops = ex.loops[:len(ex.loops)-1]
	if state.Collect {
		ex.finaliseCollect(state)
	}
	return state.Exit, false, nil
}

// execForEachCollect opens a bind-collect loop: ir.ForEach semantics
// for iteration plus per-iteration accumulators for the primary
// and (optionally) the rc envelope. The body's trailing
// ir.CollectProduce terminator appends each iteration's bind result
// into the accumulators; natural exhaustion, continue, or break
// all route through finaliseCollect so the bound names carry
// whatever was accumulated before the loop ended.
func (ex *executor) execForEachCollect(v *ir.ForEachCollect) (*ir.BasicBlock, bool, error) {
	val, err := ex.valueAt(v.List, v.Span)
	if err != nil {
		return nil, false, err
	}

	if val.IsNil() {
		return nil, false, syntax.SpanErrorf(v.Span, "bind-collect: list expression is null")
	}
	raw, ok := val.Raw().([]any)
	if !ok {
		return nil, false, syntax.SpanErrorf(v.Span, "bind-collect: expected a list, got %s", val.Kind())
	}

	state := &loopState{
		List:            raw,
		Index:           0,
		Names:           v.Names,
		Body:            v.Body,
		Exit:            v.Exit,
		EntryFrameDepth: ex.env.Session.FrameDepth(),
		Origin:          val,
		Span:            v.Span,
		Collect:         true,
		Primary:         v.Target,
		Guard:           v.Guard,
		CollectOK:       true,
	}
	if len(raw) == 0 {
		ex.finaliseCollect(state)
		return state.Exit, false, nil
	}
	ex.loops = append(ex.loops, state)
	if err := ex.beginIteration(state); err != nil {
		return nil, false, err
	}

	return state.Body, false, nil
}

// execCollectProduce is the per-iteration terminator inside a
// bind-collect body. It pulls the BindResult that the trailing
// ir.DispatchBind populated, applies the guard policy (halt with
// GuardFailure on a non-ok envelope when guarded), accumulates
// the primary value (and rc envelope, when bound), and then
// advances the loop in the same way as a natural ir.ForEachContinue.
func (ex *executor) execCollectProduce(v *ir.CollectProduce) (*ir.BasicBlock, bool, error) {
	if len(ex.loops) == 0 {
		return nil, false, syntax.SpanErrorf(v.Span, "collect-produce: outside any loop")
	}
	state := ex.loops[len(ex.loops)-1]
	if !state.Collect {
		return nil, false, syntax.SpanErrorf(v.Span, "collect-produce: not in a bind-collect")
	}
	res, ok := ex.temps[v.Result].(BindResult)
	if !ok {
		return nil, false, syntax.SpanErrorf(v.Span, "collect-produce: t%d expected BindResult, got %T", v.Result, ex.temps[v.Result])
	}

	outcome := ValueFromOutcome(res)
	state.Results = append(state.Results, outcome)
	if res.Rc.OK() {
		state.Values = append(state.Values, res.Primary)
	} else {
		state.CollectOK = false
	}
	if state.Guard && !res.Rc.OK() {
		gf := &GuardFailure{Span: v.Span, Primary: state.Primary, Args: ex.bindArgs[v.Result], Envelope: res.Rc}
		if ex.inDef {
			return nil, false, gf
		}
		frame := v.FrameSpan
		if frame.Pos.Line == 0 {
			// Fall back to the statement span when FrameSpan is unset.
			frame = v.Span
		}
		return nil, false, syntax.FrameAt(frame, gf)
	}
	return ex.advanceLoop(state)
}

// advanceLoop is the shared "advance the iterator and either
// re-enter the body or finalise" path used by ir.ForEachContinue and
// ir.CollectProduce. Bind-collect natural exhaustion publishes the
// accumulators; plain foreach exhaustion just transfers to Exit.
func (ex *executor) advanceLoop(state *loopState) (*ir.BasicBlock, bool, error) {
	ex.popToDepth(state.EntryFrameDepth)
	state.Index++
	if state.Index >= len(state.List) {
		ex.loops = ex.loops[:len(ex.loops)-1]
		if state.Collect {
			ex.finaliseCollect(state)
		}
		return state.Exit, false, nil
	}
	if err := ex.beginIteration(state); err != nil {
		return nil, false, err
	}

	return state.Body, false, nil
}

// finaliseCollect publishes the accumulated primary and rc lists
// under the names recorded on the loopState. rc is wrapped via
// ValueFromAny over its slice of envelope mirrors, primary
// likewise but with an optional per-element origin slice so
// IndexValue on the bound list reconstructs each element's
// typed Value at later access sites.
func (ex *executor) finaliseCollect(state *loopState) {
	if state.Primary != "" && state.Primary != "_" {
		if state.Guard {
			ex.env.Session.Set(state.Primary, valueList(state.Values))
		} else {
			ex.env.Session.Set(state.Primary, ValueFromCollectOutcome(state.CollectOK, state.Results, state.Values))
		}
	}
}

// popToDepth pops frames from the session until FrameDepth equals
// target. Used by the loop terminators to clean up everything
// opened during one iteration in one shot.
func (ex *executor) popToDepth(target int) {
	for ex.env.Session.FrameDepth() > target {
		ex.env.Session.PopFrame()
	}
}

// bindDestructure binds a name list against a list Value: each
// non-"_" name binds to the corresponding element, "_" entries
// discard, and a length mismatch is a runtime error. The list
// contents are pulled via the Value API so structured-value origin
// information stays intact for downstream path accesses.
func (ex *executor) bindDestructure(names []string, val Value, sp source.Span) error {
	if val.IsNil() {
		return syntax.SpanErrorf(sp, "let: destructure RHS produced no result")
	}
	raw, ok := val.Raw().([]any)
	if !ok {
		return syntax.SpanErrorf(sp, "let: destructure RHS is not a list, cannot bind %d names", len(names))
	}

	if len(raw) != len(names) {
		return syntax.SpanErrorf(sp, "let: destructure RHS has %d elements, cannot bind %d names", len(raw), len(names))
	}
	for i, name := range names {
		if name == "" || name == "_" {
			continue
		}
		ex.env.Session.Set(name, val.IndexValue(i))
	}
	return nil
}

// argvAt reads temps[t] expecting an argv ([]Arg). Returns a
// descriptive error if the slot holds the wrong type, which can only
// happen if the lowerer emitted instructions in the wrong order.
func (ex *executor) argvAt(t ir.Temp, sp source.Span) ([]Arg, error) {
	v := ex.temps[t]
	args, ok := v.([]Arg)
	if !ok {
		return nil, syntax.SpanErrorf(sp, "exec: t%d expected argv, got %T", t, v)
	}

	return args, nil
}

// valueAt reads temps[t] expecting a Value. Same invariant-check
// shape as argvAt: a mismatch means the IR is internally
// inconsistent, not a user error.
func (ex *executor) valueAt(t ir.Temp, sp source.Span) (Value, error) {
	v := ex.temps[t]
	val, ok := v.(Value)
	if !ok {
		return Value{}, syntax.SpanErrorf(sp, "exec: t%d expected Value, got %T", t, v)
	}

	return val, nil
}

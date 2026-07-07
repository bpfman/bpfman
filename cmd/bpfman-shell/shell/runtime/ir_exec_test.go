package runtime

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

// execCall is one observable side effect: which dispatch lane
// (command or bind) the runtime took and the rendered argv of the
// call.
type execCall struct {
	Lane string // "command" or "bind"
	Argv string // space-joined arg text
}

// renderArgv joins WordArg/QuotedArg/ScalarValueArg leaves to a
// space-separated string. Tests use this to canonicalise argv so
// assertions are on what the runtime actually dispatched, not on
// Arg shape.
func renderArgv(args []Arg) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, argText(a))
	}
	return strings.Join(parts, " ")
}

func argText(a Arg) string {
	switch v := a.(type) {
	case WordArg:
		return v.Text
	case QuotedArg:
		return v.Text
	case ScalarValueArg:
		if rendered, err := RenderCompact(v.Value); err == nil {
			return rendered
		}
		return fmt.Sprintf("%v", v.Value)
	case StructuredValueArg:
		if rendered, err := RenderCompact(v.Value); err == nil {
			return rendered
		}
		return fmt.Sprintf("%v", v.Value)
	default:
		return fmt.Sprintf("<%T>", a)
	}
}

// recordingEnv builds an Env whose ExecCommand and ExecBind hooks
// append a execCall to calls. The returned session is fresh;
// the env reads and writes its bindings as either engine would.
func recordingEnv(t *testing.T) (*Env, *[]execCall) {
	t.Helper()
	var calls []execCall
	env := &Env{
		Session: NewSession(),
		ExecCommand: func(args []Arg, span source.Span) (Value, error) {
			calls = append(calls, execCall{Lane: "command", Argv: renderArgv(args)})
			return Value{}, nil
		},
		ExecBind: func(args []Arg, span source.Span) (BindResult, error) {
			calls = append(calls, execCall{Lane: "bind", Argv: renderArgv(args)})
			return BindResult{Rc: OkEnvelope()}, nil
		},
	}
	return env, &calls
}

// TestExec_CommandStmt pins the lowered dispatch path for the
// smallest possible script: one command statement.
func TestExec_CommandStmt(t *testing.T) {
	t.Parallel()
	src := "echo hello world"
	assertCalls(t, runOnLowered(t, src), []execCall{{Lane: "command", Argv: "echo hello world"}})
}

// TestExec_LetThenCommand exercises Eval + BindName +
// BuildArgs + DispatchCommand: bind a value, then a subsequent
// command argv interpolates that value.
func TestExec_LetThenCommand(t *testing.T) {
	t.Parallel()
	src := "let x = 42\necho hello $x"
	assertCalls(t, runOnLowered(t, src), []execCall{{Lane: "command", Argv: "echo hello 42"}})
}

// TestExec_ThreadAndPureCall exercises the expression families
// that dispatch through ExecBind from concrete IR-owned expression
// nodes.
func TestExec_ThreadAndPureCall(t *testing.T) {
	t.Parallel()

	name := "u32le"
	src := "let base = 41\nlet piped = $base |> stage\nlet called = " + name + " $piped\necho $piped $called"

	run := func(t *testing.T) []execCall {
		t.Helper()
		prog := parseProgram(t, src)
		var calls []execCall
		env := &Env{
			Session: NewSession(),
			ExecCommand: func(args []Arg, span source.Span) (Value, error) {
				calls = append(calls, execCall{Lane: "command", Argv: renderArgv(args)})
				return Value{}, nil
			},
			ExecBind: func(args []Arg, span source.Span) (BindResult, error) {
				calls = append(calls, execCall{Lane: "bind", Argv: renderArgv(args)})
				head := argText(args[0])
				switch head {
				case "stage":
					last := argText(args[len(args)-1])
					return BindResult{Rc: OkEnvelope(), Primary: StringValue("<" + last + ">")}, nil
				case name:
					return BindResult{Rc: OkEnvelope(), Primary: StringValue("pure(" + argText(args[1]) + ")")}, nil
				default:
					return BindResult{Rc: OkEnvelope()}, nil
				}
			},
		}
		lp, err := lowerToIR(prog)
		if err != nil {
			t.Fatalf("Lower: %v", err)
		}

		if err := Exec(lp, env); err != nil {
			t.Fatalf("Exec: %v", err)
		}
		return calls
	}

	assertCalls(t, run(t), []execCall{
		{Lane: "bind", Argv: "stage 41"},
		{Lane: "bind", Argv: name + " <41>"},
		{Lane: "command", Argv: "echo <41> pure(<41>)"},
	})
}

// TestExec_Defer registers a defer and a foreground
// command; the engines should fire the foreground command first
// and the defer at scope exit, producing the same recorded
// sequence. The recording captures ExecBind for defers (defers
// dispatch in bind position) so the order shows up in the call list.
func TestExec_Defer(t *testing.T) {
	t.Parallel()
	src := "defer echo cleanup\necho main"
	assertCalls(t, runOnLowered(t, src), []execCall{
		{Lane: "command", Argv: "echo main"},
		{Lane: "bind", Argv: "echo cleanup"},
	})
}

// TestExec_BindNonGuard exercises DispatchBind + ApplyBind
// without a guard: the bind should record an ExecBind invocation
// and afterwards the named slot holds the primary result. The
// lowered engine should produce the expected bind then command sequence.
func TestExec_BindNonGuard(t *testing.T) {
	t.Parallel()
	src := "let r <- echo hello\necho follow"
	assertCalls(t, runOnLowered(t, src), []execCall{
		{Lane: "bind", Argv: "echo hello"},
		{Lane: "command", Argv: "echo follow"},
	})
}

// TestExec_BindGuardSuccess covers the happy path of a
// guard bind: the rc envelope is ok, so the guard falls through
// to the continuation; the second command runs.
func TestExec_BindGuardSuccess(t *testing.T) {
	t.Parallel()
	src := "guard r <- echo hello\necho follow"
	assertCalls(t, runOnLowered(t, src), []execCall{
		{Lane: "bind", Argv: "echo hello"},
		{Lane: "command", Argv: "echo follow"},
	})
}

// TestExec_If exercises Branch: a true condition takes the
// then-branch and runs a command; the else-branch's command does
// not run.
func TestExec_If(t *testing.T) {
	t.Parallel()
	src := "if true { echo yes } else { echo no }"
	assertCalls(t, runOnLowered(t, src), []execCall{{Lane: "command", Argv: "echo yes"}})
}

// TestExec_LetDestructure exercises Eval +
// BindDestructure: a list-shaped RHS binds positional names,
// and a subsequent command can interpolate one of them.
func TestExec_LetDestructure(t *testing.T) {
	t.Parallel()
	src := "let (a b) = [1 2]\necho $a $b"
	assertCalls(t, runOnLowered(t, src), []execCall{{Lane: "command", Argv: "echo 1 2"}})
}

// TestExec_LetDestructureErrorText pins the runtime
// diagnostic for a non-list destructure RHS: the bad script
// must render the documented user-facing message.
func TestExec_LetDestructureErrorText(t *testing.T) {
	t.Parallel()
	src := "let (a b) = 1"
	err := runScriptError(t, src, nil)
	if err == nil {
		t.Fatal("expected destructure error")
	}
	if !strings.Contains(err.Error(), "let: destructure RHS is not a list") {
		t.Fatalf("error lost the expected destructure wording: %v", err)
	}
}

// TestExec_Foreach iterates a literal list and runs an
// echo per element. Both engines should record one command call
// per element with the iter variable expanded.
func TestExec_Foreach(t *testing.T) {
	t.Parallel()
	src := "foreach x in [1 2 3] { echo $x }"
	assertCalls(t, runOnLowered(t, src), []execCall{
		{Lane: "command", Argv: "echo 1"},
		{Lane: "command", Argv: "echo 2"},
		{Lane: "command", Argv: "echo 3"},
	})
}

// TestExec_Break exits the loop after the second
// element; both engines should record exactly two commands.
func TestExec_Break(t *testing.T) {
	t.Parallel()
	src := "foreach x in [1 2 3] { if $x == 2 { break } ; echo $x }"
	assertCalls(t, runOnLowered(t, src), []execCall{{Lane: "command", Argv: "echo 1"}})
}

// TestExec_Continue skips iteration where the iter var
// equals 2; both engines should record echo 1 and echo 3.
func TestExec_Continue(t *testing.T) {
	t.Parallel()
	src := "foreach x in [1 2 3] { if $x == 2 { continue } ; echo $x }"
	assertCalls(t, runOnLowered(t, src), []execCall{
		{Lane: "command", Argv: "echo 1"},
		{Lane: "command", Argv: "echo 3"},
	})
}

// TestExec_Assert exercises the assert path under both
// lowered runtime. The recording
// uses a counter rather than the call list because neither
// callback is on the recordedCall channel.
func TestExec_Assert(t *testing.T) {
	t.Parallel()
	src := "assert 1 == 1\nrequire 2 == 2\necho done"
	count, calls := runAssertCounted(t, src)
	if count != 2 {
		t.Fatalf("expected 2 assertion callbacks, got %d", count)
	}
	assertCalls(t, calls, []execCall{{Lane: "command", Argv: "echo done"}})
}

// TestExec_DefForwardReference verifies that top-level
// defs are hoisted before body execution under both engines.
// Calling a def before its textual declaration should still
// route through callDef rather than falling through to the raw
// external-command lane.
func TestExec_DefForwardReference(t *testing.T) {
	t.Parallel()
	src := "f 42\ndef f(x) { echo $x }"
	got := runOnLowered(t, src)
	assertCalls(t, got, []execCall{{Lane: "command", Argv: "echo 42"}})
	if len(got) != 1 || got[0].Lane != "command" || !strings.HasPrefix(got[0].Argv, "echo ") || !strings.Contains(got[0].Argv, "42") {
		t.Fatalf("forward reference should resolve through the hoisted def, got %v", got)
	}
}

// TestExec_DefCommandPosition registers a def and
// invokes it in command position. Both engines must route
// through callDef (so the def is preferred over the external
// ExecCommand path) and produce the same echo argv.
func TestExec_DefCommandPosition(t *testing.T) {
	t.Parallel()
	src := "def f(x) { echo $x }\nf 42"
	assertCalls(t, runOnLowered(t, src), []execCall{{Lane: "command", Argv: "echo 42"}})
}

// TestExec_DefBindPosition exercises a def call in bind
// position so callDefAsBind is the dispatch path. The bound
// primary is used by the following command; both engines should
// record the same echo with the def's returned value
// interpolated.
func TestExec_DefBindPosition(t *testing.T) {
	t.Parallel()
	src := "def f(x) { return $x }\nguard y <- f 42\necho $y"
	assertCalls(t, runOnLowered(t, src), []execCall{{Lane: "command", Argv: "echo 42"}})
}

// TestExec_DefLocalDefer pairs a def with a defer and
// asserts the engines agree on the cleanup order: the body's
// command runs, then the def-local defer fires before the call
// returns. Under the lowered engine this exercises the full
// def-body IR -- EnterFrame, EnterDeferScope, RegisterDefer,
// body command, RunDefers def-local, ExitFrame -- and not the
// fallback AST walk.
func TestExec_DefLocalDefer(t *testing.T) {
	t.Parallel()
	src := "def f() { defer echo cleanup ; echo body }\nf"
	assertCalls(t, runOnLowered(t, src), []execCall{
		{Lane: "command", Argv: "echo body"},
		{Lane: "bind", Argv: "echo cleanup"},
	})
}

// TestExec_ReturnAfterDefer pins the documented order
// "stash return -> run def-local defers -> pop frame": the
// defer fires after the return value has been computed but
// before control leaves the call. Both engines should record
// the body command, the deferred cleanup, then the caller's
// command using the returned value.
func TestExec_ReturnAfterDefer(t *testing.T) {
	t.Parallel()
	src := "def f() { defer echo cleanup ; return 7 }\nguard x <- f\necho got $x"
	assertCalls(t, runOnLowered(t, src), []execCall{
		{Lane: "bind", Argv: "echo cleanup"},
		{Lane: "command", Argv: "echo got 7"},
	})
}

// TestExec_BindCollect drives bind-collect: iterate
// a list, dispatch a bind producer per element, and bind the
// accumulated primaries to a name. The trailing echo uses the
// bound list so both engines also see a downstream
// interpolation of the accumulated values.
func TestExec_BindCollect(t *testing.T) {
	t.Parallel()
	src := "let xs <- foreach p in [1 2 3] { echo $p }\necho done"
	assertCalls(t, runOnLowered(t, src), []execCall{
		{Lane: "bind", Argv: "echo 1"},
		{Lane: "bind", Argv: "echo 2"},
		{Lane: "bind", Argv: "echo 3"},
		{Lane: "command", Argv: "echo done"},
	})
}

// TestExec_BindCollectGuardFailureArgs verifies that a
// guarded bind-collect preserves the failing producer argv on the
// raised GuardFailure. Renderers and parity snapshots use those
// args to show the actual command line that failed.
func TestExec_BindCollectGuardFailureArgs(t *testing.T) {
	t.Parallel()
	src := "guard xs <- foreach n in [1] { probe $n }"
	schedule := map[string]int{"probe": 1}
	err := runScriptError(t, src, schedule)
	if err == nil {
		t.Fatal("expected guard failure")
	}

	var gf *GuardFailure
	if !errors.As(err, &gf) {
		t.Fatalf("expected GuardFailure, got %T", err)
	}

	args := renderArgv(gf.Args)
	if !strings.HasPrefix(args, "probe ") {
		t.Fatalf("guard args lost the producer head: %q", args)
	}
}

// TestExec_FinalBindingsAcrossLetSequence compares the
// final values of every variable a multi-statement let-chain
// binds. This test extends parity to the binding side of
// Env.Session so a divergence in how Eval or BindName
// produces values shows up directly.
func TestExec_FinalBindingsAcrossLetSequence(t *testing.T) {
	t.Parallel()
	src := "let a = 1\nlet b = 2\nlet pair = [$a $b]"
	env := runForBindings(t, src)
	assertBindingRaw(t, env, "a", "1")
	assertBindingRaw(t, env, "b", "2")
	assertBindingRaw(t, env, "pair", "[1 2]")
}

// TestExec_DefReturnsStructuredValue exercises a def
// that returns a list literal. The caller binds the returned
// value to a name; both engines must publish a Value whose
// Raw() is the same []any so foreach / IndexValue on the
// result behaves the same downstream.
func TestExec_DefReturnsStructuredValue(t *testing.T) {
	t.Parallel()
	src := "def make() { return [1 2 3] }\nguard xs <- make"
	env := runForBindings(t, src)
	assertBindingRaw(t, env, "xs", "[1 2 3]")
}

// TestExec_DeferFailureCounter pairs a defer with a
// command that fails. Both engines should run the defer at
// scope exit, record the failure via Session.RecordDeferFailure,
// and end with DeferFailures() == 1.
func TestExec_DeferFailureCounter(t *testing.T) {
	t.Parallel()
	src := "defer cleanup\necho main"
	env := runScriptWithSchedule(t, src, map[string]int{"cleanup": 1})
	if env.Session.DeferFailures() != 1 {
		t.Errorf("expected 1 defer failure, got %d", env.Session.DeferFailures())
	}
}

// TestExec_GuardFailureAtTopLevel halts the program
// with a guard failure outside any poll. Both engines
// should propagate a *GuardFailure whose Envelope, Primary,
// and Args fields match. The error type is the language's
// load-bearing signal for retry-vs-fatal and for diagnostic
// rendering.
func TestExec_GuardFailureAtTopLevel(t *testing.T) {
	t.Parallel()
	src := "guard r <- failing one two"
	err := runScriptError(t, src, map[string]int{"failing": 1})
	var gf *GuardFailure
	if !errors.As(err, &gf) {
		t.Fatalf("error is not *GuardFailure: %T", err)
	}
	if gf.Envelope != FailEnvelope() {
		t.Errorf("envelope mismatch: got=%+v want=%+v", gf.Envelope, FailEnvelope())
	}
	if gf.Primary != "r" {
		t.Errorf("primary mismatch: got=%q want=%q", gf.Primary, "r")
	}
	if renderArgv(gf.Args) != "failing one two" {
		t.Errorf("args mismatch: got=%s want=%s", renderArgv(gf.Args), "failing one two")
	}
}

// TestExec_RecursionDepthDiagnostic exercises the
// shared recursion-depth check in runDefCall. A def that calls
// itself unconditionally is caught by env.defCallDepth >=
// MaxDefCallDepth and both engines emit the same diagnostic.
// The check fires before runDefCall branches to the AST or
// lowered lane, so the message is identical by construction;
// pinning it with a test prevents accidental drift.
func TestExec_RecursionDepthDiagnostic(t *testing.T) {
	t.Parallel()
	src := "def f() { f }\nf"
	err := runScriptError(t, src, nil)
	if err == nil {
		t.Fatal("expected recursion-depth error")
	}
	if !strings.Contains(err.Error(), "recursion depth limit exceeded") {
		t.Errorf("error does not name the recursion-depth check: %v", err)
	}
}

// TestExec_DeferFailuresAcrossScopes pairs multiple
// defers inside both the program scope and a def scope and
// confirms both engines accumulate the same total. The tree
// defer failures in both program and def scope must end up on
// Session.DeferFailures.
func TestExec_DeferFailuresAcrossScopes(t *testing.T) {
	t.Parallel()
	src := "def f() { defer probe_a }\ndefer probe_b\nf"
	schedule := map[string]int{"probe_a": 1, "probe_b": 1}
	env := runScriptWithSchedule(t, src, schedule)
	if env.Session.DeferFailures() != 2 {
		t.Errorf("expected 2 defer failures (program + def), got %d", env.Session.DeferFailures())
	}
}

// TestExec_RepresentativeScriptState exercises a multi-
// construct script (lets, list literal, foreach with body
// effect, def call, assert) and compares every visible
// binding plus the assertion-failure counter between engines.
// A divergence under any individual construct surfaces as a
// binding or counter mismatch.
func TestExec_RepresentativeScriptState(t *testing.T) {
	t.Parallel()
	src := `let xs = [1 2 3]
let acc = 0
foreach v in $xs {
    let acc = $v
}
def f(x) { return $x }
guard r <- f $acc
assert 1 == 1
`
	env := runForBindings(t, src)
	assertBindingRaw(t, env, "xs", "[1 2 3]")
	assertBindingRaw(t, env, "acc", "0")
	assertBindingRaw(t, env, "r", "0")
	if env.Session.AssertFailures() != 0 {
		t.Errorf("AssertFailures mismatch: got=%d want=0", env.Session.AssertFailures())
	}
}

// runScriptErrorInFile runs src through one engine after stamping
// parser positions with sourceFile. Mirrors runScriptError otherwise;
// tests that want to assert decorated error parity use this.
// runForBindings runs src through one engine and returns the
// Env so tests can probe final Session bindings. The Env's
// ExecCommand and ExecBind are no-op recorders that always
// return ok; ExecAssert records a Session counter so asserts
// in the script do not fall over for lack of an assertion
// executor.
func runForBindings(t *testing.T, src string) *Env {
	t.Helper()
	prog := parseProgram(t, src)
	assertFn := func(a *ir.Assert, env *Env) error {
		clause, ok := a.Clause.(*ir.AssertExprClause)
		require.True(t, ok, "assert clause = %T, want *ir.AssertExprClause", a.Clause)
		v, err := EvalExpr(clause.Expr, env)
		if err != nil {
			return err
		}

		pass, err := AsBool(v)
		if err != nil {
			return err
		}
		if !pass {
			env.Session.RecordAssertFailure()
		}
		return nil
	}
	env := &Env{
		Session: NewSession(),
		ExecCommand: func(args []Arg, span source.Span) (Value, error) {
			return Value{}, nil
		},
		ExecBind: func(args []Arg, span source.Span) (BindResult, error) {
			return BindResult{Rc: OkEnvelope()}, nil
		},
		ExecAssert: assertFn,
	}
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	if err := Exec(lp, env); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	return env
}

// runScriptWithSchedule is a counter-focused variant of
// runWithFailureScheduleEnv. Same Env shape, but the recording
// hooks are no-ops because tests using this helper only care
// about Session counters.
func runScriptWithSchedule(t *testing.T, src string, schedule map[string]int) *Env {
	t.Helper()
	prog := parseProgram(t, src)
	failures := make(map[string]int, len(schedule))
	maps.Copy(failures, schedule)
	env := &Env{
		Session: NewSession(),
		ExecCommand: func(args []Arg, span source.Span) (Value, error) {
			return Value{}, nil
		},
		ExecBind: func(args []Arg, span source.Span) (BindResult, error) {
			name := commandHead(args)
			if failures[name] > 0 {
				failures[name]--
				return BindResult{Rc: FailEnvelope()}, nil
			}
			return BindResult{Rc: OkEnvelope()}, nil
		},
	}
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	if err := Exec(lp, env); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	return env
}

// runScriptError runs src and returns the engine's terminating
// error. Used by parity tests that expect an error and compare
// its type or fields. The schedule populates per-command failure
// counts for ExecBind.
func runScriptError(t *testing.T, src string, schedule map[string]int) error {
	t.Helper()
	prog := parseProgram(t, src)
	failures := make(map[string]int, len(schedule))
	maps.Copy(failures, schedule)
	env := &Env{
		Session: NewSession(),
		ExecAssert: func(a *ir.Assert, env *Env) error {
			switch clause := a.Clause.(type) {
			case *ir.AssertExprClause:
				val, err := EvalExpr(clause.Expr, env)
				if err != nil {
					return err
				}

				pass, err := AsBool(val)
				if err != nil {
					return err
				}
				if pass {
					return nil
				}
				if a.IsRequire {
					return &RequireFailure{Span: a.Span, Expr: ir.FormatAssertClauseSource(a.Clause)}
				}
				return &AssertFailure{Span: a.Span, Expr: ir.FormatAssertClauseSource(a.Clause)}
			default:
				return fmt.Errorf("unsupported assert clause %T", a.Clause)
			}
		},
		ExecCommand: func(args []Arg, span source.Span) (Value, error) {
			return Value{}, nil
		},
		ExecBind: func(args []Arg, span source.Span) (BindResult, error) {
			name := commandHead(args)
			if failures[name] > 0 {
				failures[name]--
				return BindResult{Rc: FailEnvelope()}, nil
			}
			return BindResult{Rc: OkEnvelope()}, nil
		},
	}
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}
	return Exec(lp, env)
}

// commandHead returns the leading word of an argv -- the
// command name a dispatcher would resolve against the def
// table or external lane. Empty argv or a non-word head
// returns "".
func commandHead(args []Arg) string {
	if len(args) == 0 {
		return ""
	}
	w, ok := args[0].(WordArg)
	if !ok {
		return ""
	}
	return w.Text
}

// runAssertCounted runs src through the lowered engine with an Env that
// counts ExecAssert invocations and records ExecCommand / ExecBind.
func runAssertCounted(t *testing.T, src string) (int, []execCall) {
	t.Helper()
	prog := parseProgram(t, src)
	var calls []execCall
	asserts := 0
	assertFn := func(*ir.Assert, *Env) error {
		asserts++
		return nil
	}
	env := &Env{
		Session: NewSession(),
		ExecCommand: func(args []Arg, span source.Span) (Value, error) {
			calls = append(calls, execCall{Lane: "command", Argv: renderArgv(args)})
			return Value{}, nil
		},
		ExecBind: func(args []Arg, span source.Span) (BindResult, error) {
			calls = append(calls, execCall{Lane: "bind", Argv: renderArgv(args)})
			return BindResult{Rc: OkEnvelope()}, nil
		},
		ExecAssert: assertFn,
	}
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	if err := Exec(lp, env); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	return asserts, calls
}

// runOnLowered runs src through the IR interpreter and returns the
// recorded calls.
func runOnLowered(t *testing.T, src string) []execCall {
	t.Helper()
	prog := parseProgram(t, src)
	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	env, calls := recordingEnv(t)
	if err := Exec(lp, env); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	return *calls
}

func equalCalls(a, b []execCall) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func assertCalls(t *testing.T, got, want []execCall) {
	t.Helper()
	if !equalCalls(got, want) {
		t.Fatalf("calls mismatch\n  got:  %v\n  want: %v", got, want)
	}
}

func assertBindingRaw(t *testing.T, env *Env, name, want string) {
	t.Helper()
	v, ok := env.Session.Get(name)
	if !ok {
		t.Fatalf("missing binding %q", name)
	}
	if got := fmt.Sprintf("%v", v.Raw()); got != want {
		t.Fatalf("binding %q mismatch: got=%v want=%v", name, got, want)
	}
}

// TestExecInScope_PreservesInheritedDeferScope pins the
// contract the script driver relies on: a caller that opens
// a long-lived defer scope via withDeferScope can run a sequence
// of source units through execInScope, each registering its own
// top-level defers, and every defer fires when the outer scope
// drains. execInScope must therefore neither drain nor
// clear env.defers on exit; the caller owns that pointer.
//
// The two examples scripts in the corpus (kprobe, tracepoint,
// ...) all hit this path.
func TestExecInScope_PreservesInheritedDeferScope(t *testing.T) {
	t.Parallel()
	env, calls := recordingEnv(t)

	sources := []string{
		"defer echo cleanup-1",
		"defer echo cleanup-2",
		"echo body",
	}

	err := withDeferScope(env, func() error {
		for _, src := range sources {
			prog := parseProgram(t, src)
			lp, lowErr := lowerToIR(prog)
			if lowErr != nil {
				return lowErr
			}

			if execErr := execInScope(lp, env); execErr != nil {
				return execErr
			}

			if env.defers == nil {
				t.Fatalf("after execInScope(%q): env.defers was cleared; caller's scope lost", src)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withDeferScope: %v", err)
	}

	// Body executes during its source unit; the two defers fire in
	// LIFO order when the outer scope drains. Defers dispatch
	// in bind position (matching TestExec_Defer above),
	// hence the "bind" lane.
	want := []execCall{
		{Lane: "command", Argv: "echo body"},
		{Lane: "bind", Argv: "echo cleanup-2"},
		{Lane: "bind", Argv: "echo cleanup-1"},
	}
	if !equalCalls(*calls, want) {
		t.Fatalf("calls mismatch\n  got:  %v\n  want: %v", *calls, want)
	}
}

// TestExecInScope_DefCallPreservesOuterDeferScope is the
// def-call sibling of the test above. A def has its own defer
// scope (push at EnterDeferScope DeferScopeDef, drain at
// RunDefers RunDefersDefLocal), so runLoweredDefCall sets up and
// tears down its own scope around the def body. When the def
// returns cleanly, env.defers must be back at the caller's
// outer pointer.
//
// To isolate the def-call lane (rather than the InScope source-unit
// boundary covered by the test above), everything happens in a
// single source unit: define a def with a def-local defer, call it,
// then register a top-level defer. The single execInScope
// call exercises runLoweredDefCall's unwindOnExit; the post-
// run env.defers check then catches any clobber.
func TestExecInScope_DefCallPreservesOuterDeferScope(t *testing.T) {
	t.Parallel()
	env, calls := recordingEnv(t)

	src := "def cleanup() {\n" +
		"    defer echo def-local\n" +
		"    echo def-body\n" +
		"}\n" +
		"cleanup\n" +
		"defer echo top-level-after\n"

	err := withDeferScope(env, func() error {
		prog := parseProgram(t, src)
		lp, lowErr := lowerToIR(prog)
		if lowErr != nil {
			return lowErr
		}

		if execErr := execInScope(lp, env); execErr != nil {
			return execErr
		}

		if env.defers == nil {
			t.Fatalf("after execInScope: env.defers cleared; caller's scope lost")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withDeferScope: %v", err)
	}

	want := []execCall{
		{Lane: "command", Argv: "echo def-body"},
		{Lane: "bind", Argv: "echo def-local"},
		{Lane: "bind", Argv: "echo top-level-after"},
	}
	if !equalCalls(*calls, want) {
		t.Fatalf("calls mismatch\n  got:  %v\n  want: %v", *calls, want)
	}
}

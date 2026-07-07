package runtime

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

// Parse-level tests pin the AST shape and the diagnostic for the
// rejected forms.

func TestParse_Return_BindsExpression(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, `
def f() {
  return 1
}
`)
	require.Len(t, prog.Stmts, 1)
	d, ok := prog.Stmts[0].(*syntax.DefStmt)
	require.True(t, ok)
	require.Len(t, d.Body, 1)
	r, ok := d.Body[0].(*syntax.ReturnStmt)
	require.True(t, ok)
	require.NotNil(t, r.Expr)
}

func TestParse_Return_BareReturnRejected(t *testing.T) {
	t.Parallel()
	src := `
def f() {
  return
}
`
	tokens, err := syntax.Tokenise(src)
	require.NoError(t, err)
	_, err = syntax.Parse(tokens)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "return requires an expression")
}

func TestParse_Return_VarRef(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, `
def f(x) {
  return $x
}
`)
	d := prog.Stmts[0].(*syntax.DefStmt)
	require.Len(t, d.Body, 1)
	r := d.Body[0].(*syntax.ReturnStmt)
	_, ok := r.Expr.(*syntax.VarRefExpr)
	require.True(t, ok, "return $x must parse the RHS as a VarRefExpr")
}

func TestParse_Return_List(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, `
def f() {
  return [1 2 3]
}
`)
	d := prog.Stmts[0].(*syntax.DefStmt)
	r := d.Body[0].(*syntax.ReturnStmt)
	_, ok := r.Expr.(*syntax.ListExpr)
	require.True(t, ok, "return [list] must parse the RHS as a ListExpr")
}

func TestParse_Return_Arithmetic(t *testing.T) {
	t.Parallel()
	prog := parseProgram(t, `
def f(x) {
  return $x + 1
}
`)
	d := prog.Stmts[0].(*syntax.DefStmt)
	r := d.Body[0].(*syntax.ReturnStmt)
	_, ok := r.Expr.(*syntax.BinaryExpr)
	require.True(t, ok, "return $x + 1 must parse the RHS as a BinaryExpr")
}

func TestParse_Return_TopLevelParsesButRejectedAtRuntime(t *testing.T) {
	t.Parallel()
	// The parser does not know context; a top-level return parses
	// as a ReturnStmt and is rejected at evaluation time. The
	// static checker rejects it earlier; the runtime guard is the
	// safety net documented on evalProgramBody.
	prog := parseProgram(t, `return 1`)
	require.Len(t, prog.Stmts, 1)
	_, ok := prog.Stmts[0].(*syntax.ReturnStmt)
	require.True(t, ok)
}

// Runtime tests pin the statement-form contract: a return is an
// early exit, the value is discarded at command-form position,
// and the early-exit honours def-local defers and frame
// discipline.

func TestExecSource_Return_StatementFormIsEarlyExit(t *testing.T) {
	t.Parallel()
	src := `
def f() {
  before
  return 1
  after
}
f
`
	_, calls := runProgram(t, src)
	require.Len(t, calls, 1)
	assert.Equal(t, []string{"before"}, calls[0])
}

func TestExecSource_Return_OutsideDefIsRuntimeError(t *testing.T) {
	t.Parallel()
	src := `return 1`
	prog := parseProgram(t, src)
	env := &Env{
		Session:     NewSession(),
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
	}
	err := execParsedProgram(t, prog, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "return")
	assert.Contains(t, err.Error(), "outside any def")
}

func TestExecSource_Return_InIfOutsideDefIsRuntimeError(t *testing.T) {
	t.Parallel()
	// A return inside an if at script top level still has no
	// enclosing def and must be caught.
	src := `
if true {
  return 1
}
`
	prog := parseProgram(t, src)
	env := &Env{
		Session:     NewSession(),
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
	}
	err := execParsedProgram(t, prog, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "return")
	assert.Contains(t, err.Error(), "outside any def")
}

func TestExecSource_Return_InForeachInsideDefHonoursEarlyExit(t *testing.T) {
	t.Parallel()
	// A return inside a foreach body inside a def stops the
	// iteration and the def. The post-foreach statement does not
	// run, nor does any subsequent foreach element.
	src := `
def f() {
  foreach x in [a b c] {
    if $x == "b" {
      return 1
    }
    seen $x
  }
  after-loop
}
f
`
	_, calls := runProgram(t, src)
	require.Len(t, calls, 1)
	assert.Equal(t, []string{"seen", "a"}, calls[0])
}

func TestExecSource_Return_RunsDefLocalDefers(t *testing.T) {
	t.Parallel()
	// def-local defers register at body time and unwind on
	// return. The early exit must not skip them; the captured
	// argument vector must survive the frame pop. Defers
	// dispatch via ExecBind, so the recorder pattern from
	// defer_test.go is the right test fixture.
	r := &recorder{}
	env := &Env{
		Session:     NewSession(),
		ExecBind:    r.execBind,
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
	}
	src := `
def f() {
  defer cleanup "A"
  defer cleanup "B"
  return 1
  defer cleanup "C"
}
f
`
	require.NoError(t, runProgramWithEnv(t, src, env))
	require.Len(t, r.calls, 2, "two defers registered before return; the third is unreachable")
	// LIFO unwind: B first, then A.
	assert.Equal(t, "cleanup B", joinArgTexts(r.calls[0]))
	assert.Equal(t, "cleanup A", joinArgTexts(r.calls[1]))
}

func TestExecSource_Return_DoesNotLeakBindings(t *testing.T) {
	t.Parallel()
	// A return must not bleed body-locals into the caller; the
	// frame pop is the same as the normal-exit path.
	src := `
def f() {
  let scratch = inside
  return 1
}
f
`
	s, _ := runProgram(t, src)
	_, ok := s.Get("scratch")
	assert.False(t, ok, "return must pop the call frame and discard body-locals")
}

func TestExecSource_Return_ExpressionUsesCallFrame(t *testing.T) {
	t.Parallel()
	// The expression evaluates inside the call frame: a return
	// referencing a body-local sees the body-local, not the
	// caller's same-named variable.
	src := `
let label = outer
def f() {
  let label = inner
  return $label
}
f
seen $label
`
	_, calls := runProgram(t, src)
	require.Len(t, calls, 1)
	// After the call, the caller's "outer" is reinstated.
	assert.Equal(t, []string{"seen", "outer"}, calls[0])
}

func TestExecSource_Return_UnboundVariableIsFatal(t *testing.T) {
	t.Parallel()
	// A return expression that itself errors halts the def
	// without raising the return signal -- the error propagates
	// as fatal and the script halts.
	src := `
def f() {
  return $missing
}
f
`
	prog := parseProgram(t, src)
	env := &Env{
		Session:     NewSession(),
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
	}
	err := execParsedProgram(t, prog, env)
	require.Error(t, err)
	// One of the unbound-variable messages; not "return outside a def body".
	assert.True(t, strings.Contains(err.Error(), "missing") || strings.Contains(err.Error(), "undefined"), "expected unbound-variable diagnostic, got %q", err.Error())
}

func TestExecSource_Return_NestedDefsHonourEachOwnReturn(t *testing.T) {
	t.Parallel()
	// An inner def's return must not unwind out of the outer
	// def. The outer's post-call statement runs; the inner's
	// post-return statement does not.
	src := `
def inner() {
  before-inner
  return 1
  after-inner
}
def outer() {
  before-outer
  inner
  after-outer
}
outer
`
	_, calls := runProgram(t, src)
	require.Len(t, calls, 3)
	assert.Equal(t, []string{"before-outer"}, calls[0])
	assert.Equal(t, []string{"before-inner"}, calls[1])
	assert.Equal(t, []string{"after-outer"}, calls[2])
}

// Bind-position tests pin the value-returning contract: guard
// publishes a def's return Value as the unwrapped primary, let keeps
// the inspectable outcome, and defer failures during return-unwind
// mark Rc failed.

// bindEnv builds an Env for tests that exercise the bind path:
// the recorder captures every ExecBind call so a non-def-dispatch
// fallback shows up as a recorded call, and ExecCommand is a
// no-op so command-form invocations (the def's own call site) do
// not interfere. The recorder's rc function lets a test mark
// specific commands as failing -- used by the defer-failure
// tests where a defer fires through ExecBind and must surface a
// non-ok envelope.
func bindEnv(r *recorder) *Env {
	return &Env{
		Session:     NewSession(),
		ExecBind:    r.execBind,
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
	}
}

func TestExecSource_Return_GuardCarriesPrimary(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	env := bindEnv(r)
	src := `
def f() { return 7 }
guard v <- f
seen $v
`
	require.NoError(t, runProgramWithEnv(t, src, env))
	v, ok := env.Session.Get("v")
	require.True(t, ok, "guard bind must set v")
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "7", got)
}

func TestExecSource_Return_BindFromParameter(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	env := bindEnv(r)
	src := `
def echo(x) { return $x }
guard v <- echo "hello"
`
	require.NoError(t, runProgramWithEnv(t, src, env))
	v, ok := env.Session.Get("v")
	require.True(t, ok)
	got, _ := v.Scalar()
	assert.Equal(t, "hello", got)
}

func TestExecSource_Return_BindReturnsList(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	env := bindEnv(r)
	src := `
def triple() { return [1 2 3] }
guard xs <- triple
`
	require.NoError(t, runProgramWithEnv(t, src, env))
	xs, ok := env.Session.Get("xs")
	require.True(t, ok)
	raw, ok := xs.Raw().([]any)
	require.True(t, ok, "expected a list, got %T", xs.Raw())
	assert.Len(t, raw, 3)
}

func TestExecSource_Return_BindIntoListThenDestructure(t *testing.T) {
	t.Parallel()
	// The documented two-value pattern: return a list, bind it,
	// destructure into named slots. Pins the composition for a
	// helper that returns structured values for caller-side cleanup.
	r := &recorder{}
	env := bindEnv(r)
	src := `
def pair() { return [left right] }
guard p <- pair
let (a b) = $p
seen $a $b
`
	require.NoError(t, runProgramWithEnv(t, src, env))
	a, _ := env.Session.Get("a")
	b, _ := env.Session.Get("b")
	ga, _ := a.Scalar()
	gb, _ := b.Scalar()
	assert.Equal(t, "left", ga)
	assert.Equal(t, "right", gb)
}

func TestExecSource_Return_LetBindSetsOutcomeAndValue(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	env := bindEnv(r)
	src := `
def f() { return "primary-value" }
let r <- f
`
	require.NoError(t, runProgramWithEnv(t, src, env))
	got, ok := env.Session.Get("r")
	require.True(t, ok)
	raw, ok := got.Raw().(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, raw["ok"], "outcome is ok for a clean return")
	p, err := got.LookupValue("r", "value")
	require.NoError(t, err)
	gp, err := p.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "primary-value", gp)
}

func TestExecSource_Return_GuardOnSuccessBindsAndContinues(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	env := bindEnv(r)
	src := `
def f() { return 1 }
guard v <- f
after $v
`
	require.NoError(t, runProgramWithEnv(t, src, env))
	v, ok := env.Session.Get("v")
	require.True(t, ok, "guard must bind on success")
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "1", got)
}

func TestExecSource_Return_NoReturnBindsEnvelope(t *testing.T) {
	t.Parallel()
	// A def with no `return` in bind position yields the
	// envelope-mirror as the primary, matching no-payload
	// providers (exec, bpftool, wait). The primary's .ok
	// resolves to true on a clean run.
	r := &recorder{}
	env := bindEnv(r)
	src := `
def f() { do-something }
let v <- f
`
	require.NoError(t, runProgramWithEnv(t, src, env))
	v, ok := env.Session.Get("v")
	require.True(t, ok)
	raw, ok := v.Raw().(map[string]any)
	require.True(t, ok, "no-return def's primary must be an envelope-shaped map")
	assert.Equal(t, true, raw["ok"])
}

func TestExecSource_Return_DiscardUnderscorePrimary(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	env := bindEnv(r)
	src := `
def f() { return 42 }
let _ <- f
`
	require.NoError(t, runProgramWithEnv(t, src, env))
	_, ok := env.Session.Get("_")
	assert.False(t, ok, "_ must not establish a binding")
}

func TestExecSource_Return_DeferFailureFlipsRcOk(t *testing.T) {
	t.Parallel()
	// A defer that fires inside the def body and returns non-ok
	// must mark the bind-position Rc failed even though
	// `return EXPR` itself evaluated cleanly. The outcome exposes
	// the failed envelope while .value still carries the returned
	// payload.
	r := &recorder{rc: func(args []Arg) Envelope {
		if len(args) > 0 {
			if w, ok := args[0].(WordArg); ok && w.Text == "cleanup" {
				return Envelope{ExitCode: 1}
			}
		}
		return Envelope{}
	}}
	env := bindEnv(r)
	// RenderDeferFailure is required so the defer dispatcher
	// counts the failure on the session; without it the run
	// short-circuits before incrementing the counter and the
	// flip never happens.
	env.RenderDeferFailure = func(source.Pos, []Arg, Envelope) {}
	src := `
def f() {
  defer cleanup "kaboom"
  return 1
}
let r <- f
`
	require.NoError(t, runProgramWithEnv(t, src, env))
	got, ok := env.Session.Get("r")
	require.True(t, ok)
	raw, ok := got.Raw().(map[string]any)
	require.True(t, ok)
	assert.Equal(t, false, raw["ok"], "defer failure during return unwind must flip outcome ok")
	// outcome.exit_code must be non-zero when outcome.ok is false; an
	// envelope with ok=false / exit_code=0 is internally
	// inconsistent and confuses the guard-failure renderer
	// (which prints "exit: 0" for what was actually a
	// failed call).
	exitCodeStr := fmt.Sprint(raw["exit_code"])
	assert.NotEqual(t, "0", exitCodeStr, "outcome exit_code must be non-zero alongside ok=false")
	p, err := got.LookupValue("r", "value")
	require.NoError(t, err)
	gp, err := p.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "1", gp, "value still carries the returned payload")
}

func TestExecSource_Return_GuardHaltsOnDeferFailure(t *testing.T) {
	t.Parallel()
	// A guard form sees rc.ok=false from the defer flip and
	// halts via GuardFailure before any post-call statement
	// runs.
	r := &recorder{rc: func(args []Arg) Envelope {
		if len(args) > 0 {
			if w, ok := args[0].(WordArg); ok && w.Text == "cleanup" {
				return Envelope{ExitCode: 1}
			}
		}
		return Envelope{}
	}}
	env := bindEnv(r)
	env.RenderDeferFailure = func(source.Pos, []Arg, Envelope) {}
	src := `
def f() {
  defer cleanup
  return 1
}
guard p <- f
after $p
`
	prog := parseProgram(t, src)
	err := execParsedProgram(t, prog, env)
	require.Error(t, err)
	var gf *GuardFailure
	require.True(t, errors.As(err, &gf), "expected GuardFailure, got %T: %v", err, err)
	assert.False(t, gf.Envelope.OK())
	// The guard-failure envelope must carry a non-zero exit code
	// when rc.ok is false. The driver-side
	// RenderEnvelopeFailure block prints "exit: <exit_code>", so a
	// exit_code of 0 alongside ok=false reads as a successful exit
	// in the rendered diagnostic -- internally inconsistent
	// and actively misleading.
	assert.NotZero(t, gf.Envelope.ExitCode, "guard envelope's exit_code must be non-zero on failure")
}

func TestExecSource_Return_RecursiveValueReturn(t *testing.T) {
	t.Parallel()
	// Recursion through value-returning defs: each call gets
	// its own frame and its own returnSignal. The outer call's
	// `let v <- inner` routes through callDefAsBind, the inner
	// call's `return` raises a separate returnSignal that the
	// inner callDefAsBind catches, and the bound value crosses
	// the call boundary intact for the outer to publish.
	//
	// Numeric recursion (sum_to N) would force the test to deal
	// with the language's command-arg stringification rule --
	// arguments lose their numeric Kind at the call boundary, by
	// design. String equality lets the test stay focused on the
	// recursion contract.
	r := &recorder{}
	env := bindEnv(r)
	src := `
def chain(depth) {
  if $depth == "stop" {
    return "base"
  }
  guard next <- chain stop
  return "wrap:${next}"
}
guard v <- chain go
`
	require.NoError(t, runProgramWithEnv(t, src, env))
	v, ok := env.Session.Get("v")
	require.True(t, ok)
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "wrap:base", got)
}

func TestExecSource_Return_BindFatalErrorPropagates(t *testing.T) {
	t.Parallel()
	// A body error unrelated to return -- here, an unbound
	// variable inside the return expression -- escapes
	// callDefAsBind as a Go error; the bind path frames it and
	// no binding happens.
	r := &recorder{}
	env := bindEnv(r)
	src := `
def f() {
  return $missing
}
let v <- f
`
	prog := parseProgram(t, src)
	err := execParsedProgram(t, prog, env)
	require.Error(t, err)
	_, hasV := env.Session.Get("v")
	assert.False(t, hasV, "no binding when the def body errors")
}

func TestExecSource_Return_BindCollectFromDefProducer(t *testing.T) {
	t.Parallel()
	// A def callable in bind position is also a valid
	// bind-collect producer. Each iteration calls the def, the
	// primary accumulates into the bound list.
	r := &recorder{}
	env := bindEnv(r)
	src := `
def square(x) { return $x * $x }
guard squares <- foreach n in [1 2 3 4] {
  square $n
}
`
	require.NoError(t, runProgramWithEnv(t, src, env))
	xs, ok := env.Session.Get("squares")
	require.True(t, ok)
	raw, ok := xs.Raw().([]any)
	require.True(t, ok, "expected list, got %T", xs.Raw())
	require.Len(t, raw, 4)
	// Each element is a string of the squared integer.
	gotSquares := make([]string, len(raw))
	for i, el := range raw {
		gotSquares[i] = elementText(el)
	}
	assert.Equal(t, []string{"1", "4", "9", "16"}, gotSquares)
}

// elementText extracts a stringy view of a list element for
// assertion purposes. The bind-collect path stores .Raw() of each
// element; scalars come through as their underlying type so we
// normalise via fmt.Sprint for the test.
func elementText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		// fall through to a string form; json.Number and ints
		// both render via Sprint without quoting.
		return fmt.Sprint(x)
	}
}

// Checker tests pin the static-time rejection. The runtime catch
// in evalProgramBody is the safety net; the checker rejection is
// what scripts hit first, before any side effects fire.

func TestCheck_Return_InsideDefIsClean(t *testing.T) {
	t.Parallel()
	issues := checkSource(t, "def f() { return 1 }")
	assert.Empty(t, issues)
}

func TestCheck_Return_InsideIfInsideDefIsClean(t *testing.T) {
	t.Parallel()
	// A return inside a nested block is fine as long as some
	// enclosing def opens the call context. The depth counter
	// tracks "any enclosing def", not "directly enclosed".
	issues := checkSource(t, "def f(x) { if $x { return 1 } }")
	assert.Empty(t, issues)
}

func TestCheck_Return_InsideForeachInsideDefIsClean(t *testing.T) {
	t.Parallel()
	src := "def f(xs) { foreach x in $xs { return $x } }"
	issues := checkSource(t, src)
	assert.Empty(t, issues)
}

func TestCheck_Return_AtTopLevelIsRejected(t *testing.T) {
	t.Parallel()
	issues := checkSource(t, "return 1")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "return outside a def body")
}

func TestCheck_Return_InsideIfAtTopLevelIsRejected(t *testing.T) {
	t.Parallel()
	// An if at script top level is not a def context; a return
	// inside is still rejected.
	src := "if true { return 1 }"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "return outside a def body")
}

func TestCheck_Return_AfterDefIsStillTopLevel(t *testing.T) {
	t.Parallel()
	// defDepth must unwind when the def body exits: a return
	// written after a def declaration is at top level and must
	// be rejected.
	src := "def f() { print 1 }\nreturn 1"
	issues := checkSource(t, src)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Msg, "return outside a def body")
}

func TestCheck_Return_ExpressionStillChecked(t *testing.T) {
	t.Parallel()
	// A bad expression on the return RHS reports the
	// undefined-variable issue even when the position is also
	// wrong, so a script with multiple problems shows them all.
	src := "return $missing"
	issues := checkSource(t, src)
	require.Len(t, issues, 2)
	msgs := []string{issues[0].Msg, issues[1].Msg}
	assert.Contains(t, strings.Join(msgs, "\n"), "return outside a def body")
	assert.Contains(t, strings.Join(msgs, "\n"), "undefined variable: missing")
}

func TestCheck_Return_DefInsideDefIsRejected(t *testing.T) {
	t.Parallel()
	// Def declarations are top-level only. A nested def still
	// walks its body so internal `return` use stays well-formed,
	// but the declaration itself must be rejected.
	src := "def outer() { def inner() { return 1 }\nreturn 2 }"
	issues := checkSource(t, src)
	require.NotEmpty(t, issues)
	assert.Contains(t, issues[0].Msg, `def "inner" must be declared at top level`)
}

// Regression: runaway recursion through value-returning defs
// must surface a clean diagnostic rather than a Go runtime
// stack overflow. The corpus's natural shape -- a recursive
// helper that forgets its base case -- would dump pages of
// goroutine traces; the evaluator should catch the depth
// excess and emit "in def NAME: recursion depth limit
// exceeded (N)". The exact limit is implementation-defined
// but must be a few orders of magnitude smaller than Go's
// stack so the diagnostic fires before the runtime panics.
func TestExecSource_Return_RecursionDepthGuard(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	env := bindEnv(r)
	src := `
def loop() {
  let next <- loop
  return $next
}
let v <- loop
`
	prog := parseProgram(t, src)
	err := execParsedProgram(t, prog, env)
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "recursion", "diagnostic must name the failure class")
	assert.Contains(t, msg, "loop", "diagnostic must name the offending def")
}

// A `defer my_def` statement must route through the def's body,
// not through env.ExecBind's external-dispatch fallback. The
// bind-statement path already does the def lookup ahead of
// ExecBind, and the defer path follows the same precedence.
//
// The recorder's ExecBind records every call it receives, so a
// successful def dispatch must leave no defer-side recording
// for the def name itself: the def's body runs via callDefAsBind
// internally, and any commands the body invokes route through
// ExecCommand at command position. The probe runs a def whose
// body calls a wrapped sentinel command so the test can
// distinguish "def dispatched through callDef" (sentinel
// captured via ExecCommand) from "exec attempted on the def
// name" (defer recorded an exec of "cleanup").
func TestExecSource_Return_DeferDispatchesDef(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	var commandCalls []string
	env := &Env{
		Session:  NewSession(),
		ExecBind: r.execBind,
		ExecCommand: func(args []Arg, _ source.Span) (Value, error) {
			if len(args) > 0 {
				if w, ok := args[0].(WordArg); ok {
					commandCalls = append(commandCalls, w.Text)
				}
			}
			return Value{}, nil
		},
	}
	src := `
def cleanup() {
  marker
}
defer cleanup
print "main"
`
	require.NoError(t, runProgramWithEnv(t, src, env))
	// The def's body called marker at command position, so it
	// shows up via ExecCommand. The def name itself must not
	// have been seen by ExecBind as a top-level dispatch.
	assert.Contains(t, commandCalls, "marker", "def body must run when the defer fires")
	for _, c := range r.calls {
		if w, ok := c.args[0].(WordArg); ok && w.Text == "cleanup" {
			t.Fatalf("defer dispatched the def name to ExecBind; def should resolve before external dispatch")
		}
	}
}

func TestCheck_Return_DefInsideIfIsRejected(t *testing.T) {
	t.Parallel()
	src := `
if false {
  def hidden() {
    return "v"
  }
}
`
	issues := checkSource(t, src)
	require.NotEmpty(t, issues)
	assert.Contains(t, issues[0].Msg, `def "hidden" must be declared at top level`)
}

func TestCheck_Return_DefInsideForeachIsRejected(t *testing.T) {
	t.Parallel()
	src := `
foreach x in [] {
  def hidden() {
    return $x
  }
}
`
	issues := checkSource(t, src)
	require.NotEmpty(t, issues)
	assert.Contains(t, issues[0].Msg, `def "hidden" must be declared at top level`)
}

func TestCheck_Return_DefInsidePollIsRejected(t *testing.T) {
	t.Parallel()
	src := `
poll timeout 1s every 1ms {
  def hidden() {
    return 1
  }
}
`
	issues := checkSource(t, src)
	require.NotEmpty(t, issues)
	assert.Contains(t, issues[0].Msg, `def "hidden" must be declared at top level`)
}

// Regression: top-level defs still get registered globally so
// the existing usages work. This pins the top-level-only
// boundary: legal defs still register for bind dispatch.
func TestCheck_Return_TopLevelDefStillRegistered(t *testing.T) {
	t.Parallel()
	src := `
def visible() {
  return "v"
}
guard p <- visible
print $p
`
	issues := checkSource(t, src)
	assert.Empty(t, issues, "a top-level def must still register for bind dispatch to pass preflight")
}

// Regression: `let v = my_def` silently binds the literal
// string "my_def" when my_def is a registered def. The two-
// operator distinction between `=` (expression) and `<-`
// (bind) is intentional, but the silent-wrong-thing failure
// mode is steep enough that new users routinely walk off the
// cliff. The checker has the defs map already populated; on a
// single-name `let v = bareword` where
// the bareword matches a known def, emit a hint pointing at
// the bind form. Does NOT restrict the user -- a def name is
// a valid bareword literal -- just helps when the shape is
// almost certainly a typo.
func TestCheck_Return_LetEqualsDefNameHintsAtBind(t *testing.T) {
	t.Parallel()
	src := `
def maker() {
  return "value"
}
let v = maker
`
	issues := checkSource(t, src)
	require.NotEmpty(t, issues, "the suspicious shape must produce an issue")
	var combined strings.Builder
	combined.WriteString(issues[0].Msg)
	for _, i := range issues[1:] {
		combined.WriteString("\n" + i.Msg)
	}
	assert.Contains(t, combined.String(), "maker", "the diagnostic must name the def")
	assert.Contains(t, combined.String(), "<-", "the diagnostic must point at the bind form")
}

// Regression: the same hint must NOT fire for a let whose RHS
// names something that is NOT a def. A bareword on the RHS
// of `=` is a perfectly valid string literal; we only intercept
// when the bareword is a known def, where the shape is almost
// certainly a typo'd bind. Without this guard the checker
// would emit hints for every bareword-named-string assignment.
func TestCheck_Return_LetEqualsNonDefIsClean(t *testing.T) {
	t.Parallel()
	src := `let v = literal_string_value`
	issues := checkSource(t, src)
	assert.Empty(t, issues, "a bareword RHS that is not a def must not trigger the hint")
}

// With the defs map populated, the checker fuzzy-matches a
// typo'd bind head against the known defs and emits a "did you
// mean ..." hint. Doesn't restrict: an unknown head might
// genuinely be an external command, so the hint is
// informational.
func TestCheck_Return_BindRHSTypoSuggestsDefName(t *testing.T) {
	t.Parallel()
	src := `
def loader() {
  return "value"
}
let v <- loaderr
`
	issues := checkSource(t, src)
	require.NotEmpty(t, issues, "the typo'd bind head must produce a hint")
	var combined strings.Builder
	combined.WriteString(issues[0].Msg)
	for _, i := range issues[1:] {
		combined.WriteString("\n" + i.Msg)
	}
	assert.Contains(t, combined.String(), "loaderr", "the diagnostic must name the typo")
	assert.Contains(t, combined.String(), "loader", "the diagnostic must suggest the actual def name")
	assert.Contains(t, combined.String(), "did you mean", "the diagnostic must explicitly frame the suggestion")
}

// Regression: a bind head that genuinely is an unknown
// external command -- no defs in scope, or all defs far away
// from the head's text -- must NOT trip the typo hint. The
// strdist threshold gates suggestions to short edit
// distances, but the no-defs-at-all path also needs to stay
// clean.
func TestCheck_Return_BindRHSUnknownNoDefsIsClean(t *testing.T) {
	t.Parallel()
	src := `let v <- some_external_command`
	issues := checkSource(t, src)
	assert.Empty(t, issues, "no defs to match against -- the hint must not fire")
}

// Regression: a comparison operand naming a def must hint
// at the bind form. The arithmetic-operand hint covers
// `let x = two + 3`; this is the parallel for `if two == 2`.
// Same shape, different code path.
func TestCheck_Return_ComparisonHintsAtDefBindForm(t *testing.T) {
	t.Parallel()
	src := `
def two() {
  return 2
}
if two == 2 {
  print "yes"
}
`
	issues := checkSource(t, src)
	require.NotEmpty(t, issues, "the mismatch must still be reported")
	var combined strings.Builder
	combined.WriteString(issues[0].Msg)
	for _, i := range issues[1:] {
		combined.WriteString("\n" + i.Msg)
	}
	assert.Contains(t, combined.String(), "two", "the diagnostic must name the offending operand")
	assert.Contains(t, combined.String(), "<-", "the diagnostic must point at the bind form")
}

// Regression: a non-numeric arithmetic operand whose text
// happens to be a known def name produces a confusing
// "operand 'two' is not numeric" diagnostic. The user almost
// certainly meant to call the def. The checker should append
// a "did you mean `let v <- two`?" hint so the corrective
// shape is obvious.
func TestCheck_Return_ArithmeticHintsAtDefBindForm(t *testing.T) {
	t.Parallel()
	src := `
def two() {
  return 2
}
let x = two + 3
`
	issues := checkSource(t, src)
	require.NotEmpty(t, issues, "non-numeric operand still rejected")
	var combined strings.Builder
	combined.WriteString(issues[0].Msg)
	for _, i := range issues[1:] {
		combined.WriteString("\n" + i.Msg)
	}
	assert.Contains(t, combined.String(), "two", "the diagnostic must name the offending operand")
	assert.Contains(t, combined.String(), "<-", "hint must point at the bind form")
}

// Regression: a runtime error inside a def body cited only
// the body location, leaving the caller unable to tell which
// of several call sites tripped it. With value-returning
// helpers designed for reuse, the ambiguity is acute. The
// diagnostic must name the call site so the user can navigate
// to the offending caller.
func TestExecSource_Return_RuntimeErrorNamesCallSite(t *testing.T) {
	t.Parallel()
	// The leading newline puts the def on line 2 of the source
	// and the bind on line 6, so the test can assert both the
	// body line in the rust-frame citation and the call line
	// in the embedded "in def echo (called at ...)" note.
	src := `
def echo(x) {
  return $x.field
}

let a <- echo "first"
`
	prog := parseProgram(t, src)
	env := &Env{
		Session: NewSession(),
		ExecCommand: func([]Arg, source.Span) (Value, error) {
			return Value{}, nil
		},
		ExecBind: func([]Arg, source.Span) (BindResult, error) {
			return BindResult{Rc: Envelope{}}, nil
		},
	}
	err := execParsedProgram(t, prog, env)
	require.Error(t, err)
	msg := err.Error()
	// The body-side citation must still be present so the
	// user sees the inner error location.
	assert.Contains(t, msg, "cannot access field", "the body error must still be cited")
	// The diagnostic must name the called def.
	assert.Contains(t, msg, "echo", "the diagnostic must name the called def")
	// And the call site line must appear in the message. The
	// bind statement is on line 6 of the source; the call
	// itself is at the head of the RHS command.
	assert.Contains(t, msg, "called at 6:", "the call site must be cited at the bind's line")
}

// Regression: with two def-call frames -- outer calls inner --
// the call-site annotation must point at the line WITHIN the
// outer def's body where the call to inner appears, not at some
// ambient execution offset. Positions are absolute now, so the
// multi-source case is simply "parse each source with its real
// file and starting line" and the innermost call site should
// survive intact.
func TestExecSource_Return_DeepChainCallSiteIsInnermost(t *testing.T) {
	t.Parallel()

	s := NewSession()
	env := &Env{
		Session:     s,
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
		ExecBind: func([]Arg, source.Span) (BindResult, error) {
			return BindResult{Rc: Envelope{}}, nil
		},
	}

	parseAt := func(file string, startLine int, src string) *syntax.Program {
		t.Helper()
		tokens, err := syntax.TokeniseAt(source.Pos{File: file, Line: startLine, Col: 1}, src)
		require.NoError(t, err)
		prog, err := syntax.Parse(tokens)
		require.NoError(t, err)
		return prog
	}

	// Source 1: inner def at file lines 2-4 inclusive.
	innerProg := parseAt("helpers.bpfman", 2, "def inner(x) {\n  return $x.bad\n}")
	require.NoError(t, execParsedProgram(t, innerProg, env))

	// Source 2: outer def at file lines 6-9. The call to
	// inner is on the second body line, file line 7.
	outerProg := parseAt("helpers.bpfman", 6, "def outer(x) {\n  let v <- inner $x\n  return $v\n}")
	require.NoError(t, execParsedProgram(t, outerProg, env))

	// Source 3: top-level invocation at file line 11.
	callProg := parseAt("main.bpfman", 11, `let r <- outer "hi"`)
	err := execParsedProgram(t, callProg, env)
	require.Error(t, err)
	msg := err.Error()

	assert.Contains(t, msg, "in def inner", "annotation must name the innermost def")
	// The innermost call -- outer's body invoking inner -- is
	// on file line 7.
	assert.Contains(t, msg, "called at 7:", "innermost call site is on line 7 (outer's body invoking inner)")
	assert.NotContains(t, msg, "called at 11", "must not report the top-level source line as the failing call's location")
}

// Regression: ordinary whole-source lowering still cites the raw
// script line directly at the failing call site.
func TestExecSource_Return_CallSiteAtEmbeddedTopLevel(t *testing.T) {
	t.Parallel()
	src := "def f(x) { return $x.y }\nlet v <- f \"hi\"\n"
	prog := parseProgram(t, src)
	env := &Env{
		Session:     NewSession(),
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
		ExecBind: func([]Arg, source.Span) (BindResult, error) {
			return BindResult{Rc: Envelope{}}, nil
		},
	}
	err := execParsedProgram(t, prog, env)
	require.Error(t, err)
	msg := err.Error()
	// The bind is on line 2; the call is at the head of the RHS.
	assert.Contains(t, msg, "called at 2:", "call site at line 2 must be cited")
}

// Regression: the static checker must treat a parameter-dependent
// def return as an open-shape source, not as a result envelope.
// The Envelope shape is sealed -- its fields are
// ok/exit_code/stdout/stderr/... -- so a field name like `.id` that
// does not exist there gets rejected at preflight when the
// primary is mis-shaped as an envelope. Parameter passthrough is
// still dynamic under monomorphic return-shape inference, so the
// primary remains open and field access passes preflight.
func TestCheck_Return_BindFromDef_UnknownShapeAllowsFieldAccess(t *testing.T) {
	t.Parallel()
	src := `
def load_prog(x) {
  return $x
}
guard p <- load_prog hello
print $p.id
`
	issues := checkSource(t, src)
	assert.Empty(t, issues, "field access on a def-returned value must not be rejected by the checker")
}

// Regression: a recursive value-returning def -- one whose
// body contains `let v <- self ...` against itself -- must
// pass preflight. The checker records the def name BEFORE
// walking the body so the inner bind site resolves the head
// to a known def and binds an open shape on $v.
func TestCheck_Return_RecursiveValueReturn_FieldAccess(t *testing.T) {
	t.Parallel()
	src := `
def chain(depth) {
  if $depth == "stop" {
    return "base"
  }
  guard next <- chain stop
  return ${next}
}
guard v <- chain go
print $v.field
`
	issues := checkSource(t, src)
	assert.Empty(t, issues, "recursive value-returning def must allow field access on its primary")
}

// A no-return def in bind position produces the envelope
// mirror as the primary (matching the no-payload command-bind
// family); the checker must keep the sealed envelope shape for
// that case so accessing a non-envelope field on the bound
// primary is caught at preflight rather than failing at
// runtime.
func TestCheck_Return_NoReturnDefBindsSealedEnvelope(t *testing.T) {
	t.Parallel()
	src := `
def side_effect() {
  print "ran"
}
let v <- side_effect
print $v.id
`
	issues := checkSource(t, src)
	require.NotEmpty(t, issues, "no-return def bind must keep the sealed envelope shape so .id is rejected at preflight")
	// The diagnostic still points at the offending field
	// access, not at the def call itself.
	assert.Contains(t, issues[0].Msg, "id")
}

// Regression: a def whose name shadows a registered pure
// builtin must route through the def at preflight, so the
// pure-builtin <- rejection does not fire. The runtime's
// def lookup wins ahead of the external dispatch path; the
// checker must mirror that precedence.
func TestCheck_Return_DefShadowingPureBuiltinIsAccepted(t *testing.T) {
	t.Parallel()
	// `range` is a registered pure builtin; if a script
	// shadows it with a def, the def takes over on the bind
	// RHS at runtime and the checker must agree.
	src := `
def range() {
  return "shadowed"
}
let v <- range
`
	issues := checkSource(t, src)
	assert.Empty(t, issues, "a def shadowing a pure builtin must not trip the pure-builtin <- rejection")
}

// Regression: a defer-failure flip on the bind-position outcome
// must reflect the def's OWN cleanup outcome, not the session-
// wide counter. An inner def whose defer fails -- invoked as a
// command form and discarding its own rc -- must not cause the
// outer def's `let r <- outer` to land with r.ok = false.
// The contract is def-local cleanup; nested defer failures must
// not leak across call boundaries.
func TestExecSource_Return_NestedDeferFailureDoesNotLeak(t *testing.T) {
	t.Parallel()
	r := &recorder{rc: func(args []Arg) Envelope {
		if len(args) > 0 {
			if w, ok := args[0].(WordArg); ok && w.Text == "cleanup_inner" {
				return Envelope{ExitCode: 1}
			}
		}
		return Envelope{}
	}}
	env := bindEnv(r)
	env.RenderDeferFailure = func(source.Pos, []Arg, Envelope) {}
	src := `
def inner() {
  defer cleanup_inner
}
def outer() {
  inner
  return 1
}
let r <- outer
`
	require.NoError(t, runProgramWithEnv(t, src, env))
	rc, ok := env.Session.Get("r")
	require.True(t, ok)
	rawRc, ok := rc.Raw().(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, rawRc["ok"], "outer's r.ok must reflect outer's OWN cleanup, not the inner def's defer failure")
}

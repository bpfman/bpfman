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

// recordedCall captures one ExecBind invocation for inspection.
type recordedCall struct {
	args []Arg
	rc   Envelope
}

// recorder builds an ExecBind that records every invocation in
// order and returns the configured rc/primary. ok controls
// whether the rc is treated as successful by guard/defer.
type recorder struct {
	calls []recordedCall
	rc    func(args []Arg) Envelope
}

func (r *recorder) execBind(args []Arg, _ source.Span) (BindResult, error) {
	rc := Envelope{}
	if r.rc != nil {
		rc = r.rc(args)
	}
	r.calls = append(r.calls, recordedCall{args: copyArgs(args), rc: rc})
	return BindResult{Rc: rc, Primary: ValueFromEnvelope(rc)}, nil
}

func copyArgs(args []Arg) []Arg {
	out := make([]Arg, len(args))
	copy(out, args)
	return out
}

// argText extracts the first argument's text for a recorded call,
// matching the syntax tests use to identify which command ran.
func argText0(c recordedCall) string {
	if len(c.args) == 0 {
		return ""
	}
	if w, ok := c.args[0].(WordArg); ok {
		return w.Text
	}
	return ""
}

// joinArgTexts flattens a recorded call's args into a single string
// so tests can match on the full command line.
func joinArgTexts(c recordedCall) string {
	parts := make([]string, 0, len(c.args))
	for _, a := range c.args {
		switch v := a.(type) {
		case WordArg:
			parts = append(parts, v.Text)
		case ScalarValueArg:
			parts = append(parts, v.Text)
		case QuotedArg:
			parts = append(parts, v.Text)
		case StructuredValueArg:
			parts = append(parts, "$"+v.Name)
		default:
			parts = append(parts, fmt.Sprintf("%T", v))
		}
	}
	return joinWithSpace(parts)
}

func joinWithSpace(parts []string) string {
	var out strings.Builder
	for i, p := range parts {
		if i > 0 {
			out.WriteString(" ")
		}
		out.WriteString(p)
	}
	return out.String()
}

func runProgramWithEnv(t *testing.T, src string, env *Env) error {
	t.Helper()
	return execSourceProgram(t, src, env)
}

// A defer command's stdout/stderr is captured into its result
// envelope; an Env.RenderDeferOutput callback the driver wires
// flushes the captured streams, and the shell layer invokes it
// after every defer dispatch so the driver decides where the
// bytes go.
func TestExecSource_Defer_StdoutFlushedThroughRenderDeferOutput(t *testing.T) {
	t.Parallel()

	r := &recorder{rc: func(args []Arg) Envelope {
		// Simulate a defer whose dispatched handler produced
		// stdout (the way the print builtin would inside
		// makeExecBind's captured stdout buffer).
		head := argText0(recordedCall{args: args})
		if head == "say" && len(args) > 1 {
			if w, ok := args[1].(WordArg); ok {
				return Envelope{Stdout: w.Text}
			}
		}
		return Envelope{}
	}}
	var flushed []string
	env := &Env{
		Session:  NewSession(),
		ExecBind: r.execBind,
		RenderDeferOutput: func(args []Arg, rc Envelope) {
			if rc.Stdout != "" {
				flushed = append(flushed, rc.Stdout)
			}
		},
	}
	require.NoError(t, runProgramWithEnv(t, "defer say hello\n", env))
	require.Len(t, flushed, 1, "RenderDeferOutput must fire for the dispatched defer")
	assert.Equal(t, "hello", flushed[0])
}

func TestExecSource_Defer_LIFOOrder(t *testing.T) {
	t.Parallel()

	r := &recorder{}
	env := &Env{Session: NewSession(), ExecBind: r.execBind}
	src := "defer cleanup a\ndefer cleanup b\n"
	require.NoError(t, runProgramWithEnv(t, src, env))

	require.Len(t, r.calls, 2)
	assert.Equal(t, "cleanup b", joinArgTexts(r.calls[0]))
	assert.Equal(t, "cleanup a", joinArgTexts(r.calls[1]))
}

func TestExecSource_Defer_LifecyclePair(t *testing.T) {
	t.Parallel()

	// The lifecycle case: two resources acquired and registered
	// in order, then a guard fails. Cleanup must fire in reverse
	// (b before a) so the resource graph unwinds correctly.
	guards := 0
	r := &recorder{
		rc: func(args []Arg) Envelope {
			head := argText0(recordedCall{args: args})
			if head == "fail-now" {
				return Envelope{ExitCode: 1, Stderr: "boom"}
			}
			if head == "make-resource" {
				guards++
				return Envelope{}
			}
			return Envelope{}
		},
	}
	env := &Env{Session: NewSession(), ExecBind: r.execBind}
	src := "guard a <- make-resource a\n" +
		"defer cleanup $a\n" +
		"guard b <- make-resource b\n" +
		"defer cleanup $b\n" +
		"guard _ <- fail-now\n"

	err := runProgramWithEnv(t, src, env)
	require.Error(t, err)
	var gf *GuardFailure
	require.True(t, errors.As(err, &gf), "expected GuardFailure, got %T", err)

	// Recorded order: two make-resource, then fail-now (guard
	// failure), then two cleanups in LIFO order.
	require.Len(t, r.calls, 5)
	assert.Equal(t, "make-resource a", joinArgTexts(r.calls[0]))
	assert.Equal(t, "make-resource b", joinArgTexts(r.calls[1]))
	assert.Equal(t, "fail-now", joinArgTexts(r.calls[2]))
	// The two cleanup calls each receive a captured envelope as
	// their second argument; the rendering reads the variable
	// name from the StructuredValueArg so '$b' precedes '$a'.
	assert.Equal(t, "cleanup $b", joinArgTexts(r.calls[3]))
	assert.Equal(t, "cleanup $a", joinArgTexts(r.calls[4]))
}

func TestExecSource_Defer_ArgsCapturedAtRegisterTime(t *testing.T) {
	t.Parallel()

	// Rebinding the variable between defer and scope exit must
	// not change the deferred call's argument: the value at
	// defer time is what runs.
	r := &recorder{}
	env := &Env{Session: NewSession(), ExecBind: r.execBind}
	src := "let target = original\n" +
		"defer cleanup $target\n" +
		"let target = replaced\n"
	require.NoError(t, runProgramWithEnv(t, src, env))

	require.Len(t, r.calls, 1)
	assert.Equal(t, "cleanup original", joinArgTexts(r.calls[0]))
}

func TestExecSource_Defer_ShadowRebindingDoesNotLeak(t *testing.T) {
	t.Parallel()

	// The language permits shadowing: 'let' may rebind a name
	// that already exists. The defer at the rebind boundary must
	// see the original binding, regardless of which form did the
	// rebind ('=' assignment or '<-' command capture). Three
	// defers attached at distinct points in the rebind chain
	// each capture a different snapshot, and the deferred calls
	// fire in LIFO order over those frozen values.
	r := &recorder{
		rc: func(args []Arg) Envelope {
			head := argText0(recordedCall{args: args})
			if head == "fetch-third" {
				return Envelope{}
			}
			return Envelope{}
		},
	}
	env := &Env{Session: NewSession(), ExecBind: r.execBind}
	src := "let r = first\n" +
		"defer cleanup $r\n" +
		"let r = second\n" +
		"defer cleanup $r\n" +
		"let r <- fetch-third\n" +
		"defer cleanup $r\n"
	require.NoError(t, runProgramWithEnv(t, src, env))

	// Recorded ExecBind calls: one for the bind 'fetch-third',
	// then three cleanups in LIFO order. The third cleanup runs
	// first and saw the rc envelope from fetch-third (a
	// StructuredValueArg). The second cleanup saw 'second' (a
	// scalar). The first cleanup saw 'first'.
	require.Len(t, r.calls, 4)
	assert.Equal(t, "fetch-third", joinArgTexts(r.calls[0]))
	assert.Equal(t, "cleanup $r", joinArgTexts(r.calls[1]))
	assert.Equal(t, "cleanup second", joinArgTexts(r.calls[2]))
	assert.Equal(t, "cleanup first", joinArgTexts(r.calls[3]))
}

func TestExecSource_Defer_FailureRendersAndCounts(t *testing.T) {
	t.Parallel()

	// Defer that returns !ok should be rendered (callback fires)
	// and counted in Session.DeferFailures(). Cleanup continues
	// to subsequent defers; the script's body return value is
	// unaffected because the defer is on the success path.
	rendered := 0
	r := &recorder{
		rc: func(args []Arg) Envelope {
			if argText0(recordedCall{args: args}) == "broken-cleanup" {
				return Envelope{ExitCode: 2, Stderr: "broken"}
			}
			return Envelope{}
		},
	}
	session := NewSession()
	env := &Env{
		Session:  session,
		ExecBind: r.execBind,
		RenderDeferFailure: func(stmtLoc source.Pos, args []Arg, rc Envelope) {
			rendered++
		},
	}
	src := "defer cleanup a\ndefer broken-cleanup\ndefer cleanup b\n"
	require.NoError(t, runProgramWithEnv(t, src, env))

	// All three defers ran (LIFO: b, broken, a).
	require.Len(t, r.calls, 3)
	assert.Equal(t, "cleanup b", joinArgTexts(r.calls[0]))
	assert.Equal(t, "broken-cleanup", joinArgTexts(r.calls[1]))
	assert.Equal(t, "cleanup a", joinArgTexts(r.calls[2]))
	assert.Equal(t, 1, rendered, "renderer fires for the broken cleanup only")
	assert.Equal(t, 1, session.DeferFailures())
}

func TestExecSource_Defer_RunsOnGuardHalt(t *testing.T) {
	t.Parallel()

	// Defer registered before the failing guard must still run.
	r := &recorder{
		rc: func(args []Arg) Envelope {
			if argText0(recordedCall{args: args}) == "fail-now" {
				return Envelope{ExitCode: 1}
			}
			return Envelope{}
		},
	}
	env := &Env{Session: NewSession(), ExecBind: r.execBind}
	src := "defer cleanup\nguard _ <- fail-now\n"

	err := runProgramWithEnv(t, src, env)
	require.Error(t, err)
	var gf *GuardFailure
	require.True(t, errors.As(err, &gf))

	// First the failing guard, then the defer.
	require.Len(t, r.calls, 2)
	assert.Equal(t, "fail-now", joinArgTexts(r.calls[0]))
	assert.Equal(t, "cleanup", joinArgTexts(r.calls[1]))
}

func TestExecSource_Defer_ForEachRegistersInEnclosing(t *testing.T) {
	t.Parallel()

	// foreach is not a defer scope. defers registered inside the
	// loop attach to the enclosing scope and run after the loop
	// completes, in LIFO order across all iterations.
	r := &recorder{}
	s := NewSession()
	s.Set("xs", ValueFromAny([]any{"a", "b", "c"}))
	env := &Env{Session: s, ExecBind: r.execBind}
	src := "foreach x in $xs {\n  defer cleanup $x\n}\n"
	require.NoError(t, runProgramWithEnv(t, src, env))

	require.Len(t, r.calls, 3)
	assert.Equal(t, "cleanup c", joinArgTexts(r.calls[0]))
	assert.Equal(t, "cleanup b", joinArgTexts(r.calls[1]))
	assert.Equal(t, "cleanup a", joinArgTexts(r.calls[2]))
}

// Tests below pin defer-capture-at-registration behaviour as a
// load-bearing contract. Block frames pop at body exit, so a
// deferred command registered inside a foreach body, an if
// branch, or a def call survives the frame disappearing. The
// deferred argument vector was resolved when the defer ran --
// not at unwind -- so the call fires with the values the body
// saw, even after the frame is gone.

func TestExecSource_Defer_InsideForEach_CapturesIterationVariable(t *testing.T) {
	t.Parallel()

	// Each iteration registers a defer that mentions $x. The
	// defer's args are resolved at registration time, so the
	// frozen vector encodes the iteration's value rather than
	// referring to $x by name. Defers unwind LIFO at script
	// exit, after the loop has finished, by which time the
	// iteration frame is long gone -- but the captured value
	// is independent of the frame.
	r := &recorder{}
	s := NewSession()
	s.Set("xs", ValueFromAny([]any{"a", "b", "c"}))
	env := &Env{Session: s, ExecBind: r.execBind}
	src := "foreach x in $xs {\n  defer cleanup $x\n}\n"
	require.NoError(t, runProgramWithEnv(t, src, env))

	require.Len(t, r.calls, 3)
	assert.Equal(t, "cleanup c", joinArgTexts(r.calls[0]))
	assert.Equal(t, "cleanup b", joinArgTexts(r.calls[1]))
	assert.Equal(t, "cleanup a", joinArgTexts(r.calls[2]))
}

func TestExecSource_Defer_InsideDef_CapturesCallParameter(t *testing.T) {
	t.Parallel()

	// A defer registered in a def body must survive the call
	// frame's disappearance and fire with the value the call
	// saw. Without capture-at-registration, the deferred
	// resolution would look up $prog in the post-call session
	// state and either find a stale value or an unbound name.
	r := &recorder{}
	env := &Env{Session: NewSession(), ExecBind: r.execBind}
	src := "def use(prog) {\n" +
		"  defer cleanup $prog\n" +
		"}\n" +
		"use first\n" +
		"use second\n"
	require.NoError(t, runProgramWithEnv(t, src, env))

	// Each def call opens its own defer scope, so the cleanup
	// fires at end-of-def with the frozen parameter value
	// rather than being deferred to script exit. Two calls
	// produce two cleanups in call order.
	require.Len(t, r.calls, 2)
	assert.Equal(t, "cleanup first", joinArgTexts(r.calls[0]))
	assert.Equal(t, "cleanup second", joinArgTexts(r.calls[1]))
}

func TestExecSource_Defer_RebindSnapshot_InsideForEach(t *testing.T) {
	t.Parallel()

	// Rebinding the loop variable between defer registrations
	// must not retroactively change earlier deferred calls.
	// Three defers in the body each see the iteration's
	// rebound value; unwind fires them in the order they were
	// registered (LIFO across the whole loop).
	r := &recorder{}
	s := NewSession()
	s.Set("xs", ValueFromAny([]any{"a", "b"}))
	env := &Env{Session: s, ExecBind: r.execBind}
	src := "foreach x in $xs {\n" +
		"  defer cleanup $x\n" +
		"  let x = rebound\n" +
		"  defer cleanup $x\n" +
		"}\n"
	require.NoError(t, runProgramWithEnv(t, src, env))

	// Defers fire LIFO across both iterations. Each iteration
	// registers two defers: one with the original loop value,
	// one with the rebound value. So the unwind order is:
	//   iter b: rebound, b
	//   iter a: rebound, a
	// reversed to:
	//   rebound (b's), b, rebound (a's), a
	require.Len(t, r.calls, 4)
	assert.Equal(t, "cleanup rebound", joinArgTexts(r.calls[0]))
	assert.Equal(t, "cleanup b", joinArgTexts(r.calls[1]))
	assert.Equal(t, "cleanup rebound", joinArgTexts(r.calls[2]))
	assert.Equal(t, "cleanup a", joinArgTexts(r.calls[3]))
}

func TestExecSource_Defer_RebindSnapshot_InsideIf(t *testing.T) {
	t.Parallel()

	// An if-branch let rebinds $x within the branch. The defer
	// in the branch captures the inner value; the defer
	// outside the if captures the outer value. Both must fire
	// against their own frozen values, regardless of what
	// $x resolves to at script exit.
	r := &recorder{}
	env := &Env{Session: NewSession(), ExecBind: r.execBind}
	src := "let x = outer\n" +
		"defer cleanup outer-defer $x\n" +
		"if true {\n" +
		"  let x = inner\n" +
		"  defer cleanup inner-defer $x\n" +
		"}\n"
	require.NoError(t, runProgramWithEnv(t, src, env))

	require.Len(t, r.calls, 2)
	// LIFO: the if-branch defer was registered last, so it
	// fires first, holding "inner".
	assert.Equal(t, "cleanup inner-defer inner", joinArgTexts(r.calls[0]))
	// The script-scope defer was registered first, holds "outer".
	assert.Equal(t, "cleanup outer-defer outer", joinArgTexts(r.calls[1]))
}

func TestParse_Defer_RequiresCommand(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "defer\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "defer requires a command form")
}

func TestParse_Defer_BindsToDeferStmt(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "defer cleanup $x")
	require.NoError(t, err)
	d, ok := firstStmt(t, prog).(*syntax.DeferStmt)
	require.True(t, ok, "expected DeferStmt, got %T", firstStmt(t, prog))
	require.NotNil(t, d.Cmd)
	require.Len(t, d.Cmd.Args, 2)
	head, ok := d.Cmd.Args[0].(*syntax.LiteralExpr)
	require.True(t, ok)
	assert.Equal(t, "cleanup", head.Text)
}

func TestParse_Defer_IsReservedDefName(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "def defer() { print hi }")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved word \"defer\"")
}

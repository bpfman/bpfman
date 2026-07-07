package runtime

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// runtimeCapture summarises everything observable from running
// a script through the runtime under a recording Env.
type runtimeCapture struct {
	Calls       []execCall
	Bindings    map[string]string
	AssertFails int
	DeferFails  int
	JobLeaks    int
	Err         string
	ErrType     string
	GuardEnv    Envelope
	GuardName   string
	GuardArgs   string
}

type runtimeCaptureOpts struct {
	Schedule map[string]int
	File     string
	Names    []string
}

func captureLoweredRuntime(t *testing.T, src string, opts runtimeCaptureOpts) runtimeCapture {
	t.Helper()

	var (
		prog *syntax.Program
		err  error
	)
	if opts.File != "" {
		tokens, tokErr := syntax.TokeniseAt(source.Pos{File: opts.File, Line: 1, Col: 1}, src)
		require.NoError(t, tokErr)
		prog, err = syntax.Parse(tokens)
		require.NoError(t, err)
	} else {
		prog = parseProgram(t, src)
	}

	failures := make(map[string]int, len(opts.Schedule))
	maps.Copy(failures, opts.Schedule)

	var calls []execCall
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
			if a.IsRequire {
				return &RequireFailure{Span: a.Span, Expr: fmt.Sprintf("%v", v.Raw())}
			}
			env.Session.RecordAssertFailure()
		}
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
			name := commandHead(args)
			if failures[name] > 0 {
				failures[name]--
				return BindResult{Rc: FailEnvelope()}, nil
			}
			return BindResult{Rc: OkEnvelope()}, nil
		},
		ExecAssert: assertFn,
	}

	lp, err := lowerToIR(prog)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}

	runErr := Exec(lp, env)

	cap := runtimeCapture{
		Calls:       calls,
		Bindings:    map[string]string{},
		AssertFails: env.Session.AssertFailures(),
		DeferFails:  env.Session.DeferFailures(),
		JobLeaks:    env.Session.JobLeaks(),
	}
	if runErr != nil {
		cap.Err = runErr.Error()
		cap.ErrType = fmt.Sprintf("%T", runErr)
		var gf *GuardFailure
		if errors.As(runErr, &gf) {
			cap.GuardEnv = gf.Envelope
			cap.GuardName = gf.Primary
			cap.GuardArgs = renderArgv(gf.Args)
		}
	}

	names := opts.Names
	if len(names) == 0 {
		names = env.Session.Names()
	}
	for _, name := range names {
		if v, ok := env.Session.Get(name); ok {
			cap.Bindings[name] = fmt.Sprintf("%v", v.Raw())
		}
	}
	return cap
}

// TestLowered_RequireFailureInNestedDef pins the lowered runtime's
// current shell-level require behaviour under the recording
// callback used by these unit tests: the helper fails
// immediately, and require does not increment the session's
// committed assertion count.
func TestLowered_RequireFailureInNestedDef(t *testing.T) {
	t.Parallel()

	cap := captureLoweredRuntime(t, "def helper() { require false }\nhelper", runtimeCaptureOpts{})
	assert.Equal(t, "*syntax.SyntaxError", cap.ErrType)
	assert.Contains(t, cap.Err, "require failed: false")
	assert.Equal(t, 0, cap.AssertFails)
	assert.Empty(t, cap.Calls)
}

// TestLowered_AssertFailureInNestedDef covers the non-halting
// counterpart: assert false inside a helper records the failure,
// returns, and the caller continues into the following command.
func TestLowered_AssertFailureInNestedDef(t *testing.T) {
	t.Parallel()

	cap := captureLoweredRuntime(t, "def helper() { assert false }\nhelper\necho done", runtimeCaptureOpts{})
	assert.Empty(t, cap.Err)
	assert.Empty(t, cap.ErrType)
	assert.Equal(t, 1, cap.AssertFails)
	assert.Equal(t, []execCall{{Lane: "command", Argv: "echo done"}}, cap.Calls)
}

// TestLowered_RequireInsidePoll exercises require inside a poll body
// via a nested def call. The construct should halt with the require
// failure and must not reframe it as a timeout.
func TestLowered_RequireInsidePoll(t *testing.T) {
	t.Parallel()

	cap := captureLoweredRuntime(t, "def helper() { require false }\npoll timeout 20ms every 5ms { helper }", runtimeCaptureOpts{})
	assert.Equal(t, "*syntax.SyntaxError", cap.ErrType)
	assert.Contains(t, cap.Err, "require failed: false")
	assert.Equal(t, 0, cap.AssertFails)
	assert.Empty(t, cap.Calls)
	assert.Equal(t, 0, cap.DeferFails)
	assert.Equal(t, 0, cap.JobLeaks)
	assert.False(t, strings.Contains(cap.Err, "timed out after"))
}

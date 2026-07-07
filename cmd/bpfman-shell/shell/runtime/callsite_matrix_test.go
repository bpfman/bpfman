package runtime

import (
	"errors"
	"fmt"
	"testing"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

type scriptSource struct {
	file      string
	startLine int
	src       string
}

type sourceRun struct {
	env   *Env
	calls []execCall
	err   error
}

func TestImportedNestedCommandGuardFailure(t *testing.T) {
	t.Parallel()

	sources := []scriptSource{
		{
			file:      "library.bpfman",
			startLine: 2,
			src: "def inner() {\n" +
				"  guard _ <- probe\n" +
				"}\n",
		},
		{
			file:      "helpers.bpfman",
			startLine: 10,
			src: "def outer() {\n" +
				"  inner\n" +
				"  print after_inner\n" +
				"}\n",
		},
		{
			file:      "main.bpfman",
			startLine: 20,
			src: "outer\n" +
				"print after_top\n",
		},
	}

	tokens, err := syntax.TokeniseAt(source.Pos{File: "main.bpfman", Line: 20, Col: 1}, sources[2].src)
	require.NoError(t, err)
	mainProg, err := syntax.Parse(tokens)
	require.NoError(t, err)
	expectedOuterSpan := syntax.NodeSpan(mainProg.Stmts[0])

	run := runSourceSequence(t, sources, map[string]int{"probe": 1})
	require.Error(t, run.err)
	assert.Contains(t, run.err.Error(), "guard _: command failed", "the failing guard must remain the user-visible cause")
	assert.NotContains(t, run.err.Error(), "in def outer", "command-position guard failures should not be re-attributed to the wrapper def")
	assertSyntaxErrorFile(t, run.err, "main.bpfman")
	assertSyntaxErrorSpan(t, run.err, expectedOuterSpan)
	assert.False(t, callSeen(run.calls, "print after_inner"), "inner continuation must not run after the failing helper call")
	assert.False(t, callSeen(run.calls, "print after_top"), "top-level continuation must not run after the failing helper call")
	assert.Equal(t, []execCall{{Lane: "bind", Argv: "probe"}}, run.calls)
}

func TestImportedNestedFailureMatrix(t *testing.T) {
	t.Parallel()

	type failureMode struct {
		name     string
		innerSrc string
		schedule map[string]int
	}
	type topSite struct {
		name string
		wrap func(string) string
	}

	modes := []failureMode{
		{
			name: "syntax_error",
			innerSrc: "def inner() {\n" +
				"  let x = $missing\n" +
				"}\n",
		},
		{
			name: "guard_failure",
			innerSrc: "def inner() {\n" +
				"  guard _ <- probe\n" +
				"}\n",
			schedule: map[string]int{"probe": 1},
		},
	}

	sites := []topSite{
		{
			name: "command",
			wrap: func(head string) string {
				return head + "\nprint after_top\n"
			},
		},
		{
			name: "bind",
			wrap: func(head string) string {
				return "let r <- " + head + "\nprint after_top\n"
			},
		},
		{
			name: "guard",
			wrap: func(head string) string {
				return "guard r <- " + head + "\nprint after_top\n"
			},
		},
		{
			name: "defer",
			wrap: func(head string) string {
				return "defer " + head + "\nprint body_done\n"
			},
		},
		{
			name: "producer",
			wrap: func(head string) string {
				return "let xs <- foreach n in [1] { " + head + " }\nprint after_top\n"
			},
		},
	}

	for _, mode := range modes {
		for _, site := range sites {
			name := fmt.Sprintf("%s/%s", mode.name, site.name)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				tokens, err := syntax.TokeniseAt(source.Pos{File: "main.bpfman", Line: 20, Col: 1}, site.wrap("outer"))
				require.NoError(t, err)
				mainProg, err := syntax.Parse(tokens)
				require.NoError(t, err)
				expectedOuterSpan := syntax.NodeSpan(mainProg.Stmts[0])
				sources := []scriptSource{
					{
						file:      "library.bpfman",
						startLine: 2,
						src:       mode.innerSrc,
					},
					{
						file:      "helpers.bpfman",
						startLine: 10,
						src: "def outer() {\n" +
							"  inner\n" +
							"  print after_inner\n" +
							"}\n",
					},
					{
						file:      "main.bpfman",
						startLine: 20,
						src:       site.wrap("outer"),
					},
				}

				run := runSourceSequence(t, sources, mode.schedule)
				if site.name == "defer" {
					require.NoError(t, run.err)
					assert.Equal(t, 1, run.env.Session.DeferFailures(), "the failing deferred helper should count exactly once")
					assert.True(t, callSeen(run.calls, "print body_done"), "the main body must complete before deferred failure fires")
					assert.False(t, callSeen(run.calls, "print after_inner"), "wrapper def continuation must not run after the failing helper call")
					if mode.name == "guard_failure" {
						assert.Equal(t, 1, callPrefixCount(run.calls, "probe"), "deferred helper should still reach the failing probe exactly once")
					} else {
						assert.NotContains(t, run.calls, execCall{Lane: "bind", Argv: "probe"})
					}
				} else {
					require.Error(t, run.err)
					if mode.name == "guard_failure" && site.name != "producer" {
						assertSyntaxErrorSpan(t, run.err, expectedOuterSpan)
					}
					if mode.name == "guard_failure" {
						guard, ok := snapshotGuardFailure(run.err)
						require.True(t, ok, "guard failures should remain typed under lowered execution")
						assert.Equal(t, "_", guard.Primary)
						assert.Equal(t, "probe", guard.Args)
						assert.Equal(t, Envelope{ExitCode: 17, Stdout: "probe-stdout", Stderr: "probe-stderr"}, guard.Envelope)
						assert.Equal(t, 1, callPrefixCount(run.calls, "probe"), "the failing helper should reach probe exactly once")
					} else {
						assert.Contains(t, run.err.Error(), "undefined variable")
						assert.NotContains(t, run.calls, execCall{Lane: "bind", Argv: "probe"})
					}
					assert.False(t, callSeen(run.calls, "print after_top"), "post-failure top-level continuation must not run")
					assert.False(t, callSeen(run.calls, "print after_inner"), "wrapper def continuation must not run after the failing helper call")
				}
				if mode.name == "syntax_error" && site.name != "defer" {
					assertSyntaxErrorFile(t, run.err, "library.bpfman")
				}
			})
		}
	}
}

func runSourceSequence(t *testing.T, sources []scriptSource, schedule map[string]int) sourceRun {
	t.Helper()

	failures := cloneFailureSchedule(schedule)
	var calls []execCall
	env := &Env{
		Session: NewSession(),
		ExecCommand: func(args []Arg, span source.Span) (Value, error) {
			calls = append(calls, execCall{Lane: "command", Argv: renderArgv(args)})
			return Value{}, nil
		},
		ExecBind: func(args []Arg, span source.Span) (BindResult, error) {
			calls = append(calls, execCall{Lane: "bind", Argv: renderArgv(args)})
			head := commandHead(args)
			if failures[head] > 0 {
				failures[head]--
				return BindResult{Rc: scriptedCommandEnvelope(head)}, nil
			}
			return BindResult{Rc: OkEnvelope()}, nil
		},
		RenderDeferFailure: func(source.Pos, []Arg, Envelope) {},
	}

	var runErr error
	for i, unit := range sources {
		tokens, err := syntax.TokeniseAt(source.Pos{File: unit.file, Line: unit.startLine, Col: 1}, unit.src)
		require.NoError(t, err)
		prog, err := syntax.Parse(tokens)
		require.NoError(t, err)
		lp, err := lowerToIR(prog)
		require.NoError(t, err)
		runErr = Exec(lp, env)
		if i < len(sources)-1 {
			require.NoError(t, runErr, "source %d (%s) should register helpers cleanly", i, unit.file)
		}
	}

	return sourceRun{env: env, calls: calls, err: runErr}
}

func callSeen(calls []execCall, want string) bool {
	for _, call := range calls {
		if call.Argv == want {
			return true
		}
	}
	return false
}

func assertSyntaxErrorFile(t *testing.T, err error, want string) {
	t.Helper()

	var se *syntax.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("expected SyntaxError, got %T", err)
	}

	assert.Equal(t, want, se.Span.Pos.File)
}

func assertSyntaxErrorSpan(t *testing.T, err error, want source.Span) {
	t.Helper()

	var se *syntax.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("expected SyntaxError, got %T", err)
	}

	assert.Equal(t, want, se.Span)
}

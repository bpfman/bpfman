package runtime

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

type bindCollectRun struct {
	env   *Env
	calls []execCall
	err   error
}

type fakeLinkRecord struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func TestBindCollect_WorkflowMatrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		src    string
		names  []string
		assert func(*testing.T, *Env, []execCall, error)
	}{
		{
			name: "filter_break_continue",
			src: "guard xs <- foreach n in [1 2 3 4] {\n" +
				"  if $n == 2 { continue }\n" +
				"  if $n == 4 { break }\n" +
				"  probe $n\n" +
				"}\n" +
				"foreach x in $xs {\n" +
				"  print $x\n" +
				"}\n",
			names: []string{"xs"},
			assert: func(t *testing.T, env *Env, calls []execCall, err error) {
				require.NoError(t, err)
				assert.Equal(t, "[\"probe-1\",\"probe-3\"]", captureBindings(t, env, []string{"xs"})["xs"])
				assert.Equal(t, 2, callPrefixCount(calls, "probe "))
			},
		},
		{
			name: "let_outcome_results_and_values",
			src: "let r <- foreach n in [1 2] { probe $n }\n" +
				"foreach x in $r.values {\n" +
				"  print $x\n" +
				"}\n",
			names: []string{"r"},
			assert: func(t *testing.T, env *Env, calls []execCall, err error) {
				require.NoError(t, err)
				r := mustBindingMap(t, env, "r")
				assert.Equal(t, true, r["ok"])
				results := r["results"].([]any)
				require.Len(t, results, 2)
				for _, entry := range results {
					m := entry.(map[string]any)
					assert.Equal(t, true, m["ok"])
					assert.Equal(t, 0, mustReadInt(t, m["exit_code"]))
				}
				assert.Equal(t, []any{"probe-1", "probe-2"}, r["values"])
			},
		},
		{
			name: "guard_fail_stops_iteration",
			src: "guard xs <- foreach n in [1 2 3] { maybe $n }\n" +
				"print after\n",
			assert: func(t *testing.T, env *Env, calls []execCall, err error) {
				require.Error(t, err)
				assert.Equal(t, 2, callPrefixCount(calls, "maybe "))
				assert.False(t, callSeen(calls, "print after"))
			},
		},
		{
			name: "helper_structured_followon_field_access",
			src: "def mk(n) {\n" +
				"  guard rec <- record $n\n" +
				"  return $rec\n" +
				"}\n" +
				"guard xs <- foreach n in [1 2] { mk $n }\n" +
				"foreach item in $xs {\n" +
				"  print $item.id\n" +
				"  print $item.name\n" +
				"}\n",
			names: []string{"xs"},
			assert: func(t *testing.T, env *Env, calls []execCall, err error) {
				require.NoError(t, err)
				assert.Equal(t, "[{\"id\":1,\"name\":\"record-1\"},{\"id\":2,\"name\":\"record-2\"}]", captureBindings(t, env, []string{"xs"})["xs"])
				assert.Equal(t, 4, callPrefixCount(calls, "print "))
			},
		},
		{
			name: "typed_origin_survives_followon_command",
			src: "def mk(n) {\n" +
				"  guard rec <- link $n\n" +
				"  return $rec\n" +
				"}\n" +
				"guard xs <- foreach n in [1 2] { mk $n }\n" +
				"foreach item in $xs {\n" +
				"  guard _ <- accept $item\n" +
				"}\n" +
				"print done\n",
			names: []string{"xs"},
			assert: func(t *testing.T, env *Env, calls []execCall, err error) {
				require.NoError(t, err)
				assert.Equal(t, 2, callPrefixCount(calls, "accept "))
				assert.True(t, callSeen(calls, "print done"))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			run := runBindCollectWorkflow(t, tc.src)
			tc.assert(t, run.env, run.calls, run.err)
		})
	}
}

func TestLetBind_TypedProviderBindsOutcomeWithValue(t *testing.T) {
	t.Parallel()

	src := "let r <- link 7\n"
	run := runBindCollectWorkflow(t, src)
	require.NoError(t, run.err)

	r := mustBindingMap(t, run.env, "r")
	assert.Equal(t, true, r["ok"])
	value := r["value"].(map[string]any)
	assert.Equal(t, 7, mustReadInt(t, value["id"]))
	assert.Equal(t, "link-7", value["name"])
}

func TestLetBind_TypedProviderFailureBindsOutcomeWithoutValue(t *testing.T) {
	t.Parallel()

	src := "let r <- maybe 2\n"
	run := runBindCollectWorkflow(t, src)
	require.NoError(t, run.err)

	r := mustBindingMap(t, run.env, "r")
	assert.Equal(t, false, r["ok"])
	assert.Equal(t, 19, mustReadInt(t, r["exit_code"]))
	assert.Equal(t, "blocked", r["stderr"])
	assert.NotContains(t, r, "value")
}

func TestBindCollect_LetBindsAggregateOutcome(t *testing.T) {
	t.Parallel()

	src := "let r <- foreach n in [1 2 3] { maybe $n }\n"
	run := runBindCollectWorkflow(t, src)
	require.NoError(t, run.err)

	r := mustBindingMap(t, run.env, "r")
	assert.Equal(t, false, r["ok"])

	results := r["results"].([]any)
	require.Len(t, results, 3)
	assert.Equal(t, true, results[0].(map[string]any)["ok"])
	assert.Equal(t, false, results[1].(map[string]any)["ok"])
	assert.Equal(t, true, results[2].(map[string]any)["ok"])

	values := r["values"].([]any)
	require.Len(t, values, 2)
	assert.Equal(t, "maybe-1", values[0])
	assert.Equal(t, "maybe-3", values[1])
}

func TestBindCollect_EmptyLetOutcomeIsOk(t *testing.T) {
	t.Parallel()

	src := "let r <- foreach n in [] { maybe $n }\n"
	run := runBindCollectWorkflow(t, src)
	require.NoError(t, run.err)

	r := mustBindingMap(t, run.env, "r")
	assert.Equal(t, true, r["ok"])
	assert.Empty(t, r["results"].([]any))
	assert.Empty(t, r["values"].([]any))
}

func runBindCollectWorkflow(t *testing.T, src string) bindCollectRun {
	t.Helper()

	var calls []execCall
	env := &Env{
		Session: NewSession(),
		ExecCommand: func(args []Arg, span source.Span) (Value, error) {
			calls = append(calls, execCall{Lane: "command", Argv: renderArgv(args)})
			return Value{}, nil
		},
		RenderDeferFailure: func(source.Pos, []Arg, Envelope) {},
	}
	env.ExecBind = func(args []Arg, span source.Span) (BindResult, error) {
		calls = append(calls, execCall{Lane: "bind", Argv: renderArgv(args)})
		return execBindCollectRuntime(args)
	}

	prog := parseProgram(t, src)
	lp, err := lowerToIR(prog)
	require.NoError(t, err)
	runErr := Exec(lp, env)
	return bindCollectRun{env: env, calls: calls, err: runErr}
}

func execBindCollectRuntime(args []Arg) (BindResult, error) {
	head := commandHead(args)
	switch head {
	case "probe":
		n := mustArgInt(args, 1)
		return BindResult{Rc: OkEnvelope(), Primary: StringValue(fmt.Sprintf("probe-%d", n))}, nil
	case "maybe":
		n := mustArgInt(args, 1)
		if n == 2 {
			return BindResult{Rc: Envelope{ExitCode: 19, Stderr: "blocked"}}, nil
		}
		return BindResult{Rc: OkEnvelope(), Primary: StringValue(fmt.Sprintf("maybe-%d", n))}, nil
	case "record":
		n := mustArgInt(args, 1)
		return BindResult{
			Rc: OkEnvelope(),
			Primary: ValueFromAny(map[string]any{
				"id":   numFromInt(n),
				"name": fmt.Sprintf("record-%d", n),
			}),
		}, nil
	case "link":
		n := mustArgInt(args, 1)
		raw := map[string]any{
			"id":   numFromInt(n),
			"name": fmt.Sprintf("link-%d", n),
		}
		origin := fakeLinkRecord{ID: n, Name: fmt.Sprintf("link-%d", n)}
		return BindResult{Rc: OkEnvelope(), Primary: ValueFromAny(raw).withOrigin(origin, semantics.OriginUnknown)}, nil
	case "accept":
		if len(args) < 2 {
			return BindResult{Rc: FailEnvelopeFromError(fmt.Errorf("accept: expected value"))}, nil
		}
		switch v := args[1].(type) {
		case StructuredValueArg:
			if _, ok := v.Value.Origin().(fakeLinkRecord); ok {
				return BindResult{Rc: OkEnvelope(), Primary: ValueFromEnvelope(OkEnvelope())}, nil
			}
		case ScalarValueArg:
			if _, ok := v.Value.Origin().(fakeLinkRecord); ok {
				return BindResult{Rc: OkEnvelope(), Primary: ValueFromEnvelope(OkEnvelope())}, nil
			}
		}
		return BindResult{Rc: Envelope{ExitCode: 7, Stderr: "origin-lost"}}, nil
	default:
		return BindResult{Rc: OkEnvelope(), Primary: ValueFromEnvelope(OkEnvelope())}, nil
	}
}

func mustArgInt(args []Arg, idx int) int {
	if len(args) <= idx {
		panic("missing argument")
	}
	var text string
	switch v := args[idx].(type) {
	case WordArg:
		text = v.Text
	case QuotedArg:
		text = v.Text
	case ScalarValueArg:
		text = v.Text
	default:
		text = argText(args[idx])
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		panic(err)
	}

	return n
}

func callPrefixCount(calls []execCall, prefix string) int {
	count := 0
	for _, call := range calls {
		if strings.HasPrefix(call.Argv, prefix) {
			count++
		}
	}
	return count
}

package runtime

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

type jobWorkflowRun struct {
	env   *Env
	calls []execCall
	err   error
}

func TestJobs_WorkflowMatrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		src    string
		names  []string
		assert func(*testing.T, *Env)
	}{
		{
			name: "inline_wait",
			src: "guard p <- start sleep 60\n" +
				"let rc <- wait $p\n" +
				"print $p.pid\n" +
				"print $rc.ok\n" +
				"print $rc.exit_code\n",
			names: []string{"p", "rc"},
			assert: func(t *testing.T, env *Env) {
				assert.Equal(t, 0, env.Session.JobLeaks())
				rc := mustBindingMap(t, env, "rc")
				assert.Equal(t, true, rc["ok"])
				assert.Equal(t, 0, mustReadInt(t, rc["exit_code"]))
				assert.Equal(t, false, rc["killed"])
			},
		},
		{
			name: "helper_return_wait",
			src: "def spawn() {\n" +
				"  guard p <- start sleep 60\n" +
				"  return $p\n" +
				"}\n" +
				"guard p <- spawn\n" +
				"let rc <- wait $p\n" +
				"print $p.pid\n" +
				"print $rc.ok\n" +
				"print $rc.exit_code\n",
			names: []string{"p", "rc"},
			assert: func(t *testing.T, env *Env) {
				assert.Equal(t, 0, env.Session.JobLeaks())
				rc := mustBindingMap(t, env, "rc")
				assert.Equal(t, true, rc["ok"])
				assert.Equal(t, 0, mustReadInt(t, rc["exit_code"]))
			},
		},
		{
			name: "helper_defer_kill_then_wait",
			src: "def spawn() {\n" +
				"  guard p <- start sleep 60\n" +
				"  defer kill $p\n" +
				"  return $p\n" +
				"}\n" +
				"let result <- spawn\n" +
				"let p = $result.value\n" +
				"let rc <- wait $p\n" +
				"print $rc.killed\n" +
				"print $rc.signal\n" +
				"print $rc.exit_code\n",
			names: []string{"p", "rc"},
			assert: func(t *testing.T, env *Env) {
				assert.Equal(t, 0, env.Session.JobLeaks())
				rc := mustBindingMap(t, env, "rc")
				assert.Equal(t, false, rc["ok"])
				assert.Equal(t, true, rc["killed"])
				assert.Equal(t, "TERM", rc["signal"])
				assert.Equal(t, 143, mustReadInt(t, rc["exit_code"]))
			},
		},
		{
			name: "jobs_reap_preserves_running",
			src: "guard p <- start sleep 60\n" +
				"guard q <- start sleep 60\n" +
				"let done <- wait $p\n" +
				"guard live_before <- jobs\n" +
				"let _ <- reap\n" +
				"guard live_after <- jobs\n" +
				"let _ <- kill $q\n",
			names: []string{"p", "q", "done", "live_before", "live_after"},
			assert: func(t *testing.T, env *Env) {
				assert.Equal(t, 0, env.Session.JobLeaks())
				before := mustBindingList(t, env, "live_before")
				after := mustBindingList(t, env, "live_after")
				require.Len(t, before, 2)
				require.Len(t, after, 1)
				wantPID := mustReadInt(t, mustBindingMap(t, env, "q")["pid"])
				only := after[0].(map[string]any)
				assert.Equal(t, wantPID, mustReadInt(t, only["pid"]))
			},
		},
		{
			name: "kill_then_wait",
			src: "guard p <- start sleep 60\n" +
				"let killrc <- kill $p\n" +
				"let waitrc <- wait $p\n" +
				"print $killrc.killed\n" +
				"print $waitrc.killed\n",
			names: []string{"p", "killrc", "waitrc"},
			assert: func(t *testing.T, env *Env) {
				assert.Equal(t, 0, env.Session.JobLeaks())
				killrc := mustBindingMap(t, env, "killrc")
				waitrc := mustBindingMap(t, env, "waitrc")
				assert.Equal(t, true, killrc["killed"])
				assert.Equal(t, true, waitrc["killed"])
				assert.Equal(t, 143, mustReadInt(t, waitrc["exit_code"]))
			},
		},
		{
			name: "helper_leak",
			src: "def spawn() {\n" +
				"  guard p <- start sleep 60\n" +
				"  return $p\n" +
				"}\n" +
				"guard p <- spawn\n" +
				"print $p.pid\n",
			names: []string{"p"},
			assert: func(t *testing.T, env *Env) {
				assert.Equal(t, 1, env.Session.JobLeaks())
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			run := runJobWorkflow(t, tc.src)
			require.NoError(t, run.err)
			tc.assert(t, run.env)
		})
	}
}

func runJobWorkflow(t *testing.T, src string) jobWorkflowRun {
	t.Helper()

	runtime := &fakeJobRuntime{nextPID: 4100}
	sess := NewSession()
	rec := &jobLeakRecorder{session: sess}
	var calls []execCall
	env := &Env{
		Session:            sess,
		HandleJobLeak:      rec.handle,
		RenderDeferFailure: func(source.Pos, []Arg, Envelope) {},
		ExecCommand: func(args []Arg, span source.Span) (Value, error) {
			calls = append(calls, execCall{Lane: "command", Argv: renderArgv(args)})
			return Value{}, nil
		},
	}
	env.ExecBind = func(args []Arg, span source.Span) (BindResult, error) {
		calls = append(calls, execCall{Lane: "bind", Argv: renderArgv(args)})
		return runtime.exec(env, args, span)
	}

	prog := parseProgram(t, src)
	lp, err := lowerToIR(prog)
	require.NoError(t, err)
	runErr := Exec(lp, env)
	return jobWorkflowRun{env: env, calls: calls, err: runErr}
}

type fakeJobRuntime struct {
	nextPID int
}

func (rt *fakeJobRuntime) exec(env *Env, args []Arg, span source.Span) (BindResult, error) {
	head := commandHead(args)
	switch head {
	case "start":
		job := &Job{
			PID:          rt.nextPID,
			Done:         make(chan struct{}),
			Args:         argvTexts(args[1:]),
			Origin:       fmt.Sprintf("%s:%d", span.Pos.File, span.Pos.Line),
			TargetBinary: firstArgText(args[1:]),
		}
		rt.nextPID++
		env.RegisterJob(job)
		return BindResult{Rc: OkEnvelope(), Primary: ValueFromJob(job)}, nil
	case "wait":
		job, err := jobFromArgs(args)
		if err != nil {
			return BindResult{}, err
		}

		job.MarkManaged()
		if !jobDone(job) {
			job.ExitCode = 0
			close(job.Done)
		}
		rc := Envelope{
			ExitCode: job.ExitCode,
			Stdout:   job.Stdout,
			Stderr:   job.Stderr,
			Killed:   job.Killed,
			Signal:   job.Signal,
			HasPID:   true,
			PID:      job.PID,
		}
		return BindResult{Rc: rc, Primary: ValueFromEnvelope(rc)}, nil
	case "kill":
		job, err := jobFromArgs(args)
		if err != nil {
			return BindResult{}, err
		}

		job.MarkManaged()
		job.Killed = true
		job.Signal = "TERM"
		job.ExitCode = 143
		if !jobDone(job) {
			close(job.Done)
		}
		rc := Envelope{
			ExitCode: 143,
			Killed:   true,
			Signal:   "TERM",
			HasPID:   true,
			PID:      job.PID,
		}
		return BindResult{Rc: rc, Primary: ValueFromEnvelope(rc)}, nil
	case "jobs":
		live := env.ActiveJobs()
		out := make([]any, 0, len(live))
		for _, job := range live {
			out = append(out, ValueFromJob(job).Raw())
		}
		return BindResult{Rc: OkEnvelope(), Primary: ValueFromAny(out)}, nil
	case "reap":
		env.ReapJobs(func(job *Job) bool { return jobDone(job) })
		rc := OkEnvelope()
		return BindResult{Rc: rc, Primary: ValueFromEnvelope(rc)}, nil
	default:
		return BindResult{Rc: OkEnvelope(), Primary: ValueFromEnvelope(OkEnvelope())}, nil
	}
}

func jobFromArgs(args []Arg) (*Job, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("expected job argument")
	}
	switch v := args[1].(type) {
	case StructuredValueArg:
		job, ok := v.Value.Origin().(*Job)
		if !ok || job == nil {
			return nil, fmt.Errorf("expected job argument")
		}
		return job, nil
	case ScalarValueArg:
		job, ok := v.Value.Origin().(*Job)
		if !ok || job == nil {
			return nil, fmt.Errorf("expected job argument")
		}
		return job, nil
	default:
		return nil, fmt.Errorf("expected job argument")
	}
}

func jobDone(job *Job) bool {
	select {
	case <-job.Done:
		return true
	default:
		return false
	}
}

func argvTexts(args []Arg) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, argText(arg))
	}
	return out
}

func firstArgText(args []Arg) string {
	if len(args) == 0 {
		return ""
	}
	return argText(args[0])
}

func captureBindings(t *testing.T, env *Env, names []string) map[string]string {
	t.Helper()

	out := make(map[string]string, len(names))
	for _, name := range names {
		v, ok := env.Session.Get(name)
		if !ok {
			out[name] = "<missing>"
			continue
		}

		rendered, err := RenderCompact(v)
		require.NoError(t, err)
		out[name] = rendered
	}
	return out
}

func mustBindingMap(t *testing.T, env *Env, name string) map[string]any {
	t.Helper()

	v, ok := env.Session.Get(name)
	require.True(t, ok, "missing binding %q", name)
	m, ok := v.Raw().(map[string]any)
	require.True(t, ok, "binding %q is %T, want map[string]any", name, v.Raw())
	return m
}

func mustBindingList(t *testing.T, env *Env, name string) []any {
	t.Helper()

	v, ok := env.Session.Get(name)
	require.True(t, ok, "missing binding %q", name)
	list, ok := v.Raw().([]any)
	require.True(t, ok, "binding %q is %T, want []any", name, v.Raw())
	return list
}

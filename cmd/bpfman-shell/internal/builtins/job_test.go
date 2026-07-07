package builtins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

// startCall wraps args in a minimal driver.Ctx for handleStart.
// Tests drive the spawn path without going through the dispatch
// boundary, so Ctx is the only context field they need; Env is
// left nil because none of these tests register the spawned job
// in a scope.
func startCall(t *testing.T, args []runtime.Arg) (runtime.Value, error) {
	return startCallContext(t.Context(), args)
}

func startCallContext(ctx context.Context, args []runtime.Arg) (runtime.Value, error) {
	return handleStart(driver.Ctx{Ctx: ctx, Args: args})
}

// waitForJob blocks until the job exits or the timeout fires.
// Tests use a generous timeout: the OS-level signal-to-exit path is
// microseconds and a healthy single-test run finishes in well under
// a second, but under -race plus heavy CI contention the Go runtime
// can take seconds to schedule the reaper goroutine that closes
// Done after the child has actually exited. 15s absorbs that
// scheduling lag while still failing fast on a genuine hang
// (compared to a real hang's open-ended duration).
func waitForJob(t *testing.T, j *runtime.Job) {
	t.Helper()
	select {
	case <-j.Done:
	case <-time.After(15 * time.Second):
		t.Fatalf("job pid %d did not exit within timeout", j.PID)
	}
}

// waitForFile blocks until path exists or timeout elapses. Used by
// the kill tests to synchronise with a sentinel the spawned shell
// drops after it has fully entered its long-running sleep: without
// the wait, kill races the shell's "trap registered but sleep child
// not yet forked" window. In that window SIGUSR1 reaches the shell
// (trap fires, queues `exit 17`) but the not-yet-forked sleep child
// misses the group signal, so the shell's subsequent wait4 blocks
// for sleep's full duration and the test times out.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("sentinel file %s not created within timeout", path)
}

func TestJobStart_SpawnsAndCapturesStdout(t *testing.T) {
	t.Parallel()

	val, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.WordArg{Text: "echo hello"},
	})
	require.NoError(t, err)
	assert.Equal(t, semantics.OriginJob, val.Kind())

	job, ok := val.Origin().(*runtime.Job)
	require.True(t, ok, "Origin should be *runtime.Job, got %T", val.Origin())
	assert.Greater(t, job.PID, 0, "PID should be set")
	assert.Equal(t, []string{"sh", "-c", "echo hello"}, job.Args)

	waitForJob(t, job)

	job.Mu.Lock()
	defer job.Mu.Unlock()
	assert.Equal(t, "hello\n", job.Stdout)
	assert.Empty(t, job.Stderr)
	assert.Equal(t, 0, job.ExitCode)
}

func TestJobStart_NonZeroExitCodeCaptured(t *testing.T) {
	t.Parallel()

	val, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.WordArg{Text: "echo boom 1>&2; exit 7"},
	})
	require.NoError(t, err)
	job := val.Origin().(*runtime.Job)

	waitForJob(t, job)

	job.Mu.Lock()
	defer job.Mu.Unlock()
	assert.Empty(t, job.Stdout)
	assert.Equal(t, "boom\n", job.Stderr)
	assert.Equal(t, 7, job.ExitCode)
}

func TestJobStart_NoArgsIsError(t *testing.T) {
	t.Parallel()

	_, err := startCall(t, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start requires at least one argument")
}

func TestJobStart_StructuredArgRejected(t *testing.T) {
	t.Parallel()

	// A program-typed Value cannot flatten into argv text, the
	// same constraint exec applies via driver.RunExternal.
	prog := runtime.ValueFromMap(map[string]any{"id": "42"}).WithKind(semantics.OriginProgram)
	_, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "echo"},
		runtime.StructuredValueArg{Name: "prog", Value: prog},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "argument 2 is a program value")
}

func TestJobStart_LaunchFailureIsStructuralError(t *testing.T) {
	t.Parallel()

	// A non-existent binary fails at Start(), not after the
	// process runs. The error path produces no Job: this is
	// 'structural failure' and propagates back to halt the bind
	// rather than landing in a not-ok envelope.
	_, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "__definitely_not_a_real_command_2026__"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start __definitely_not_a_real_command_2026__")
}

func TestJobWait_BlocksAndCapturesEnvelope(t *testing.T) {
	t.Parallel()

	val, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.WordArg{Text: "echo hello; sleep 0.05; echo bye"},
	})
	require.NoError(t, err)
	job := val.Origin().(*runtime.Job)

	env, err := WaitEnvelope(t.Context(), []runtime.Arg{
		runtime.StructuredValueArg{Name: "job", Value: val},
	})
	require.NoError(t, err)
	assert.True(t, env.OK(), "successful exit -> ok envelope")
	assert.Equal(t, 0, env.ExitCode)
	assert.Equal(t, "hello\nbye\n", env.Stdout)
	assert.Empty(t, env.Stderr)
	assert.True(t, job.IsManaged(), "wait must mark the job managed")
}

func TestJobWait_AfterAlreadyCompleted(t *testing.T) {
	t.Parallel()

	// 'start ls /run' may exit before this goroutine reaches
	// WaitEnvelope. The cached envelope must still be returned.
	val, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.WordArg{Text: "echo done"},
	})
	require.NoError(t, err)
	job := val.Origin().(*runtime.Job)

	// Drain the reaper before WaitEnvelope sees it. After this
	// point the job is in the 'completed' state from your
	// state machine: result cached, Done closed, Managed
	// still false.
	waitForJob(t, job)
	require.False(t, job.IsManaged())

	env, err := WaitEnvelope(t.Context(), []runtime.Arg{
		runtime.StructuredValueArg{Name: "job", Value: val},
	})
	require.NoError(t, err)
	assert.True(t, env.OK())
	assert.Equal(t, "done\n", env.Stdout)
	assert.True(t, job.IsManaged(), "wait on a completed job still marks it managed")
}

func TestJobWait_NonZeroExitProducesNotOk(t *testing.T) {
	t.Parallel()

	val, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.WordArg{Text: "exit 7"},
	})
	require.NoError(t, err)

	env, err := WaitEnvelope(t.Context(), []runtime.Arg{
		runtime.StructuredValueArg{Name: "job", Value: val},
	})
	require.NoError(t, err)
	assert.False(t, env.OK(), "non-zero exit -> not ok")
	assert.Equal(t, 7, env.ExitCode)
}

func TestJobWait_ContextCancelReturnsNotOk(t *testing.T) {
	t.Parallel()

	val, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.WordArg{Text: "sleep 60"},
	})
	require.NoError(t, err)
	job := val.Origin().(*runtime.Job)

	// Cancel the wait's context before the long sleep
	// finishes. wait should return promptly with a not-ok
	// envelope citing the cancellation reason. The
	// underlying process keeps running until syscall.Kill or
	// the start ctx ends; tests clean up by killing
	// directly.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	env, err := WaitEnvelope(ctx, []runtime.Arg{
		runtime.StructuredValueArg{Name: "job", Value: val},
	})
	require.NoError(t, err)
	assert.False(t, env.OK())
	assert.Equal(t, -1, env.ExitCode)
	assert.Contains(t, env.Stderr, "context canceled")

	// Tear down the still-running process so the test does
	// not leak a sleep into the suite.
	_ = syscall.Kill(-job.PID, syscall.SIGKILL)
	waitForJob(t, job)
}

func TestJobStart_ContextCancelInterruptsGrandchildInProcessGroup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	ack := filepath.Join(dir, "ack")
	grandchild := filepath.Join(dir, "grandchild.sh")
	require.NoError(t, os.WriteFile(grandchild, []byte(`#!/bin/sh
trap 'echo interrupted > "$1"; exit 0' INT
echo ready > "$2"
sleep 5
`), 0o755))

	ctx, cancel := context.WithCancel(t.Context())
	val, err := startCallContext(ctx, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.QuotedArg{Text: `"$1" "$2" "$3"; :`},
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: grandchild},
		runtime.WordArg{Text: ack},
		runtime.WordArg{Text: ready},
	})
	require.NoError(t, err)
	job := val.Origin().(*runtime.Job)
	defer func() {
		_ = syscall.Kill(-job.PID, syscall.SIGKILL)
		waitForJob(t, job)
	}()

	waitForFile(t, ready)
	cancel()

	assert.Eventually(t, func() bool {
		_, statErr := os.Stat(ack)
		return statErr == nil
	}, time.Second, 20*time.Millisecond)
}

func TestJobStart_ContextCancelRecordsInterruptSignal(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	val, err := startCallContext(ctx, []runtime.Arg{
		runtime.WordArg{Text: "sleep"},
		runtime.WordArg{Text: "5"},
	})
	require.NoError(t, err)
	job := val.Origin().(*runtime.Job)

	cancel()
	waitForJob(t, job)

	job.Mu.Lock()
	defer job.Mu.Unlock()
	assert.Equal(t, "INT", job.Signal)
	assert.Equal(t, 130, job.ExitCode)
}

func TestJobWait_RejectsNonJobArg(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		arg  runtime.Arg
	}{
		{"plain word", runtime.WordArg{Text: "hello"}},
		{"non-job structured", runtime.StructuredValueArg{
			Name:  "prog",
			Value: runtime.ValueFromMap(nil).WithKind(semantics.OriginProgram),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := WaitEnvelope(t.Context(), []runtime.Arg{tc.arg})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "$job")
		})
	}
}

func TestJobWait_NoArgsIsError(t *testing.T) {
	t.Parallel()

	_, err := WaitEnvelope(t.Context(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one argument")
}

func TestJobKill_TerminatesAndMarksManaged(t *testing.T) {
	t.Parallel()

	val, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.WordArg{Text: "sleep 60"},
	})
	require.NoError(t, err)
	job := val.Origin().(*runtime.Job)

	env, err := KillEnvelope(t.Context(), []runtime.Arg{
		runtime.StructuredValueArg{Name: "job", Value: val},
	})
	require.NoError(t, err)
	assert.True(t, env.OK(), "kill that delivered a signal is ok")
	assert.True(t, job.IsManaged(), "kill marks the job managed")

	waitForJob(t, job)
	job.Mu.Lock()
	defer job.Mu.Unlock()
	assert.True(t, job.Killed, "Killed flag persists for wait to read")
}

func TestJobKill_KilledThenWaitReportsLifecycle(t *testing.T) {
	t.Parallel()

	// 'ok' is tied to "exit code 0", so a killed job that did
	// not return zero reports !ok. The lifecycle facts are
	// carried by 'killed' and 'signal', and 'exit_code' uses the
	// shell convention 128+signum for a SIGTERM kill.
	val, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.WordArg{Text: "sleep 60"},
	})
	require.NoError(t, err)

	_, err = KillEnvelope(t.Context(), []runtime.Arg{
		runtime.StructuredValueArg{Name: "job", Value: val},
	})
	require.NoError(t, err)

	env, err := WaitEnvelope(t.Context(), []runtime.Arg{
		runtime.StructuredValueArg{Name: "job", Value: val},
	})
	require.NoError(t, err)
	assert.False(t, env.OK(), "killed != ok; ok stays tied to exit-code-0")
	assert.True(t, env.Killed, "killed flag carries the lifecycle fact")
	assert.Equal(t, "TERM", env.Signal)
	assert.Equal(t, 128+15, env.ExitCode, "shell convention: SIGTERM -> 143")
}

func TestJobKill_AlreadyExitedIsOk(t *testing.T) {
	t.Parallel()

	// 'kill' is best-effort: if the process exited on its
	// own before kill landed, ESRCH is treated as success.
	val, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.WordArg{Text: "exit 0"},
	})
	require.NoError(t, err)
	job := val.Origin().(*runtime.Job)
	waitForJob(t, job)

	env, err := KillEnvelope(t.Context(), []runtime.Arg{
		runtime.StructuredValueArg{Name: "job", Value: val},
	})
	require.NoError(t, err)
	assert.True(t, env.OK(), "kill against already-exited job is ok (ESRCH swallowed)")
}

func TestJobKill_SignalFlag(t *testing.T) {
	t.Parallel()

	// '--signal=USR1' overrides the SIGTERM default. The
	// shell traps USR1 and exits with code 42 so the test can
	// confirm the signal was delivered without relying on
	// signal-status reporting. The kill envelope reports ok
	// because the signal was successfully delivered (kill
	// itself is "did the signal go through?", not "did the
	// target terminate cleanly?").
	val, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.WordArg{Text: "trap 'exit 42' USR1; sleep 60"},
	})
	require.NoError(t, err)
	job := val.Origin().(*runtime.Job)

	// Give the trap a moment to install before signalling;
	// otherwise the USR1 may arrive while sh is still
	// initialising and the trap has no effect.
	time.Sleep(50 * time.Millisecond)

	env, err := KillEnvelope(t.Context(), []runtime.Arg{
		runtime.WordArg{Text: "--signal=USR1"},
		runtime.StructuredValueArg{Name: "job", Value: val},
	})
	require.NoError(t, err)
	assert.True(t, env.OK(), "kill itself succeeded (signal delivered)")

	waitForJob(t, job)
	job.Mu.Lock()
	defer job.Mu.Unlock()
	assert.Equal(t, 42, job.ExitCode, "trap fired -> the chosen signal was delivered")
	assert.True(t, job.Killed)
	assert.Equal(t, "USR1", job.Signal)
}

func TestJobKill_DefaultPathBlocksUntilReaped(t *testing.T) {
	t.Parallel()

	// The contract: 'kill $job' returns only after the
	// reaper has closed Done. Even on a process that exits
	// immediately on SIGTERM (no escalation needed), the call
	// must not return earlier than the Done close.
	val, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.WordArg{Text: "sleep 30"},
	})
	require.NoError(t, err)
	job := val.Origin().(*runtime.Job)

	env, err := KillEnvelope(t.Context(), []runtime.Arg{
		runtime.StructuredValueArg{Name: "job", Value: val},
	})
	require.NoError(t, err)
	assert.True(t, env.OK())

	// Done must already be closed: the kill builtin returned,
	// so the reaper has settled the job.
	select {
	case <-job.Done:
	default:
		t.Fatalf("kill returned but job.Done is still open; sync contract broken")
	}
	assert.True(t, job.IsManaged())
}

func TestJobKill_GraceZeroSendsKillImmediately(t *testing.T) {
	t.Parallel()

	// --grace=0 skips the wait between SIGTERM and SIGKILL.
	// The job's Signal field, set by the escalation path,
	// reflects KILL once kill returns.
	val, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.WordArg{Text: "sleep 30"},
	})
	require.NoError(t, err)
	job := val.Origin().(*runtime.Job)

	env, err := KillEnvelope(t.Context(), []runtime.Arg{
		runtime.WordArg{Text: "--grace=0"},
		runtime.StructuredValueArg{Name: "job", Value: val},
	})
	require.NoError(t, err)
	assert.True(t, env.OK())

	select {
	case <-job.Done:
	default:
		t.Fatalf("kill --grace=0 returned but job.Done is still open")
	}
	job.Mu.Lock()
	defer job.Mu.Unlock()
	assert.Equal(t, "KILL", job.Signal, "--grace=0 takes the escalation path and ends on SIGKILL")
}

func TestJobKill_CustomSignalSkipsEscalation(t *testing.T) {
	t.Parallel()

	// A custom --signal=NAME is a control-flow signal, not a
	// termination request. kill must deliver and return; it
	// must not wait for grace and must not escalate to SIGKILL.
	// The Signal field reflects the requested signal, not KILL.
	//
	// The shell signals readiness by touching `ready` after it
	// has forked the sleep child into the background. Sending
	// USR1 before that touch races a documented bash window: the
	// trap is installed but sleep is not yet forked, so the
	// group-signal reaches bash (which queues `exit 17`) but
	// misses sleep, and bash's subsequent wait4 blocks for
	// sleep's full duration. Synchronising on the sentinel
	// closes that window.
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	script := fmt.Sprintf(
		"trap 'exit 17' USR1\n"+
			"sleep 30 &\n"+
			"touch %s\n"+
			"wait\n",
		ready)
	val, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.WordArg{Text: script},
	})
	require.NoError(t, err)
	job := val.Origin().(*runtime.Job)

	waitForFile(t, ready)

	env, err := KillEnvelope(t.Context(), []runtime.Arg{
		runtime.WordArg{Text: "--signal=USR1"},
		runtime.StructuredValueArg{Name: "job", Value: val},
	})
	require.NoError(t, err)
	assert.True(t, env.OK())

	// Reap separately, then assert Signal stayed as USR1
	// (escalation would have rewritten to KILL).
	waitForJob(t, job)
	job.Mu.Lock()
	defer job.Mu.Unlock()
	assert.Equal(t, "USR1", job.Signal, "custom signal must not be overwritten by escalation")
}

func TestJobKill_NegativeGraceIsError(t *testing.T) {
	t.Parallel()

	_, err := KillEnvelope(t.Context(), []runtime.Arg{
		runtime.WordArg{Text: "--grace=-1s"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--grace")
}

func TestJobKill_MalformedGraceIsError(t *testing.T) {
	t.Parallel()

	_, err := KillEnvelope(t.Context(), []runtime.Arg{
		runtime.WordArg{Text: "--grace=banana"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--grace")
}

func TestJobKill_UnknownSignalIsError(t *testing.T) {
	t.Parallel()

	_, err := KillEnvelope(t.Context(), []runtime.Arg{
		runtime.WordArg{Text: "--signal=NOSUCHSIG"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown signal")
}

func TestJobKill_RejectsNonJobArg(t *testing.T) {
	t.Parallel()

	_, err := KillEnvelope(t.Context(), []runtime.Arg{
		runtime.WordArg{Text: "not-a-job"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "$job")
}

func TestJobKill_NoArgsIsError(t *testing.T) {
	t.Parallel()

	_, err := KillEnvelope(t.Context(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kill requires a $job argument")
}

func TestJobStart_ProcessGroupIsSet(t *testing.T) {
	t.Parallel()

	// The child runs in its own process group so 'kill' can
	// later signal the whole group. Verify by reading
	// /proc/<pid>/stat which exposes pgid as the fifth field.
	val, err := startCall(t, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.WordArg{Text: "sleep 0.1"},
	})
	require.NoError(t, err)
	job := val.Origin().(*runtime.Job)
	defer waitForJob(t, job)

	// While the process is alive, its pgid must equal its own
	// PID (Setpgid: true makes the child its own group leader).
	pgid, err := syscall.Getpgid(job.PID)
	require.NoError(t, err)
	assert.Equal(t, job.PID, pgid, "child should be its own process-group leader")
}

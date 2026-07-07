package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

// jobLeakRecorder captures the Jobs that HandleJobLeak fires
// on and bumps the session leak counter, mirroring the
// strict-mode driver contract: the shell layer does not record
// a leak itself; the handler decides whether the leak counts.
// Tests use this stand-in to assert both the per-leak callback
// and the session counter without rebuilding the whole
// driver-side renderer.
type jobLeakRecorder struct {
	session *Session
	leaks   []*Job
}

func (r *jobLeakRecorder) handle(j *Job) {
	r.leaks = append(r.leaks, j)
	if r.session != nil {
		r.session.RecordJobLeak()
	}
}

func TestRunWithDeferScope_UnmanagedJobReported(t *testing.T) {
	t.Parallel()

	sess := NewSession()
	rec := &jobLeakRecorder{session: sess}
	env := &Env{
		Session:       sess,
		HandleJobLeak: rec.handle,
	}

	job := &Job{PID: 4242, Args: []string{"sleep", "60"}, Origin: "test.bpfman:7"}
	err := withJobScope(env, func() error {
		env.RegisterJob(job)
		return nil
	})
	require.NoError(t, err)

	require.Len(t, rec.leaks, 1, "an unmanaged job should be reported once")
	assert.Same(t, job, rec.leaks[0])
	assert.Equal(t, 1, env.Session.JobLeaks(), "session counter should reflect the leak")
}

func TestRunWithDeferScope_ManagedJobNotReported(t *testing.T) {
	t.Parallel()

	sess := NewSession()
	rec := &jobLeakRecorder{session: sess}
	env := &Env{
		Session:       sess,
		HandleJobLeak: rec.handle,
	}

	job := &Job{PID: 4242, Args: []string{"sleep", "60"}}
	err := withJobScope(env, func() error {
		env.RegisterJob(job)
		// Simulate the script having waited or killed the job.
		job.MarkManaged()
		return nil
	})
	require.NoError(t, err)

	assert.Empty(t, rec.leaks, "a managed job must not fire HandleJobLeak")
	assert.Equal(t, 0, env.Session.JobLeaks(), "session counter must stay at zero")
}

func TestRunWithDeferScope_DeferKillRunsBeforeLeakCheck(t *testing.T) {
	t.Parallel()

	// Models 'defer kill $job': the deferred command marks the
	// job Managed, so the post-defers leak walk must see the
	// updated state and skip the job.
	sess := NewSession()
	rec := &jobLeakRecorder{session: sess}
	job := &Job{PID: 4242, Args: []string{"sleep", "60"}}

	env := &Env{
		Session:       sess,
		HandleJobLeak: rec.handle,
		ExecBind: func(args []Arg, _ source.Span) (BindResult, error) {
			job.MarkManaged()
			return BindResult{Rc: Envelope{}}, nil
		},
	}

	// Compose 'WithJobScope { WithDeferScope { body } }' the
	// same way the drivers do. Inner defer scope unwinds first
	// (so 'defer kill' marks the job), outer job scope unwinds
	// after (so the leak walk sees the updated state).
	err := withJobScope(env, func() error {
		return withDeferScope(env, func() error {
			env.RegisterJob(job)
			// Stand-in for 'defer kill $job': any deferred
			// entry suffices because the test ExecBind
			// unconditionally marks the job Managed when
			// the defer fires.
			*env.defers = append(*env.defers, deferEntry{
				Args:   []Arg{WordArg{Text: "kill"}},
				policy: ir.DispatchPolicyDefThenExecBind,
			})
			return nil
		})
	})
	require.NoError(t, err)

	assert.True(t, job.IsManaged(), "defer must have run and marked the job")
	assert.Empty(t, rec.leaks, "defer-marked job must not be reported as a leak")
	assert.Equal(t, 0, env.Session.JobLeaks())
}

func TestWithJobScope_NestedJobScopesAreIndependent(t *testing.T) {
	t.Parallel()

	// Two explicit job scopes: each fires its own leak walk
	// when it unwinds. Nesting is rare in practice (drivers
	// open one outer job scope per session unit) but the
	// mechanism must compose for any embedder that wants
	// finer-grained tracking.
	sess := NewSession()
	rec := &jobLeakRecorder{session: sess}
	inner := &Job{PID: 1, Args: []string{"inner"}}
	outer := &Job{PID: 2, Args: []string{"outer"}}

	env := &Env{
		Session:       sess,
		HandleJobLeak: rec.handle,
	}

	err := withJobScope(env, func() error {
		env.RegisterJob(outer)
		return withJobScope(env, func() error {
			env.RegisterJob(inner)
			return nil
		})
	})
	require.NoError(t, err)

	require.Len(t, rec.leaks, 2, "both scopes leak their own job")
	assert.Same(t, inner, rec.leaks[0], "inner scope reports first (unwinds first)")
	assert.Same(t, outer, rec.leaks[1])
	assert.Equal(t, 2, env.Session.JobLeaks())
}

func TestWithJobScope_DefBodyDoesNotOpenNewJobScope(t *testing.T) {
	t.Parallel()

	// A def body opens its own defer scope but inherits the
	// caller's job scope: a job started inside a def joins the
	// caller's registry, and returning the handle for the
	// caller to wait does not leak. Models the
	// 'WithJobScope { ... WithDeferScope { def body } ... }'
	// driver shape.
	sess := NewSession()
	rec := &jobLeakRecorder{session: sess}
	job := &Job{PID: 1, Args: []string{"sleep"}}

	env := &Env{
		Session:       sess,
		HandleJobLeak: rec.handle,
	}

	err := withJobScope(env, func() error {
		// def body: only a defer scope, no nested job scope.
		err := withDeferScope(env, func() error {
			env.RegisterJob(job)
			return nil
		})
		// No leak fired yet: outer job scope still active.
		assert.Empty(t, rec.leaks)
		// Caller marks the job (stands in for a wait
		// outside the def).
		job.MarkManaged()
		return err
	})
	require.NoError(t, err)
	assert.Empty(t, rec.leaks, "managed job in caller's scope must not leak")
}

func TestWithJobScope_NilHandleJobLeakIsSilent(t *testing.T) {
	t.Parallel()

	// The shell layer takes no opinion when HandleJobLeak is
	// nil: no panic, no counter bump, the leak passes silently.
	// This is the contract embedders without a renderer rely
	// on.
	env := &Env{
		Session: NewSession(),
	}
	err := withJobScope(env, func() error {
		env.RegisterJob(&Job{PID: 1, Args: []string{"x"}})
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 0, env.Session.JobLeaks(), "nil handler must not bump the counter")
}

func TestEnv_ActiveJobsReturnsRegisteredOrder(t *testing.T) {
	t.Parallel()

	env := &Env{Session: NewSession()}

	first := &Job{PID: 1, Args: []string{"a"}}
	second := &Job{PID: 2, Args: []string{"b"}}
	third := &Job{PID: 3, Args: []string{"c"}}

	var snap []*Job
	err := withJobScope(env, func() error {
		env.RegisterJob(first)
		env.RegisterJob(second)
		env.RegisterJob(third)
		snap = env.ActiveJobs()
		return nil
	})
	require.NoError(t, err)

	require.Len(t, snap, 3, "ActiveJobs returns every registered job")
	assert.Same(t, first, snap[0])
	assert.Same(t, second, snap[1])
	assert.Same(t, third, snap[2])
}

func TestEnv_ActiveJobsOutsideScopeIsNil(t *testing.T) {
	t.Parallel()

	env := &Env{Session: NewSession()}
	assert.Nil(t, env.ActiveJobs(), "no scope means no jobs")
}

func TestEnv_ActiveJobsIsCopy(t *testing.T) {
	t.Parallel()

	// The slice is a snapshot: callers must not see future
	// registrations and must not be able to corrupt the
	// registry by mutating the returned slice.
	env := &Env{Session: NewSession()}
	first := &Job{PID: 1, Args: []string{"a"}}
	second := &Job{PID: 2, Args: []string{"b"}}

	err := withJobScope(env, func() error {
		env.RegisterJob(first)
		snap := env.ActiveJobs()
		require.Len(t, snap, 1)

		// Mutate the snapshot; the next ActiveJobs call must
		// not reflect the change.
		snap[0] = nil

		env.RegisterJob(second)
		fresh := env.ActiveJobs()
		require.Len(t, fresh, 2, "the second registration must be visible")
		assert.Same(t, first, fresh[0], "snapshot mutation must not corrupt the registry")
		assert.Same(t, second, fresh[1])
		return nil
	})
	require.NoError(t, err)
}

func TestEnv_ReapJobsKeepsRunningDropsCompleted(t *testing.T) {
	t.Parallel()

	env := &Env{Session: NewSession()}

	// Two jobs: one with Done already closed (completed),
	// one with Done open (still running). The predicate is
	// "Done is closed".
	completed := &Job{PID: 1, Args: []string{"done"}, Done: closedDone()}
	running := &Job{PID: 2, Args: []string{"running"}, Done: make(chan struct{})}

	err := withJobScope(env, func() error {
		env.RegisterJob(completed)
		env.RegisterJob(running)

		env.ReapJobs(func(j *Job) bool {
			select {
			case <-j.Done:
				return true
			default:
				return false
			}
		})

		survivors := env.ActiveJobs()
		require.Len(t, survivors, 1, "completed job is reaped, running job stays")
		assert.Same(t, running, survivors[0])
		return nil
	})
	require.NoError(t, err)
}

func TestEnv_ReapJobsOutsideScopeIsNoop(t *testing.T) {
	t.Parallel()

	env := &Env{Session: NewSession()}
	require.NotPanics(t, func() {
		env.ReapJobs(func(*Job) bool { return true })
	})
}

// closedDone returns a chan that is already closed, so reap
// predicates that select on Done observe completion
// immediately without spawning real processes.
func closedDone() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func TestEnv_RegisterJobOutsideScopeIsNoop(t *testing.T) {
	t.Parallel()

	env := &Env{Session: NewSession()}
	// No active scope means env.jobs is nil. Registering must
	// not panic; the job simply has nowhere to be tracked.
	require.NotPanics(t, func() {
		env.RegisterJob(&Job{PID: 1})
	})
}

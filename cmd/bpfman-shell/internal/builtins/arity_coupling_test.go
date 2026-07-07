package builtins

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/check"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// The static checker's checkBuiltinArity is not a duplicate of a
// runtime arity table -- the runtime has no such table, only the
// per-handler guards exercised here. The checker is a deliberately
// loose static approximation of those guards, so the risk is drift:
// a runtime guard tightening (or loosening) without the checker
// following, which would let a script pass preflight and then fail
// mid-run after earlier statements have already taken effect.
//
// These tests lock the two sides together. For each invalid form the
// checker claims to cover, the runtime handler must reject it too --
// proving the corpus still matches the real guard boundary. Where the
// checker is intentionally lax (fire, whose interpolated flags defeat
// static positional counting), the case is tagged runtime-only so the
// test documents the laxity rather than hiding it. Driving the runtime
// side is cheap and side-effect-free: every arity guard returns before
// any spawn or kill.

// checkArity tokenises, parses and checks src, returning the issues.
func checkArity(t *testing.T, src string) []check.Issue {
	t.Helper()
	tokens, err := syntax.Tokenise(src)
	require.NoError(t, err)
	prog, err := syntax.Parse(tokens)
	require.NoError(t, err)
	return check.Check(prog)
}

// arityIssues filters issues down to the arity-shape diagnostics so a
// case can assert on the arity dimension alone, ignoring incidental
// findings (an undefined $job, a job leak) that belong to other
// checks.
func arityIssues(issues []check.Issue) []string {
	var out []string
	for _, i := range issues {
		if strings.Contains(i.Msg, "expected at least") || strings.Contains(i.Msg, "expected at most") {
			out = append(out, i.Msg)
		}
	}
	return out
}

func TestBuiltinArity_CheckerAndRuntimeAgree(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	arg := runtime.WordArg{Text: "x"}

	// jobArg is a real $job value. The "too many" cases must pass
	// genuine $job arguments, not dummy words: a dummy word would
	// trip the not-a-$job guard in jobFromArg, so the handler would
	// error even if the arity guard were loosened, and the test would
	// pass for the wrong reason. With real $job args the arity guard
	// is the only thing that can reject, which is what we mean to pin.
	// The current arity guards return before the job is touched, so the
	// handle is never inspected; Done is pre-closed only so that a
	// loosened guard falls through to a prompt return and fails the
	// message assertion cleanly, rather than blocking on wait.
	done := make(chan struct{})
	close(done)
	jobArg := runtime.StructuredValueArg{
		Name:  "job",
		Value: runtime.ValueFromJob(&runtime.Job{Done: done}),
	}

	cases := []struct {
		name string
		src  string // checker input
		run  func() error
		// checkerArity is the substring the checker must emit. An empty
		// string means the checker is intentionally lax for this form
		// and must NOT raise an arity issue: the runtime owns the shape.
		checkerArity string
		// runtimeMsg is the substring the runtime handler's error must
		// contain. Asserting on the message (not just that some error
		// occurred) pins the rejection to the arity guard, so a guard
		// that loosened would surface as a different error and fail here.
		runtimeMsg string
	}{
		{
			name:         "start without command",
			src:          "start",
			run:          func() error { _, err := handleStart(driver.Ctx{Ctx: ctx}); return err },
			checkerArity: "start: expected at least 1",
			runtimeMsg:   "start requires at least one argument",
		},
		{
			name:         "wait without job",
			src:          "wait",
			run:          func() error { _, err := WaitEnvelope(ctx, nil); return err },
			checkerArity: "wait: expected at least 1",
			runtimeMsg:   "wait requires exactly one argument",
		},
		{
			name:         "wait with two args",
			src:          "wait a b",
			run:          func() error { _, err := WaitEnvelope(ctx, []runtime.Arg{jobArg, jobArg}); return err },
			checkerArity: "wait: expected at most 1",
			runtimeMsg:   "wait requires exactly one argument",
		},
		{
			name:         "kill without job",
			src:          "kill",
			run:          func() error { _, err := KillEnvelope(ctx, nil); return err },
			checkerArity: "kill: expected at least 1",
			runtimeMsg:   "kill requires a $job argument",
		},
		{
			name:         "kill with two positionals",
			src:          "kill a b",
			run:          func() error { _, err := KillEnvelope(ctx, []runtime.Arg{jobArg, jobArg}); return err },
			checkerArity: "kill: expected at most 1",
			runtimeMsg:   "got more than one",
		},
		{
			name:         "jobs with extra arg",
			src:          "jobs extra",
			run:          func() error { _, err := handleJobs(driver.Ctx{Ctx: ctx, Args: []runtime.Arg{arg}}); return err },
			checkerArity: "jobs: expected at most 0",
			runtimeMsg:   "jobs takes no arguments",
		},
		{
			name:         "reap with extra arg",
			src:          "reap extra",
			run:          func() error { _, err := handleReap(driver.Ctx{Ctx: ctx, Args: []runtime.Arg{arg}}); return err },
			checkerArity: "reap: expected at most 0",
			runtimeMsg:   "reap takes no arguments",
		},
		{
			// fire's positional shape (KIND SENTINEL ACK) is enforced
			// only at runtime: 'fire onearg' satisfies the checker's
			// min-1 spec but the handler rejects the positional count
			// before spawning. This is the documented checker laxity.
			name:         "fire wrong positional count (runtime-only)",
			src:          "fire onearg",
			run:          func() error { _, err := handleFire(driver.Ctx{Ctx: ctx, Args: []runtime.Arg{arg}}); return err },
			checkerArity: "",
			runtimeMsg:   "expected 3 positional arguments",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.ErrorContains(t, tc.run(), tc.runtimeMsg, "runtime handler must reject %q on arity", tc.src)

			arity := arityIssues(checkArity(t, tc.src))
			if tc.checkerArity == "" {
				assert.Empty(t, arity, "checker is intentionally lax for %q; runtime owns the shape", tc.src)
				return
			}
			require.NotEmpty(t, arity, "checker must flag arity for %q", tc.src)
			assert.Contains(t, strings.Join(arity, " | "), tc.checkerArity)
		})
	}
}

func TestBuiltinArity_ValidFormsRaiseNoArityIssue(t *testing.T) {
	t.Parallel()

	// Accepted forms: executing these would spawn or act, so this
	// asserts only the checker side, and only the arity dimension --
	// that a well-formed invocation is not falsely rejected. An
	// undefined $job here is a separate diagnostic and out of scope.
	for _, src := range []string{
		"jobs",
		"reap",
		"start sleep 1",
		"kill --signal=USR1 $job",
		"wait $job",
	} {
		assert.Empty(t, arityIssues(checkArity(t, src)), "no arity issue expected for %q", src)
	}
}

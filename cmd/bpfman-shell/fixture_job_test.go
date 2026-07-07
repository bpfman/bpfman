package main

import (
	"testing"
)

func TestScriptRun_JobWorkflowFixtureMatrix(t *testing.T) {
	t.Parallel()

	cases := []fixtureWorkflowCase{
		{
			name:    "helper_return_wait",
			fixture: "jobs/helper-return-wait",
		},
		{
			name:    "helper_defer_kill_then_wait",
			fixture: "jobs/helper-defer-kill-then-wait",
		},
		{
			name:    "jobs_reap_preserves_running",
			fixture: "jobs/jobs-reap-preserves-running",
		},
		{
			name:    "helper_leak_is_failure",
			fixture: "jobs/helper-leak-is-failure",
		},
		{
			name:    "guarded_wait_halts_script",
			fixture: "jobs/guarded-wait-halts-script",
		},
		{
			name:    "bind_missing_command_keeps_command_not_found",
			fixture: "jobs/bind-missing-command-keeps-command-not-found",
		},
		{
			name:    "kill_after_exit_does_not_rewrite_wait",
			fixture: "jobs/kill-after-exit-does-not-rewrite-wait",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFixtureWorkflowMatrix(t, tc)
		})
	}
}

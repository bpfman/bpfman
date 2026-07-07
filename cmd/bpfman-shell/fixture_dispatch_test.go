package main

import "testing"

func TestScriptRun_DispatchFixtureMatrix(t *testing.T) {
	t.Parallel()

	cases := []fixtureWorkflowCase{
		{
			name:    "plain_missing_command_fails_immediately",
			fixture: "dispatch/plain-missing-command-fails-immediately",
		},
		{
			name:    "guard_missing_command_fails_immediately",
			fixture: "dispatch/guard-missing-command-fails-immediately",
		},
		{
			name:    "defer_missing_command_fails_at_exit",
			fixture: "dispatch/defer-missing-command-fails-at-exit",
		},
		{
			name:    "poll_missing_command_is_fatal_not_timeout",
			fixture: "dispatch/poll-missing-command-is-fatal-not-timeout",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFixtureWorkflowMatrix(t, tc)
		})
	}
}

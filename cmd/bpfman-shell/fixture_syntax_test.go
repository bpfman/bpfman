package main

import "testing"

func TestScriptRun_SyntaxFixtureMatrix(t *testing.T) {
	t.Parallel()

	cases := []fixtureWorkflowCase{
		{
			name:    "duplicate_def_params_rejected",
			fixture: "syntax/duplicate-def-params-rejected",
		},
		{
			name:    "reserved_word_def_name_rejected",
			fixture: "syntax/reserved-word-def-name-rejected",
		},
		{
			name:    "invalid_bind_target_rejected",
			fixture: "syntax/invalid-bind-target-rejected",
		},
		{
			name:    "bind_collect_non_command_tail_rejected",
			fixture: "syntax/bind-collect-non-command-tail-rejected",
		},
		{
			name:    "retry_outside_poll_is_rejected",
			fixture: "syntax/retry-outside-poll-is-rejected",
		},
		{
			name:    "retry_unless_missing_condition_is_rejected",
			fixture: "syntax/retry-unless-missing-condition-is-rejected",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFixtureWorkflowMatrix(t, tc)
		})
	}
}

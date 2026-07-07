package main

import (
	"strings"
	"testing"
)

func TestScriptRun_PollWorkflowFixtureMatrix(t *testing.T) {
	t.Parallel()

	cases := []fixtureWorkflowCase{
		{
			name:    "retry_unless_retries_until_ready",
			fixture: "poll/retry-unless-retries-until-ready",
		},
		{
			name:    "command_exit_status_drives_retry_until_ready",
			fixture: "poll/command-exit-status-drives-retry-until-ready",
		},
		{
			name:    "json_field_and_guard_retry_until_ready",
			fixture: "poll/json-field-and-guard-retry-until-ready",
		},
		{
			name:    "timeout_renders_last_retry",
			fixture: "poll/timeout-renders-last-retry",
		},
		{
			name:    "command_helper_retry_called_from_poll_retries_until_ready",
			fixture: "poll/command-helper-retry-called-from-poll-retries-until-ready",
		},
		{
			name:    "let_bind_helper_retry_called_from_poll_retries_until_ready",
			fixture: "poll/let-bind-helper-retry-called-from-poll-retries-until-ready",
		},
		{
			name:    "guard_bind_helper_retry_called_from_poll_retries_until_ready",
			fixture: "poll/guard-bind-helper-retry-called-from-poll-retries-until-ready",
		},
		{
			name:    "helper_retry_outside_poll_is_runtime_error",
			fixture: "poll/helper-retry-outside-poll-is-runtime-error",
		},
		{
			name:    "require_inside_poll_halts_immediately",
			fixture: "poll/require-inside-poll-halts-immediately",
		},
		{
			name:    "assert_in_helper_called_from_poll_is_rejected",
			fixture: "poll/assert-in-helper-called-from-poll-is-rejected",
		},
		{
			name:    "undefined_variable_inside_poll_is_fatal_not_timeout",
			fixture: "poll/undefined-variable-inside-poll-is-fatal-not-timeout",
		},
		{
			name:    "break_inside_poll_is_rejected",
			fixture: "poll/break-inside-poll-is-rejected",
		},
		{
			name:    "continue_inside_poll_is_rejected",
			fixture: "poll/continue-inside-poll-is-rejected",
		},
		{
			name:    "assert_inside_poll_is_rejected",
			fixture: "poll/assert-inside-poll-is-rejected",
		},
		{
			name:    "trace_poll_success_only_after_real_success",
			fixture: "poll/trace-poll-success-only-after-real-success",
			validate: func(t *testing.T, _ string, run fixtureWorkflowRun) {
				t.Helper()

				if count := strings.Count(run.stderr, "poll success"); count != 1 {
					t.Fatalf("poll success trace count = %d, want 1\nstderr:\n%s", count, run.stderr)
				}
				retryAt := strings.Index(run.stderr, "poll retry")
				successAt := strings.Index(run.stderr, "poll success")
				if retryAt < 0 || successAt < 0 {
					t.Fatalf("missing retry/success trace lines\nstderr:\n%s", run.stderr)
				}
				if successAt < retryAt {
					t.Fatalf("poll success traced before retry\nstderr:\n%s", run.stderr)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFixtureWorkflowMatrix(t, tc)
		})
	}
}

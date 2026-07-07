package main

import (
	"testing"
)

func TestScriptRun_ForeachWorkflowFixtureMatrix(t *testing.T) {
	t.Parallel()

	cases := []fixtureWorkflowCase{
		{
			name:    "helper_build_file_list",
			fixture: "foreach/helper-build-file-list",
		},
		{
			name:    "break_outside_foreach_is_rejected",
			fixture: "foreach/break-outside-foreach-is-rejected",
		},
		{
			name:    "continue_outside_foreach_is_rejected",
			fixture: "foreach/continue-outside-foreach-is-rejected",
		},
		{
			name:    "break_in_helper_called_from_foreach_is_rejected",
			fixture: "foreach/break-in-helper-called-from-foreach-is-rejected",
		},
		{
			name:    "loop_var_does_not_leak_after_foreach",
			fixture: "foreach/loop-var-does-not-leak-after-foreach",
		},
		{
			name:    "tuple_bind_pure_builtin_body_is_rejected",
			fixture: "foreach/tuple-bind-pure-builtin-body-is-rejected",
		},
		{
			name:    "helper_cleanup_before_return",
			fixture: "foreach/helper-cleanup-before-return",
		},
		{
			name:    "break_continue_collection",
			fixture: "foreach/break-continue-collection",
		},
		{
			name:    "guard_fail_stops_iteration",
			fixture: "foreach/guard-fail-stops-iteration",
		},
		{
			name:    "tuple_rc_and_primary_lists",
			fixture: "foreach/tuple-rc-and-primary-lists",
		},
		{
			name:    "nested_break_stays_inner",
			fixture: "foreach/nested-break-stays-inner",
		},
		{
			name:    "nested_continue_stays_inner",
			fixture: "foreach/nested-continue-stays-inner",
		},
		{
			name:    "guard_failure_in_body_cites_inner_line",
			fixture: "foreach/guard-failure-in-body-cites-inner-line",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFixtureWorkflowMatrix(t, tc)
		})
	}
}

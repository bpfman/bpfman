package main

import "testing"

func TestScriptRun_ConditionalWorkflowFixtureMatrix(t *testing.T) {
	t.Parallel()

	cases := []fixtureWorkflowCase{
		{
			name:    "runtime_check_selects_then_branch",
			fixture: "conditionals/runtime-check-selects-then-branch",
		},
		{
			name:    "matches_expression_selects_then_branch",
			fixture: "conditionals/matches-expression-selects-then-branch",
		},
		{
			name:    "elif_selects_middle_branch",
			fixture: "conditionals/elif-selects-middle-branch",
		},
		{
			name:    "nested_else_writes_selected_file",
			fixture: "conditionals/nested-else-writes-selected-file",
		},
		{
			name:    "selected_branch_guard_failure_halts_script",
			fixture: "conditionals/selected-branch-guard-failure-halts-script",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFixtureWorkflowMatrix(t, tc)
		})
	}
}

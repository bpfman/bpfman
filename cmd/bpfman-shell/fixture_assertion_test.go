package main

import "testing"

func TestScriptRun_AssertionWorkflowFixtureMatrix(t *testing.T) {
	t.Parallel()

	cases := []fixtureWorkflowCase{
		{
			name:    "command_status_assertions_gate_file_workflow",
			fixture: "assertions/command-status-assertions-gate-file-workflow",
		},
		{
			name:    "require_failure_halts_script_immediately",
			fixture: "assertions/require-failure-halts-script-immediately",
		},
		{
			name:    "helper_assert_failure_keeps_caller_running",
			fixture: "assertions/helper-assert-failure-keeps-caller-running",
		},
		{
			name:    "present_predicate_failure_describes_missing_field",
			fixture: "assertions/present-predicate-failure-describes-missing-field",
		},
		{
			name:    "negated_missing_predicate_failure_reads_sensibly",
			fixture: "assertions/negated-missing-predicate-failure-reads-sensibly",
		},
		{
			name:    "empty_predicate_failure_describes_non_empty_value",
			fixture: "assertions/empty-predicate-failure-describes-non-empty-value",
		},
		{
			name:    "malformed_matches_entry_missing_colon_is_rejected",
			fixture: "assertions/malformed-matches-entry-missing-colon-is-rejected",
		},
		{
			name:    "require_null_without_target_is_rejected",
			fixture: "assertions/require-null-without-target-is-rejected",
		},
		{
			name:    "predicate_workflow_gates_path_and_content",
			fixture: "assertions/predicate-workflow-gates-path-and-content",
		},
		{
			name:    "shape_predicates_describe_json_value",
			fixture: "assertions/shape-predicates-describe-json-value",
		},
		{
			name:    "predicate_failure_keeps_script_running",
			fixture: "assertions/predicate-failure-keeps-script-running",
		},
		{
			name:    "nested_matches_exhaustive_cites_full_paths",
			fixture: "assertions/nested-matches-exhaustive-cites-full-paths",
		},
		{
			name:    "matches_indexed_paths_work",
			fixture: "assertions/matches-indexed-paths-work",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFixtureWorkflowMatrix(t, tc)
		})
	}
}

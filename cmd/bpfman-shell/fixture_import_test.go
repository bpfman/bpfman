package main

import "testing"

func TestScriptRun_ImportedWorkflowFixtureMatrix(t *testing.T) {
	t.Parallel()

	cases := []fixtureWorkflowCase{
		{
			name:    "tuple_bind_from_imported_value_return",
			fixture: "import/tuple-bind-from-imported-value-return",
		},
		{
			name:    "defer_dispatches_imported_def",
			fixture: "import/defer-dispatches-imported-def",
		},
		{
			name:    "bind_collect_uses_imported_def_producer",
			fixture: "import/bind-collect-uses-imported-def-producer",
		},
		{
			name:    "imported_helper_cleanup_runs_before_return",
			fixture: "import/imported-helper-cleanup-runs-before-return",
		},
		{
			name:    "nested_imported_runtime_error_keeps_library_callsite",
			fixture: "import/nested-imported-runtime-error-keeps-library-callsite",
		},
		{
			name:    "guard_on_imported_helper_cleanup_failure_halts_script",
			fixture: "import/guard-on-imported-helper-cleanup-failure-halts-script",
		},
		{
			name:    "imported_expression_assert_cites_library_line",
			fixture: "import/imported-expression-assert-cites-library-line",
		},
		{
			name:    "imported_expression_require_halts_at_library_line",
			fixture: "import/imported-expression-require-halts-at-library-line",
		},
		{
			name:    "imported_predicate_require_halts_at_library_line",
			fixture: "import/imported-predicate-require-halts-at-library-line",
		},
		{
			name:    "imported_matches_assert_cites_library_entry_lines",
			fixture: "import/imported-matches-assert-cites-library-entry-lines",
		},
		{
			name:    "imported_library_cannot_import_transitively",
			fixture: "import/imported-library-cannot-import-transitively",
		},
		{
			name:    "import_must_be_top_level",
			fixture: "import/import-must-be-top-level",
		},
		{
			name:    "imported_helper_break_called_from_foreach_is_rejected",
			fixture: "import/imported-helper-break-called-from-foreach-is-rejected",
		},
		{
			name:    "imported_helper_continue_called_from_foreach_is_rejected",
			fixture: "import/imported-helper-continue-called-from-foreach-is-rejected",
		},
		{
			name:    "imported_helper_break_called_from_poll_is_rejected",
			fixture: "import/imported-helper-break-called-from-poll-is-rejected",
		},
		{
			name:    "imported_helper_continue_called_from_poll_is_rejected",
			fixture: "import/imported-helper-continue-called-from-poll-is-rejected",
		},
		{
			name:    "imported_helper_break_called_from_guard_is_rejected",
			fixture: "import/imported-helper-break-called-from-guard-is-rejected",
		},
		{
			name:    "imported_helper_continue_called_from_guard_is_rejected",
			fixture: "import/imported-helper-continue-called-from-guard-is-rejected",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFixtureWorkflowMatrix(t, tc)
		})
	}
}

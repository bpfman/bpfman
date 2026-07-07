package main

import "testing"

func TestScriptRun_ScopeFixtureMatrix(t *testing.T) {
	t.Parallel()

	cases := []fixtureWorkflowCase{
		{
			name:    "same_scope_relet_rebinds_name",
			fixture: "scope/same-scope-relet-rebinds-name",
		},
		{
			name:    "nested_scope_shadowing_keeps_outer_and_allows_late_rebind",
			fixture: "scope/nested-scope-shadowing-keeps-outer-and-allows-late-rebind",
		},
		{
			name:    "duplicate_top_level_def_in_one_file_rejected",
			fixture: "scope/duplicate-top-level-def-in-one-file-rejected",
		},
		{
			name:    "destructure_duplicate_name_is_rejected",
			fixture: "scope/destructure-duplicate-name-is-rejected",
		},
		{
			name:    "tuple_bind_duplicate_name_is_rejected",
			fixture: "scope/tuple-bind-duplicate-name-is-rejected",
		},
		{
			name:    "guard_tuple_duplicate_name_is_rejected",
			fixture: "scope/guard-tuple-duplicate-name-is-rejected",
		},
		{
			name:    "def_shadows_pure_builtin_in_bind",
			fixture: "scope/def-shadows-pure-builtin-in-bind",
		},
		{
			name:    "duplicate_range_def_is_rejected",
			fixture: "scope/duplicate-range-def-is-rejected",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFixtureWorkflowMatrix(t, tc)
		})
	}
}

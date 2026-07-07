package main

import "testing"

func TestScriptRun_HoistedDefsFixtureMatrix(t *testing.T) {
	t.Parallel()

	cases := []fixtureWorkflowCase{
		{
			name:    "forward_main_def_call",
			fixture: "hoist/forward-main-def-call",
		},
		{
			name:    "forward_imported_def_call",
			fixture: "hoist/forward-imported-def-call",
		},
		{
			name:    "imported_helper_sees_later_main_def",
			fixture: "hoist/imported-helper-sees-later-main-def",
		},
		{
			name:    "duplicate_imported_def_is_rejected",
			fixture: "hoist/duplicate-imported-def-is-rejected",
		},
		{
			name:    "duplicate_main_after_import_def_is_rejected",
			fixture: "hoist/duplicate-main-after-import-def-is-rejected",
		},
		{
			name:    "duplicate_across_two_imports_is_rejected",
			fixture: "hoist/duplicate-across-two-imports-is-rejected",
		},
		{
			name:    "forward_call_duplicate_def_is_rejected",
			fixture: "hoist/forward-call-duplicate-def-is-rejected",
		},
		{
			name:    "forward_call_arity_error_cites_declaration",
			fixture: "hoist/forward-call-arity-error-cites-declaration",
		},
		{
			name:    "imported_helper_forward_arity_error_cites_library_callsite",
			fixture: "hoist/imported-helper-forward-arity-error-cites-library-callsite",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFixtureWorkflowMatrix(t, tc)
		})
	}
}

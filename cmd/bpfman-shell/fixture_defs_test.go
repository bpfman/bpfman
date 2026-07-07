package main

import "testing"

func TestScriptRun_DefsFixtureMatrix(t *testing.T) {
	t.Parallel()

	cases := []fixtureWorkflowCase{
		{
			name:    "single_file_lists_sorted_and_stable",
			fixture: "defs/single-file-lists-sorted-and-stable",
		},
		{
			name:    "imported_and_builtin_shadowing_defs_listed_sorted",
			fixture: "defs/imported-and-builtin-shadowing-defs-listed-sorted",
		},
		{
			name:    "duplicate_def_preflight_prevents_defs_listing",
			fixture: "defs/duplicate-def-preflight-prevents-defs-listing",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFixtureWorkflowMatrix(t, tc)
		})
	}
}

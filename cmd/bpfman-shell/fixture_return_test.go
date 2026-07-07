package main

import "testing"

func TestScriptRun_ReturnWorkflowFixtureMatrix(t *testing.T) {
	t.Parallel()

	cases := []fixtureWorkflowCase{
		{
			name:    "return_inside_conditional_skips_helper_tail",
			fixture: "returns/return-inside-conditional-skips-helper-tail",
		},
		{
			name:    "return_inside_foreach_stops_helper",
			fixture: "returns/return-inside-foreach-stops-helper",
		},
		{
			name:    "no_return_helper_bind_yields_envelope",
			fixture: "returns/no-return-helper-bind-yields-envelope",
		},
		{
			name:    "helper_cleanup_runs_before_returned_tempdir_escapes",
			fixture: "returns/helper-cleanup-runs-before-returned-tempdir-escapes",
		},
		{
			name:    "inner_defer_failure_does_not_flip_outer_rc",
			fixture: "returns/inner-defer-failure-does-not-flip-outer-rc",
		},
		{
			name:    "top_level_defer_dispatches_local_def",
			fixture: "returns/top-level-defer-dispatches-local-def",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFixtureWorkflowMatrix(t, tc)
		})
	}
}

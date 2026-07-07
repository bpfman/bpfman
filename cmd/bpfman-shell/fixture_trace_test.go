package main

import "testing"

func TestScriptRun_TraceFixtureMatrix(t *testing.T) {
	t.Parallel()

	cases := []fixtureWorkflowCase{
		{
			name:    "trace_invalid_argument_is_rejected",
			fixture: "trace/trace-invalid-argument-is-rejected",
		},
		{
			name:    "trace_off_stops_later_command_traces",
			fixture: "trace/trace-off-stops-later-command-traces",
		},
		{
			name:    "trace_on_registration_off_at_fire_suppresses_defer_fire",
			fixture: "trace/trace-on-registration-off-at-fire-suppresses-defer-fire",
		},
		{
			name:    "trace_off_registration_on_at_fire_emits_defer_fire",
			fixture: "trace/trace-off-registration-on-at-fire-emits-defer-fire",
		},
		{
			name:    "imported_helper_defer_traces_during_main_unwind",
			fixture: "trace/imported-helper-defer-traces-during-main-unwind",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFixtureWorkflowMatrix(t, tc)
		})
	}
}

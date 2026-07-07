package main

import "testing"

func TestScriptRun_GuardFixtureMatrix(t *testing.T) {
	t.Parallel()

	cases := []fixtureWorkflowCase{
		{
			name:    "guard_proceeds_on_ok_envelope_without_extra_assert",
			fixture: "guard/guard-proceeds-on-ok-envelope-without-extra-assert",
		},
		{
			name:    "guard_halts_on_non_ok_envelope",
			fixture: "guard/guard-halts-on-non-ok-envelope",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFixtureWorkflowMatrix(t, tc)
		})
	}
}

package main

import "testing"

func TestScriptRun_ExpressionWorkflowFixtureMatrix(t *testing.T) {
	t.Parallel()

	cases := []fixtureWorkflowCase{
		{
			name:    "file_adapter_externalises_structured_value",
			fixture: "expressions/file-adapter-externalises-structured-value",
		},
		{
			name:    "little_endian_encoders_build_global_lines",
			fixture: "expressions/little-endian-encoders-build-global-lines",
		},
		{
			name:    "threaded_dataflow_drives_foreach",
			fixture: "expressions/threaded-dataflow-drives-foreach",
		},
		{
			name:    "zip_length_mismatch_halts_before_body",
			fixture: "expressions/zip-length-mismatch-halts-before-body",
		},
		{
			name:    "exact_large_integer_comparisons",
			fixture: "expressions/exact-large-integer-comparisons",
		},
		{
			name:    "exact_large_integer_arithmetic",
			fixture: "expressions/exact-large-integer-arithmetic",
		},
		{
			name:    "invalid_numeric_literal_rejected",
			fixture: "expressions/invalid-numeric-literal-rejected",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFixtureWorkflowMatrix(t, tc)
		})
	}
}

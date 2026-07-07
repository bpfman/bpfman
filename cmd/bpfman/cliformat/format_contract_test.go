package cliformat

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStdlibJSONMarshal_NoTrailingNewline pins the encoding/json
// "no trailing newline" contract that every marshaller-driven
// formatter in format.go relies on. The contract is documented in
// the encoding/json package godoc; we restate it here as an
// executable assertion so that a Go runtime upgrade or a stdlib
// behaviour change that introduces a trailing newline is caught
// before it leaks through to CLI consumers.
//
// See the file-level comment block in format.go for the broader
// CLI-output trailing-newline contract.
func TestStdlibJSONMarshal_NoTrailingNewline(t *testing.T) {
	t.Parallel()

	// A representative mix of value shapes: object, array of
	// objects, primitive, empty array, nested object. If any of
	// these grows a trailing newline in a future stdlib version,
	// the marshaller-driven formatters in format.go would emit
	// two trailing newlines instead of one.
	cases := []struct {
		name string
		v    any
	}{
		{"object", map[string]any{"k": "v"}},
		{"array", []map[string]any{{"id": 1}, {"id": 2}}},
		{"primitive", 42},
		{"empty array", []any{}},
		{"nested", map[string]any{"outer": map[string]any{"inner": 1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			plain, err := json.Marshal(tc.v)
			require.NoError(t, err)
			require.False(t, bytes.HasSuffix(plain, []byte("\n")), "json.Marshal(%v) unexpectedly ends with \\n: %q", tc.v, plain)

			indented, err := json.MarshalIndent(tc.v, "", "  ")
			require.NoError(t, err)
			require.False(t, bytes.HasSuffix(indented, []byte("\n")), "json.MarshalIndent(%v) unexpectedly ends with \\n: %q", tc.v, indented)
		})
	}
}

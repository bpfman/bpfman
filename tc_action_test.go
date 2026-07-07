package bpfman_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
)

// TestParseTCAction pins the boundary parser: matching is
// case-insensitive, and an unrecognised value is rejected.
func TestParseTCAction(t *testing.T) {
	t.Parallel()

	a, err := bpfman.ParseTCAction("OK")
	require.NoError(t, err, "parsing is case-insensitive")
	assert.Equal(t, bpfman.TCActionOK, a)

	_, err = bpfman.ParseTCAction("garbage")
	assert.Error(t, err, "unknown action should be rejected")
}

// TestTCAction_Int32RoundTrip pins the value-object guarantee: every
// action carries its kernel code (Int32 is total), the code round-trips
// back to the same action, and an unknown code is rejected.
func TestTCAction_Int32RoundTrip(t *testing.T) {
	t.Parallel()

	for _, name := range bpfman.TCActionNames() {
		a, err := bpfman.ParseTCAction(name)
		require.NoErrorf(t, err, "ParseTCAction(%q)", name)

		back, err := bpfman.TCActionFromInt32(a.Int32())
		require.NoErrorf(t, err, "TCActionFromInt32(%d)", a.Int32())
		assert.Equalf(t, a, back, "code round-trip for %s", a)
	}

	_, err := bpfman.TCActionFromInt32(9999)
	assert.Error(t, err, "unknown code should be rejected")
}

// TestTCAction_JSONRoundTrip pins the wire form: a TCAction marshals to
// its name and decodes back through ParseTCAction to the same value.
// Decoding is strict -- an unrecognised name is rejected -- because the
// value object is reconstructed by parsing, not populated natively.
func TestTCAction_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	for _, name := range bpfman.TCActionNames() {
		a, err := bpfman.ParseTCAction(name)
		require.NoErrorf(t, err, "ParseTCAction(%q)", name)

		data, err := json.Marshal(a)
		require.NoErrorf(t, err, "marshal %s", a)
		assert.Equalf(t, `"`+a.String()+`"`, string(data), "wire form of %s", a)

		var got bpfman.TCAction
		require.NoErrorf(t, json.Unmarshal(data, &got), "unmarshal %s", a)
		assert.Equalf(t, a, got, "round-trip %s", a)
	}

	var got bpfman.TCAction
	assert.Error(t, json.Unmarshal([]byte(`"garbage"`), &got), "unknown name should be rejected on decode")
}

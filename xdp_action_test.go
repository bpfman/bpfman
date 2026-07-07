package bpfman_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
)

// xdpActions is the full set of known XDP actions, used to drive the
// round-trip tests.
var xdpActions = []bpfman.XDPAction{
	bpfman.XDPActionAborted,
	bpfman.XDPActionDrop,
	bpfman.XDPActionPass,
	bpfman.XDPActionTX,
	bpfman.XDPActionRedirect,
	bpfman.XDPActionDispatcherReturn,
}

// TestParseXDPAction pins the boundary parser: matching is
// case-insensitive, and an unrecognised value is rejected.
func TestParseXDPAction(t *testing.T) {
	t.Parallel()

	a, err := bpfman.ParseXDPAction("PASS")
	require.NoError(t, err, "parsing is case-insensitive")
	assert.Equal(t, bpfman.XDPActionPass, a)

	_, err = bpfman.ParseXDPAction("garbage")
	assert.Error(t, err, "unknown action should be rejected")
}

// TestXDPAction_Int32RoundTrip pins the value-object guarantee: every
// action carries its kernel code, the code round-trips back to the same
// action, and an unknown code is rejected.
func TestXDPAction_Int32RoundTrip(t *testing.T) {
	t.Parallel()

	for _, a := range xdpActions {
		back, err := bpfman.XDPActionFromInt32(a.Int32())
		require.NoErrorf(t, err, "XDPActionFromInt32(%d)", a.Int32())
		assert.Equalf(t, a, back, "code round-trip for %s", a)
	}

	_, err := bpfman.XDPActionFromInt32(9999)
	assert.Error(t, err, "unknown code should be rejected")
}

// TestXDPAction_JSONRoundTrip pins the wire form: an XDPAction marshals
// to its name (via TextMarshaler) and decodes back through
// ParseXDPAction to the same value. Decoding is strict -- an
// unrecognised name is rejected.
func TestXDPAction_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	for _, a := range xdpActions {
		data, err := json.Marshal(a)
		require.NoErrorf(t, err, "marshal %s", a)
		assert.Equalf(t, `"`+a.String()+`"`, string(data), "wire form of %s", a)

		var got bpfman.XDPAction
		require.NoErrorf(t, json.Unmarshal(data, &got), "unmarshal %s", a)
		assert.Equalf(t, a, got, "round-trip %s", a)
	}

	var got bpfman.XDPAction
	assert.Error(t, json.Unmarshal([]byte(`"garbage"`), &got), "unknown name should be rejected on decode")
}

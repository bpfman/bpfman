package runtime

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

func testNetnsVethPair() *NetnsVethPair {
	return NewNetnsVethPair(
		NetnsVethEndpoint{
			Ns:      "B000000000001Na",
			Link:    "B000000000001Na",
			Addr:    "198.51.100.1",
			Ifindex: 7,
			Nsid:    4026531840,
		},
		NetnsVethEndpoint{
			Ns:      "B000000000001Nb",
			Link:    "B000000000001Nb",
			Addr:    "198.51.100.2",
			Ifindex: 2,
			Nsid:    4026531841,
		},
	)
}

func TestNetnsVethPair_ReleaseLatch(t *testing.T) {
	t.Parallel()

	pair := testNetnsVethPair()
	require.False(t, pair.IsReleased())
	assert.False(t, pair.MarkReleased(), "first MarkReleased reports the latch was not yet set")
	assert.True(t, pair.IsReleased())
	assert.True(t, pair.MarkReleased(), "second MarkReleased short-circuits")
}

func TestNewNetnsVethPair_EndpointBackPointers(t *testing.T) {
	t.Parallel()

	pair := testNetnsVethPair()
	require.NotNil(t, pair.A)
	require.NotNil(t, pair.B)
	assert.Same(t, pair, pair.A.Pair)
	assert.Same(t, pair, pair.B.Pair)
}

func TestValueFromNetnsVethPair_PairKindAndOrigin(t *testing.T) {
	t.Parallel()

	pair := testNetnsVethPair()
	v := ValueFromNetnsVethPair(pair)
	assert.Equal(t, semantics.OriginNetnsVethPair, v.Kind())
	assert.Same(t, pair, v.Origin())
}

func TestValueFromNetnsVethPair_EndpointRecovery(t *testing.T) {
	t.Parallel()

	pair := testNetnsVethPair()
	v := ValueFromNetnsVethPair(pair)

	a, err := v.LookupValue("$pair", "a")
	require.NoError(t, err)
	assert.Equal(t, semantics.OriginNetnsVethEndpoint, a.Kind())
	assert.Same(t, pair.A, a.Origin())

	b, err := v.LookupValue("$pair", "b")
	require.NoError(t, err)
	assert.Equal(t, semantics.OriginNetnsVethEndpoint, b.Kind())
	assert.Same(t, pair.B, b.Origin())
}

func TestValueFromNetnsVethPair_FieldAccess(t *testing.T) {
	t.Parallel()

	v := ValueFromNetnsVethPair(testNetnsVethPair())

	cases := []struct {
		path string
		want string
	}{
		{"a.ns", "B000000000001Na"},
		{"a.link", "B000000000001Na"},
		{"a.addr", "198.51.100.1"},
		{"a.ifindex", "7"},
		{"a.nsid", "4026531840"},
		{"b.ns", "B000000000001Nb"},
		{"b.link", "B000000000001Nb"},
		{"b.addr", "198.51.100.2"},
		{"b.ifindex", "2"},
		{"b.nsid", "4026531841"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			got, err := v.Lookup("$pair", tc.path)
			require.NoError(t, err)
			s, err := got.Scalar()
			require.NoError(t, err)
			assert.Equal(t, tc.want, s)
		})
	}
}

func TestValueFromNetnsVethPair_NumericFieldsAreNumbers(t *testing.T) {
	t.Parallel()

	v := ValueFromNetnsVethPair(testNetnsVethPair())
	for _, path := range []string{"a.ifindex", "a.nsid", "b.ifindex", "b.nsid"} {
		got, err := v.LookupValue("$pair", path)
		require.NoError(t, err)
		_, ok := got.Raw().(json.Number)
		assert.True(t, ok, "%s should be a json.Number, got %T", path, got.Raw())
	}
}

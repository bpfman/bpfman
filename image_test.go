package bpfman_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
)

// TestImagePullPolicy_JSONRoundTrip pins the wire form. ImagePullPolicy
// is a plain string enum with no custom (Un)MarshalText, so each policy
// serialises to its canonical name and decodes back to the same value.
func TestImagePullPolicy_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	for _, p := range []bpfman.ImagePullPolicy{
		bpfman.PullAlways, bpfman.PullIfNotPresent, bpfman.PullNever,
	} {
		data, err := json.Marshal(p)
		require.NoErrorf(t, err, "marshal %s", p)
		assert.Equalf(t, `"`+p.String()+`"`, string(data), "wire form of %s", p)

		var got bpfman.ImagePullPolicy
		require.NoErrorf(t, json.Unmarshal(data, &got), "unmarshal %s", p)
		assert.Equalf(t, p, got, "round-trip %s", p)
	}
}

// TestImagePullPolicy_Valid pins strict membership: the zero value and
// an unrecognised value are invalid; the three known policies are valid.
func TestImagePullPolicy_Valid(t *testing.T) {
	t.Parallel()

	assert.False(t, bpfman.ImagePullPolicy("").Valid(), "zero value is not valid")
	assert.False(t, bpfman.ImagePullPolicy("Sometimes").Valid(), "unknown value is not valid")
	for _, p := range []bpfman.ImagePullPolicy{
		bpfman.PullAlways, bpfman.PullIfNotPresent, bpfman.PullNever,
	} {
		assert.Truef(t, p.Valid(), "%s should be valid", p)
	}
}

// TestParseImagePullPolicy pins the boundary parser: matching is
// case-insensitive, the empty string maps to the IfNotPresent default,
// and an unrecognised value is rejected with the zero value.
func TestParseImagePullPolicy(t *testing.T) {
	t.Parallel()

	cases := map[string]bpfman.ImagePullPolicy{
		"":             bpfman.PullIfNotPresent,
		"always":       bpfman.PullAlways,
		"ALWAYS":       bpfman.PullAlways,
		"IfNotPresent": bpfman.PullIfNotPresent,
		"never":        bpfman.PullNever,
	}
	for in, want := range cases {
		got, err := bpfman.ParseImagePullPolicy(in)
		require.NoErrorf(t, err, "ParseImagePullPolicy(%q)", in)
		assert.Equalf(t, want, got, "ParseImagePullPolicy(%q)", in)
	}

	got, err := bpfman.ParseImagePullPolicy("bogus")
	assert.Error(t, err, "unknown policy should be rejected")
	assert.Equal(t, bpfman.ImagePullPolicy(""), got, "rejected parse returns zero value")
}

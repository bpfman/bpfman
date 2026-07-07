package bpfman_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
)

// TestProgramListResult_EmptyMarshalsAsEmptyArray pins the wire
// contract that an empty program list serialises as
// `"programs": []`, never `"programs": null`. The shell binds
// list results through ValueFromStruct -> json.Marshal, and a
// `null` payload would break consumer jq expressions such as
// `.programs[]`. The producer (manager.ListProgramEntries) is
// responsible for returning the entries as a non-nil slice on the
// empty case; this test pins the resulting wire shape so an
// accidental regression in the producer is caught at the
// shell-facing boundary rather than in distant e2e scripts.
func TestProgramListResult_EmptyMarshalsAsEmptyArray(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(bpfman.ProgramListResult{Programs: []bpfman.ProgramListEntry{}})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"programs":[]`)
	assert.NotContains(t, string(data), `"programs":null`)
}

func TestProgramTypeConsistency(t *testing.T) {
	t.Parallel()

	// Verify AllProgramTypes and ProgramTypeNames are consistent
	allTypes := bpfman.AllProgramTypes()
	allNames := bpfman.ProgramTypeNames()

	require.Equal(t, len(allTypes), len(allNames), "AllProgramTypes and ProgramTypeNames should have same length")

	for i, pt := range allTypes {
		assert.Equal(t, pt.String(), allNames[i], "ProgramTypeNames[%d] should match AllProgramTypes[%d].String()", i, i)
	}

	// Verify ParseProgramType accepts all names from ProgramTypeNames
	for _, name := range allNames {
		pt, err := bpfman.ParseProgramType(name)
		assert.NoError(t, err, "ParseProgramType should accept %q", name)
		assert.Equal(t, name, pt.String(), "round-trip should preserve name")
	}

	// Verify AllProgramTypes doesn't include zero value
	for _, pt := range allTypes {
		assert.NotEqual(t, bpfman.ProgramType(""), pt, "AllProgramTypes should not include zero value")
	}
}

// TestProgramType_JSONRoundTrip pins ProgramType's wire format. It is a
// plain string enum with no custom (Un)MarshalText, so every value must
// serialise to its lowercase name in quotes via Go's native string
// encoding and decode back to the same value. This locks the JSON form
// against an accidental regression.
func TestProgramType_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	for _, pt := range bpfman.AllProgramTypes() {
		data, err := json.Marshal(pt)
		require.NoErrorf(t, err, "marshal %s", pt)
		assert.Equalf(t, `"`+pt.String()+`"`, string(data), "wire form of %s", pt)

		var got bpfman.ProgramType
		require.NoErrorf(t, json.Unmarshal(data, &got), "unmarshal %s", pt)
		assert.Equalf(t, pt, got, "round-trip %s", pt)
	}
}

// TestProgramType_ParseRejectsUnknown pins the sole validation boundary:
// ParseProgramType rejects an unrecognised name and returns the zero
// value. JSON decoding is deliberately permissive (it trusts bpfman's
// own stored values), so external input is validated here, not on decode.
func TestProgramType_ParseRejectsUnknown(t *testing.T) {
	t.Parallel()

	pt, err := bpfman.ParseProgramType("bogus")
	assert.Error(t, err, "ParseProgramType should reject an unknown type")
	assert.Equal(t, bpfman.ProgramType(""), pt, "rejected parse should return the zero value")
}

// TestProgramType_Valid pins that Valid is a strict known-value
// predicate, not a non-empty check: it rejects both the zero value and
// a non-empty but unrecognised value, and accepts every known type. The
// constructors gate on this, so a weakened Valid would let an unknown
// type through NewLoadSpec.
func TestProgramType_Valid(t *testing.T) {
	t.Parallel()

	assert.False(t, bpfman.ProgramType("").Valid(), "zero value is not valid")
	assert.False(t, bpfman.ProgramType("garbage").Valid(), "unknown value is not valid")
	for _, pt := range bpfman.AllProgramTypes() {
		assert.Truef(t, pt.Valid(), "%s should be valid", pt)
	}
}

// TestProgramType_KernelType pins the projection from bpfman's
// attach-oriented taxonomy onto the coarser kernel taxonomy. The
// mapping is deliberately many-to-one: tc/tcx -> schedcls, the probe
// family -> kprobe, fentry/fexit -> tracing.
func TestProgramType_KernelType(t *testing.T) {
	t.Parallel()

	cases := map[bpfman.ProgramType]string{
		bpfman.ProgramTypeXDP:        "xdp",
		bpfman.ProgramTypeTC:         "schedcls",
		bpfman.ProgramTypeTCX:        "schedcls",
		bpfman.ProgramTypeTracepoint: "tracepoint",
		bpfman.ProgramTypeKprobe:     "kprobe",
		bpfman.ProgramTypeKretprobe:  "kprobe",
		bpfman.ProgramTypeUprobe:     "kprobe",
		bpfman.ProgramTypeUretprobe:  "kprobe",
		bpfman.ProgramTypeFentry:     "tracing",
		bpfman.ProgramTypeFexit:      "tracing",
	}
	for pt, want := range cases {
		assert.Equalf(t, want, pt.KernelType().String(), "KernelType of %s", pt)
	}
}

// TestMatchesKernelOnly_CoarseProjection pins the lossy filter for
// kernel-only programs: a --type filter is projected onto the kernel
// taxonomy before comparison, so tc and tcx both match a schedcls row
// and every probe variant matches a kprobe row. A mismatching kernel
// type is excluded, and an empty type filter matches everything.
func TestMatchesKernelOnly_CoarseProjection(t *testing.T) {
	t.Parallel()

	schedcls := kernel.NewProgramType("schedcls")
	kprobe := kernel.NewProgramType("kprobe")

	// tc and tcx are indistinguishable at the kernel level: both match.
	assert.True(t, bpfman.ApplyListOptions(bpfman.WithTypes(bpfman.ProgramTypeTC)).MatchesKernelOnly(schedcls))
	assert.True(t, bpfman.ApplyListOptions(bpfman.WithTypes(bpfman.ProgramTypeTCX)).MatchesKernelOnly(schedcls))

	// every probe variant matches a kernel-only kprobe.
	for _, pt := range []bpfman.ProgramType{
		bpfman.ProgramTypeKprobe, bpfman.ProgramTypeKretprobe,
		bpfman.ProgramTypeUprobe, bpfman.ProgramTypeUretprobe,
	} {
		assert.Truef(t, bpfman.ApplyListOptions(bpfman.WithTypes(pt)).MatchesKernelOnly(kprobe), "%s should match a kernel-only kprobe", pt)
	}

	// a mismatching kernel type is excluded.
	assert.False(t, bpfman.ApplyListOptions(bpfman.WithTypes(bpfman.ProgramTypeXDP)).MatchesKernelOnly(schedcls))

	// no type filter matches any kernel-only program.
	assert.True(t, bpfman.ApplyListOptions().MatchesKernelOnly(schedcls))
}

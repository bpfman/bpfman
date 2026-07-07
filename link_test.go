package bpfman_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
)

// TestLinkRecord_UnmarshalJSON_RoundTripsEveryLinkKind asserts
// that every LinkKind round-trips its Details field through
// json.Marshal -> json.Unmarshal, including the kretprobe /
// uretprobe variants which share a struct with their kprobe /
// uprobe partners and rely on the Retprobe bool to discriminate.
func TestLinkRecord_UnmarshalJSON_RoundTripsEveryLinkKind(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pin := bpfman.LinkPath("/run/bpfman/fs/example/link_0")

	cases := []struct {
		name    string
		details bpfman.LinkDetails
		kind    bpfman.LinkKind
	}{
		{
			"xdp",
			bpfman.XDPDetails{Interface: "eth0", Ifindex: 3, Priority: 50, Position: 0},
			bpfman.LinkKindXDP,
		},
		{
			"tc",
			bpfman.TCDetails{Interface: "eth0", Ifindex: 3, Direction: bpfman.TCDirectionIngress, Priority: 100, Position: 0, ProceedOn: []int32{0, 3, 30}},
			bpfman.LinkKindTC,
		},
		{
			"tcx",
			bpfman.TCXDetails{Interface: "eth0", Ifindex: 3, Direction: bpfman.TCDirectionEgress, Priority: 50, Position: 0},
			bpfman.LinkKindTCX,
		},
		{
			"tracepoint",
			bpfman.TracepointDetails{Group: "sched", Name: "sched_switch"},
			bpfman.LinkKindTracepoint,
		},
		{
			"kprobe",
			bpfman.KprobeDetails{FnName: "do_unlinkat"},
			bpfman.LinkKindKprobe,
		},
		{
			"kretprobe",
			bpfman.KprobeDetails{FnName: "do_unlinkat", Retprobe: true},
			bpfman.LinkKindKretprobe,
		},
		{
			"uprobe",
			bpfman.UprobeDetails{Target: "/bin/sh", FnName: "main"},
			bpfman.LinkKindUprobe,
		},
		{
			"uretprobe",
			bpfman.UprobeDetails{Target: "/bin/sh", FnName: "main", Retprobe: true},
			bpfman.LinkKindUretprobe,
		},
		{
			"fentry",
			bpfman.FentryDetails{FnName: "do_unlinkat"},
			bpfman.LinkKindFentry,
		},
		{
			"fexit",
			bpfman.FexitDetails{FnName: "do_unlinkat"},
			bpfman.LinkKindFexit,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			original := bpfman.LinkRecord{
				ID:        bpfman.LinkID(42),
				ProgramID: kernel.ProgramID(43),
				Kind:      tc.kind,
				PinPath:   &pin,
				Details:   tc.details,
				CreatedAt: fixed,
			}

			data, err := json.Marshal(original)
			require.NoError(t, err)

			var got bpfman.LinkRecord
			require.NoError(t, json.Unmarshal(data, &got))

			assert.Equal(t, original, got)
		})
	}
}

// TestLinkRecord_UnmarshalJSON_AcceptsNilDetails verifies that a
// record with no Details round-trips with Details left nil.
func TestLinkRecord_UnmarshalJSON_AcceptsNilDetails(t *testing.T) {
	t.Parallel()

	data := []byte(`{"id":1,"program_id":2,"kind":"kprobe","created_at":"2026-01-01T00:00:00Z"}`)
	var got bpfman.LinkRecord
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Nil(t, got.Details)
	assert.Equal(t, bpfman.LinkKindKprobe, got.Kind)
}

// TestLinkRecord_JSON_MetadataRoundTrip verifies user metadata survives a
// marshal/unmarshal cycle, and that an absent map decodes to nil rather
// than an empty map so the in-memory form stays canonical.
func TestLinkRecord_JSON_MetadataRoundTrip(t *testing.T) {
	t.Parallel()

	withMeta := bpfman.LinkRecord{
		ID:        bpfman.LinkID(1),
		ProgramID: kernel.ProgramID(2),
		Kind:      bpfman.LinkKindTracepoint,
		Details:   bpfman.TracepointDetails{Group: "sched", Name: "sched_switch"},
		Metadata:  map[string]string{"owner": "acme", "env": "test"},
	}
	data, err := json.Marshal(withMeta)
	require.NoError(t, err)
	var got bpfman.LinkRecord
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, map[string]string{"owner": "acme", "env": "test"}, got.Metadata)

	bare := bpfman.LinkRecord{
		ID:        bpfman.LinkID(1),
		ProgramID: kernel.ProgramID(2),
		Kind:      bpfman.LinkKindTracepoint,
		Details:   bpfman.TracepointDetails{Group: "sched", Name: "sched_switch"},
	}
	data, err = json.Marshal(bare)
	require.NoError(t, err)
	var gotBare bpfman.LinkRecord
	require.NoError(t, json.Unmarshal(data, &gotBare))
	assert.Nil(t, gotBare.Metadata, "absent metadata decodes to nil, not an empty map")
}

func TestParseTCDirection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bpfman.TCDirection
	}{
		{"ingress", bpfman.TCDirectionIngress},
		{"egress", bpfman.TCDirectionEgress},
		{"Ingress", bpfman.TCDirectionIngress},
		{"  egress  ", bpfman.TCDirectionEgress},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := bpfman.ParseTCDirection(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	got, err := bpfman.ParseTCDirection("sideways")
	assert.Error(t, err, "unknown direction should be rejected")
	assert.Equal(t, bpfman.TCDirection(""), got, "rejected parse returns zero value")
}

// TestTCDirection_Valid pins strict membership: the zero value and an
// unrecognised value are invalid; ingress and egress are valid.
func TestTCDirection_Valid(t *testing.T) {
	t.Parallel()

	assert.False(t, bpfman.TCDirection("").Valid(), "zero value is not valid")
	assert.False(t, bpfman.TCDirection("sideways").Valid(), "unknown value is not valid")
	assert.True(t, bpfman.TCDirectionIngress.Valid())
	assert.True(t, bpfman.TCDirectionEgress.Valid())
}

// TestTCDirection_JSONRoundTrip pins the wire form: a plain string enum
// with no custom (Un)MarshalText round-trips through native encoding.
func TestTCDirection_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	for _, d := range []bpfman.TCDirection{bpfman.TCDirectionIngress, bpfman.TCDirectionEgress} {
		data, err := json.Marshal(d)
		require.NoErrorf(t, err, "marshal %s", d)
		assert.Equalf(t, `"`+d.String()+`"`, string(data), "wire form of %s", d)

		var got bpfman.TCDirection
		require.NoErrorf(t, json.Unmarshal(data, &got), "unmarshal %s", d)
		assert.Equalf(t, d, got, "round-trip %s", d)
	}
}

// TestLinkKind_Valid pins strict membership: the zero value and an
// unrecognised value are invalid; every known kind is valid.
func TestLinkKind_Valid(t *testing.T) {
	t.Parallel()

	assert.False(t, bpfman.LinkKind("").Valid(), "zero value is not valid")
	assert.False(t, bpfman.LinkKind("garbage").Valid(), "unknown value is not valid")
	for _, k := range bpfman.AllLinkKinds() {
		assert.Truef(t, k.Valid(), "%s should be valid", k)
	}
}

// TestParseLinkKind pins the boundary parser: it accepts every known
// kind name and rejects an unrecognised one with the zero value.
func TestParseLinkKind(t *testing.T) {
	t.Parallel()

	for _, name := range bpfman.LinkKindNames() {
		k, err := bpfman.ParseLinkKind(name)
		require.NoErrorf(t, err, "ParseLinkKind(%q)", name)
		assert.Equalf(t, name, k.String(), "round-trip name %q", name)
	}

	k, err := bpfman.ParseLinkKind("garbage")
	assert.Error(t, err, "unknown kind should be rejected")
	assert.Equal(t, bpfman.LinkKind(""), k, "rejected parse returns zero value")
}

// TestLinkKind_JSONRoundTrip pins the wire form: a plain string enum
// with no custom (Un)MarshalText round-trips through native encoding.
func TestLinkKind_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	for _, k := range bpfman.AllLinkKinds() {
		data, err := json.Marshal(k)
		require.NoErrorf(t, err, "marshal %s", k)
		assert.Equalf(t, `"`+k.String()+`"`, string(data), "wire form of %s", k)

		var got bpfman.LinkKind
		require.NoErrorf(t, json.Unmarshal(data, &got), "unmarshal %s", k)
		assert.Equalf(t, k, got, "round-trip %s", k)
	}
}

// TestLinkAttachKindDetailsType_CoversEveryAttachKind asserts
// that every attach subcommand keyword in LinkAttachKinds()
// resolves to a concrete reflect.Type and that an unknown
// keyword resolves to nil.
func TestLinkAttachKindDetailsType_CoversEveryAttachKind(t *testing.T) {
	t.Parallel()

	want := map[string]reflect.Type{
		"xdp":        reflect.TypeFor[bpfman.XDPDetails](),
		"tc":         reflect.TypeFor[bpfman.TCDetails](),
		"tcx":        reflect.TypeFor[bpfman.TCXDetails](),
		"tracepoint": reflect.TypeFor[bpfman.TracepointDetails](),
		"kprobe":     reflect.TypeFor[bpfman.KprobeDetails](),
		"uprobe":     reflect.TypeFor[bpfman.UprobeDetails](),
		"fentry":     reflect.TypeFor[bpfman.FentryDetails](),
		"fexit":      reflect.TypeFor[bpfman.FexitDetails](),
	}

	for _, kind := range bpfman.LinkAttachKinds() {
		assert.Equal(t, want[kind], bpfman.LinkAttachKindDetailsType(kind), "attach kind %q", kind)
	}

	assert.Nil(t, bpfman.LinkAttachKindDetailsType("nonexistent_kind"))
}

// TestLinkListResult_EmptyMarshalsAsEmptyArray pins the wire
// contract that an empty link list serialises as `"links": []`,
// never `"links": null`. The shell binds list results through
// ValueFromStruct -> json.Marshal, and a `null` payload would
// break consumer jq expressions such as `.links[]`. The producer
// (manager.ListLinks) is responsible for returning a non-nil
// slice on the empty case; this test pins the resulting wire
// shape so an accidental regression in the producer is caught
// at the shell-facing boundary rather than in distant e2e
// scripts.
func TestLinkListResult_EmptyMarshalsAsEmptyArray(t *testing.T) {
	t.Parallel()

	result := bpfman.LinkListResult{Links: []bpfman.LinkRecord{}}
	data, err := json.Marshal(result)
	require.NoError(t, err)
	assert.JSONEq(t, `{"links": []}`, string(data))
}

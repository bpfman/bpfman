package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

func TestOriginKind_String(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind semantics.OriginKind
		want string
	}{
		{semantics.OriginUnknown, "unknown"},
		{semantics.OriginScalar, "scalar"},
		{semantics.OriginBool, "boolean"},
		{semantics.OriginProgram, "program"},
		{semantics.OriginLink, "link"},
		{semantics.OriginDispatcher, "dispatcher"},
		{semantics.OriginMap, "map"},
		{semantics.OriginEnvelope, "result"},
		{semantics.OriginNetnsVethPair, "netns-veth-pair"},
		{semantics.OriginNetnsVethEndpoint, "netns-veth-pair endpoint"},
		{semantics.OriginKind(999), "OriginKind(999)"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.kind.String())
		})
	}
}

func TestValue_KindDefaultsToUnknown(t *testing.T) {
	t.Parallel()

	v, err := ValueFromJSON([]byte(`{"a":1}`))
	require.NoError(t, err)
	assert.Equal(t, semantics.OriginUnknown, v.Kind())

	v = ValueFromMap(map[string]any{"a": 1})
	assert.Equal(t, semantics.OriginUnknown, v.Kind())

	type S struct{ X int }
	v, err = ValueFromStruct(S{X: 1})
	require.NoError(t, err)
	assert.Equal(t, semantics.OriginUnknown, v.Kind())
}

func TestValue_ConstructorKinds(t *testing.T) {
	t.Parallel()

	assert.Equal(t, semantics.OriginScalar, StringValue("x").Kind())
	assert.Equal(t, semantics.OriginBool, BoolValue(true).Kind())
}

func TestValue_InternalOriginMirrorKeysMatchSealedShapeFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		kind semantics.OriginKind
		v    Value
	}{
		{
			name: "envelope",
			kind: semantics.OriginEnvelope,
			v: ValueFromEnvelope(Envelope{
				ExitCode: 143,
				Stdout:   "out",
				Stderr:   "err",
				Killed:   true,
				Signal:   "TERM",
				HasPID:   true,
				PID:      4321,
			}),
		},
		{
			name: "job",
			kind: semantics.OriginJob,
			v:    ValueFromJob(&Job{PID: 4321, TargetBinary: "/usr/bin/sleep"}),
		},
		{
			name: semantics.OriginNetPair.String(),
			kind: semantics.OriginNetPair,
			v: ValueFromNetPair(&NetPair{
				Ns:          "ns0",
				HostLink:    "veth0",
				PeerLink:    "veth1",
				HostAddr:    "192.0.2.1",
				PeerAddr:    "192.0.2.2",
				HostIfindex: 10,
				HostNsid:    20,
			}),
		},
		{
			name: semantics.OriginKfunc.String(),
			kind: semantics.OriginKfunc,
			v:    ValueFromKfunc(&Kfunc{Index: 1, Name: "bpfman_test", Trigger: "/sys/kernel/debug/tracing/events/foo", Count: "/sys/kernel/debug/tracing/events/foo/count"}),
		},
		{
			name: semantics.OriginNetnsVethPair.String(),
			kind: semantics.OriginNetnsVethPair,
			v:    ValueFromNetnsVethPair(testNetnsVethPair()),
		},
		{
			name: semantics.OriginNetnsVethEndpoint.String(),
			kind: semantics.OriginNetnsVethEndpoint,
			v:    valueFromNetnsVethEndpoint(testNetnsVethPair().A),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.kind, tc.v.Kind())
			shape := semantics.KindShape(tc.kind)
			require.True(t, shape.Sealed, "%s shape must be sealed", tc.kind)
			raw, ok := tc.v.Raw().(map[string]any)
			require.True(t, ok, "%s mirror should be a JSON object, got %T", tc.kind, tc.v.Raw())
			assert.ElementsMatch(t, shapeFieldNames(shape), valueFieldNames(raw))
		})
	}
}

func TestValue_WithKind(t *testing.T) {
	t.Parallel()

	v := StringValue("x").WithKind(semantics.OriginProgram)
	assert.Equal(t, semantics.OriginProgram, v.Kind())

	// WithKind returns a copy; original is unchanged.
	base := StringValue("x")
	tagged := base.WithKind(semantics.OriginLink)
	assert.Equal(t, semantics.OriginScalar, base.Kind())
	assert.Equal(t, semantics.OriginLink, tagged.Kind())
}

func shapeFieldNames(shape semantics.Shape) []string {
	names := make([]string, 0, len(shape.Fields))
	for name := range shape.Fields {
		names = append(names, name)
	}
	return names
}

func valueFieldNames(raw map[string]any) []string {
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	return names
}

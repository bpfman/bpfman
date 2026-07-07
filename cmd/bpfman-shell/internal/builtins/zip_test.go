package builtins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

// zipCall wraps args in a minimal driver.Ctx for HandleZip.
func zipCall(t *testing.T, args []runtime.Arg) (runtime.Value, error) {
	return HandleZip(driver.Ctx{Ctx: t.Context(), Args: args})
}

// listArg wraps a []any as a StructuredValueArg suitable for
// HandleZip. The constructor matches what dispatchPureCall would
// supply at runtime when the caller writes (zip $xs $ys).
func listArg(name string, elems []any) runtime.Arg {
	return runtime.StructuredValueArg{Name: name, Value: runtime.ValueFromAny(elems)}
}

func TestZip_EmptyLists(t *testing.T) {
	t.Parallel()
	v, err := zipCall(t, []runtime.Arg{listArg("a", []any{}), listArg("b", []any{})})
	require.NoError(t, err)
	raw, ok := v.Raw().([]any)
	require.True(t, ok, "zip result should be []any, got %T", v.Raw())
	assert.Empty(t, raw)
}

func TestZip_EqualLengthLists(t *testing.T) {
	t.Parallel()
	a := listArg("a", []any{"x", "y", "z"})
	b := listArg("b", []any{"1", "2", "3"})
	v, err := zipCall(t, []runtime.Arg{a, b})
	require.NoError(t, err)
	raw, ok := v.Raw().([]any)
	require.True(t, ok)
	require.Len(t, raw, 3)
	for i, want := range [][]any{
		{"x", "1"},
		{"y", "2"},
		{"z", "3"},
	} {
		pair, ok := raw[i].([]any)
		require.True(t, ok, "element %d should be []any, got %T", i, raw[i])
		assert.Equal(t, want, pair)
	}
}

func TestZip_LengthMismatchIsError(t *testing.T) {
	t.Parallel()
	a := listArg("a", []any{"x", "y", "z"})
	b := listArg("b", []any{"1", "2"})
	_, err := zipCall(t, []runtime.Arg{a, b})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "length mismatch")
}

func TestZip_FirstArgNotListIsError(t *testing.T) {
	t.Parallel()
	_, err := zipCall(t, []runtime.Arg{
		runtime.WordArg{Text: "not-a-list"},
		listArg("b", []any{"1"}),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "arg 0 must be a list")
}

func TestZip_SecondArgNotListIsError(t *testing.T) {
	t.Parallel()
	_, err := zipCall(t, []runtime.Arg{
		listArg("a", []any{"x"}),
		runtime.ScalarValueArg{Text: "scalar"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "arg 1 must be a list")
}

func TestZip_WrongArityIsError(t *testing.T) {
	t.Parallel()
	_, err := zipCall(t, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected exactly 2")

	_, err = zipCall(t, []runtime.Arg{listArg("a", []any{"x"})})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected exactly 2")

	_, err = zipCall(t, []runtime.Arg{
		listArg("a", []any{"x"}),
		listArg("b", []any{"1"}),
		listArg("c", []any{"!"}),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected exactly 2")
}

func TestZip_PairElementsAreReadableAsLists(t *testing.T) {
	t.Parallel()
	a := listArg("a", []any{"p1", "p2"})
	b := listArg("b", []any{"q1", "q2"})
	v, err := zipCall(t, []runtime.Arg{a, b})
	require.NoError(t, err)

	first := v.IndexValue(0)
	require.False(t, first.IsNil(), "first pair should be a present value")
	pair, ok := first.Raw().([]any)
	require.True(t, ok, "pair should be []any, got %T", first.Raw())
	assert.Equal(t, []any{"p1", "q1"}, pair)

	zeroth := first.IndexValue(0)
	require.False(t, zeroth.IsNil())
	assert.Equal(t, "p1", zeroth.Raw())

	first2 := first.IndexValue(1)
	require.False(t, first2.IsNil())
	assert.Equal(t, "q1", first2.Raw())
}

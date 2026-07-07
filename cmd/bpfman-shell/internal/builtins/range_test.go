package builtins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

// rangeCall wraps args in a minimal driver.Ctx for HandleRange.
func rangeCall(t *testing.T, args []runtime.Arg) (runtime.Value, error) {
	return HandleRange(driver.Ctx{Ctx: t.Context(), Args: args})
}

func TestRange_ProducesZeroIndexedSequence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want []any
	}{
		{"0", []any{}},
		{"1", []any{anyN("0")}},
		{"5", []any{anyN("0"), anyN("1"), anyN("2"), anyN("3"), anyN("4")}},
	}
	for _, c := range tests {
		t.Run(c.in, func(t *testing.T) {
			t.Parallel()
			v, err := rangeCall(t, []runtime.Arg{runtime.WordArg{Text: c.in}})
			require.NoError(t, err)
			raw, ok := v.Raw().([]any)
			require.True(t, ok, "range result should be []any, got %T", v.Raw())
			if len(c.want) == 0 {
				assert.Empty(t, raw)
			} else {
				assert.Equal(t, c.want, raw)
			}
		})
	}
}

func TestRange_NegativeIsError(t *testing.T) {
	t.Parallel()
	_, err := rangeCall(t, []runtime.Arg{runtime.WordArg{Text: "-3"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "negative")
}

func TestRange_NonIntegerIsError(t *testing.T) {
	t.Parallel()
	_, err := rangeCall(t, []runtime.Arg{runtime.WordArg{Text: "x"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid integer")
}

func TestRange_ExceedingMaxIsError(t *testing.T) {
	t.Parallel()
	_, err := rangeCall(t, []runtime.Arg{runtime.WordArg{Text: "4294967295"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds the maximum")
}

func TestRange_WrongArityIsError(t *testing.T) {
	t.Parallel()
	_, err := rangeCall(t, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected exactly 1")
}

// anyN constructs a json.Number-wrapped element matching what
// rangeCall emits, so the test asserts against the exact runtime
// shape foreach iterates.
func anyN(s string) any {
	v, err := runtime.ValueFromJSON([]byte(s))
	if err != nil {
		panic(err)
	}
	return v.Raw()
}

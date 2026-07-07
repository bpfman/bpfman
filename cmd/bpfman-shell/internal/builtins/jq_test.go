package builtins

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

// jqCall invokes HandleJQ with the minimal driver.Ctx the handler
// reads (just Args; Ctx is set for symmetry with other handlers
// but jq does not consult it).
func jqCall(t *testing.T, args []runtime.Arg) (runtime.Value, error) {
	return HandleJQ(driver.Ctx{Ctx: t.Context(), Args: args})
}

// HandleJQ is the "jq FILTER VALUE" shell builtin. Scalars pass
// through, structured values are walked, and aggregation filters
// (add, length, map, select, group_by) all reduce to a Value.

func TestJQ_IdentityOnJSONScalar(t *testing.T) {
	t.Parallel()

	v, err := jqCall(t, []runtime.Arg{
		runtime.WordArg{Text: "."},
		runtime.QuotedArg{Text: `"hello"`},
	})
	require.NoError(t, err)
	s, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "hello", s)
}

func TestJQ_IdentityOnJSONNumber(t *testing.T) {
	t.Parallel()

	v, err := jqCall(t, []runtime.Arg{
		runtime.WordArg{Text: "."},
		runtime.WordArg{Text: "42"},
	})
	require.NoError(t, err)
	s, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "42", s)
}

func TestJQ_ScalarNotValidJSONIsError(t *testing.T) {
	t.Parallel()

	_, err := jqCall(t, []runtime.Arg{
		runtime.WordArg{Text: "."},
		runtime.ScalarValueArg{Text: "hello"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not valid JSON")
}

func TestJQ_PathOnStructured(t *testing.T) {
	t.Parallel()

	input := runtime.ValueFromMap(map[string]any{"a": "apple", "b": "banana"})
	v, err := jqCall(t, []runtime.Arg{
		runtime.WordArg{Text: ".a"},
		runtime.StructuredValueArg{Value: input},
	})
	require.NoError(t, err)
	s, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "apple", s)
}

func TestJQ_ShellResolvedScalarStringIsInputValue(t *testing.T) {
	t.Parallel()

	input := runtime.StringValue("hello")
	v, err := jqCall(t, []runtime.Arg{
		runtime.WordArg{Text: "."},
		runtime.ScalarValueArg{Text: "hello", Value: input, HasValue: true},
	})
	require.NoError(t, err)
	s, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "hello", s)
}

func TestJQ_ListValueIsInputArray(t *testing.T) {
	t.Parallel()

	input := runtime.ValueFromAny([]any{
		json.Number("1"),
		json.Number("2"),
		json.Number("3"),
	})
	v, err := jqCall(t, []runtime.Arg{
		runtime.QuotedArg{Text: "add"},
		runtime.StructuredValueArg{Value: input},
	})
	require.NoError(t, err)
	s, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "6", s)
}

func TestJQ_RecordValueIsInputObject(t *testing.T) {
	t.Parallel()

	input := runtime.ValueFromRecord(map[string]runtime.Value{
		"name":  runtime.StringValue("demo"),
		"count": runtime.ValueFromAny(json.Number("3")),
	})
	v, err := jqCall(t, []runtime.Arg{
		runtime.QuotedArg{Text: `.name + ":" + (.count | tostring)`},
		runtime.StructuredValueArg{Value: input},
	})
	require.NoError(t, err)
	s, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "demo:3", s)
}

func TestJQ_StructValueIsInputObject(t *testing.T) {
	t.Parallel()

	type sample struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	input, err := runtime.ValueFromStruct(sample{Name: "demo", Count: 3})
	require.NoError(t, err)
	v, err := jqCall(t, []runtime.Arg{
		runtime.QuotedArg{Text: `.name + ":" + (.count | tostring)`},
		runtime.StructuredValueArg{Value: input},
	})
	require.NoError(t, err)
	s, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "demo:3", s)
}

func TestJQ_AggregateSum(t *testing.T) {
	t.Parallel()

	input := runtime.ValueFromMap(map[string]any{
		"items": []any{
			map[string]any{"v": json.Number("1")},
			map[string]any{"v": json.Number("2")},
			map[string]any{"v": json.Number("3")},
		},
	})
	v, err := jqCall(t, []runtime.Arg{
		runtime.QuotedArg{Text: "[.items[].v] | add"},
		runtime.StructuredValueArg{Value: input},
	})
	require.NoError(t, err)
	s, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "6", s)
}

func TestJQ_Length(t *testing.T) {
	t.Parallel()

	input := runtime.ValueFromMap(map[string]any{
		"items": []any{"a", "b", "c"},
	})
	v, err := jqCall(t, []runtime.Arg{
		runtime.QuotedArg{Text: ".items | length"},
		runtime.StructuredValueArg{Value: input},
	})
	require.NoError(t, err)
	s, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "3", s)
}

func TestJQ_Map(t *testing.T) {
	t.Parallel()

	input := runtime.ValueFromMap(map[string]any{
		"items": []any{
			map[string]any{"name": "foo"},
			map[string]any{"name": "bar"},
		},
	})
	v, err := jqCall(t, []runtime.Arg{
		runtime.QuotedArg{Text: ".items | map(.name)"},
		runtime.StructuredValueArg{Value: input},
	})
	require.NoError(t, err)
	require.True(t, v.IsStructured())
	raw, ok := v.Raw().([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"foo", "bar"}, raw)
}

func TestJQ_MultiResultCollected(t *testing.T) {
	t.Parallel()

	input := runtime.ValueFromMap(map[string]any{
		"items": []any{"a", "b", "c"},
	})
	v, err := jqCall(t, []runtime.Arg{
		runtime.QuotedArg{Text: ".items[]"},
		runtime.StructuredValueArg{Value: input},
	})
	require.NoError(t, err)
	require.True(t, v.IsStructured())
	raw, ok := v.Raw().([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"a", "b", "c"}, raw)
}

func TestJQ_BooleanResultIsOriginBool(t *testing.T) {
	t.Parallel()

	input := runtime.ValueFromMap(map[string]any{"a": json.Number("5")})
	v, err := jqCall(t, []runtime.Arg{
		runtime.QuotedArg{Text: ".a > 3"},
		runtime.StructuredValueArg{Value: input},
	})
	require.NoError(t, err)
	b, err := runtime.AsBool(v)
	require.NoError(t, err)
	assert.True(t, b)
}

func TestJQ_NullResultIsPresentNull(t *testing.T) {
	t.Parallel()

	input := runtime.ValueFromMap(map[string]any{"a": "apple"})
	v, err := jqCall(t, []runtime.Arg{
		runtime.WordArg{Text: ".missing"},
		runtime.StructuredValueArg{Value: input},
	})
	require.NoError(t, err)
	assert.False(t, v.IsNil(), "null result must be a present value, not absent")
	assert.True(t, v.IsNull(), "kind should be OriginNull for jq null results")
	s, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "null", s)
}

func TestJQ_InvalidFilter(t *testing.T) {
	t.Parallel()

	_, err := jqCall(t, []runtime.Arg{
		runtime.QuotedArg{Text: "{{{ not valid"},
		runtime.ScalarValueArg{Text: "x"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "jq")
}

func TestJQ_WrongArgCount(t *testing.T) {
	t.Parallel()

	_, err := jqCall(t, nil)
	require.Error(t, err)
	_, err = jqCall(t, []runtime.Arg{runtime.WordArg{Text: "."}})
	require.Error(t, err)
	_, err = jqCall(t, []runtime.Arg{
		runtime.WordArg{Text: "."},
		runtime.ScalarValueArg{Text: "x"},
		runtime.ScalarValueArg{Text: "y"},
	})
	require.Error(t, err)
}

func TestJQ_FlagArgGetsHint(t *testing.T) {
	t.Parallel()

	_, err := jqCall(t, []runtime.Arg{
		runtime.WordArg{Text: "-c"},
		runtime.QuotedArg{Text: `"x"`},
		runtime.QuotedArg{Text: `"y"`},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "filter-only")
	assert.Contains(t, err.Error(), "-c")
}

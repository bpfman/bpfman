package runtime

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValueFromJSON(t *testing.T) {
	t.Parallel()

	t.Run("object", func(t *testing.T) {
		t.Parallel()
		v, err := ValueFromJSON([]byte(`{"id": 42, "name": "test"}`))
		require.NoError(t, err)
		assert.True(t, v.IsStructured())
		assert.False(t, v.IsScalar())
		assert.False(t, v.IsNil())
	})

	t.Run("string", func(t *testing.T) {
		t.Parallel()
		v, err := ValueFromJSON([]byte(`"hello"`))
		require.NoError(t, err)
		assert.True(t, v.IsScalar())
		assert.False(t, v.IsStructured())
		s, err := v.Scalar()
		require.NoError(t, err)
		assert.Equal(t, "hello", s)
	})

	t.Run("number preserved as json.Number", func(t *testing.T) {
		t.Parallel()
		v, err := ValueFromJSON([]byte(`42`))
		require.NoError(t, err)
		_, ok := v.Raw().(json.Number)
		assert.True(t, ok, "expected json.Number, got %T", v.Raw())
	})

	t.Run("null preserves IsNull distinction", func(t *testing.T) {
		t.Parallel()
		// A top-level JSON null is a value -- the JSON
		// null literal -- not the absent Value{} sentinel.
		// The deliberate IsNull / IsNil split in the Value
		// vocabulary lives across every entry point that
		// produces a Value: matches blocks (`field: null`),
		// the null / present / missing shape predicates,
		// the path-walk landing on a null terminal already
		// honoured via LookupValue. ValueFromJSON must
		// follow the same contract so a Value built from
		// JSON does not lose the distinction at the
		// boundary.
		v, err := ValueFromJSON([]byte(`null`))
		require.NoError(t, err)
		assert.True(t, v.IsNull(), "JSON null must surface as IsNull")
		assert.False(t, v.IsNil(), "explicit null is a present value, not absent")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		t.Parallel()
		_, err := ValueFromJSON([]byte(`{invalid`))
		require.Error(t, err)
	})

	t.Run("trailing garbage after value", func(t *testing.T) {
		t.Parallel()
		_, err := ValueFromJSON([]byte(`123 junk`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "trailing data")
	})

	t.Run("trailing garbage after object", func(t *testing.T) {
		t.Parallel()
		_, err := ValueFromJSON([]byte(`{"a":1} {"b":2}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "trailing data")
	})

	t.Run("trailing close-bracket after value", func(t *testing.T) {
		t.Parallel()
		// dec.More() returns false when the next non-whitespace
		// byte is ']' or '}' because the stdlib treats those as
		// "no more elements in the current array/object". At
		// the top level there is no array or object, so a
		// stray ']' or '}' after an otherwise well-formed
		// value should be reported as trailing data rather
		// than silently accepted.
		_, err := ValueFromJSON([]byte(`123 ]`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "trailing data")
	})

	t.Run("trailing close-brace after value", func(t *testing.T) {
		t.Parallel()
		_, err := ValueFromJSON([]byte(`123 }`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "trailing data")
	})

	t.Run("trailing whitespace is not trailing garbage", func(t *testing.T) {
		t.Parallel()
		v, err := ValueFromJSON([]byte("42  \n  "))
		require.NoError(t, err)
		s, err := v.Scalar()
		require.NoError(t, err)
		assert.Equal(t, "42", s)
	})
}

func TestValueFromStruct(t *testing.T) {
	t.Parallel()

	type Result struct {
		ID   uint32 `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	}

	v, err := ValueFromStruct(Result{ID: 123, Name: "my_prog", Type: "tracepoint"})
	require.NoError(t, err)
	assert.True(t, v.IsStructured())

	// Verify fields accessible via Lookup.
	id, err := v.Lookup("result", "id")
	require.NoError(t, err)
	s, err := id.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "123", s)

	name, err := v.Lookup("result", "name")
	require.NoError(t, err)
	s, err = name.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "my_prog", s)
}

func TestValueConvenience(t *testing.T) {
	t.Parallel()

	t.Run("StringValue", func(t *testing.T) {
		t.Parallel()
		v := StringValue("hello")
		assert.True(t, v.IsScalar())
		s, err := v.Scalar()
		require.NoError(t, err)
		assert.Equal(t, "hello", s)
	})

	t.Run("BoolValue", func(t *testing.T) {
		t.Parallel()
		v := BoolValue(true)
		assert.True(t, v.IsScalar())
		s, err := v.Scalar()
		require.NoError(t, err)
		assert.Equal(t, "true", s)
	})
}

func TestValueScalar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   Value
		want    string
		wantErr string
	}{
		{
			name:  "string",
			value: StringValue("hello"),
			want:  "hello",
		},
		{
			name:  "json.Number int",
			value: Value{v: json.Number("42")},
			want:  "42",
		},
		{
			name:  "json.Number float",
			value: Value{v: json.Number("3.14")},
			want:  "3.14",
		},
		{
			name:  "bool true",
			value: BoolValue(true),
			want:  "true",
		},
		{
			name:  "bool false",
			value: BoolValue(false),
			want:  "false",
		},
		{
			name:  "float64",
			value: Value{v: float64(2.5)},
			want:  "2.5",
		},
		{
			name:    "nil",
			value:   Value{v: nil},
			wantErr: "value is null",
		},
		{
			name:    "map",
			value:   ValueFromMap(map[string]any{"a": 1}),
			wantErr: "value is not a scalar",
		},
		{
			name:    "slice",
			value:   Value{v: []any{1, 2, 3}},
			wantErr: "value is not a scalar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.value.Scalar()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValueLookup(t *testing.T) {
	t.Parallel()

	// Build a structured value with nested fields and arrays.
	data := map[string]any{
		"id":   json.Number("42"),
		"name": "test_prog",
		"maps": []any{
			map[string]any{"name": "counts", "pin": "/sys/fs/bpf/counts"},
			map[string]any{"name": "events", "pin": "/sys/fs/bpf/events"},
		},
		"details": map[string]any{
			"kernel_id": json.Number("99"),
			"type":      "tracepoint",
		},
		"nullable": nil,
		"nested_arr": []any{
			[]any{"a", "b"},
		},
	}
	v := ValueFromMap(data)

	tests := []struct {
		name    string
		path    string
		want    string
		wantErr string
	}{
		{
			name: "simple field",
			path: "name",
			want: "test_prog",
		},
		{
			name: "numeric field",
			path: "id",
			want: "42",
		},
		{
			name: "nested field",
			path: "details.kernel_id",
			want: "99",
		},
		{
			name: "nested dotted field",
			path: "details.type",
			want: "tracepoint",
		},
		{
			name: "array index then field",
			path: "maps[0].name",
			want: "counts",
		},
		{
			name: "array second element",
			path: "maps[1].pin",
			want: "/sys/fs/bpf/events",
		},
		{
			name:    "missing field",
			path:    "nonexistent",
			wantErr: "field nonexistent not found in variable v",
		},
		{
			name:    "missing nested field",
			path:    "details.missing",
			wantErr: "field missing not found in variable v.details",
		},
		{
			name:    "index out of range",
			path:    "maps[5]",
			wantErr: "index 5 out of range for variable v.maps (length 2)",
		},
		{
			name:    "null field",
			path:    "nullable",
			wantErr: "variable v.nullable is null",
		},
		{
			name:    "object leaf",
			path:    "details",
			wantErr: "variable v.details is an object; use field access to reach a scalar value",
		},
		{
			name:    "array leaf",
			path:    "maps",
			wantErr: "variable v.maps is an array; use indexing to reach a scalar value",
		},
		{
			name:    "index on non-array",
			path:    "name[0]",
			wantErr: "cannot index non-array in variable v.name",
		},
		{
			name:    "field on non-object",
			path:    "id.sub",
			wantErr: "cannot access field sub on non-object in variable v.id",
		},
		{
			name: "empty path returns value itself (structured triggers error)",
			path: "",
			// Empty path returns the value, which is a map,
			// so Lookup returns it without error -- but the
			// caller would get an error from Scalar().
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := v.Lookup("v", tt.path)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.want != "" {
				s, err := got.Scalar()
				require.NoError(t, err)
				assert.Equal(t, tt.want, s)
			}
		})
	}
}

func TestValueLookupEmptyPath(t *testing.T) {
	t.Parallel()

	v := StringValue("hello")
	got, err := v.Lookup("v", "")
	require.NoError(t, err)
	s, err := got.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "hello", s)
}

func TestValuePrecision(t *testing.T) {
	t.Parallel()

	t.Run("large uint32", func(t *testing.T) {
		t.Parallel()
		v, err := ValueFromJSON([]byte(`{"id": 4294967295}`))
		require.NoError(t, err)
		id, err := v.Lookup("v", "id")
		require.NoError(t, err)
		s, err := id.Scalar()
		require.NoError(t, err)
		assert.Equal(t, "4294967295", s)
	})

	t.Run("2^53+1 via json.Number", func(t *testing.T) {
		t.Parallel()
		v, err := ValueFromJSON([]byte(`{"big": 9007199254740993}`))
		require.NoError(t, err)
		big, err := v.Lookup("v", "big")
		require.NoError(t, err)
		s, err := big.Scalar()
		require.NoError(t, err)
		assert.Equal(t, "9007199254740993", s)
	})
}

func TestValueLookupValue(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"id":   json.Number("42"),
		"name": "test_prog",
		"maps": []any{
			map[string]any{"name": "counts", "pin": "/sys/fs/bpf/counts"},
			map[string]any{"name": "events", "pin": "/sys/fs/bpf/events"},
		},
		"details": map[string]any{
			"kernel_id": json.Number("99"),
			"type":      "tracepoint",
		},
		"nullable": nil,
	}
	v := ValueFromMap(data)

	t.Run("returns structured map", func(t *testing.T) {
		t.Parallel()
		got, err := v.LookupValue("v", "details")
		require.NoError(t, err)
		assert.True(t, got.IsStructured())
		m, ok := got.Raw().(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "tracepoint", m["type"])
	})

	t.Run("returns structured array", func(t *testing.T) {
		t.Parallel()
		got, err := v.LookupValue("v", "maps")
		require.NoError(t, err)
		assert.True(t, got.IsStructured())
		arr, ok := got.Raw().([]any)
		require.True(t, ok)
		assert.Len(t, arr, 2)
	})

	t.Run("returns explicit null for JSON null terminal", func(t *testing.T) {
		t.Parallel()
		// A path landing on an explicit JSON null is a value
		// that happens to be null, not the absent slot a bare
		// Value{} represents. The two halves of the Value
		// vocabulary are deliberately distinct (IsNull vs
		// IsNil): predicates like `field: null` in a matches
		// block, the present/missing/strict-null shape tests,
		// and any caller chaining a further lookup all rely
		// on the lookup preserving the distinction rather
		// than collapsing the null carrier back to "absent".
		got, err := v.LookupValue("v", "nullable")
		require.NoError(t, err)
		assert.True(t, got.IsNull(), "terminal JSON null must surface as IsNull")
		assert.False(t, got.IsNil(), "explicit null is a present value, not absent")
	})

	t.Run("returns scalar", func(t *testing.T) {
		t.Parallel()
		got, err := v.LookupValue("v", "name")
		require.NoError(t, err)
		assert.True(t, got.IsScalar())
		s, err := got.Scalar()
		require.NoError(t, err)
		assert.Equal(t, "test_prog", s)
	})

	t.Run("nested array element", func(t *testing.T) {
		t.Parallel()
		got, err := v.LookupValue("v", "maps[0]")
		require.NoError(t, err)
		assert.True(t, got.IsStructured())
		m, ok := got.Raw().(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "counts", m["name"])
	})

	t.Run("empty path returns whole value", func(t *testing.T) {
		t.Parallel()
		got, err := v.LookupValue("v", "")
		require.NoError(t, err)
		assert.True(t, got.IsStructured())
	})

	t.Run("missing field errors", func(t *testing.T) {
		t.Parallel()
		_, err := v.LookupValue("v", "nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "field nonexistent not found")
	})

	t.Run("index out of range errors", func(t *testing.T) {
		t.Parallel()
		_, err := v.LookupValue("v", "maps[5]")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "index 5 out of range")
	})
}

func TestValueKeys(t *testing.T) {
	t.Parallel()

	t.Run("map returns sorted keys", func(t *testing.T) {
		t.Parallel()
		v := ValueFromMap(map[string]any{
			"zebra": "z",
			"alpha": "a",
			"mid":   "m",
		})
		assert.Equal(t, []string{"alpha", "mid", "zebra"}, v.Keys())
	})

	t.Run("array returns index strings", func(t *testing.T) {
		t.Parallel()
		v := Value{v: []any{"a", "b", "c"}}
		assert.Equal(t, []string{"[0]", "[1]", "[2]"}, v.Keys())
	})

	t.Run("empty map returns empty slice", func(t *testing.T) {
		t.Parallel()
		v := ValueFromMap(map[string]any{})
		assert.Empty(t, v.Keys())
	})

	t.Run("empty array returns empty slice", func(t *testing.T) {
		t.Parallel()
		v := Value{v: []any{}}
		assert.Equal(t, []string{}, v.Keys())
	})

	t.Run("scalar returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, StringValue("hello").Keys())
	})

	t.Run("nil returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, Value{}.Keys())
	})

	t.Run("number returns nil", func(t *testing.T) {
		t.Parallel()
		v := Value{v: json.Number("42")}
		assert.Nil(t, v.Keys())
	})

	t.Run("bool returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, BoolValue(true).Keys())
	})
}

func TestValueIsPredicates(t *testing.T) {
	t.Parallel()

	assert.True(t, Value{}.IsNil())
	assert.False(t, Value{}.IsScalar())
	assert.False(t, Value{}.IsStructured())

	assert.False(t, StringValue("x").IsNil())
	assert.True(t, StringValue("x").IsScalar())
	assert.False(t, StringValue("x").IsStructured())

	m := ValueFromMap(map[string]any{})
	assert.False(t, m.IsNil())
	assert.False(t, m.IsScalar())
	assert.True(t, m.IsStructured())
}

func TestValueFromStruct_PreservesOrigin(t *testing.T) {
	t.Parallel()

	type MyStruct struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	orig := MyStruct{ID: 1, Name: "test"}
	v, err := ValueFromStruct(orig)
	require.NoError(t, err)
	assert.Equal(t, orig, v.Origin())
}

func TestValueFromJSON_NilOrigin(t *testing.T) {
	t.Parallel()

	v, err := ValueFromJSON([]byte(`{"id": 1}`))
	require.NoError(t, err)
	assert.Nil(t, v.Origin())
}

func TestStringValue_NilOrigin(t *testing.T) {
	t.Parallel()

	v := StringValue("hello")
	assert.Nil(t, v.Origin())
}

func TestRenderValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		val  Value
		want string
	}{
		{
			name: "scalar string",
			val:  StringValue("hello world"),
			want: "hello world",
		},
		{
			name: "empty string",
			val:  StringValue(""),
			want: "",
		},
		{
			name: "string with trailing newline preserved",
			val:  StringValue("line1\nline2\n"),
			want: "line1\nline2\n",
		},
		{
			name: "string without trailing newline stays without",
			val:  StringValue("no newline"),
			want: "no newline",
		},
		{
			name: "json.Number integer",
			val:  Value{v: json.Number("42")},
			want: "42",
		},
		{
			name: "json.Number float",
			val:  Value{v: json.Number("3.14")},
			want: "3.14",
		},
		{
			name: "float64",
			val:  Value{v: float64(2.5)},
			want: "2.5",
		},
		{
			name: "bool true",
			val:  BoolValue(true),
			want: "true",
		},
		{
			name: "bool false",
			val:  BoolValue(false),
			want: "false",
		},
		{
			name: "null",
			val:  Value{},
			want: "null",
		},
		{
			name: "structured object with sorted keys",
			val: ValueFromMap(map[string]any{
				"zebra": "z",
				"alpha": "a",
			}),
			want: "{\n  \"alpha\": \"a\",\n  \"zebra\": \"z\"\n}\n",
		},
		{
			name: "structured array",
			val:  Value{v: []any{"a", "b", "c"}},
			want: "[\n  \"a\",\n  \"b\",\n  \"c\"\n]\n",
		},
		{
			name: "nested structured value",
			val: ValueFromMap(map[string]any{
				"items": []any{
					map[string]any{"id": json.Number("1"), "name": "first"},
				},
			}),
			want: "{\n  \"items\": [\n    {\n      \"id\": 1,\n      \"name\": \"first\"\n    }\n  ]\n}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := RenderValue(tt.val)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

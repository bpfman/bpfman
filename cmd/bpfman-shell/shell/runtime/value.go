package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// ErrFieldMissing is a sentinel returned by the path walker when a
// dotted-path lookup names a field that is not present in the value
// tree. It is distinct from "the field exists but its value is JSON
// null", which the walker reports by returning a nil leaf with no
// error. Predicates that distinguish missing-from-the-shape from
// present-and-null (the verb-form present / missing / strict-null
// predicates) inspect this sentinel via errors.Is.
var ErrFieldMissing = errors.New("field not found")

// presenceState classifies a path-lookup outcome at the predicate
// boundary. It mirrors the missing/null/value distinction the
// shape-test predicates need to give precise answers without
// requiring callers to decode error sentinels themselves.
type presenceState int

const (
	// presenceMissing means the path does not resolve in the value
	// tree (an intermediate field or terminal field is missing).
	presenceMissing presenceState = iota
	// presenceNull means the path resolves and the terminal value
	// is JSON null.
	presenceNull
	// presenceValue means the path resolves and the terminal
	// value is non-null.
	presenceValue
)

// Presence is the predicate-facing outcome of a soft path lookup.
// It carries the resolved value when there is one and lets callers
// ask the semantically relevant questions directly: is the path
// missing, does it resolve to null, or does it resolve to a
// concrete non-null value?
type Presence struct {
	value Value
	state presenceState
}

// Value returns the value the path resolved to. It is the zero
// Value when the path was missing and the explicit-null carrier
// when the path resolved to JSON null.
func (p Presence) Value() Value { return p.value }

// IsMissing reports whether the path did not resolve in the value
// tree (an intermediate or terminal field is absent from the shape).
func (p Presence) IsMissing() bool { return p.state == presenceMissing }

// IsNull reports whether the path resolved and its terminal value
// is JSON null.
func (p Presence) IsNull() bool { return p.state == presenceNull }

// HasValue reports whether the path resolved to a concrete,
// non-null value.
func (p Presence) HasValue() bool { return p.state == presenceValue }

// IsPresent reports whether the path resolved at all, i.e. to either
// a concrete value or an explicit null. It is the negation of
// IsMissing.
func (p Presence) IsPresent() bool { return p.state != presenceMissing }

// Value wraps a JSON-compatible dynamic value for use as a shell
// variable. The underlying representation is one of: map[string]any,
// []any, string, json.Number, bool, or nil.
//
// When created via ValueFromStruct, the original Go value is
// preserved in the origin field so that callers can recover type
// information that the JSON round-trip erases.
//
// The kind field declares what the Value represents (see semantics.OriginKind).
// Producers set it at construction time via WithKind.
// semantics.OriginUnknown is the default and acts as a wildcard for
// origin-less values (e.g. JSON parsed without explicit tagging, map
// literals, path-lookup results).
type Value struct {
	v            any                  // JSON-decoded tree (map[string]any, etc.)
	origin       any                  // original Go value, nil for non-struct values
	kind         semantics.OriginKind // declared origin kind, semantics.OriginUnknown by default
	recordFields map[string]Value     // field values for records, preserving per-field origin
}

// ValueFromMap wraps an existing map as a Value.
func ValueFromMap(data map[string]any) Value {
	return Value{v: data}
}

// ValueFromRecord wraps named field Values as a structured record.
// The raw map is the JSON-facing representation; recordFields is
// the origin-preserving side table that lets $r.prog return the
// original typed Program value rather than an origin-less object.
func ValueFromRecord(fields map[string]Value) Value {
	raw := make(map[string]any, len(fields))
	copied := make(map[string]Value, len(fields))
	for name, v := range fields {
		raw[name] = v.Raw()
		copied[name] = v
	}
	return Value{v: raw, recordFields: copied}
}

// ValueFromAny wraps an arbitrary JSON-compatible value as a
// Value. Unlike ValueFromJSON it does not parse anything; it
// stores x directly. Suitable for integration points (e.g. gojq)
// that already produce Go-native types matching the Value's
// internal vocabulary (map[string]any, []any, string, json.Number,
// float64, bool, nil). Callers that know the domain kind should
// chain WithKind.
func ValueFromAny(x any) Value {
	return Value{v: x}
}

// ValueFromJSON decodes JSON bytes into a Value. Numbers are
// preserved as json.Number to avoid float64 precision loss. The
// resulting Value has semantics.OriginUnknown; callers that need a declared
// kind should chain WithKind.
func ValueFromJSON(b []byte) (Value, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return Value{}, fmt.Errorf("decode JSON: %w", err)
	}

	// A second Decode is the only way to reject every form of
	// trailing data at the top level. dec.More() looks right
	// for the common cases but treats ']' and '}' as natural
	// container terminators, so "123 ]" and "123 }" would slip
	// through unnoticed. Decode returns io.EOF iff the input
	// held exactly one well-formed value plus optional
	// whitespace; any other outcome -- a successful second
	// value or a syntax error mid-stream -- is trailing data
	// and must surface as such.
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Value{}, fmt.Errorf("decode JSON: trailing data after value")
		}
		return Value{}, fmt.Errorf("decode JSON: trailing data after value: %w", err)
	}

	// Preserve the IsNull / IsNil distinction at the JSON
	// boundary: a top-level `null` decodes to a nil interface,
	// but that should surface as the explicit-null carrier
	// (kind OriginNull) rather than the absent Value{} sentinel.
	// Downstream callers (matches blocks, the null / present /
	// missing predicates, comparison operators, the path-walk
	// in LookupValue) all rely on the distinction holding once
	// a Value is constructed.
	if v == nil {
		return NullValue(), nil
	}
	return Value{v: v}, nil
}

// ValueFromStruct converts a struct to a Value via JSON round-trip,
// preserving the original Go value for type checking. The resulting
// Value has semantics.OriginUnknown; callers that know the domain kind should
// chain WithKind (e.g. WithKind(semantics.OriginProgram) for a program record).
func ValueFromStruct(s any) (Value, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return Value{}, fmt.Errorf("marshal struct: %w", err)
	}

	v, err := ValueFromJSON(b)
	if err != nil {
		return Value{}, err
	}

	v.origin = s
	return v, nil
}

// Origin returns the original Go value if this Value was created
// from a struct via ValueFromStruct. Returns nil otherwise.
func (v Value) Origin() any {
	return v.origin
}

// Kind returns the declared origin kind. semantics.OriginUnknown is the
// default for values whose producer did not tag them.
func (v Value) Kind() semantics.OriginKind {
	return v.kind
}

// WithKind returns a copy of the Value with its origin kind set to
// k. Use at the construction site: `shell.ValueFromStruct(p).WithKind(shell.semantics.OriginProgram)`.
func (v Value) WithKind(k semantics.OriginKind) Value {
	v.kind = k
	return v
}

// withOrigin returns a copy of v with origin set to o and kind set
// to k. Used internally by list-building paths (lowered list
// expressions, bind-collect) to attach a parallel origin slice so foreach
// iteration and path indexing can reconstruct each element's
// typed Value. The function is unexported because callers outside
// the shell package should reach for ValueFromStruct + WithKind
// instead; this constructor exists for the list-element origin
// preservation path where origin is a parallel []any.
func (v Value) withOrigin(o any, k semantics.OriginKind) Value {
	v.origin = o
	v.kind = k
	return v
}

// IndexValue returns the i'th element of v.v (which must be []any)
// as a Value, preserving per-element origin and kind when v carries
// a parallel origin slice. Out-of-range i or non-list v returns an
// empty Value.
//
// Used by foreach iteration and bind-collect so that a list whose
// elements carry typed origins (e.g. a list of bpfman.Link records
// produced by a guard bind-collect) yields per-element Values that
// satisfy the same capability-interface dispatch in command.go as
// the original single-bind Value did. Without this, iterating a
// typed list strips kind/origin and downstream structured-arg
// calls fail with "no kernel ID capability".
func (v Value) IndexValue(i int) Value {
	list, ok := v.v.([]any)
	if !ok || i < 0 || i >= len(list) {
		return Value{}
	}
	out := Value{v: list[i]}
	if origin, kind := semantics.WalkSuborigin(v.origin, []syntax.PathStep{{Index: i, IsIndex: true}}); origin != nil {
		out.origin = origin
		out.kind = kind
	}
	return out
}

func (v Value) stepValue(varName, traversed string, step syntax.PathStep) (Value, string, error) {
	if step.IsIndex {
		arr, ok := v.v.([]any)
		if !ok {
			return Value{}, traversed, fmt.Errorf("cannot index non-array in variable %s", traversed)
		}

		if step.Index < 0 || step.Index >= len(arr) {
			return Value{}, traversed, fmt.Errorf("index %d out of range for variable %s (length %d)", step.Index, traversed, len(arr))
		}
		return v.IndexValue(step.Index), fmt.Sprintf("%s[%d]", traversed, step.Index), nil
	}

	if v.recordFields != nil {
		field, exists := v.recordFields[step.Field]
		if !exists {
			return Value{}, traversed, fmt.Errorf("field %s not found in variable %s: %w", step.Field, traversed, ErrFieldMissing)
		}

		return field, appendFieldPath(varName, traversed, step.Field), nil
	}

	m, ok := v.v.(map[string]any)
	if !ok {
		if v.v == nil {
			return Value{}, traversed, fmt.Errorf("variable %s is null", traversed)
		}
		return Value{}, traversed, fmt.Errorf("cannot access field %s on non-object in variable %s", step.Field, traversed)
	}

	raw, exists := m[step.Field]
	if !exists {
		return Value{}, traversed, fmt.Errorf("field %s not found in variable %s: %w", step.Field, traversed, ErrFieldMissing)
	}

	out := Value{v: raw}
	if raw == nil {
		out.kind = semantics.OriginNull
	}
	if v.origin != nil {
		if origin, kind := semantics.WalkSuborigin(v.origin, []syntax.PathStep{step}); origin != nil {
			out.origin = origin
			out.kind = kind
		}
	}
	return out, appendFieldPath(varName, traversed, step.Field), nil
}

func appendFieldPath(varName, traversed, field string) string {
	if traversed == varName {
		return varName + "." + field
	}
	return traversed + "." + field
}

func (v Value) lookupPath(varName, path string) (Value, string, error) {
	if path == "" {
		return v, varName, nil
	}
	steps, err := syntax.ParsePath(path)
	if err != nil {
		return Value{}, varName, err
	}

	current := v
	traversed := varName
	for _, step := range steps {
		current, traversed, err = current.stepValue(varName, traversed, step)
		if err != nil {
			return Value{}, traversed, err
		}
	}
	return current, traversed, nil
}

// StringValue wraps a plain string as a Value with semantics.OriginScalar.
func StringValue(s string) Value {
	return Value{v: s, kind: semantics.OriginScalar}
}

// BoolValue wraps a boolean as a Value with semantics.OriginBool.
func BoolValue(b bool) Value {
	return Value{v: b, kind: semantics.OriginBool}
}

// NullValue returns a Value that represents an explicit JSON null
// -- a present value with null content, distinct from an absent
// (zero) Value. Use this where a command or filter produced a
// null result by design (for example, gojq evaluating a filter
// against a missing field) rather than as a stand-in for "no
// result".
func NullValue() Value {
	return Value{kind: semantics.OriginNull}
}

// IsNil reports whether the Value is absent -- a zero/uninitialised
// Value that signals "no result", as distinct from a present
// value whose content happens to be null. A Value constructed
// via NullValue has kind semantics.OriginNull and IsNil returns false for
// it; a Value{} returned by a failed lookup or an unproduced
// command has kind semantics.OriginUnknown and IsNil returns true.
// Callers deciding whether to error with "produced no value"
// should use IsNil; callers asking "is this content null"
// should use IsNull.
func (v Value) IsNil() bool {
	return v.v == nil && v.kind != semantics.OriginNull
}

// IsNull reports whether the Value represents an explicit JSON
// null. See NullValue.
func (v Value) IsNull() bool {
	return v.kind == semantics.OriginNull
}

// IsScalar reports whether the value stringifies as a single
// token: string, number, bool, or the null marker. Structured
// types (map, slice) and absent values are not scalars.
func (v Value) IsScalar() bool {
	if v.kind == semantics.OriginNull {
		return true
	}
	switch v.v.(type) {
	case string, json.Number, float64, bool:
		return true
	default:
		return false
	}
}

// IsStructured reports whether the value is a map or slice.
func (v Value) IsStructured() bool {
	switch v.v.(type) {
	case map[string]any, []any:
		return true
	default:
		return false
	}
}

// Raw returns the underlying dynamic value.
func (v Value) Raw() any {
	return v.v
}

// Keys returns the navigable children of the value. For maps it
// returns sorted field names; for arrays it returns index strings
// ("[0]", "[1]", ...); for scalars and nil it returns nil.
func (v Value) Keys() []string {
	switch x := v.v.(type) {
	case map[string]any:
		return slices.Sorted(maps.Keys(x))
	case []any:
		keys := make([]string, len(x))
		for i := range x {
			keys[i] = fmt.Sprintf("[%d]", i)
		}
		return keys
	default:
		return nil
	}
}

// Scalar converts a scalar value to its string representation.
// It handles string, json.Number, float64, and bool. It returns
// an error for nil, map, and slice values.
func (v Value) Scalar() (string, error) {
	if v.kind == semantics.OriginNull {
		return "null", nil
	}
	switch x := v.v.(type) {
	case string:
		return x, nil
	case json.Number:
		return x.String(), nil
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(x), nil
	case nil:
		return "", fmt.Errorf("value is null")
	default:
		return "", fmt.Errorf("value is not a scalar")
	}
}

// LookupValue walks a dotted field path (with optional [n] indexing)
// into the value and returns whatever is found, including structured
// types and nil. The varName parameter is used only for error
// messages. An empty path returns the value itself.
func (v Value) LookupValue(varName, path string) (Value, error) {
	out, _, err := v.lookupPath(varName, path)
	if err != nil {
		return Value{}, err
	}

	return out, nil
}

// LookupPresence walks a dotted field path the same way LookupValue
// does but does not error on a missing field or a null terminal.
// Instead it returns a Presence that answers the predicate-facing
// questions directly. Errors are reserved for problems the caller
// cannot meaningfully recover from (malformed path syntax,
// non-traversable intermediate such as indexing into a non-array,
// or an out-of-range list index).
func (v Value) LookupPresence(varName, path string) (Presence, error) {
	current, _, err := v.lookupPath(varName, path)
	if err != nil {
		if errors.Is(err, ErrFieldMissing) {
			return Presence{state: presenceMissing}, nil
		}
		return Presence{}, err
	}

	if current.IsNil() || current.IsNull() {
		return Presence{value: current, state: presenceNull}, nil
	}
	return Presence{value: current, state: presenceValue}, nil
}

// Lookup walks a dotted field path (with optional [n] indexing) into
// the value. The varName parameter is used only for error messages.
// An empty path returns the value itself. Unlike LookupValue, Lookup
// rejects structured and nil results, enforcing scalar access.
func (v Value) Lookup(varName, path string) (Value, error) {
	if path == "" {
		return v, nil
	}
	current, traversed, err := v.lookupPath(varName, path)
	if err != nil {
		return Value{}, err
	}

	if current.IsNil() || current.IsNull() {
		return Value{}, fmt.Errorf("variable %s is null", traversed)
	}

	switch current.Raw().(type) {
	case map[string]any:
		return Value{}, fmt.Errorf("variable %s is an object; use field access to reach a scalar value", traversed)
	case []any:
		return Value{}, fmt.Errorf("variable %s is an array; use indexing to reach a scalar value", traversed)
	}

	return current, nil
}

// RenderValue produces the byte representation of a Value suitable
// for writing to a file. Scalar strings are written verbatim (no
// trailing newline added). Numbers, booleans, and null are rendered
// as their text forms. Structured values (maps, slices) are rendered
// as deterministic pretty-printed JSON with sorted keys, two-space
// indentation, and a trailing newline after the final bracket.
func RenderValue(v Value) ([]byte, error) {
	switch x := v.v.(type) {
	case string:
		return []byte(x), nil
	case json.Number:
		return []byte(x.String()), nil
	case float64:
		return []byte(strconv.FormatFloat(x, 'f', -1, 64)), nil
	case bool:
		if x {
			return []byte("true"), nil
		}
		return []byte("false"), nil
	case nil:
		return []byte("null"), nil
	default:
		// Structured: map or slice.
		b, err := json.MarshalIndent(v.v, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("render value: %w", err)
		}

		return append(b, '\n'), nil
	}
}

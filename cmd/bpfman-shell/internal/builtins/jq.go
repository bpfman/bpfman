// jq builtin: a thin wrapper over the embedded gojq interpreter
// that lets scripts project, filter, and reshape values through
// jq expressions. The shell deliberately does not grow its own
// data-shaping operators; everything structured goes through jq.
package builtins

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/itchyny/gojq"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

func init() {
	Register(driver.Builtin{
		Name:     "jq",
		Handler:  HandleJQ,
		Category: driver.CategoryIO,
		Usage:    "jq <filter> <value>",
		Summary:  "Apply a jq filter to a value (assignable).",
	})
}

// HandleJQ runs a jq filter against a Value using an embedded gojq
// interpreter. It is the DSL's "higher-order ops over JSON-shaped
// data" primitive. Shape: jq <filter> <value>.
//
// The filter is a scalar (Word/Quoted/ScalarValue); the value may
// be scalar or structured. Multiple jq results are collected into
// a list Value; zero results yield a nil Value; a single result is
// returned directly. Integer outputs from gojq are normalised to
// json.Number so downstream Scalar() and path access treat them
// like any other numeric value in the pipeline. Bool results get
// OriginBool so AsBool works on them for assertions.
func HandleJQ(c driver.Ctx) (runtime.Value, error) {
	args := c.Args
	if len(args) != 2 {
		// Users reaching for standalone jq reflexively include
		// output-formatting flags (-c, -r, --tab). Ours is
		// filter-only -- rendering is done by the consumer
		// (compact via "${...}" interpolation, indented via
		// the shell's auto-print). Surface that explicitly so
		// the user is not left guessing why -c was rejected.
		if flag, ok := firstFlagArg(args); ok {
			return runtime.Value{}, fmt.Errorf("usage: jq <filter> <value>; our jq is filter-only (no %q flag); use \"${expr}\" for compact JSON or run the real jq via [exec jq ...]", flag)
		}
		return runtime.Value{}, fmt.Errorf("usage: jq <filter> <value>")
	}
	filterText := driver.ArgText(args[0])
	query, err := gojq.Parse(filterText)
	if err != nil {
		return runtime.Value{}, fmt.Errorf("jq: parse filter: %w", err)
	}

	input, err := argToJQInput(args[1])
	if err != nil {
		return runtime.Value{}, fmt.Errorf("jq: %w", err)
	}

	iter := query.Run(input)
	var results []any
	for {
		v, hasMore := iter.Next()
		if !hasMore {
			break
		}
		if iterErr, ok := v.(error); ok {
			return runtime.Value{}, fmt.Errorf("jq: %w", iterErr)
		}
		results = append(results, normaliseJQValue(v))
	}
	switch len(results) {
	case 0:
		return runtime.Value{}, nil
	case 1:
		return wrapJQResult(results[0]), nil
	default:
		return wrapJQResult(results), nil
	}
}

// firstFlagArg returns the text of the first "-x" or "--long"
// argument in args, or ("", false) if none appear. Used to craft
// specific error messages when a user passes shell-jq-style flags
// to the filter-only HandleJQ.
func firstFlagArg(args []runtime.Arg) (string, bool) {
	for _, a := range args {
		text := driver.ArgText(a)
		if len(text) >= 2 && text[0] == '-' && text[1] != '0' && (text[1] < '0' || text[1] > '9') && text[1] != '.' {
			return text, true
		}
	}
	return "", false
}

// argToJQInput extracts a JSON-compatible any from a runtime.Arg.
// Structured args pass through as their Raw representation;
// scalar args are parsed as JSON text, matching the default
// behaviour of the standalone jq CLI (which reads stdin as JSON).
// A scalar that isn't valid JSON is an error -- users who want
// to pass a literal string wrap it in JSON quotes ('"hello"'
// rather than 'hello').
// argToJQInput honours the boundary invariant on ScalarValueArg:
//
//	User-written input is decoded from source text.
//	Shell-resolved input is passed as its original Value.
//
// WordArg and QuotedArg are user-typed tokens, so their text is
// parsed as JSON (a bareword `42` becomes the number 42, a
// quoted '"hello"' becomes the string "hello", and a bareword
// `hello` errors -- the user is telling jq it's JSON-shaped
// input). ScalarValueArg comes from variable expansion or
// thread/interp resolution; when it carries the originating
// Value (HasValue=true) we pass Value.Raw() through directly so
// strings, numbers, booleans, and structured-as-scalar values
// preserve their type without round-tripping through JSON text.
// Synthesised scalars without a backing Value (HasValue=false)
// fall back to the user-input path.
func argToJQInput(a runtime.Arg) (any, error) {
	switch v := a.(type) {
	case runtime.WordArg:
		return decodeJQScalar(v.Text)
	case runtime.QuotedArg:
		return decodeJQScalar(v.Text)
	case runtime.ScalarValueArg:
		if v.HasValue {
			return v.Value.Raw(), nil
		}
		return decodeJQScalar(v.Text)
	case runtime.StructuredValueArg:
		return v.Value.Raw(), nil
	case runtime.AdapterArg:
		return v.Value.Raw(), nil
	case runtime.NilArg:
		// JSON null at the boundary: gojq treats Go nil as null,
		// so filters like `length` or `type` work as the user
		// would expect from the standalone jq CLI.
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported input type %T", a)
	}
}

// decodeJQScalar parses a scalar as a single JSON value. Numbers
// come back as json.Number so Value.Scalar() renders them
// losslessly; trailing data after the value is rejected so sloppy
// inputs fail fast.
func decodeJQScalar(text string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("input is not valid JSON: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("input is not valid JSON: trailing data after value")
	}
	return v, nil
}

// normaliseJQValue walks a jq output and converts Go-native
// integer types to json.Number so the result lines up with the
// rest of the pipeline, which carries numbers as json.Number
// throughout. float64 is left alone; nested maps and slices are
// rewritten recursively.
func normaliseJQValue(x any) any {
	switch v := x.(type) {
	case int:
		return json.Number(strconv.Itoa(v))
	case int64:
		return json.Number(strconv.FormatInt(v, 10))
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = normaliseJQValue(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = normaliseJQValue(e)
		}
		return out
	default:
		return v
	}
}

// wrapJQResult turns a single jq output into a runtime.Value,
// preferring the most specific origin kind so assertions and
// other origin-aware consumers see the shape they expect: bool
// -> OriginBool, scalar -> OriginScalar, structured ->
// OriginUnknown.
func wrapJQResult(x any) runtime.Value {
	if x == nil {
		// jq produced an explicit null (for example, ".name"
		// against an object without a name field). Return a
		// present null rather than a zero Value so downstream
		// substitution, assignment, interpolation, and
		// comparisons treat it as a real value.
		return runtime.NullValue()
	}
	if b, ok := x.(bool); ok {
		return runtime.BoolValue(b)
	}
	v := runtime.ValueFromAny(x)
	if v.IsScalar() {
		return v.WithKind(semantics.OriginScalar)
	}
	return v
}

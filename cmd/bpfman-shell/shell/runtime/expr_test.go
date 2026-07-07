package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// evalEnv returns an Env with the given session and no command
// runners. Suitable for expression tests that stay inside the
// pure-evaluation layer.
func evalEnv(s *Session) *Env {
	return &Env{Session: s}
}

// bindFromValue adapts a (Value, error)-returning closure to the
// ExecBind signature.
func bindFromValue(f func([]Arg, source.Span) (Value, error)) func([]Arg, source.Span) (BindResult, error) {
	return func(args []Arg, span source.Span) (BindResult, error) {
		v, err := f(args, span)
		if err != nil {
			return BindResult{}, err
		}
		return BindResult{Rc: Envelope{}, Primary: v}, nil
	}
}

func idents(names ...string) []syntax.Ident {
	out := make([]syntax.Ident, 0, len(names))
	for _, name := range names {
		out = append(out, syntax.Ident{Text: name})
	}
	return out
}

func identTexts(params []syntax.DefParam) []string {
	out := make([]string, 0, len(params))
	for _, p := range params {
		out = append(out, p.Name.Text)
	}
	return out
}

func TestEvalExpr_Literal(t *testing.T) {
	t.Parallel()

	s := NewSession()
	v, err := evalLoweredExpr(&syntax.LiteralExpr{Text: "hello"}, evalEnv(s))
	require.NoError(t, err)
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "hello", got)
}

func TestEvalExpr_Literal_Classification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expr    *syntax.LiteralExpr
		wantRaw any
	}{
		{"unquoted_int", &syntax.LiteralExpr{Text: "5"}, json.Number("5")},
		{"unquoted_float", &syntax.LiteralExpr{Text: "5.5"}, json.Number("5.5")},
		{"unquoted_negative", &syntax.LiteralExpr{Text: "-3"}, json.Number("-3")},
		{"unquoted_true", &syntax.LiteralExpr{Text: "true"}, true},
		{"unquoted_false", &syntax.LiteralExpr{Text: "false"}, false},
		{"unquoted_word", &syntax.LiteralExpr{Text: "fentry"}, "fentry"},
		{"unquoted_path", &syntax.LiteralExpr{Text: "/tmp/x"}, "/tmp/x"},
		{"quoted_numeric_text_stays_string", &syntax.LiteralExpr{Text: "5", Quoted: true}, "5"},
		{"quoted_true_stays_string", &syntax.LiteralExpr{Text: "true", Quoted: true}, "true"},
		{"quoted_word", &syntax.LiteralExpr{Text: "hello", Quoted: true}, "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewSession()
			v, err := evalLoweredExpr(tt.expr, evalEnv(s))
			require.NoError(t, err)
			assert.Equal(t, tt.wantRaw, v.Raw())
		})
	}
}

func TestEvalExpr_Literal_InvalidNumericLookingFormsRejected(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"007", "0xff", "1e309"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			_, err := evalLoweredExpr(&syntax.LiteralExpr{Text: text}, evalEnv(NewSession()))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "numeric literal")
		})
	}
}

func TestEvalExpr_VarRef_Bare(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("x", StringValue("bound"))
	v, err := evalLoweredExpr(&syntax.VarRefExpr{Name: "x"}, evalEnv(s))
	require.NoError(t, err)
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "bound", got)
}

func TestRegisterLoweredDefs_PreservesAbsoluteDefSpan(t *testing.T) {
	t.Parallel()

	sess := NewSession()
	env := &Env{Session: sess}
	prog, err := parseSource(t, "def loaded() { return 7 }\n")
	require.NoError(t, err)
	lp, err := lowerToIR(prog)
	require.NoError(t, err)
	require.Len(t, lp.Defs, 1)

	def := lp.Defs[0]
	registerLoweredDefs(lp.Defs, env)
	got, ok := sess.getDef("loaded")
	require.True(t, ok)
	assert.Equal(t, def.Span, got.Span)
	assert.True(t, got.HasReturn)
}

func TestEvalExpr_VarRef_Path(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("prog", ValueFromMap(map[string]any{
		"record": map[string]any{"program_id": "42"},
	}))
	v, err := evalLoweredExpr(&syntax.VarRefExpr{Name: "prog", Path: "record.program_id"}, evalEnv(s))
	require.NoError(t, err)
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "42", got)
}

func TestEvalExpr_RecordLiteral_FieldAccessPreservesOrigin(t *testing.T) {
	t.Parallel()

	origin := struct{ ID int }{ID: 42}
	prog := ValueFromMap(map[string]any{
		"record": map[string]any{"program_id": "42"},
	}).withOrigin(origin, semantics.OriginProgram)

	s := NewSession()
	s.Set("p", prog)
	v, err := evalLoweredExpr(&syntax.RecordExpr{
		Fields: []syntax.RecordField{
			{Name: "prog", Expr: &syntax.VarRefExpr{Name: "p"}},
			{Name: "name", Expr: &syntax.LiteralExpr{Text: "loaded", Quoted: true}},
		},
	}, evalEnv(s))
	require.NoError(t, err)

	field, err := v.LookupValue("r", "prog")
	require.NoError(t, err)
	assert.Equal(t, semantics.OriginProgram, field.Kind())
	assert.Equal(t, origin, field.Origin())

	name, err := v.LookupValue("r", "name")
	require.NoError(t, err)
	got, err := name.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "loaded", got)
}

func TestEvalArgs_RecordFieldStructuredValuePreservesOrigin(t *testing.T) {
	t.Parallel()

	origin := struct{ ID int }{ID: 42}
	prog := ValueFromMap(map[string]any{
		"record": map[string]any{"program_id": "42"},
	}).withOrigin(origin, semantics.OriginProgram)

	s := NewSession()
	s.Set("p", prog)
	record, err := evalLoweredExpr(&syntax.RecordExpr{
		Fields: []syntax.RecordField{
			{Name: "prog", Expr: &syntax.VarRefExpr{Name: "p"}},
		},
	}, evalEnv(s))
	require.NoError(t, err)
	s.Set("r", record)

	args, err := evalLoweredArgs([]syntax.Expr{
		&syntax.VarRefExpr{Name: "r", Path: "prog"},
	}, evalEnv(s))
	require.NoError(t, err)
	require.Len(t, args, 1)
	structured, ok := args[0].(StructuredValueArg)
	require.True(t, ok, "arg should be StructuredValueArg, got %T", args[0])
	assert.Equal(t, semantics.OriginProgram, structured.Value.Kind())
	assert.Equal(t, origin, structured.Value.Origin())
}

func TestEvalExpr_VarRef_Undefined(t *testing.T) {
	t.Parallel()

	s := NewSession()
	_, err := evalLoweredExpr(&syntax.VarRefExpr{Name: "missing"}, evalEnv(s))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `undefined variable "missing"`)
}

func TestEvalExpr_VarRef_DynamicIndex(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("xs", ValueFromAny([]any{json.Number("100"), json.Number("200"), json.Number("300")}))
	s.Set("i", ValueFromAny(json.Number("1")))
	v, err := evalLoweredExpr(&syntax.VarRefExpr{Name: "xs", Path: "[$i]"}, evalEnv(s))
	require.NoError(t, err)
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "200", got)
}

func TestEvalExpr_VarRef_DynamicIndex_StringInteger(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("xs", ValueFromAny([]any{"a", "b", "c"}))
	s.Set("i", StringValue("2"))
	v, err := evalLoweredExpr(&syntax.VarRefExpr{Name: "xs", Path: "[$i]"}, evalEnv(s))
	require.NoError(t, err)
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "c", got)
}

func TestEvalExpr_VarRef_DynamicIndex_NestedPath(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("xs", ValueFromAny([]any{
		map[string]any{"name": "alpha"},
		map[string]any{"name": "beta"},
	}))
	s.Set("i", ValueFromAny(json.Number("0")))
	v, err := evalLoweredExpr(&syntax.VarRefExpr{Name: "xs", Path: "[$i].name"}, evalEnv(s))
	require.NoError(t, err)
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "alpha", got)
}

func TestEvalExpr_VarRef_DynamicIndex_UndefinedIndex(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("xs", ValueFromAny([]any{"a", "b"}))
	_, err := evalLoweredExpr(&syntax.VarRefExpr{Name: "xs", Path: "[$i]"}, evalEnv(s))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "index variable $i is not defined")
}

func TestEvalExpr_VarRef_DynamicIndex_NonInteger(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("xs", ValueFromAny([]any{"a", "b"}))
	s.Set("i", StringValue("not-a-number"))
	_, err := evalLoweredExpr(&syntax.VarRefExpr{Name: "xs", Path: "[$i]"}, evalEnv(s))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "index variable $i:")
	assert.Contains(t, err.Error(), "must be an integer")
}

func TestEvalExpr_VarRef_DynamicIndex_OutOfRange(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("xs", ValueFromAny([]any{"a", "b"}))
	s.Set("i", ValueFromAny(json.Number("5")))
	_, err := evalLoweredExpr(&syntax.VarRefExpr{Name: "xs", Path: "[$i]"}, evalEnv(s))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestEvalExpr_VarRef_DynamicIndex_Negative(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("xs", ValueFromAny([]any{"a", "b"}))
	s.Set("i", ValueFromAny(json.Number("-1")))
	_, err := evalLoweredExpr(&syntax.VarRefExpr{Name: "xs", Path: "[$i]"}, evalEnv(s))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

// TestEvalExpr_VarRef_DynamicIndex_BracedForm confirms the
// "${xs[$i]}" form resolves identically to the bare "$xs[$i]"
// form. Both tokeniser shapes store the path text the same way,
// so a single eval-side check is sufficient.
func TestEvalExpr_VarRef_DynamicIndex_BracedForm(t *testing.T) {
	t.Parallel()

	const src = "${xs[$i]}"
	tokens, err := syntax.Tokenise(src)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.Equal(t, syntax.TokenVarRef, tokens[0].Kind)
	require.Equal(t, "xs", tokens[0].VarName)
	require.Equal(t, "[$i]", tokens[0].VarPath)

	s := NewSession()
	s.Set("xs", ValueFromAny([]any{"a", "b", "c"}))
	s.Set("i", ValueFromAny(json.Number("2")))
	v, err := evalLoweredExpr(&syntax.VarRefExpr{Name: tokens[0].VarName, Path: tokens[0].VarPath}, evalEnv(s))
	require.NoError(t, err)
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "c", got)
}

// TestEvalExpr_AdapterArg_DynamicIndex covers the adapter
// reference path through resolveAdapterArg. The tokeniser
// recognises file:$x with the same path grammar as $x, so the
// dynamic-index resolution must travel through the adapter
// arg builder identically.
func TestEvalExpr_AdapterArg_DynamicIndex(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("paths", ValueFromAny([]any{"/a", "/b", "/c"}))
	s.Set("i", ValueFromAny(json.Number("1")))

	arg, err := resolveAdapterArgParts("file", "paths", "[$i]", source.Span{}, evalEnv(s))
	require.NoError(t, err)
	aa, ok := arg.(AdapterArg)
	require.True(t, ok)
	got, err := aa.Value.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "/b", got)
}

// TestExecSource_DynamicIndex_ParallelLists exercises the full
// tokenise -> parse -> eval pipeline for the parallel-list iteration
// shape that motivated $xs[$i]: two slot-aligned lists indexed by a
// foreach counter, with the chosen elements appearing as command
// args.
func TestExecSource_DynamicIndex_ParallelLists(t *testing.T) {
	t.Parallel()

	const src = `
let xs = [10 20 30]
let ys = ["a" "b" "c"]
foreach i in [0 1 2] {
    record $xs[$i] $ys[$i]
}
`
	tokens, err := syntax.Tokenise(src)
	require.NoError(t, err)
	prog, err := syntax.Parse(tokens)
	require.NoError(t, err)

	var captured [][]string
	env := &Env{
		Session: NewSession(),
		ExecCommand: func(args []Arg, _ source.Span) (Value, error) {
			row := make([]string, 0, len(args))
			for _, a := range args {
				switch x := a.(type) {
				case WordArg:
					row = append(row, x.Text)
				case ScalarValueArg:
					row = append(row, x.Text)
				default:
					return Value{}, fmt.Errorf("unexpected arg %T", a)
				}
			}
			captured = append(captured, row)
			return Value{}, nil
		},
	}
	require.NoError(t, execParsedProgram(t, prog, env))
	assert.Equal(t, [][]string{
		{"record", "10", "a"},
		{"record", "20", "b"},
		{"record", "30", "c"},
	}, captured)
}

func TestEvalExpr_Adapter_RejectedAsExpression(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("x", StringValue("hi"))
	_, err := evalLoweredExpr(&syntax.AdapterExpr{Adapter: "file", Name: "x"}, evalEnv(s))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "adapter")
}

func TestEvalExpr_Binary_Textual(t *testing.T) {
	t.Parallel()

	s := NewSession()
	cases := []struct {
		op         string
		left       string
		right      string
		wantResult bool
	}{
		{"==", "foo", "foo", true},
		{"==", "foo", "bar", false},
		{"!=", "foo", "bar", true},
		{"!=", "foo", "foo", false},
		{"<", "a", "b", true},
		{"<", "b", "a", false},
		{"<=", "a", "a", true},
		{">", "b", "a", true},
		{">=", "a", "a", true},
	}
	for _, tc := range cases {
		t.Run(tc.op+" "+tc.left+" "+tc.right, func(t *testing.T) {
			t.Parallel()
			e := &syntax.BinaryExpr{
				Left:  &syntax.LiteralExpr{Text: tc.left},
				Op:    tc.op,
				Right: &syntax.LiteralExpr{Text: tc.right},
			}
			v, err := evalLoweredExpr(e, evalEnv(s))
			require.NoError(t, err)
			assert.Equal(t, semantics.OriginBool, v.Kind())
			b, err := AsBool(v)
			require.NoError(t, err)
			assert.Equal(t, tc.wantResult, b)
		})
	}
}

func TestEvalExpr_Binary_Numeric(t *testing.T) {
	t.Parallel()

	s := NewSession()
	cases := []struct {
		op         string
		left       string
		right      string
		wantResult bool
	}{
		{"==", "5", "5", true},
		{"==", "5", "6", false},
		{"!=", "5", "6", true},
		{"<", "3", "4", true},
		{"<=", "3", "3", true},
		{">", "5", "4", true},
		{">=", "5", "5", true},
		{"<", "9", "10", true},
		{">", "10", "9", true},
	}
	for _, tc := range cases {
		t.Run(tc.op+" "+tc.left+" "+tc.right, func(t *testing.T) {
			t.Parallel()
			e := &syntax.BinaryExpr{
				Left:  &syntax.LiteralExpr{Text: tc.left},
				Op:    tc.op,
				Right: &syntax.LiteralExpr{Text: tc.right},
			}
			v, err := evalLoweredExpr(e, evalEnv(s))
			require.NoError(t, err)
			b, err := AsBool(v)
			require.NoError(t, err)
			assert.Equal(t, tc.wantResult, b)
		})
	}
}

func TestEvalExpr_Binary_NumericNonNumericError(t *testing.T) {
	t.Parallel()

	s := NewSession()
	e := &syntax.BinaryExpr{
		Left:  &syntax.LiteralExpr{Text: "abc"},
		Op:    "<",
		Right: &syntax.LiteralExpr{Text: "5"},
	}
	_, err := evalLoweredExpr(e, evalEnv(s))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot compare string to number")
}

func TestEvalExpr_Binary_StrictDispatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		left      syntax.Expr
		op        string
		right     syntax.Expr
		wantBool  bool
		wantError string
	}{
		{"number_eq_number_true", &syntax.LiteralExpr{Text: "10"}, "==", &syntax.LiteralExpr{Text: "10.0"}, true, ""},
		{"number_eq_number_false", &syntax.LiteralExpr{Text: "10"}, "==", &syntax.LiteralExpr{Text: "11"}, false, ""},
		{"number_lt_number", &syntax.LiteralExpr{Text: "9"}, "<", &syntax.LiteralExpr{Text: "10"}, true, ""},
		{"string_eq_string_true", &syntax.LiteralExpr{Text: "fentry"}, "==", &syntax.LiteralExpr{Text: "fentry"}, true, ""},
		{"string_eq_string_false", &syntax.LiteralExpr{Text: "fentry"}, "==", &syntax.LiteralExpr{Text: "fexit"}, false, ""},
		{"string_lt_string_lex", &syntax.LiteralExpr{Text: "9"}, "<", &syntax.LiteralExpr{Text: "10", Quoted: true}, false, "cannot compare number to string"},
		{"bool_eq_bool_true", &syntax.LiteralExpr{Text: "true"}, "==", &syntax.LiteralExpr{Text: "true"}, true, ""},
		{"bool_ne_bool", &syntax.LiteralExpr{Text: "true"}, "!=", &syntax.LiteralExpr{Text: "false"}, true, ""},
		{"bool_ordering_rejected", &syntax.LiteralExpr{Text: "true"}, "<", &syntax.LiteralExpr{Text: "false"}, false, "booleans support only == and !="},
		{"cross_type_string_number", &syntax.LiteralExpr{Text: "fentry"}, "==", &syntax.LiteralExpr{Text: "5"}, false, "cannot compare string to number"},
		{"cross_type_bool_number", &syntax.LiteralExpr{Text: "true"}, "==", &syntax.LiteralExpr{Text: "1"}, false, "cannot compare bool to number"},
		{"cross_type_bool_string", &syntax.LiteralExpr{Text: "true"}, "==", &syntax.LiteralExpr{Text: "true", Quoted: true}, false, "cannot compare bool to string"},
		{"quoted_numeric_is_string", &syntax.LiteralExpr{Text: "5", Quoted: true}, "==", &syntax.LiteralExpr{Text: "5", Quoted: true}, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewSession()
			e := &syntax.BinaryExpr{Left: tt.left, Op: tt.op, Right: tt.right}
			v, err := evalLoweredExpr(e, evalEnv(s))
			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
				return
			}
			require.NoError(t, err)
			b, err := AsBool(v)
			require.NoError(t, err)
			assert.Equal(t, tt.wantBool, b)
		})
	}
}

func TestEvalExpr_Unary_NotEmpty(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("x", StringValue("hello"))
	s.Set("y", StringValue(""))

	e := &syntax.UnaryExpr{Pred: "not-empty", Operand: &syntax.VarRefExpr{Name: "x"}}
	v, err := evalLoweredExpr(e, evalEnv(s))
	require.NoError(t, err)
	b, err := AsBool(v)
	require.NoError(t, err)
	assert.True(t, b)

	e = &syntax.UnaryExpr{Pred: "not-empty", Operand: &syntax.VarRefExpr{Name: "y"}}
	v, err = evalLoweredExpr(e, evalEnv(s))
	require.NoError(t, err)
	b, err = AsBool(v)
	require.NoError(t, err)
	assert.False(t, b)
}

func TestAsBool_RejectsNonBool(t *testing.T) {
	t.Parallel()

	cases := []Value{
		StringValue("true"),
		StringValue(""),
		ValueFromMap(map[string]any{"x": 1}),
	}
	for _, v := range cases {
		_, err := AsBool(v)
		require.Error(t, err, "kind=%s", v.Kind())
		assert.Contains(t, err.Error(), "use a comparison")
	}
}

// ThreadExpr evaluation: LHS's Value is appended as the last arg to
// the pipe's command, which then dispatches via ExecBind.

func TestEvalExpr_Thread_AppendsScalarValueAsLastArg(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("x", StringValue("42"))
	var captured []Arg
	env := &Env{
		Session: s,
		ExecBind: bindFromValue(func(args []Arg, _ source.Span) (Value, error) {
			captured = args
			return StringValue("ok"), nil
		}),
	}
	pipe := &syntax.ThreadExpr{
		LHS:  &syntax.VarRefExpr{Name: "x"},
		Args: []syntax.Expr{&syntax.LiteralExpr{Text: "jq"}, &syntax.LiteralExpr{Text: ".", Quoted: true}},
	}
	v, err := evalLoweredExpr(pipe, env)
	require.NoError(t, err)
	s2, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "ok", s2)
	// Captured args: [jq, ".", "42"] -- LHS value becomes last arg.
	require.Len(t, captured, 3)
	assert.Equal(t, WordArg{Text: "jq"}, captured[0])
	assert.Equal(t, QuotedArg{Text: "."}, captured[1])
	scalar, ok := captured[2].(ScalarValueArg)
	require.True(t, ok)
	assert.Equal(t, "42", scalar.Text)
}

func TestEvalExpr_Thread_AppendsStructuredValueAsLastArg(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("p", ValueFromMap(map[string]any{"id": "42"}).WithKind(semantics.OriginProgram))
	var captured []Arg
	env := &Env{
		Session: s,
		ExecBind: bindFromValue(func(args []Arg, _ source.Span) (Value, error) {
			captured = args
			return StringValue("ok"), nil
		}),
	}
	pipe := &syntax.ThreadExpr{
		LHS:  &syntax.VarRefExpr{Name: "p"},
		Args: []syntax.Expr{&syntax.LiteralExpr{Text: "jq"}, &syntax.LiteralExpr{Text: ".id", Quoted: true}},
	}
	_, err := evalLoweredExpr(pipe, env)
	require.NoError(t, err)
	require.Len(t, captured, 3)
	sva, ok := captured[2].(StructuredValueArg)
	require.True(t, ok, "structured LHS should produce StructuredValueArg, got %T", captured[2])
	assert.Equal(t, semantics.OriginProgram, sva.Value.Kind())
}

func TestEvalExpr_Thread_DestructuredSlotPreservesValueAsLastArg(t *testing.T) {
	t.Parallel()

	src := `
let (left right) = ["left" "right"]
let got = $left |> jq "."
`
	var captured []Arg
	env := &Env{
		Session: NewSession(),
		ExecBind: bindFromValue(func(args []Arg, _ source.Span) (Value, error) {
			captured = args
			return StringValue("ok"), nil
		}),
	}
	require.NoError(t, execSourceProgram(t, src, env))
	require.Len(t, captured, 3)
	scalar, ok := captured[2].(ScalarValueArg)
	require.True(t, ok, "destructured scalar should produce ScalarValueArg, got %T", captured[2])
	assert.Equal(t, "left", scalar.Text)
	assert.True(t, scalar.HasValue)
	assert.Equal(t, "left", scalar.Value.Raw())
}

func TestEvalExpr_Thread_NilLHSPassesAsNilArg(t *testing.T) {
	t.Parallel()

	// Threading a null value into a command passes NilArg to the
	// command, which decides for itself how to interpret null at
	// its input boundary (e.g. jq treats it as JSON null). This
	// is the natural
	// shape-test pattern `$got.status.links |> jq "length"`
	// where the source field is the JSON value null.
	s := NewSession()
	s.Set("x", Value{}) // nil value
	var received []Arg
	env := &Env{
		Session: s,
		ExecBind: bindFromValue(func(args []Arg, _ source.Span) (Value, error) {
			received = args
			return StringValue("ran"), nil
		}),
	}
	pipe := &syntax.ThreadExpr{
		LHS:  &syntax.VarRefExpr{Name: "x"},
		Args: []syntax.Expr{&syntax.LiteralExpr{Text: "jq"}},
	}
	got, err := evalLoweredExpr(pipe, env)
	require.NoError(t, err)
	s2, _ := got.Scalar()
	assert.Equal(t, "ran", s2)
	require.Len(t, received, 2)
	_, isNil := received[1].(NilArg)
	assert.True(t, isNil, "thread LHS null surfaces as NilArg")
}

func TestEvalExpr_Thread_NoSubstitutionRunnerIsError(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("x", StringValue("42"))
	env := &Env{Session: s}
	e := &syntax.ThreadExpr{
		LHS:  &syntax.VarRefExpr{Name: "x"},
		Args: []syntax.Expr{&syntax.LiteralExpr{Text: "jq"}},
	}
	_, err := evalLoweredExpr(e, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'|>'")
}

func TestEvalExpr_Thread_Chain_FeedsSuccessively(t *testing.T) {
	t.Parallel()

	// Build ((x | stage1) | stage2) manually; each stage's runner
	// returns a known Value, and the outer stage asserts that its
	// last arg is the inner stage's return.
	s := NewSession()
	s.Set("x", StringValue("start"))
	env := &Env{
		Session: s,
		ExecBind: bindFromValue(func(args []Arg, _ source.Span) (Value, error) {
			// Return the last arg's text with a prefix so a chain
			// accumulates visible stages.
			last := args[len(args)-1].(ScalarValueArg).Text
			return StringValue("<" + last + ">"), nil
		}),
	}
	inner := &syntax.ThreadExpr{
		LHS:  &syntax.VarRefExpr{Name: "x"},
		Args: []syntax.Expr{&syntax.LiteralExpr{Text: "stage1"}},
	}
	outer := &syntax.ThreadExpr{
		LHS:  inner,
		Args: []syntax.Expr{&syntax.LiteralExpr{Text: "stage2"}},
	}
	v, err := evalLoweredExpr(outer, env)
	require.NoError(t, err)
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "<<start>>", got)
}

func TestEvalArgs_Thread_WrapsThreadResultAsArg(t *testing.T) {
	t.Parallel()

	// A ThreadExpr used as a command argument: the evaluator should
	// dispatch the pipe, then wrap the returned Value as a
	// ScalarValueArg or StructuredValueArg.
	s := NewSession()
	s.Set("x", StringValue("42"))
	env := &Env{
		Session: s,
		ExecBind: bindFromValue(func(args []Arg, _ source.Span) (Value, error) {
			return StringValue("piped"), nil
		}),
	}
	pipe := &syntax.ThreadExpr{
		LHS:  &syntax.VarRefExpr{Name: "x"},
		Args: []syntax.Expr{&syntax.LiteralExpr{Text: "stage"}},
	}
	exprs := []syntax.Expr{&syntax.LiteralExpr{Text: "outer"}, pipe}
	out, err := evalLoweredArgs(exprs, env)
	require.NoError(t, err)
	require.Len(t, out, 2)
	scalar, ok := out[1].(ScalarValueArg)
	require.True(t, ok)
	assert.Equal(t, "piped", scalar.Text)
}

// --- foreach ------------------------------------------------------

func TestExecSource_ForEach_IteratesList(t *testing.T) {
	t.Parallel()

	s := NewSession()
	listValue, err := ValueFromJSON([]byte(`["a","b","c"]`))
	require.NoError(t, err)
	s.Set("xs", listValue)

	var captured []string
	env := &Env{
		Session: s,
		ExecCommand: func(args []Arg, _ source.Span) (Value, error) {
			require.Len(t, args, 1)
			scalar, ok := args[0].(ScalarValueArg)
			require.True(t, ok)
			captured = append(captured, scalar.Text)
			return Value{}, nil
		},
	}

	// foreach p in $xs { $p }  -- the body is a single command
	// statement whose only arg is the loop variable, so the
	// runner captures each element's text.
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.ForEachStmt{
			Names: idents("p"),
			List:  &syntax.VarRefExpr{Name: "xs"},
			Body: []syntax.Stmt{
				&syntax.CommandStmt{Args: []syntax.Expr{&syntax.VarRefExpr{Name: "p"}}},
			},
		},
	}}
	require.NoError(t, execParsedProgram(t, prog, env))
	assert.Equal(t, []string{"a", "b", "c"}, captured)
}

func TestExecSource_ForEach_LoopVarBodyScoped(t *testing.T) {
	t.Parallel()

	s := NewSession()
	listValue, err := ValueFromJSON([]byte(`[1,2,3]`))
	require.NoError(t, err)
	s.Set("xs", listValue)
	env := &Env{
		Session:     s,
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
	}
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.ForEachStmt{
			Names: idents("i"),
			List:  &syntax.VarRefExpr{Name: "xs"},
			Body:  []syntax.Stmt{&syntax.CommandStmt{Args: []syntax.Expr{&syntax.VarRefExpr{Name: "i"}}}},
		},
	}}
	require.NoError(t, execParsedProgram(t, prog, env))
	// The loop variable is body-scoped; it must not leak.
	_, ok := s.Get("i")
	assert.False(t, ok, "loop variable $i should not be defined after the loop")
}

func TestExecSource_ForEach_LoopVarRestoresPriorBinding(t *testing.T) {
	t.Parallel()

	s := NewSession()
	listValue, err := ValueFromJSON([]byte(`[1,2,3]`))
	require.NoError(t, err)
	s.Set("xs", listValue)
	s.Set("i", StringValue("outer"))
	env := &Env{
		Session:     s,
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
	}
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.ForEachStmt{
			Names: idents("i"),
			List:  &syntax.VarRefExpr{Name: "xs"},
			Body:  []syntax.Stmt{&syntax.CommandStmt{Args: []syntax.Expr{&syntax.VarRefExpr{Name: "i"}}}},
		},
	}}
	require.NoError(t, execParsedProgram(t, prog, env))
	// A prior binding of $i in the enclosing scope is restored.
	v, ok := s.Get("i")
	require.True(t, ok)
	str, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "outer", str)
}

// TestExecSource_ForEach_BodyLet_* and TestExecSource_If_*
// pin the block-scope contract: bodies run in fresh frames,
// body-level `let` does not leak past the block, and iteration
// frames are independent of one another.

func TestExecSource_ForEach_BodyLet_DoesNotLeakAfterLoop(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("xs", ValueFromAny([]any{"a", "b", "c"}))
	env := &Env{Session: s, ExecBind: nil}
	src := "foreach x in $xs {\n  let scratch = $x\n}\n"
	require.NoError(t, runProgramWithEnv(t, src, env))

	// The body-level let lived in the iteration frame and is
	// gone now that the loop has finished.
	_, ok := s.Get("scratch")
	assert.False(t, ok, "body-level let must not leak out of foreach")
}

func TestExecSource_ForEach_BodyLet_DoesNotBleedAcrossIterations(t *testing.T) {
	t.Parallel()

	// On the first iteration, scratch is unbound, so reading
	// $scratch would error if the body-level let from the
	// previous iteration leaked into the next. We exercise that
	// by ordering the read before the assignment in each body.
	// If iteration frames bled into one another, iteration 1
	// would see iteration 0's scratch; with per-iteration
	// frames, iteration 1 starts clean and the script halts at
	// the very first read.
	r := &recorder{}
	s := NewSession()
	s.Set("xs", ValueFromAny([]any{"a", "b"}))
	env := &Env{Session: s, ExecBind: r.execBind}
	src := "foreach x in $xs {\n" +
		"  print $scratch\n" +
		"  let scratch = leaked\n" +
		"}\n"
	err := runProgramWithEnv(t, src, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "undefined variable: scratch")
}

func TestExecSource_If_BranchLet_StaysLocal(t *testing.T) {
	t.Parallel()

	s := NewSession()
	env := &Env{Session: s, ExecBind: nil}
	src := "if true {\n  let scratch = hello\n}\n"
	require.NoError(t, runProgramWithEnv(t, src, env))

	_, ok := s.Get("scratch")
	assert.False(t, ok, "if-branch let must not leak past the branch")
}

func TestExecSource_If_BranchLet_RebindsOuterShadowing(t *testing.T) {
	t.Parallel()

	// `let x = inner` inside the branch writes to the branch
	// frame and shadows the outer x for the branch's lifetime.
	// When the branch exits, the outer x is intact.
	s := NewSession()
	env := &Env{Session: s, ExecBind: nil}
	src := "let x = outer\n" +
		"if true {\n  let x = inner\n}\n"
	require.NoError(t, runProgramWithEnv(t, src, env))

	v, ok := s.Get("x")
	require.True(t, ok)
	str, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "outer", str)
}

func TestExecSource_If_SiblingBranches_DoNotShareLocals(t *testing.T) {
	t.Parallel()

	// The Then branch is the one that runs (cond true); the
	// Else branch's let is never evaluated. Neither branch's
	// binding survives the if statement -- the post-if read
	// must fail with undefined-variable.
	s := NewSession()
	env := &Env{Session: s, ExecBind: nil}
	src := "if true {\n  let from_then = 1\n} else {\n  let from_else = 2\n}\n" +
		"print $from_then\n"
	err := runProgramWithEnv(t, src, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "undefined variable: from_then")
}

func TestExecSource_If_ElifBranch_HasIndependentFrame(t *testing.T) {
	t.Parallel()

	// elif body runs when cond is false; its let does not
	// survive the construct.
	s := NewSession()
	env := &Env{Session: s, ExecBind: nil}
	src := "let cond = false\n" +
		"if $cond {\n  let from_then = t\n} elif true {\n  let from_elif = e\n}\n"
	require.NoError(t, runProgramWithEnv(t, src, env))

	_, ok := s.Get("from_elif")
	assert.False(t, ok, "elif-branch let must not leak past the branch")
	_, ok = s.Get("from_then")
	assert.False(t, ok, "unreached then-branch contributes nothing")
}

func TestExecSource_ForEach_EmptyList(t *testing.T) {
	t.Parallel()

	s := NewSession()
	listValue, err := ValueFromJSON([]byte(`[]`))
	require.NoError(t, err)
	s.Set("xs", listValue)
	callCount := 0
	env := &Env{
		Session: s,
		ExecCommand: func([]Arg, source.Span) (Value, error) {
			callCount++
			return Value{}, nil
		},
	}
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.ForEachStmt{
			Names: idents("x"),
			List:  &syntax.VarRefExpr{Name: "xs"},
			Body:  []syntax.Stmt{&syntax.CommandStmt{Args: []syntax.Expr{&syntax.LiteralExpr{Text: "body"}}}},
		},
	}}
	require.NoError(t, execParsedProgram(t, prog, env))
	assert.Equal(t, 0, callCount, "body must not run for an empty list")
	_, ok := s.Get("x")
	assert.False(t, ok, "loop variable should not be set when the list is empty")
}

func TestExecSource_ForEach_NonListIsError(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("notalist", StringValue("hello"))
	env := &Env{
		Session:     s,
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
	}
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.ForEachStmt{
			Names: idents("x"),
			List:  &syntax.VarRefExpr{Name: "notalist"},
			Body:  []syntax.Stmt{},
		},
	}}
	err := execParsedProgram(t, prog, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "foreach")
}

func TestExecSource_ForEach_BodyErrorHaltsLoop(t *testing.T) {
	t.Parallel()

	s := NewSession()
	listValue, err := ValueFromJSON([]byte(`["a","b","c"]`))
	require.NoError(t, err)
	s.Set("xs", listValue)
	seen := 0
	boom := errors.New("boom")
	env := &Env{
		Session: s,
		ExecCommand: func(args []Arg, _ source.Span) (Value, error) {
			seen++
			if seen == 2 {
				return Value{}, boom
			}
			return Value{}, nil
		},
	}
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.ForEachStmt{
			Names: idents("x"),
			List:  &syntax.VarRefExpr{Name: "xs"},
			Body:  []syntax.Stmt{&syntax.CommandStmt{Args: []syntax.Expr{&syntax.VarRefExpr{Name: "x"}}}},
		},
	}}
	evErr := execParsedProgram(t, prog, env)
	require.Error(t, evErr)
	require.ErrorIs(t, evErr, boom, "body error must remain reachable via errors.Is after the statement-level frame wrap")
	assert.Equal(t, 2, seen, "loop must stop at the first failing iteration")
}

func TestExecSource_ForEach_BreakStopsIteration(t *testing.T) {
	t.Parallel()

	s := NewSession()
	listValue, err := ValueFromJSON([]byte(`["a","b","c","d"]`))
	require.NoError(t, err)
	s.Set("xs", listValue)
	var captured []string
	env := &Env{
		Session: s,
		ExecCommand: func(args []Arg, _ source.Span) (Value, error) {
			scalar := args[0].(ScalarValueArg)
			captured = append(captured, scalar.Text)
			return Value{}, nil
		},
	}
	// foreach x in $xs {
	//   if $x == c { break }
	//   $x
	// }
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.ForEachStmt{
			Names: idents("x"),
			List:  &syntax.VarRefExpr{Name: "xs"},
			Body: []syntax.Stmt{
				&syntax.IfStmt{
					Cond: &syntax.BinaryExpr{
						Left:  &syntax.VarRefExpr{Name: "x"},
						Op:    "==",
						Right: &syntax.LiteralExpr{Text: "c"},
					},
					Then: []syntax.Stmt{&syntax.BreakStmt{}},
				},
				&syntax.CommandStmt{Args: []syntax.Expr{&syntax.VarRefExpr{Name: "x"}}},
			},
		},
	}}
	require.NoError(t, execParsedProgram(t, prog, env))
	assert.Equal(t, []string{"a", "b"}, captured)
}

func TestExecSource_ForEach_ContinueSkipsIteration(t *testing.T) {
	t.Parallel()

	s := NewSession()
	listValue, err := ValueFromJSON([]byte(`["a","b","c","d"]`))
	require.NoError(t, err)
	s.Set("xs", listValue)
	var captured []string
	env := &Env{
		Session: s,
		ExecCommand: func(args []Arg, _ source.Span) (Value, error) {
			scalar := args[0].(ScalarValueArg)
			captured = append(captured, scalar.Text)
			return Value{}, nil
		},
	}
	// foreach x in $xs {
	//   if $x == b { continue }
	//   $x
	// }
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.ForEachStmt{
			Names: idents("x"),
			List:  &syntax.VarRefExpr{Name: "xs"},
			Body: []syntax.Stmt{
				&syntax.IfStmt{
					Cond: &syntax.BinaryExpr{
						Left:  &syntax.VarRefExpr{Name: "x"},
						Op:    "==",
						Right: &syntax.LiteralExpr{Text: "b"},
					},
					Then: []syntax.Stmt{&syntax.ContinueStmt{}},
				},
				&syntax.CommandStmt{Args: []syntax.Expr{&syntax.VarRefExpr{Name: "x"}}},
			},
		},
	}}
	require.NoError(t, execParsedProgram(t, prog, env))
	assert.Equal(t, []string{"a", "c", "d"}, captured)
}

func TestExecSource_ForEach_BreakInnerOnly(t *testing.T) {
	t.Parallel()

	// Nested foreach: break in the inner loop must not escape
	// the outer loop.
	s := NewSession()
	outer, err := ValueFromJSON([]byte(`[1,2,3]`))
	require.NoError(t, err)
	inner, err := ValueFromJSON([]byte(`["x","y","z"]`))
	require.NoError(t, err)
	s.Set("outer", outer)
	s.Set("inner", inner)
	var captured []string
	env := &Env{
		Session: s,
		ExecCommand: func(args []Arg, _ source.Span) (Value, error) {
			scalar := args[0].(ScalarValueArg)
			captured = append(captured, scalar.Text)
			return Value{}, nil
		},
	}
	// foreach a in $outer {
	//   foreach b in $inner {
	//     if $b == y { break }
	//     $b
	//   }
	//   $a
	// }
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.ForEachStmt{
			Names: idents("a"),
			List:  &syntax.VarRefExpr{Name: "outer"},
			Body: []syntax.Stmt{
				&syntax.ForEachStmt{
					Names: idents("b"),
					List:  &syntax.VarRefExpr{Name: "inner"},
					Body: []syntax.Stmt{
						&syntax.IfStmt{
							Cond: &syntax.BinaryExpr{
								Left:  &syntax.VarRefExpr{Name: "b"},
								Op:    "==",
								Right: &syntax.LiteralExpr{Text: "y"},
							},
							Then: []syntax.Stmt{&syntax.BreakStmt{}},
						},
						&syntax.CommandStmt{Args: []syntax.Expr{&syntax.VarRefExpr{Name: "b"}}},
					},
				},
				&syntax.CommandStmt{Args: []syntax.Expr{&syntax.VarRefExpr{Name: "a"}}},
			},
		},
	}}
	require.NoError(t, execParsedProgram(t, prog, env))
	// For each outer iteration: inner emits "x", breaks on "y",
	// then the outer body emits the outer value.
	assert.Equal(t, []string{"x", "1", "x", "2", "x", "3"}, captured)
}

func TestExecSource_ForEach_MultiVarDestructures(t *testing.T) {
	t.Parallel()

	s := NewSession()
	// Two pairs: ["a","1"] and ["b","2"].
	listValue, err := ValueFromJSON([]byte(`[["a","1"],["b","2"]]`))
	require.NoError(t, err)
	s.Set("pairs", listValue)
	var firsts, seconds []string
	env := &Env{
		Session: s,
		ExecCommand: func(args []Arg, _ source.Span) (Value, error) {
			require.Len(t, args, 2)
			firsts = append(firsts, args[0].(ScalarValueArg).Text)
			seconds = append(seconds, args[1].(ScalarValueArg).Text)
			return Value{}, nil
		},
	}
	// foreach (k v) in $pairs { print $k $v }
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.ForEachStmt{
			Names: idents("k", "v"),
			List:  &syntax.VarRefExpr{Name: "pairs"},
			Body: []syntax.Stmt{
				&syntax.CommandStmt{Args: []syntax.Expr{
					&syntax.VarRefExpr{Name: "k"},
					&syntax.VarRefExpr{Name: "v"},
				}},
			},
		},
	}}
	require.NoError(t, execParsedProgram(t, prog, env))
	assert.Equal(t, []string{"a", "b"}, firsts)
	assert.Equal(t, []string{"1", "2"}, seconds)

	_, kOk := s.Get("k")
	_, vOk := s.Get("v")
	assert.False(t, kOk, "loop var $k must not persist after the loop")
	assert.False(t, vOk, "loop var $v must not persist after the loop")
}

func TestExecSource_ForEach_MultiVarLengthMismatchIsError(t *testing.T) {
	t.Parallel()

	s := NewSession()
	// One element is a 3-tuple but the foreach asks for two names.
	listValue, err := ValueFromJSON([]byte(`[["a","1"],["b","2","extra"]]`))
	require.NoError(t, err)
	s.Set("pairs", listValue)
	env := &Env{
		Session:     s,
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
	}
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.ForEachStmt{
			Names: idents("a", "b"),
			List:  &syntax.VarRefExpr{Name: "pairs"},
			Body:  []syntax.Stmt{},
		},
	}}
	evErr := execParsedProgram(t, prog, env)
	require.Error(t, evErr)
	assert.Contains(t, evErr.Error(), "cannot destructure")
}

func TestExecSource_ForEach_MultiVarNonListElementIsError(t *testing.T) {
	t.Parallel()

	s := NewSession()
	// An element is a scalar, not a list.
	listValue, err := ValueFromJSON([]byte(`["scalar"]`))
	require.NoError(t, err)
	s.Set("xs", listValue)
	env := &Env{
		Session:     s,
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
	}
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.ForEachStmt{
			Names: idents("a", "b"),
			List:  &syntax.VarRefExpr{Name: "xs"},
			Body:  []syntax.Stmt{},
		},
	}}
	evErr := execParsedProgram(t, prog, env)
	require.Error(t, evErr)
	assert.Contains(t, evErr.Error(), "not a list")
}

func TestExecSource_ForEach_MultiVarDiscardSlot(t *testing.T) {
	t.Parallel()

	s := NewSession()
	listValue, err := ValueFromJSON([]byte(`[["a","1"],["b","2"]]`))
	require.NoError(t, err)
	s.Set("pairs", listValue)
	var seen []string
	env := &Env{
		Session: s,
		ExecCommand: func(args []Arg, _ source.Span) (Value, error) {
			seen = append(seen, args[0].(ScalarValueArg).Text)
			return Value{}, nil
		},
	}
	// foreach (_ v) in $pairs { $v }
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.ForEachStmt{
			Names: idents("_", "v"),
			List:  &syntax.VarRefExpr{Name: "pairs"},
			Body: []syntax.Stmt{
				&syntax.CommandStmt{Args: []syntax.Expr{&syntax.VarRefExpr{Name: "v"}}},
			},
		},
	}}
	require.NoError(t, execParsedProgram(t, prog, env))
	assert.Equal(t, []string{"1", "2"}, seen)
	// The discard slot must not leak as a real binding.
	_, ok := s.Get("_")
	assert.False(t, ok, "underscore must not become a variable")
}

func TestExecSource_LetDestructure_BindsPositional(t *testing.T) {
	t.Parallel()

	s := NewSession()
	listValue, err := ValueFromJSON([]byte(`["one","two","three"]`))
	require.NoError(t, err)
	s.Set("xs", listValue)
	env := &Env{
		Session:     s,
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
	}
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.LetDestructureStmt{
			Names: idents("a", "b", "c"),
			RHS:   &syntax.VarRefExpr{Name: "xs"},
		},
	}}
	require.NoError(t, execParsedProgram(t, prog, env))

	a, ok := s.Get("a")
	require.True(t, ok)
	got, _ := a.Scalar()
	assert.Equal(t, "one", got)

	b, ok := s.Get("b")
	require.True(t, ok)
	got, _ = b.Scalar()
	assert.Equal(t, "two", got)

	c, ok := s.Get("c")
	require.True(t, ok)
	got, _ = c.Scalar()
	assert.Equal(t, "three", got)
}

func TestExecSource_LetDestructure_DiscardSlots(t *testing.T) {
	t.Parallel()

	s := NewSession()
	listValue, err := ValueFromJSON([]byte(`["first","second","third"]`))
	require.NoError(t, err)
	s.Set("xs", listValue)
	env := &Env{
		Session:     s,
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
	}
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.LetDestructureStmt{
			Names: idents("a", "_", "c"),
			RHS:   &syntax.VarRefExpr{Name: "xs"},
		},
	}}
	require.NoError(t, execParsedProgram(t, prog, env))

	a, ok := s.Get("a")
	require.True(t, ok)
	got, _ := a.Scalar()
	assert.Equal(t, "first", got)

	_, ok = s.Get("_")
	assert.False(t, ok, "underscore must not become a variable")

	c, ok := s.Get("c")
	require.True(t, ok)
	got, _ = c.Scalar()
	assert.Equal(t, "third", got)
}

func TestExecSource_LetDestructure_LengthMismatchIsError(t *testing.T) {
	t.Parallel()

	s := NewSession()
	listValue, err := ValueFromJSON([]byte(`["only"]`))
	require.NoError(t, err)
	s.Set("xs", listValue)
	env := &Env{
		Session:     s,
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
	}
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.LetDestructureStmt{
			Names: idents("a", "b"),
			RHS:   &syntax.VarRefExpr{Name: "xs"},
		},
	}}
	evErr := execParsedProgram(t, prog, env)
	require.Error(t, evErr)
	assert.Contains(t, evErr.Error(), "cannot bind 2 names")
}

func TestExecSource_LetDestructure_NonListIsError(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("x", StringValue("scalar"))
	env := &Env{
		Session:     s,
		ExecCommand: func([]Arg, source.Span) (Value, error) { return Value{}, nil },
	}
	prog := &syntax.Program{Stmts: []syntax.Stmt{
		&syntax.LetDestructureStmt{
			Names: idents("a", "b"),
			RHS:   &syntax.VarRefExpr{Name: "x"},
		},
	}}
	evErr := execParsedProgram(t, prog, env)
	require.Error(t, evErr)
	assert.Contains(t, evErr.Error(), "not a list")
}

func TestEvalExpr_NotEmpty_OffSpecCarrierIsError(t *testing.T) {
	t.Parallel()

	// An int Value reaches the default branch because int is
	// outside the documented carrier vocabulary. The IR evaluator
	// must surface that as a misuse rather than silently treating
	// the value as non-empty.
	s := NewSession()
	s.Set("x", ValueFromAny(42))
	unary := &syntax.UnaryExpr{Pred: "not-empty", Operand: &syntax.VarRefExpr{Name: "x"}}

	_, err := evalLoweredExpr(unary, evalEnv(s))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not-empty")
}

func TestExecSource_CommandArg_ParenExprArithmetic(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("x", StringValue("5"))
	var captured string
	env := &Env{
		Session: s,
		ExecCommand: func(args []Arg, _ source.Span) (Value, error) {
			require.Len(t, args, 2)
			captured = args[1].(ScalarValueArg).Text
			return Value{}, nil
		},
	}
	// print ($x + 1) -- the BinaryExpr evaluates and wraps as
	// ScalarValueArg via the default evalArg path.
	prog, err := parseSource(t, `print ($x + 1)`)
	require.NoError(t, err)
	require.NoError(t, execParsedProgram(t, prog, env))
	assert.Equal(t, "6", captured)
}

func TestExecSource_CommandArg_ParenExprListLiteral(t *testing.T) {
	t.Parallel()

	s := NewSession()
	var captured Arg
	env := &Env{
		Session: s,
		ExecCommand: func(args []Arg, _ source.Span) (Value, error) {
			require.Len(t, args, 2)
			captured = args[1]
			return Value{}, nil
		},
	}
	// print ([1 2 3]) -- a list literal in argument position
	// resolves to a StructuredValueArg via the default path.
	prog, err := parseSource(t, `print ([1 2 3])`)
	require.NoError(t, err)
	require.NoError(t, execParsedProgram(t, prog, env))
	sv, ok := captured.(StructuredValueArg)
	require.True(t, ok, "arg should be StructuredValueArg, got %T", captured)
	raw, ok := sv.Value.Raw().([]any)
	require.True(t, ok)
	assert.Len(t, raw, 3)
}

func TestExecSource_Break_OutsideLoopIsError(t *testing.T) {
	t.Parallel()

	s := NewSession()
	env := &Env{Session: s}
	prog := &syntax.Program{Stmts: []syntax.Stmt{&syntax.BreakStmt{}}}
	err := execParsedProgram(t, prog, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "break")
	assert.Contains(t, err.Error(), "outside")
}

func TestExecSource_Continue_OutsideLoopIsError(t *testing.T) {
	t.Parallel()

	s := NewSession()
	env := &Env{Session: s}
	prog := &syntax.Program{Stmts: []syntax.Stmt{&syntax.ContinueStmt{}}}
	err := execParsedProgram(t, prog, env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "continue")
	assert.Contains(t, err.Error(), "outside")
}

// --- logical operators ---------------------------------------------

func TestEvalExpr_And_BothTrue(t *testing.T) {
	t.Parallel()

	v, err := evalLoweredExpr(&syntax.LogicalExpr{
		Op:    "and",
		Left:  &syntax.BinaryExpr{Left: &syntax.LiteralExpr{Text: "1"}, Op: "==", Right: &syntax.LiteralExpr{Text: "1"}},
		Right: &syntax.BinaryExpr{Left: &syntax.LiteralExpr{Text: "2"}, Op: "==", Right: &syntax.LiteralExpr{Text: "2"}},
	}, evalEnv(NewSession()))
	require.NoError(t, err)
	b, err := AsBool(v)
	require.NoError(t, err)
	assert.True(t, b)
}

func TestEvalExpr_And_ShortCircuitsOnFalseLeft(t *testing.T) {
	t.Parallel()

	// Right operand would error on Scalar() -- if the short-circuit
	// fires correctly, it's never evaluated.
	s := NewSession()
	s.Set("m", ValueFromMap(map[string]any{"x": 1}))
	v, err := evalLoweredExpr(&syntax.LogicalExpr{
		Op:    "and",
		Left:  &syntax.BinaryExpr{Left: &syntax.LiteralExpr{Text: "1"}, Op: "==", Right: &syntax.LiteralExpr{Text: "2"}},
		Right: &syntax.VarRefExpr{Name: "m"},
	}, evalEnv(s))
	require.NoError(t, err)
	b, err := AsBool(v)
	require.NoError(t, err)
	assert.False(t, b)
}

func TestEvalExpr_Or_ShortCircuitsOnTrueLeft(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("m", ValueFromMap(map[string]any{"x": 1}))
	v, err := evalLoweredExpr(&syntax.LogicalExpr{
		Op:    "or",
		Left:  &syntax.BinaryExpr{Left: &syntax.LiteralExpr{Text: "1"}, Op: "==", Right: &syntax.LiteralExpr{Text: "1"}},
		Right: &syntax.VarRefExpr{Name: "m"},
	}, evalEnv(s))
	require.NoError(t, err)
	b, err := AsBool(v)
	require.NoError(t, err)
	assert.True(t, b)
}

func TestEvalExpr_Or_BothFalse(t *testing.T) {
	t.Parallel()

	v, err := evalLoweredExpr(&syntax.LogicalExpr{
		Op:    "or",
		Left:  &syntax.BinaryExpr{Left: &syntax.LiteralExpr{Text: "1"}, Op: "==", Right: &syntax.LiteralExpr{Text: "2"}},
		Right: &syntax.BinaryExpr{Left: &syntax.LiteralExpr{Text: "3"}, Op: "==", Right: &syntax.LiteralExpr{Text: "4"}},
	}, evalEnv(NewSession()))
	require.NoError(t, err)
	b, err := AsBool(v)
	require.NoError(t, err)
	assert.False(t, b)
}

func TestEvalExpr_Not_Negates(t *testing.T) {
	t.Parallel()

	v, err := evalLoweredExpr(&syntax.NotExpr{
		Operand: &syntax.BinaryExpr{Left: &syntax.LiteralExpr{Text: "1"}, Op: "==", Right: &syntax.LiteralExpr{Text: "1"}},
	}, evalEnv(NewSession()))
	require.NoError(t, err)
	b, err := AsBool(v)
	require.NoError(t, err)
	assert.False(t, b)
}

func TestEvalExpr_Not_RejectsNonBool(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("x", StringValue("hello"))
	_, err := evalLoweredExpr(&syntax.NotExpr{Operand: &syntax.VarRefExpr{Name: "x"}}, evalEnv(s))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not")
}

func TestEvalExpr_And_RejectsNonBoolLeft(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("x", StringValue("hello"))
	_, err := evalLoweredExpr(&syntax.LogicalExpr{
		Op:    "and",
		Left:  &syntax.VarRefExpr{Name: "x"},
		Right: &syntax.BinaryExpr{Left: &syntax.LiteralExpr{Text: "1"}, Op: "==", Right: &syntax.LiteralExpr{Text: "1"}},
	}, evalEnv(s))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "and")
}

// --- arithmetic ----------------------------------------------------

// scalarTextEval is a small helper that evaluates an expression
// and returns its scalar-formatted result. Every arithmetic
// test reduces to "evaluate, compare the rendered string" -- the
// helper keeps the call sites short.
func scalarTextEval(t *testing.T, e syntax.Expr) string {
	t.Helper()
	v, err := evalLoweredExpr(e, evalEnv(NewSession()))
	require.NoError(t, err)
	s, err := v.Scalar()
	require.NoError(t, err)
	return s
}

func TestEvalExpr_Arithmetic_AllOps(t *testing.T) {
	t.Parallel()

	cases := []struct {
		op    string
		left  string
		right string
		want  string
	}{
		// integer-valued operands render without a trailing ".0".
		{"+", "1", "2", "3"},
		{"-", "5", "3", "2"},
		{"*", "4", "3", "12"},
		{"/", "10", "4", "2.5"},
		{"%", "7", "3", "1"},
		// float operands keep their precision.
		{"+", "1.5", "2.25", "3.75"},
		{"*", "2.0", "3.0", "6"},
		// mixed (int + float) still lands on float semantics.
		{"/", "5", "2", "2.5"},
		{"%", "7.5", "2.0", "1.5"},
	}
	for _, tc := range cases {
		t.Run(tc.op+"_"+tc.left+"_"+tc.right, func(t *testing.T) {
			t.Parallel()
			e := &syntax.BinaryExpr{
				Left:  &syntax.LiteralExpr{Text: tc.left},
				Op:    tc.op,
				Right: &syntax.LiteralExpr{Text: tc.right},
			}
			assert.Equal(t, tc.want, scalarTextEval(t, e))
		})
	}
}

func TestEvalExpr_Arithmetic_DivideByZero(t *testing.T) {
	t.Parallel()

	for _, op := range []string{"/", "%"} {
		t.Run(op, func(t *testing.T) {
			t.Parallel()
			e := &syntax.BinaryExpr{
				Left:  &syntax.LiteralExpr{Text: "1"},
				Op:    op,
				Right: &syntax.LiteralExpr{Text: "0"},
			}
			_, err := evalLoweredExpr(e, evalEnv(NewSession()))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "division by zero")
		})
	}
}

func TestEvalExpr_Arithmetic_NonNumericOperand(t *testing.T) {
	t.Parallel()

	// "abc" + 1: Python-style string concat is deliberately out
	// of scope, so this must surface as a numeric-operand error
	// rather than producing a string.
	e := &syntax.BinaryExpr{
		Left:  &syntax.LiteralExpr{Text: "abc"},
		Op:    "+",
		Right: &syntax.LiteralExpr{Text: "1"},
	}
	_, err := evalLoweredExpr(e, evalEnv(NewSession()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not numeric")
}

func TestEvalExpr_Negate_Literal(t *testing.T) {
	t.Parallel()

	e := &syntax.NegateExpr{Operand: &syntax.LiteralExpr{Text: "5"}}
	assert.Equal(t, "-5", scalarTextEval(t, e))
}

func TestEvalExpr_Negate_DoubleNegate(t *testing.T) {
	t.Parallel()

	// -(-5) -> 5: stacks resolve inside-out.
	e := &syntax.NegateExpr{Operand: &syntax.NegateExpr{Operand: &syntax.LiteralExpr{Text: "5"}}}
	assert.Equal(t, "5", scalarTextEval(t, e))
}

func TestEvalExpr_Negate_StructuredIsError(t *testing.T) {
	t.Parallel()

	// Negating a map is nonsense -- must error rather than panic.
	s := NewSession()
	s.Set("m", ValueFromMap(map[string]any{"x": 1}))
	_, err := evalLoweredExpr(&syntax.NegateExpr{Operand: &syntax.VarRefExpr{Name: "m"}}, evalEnv(s))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "negate")
}

func TestEvalExpr_Negate_NonNumericScalarIsError(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("x", StringValue("hello"))
	_, err := evalLoweredExpr(&syntax.NegateExpr{Operand: &syntax.VarRefExpr{Name: "x"}}, evalEnv(s))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not numeric")
}

func TestEvalExpr_Arithmetic_InComparisonPosition(t *testing.T) {
	t.Parallel()

	// 3 + 4 > 5 -> true. Exercises the full chain:
	// comparison evaluates additive on both sides, reduces each
	// to a numeric scalar, then compares as floats.
	e := &syntax.BinaryExpr{
		Left: &syntax.BinaryExpr{
			Left:  &syntax.LiteralExpr{Text: "3"},
			Op:    "+",
			Right: &syntax.LiteralExpr{Text: "4"},
		},
		Op:    ">",
		Right: &syntax.LiteralExpr{Text: "5"},
	}
	v, err := evalLoweredExpr(e, evalEnv(NewSession()))
	require.NoError(t, err)
	b, err := AsBool(v)
	require.NoError(t, err)
	assert.True(t, b)
}

func TestEvalExpr_Arithmetic_LetRHS(t *testing.T) {
	t.Parallel()

	// let n = $count + 1: parse, evaluate, confirm the session
	// carries a numeric scalar whose text is "11".
	prog, err := parseSource(t, "let count = 10\nlet n = $count + 1")
	require.NoError(t, err)
	s := NewSession()
	require.NoError(t, execParsedProgram(t, prog, evalEnv(s)))
	v, ok := s.Get("n")
	require.True(t, ok)
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "11", got)
}

func TestEvalExpr_InterpString_LiteralOnly(t *testing.T) {
	t.Parallel()

	// An InterpStringExpr with only literal segments (rare in
	// practice -- the lexer emits TokenQuoted for that case --
	// but the evaluator is happy to concatenate literals if a
	// caller constructs the node directly).
	s := NewSession()
	e := &syntax.InterpStringExpr{
		Segments: []syntax.InterpStringSegment{
			{Literal: "hello "},
			{Literal: "world"},
		},
	}
	v, err := evalLoweredExpr(e, evalEnv(s))
	require.NoError(t, err)
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "hello world", got)
}

func TestEvalExpr_InterpString_VarRef(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("n", StringValue("60"))
	e := &syntax.InterpStringExpr{
		Segments: []syntax.InterpStringSegment{
			{Expr: &syntax.VarRefExpr{Name: "n"}},
			{Literal: "s"},
		},
	}
	v, err := evalLoweredExpr(e, evalEnv(s))
	require.NoError(t, err)
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "60s", got)
}

func TestEvalExpr_InterpString_MixedSegments(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("prog", StringValue("42"))
	e := &syntax.InterpStringExpr{
		Segments: []syntax.InterpStringSegment{
			{Literal: "/sys/fs/bpf/prog-"},
			{Expr: &syntax.VarRefExpr{Name: "prog"}},
			{Literal: "/map"},
		},
	}
	v, err := evalLoweredExpr(e, evalEnv(s))
	require.NoError(t, err)
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "/sys/fs/bpf/prog-42/map", got)
}

func TestEvalExpr_InterpString_StructuredValueCompactJSON(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("r", ValueFromMap(map[string]any{"exit_code": 0, "stdout": "hi"}))
	e := &syntax.InterpStringExpr{
		Segments: []syntax.InterpStringSegment{
			{Expr: &syntax.VarRefExpr{Name: "r"}},
		},
	}
	v, err := evalLoweredExpr(e, evalEnv(s))
	require.NoError(t, err)
	got, err := v.Scalar()
	require.NoError(t, err)
	// json.Marshal sorts map keys alphabetically, so the output is
	// stable regardless of the input map's iteration order. One
	// line, no indentation.
	assert.Equal(t, `{"exit_code":0,"stdout":"hi"}`, got)
}

func TestEvalExpr_InterpString_ArrayCompactJSON(t *testing.T) {
	t.Parallel()

	s := NewSession()
	s.Set("xs", ValueFromAny([]any{float64(1), float64(2), float64(3)}))
	e := &syntax.InterpStringExpr{
		Segments: []syntax.InterpStringSegment{
			{Literal: "items="},
			{Expr: &syntax.VarRefExpr{Name: "xs"}},
		},
	}
	v, err := evalLoweredExpr(e, evalEnv(s))
	require.NoError(t, err)
	got, err := v.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "items=[1,2,3]", got)
}

func TestEvalExpr_InterpString_NilRendersAsNull(t *testing.T) {
	t.Parallel()

	// A nil Value in the interpolation slot renders as "null" so
	// the output string stays well-formed. We exercise the
	// helper directly because nothing in the expression grammar
	// produces a bare nil Value today -- VarRefExpr with a missing
	// path errors at lookup time rather than falling through to
	// nil.
	got, err := RenderCompact(Value{})
	require.NoError(t, err)
	assert.Equal(t, "null", got)
}

func TestEvalExpr_InterpString_EndToEnd(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		setup func(*Session)
		input string
		want  string
	}{
		{
			name:  "plain literal stays a literal",
			input: `let x = "hello"`,
			want:  "hello",
		},
		{
			name:  "single variable interpolation",
			setup: func(s *Session) { s.Set("n", StringValue("60")) },
			input: `let x = "${n}s"`,
			want:  "60s",
		},
		{
			name:  "path construction",
			setup: func(s *Session) { s.Set("id", StringValue("42")) },
			input: `let x = "/sys/fs/bpf/prog-${id}/map"`,
			want:  "/sys/fs/bpf/prog-42/map",
		},
		{
			name: "adjacent interpolations",
			setup: func(s *Session) {
				s.Set("a", StringValue("hello"))
				s.Set("b", StringValue("world"))
			},
			input: `let x = "${a}${b}"`,
			want:  "helloworld",
		},
		{
			name:  "arithmetic inside interpolation",
			setup: func(s *Session) { s.Set("n", StringValue("30")) },
			input: `let x = "${$n * 2}s"`,
			want:  "60s",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := NewSession()
			if tc.setup != nil {
				tc.setup(s)
			}
			prog, err := parseSource(t, tc.input)
			require.NoError(t, err)
			require.NoError(t, execParsedProgram(t, prog, evalEnv(s)))
			v, ok := s.Get("x")
			require.True(t, ok)
			got, err := v.Scalar()
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEvalExpr_InterpString_UndefinedVar(t *testing.T) {
	t.Parallel()

	s := NewSession()
	e := &syntax.InterpStringExpr{
		Segments: []syntax.InterpStringSegment{
			{Expr: &syntax.VarRefExpr{Name: "missing"}},
		},
	}
	_, err := evalLoweredExpr(e, evalEnv(s))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "undefined variable")
}

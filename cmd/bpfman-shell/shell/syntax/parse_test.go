package syntax

// Note: additional if-statement parse tests live in parse_if_test.go.

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

// parseSource is a convenience that drives tokenisation and parsing
// so tests can speak in terms of surface syntax.
func parseSource(t *testing.T, src string) (*Program, error) {
	t.Helper()
	tokens, err := Tokenise(src)
	require.NoError(t, err)
	return Parse(tokens)
}

// firstStmt returns the single statement of a program, failing the
// test if the program contains zero or more than one statement.
func firstStmt(t *testing.T, prog *Program) Stmt {
	t.Helper()
	require.Len(t, prog.Stmts, 1)
	return prog.Stmts[0]
}

func identTextList(idents []Ident) []string {
	out := make([]string, 0, len(idents))
	for _, ident := range idents {
		out = append(out, ident.Text)
	}
	return out
}

func TestParse_SingleWordCommand(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "hostname")
	require.NoError(t, err)
	cmd, ok := firstStmt(t, prog).(*CommandStmt)
	require.True(t, ok)
	require.Len(t, cmd.Args, 1)
	lit, ok := cmd.Args[0].(*LiteralExpr)
	require.True(t, ok)
	assert.Equal(t, "hostname", lit.Text)
	assert.False(t, lit.Quoted)
}

func TestParse_PlainCommand(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "show program 123")
	require.NoError(t, err)
	cmd, ok := firstStmt(t, prog).(*CommandStmt)
	require.True(t, ok)
	require.Len(t, cmd.Args, 3)
	for i, want := range []string{"show", "program", "123"} {
		lit, ok := cmd.Args[i].(*LiteralExpr)
		require.True(t, ok, "arg %d", i)
		assert.Equal(t, want, lit.Text)
	}
}

func TestParse_LineContinuation(t *testing.T) {
	t.Parallel()

	t.Run("bare command with backslash continuation", func(t *testing.T) {
		t.Parallel()
		prog, err := parseSource(t, "show program \\\n123")
		require.NoError(t, err)
		cmd, ok := firstStmt(t, prog).(*CommandStmt)
		require.True(t, ok)
		require.Len(t, cmd.Args, 3)
		for i, want := range []string{"show", "program", "123"} {
			lit, ok := cmd.Args[i].(*LiteralExpr)
			require.True(t, ok, "arg %d", i)
			assert.Equal(t, want, lit.Text)
		}
	})

	t.Run("continuation inside bind RHS", func(t *testing.T) {
		t.Parallel()
		prog, err := parseSource(t, "let p <- show program \\\n123")
		require.NoError(t, err)
		bind, ok := firstStmt(t, prog).(*BindStmt)
		require.True(t, ok)
		assert.Equal(t, "p", bind.Target.Text)
		require.NotNil(t, bind.Cmd)
		require.Len(t, bind.Cmd.Args, 3)
	})

	t.Run("multiple continuations inside bind RHS", func(t *testing.T) {
		t.Parallel()
		src := "let p <- show \\\nprogram \\\n123"
		prog, err := parseSource(t, src)
		require.NoError(t, err)
		bind, ok := firstStmt(t, prog).(*BindStmt)
		require.True(t, ok)
		require.NotNil(t, bind.Cmd)
		require.Len(t, bind.Cmd.Args, 3)
	})
}

func TestParse_EmptyListLiteral(t *testing.T) {
	t.Parallel()

	t.Run("let RHS accepts an empty list", func(t *testing.T) {
		t.Parallel()
		prog, err := parseSource(t, "let empty = []")
		require.NoError(t, err)
		let, ok := firstStmt(t, prog).(*LetStmt)
		require.True(t, ok)
		list, ok := let.RHS.(*ListExpr)
		require.True(t, ok, "RHS should be a ListExpr, got %T", let.RHS)
		assert.Empty(t, list.Elems)
	})

	t.Run("empty list with internal whitespace", func(t *testing.T) {
		t.Parallel()
		prog, err := parseSource(t, "let empty = [  ]")
		require.NoError(t, err)
		let, ok := firstStmt(t, prog).(*LetStmt)
		require.True(t, ok)
		list, ok := let.RHS.(*ListExpr)
		require.True(t, ok)
		assert.Empty(t, list.Elems)
	})
}

func TestParse_MultilineListLiteral(t *testing.T) {
	t.Parallel()

	t.Run("let RHS wraps across newlines without continuation", func(t *testing.T) {
		t.Parallel()
		src := "let priorities = [\n100\n200\n300\n]"
		prog, err := parseSource(t, src)
		require.NoError(t, err)
		let, ok := firstStmt(t, prog).(*LetStmt)
		require.True(t, ok)
		assert.Equal(t, "priorities", let.Name.Text)
		list, ok := let.RHS.(*ListExpr)
		require.True(t, ok, "RHS should be a ListExpr, got %T", let.RHS)
		require.Len(t, list.Elems, 3)
	})

	t.Run("bind RHS wraps across newlines", func(t *testing.T) {
		t.Parallel()
		// foreach iteration source is an expression; a multi-line
		// list literal there exercises the bracket-aware bind RHS
		// collector.
		src := "let xs <- foreach p in [\n100\n200\n] {\n  echo $p\n}"
		_, err := parseSource(t, src)
		require.NoError(t, err)
	})

	t.Run("nested let inside foreach body wraps across newlines", func(t *testing.T) {
		t.Parallel()
		src := "foreach i in [0] {\n  let xs = [\n    1\n    2\n  ]\n  echo $xs\n}"
		_, err := parseSource(t, src)
		require.NoError(t, err)
	})
}

func TestParse_RecordLiteral(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, `let r = record {
    prog: $p
    link: $l
}`)
	require.NoError(t, err)
	let, ok := firstStmt(t, prog).(*LetStmt)
	require.True(t, ok, "stmt should be a LetStmt, got %T", prog.Stmts[0])
	rec, ok := let.RHS.(*RecordExpr)
	require.True(t, ok, "RHS should be a RecordExpr, got %T", let.RHS)
	require.Len(t, rec.Fields, 2)
	assert.Equal(t, "prog", rec.Fields[0].Name)
	assert.Equal(t, "link", rec.Fields[1].Name)
	_, ok = rec.Fields[0].Expr.(*VarRefExpr)
	assert.True(t, ok, "field prog should be a VarRefExpr, got %T", rec.Fields[0].Expr)
}

func TestParse_RecordLiteralErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		wantErr string
	}{
		{
			name:    "missing_open_brace",
			src:     `let r = record prog: $p }`,
			wantErr: "record literal requires '{'",
		},
		{
			name:    "duplicate_field",
			src:     `let r = record { prog: $p prog: $q }`,
			wantErr: `duplicate record field "prog"`,
		},
		{
			name:    "invalid_field_name",
			src:     `let r = record { 1prog: $p }`,
			wantErr: `invalid record field name "1prog"`,
		},
		{
			name:    "comma_separator",
			src:     `let r = record { prog: $p, link: $l }`,
			wantErr: "record fields are whitespace-separated",
		},
		{
			name:    "glued_comma_string_value",
			src:     `let r = record { prog: foo, }`,
			wantErr: "unquoted comma in expression literal",
		},
		{
			name:    "glued_comma_numeric_value",
			src:     `let r = record { count: 1, }`,
			wantErr: "unquoted comma in expression literal",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseSource(t, tt.src)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParse_ExpressionLiteralErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		wantErr string
	}{
		{
			name:    "digit_leading_string_text_must_be_quoted",
			src:     `let s = 5s`,
			wantErr: `invalid numeric literal "5s"`,
		},
		{
			name:    "digit_leading_word_must_be_number",
			src:     `let port = 8080x`,
			wantErr: `invalid numeric literal "8080x"`,
		},
		{
			name:    "sign_leading_word_must_be_number",
			src:     `let delta = -3s`,
			wantErr: `invalid numeric literal "-3s"`,
		},
		{
			name:    "negative_number_remains_valid",
			src:     `let delta = -3`,
			wantErr: "",
		},
		{
			name:    "overflowing_json_number_has_range_error",
			src:     `let huge = 1e309`,
			wantErr: `numeric literal "1e309" exceeds the representable range`,
		},
		{
			name:    "glued_division_is_one_bad_numeric_word",
			src:     `let ratio = 1/2`,
			wantErr: `invalid numeric literal "1/2"`,
		},
		{
			name:    "command_position_stays_argv_shaped",
			src:     `exec echo 5s 1/2 foo,`,
			wantErr: "",
		},
		{
			name:    "duration_position_accepts_bare_duration",
			src:     `poll timeout 5s every 100ms { print ok }`,
			wantErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseSource(t, tt.src)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParse_LetAssignment_Literal(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "let prog = 42")
	require.NoError(t, err)
	let, ok := firstStmt(t, prog).(*LetStmt)
	require.True(t, ok)
	assert.Equal(t, "prog", let.Name.Text)
	lit, ok := let.RHS.(*LiteralExpr)
	require.True(t, ok)
	assert.Equal(t, "42", lit.Text)
}

func TestParse_LetRejectsMultiTokenCommand(t *testing.T) {
	t.Parallel()

	// "load file" is two words, neither of which forms a
	// valid expression on the right of '='; the recursive-
	// descent parser surfaces this as "unexpected" against
	// the trailing word.
	_, err := parseSource(t, "let prog = load file")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected")
}

func TestParse_LetWithVarRef(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "let link = $prog")
	require.NoError(t, err)
	let, ok := firstStmt(t, prog).(*LetStmt)
	require.True(t, ok)
	assert.Equal(t, "link", let.Name.Text)
	ref, ok := let.RHS.(*VarRefExpr)
	require.True(t, ok)
	assert.Equal(t, "prog", ref.Name)
}

func TestParse_LetErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"no command after equals", "let x =", "let requires"},
		{"too few tokens", "let x", "let requires"},
		{"missing equals or bind", "let x load file", "let requires '=' or '<-'"},
		{"non-identifier LHS", "let $x = foo", "let requires an identifier"},
		{"invalid identifier", "let 0bad = foo", "invalid variable name"},
		{"second assign in RHS", "let x = a = b", "unexpected '='"},
		{"single-name underscore", "let _ = something", "single-name let cannot bind '_'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseSource(t, tc.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestParse_LetBindSingle(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "let r <- bpfman version")
	require.NoError(t, err)
	bind, ok := firstStmt(t, prog).(*BindStmt)
	require.True(t, ok, "expected BindStmt, got %T", firstStmt(t, prog))
	assert.Equal(t, "r", bind.Target.Text)
	assert.False(t, bind.Guard, "let bind must not set Guard")
	require.NotNil(t, bind.Cmd)
	require.Len(t, bind.Cmd.Args, 2)
	head, ok := bind.Cmd.Args[0].(*LiteralExpr)
	require.True(t, ok)
	assert.Equal(t, "bpfman", head.Text)
}

func TestParse_GuardBindSingle(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "guard prog <- bpfman program get $pid")
	require.NoError(t, err)
	bind, ok := firstStmt(t, prog).(*BindStmt)
	require.True(t, ok, "expected BindStmt, got %T", firstStmt(t, prog))
	assert.Equal(t, "prog", bind.Target.Text)
	assert.True(t, bind.Guard, "guard bind must set Guard")
	require.NotNil(t, bind.Cmd)
	require.Len(t, bind.Cmd.Args, 4)
	ref, ok := bind.Cmd.Args[3].(*VarRefExpr)
	require.True(t, ok, "last arg should be VarRef, got %T", bind.Cmd.Args[3])
	assert.Equal(t, "pid", ref.Name)
}

func TestParse_LetBindTupleRejected(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "let (rc prog) <- bpfman program get $pid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tuple bind after '<-' is no longer supported")
}

func TestParse_GuardBindTupleRejected(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "guard (rc prog) <- bpfman program get $pid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tuple bind after '<-' is no longer supported")
}

func TestParse_BindTupleDiscardRejected(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "let (_ prog) <- bpfman program get $pid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tuple bind after '<-' is no longer supported")
}

func TestParse_LetDestructure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"two names", "let (a b) = $xs", []string{"a", "b"}},
		{"three names", "let (a b c) = $xs", []string{"a", "b", "c"}},
		{"discard middle", "let (a _ c) = $xs", []string{"a", "_", "c"}},
		{"discard first", "let (_ b) = $xs", []string{"_", "b"}},
		{"discard last", "let (a _) = $xs", []string{"a", "_"}},
		{"list literal rhs", "let (a b) = [1 2]", []string{"a", "b"}},
		{"name list wraps over newlines", "let (a\n  b\n  c) = $xs", []string{"a", "b", "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := parseSource(t, tc.input)
			require.NoError(t, err)
			let, ok := firstStmt(t, prog).(*LetDestructureStmt)
			require.True(t, ok, "expected LetDestructureStmt, got %T", firstStmt(t, prog))
			assert.Equal(t, tc.want, identTextList(let.Names))
			assert.NotNil(t, let.RHS)
		})
	}
}

func TestParse_BindErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"empty let bind RHS", "let x <-", "bind requires a command after '<-'"},
		{"empty guard bind RHS", "guard x <-", "bind requires a command after '<-'"},
		{"guard without bind sigil", "guard x = foo", "missing '<-'"},
		{"guard without name", "guard <- foo", "guard requires"},
		{"chained bind sigils rejected", "let x <- foo <- bar", "unexpected '<-' on bind RHS"},
		{"assign inside bind RHS rejected", "let x <- foo = bar", "unexpected '=' on bind RHS"},
		{"tuple discard both rejected", "let (_ _) <- foo", "tuple bind after '<-' is no longer supported"},
		{"tuple comma rejected", "let (rc, prog) <- foo", "comma is not a separator"},
		{"tuple trailing comma rejected", "let (rc prog,) <- foo", "comma is not a separator"},
		{"tuple missing close paren", "let (rc prog <- foo", "expected name"},
		{"single-name parens rejected at let", "let (x) = $foo", "requires at least two names"},
		{"three-slot bind tuple rejected", "let (a b c) <- foo", "tuple bind after '<-' is no longer supported"},
		{"single-name parens rejected at guard", "guard (x) <- foo", "tuple bind after '<-' is no longer supported"},
		{"three-slot guard tuple rejected", "guard (a b c) <- foo", "tuple bind after '<-' is no longer supported"},
		{"all-underscore destructure rejected", "let (_ _) = $xs", "at least one must bind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseSource(t, tc.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestParse_GuardIsReservedDefName(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "def guard() { print hi }")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved word \"guard\"")
}

func TestParse_BareAssignIsError(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "prog = load file")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected '='")
}

func TestParse_AliasSyntaxRejected(t *testing.T) {
	t.Parallel()

	// The parser treats `alias name = expansion` like any other
	// command form: a stray '=' at command position is rejected
	// with the "use `let` for assignment" hint. Pinning this
	// stops the syntax sneaking back through preflight only to
	// fail at runtime.
	_, err := parseSource(t, "alias b = bpfman")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected '='")
}

func TestParse_VarRefOnlyExprStmt(t *testing.T) {
	t.Parallel()

	// A leading varref is treated as an expression statement so the
	// evaluator can auto-print its value at top level.
	prog, err := parseSource(t, "$prog.id")
	require.NoError(t, err)
	es, ok := firstStmt(t, prog).(*ExprStmt)
	require.True(t, ok)
	ref, ok := es.Expr.(*VarRefExpr)
	require.True(t, ok)
	assert.Equal(t, "prog", ref.Name)
	assert.Equal(t, "id", ref.Path)
}

func TestParse_ExprStmt_TriggerTokens(t *testing.T) {
	t.Parallel()

	// Each leading token in the trigger set must route to
	// ExprStmt, not CommandStmt. Bare words keep routing to
	// CommandStmt.
	cases := []struct {
		name  string
		input string
		want  string // "expr" or "command"
	}{
		{"varref", "$x", "expr"},
		{"varref with path", "$x.a.b", "expr"},
		{"varref with comparison", "$x == 5", "expr"},
		{"varref with thread", "$x |> jq \".\"", "expr"},
		{"quoted", "\"hello\"", "expr"},
		{"single-quoted", "'hello'", "expr"},
		{"paren expression", "(1 == 1)", "expr"},
		{"not prefix", "not $x", "expr"},
		{"unary pred with operand", "not-empty $x", "expr"},
		{"bare word", "foo", "command"},
		{"bare number", "1", "command"},
		{"command invocation", "bpfman program list", "command"},
		{"keyword let", "let x = 1", "command"}, // actually LetStmt, not CommandStmt
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := parseSource(t, tc.input)
			require.NoError(t, err)
			require.NotEmpty(t, prog.Stmts)
			stmt := prog.Stmts[0]
			switch tc.want {
			case "expr":
				_, ok := stmt.(*ExprStmt)
				assert.True(t, ok, "expected ExprStmt, got %T", stmt)
			case "command":
				// Either a CommandStmt or a keyword-led statement
				// (LetStmt, IfStmt, ...) -- anything that is not
				// ExprStmt is acceptable.
				_, isExpr := stmt.(*ExprStmt)
				assert.False(t, isExpr, "expected non-ExprStmt, got %T", stmt)
			}
		})
	}
}

func TestParse_StatementLeadingListLiteralRejected(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "[1 2 3]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list literal at statement position is not allowed")
}

func TestParse_EmptyProgram(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "")
	require.NoError(t, err)
	assert.Empty(t, prog.Stmts)
}

func TestParse_LocPropagation(t *testing.T) {
	t.Parallel()

	// Statements and expressions should carry source.Pos from their first
	// token. A multi-line program has different lines on each
	// statement.
	prog, err := parseSource(t, "hostname\nshow program")
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 2)
	first, ok := prog.Stmts[0].(*CommandStmt)
	require.True(t, ok)
	assert.Equal(t, 1, first.Pos.Line)
	second, ok := prog.Stmts[1].(*CommandStmt)
	require.True(t, ok)
	assert.Equal(t, 2, second.Pos.Line)
}

func TestParse_Thread_Basic(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "let r = $x |> jq \"add\"")
	require.NoError(t, err)
	let, ok := firstStmt(t, prog).(*LetStmt)
	require.True(t, ok)
	thread, ok := let.RHS.(*ThreadExpr)
	require.True(t, ok, "RHS should be a ThreadExpr, got %T", let.RHS)
	// LHS is the value expression producing $x.
	lhs, ok := thread.LHS.(*VarRefExpr)
	require.True(t, ok)
	assert.Equal(t, "x", lhs.Name)
	// Args is the thread's right-hand command: jq "add" (2 args).
	require.Len(t, thread.Args, 2)
	jqLit, ok := thread.Args[0].(*LiteralExpr)
	require.True(t, ok)
	assert.Equal(t, "jq", jqLit.Text)
	filterLit, ok := thread.Args[1].(*LiteralExpr)
	require.True(t, ok)
	assert.Equal(t, "add", filterLit.Text)
	assert.True(t, filterLit.Quoted)
}

func TestParse_Thread_Chain_LeftAssociative(t *testing.T) {
	t.Parallel()

	// a |> b |> c should parse as (a |> b) |> c.
	prog, err := parseSource(t, "let r = $x |> jq \".a\" |> jq \"add\"")
	require.NoError(t, err)
	let := firstStmt(t, prog).(*LetStmt)
	outer, ok := let.RHS.(*ThreadExpr)
	require.True(t, ok)
	// Outer's LHS is itself a ThreadExpr (the inner one).
	inner, ok := outer.LHS.(*ThreadExpr)
	require.True(t, ok, "outer.LHS should be a ThreadExpr (left-assoc), got %T", outer.LHS)
	innerLHS, ok := inner.LHS.(*VarRefExpr)
	require.True(t, ok)
	assert.Equal(t, "x", innerLHS.Name)
}

func TestParse_Thread_TighterThanComparison(t *testing.T) {
	t.Parallel()

	// $x |> jq "..." > 0 should parse as ($x |> jq "...") > 0.
	prog, err := parseSource(t, "let r = $x |> jq \".n\" > 0")
	require.NoError(t, err)
	let := firstStmt(t, prog).(*LetStmt)
	bin, ok := let.RHS.(*BinaryExpr)
	require.True(t, ok, "RHS should be BinaryExpr, got %T", let.RHS)
	assert.Equal(t, ">", bin.Op)
	// LHS of comparison is the thread chain.
	_, ok = bin.Left.(*ThreadExpr)
	assert.True(t, ok, "binary LHS should be ThreadExpr, got %T", bin.Left)
	// RHS is the literal 0.
	_, ok = bin.Right.(*LiteralExpr)
	assert.True(t, ok)
}

func TestParse_Thread_LocPointsAtThreadToken(t *testing.T) {
	t.Parallel()

	// ThreadExpr spans cover the full threaded expression, but
	// the PipePos still identifies the `|>` itself so errors about
	// the threading step can point at the operator rather than at
	// the LHS or RHS.
	prog, err := parseSource(t, "let r = $x |> jq \"add\"")
	require.NoError(t, err)
	let := firstStmt(t, prog).(*LetStmt)
	thread := let.RHS.(*ThreadExpr)
	assert.Equal(t, 1, thread.PipePos.Line)
	// Column of the `|>` in "let r = $x |> jq \"add\"":
	//   columns 1..9 = "let r = $"
	//   column 10 = 'x' (end of varref) -- the `|>` is
	//   after `$x ` so at column 12.
	assert.Equal(t, 12, thread.PipePos.Col)
}

func TestParse_Thread_StopsAtClosingParen(t *testing.T) {
	t.Parallel()

	// A thread inside a parenthesised expression must let the ')'
	// close the enclosing parens, not consume it as a literal arg.
	prog, err := parseSource(t, "if ($xs |> jq \".items\") { print ok }")
	require.NoError(t, err)
	ifStmt := firstStmt(t, prog).(*IfStmt)
	thread, ok := ifStmt.Cond.(*ThreadExpr)
	require.True(t, ok, "expected ThreadExpr inside the parens, got %T", ifStmt.Cond)
	require.Len(t, thread.Args, 2, "thread RHS should be jq + filter, not jq + filter + ')'")
}

func TestParse_ForEach_ParenthesisedThreadSource(t *testing.T) {
	t.Parallel()

	// foreach EXPR accepts a parenthesised thread expression so
	// the array-shaping pipeline can sit at the call site without
	// requiring an intermediate let binding.
	prog, err := parseSource(t, "foreach x in ($xs |> jq \".items\") { print $x }")
	require.NoError(t, err)
	fe := firstStmt(t, prog).(*ForEachStmt)
	_, ok := fe.List.(*ThreadExpr)
	assert.True(t, ok, "foreach List should be a ThreadExpr (the parens are unwrapped), got %T", fe.List)
}

func TestParse_Thread_StopsAtLogicalOperator(t *testing.T) {
	t.Parallel()

	// "$x |> jq foo OP $y" should parse as the logical OP between
	// (thread $x |> jq foo) and ($y), not as a thread whose RHS
	// is "jq foo OP $y" (four args). Both 'and' and 'or' flow
	// through the same stop check; cover both so a regression on
	// either spelling fails loudly.
	cases := []string{"and", "or"}
	for _, op := range cases {
		t.Run(op, func(t *testing.T) {
			t.Parallel()
			src := fmt.Sprintf(`if $x |> jq "len" %s $y { print ok }`, op)
			prog, err := parseSource(t, src)
			require.NoError(t, err)
			ifStmt := firstStmt(t, prog).(*IfStmt)
			logical, ok := ifStmt.Cond.(*LogicalExpr)
			require.True(t, ok, "expected LogicalExpr at top, got %T", ifStmt.Cond)
			assert.Equal(t, op, logical.Op)
			thread, ok := logical.Left.(*ThreadExpr)
			require.True(t, ok, "expected ThreadExpr on the left, got %T", logical.Left)
			assert.Len(t, thread.Args, 2, "thread RHS should be jq + filter, not jq + filter + %q + ...", op)
		})
	}
}

func TestParse_Thread_RejectsTrailingThread(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "let r = $x |>")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "thread")
}

func TestParse_Thread_RejectsLeadingThread(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "let r = |> jq \"add\"")
	require.Error(t, err)
}

// --- foreach ------------------------------------------------------

func TestParse_ForEach_Basic(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "foreach p in $list { print p }")
	require.NoError(t, err)
	fe, ok := firstStmt(t, prog).(*ForEachStmt)
	require.True(t, ok, "expected ForEachStmt, got %T", prog.Stmts[0])
	assert.Equal(t, []string{"p"}, identTextList(fe.Names))
	ref, ok := fe.List.(*VarRefExpr)
	require.True(t, ok)
	assert.Equal(t, "list", ref.Name)
	require.Len(t, fe.Body, 1)
	_, ok = fe.Body[0].(*CommandStmt)
	assert.True(t, ok)
}

func TestParse_ForEach_ListFromPipe(t *testing.T) {
	t.Parallel()

	// Ensure the list expression can be an arbitrary expression,
	// including a threading pipeline like [bpfman ... ] |> jq "..."
	prog, err := parseSource(t, "foreach p in $raw |> jq \".items\" { print p }")
	require.NoError(t, err)
	fe, ok := firstStmt(t, prog).(*ForEachStmt)
	require.True(t, ok)
	_, ok = fe.List.(*ThreadExpr)
	assert.True(t, ok, "list expression should be a ThreadExpr, got %T", fe.List)
}

func TestParse_ForEach_MultiStatementBody(t *testing.T) {
	t.Parallel()

	input := "foreach p in $items {\n  let x = $p.name\n  print x\n}"
	prog, err := parseSource(t, input)
	require.NoError(t, err)
	fe, ok := firstStmt(t, prog).(*ForEachStmt)
	require.True(t, ok)
	require.Len(t, fe.Body, 2)
	_, ok = fe.Body[0].(*LetStmt)
	assert.True(t, ok)
	_, ok = fe.Body[1].(*CommandStmt)
	assert.True(t, ok)
}

func TestParse_ForEach_Nested(t *testing.T) {
	t.Parallel()

	input := "foreach a in $xs {\n  foreach b in $ys {\n    print b\n  }\n}"
	prog, err := parseSource(t, input)
	require.NoError(t, err)
	outer, ok := firstStmt(t, prog).(*ForEachStmt)
	require.True(t, ok)
	require.Len(t, outer.Body, 1)
	inner, ok := outer.Body[0].(*ForEachStmt)
	require.True(t, ok)
	assert.Equal(t, []string{"b"}, identTextList(inner.Names))
}

func TestParse_ForEach_Errors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"missing identifier", "foreach in $list { print x }", "foreach requires"},
		{"invalid identifier", "foreach 1bad in $list { print x }", "invalid variable name"},
		{"missing in", "foreach p $list { print x }", "foreach requires 'in'"},
		{"missing expression", "foreach p in { print x }", "foreach requires"},
		{"missing block", "foreach p in $list print x", "expected '{'"},
		{"unterminated block", "foreach p in $list { print x", "unterminated block"},
		{"all discard", "foreach (_ _) in $list { print x }", "at least one must bind"},
		{"comma rejected", "foreach a, b in $pairs { print a }", "comma is not a separator"},
		{"unparenthesised multi-var", "foreach a b in $pairs { print a }", "foreach requires 'in'"},
		{"single-name parens rejected", "foreach (x) in $list { print x }", "requires at least two names"},
		{"empty parens rejected", "foreach () in $list { print x }", "requires at least two names"},
		{"duplicate names rejected", "foreach (a a) in $pairs { print a }", "duplicate name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseSource(t, tc.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestParse_ForEach_MultiVar(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"two names", "foreach (a b) in $pairs { print a }", []string{"a", "b"}},
		{"three names", "foreach (a b c) in $triples { print a }", []string{"a", "b", "c"}},
		{"discard slot first", "foreach (_ b) in $pairs { print b }", []string{"_", "b"}},
		{"discard slot second", "foreach (a _) in $pairs { print a }", []string{"a", "_"}},
		{"discard middle", "foreach (a _ c) in $triples { print a }", []string{"a", "_", "c"}},
		{"name list wraps over newlines", "foreach (a\n  b\n  c) in $triples { print a }", []string{"a", "b", "c"}},
		{"single discard iterate-only", "foreach _ in $list { print hi }", []string{"_"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := parseSource(t, tc.input)
			require.NoError(t, err)
			fe := firstStmt(t, prog).(*ForEachStmt)
			assert.Equal(t, tc.want, identTextList(fe.Names))
		})
	}
}

func TestParse_Break_Simple(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "foreach x in $xs { break }")
	require.NoError(t, err)
	fe := firstStmt(t, prog).(*ForEachStmt)
	require.Len(t, fe.Body, 1)
	_, ok := fe.Body[0].(*BreakStmt)
	assert.True(t, ok, "expected BreakStmt, got %T", fe.Body[0])
}

func TestParse_Continue_Simple(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "foreach x in $xs { continue }")
	require.NoError(t, err)
	fe := firstStmt(t, prog).(*ForEachStmt)
	require.Len(t, fe.Body, 1)
	_, ok := fe.Body[0].(*ContinueStmt)
	assert.True(t, ok, "expected ContinueStmt, got %T", fe.Body[0])
}

func TestParse_Break_InsideIf(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "foreach x in $xs {\n  if $x == skip { break }\n  print x\n}")
	require.NoError(t, err)
	fe := firstStmt(t, prog).(*ForEachStmt)
	require.Len(t, fe.Body, 2)
	ifStmt, ok := fe.Body[0].(*IfStmt)
	require.True(t, ok)
	require.Len(t, ifStmt.Then, 1)
	_, ok = ifStmt.Then[0].(*BreakStmt)
	assert.True(t, ok)
}

func TestParse_Break_RejectsArguments(t *testing.T) {
	t.Parallel()

	// break and continue take no arguments -- a trailing token
	// is a parse-time error so "break 2"-style multi-level
	// escapes don't silently tokenise as a command.
	_, err := parseSource(t, "foreach x in $xs { break 2 }")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "break")
}

func TestParse_Continue_RejectsArguments(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "foreach x in $xs { continue now }")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "continue")
}

// --- logical operators + parens ------------------------------------

func TestParse_LogicalOr(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "if $a or $b { print ok }")
	require.NoError(t, err)
	ifStmt := firstStmt(t, prog).(*IfStmt)
	orExpr, ok := ifStmt.Cond.(*LogicalExpr)
	require.True(t, ok, "expected LogicalExpr, got %T", ifStmt.Cond)
	assert.Equal(t, "or", orExpr.Op)
}

func TestParse_LogicalAnd(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "if $a and $b { print ok }")
	require.NoError(t, err)
	ifStmt := firstStmt(t, prog).(*IfStmt)
	andExpr, ok := ifStmt.Cond.(*LogicalExpr)
	require.True(t, ok, "expected LogicalExpr, got %T", ifStmt.Cond)
	assert.Equal(t, "and", andExpr.Op)
}

func TestParse_LogicalNot(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "if not $a { print ok }")
	require.NoError(t, err)
	ifStmt := firstStmt(t, prog).(*IfStmt)
	notExpr, ok := ifStmt.Cond.(*NotExpr)
	require.True(t, ok, "expected NotExpr, got %T", ifStmt.Cond)
	_, ok = notExpr.Operand.(*VarRefExpr)
	assert.True(t, ok)
}

func TestParse_Logical_Precedence_AndTighterThanOr(t *testing.T) {
	t.Parallel()

	// "$a or $b and $c" should parse as "$a or ($b and $c)".
	prog, err := parseSource(t, "if $a or $b and $c { print ok }")
	require.NoError(t, err)
	ifStmt := firstStmt(t, prog).(*IfStmt)
	or, ok := ifStmt.Cond.(*LogicalExpr)
	require.True(t, ok)
	assert.Equal(t, "or", or.Op)
	rhs, ok := or.Right.(*LogicalExpr)
	require.True(t, ok, "or's right operand should be an 'and' expr, got %T", or.Right)
	assert.Equal(t, "and", rhs.Op)
}

func TestParse_Logical_Precedence_NotTighterThanAnd(t *testing.T) {
	t.Parallel()

	// "not $a and $b" should parse as "(not $a) and $b".
	prog, err := parseSource(t, "if not $a and $b { print ok }")
	require.NoError(t, err)
	ifStmt := firstStmt(t, prog).(*IfStmt)
	and, ok := ifStmt.Cond.(*LogicalExpr)
	require.True(t, ok)
	assert.Equal(t, "and", and.Op)
	_, ok = and.Left.(*NotExpr)
	assert.True(t, ok, "and's left operand should be 'not', got %T", and.Left)
}

func TestParse_Logical_Precedence_NotLooserThanComparison(t *testing.T) {
	t.Parallel()

	// "not $a == $b" should parse as "not ($a == $b)" per SQL /
	// Python convention, not "(not $a) == $b" per C.
	prog, err := parseSource(t, "if not $a == $b { print ok }")
	require.NoError(t, err)
	ifStmt := firstStmt(t, prog).(*IfStmt)
	notExpr, ok := ifStmt.Cond.(*NotExpr)
	require.True(t, ok, "top should be NotExpr, got %T", ifStmt.Cond)
	_, ok = notExpr.Operand.(*BinaryExpr)
	assert.True(t, ok, "not's operand should be a BinaryExpr, got %T", notExpr.Operand)
}

func TestParse_Logical_DoubleNot(t *testing.T) {
	t.Parallel()

	// "not not $a" parses via right-associative recursion as
	// NotExpr(NotExpr($a)).
	prog, err := parseSource(t, "if not not $a { print ok }")
	require.NoError(t, err)
	ifStmt := firstStmt(t, prog).(*IfStmt)
	outer, ok := ifStmt.Cond.(*NotExpr)
	require.True(t, ok)
	inner, ok := outer.Operand.(*NotExpr)
	require.True(t, ok)
	_, ok = inner.Operand.(*VarRefExpr)
	assert.True(t, ok)
}

func TestParse_Logical_ParensOverridePrecedence(t *testing.T) {
	t.Parallel()

	// "($a or $b) and $c" should parse with 'and' at the top.
	prog, err := parseSource(t, "if ($a or $b) and $c { print ok }")
	require.NoError(t, err)
	ifStmt := firstStmt(t, prog).(*IfStmt)
	and, ok := ifStmt.Cond.(*LogicalExpr)
	require.True(t, ok, "top should be 'and', got %T", ifStmt.Cond)
	assert.Equal(t, "and", and.Op)
	or, ok := and.Left.(*LogicalExpr)
	require.True(t, ok)
	assert.Equal(t, "or", or.Op)
}

func TestParse_Logical_PredBeforeCloseParen(t *testing.T) {
	t.Parallel()

	// "($a == true) and $b": the 'true' inside the parens is
	// on the RHS of a comparison, and the next token is ')' --
	// not an operand. operandFollowsPred must treat ')' as an
	// expression terminator so 'true' parses as a literal, not
	// a UnaryExpr that greedily eats the ')'.
	prog, err := parseSource(t, "if ($a == true) and $b { print ok }")
	require.NoError(t, err)
	ifStmt := firstStmt(t, prog).(*IfStmt)
	and, ok := ifStmt.Cond.(*LogicalExpr)
	require.True(t, ok, "top should be and, got %T", ifStmt.Cond)
	assert.Equal(t, "and", and.Op)
	_, ok = and.Left.(*BinaryExpr)
	assert.True(t, ok, "and.Left should be BinaryExpr from the parens, got %T", and.Left)
}

func TestParse_Logical_ParenthesisedPrimary(t *testing.T) {
	t.Parallel()

	// A single parenthesised expression is equivalent to the
	// inner expression at the same precedence; the AST has no
	// dedicated ParenExpr node.
	prog, err := parseSource(t, "if ($a) { print ok }")
	require.NoError(t, err)
	ifStmt := firstStmt(t, prog).(*IfStmt)
	_, ok := ifStmt.Cond.(*VarRefExpr)
	assert.True(t, ok, "expected VarRefExpr from parenthesised primary, got %T", ifStmt.Cond)
}

func TestParse_Logical_UnmatchedParen(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "if ($a or $b { print ok }")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "')'")
}

func TestParse_Logical_StrayCloseParen(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "if $a) { print ok }")
	require.Error(t, err)
}

// --- poll / retry --------------------------------------------------

func TestParse_Retry_StatementForms(t *testing.T) {
	t.Parallel()

	cases := []string{
		"retry",
		`retry "waiting"`,
		"retry unless $ok",
		`retry "waiting" unless $ok`,
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			prog, err := parseSource(t, src)
			require.NoError(t, err)
			_, ok := firstStmt(t, prog).(*RetryStmt)
			require.True(t, ok)
		})
	}
}

func TestParse_Poll_RequiresTimeoutAndEvery(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "poll timeout 5s { print ok }")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "poll requires 'every DUR'")
}

// --- arithmetic ----------------------------------------------------

func TestParse_Arithmetic_AdditivePrecedence(t *testing.T) {
	t.Parallel()

	// 1 + 2 * 3 should parse as 1 + (2 * 3): the '+' is at the
	// root, with the '*' nested inside its right operand.
	prog, err := parseSource(t, "let r = 1 + 2 * 3")
	require.NoError(t, err)
	let := firstStmt(t, prog).(*LetStmt)
	bin, ok := let.RHS.(*BinaryExpr)
	require.True(t, ok, "root should be BinaryExpr, got %T", let.RHS)
	assert.Equal(t, "+", bin.Op)
	right, ok := bin.Right.(*BinaryExpr)
	require.True(t, ok, "right operand should be BinaryExpr (*), got %T", bin.Right)
	assert.Equal(t, "*", right.Op)
}

func TestParse_Arithmetic_ParensOverridePrecedence(t *testing.T) {
	t.Parallel()

	// (1 + 2) * 3: parens force '+' inside the left operand of '*'.
	prog, err := parseSource(t, "let r = (1 + 2) * 3")
	require.NoError(t, err)
	let := firstStmt(t, prog).(*LetStmt)
	bin, ok := let.RHS.(*BinaryExpr)
	require.True(t, ok, "root should be BinaryExpr, got %T", let.RHS)
	assert.Equal(t, "*", bin.Op)
	left, ok := bin.Left.(*BinaryExpr)
	require.True(t, ok, "left operand should be BinaryExpr (+), got %T", bin.Left)
	assert.Equal(t, "+", left.Op)
}

func TestParse_Arithmetic_LeftAssociativeChain(t *testing.T) {
	t.Parallel()

	// 1 - 2 - 3 should parse as (1 - 2) - 3.
	prog, err := parseSource(t, "let r = 1 - 2 - 3")
	require.NoError(t, err)
	let := firstStmt(t, prog).(*LetStmt)
	outer, ok := let.RHS.(*BinaryExpr)
	require.True(t, ok)
	assert.Equal(t, "-", outer.Op)
	inner, ok := outer.Left.(*BinaryExpr)
	require.True(t, ok, "left-assoc: left operand should be BinaryExpr, got %T", outer.Left)
	assert.Equal(t, "-", inner.Op)
}

func TestParse_Arithmetic_LooserThanComparison(t *testing.T) {
	t.Parallel()

	// $x + 1 == 5: '==' at the root, additive as the left operand.
	prog, err := parseSource(t, "let r = $x + 1 == 5")
	require.NoError(t, err)
	let := firstStmt(t, prog).(*LetStmt)
	cmp, ok := let.RHS.(*BinaryExpr)
	require.True(t, ok)
	assert.Equal(t, "==", cmp.Op)
	add, ok := cmp.Left.(*BinaryExpr)
	require.True(t, ok, "comparison LHS should be BinaryExpr (additive), got %T", cmp.Left)
	assert.Equal(t, "+", add.Op)
}

func TestParse_Arithmetic_UnaryNegate_VarRef(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "let r = - $x")
	require.NoError(t, err)
	let := firstStmt(t, prog).(*LetStmt)
	neg, ok := let.RHS.(*NegateExpr)
	require.True(t, ok, "expected NegateExpr, got %T", let.RHS)
	_, ok = neg.Operand.(*VarRefExpr)
	assert.True(t, ok)
}

func TestParse_Arithmetic_UnaryNegate_ParenExpr(t *testing.T) {
	t.Parallel()

	// -(1 + 2) -- negation of a parenthesised additive expression.
	prog, err := parseSource(t, "let r = -(1 + 2)")
	require.NoError(t, err)
	let := firstStmt(t, prog).(*LetStmt)
	neg, ok := let.RHS.(*NegateExpr)
	require.True(t, ok, "expected NegateExpr, got %T", let.RHS)
	add, ok := neg.Operand.(*BinaryExpr)
	require.True(t, ok)
	assert.Equal(t, "+", add.Op)
}

func TestParse_Arithmetic_UnaryNegate_Stacked(t *testing.T) {
	t.Parallel()

	// - - 3 (with spaces) stacks two negations. "-3" alone
	// tokenises as a single WORD, so we force separation.
	prog, err := parseSource(t, "let r = - - $x")
	require.NoError(t, err)
	let := firstStmt(t, prog).(*LetStmt)
	outer, ok := let.RHS.(*NegateExpr)
	require.True(t, ok)
	inner, ok := outer.Operand.(*NegateExpr)
	require.True(t, ok, "stacked negation should nest, got %T", outer.Operand)
	_, ok = inner.Operand.(*VarRefExpr)
	assert.True(t, ok)
}

func TestParse_Arithmetic_NegativeLiteralUnchanged(t *testing.T) {
	t.Parallel()

	// A negative numeric literal with no space still tokenises
	// as a single WORD, not as "negate token + literal".
	prog, err := parseSource(t, "let r = -3")
	require.NoError(t, err)
	let := firstStmt(t, prog).(*LetStmt)
	lit, ok := let.RHS.(*LiteralExpr)
	require.True(t, ok, "expected LiteralExpr, got %T", let.RHS)
	assert.Equal(t, "-3", lit.Text)
}

func TestParse_Arithmetic_TrailingOperatorIsError(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "let r = $x +")
	require.Error(t, err)
}

func TestParse_Arithmetic_NoWhitespace_PlusStarPercent(t *testing.T) {
	t.Parallel()

	// The tokeniser splits '+', '*', and '%' even without
	// surrounding whitespace, so the compact forms that users
	// naturally type work identically to the spaced forms.
	cases := []struct {
		name string
		src  string
		op   string
	}{
		{"plus no space", "let r = $x+1", "+"},
		{"plus mixed space", "let r = $x +1", "+"},
		{"star no space", "let r = $x*2", "*"},
		{"percent no space", "let r = 7%3", "%"},
		{"literal plus literal", "let r = 1+1", "+"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := parseSource(t, tc.src)
			require.NoError(t, err)
			let := firstStmt(t, prog).(*LetStmt)
			bin, ok := let.RHS.(*BinaryExpr)
			require.True(t, ok, "RHS should be BinaryExpr, got %T", let.RHS)
			assert.Equal(t, tc.op, bin.Op)
		})
	}
}

func TestParse_Arithmetic_SmushedMinusHintsAtWhitespace(t *testing.T) {
	t.Parallel()

	// '-' and '/' stay word-interior (negative literals, flags,
	// paths), so "$x -1" still tokenises as "$x" + "-1" and
	// fails to parse. The error should point at whitespace
	// rather than the generic "commands belong on '<-'"
	// suggestion.
	_, err := parseSource(t, "let r = $x -1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "whitespace")
	assert.Contains(t, err.Error(), "'-'")
}

func TestParse_AllNodesHaveSourcePosition(t *testing.T) {
	t.Parallel()

	// Regression guard for the position-completeness
	// invariant: every AST node Parse produces must have
	// both line and column populated. A new AST variant
	// added without copying its source position would
	// silently surface as an empty source.Pos in user-facing
	// diagnostics; this test catches that at parse time.
	cases := []string{
		"let x = 1",
		"let r = 4 * 2 + 1",
		`print "${$n * 2}"`,
		"let p <- start sleep 60\nwait $p",
		"foreach x in $xs { print $x }",
		"if $x { let r = 1 } elif $y { let r = 2 } else { let r = 3 }",
		"poll timeout 30s every 250ms { let r <- foo\nretry unless $r.ok }",
		"def greet(name) { print $name }\ngreet alice",
		"defer kill $p",
		"assert $a == $b",
		"let z = $x |> jq tonumber",
		"assert $rec matches {\n    .name: \"foo\"\n    .id: 5\n}",
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			tokens, err := Tokenise(src)
			require.NoError(t, err)
			prog, err := Parse(tokens)
			require.NoError(t, err, "src=%q", src)
			Inspect(prog, func(n Node) bool {
				if n == nil {
					return true
				}
				if p, ok := n.(*Program); ok && len(p.Stmts) == 0 {
					return true
				}
				sp := NodeSpan(n)
				assert.Greater(t, sp.Pos.Line, 0, "%T missing line", n)
				assert.Greater(t, sp.Pos.Col, 0, "%T missing col", n)
				return true
			})
		})
	}
}

func TestParse_EmptyProgramAccepted(t *testing.T) {
	t.Parallel()

	// validateLocs skips the empty-program case; an empty
	// input is a valid parse with an empty source.Pos and must not
	// be rejected as 'missing source position'.
	prog, err := parseSource(t, "")
	require.NoError(t, err)
	require.NotNil(t, prog)
	assert.Empty(t, prog.Stmts)
}

func TestValidateLocs_FailsOnDeliberatelyBrokenNode(t *testing.T) {
	t.Parallel()

	// Confirm the invariant has teeth: a hand-built program
	// whose statement carries a zero source.Pos is rejected with the
	// internal-error message. If a future AST variant lands
	// without copying its source position, this is the shape
	// the failure takes.
	prog := &Program{
		Stmts: []Stmt{
			&LetStmt{Name: Ident{Text: "x"}, RHS: &LiteralExpr{Text: "1", Span: source.Span{Pos: source.Pos{Line: 1, Col: 9}}}},
		},
		Span: source.Span{Pos: source.Pos{Line: 1, Col: 1}},
	}
	err := validateLocs(prog)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "incomplete source spans")
	assert.Contains(t, err.Error(), "LetStmt")
}

func TestParse_Arithmetic_SmushedSlashHintsAtWhitespace(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "let r = $x /2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "whitespace")
	assert.Contains(t, err.Error(), "'/'")
}

func TestParseInterpBody_BareNameShortcut(t *testing.T) {
	t.Parallel()

	// "${name}" is a variable reference shortcut: the user
	// writes the bare identifier rather than $-prefixing.
	expr, err := parseInterpBody("name", source.Span{})
	require.NoError(t, err)
	v, ok := expr.(*VarRefExpr)
	require.True(t, ok, "expected VarRefExpr, got %T", expr)
	assert.Equal(t, "name", v.Name)
	assert.Empty(t, v.Path)
}

func TestParseInterpBody_BareNameWithPath(t *testing.T) {
	t.Parallel()

	expr, err := parseInterpBody("rec.field", source.Span{})
	require.NoError(t, err)
	v, ok := expr.(*VarRefExpr)
	require.True(t, ok, "expected VarRefExpr, got %T", expr)
	assert.Equal(t, "rec", v.Name)
	assert.Equal(t, "field", v.Path)
}

func TestParseInterpBody_SigilLedExpression(t *testing.T) {
	t.Parallel()

	// "${$n * 2}" is the sigil-led expression form: bash's
	// $((...)) shape transposed to ${...}. The parser
	// treats the whole body as an expression.
	expr, err := parseInterpBody("$n * 2", source.Span{})
	require.NoError(t, err)
	_, ok := expr.(*BinaryExpr)
	require.True(t, ok, "expected BinaryExpr, got %T", expr)
}

func TestParseInterpBody_LiteralLedExpression(t *testing.T) {
	t.Parallel()

	// "${4 * 2}" is the literal-led expression form: useful
	// for inline arithmetic in command args without a named
	// intermediate.
	expr, err := parseInterpBody("4 * 2", source.Span{})
	require.NoError(t, err)
	_, ok := expr.(*BinaryExpr)
	require.True(t, ok, "expected BinaryExpr, got %T", expr)
}

func TestParseInterpBody_ComplexLiteralLedExpression(t *testing.T) {
	t.Parallel()

	// "${(1 + 2) * 3}" exercises the parser's grouping path
	// from a literal-led starting position.
	expr, err := parseInterpBody("(1 + 2) * 3", source.Span{})
	require.NoError(t, err)
	_, ok := expr.(*BinaryExpr)
	require.True(t, ok, "expected BinaryExpr, got %T", expr)
}

func TestParseInterpBody_PreservesAbsoluteSourcePosition(t *testing.T) {
	t.Parallel()

	span := source.Span{
		Pos: source.Pos{File: "main.bpfman", Line: 12, Col: 5},
		End: source.Pos{File: "main.bpfman", Line: 12, Col: 15},
	}
	expr, err := parseInterpBody("4 * 2", span)
	require.NoError(t, err)
	assert.Equal(t, source.Span{
		Pos: source.Pos{File: "main.bpfman", Line: 12, Col: 7},
		End: source.Pos{File: "main.bpfman", Line: 12, Col: 12},
	}, NodeSpan(expr))
}

func TestParseInterpBody_EmptyIsError(t *testing.T) {
	t.Parallel()

	_, err := parseInterpBody("", source.Span{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestParseInterpBody_GarbageIsError(t *testing.T) {
	t.Parallel()

	// Tokens that do not form a valid expression must error
	// rather than silently produce a malformed Expr.
	_, err := parseInterpBody(") +", source.Span{})
	require.Error(t, err)
}

func TestParse_EmptyParens_UniformMessage(t *testing.T) {
	t.Parallel()

	// '()' in any expression position (let RHS, paren arg, inside a
	// pure-call arg, assert operand) must surface the same
	// "empty parenthesised expression" message rather than a
	// misleading "missing ')'" when the closing paren is right
	// there.
	cases := []struct {
		name  string
		input string
	}{
		{"let RHS", "let n = ()"},
		{"pure-call arg", `let n = jq "." ()`},
		{"assert operand", "assert () == 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseSource(t, tc.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "empty parenthesised expression")
		})
	}
}

func TestParse_PureCallArg_ListLiteral(t *testing.T) {
	t.Parallel()

	// A pure-builtin call's argument grammar must accept a list
	// literal as a primary.
	//
	// 'jq "." [1 2 3]' is the smallest exercise: jq takes 2 args,
	// arg 0 is the filter, arg 1 should be the whole list literal.
	prog, err := parseSource(t, `let v = jq "." [1 2 3]`)
	require.NoError(t, err)
	let, ok := firstStmt(t, prog).(*LetStmt)
	require.True(t, ok)
	call, ok := let.RHS.(*PureCallExpr)
	require.True(t, ok, "RHS should be PureCallExpr, got %T", let.RHS)
	require.Len(t, call.Args, 2)
	_, ok = call.Args[1].(*ListExpr)
	assert.True(t, ok, "arg 1 should be ListExpr, got %T", call.Args[1])
}

func TestParse_CommandArg_ParenExprThread(t *testing.T) {
	t.Parallel()

	// 'print ($x |> jq ...)' must produce a print command whose
	// sole argument is a ThreadExpr -- the inner '|>' reaches the
	// expression parser via the new arg-form, rather than tokens
	// in the run leaking out as literal '(' / ')' surrounding
	// orphaned varref / quoted text.
	prog, err := parseSource(t, `print ($x |> jq ".y")`)
	require.NoError(t, err)
	cmd, ok := firstStmt(t, prog).(*CommandStmt)
	require.True(t, ok)
	require.Len(t, cmd.Args, 2)
	lit, ok := cmd.Args[0].(*LiteralExpr)
	require.True(t, ok)
	assert.Equal(t, "print", lit.Text)
	_, ok = cmd.Args[1].(*ThreadExpr)
	assert.True(t, ok, "arg should be ThreadExpr, got %T", cmd.Args[1])
}

func TestParse_CommandArg_ParenExprPureCall(t *testing.T) {
	t.Parallel()

	// jq is registered by the shell package itself and has arity
	// 2, so the call form below matches the registered shape.
	prog, err := parseSource(t, `print (jq "." 42)`)
	require.NoError(t, err)
	cmd, ok := firstStmt(t, prog).(*CommandStmt)
	require.True(t, ok)
	require.Len(t, cmd.Args, 2)
	_, ok = cmd.Args[1].(*PureCallExpr)
	assert.True(t, ok, "arg should be PureCallExpr, got %T", cmd.Args[1])
}

func TestParse_CommandArg_ParenExprNested(t *testing.T) {
	t.Parallel()

	// Nested parens (a pure-builtin call inside the parenthesised
	// arg) must balance correctly so the outer ')' is the matching
	// close, not the inner one.
	prog, err := parseSource(t, `print (jq "." (jq ".x" 42))`)
	require.NoError(t, err)
	cmd, ok := firstStmt(t, prog).(*CommandStmt)
	require.True(t, ok)
	require.Len(t, cmd.Args, 2)
	_, ok = cmd.Args[1].(*PureCallExpr)
	assert.True(t, ok)
}

func TestParse_CommandArg_ParenExprArithmetic(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, `print ($x + 1)`)
	require.NoError(t, err)
	cmd, ok := firstStmt(t, prog).(*CommandStmt)
	require.True(t, ok)
	require.Len(t, cmd.Args, 2)
	_, ok = cmd.Args[1].(*BinaryExpr)
	assert.True(t, ok, "arg should be BinaryExpr, got %T", cmd.Args[1])
}

func TestParse_CommandArg_ListLiteral(t *testing.T) {
	t.Parallel()

	// '[' at the start of an argument run must route to
	// parseListLiteral so 'print [1 2 3]' produces one ListExpr
	// argument rather than five separate literal tokens for '[',
	// '1', '2', '3', ']'.
	prog, err := parseSource(t, `print [1 2 3]`)
	require.NoError(t, err)
	cmd, ok := firstStmt(t, prog).(*CommandStmt)
	require.True(t, ok)
	require.Len(t, cmd.Args, 2)
	list, ok := cmd.Args[1].(*ListExpr)
	require.True(t, ok, "arg 1 should be ListExpr, got %T", cmd.Args[1])
	assert.Len(t, list.Elems, 3)
}

func TestParse_CommandArg_NestedListLiteral(t *testing.T) {
	t.Parallel()

	// Nested brackets must balance via findMatchingBracket so the
	// outer ']' closes the outer literal rather than the inner.
	// '[[1] [2 3]]' parses as a two-element list of lists.
	prog, err := parseSource(t, `print [[1] [2 3]]`)
	require.NoError(t, err)
	cmd, ok := firstStmt(t, prog).(*CommandStmt)
	require.True(t, ok)
	require.Len(t, cmd.Args, 2)
	outer, ok := cmd.Args[1].(*ListExpr)
	require.True(t, ok)
	assert.Len(t, outer.Elems, 2)
	for i, e := range outer.Elems {
		_, ok := e.(*ListExpr)
		assert.True(t, ok, "elem %d should be ListExpr, got %T", i, e)
	}
}

func TestParse_CommandArg_ListLiteralErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"unmatched open bracket", "print [1 2", "unmatched '['"},
		{"stray close bracket", "print abc]", "unmatched ']'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseSource(t, tc.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestParse_CommandArg_ParenExprErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"empty parens", "print ()", "empty parenthesised"},
		{"unmatched open", "print (foo", "unmatched"},
		{"stray close", "print abc)", "unmatched ')'"},
		{"stray close after expr", "print (1 + 2))", "unmatched ')'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseSource(t, tc.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestParse_DefParamAnnotations(t *testing.T) {
	t.Parallel()

	t.Run("annotated and unannotated params mix", func(t *testing.T) {
		t.Parallel()
		prog, err := parseSource(t, "def f(a: number b c: string d: bool) { print $a }")
		require.NoError(t, err)
		def := prog.Stmts[0].(*DefStmt)
		require.Len(t, def.Params, 4)
		assert.Equal(t, "a", def.Params[0].Name.Text)
		assert.Equal(t, "number", def.Params[0].Type)
		assert.Equal(t, "b", def.Params[1].Name.Text)
		assert.Equal(t, "", def.Params[1].Type)
		assert.Equal(t, "c", def.Params[2].Name.Text)
		assert.Equal(t, "string", def.Params[2].Type)
		assert.Equal(t, "d", def.Params[3].Name.Text)
		assert.Equal(t, "bool", def.Params[3].Type)
	})

	t.Run("unknown type rejected with allowed list", func(t *testing.T) {
		t.Parallel()
		_, err := parseSource(t, "def f(a: integer) { print $a }")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown parameter type "integer"`)
		assert.Contains(t, err.Error(), "number, string, bool")
	})

	t.Run("annotation missing type", func(t *testing.T) {
		t.Parallel()
		_, err := parseSource(t, "def f(a:) { print $a }")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `parameter "a": annotation requires a type`)
	})

	t.Run("glued colon rejected", func(t *testing.T) {
		t.Parallel()
		_, err := parseSource(t, "def f(a:number) { print $a }")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "write it as \"a: number\"")
	})

	t.Run("duplicate names still rejected when annotated", func(t *testing.T) {
		t.Parallel()
		_, err := parseSource(t, "def f(a: number a: string) { print $a }")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate parameter name")
	})
}

func TestIsJSONNumber(t *testing.T) {
	t.Parallel()

	accepted := []string{"0", "5", "-5", "3.14", "1e3", "1E-2", "4026531840"}
	for _, s := range accepted {
		assert.True(t, IsJSONNumber(s), "%q must be a JSON number", s)
	}
	rejected := []string{"NaN", "Inf", "+Inf", "-Inf", "Infinity", "+5", "05", "0x10", "1e309", "1 2", "", "abc", "1.10.3"}
	for _, s := range rejected {
		assert.False(t, IsJSONNumber(s), "%q must not be a JSON number", s)
	}
}

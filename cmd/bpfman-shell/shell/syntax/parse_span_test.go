package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

func assertSpanEqual(t *testing.T, got source.Span, want source.Span) {
	t.Helper()
	assert.Equal(t, want, got)
}

func parseSingleStmt(t *testing.T, src string) Stmt {
	t.Helper()
	prog, err := parseSource(t, src)
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 1)
	return prog.Stmts[0]
}

func parseLetRHSExpr(t *testing.T, rhs string) (*LetStmt, Expr) {
	t.Helper()
	stmt := parseSingleStmt(t, "let x = "+rhs)
	letStmt := stmt.(*LetStmt)
	return letStmt, letStmt.RHS
}

func TestParseSpans_LetAndLiteral(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "let x = 42")
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 1)

	letStmt := prog.Stmts[0].(*LetStmt)
	lit := letStmt.RHS.(*LiteralExpr)

	assertSpanEqual(t, letStmt.Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 1, Col: 11}})
	assertSpanEqual(t, lit.Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 11}})
}

func TestParseSpans_BinaryExpr(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "let y = 1 + 23")
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 1)

	letStmt := prog.Stmts[0].(*LetStmt)
	bin := letStmt.RHS.(*BinaryExpr)

	assertSpanEqual(t, bin.Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 15}})
	assertSpanEqual(t, bin.Left.(*LiteralExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 10}})
	assertSpanEqual(t, bin.Right.(*LiteralExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 13}, End: source.Pos{Line: 1, Col: 15}})
}

func TestParseSpans_InterpStringExpr(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, `let s = "v=${$n * 2}"`)
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 1)

	letStmt := prog.Stmts[0].(*LetStmt)
	is := letStmt.RHS.(*InterpStringExpr)

	assertSpanEqual(t, is.Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 22}})
	require.Len(t, is.Segments, 2)
	assert.Equal(t, "v=", is.Segments[0].Literal)

	bin := is.Segments[1].Expr.(*BinaryExpr)
	assertSpanEqual(t, bin.Span, source.Span{Pos: source.Pos{Line: 1, Col: 14}, End: source.Pos{Line: 1, Col: 20}})
}

func TestParseSpans_LogicalAndThreadExpr(t *testing.T) {
	t.Parallel()

	_, expr := parseLetRHSExpr(t, "$a and $b or $c")
	orExpr := expr.(*LogicalExpr)
	andExpr := orExpr.Left.(*LogicalExpr)

	assertSpanEqual(t, orExpr.Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 24}})
	assertSpanEqual(t, andExpr.Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 18}})
	assertSpanEqual(t, andExpr.Left.(*VarRefExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 11}})
	assertSpanEqual(t, andExpr.Right.(*VarRefExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 16}, End: source.Pos{Line: 1, Col: 18}})
	assertSpanEqual(t, orExpr.Right.(*VarRefExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 22}, End: source.Pos{Line: 1, Col: 24}})

	_, expr = parseLetRHSExpr(t, "$x |> jq .id")
	thread := expr.(*ThreadExpr)

	assertSpanEqual(t, thread.Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 21}})
	assert.Equal(t, source.Pos{Line: 1, Col: 12}, thread.PipePos)
	assertSpanEqual(t, thread.LHS.(*VarRefExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 11}})
	require.Len(t, thread.Args, 2)
	assertSpanEqual(t, thread.Args[0].(*LiteralExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 15}, End: source.Pos{Line: 1, Col: 17}})
	assertSpanEqual(t, thread.Args[1].(*LiteralExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 18}, End: source.Pos{Line: 1, Col: 21}})
}

func TestParseSpans_PrefixAndAtomExprs(t *testing.T) {
	t.Parallel()

	_, expr := parseLetRHSExpr(t, "$prog.id")
	assertSpanEqual(t, expr.(*VarRefExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 17}})

	_, expr = parseLetRHSExpr(t, "file:$tmp.path")
	assertSpanEqual(t, expr.(*AdapterExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 23}})

	_, expr = parseLetRHSExpr(t, "not-empty $v")
	unary := expr.(*UnaryExpr)
	assertSpanEqual(t, unary.Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 21}})
	assertSpanEqual(t, unary.Operand.(*VarRefExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 19}, End: source.Pos{Line: 1, Col: 21}})

	_, expr = parseLetRHSExpr(t, "not $v")
	notExpr := expr.(*NotExpr)
	assertSpanEqual(t, notExpr.Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 15}})

	_, expr = parseLetRHSExpr(t, "- $v")
	neg := expr.(*NegateExpr)
	assertSpanEqual(t, neg.Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 13}})

	_, expr = parseLetRHSExpr(t, "range 5")
	pure := expr.(*PureCallExpr)
	assertSpanEqual(t, pure.Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 16}})
	assertSpanEqual(t, pure.Args[0].(*LiteralExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 15}, End: source.Pos{Line: 1, Col: 16}})

	_, expr = parseLetRHSExpr(t, "[1 2 ($n + 1)]")
	list := expr.(*ListExpr)
	assertSpanEqual(t, list.Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 23}})
	require.Len(t, list.Elems, 3)
	assertSpanEqual(t, list.Elems[0].(*LiteralExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 10}, End: source.Pos{Line: 1, Col: 11}})
	assertSpanEqual(t, list.Elems[1].(*LiteralExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 12}, End: source.Pos{Line: 1, Col: 13}})
	inner := list.Elems[2].(*BinaryExpr)
	assertSpanEqual(t, inner.Span, source.Span{Pos: source.Pos{Line: 1, Col: 15}, End: source.Pos{Line: 1, Col: 21}})
}

func TestParseSpans_CommandStmt(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, `print "hi"`)
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 1)

	cmd := prog.Stmts[0].(*CommandStmt)

	assertSpanEqual(t, cmd.Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 1, Col: 11}})
	assertSpanEqual(t, cmd.Args[0].(*LiteralExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 1, Col: 6}})
	assertSpanEqual(t, cmd.Args[1].(*LiteralExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 7}, End: source.Pos{Line: 1, Col: 11}})
}

func TestParseSpans_IfAndNestedCommand(t *testing.T) {
	t.Parallel()

	src := "if $x {\n  print yes\n}"
	prog, err := parseSource(t, src)
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 1)

	ifStmt := prog.Stmts[0].(*IfStmt)
	cmd := ifStmt.Then[0].(*CommandStmt)

	assertSpanEqual(t, ifStmt.Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 3, Col: 2}})
	assertSpanEqual(t, cmd.Span, source.Span{Pos: source.Pos{Line: 2, Col: 3}, End: source.Pos{Line: 2, Col: 12}})
}

func TestParseSpans_IfStmtEndsAtClosingBrace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		src        string
		want       source.Pos
		wantElif   source.Pos
		wantNested source.Pos
	}{
		{
			name: "plain",
			src:  "if $x {\n  print yes\n}\nprint after",
			want: source.Pos{Line: 3, Col: 2},
		},
		{
			name: "eof-after-closing-brace",
			src:  "if $x {\n  print yes\n}",
			want: source.Pos{Line: 3, Col: 2},
		},
		{
			name: "comment-after-closing-brace",
			src:  "if $x {\n  print yes\n}\n\n# top-level\nprint after",
			want: source.Pos{Line: 3, Col: 2},
		},
		{
			name:     "elif",
			src:      "if $x {\n  print x\n}\nelif $y {\n  print y\n}\nprint after",
			want:     source.Pos{Line: 6, Col: 2},
			wantElif: source.Pos{Line: 6, Col: 2},
		},
		{
			name: "else",
			src:  "if $x {\n  print x\n}\nelse {\n  print y\n}\nprint after",
			want: source.Pos{Line: 6, Col: 2},
		},
		{
			name:     "elif-else",
			src:      "if $x {\n  print x\n}\nelif $y {\n  print y\n}\nelse {\n  print z\n}\nprint after",
			want:     source.Pos{Line: 9, Col: 2},
			wantElif: source.Pos{Line: 6, Col: 2},
		},
		{
			name:       "nested",
			src:        "if $outer {\n  if $inner {\n    print inner\n  }\n}\nprint after",
			want:       source.Pos{Line: 5, Col: 2},
			wantNested: source.Pos{Line: 4, Col: 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prog, err := parseSource(t, tt.src)
			require.NoError(t, err)
			require.NotEmpty(t, prog.Stmts)

			ifStmt := prog.Stmts[0].(*IfStmt)
			assert.Equal(t, tt.want, ifStmt.Span.End)
			if tt.wantElif.Line != 0 {
				require.NotEmpty(t, ifStmt.Elifs)
				assert.Equal(t, tt.wantElif, ifStmt.Elifs[0].Span.End)
			}
			if tt.wantNested.Line != 0 {
				nested := ifStmt.Then[0].(*IfStmt)
				assert.Equal(t, tt.wantNested, nested.Span.End)
			}
		})
	}
}

func TestParseSpans_ForEachAndContinue(t *testing.T) {
	t.Parallel()

	src := "foreach x in $xs {\n  continue\n}"
	prog, err := parseSource(t, src)
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 1)

	fe := prog.Stmts[0].(*ForEachStmt)
	cont := fe.Body[0].(*ContinueStmt)

	assertSpanEqual(t, fe.Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 3, Col: 2}})
	assertSpanEqual(t, cont.Span, source.Span{Pos: source.Pos{Line: 2, Col: 3}, End: source.Pos{Line: 2, Col: 11}})
}

func TestParseSpans_LetDestructureAndBindForms(t *testing.T) {
	t.Parallel()

	stmt := parseSingleStmt(t, "let (a b) = $pair")
	destruct := stmt.(*LetDestructureStmt)
	assertSpanEqual(t, destruct.Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 1, Col: 18}})
	assertSpanEqual(t, destruct.RHS.(*VarRefExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 13}, End: source.Pos{Line: 1, Col: 18}})

	stmt = parseSingleStmt(t, "let x <- echo hi")
	bind := stmt.(*BindStmt)
	assertSpanEqual(t, bind.Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 1, Col: 17}})
	assertSpanEqual(t, bind.Cmd.Span, source.Span{Pos: source.Pos{Line: 1, Col: 10}, End: source.Pos{Line: 1, Col: 17}})

	stmt = parseSingleStmt(t, "let xs <- foreach x in $src {\n  echo $x\n}")
	bind = stmt.(*BindStmt)
	require.NotNil(t, bind.Collect)
	assertSpanEqual(t, bind.Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 3, Col: 2}})
	assertSpanEqual(t, bind.Collect.Span, source.Span{Pos: source.Pos{Line: 1, Col: 11}, End: source.Pos{Line: 3, Col: 2}})
	assertSpanEqual(t, bind.Collect.Body[0].(*CommandStmt).Span, source.Span{Pos: source.Pos{Line: 2, Col: 3}, End: source.Pos{Line: 2, Col: 10}})

}

func TestParseSpans_StatementForms(t *testing.T) {
	t.Parallel()

	stmt := parseSingleStmt(t, `'hi'`)
	exprStmt := stmt.(*ExprStmt)
	assertSpanEqual(t, exprStmt.Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 1, Col: 5}})
	assertSpanEqual(t, exprStmt.Expr.(*LiteralExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 1, Col: 5}})

	stmt = parseSingleStmt(t, "require $ok")
	requireStmt := stmt.(*AssertStmt)
	assertSpanEqual(t, requireStmt.Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 1, Col: 12}})
	reqClause := requireStmt.Clause.(*AssertExprClause)
	assertSpanEqual(t, reqClause.Expr.(*VarRefExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 9}, End: source.Pos{Line: 1, Col: 12}})

	stmt = parseSingleStmt(t, "defer echo bye")
	deferStmt := stmt.(*DeferStmt)
	assertSpanEqual(t, deferStmt.Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 1, Col: 15}})
	assertSpanEqual(t, deferStmt.Cmd.Span, source.Span{Pos: source.Pos{Line: 1, Col: 7}, End: source.Pos{Line: 1, Col: 15}})

	stmt = parseSingleStmt(t, "def f(x) {\n  return $x\n}")
	defStmt := stmt.(*DefStmt)
	ret := defStmt.Body[0].(*ReturnStmt)
	assertSpanEqual(t, defStmt.Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 3, Col: 2}})
	assertSpanEqual(t, ret.Span, source.Span{Pos: source.Pos{Line: 2, Col: 3}, End: source.Pos{Line: 2, Col: 12}})

	stmt = parseSingleStmt(t, "if $a { echo a } elif $b { echo b } else { echo c }")
	ifStmt := stmt.(*IfStmt)
	assertSpanEqual(t, ifStmt.Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 1, Col: 52}})
	require.Len(t, ifStmt.Elifs, 1)
	assertSpanEqual(t, ifStmt.Elifs[0].Span, source.Span{Pos: source.Pos{Line: 1, Col: 18}, End: source.Pos{Line: 1, Col: 36}})
	assertSpanEqual(t, ifStmt.Else[0].(*CommandStmt).Span, source.Span{Pos: source.Pos{Line: 1, Col: 44}, End: source.Pos{Line: 1, Col: 50}})

	stmt = parseSingleStmt(t, "poll timeout 1s every 10ms {\n  echo ok\n}")
	pollStmt := stmt.(*PollStmt)
	assertSpanEqual(t, pollStmt.Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 3, Col: 2}})
	assertSpanEqual(t, pollStmt.Body[0].(*CommandStmt).Span, source.Span{Pos: source.Pos{Line: 2, Col: 3}, End: source.Pos{Line: 2, Col: 10}})

	stmt = parseSingleStmt(t, `retry "waiting" unless $ok`)
	retryStmt := stmt.(*RetryStmt)
	assertSpanEqual(t, retryStmt.Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 1, Col: 27}})
}

func TestParseSpans_BreakAndMatchesBlock(t *testing.T) {
	t.Parallel()

	stmt := parseSingleStmt(t, "foreach x in $xs {\n  break\n}")
	fe := stmt.(*ForEachStmt)
	brk := fe.Body[0].(*BreakStmt)
	assertSpanEqual(t, brk.Span, source.Span{Pos: source.Pos{Line: 2, Col: 3}, End: source.Pos{Line: 2, Col: 8}})

	stmt2, matches := parseMatchesAssert(t, `assert $p matches { a.b: x }`)
	assertSpanEqual(t, stmt2.Span, source.Span{Pos: source.Pos{Line: 1, Col: 1}, End: source.Pos{Line: 1, Col: 29}})
	block := matches.Block
	assertSpanEqual(t, block.Span, source.Span{Pos: source.Pos{Line: 1, Col: 19}, End: source.Pos{Line: 1, Col: 29}})
	require.Len(t, block.Entries, 1)
	assertSpanEqual(t, block.Entries[0].Span, source.Span{Pos: source.Pos{Line: 1, Col: 21}, End: source.Pos{Line: 1, Col: 27}})
	assertSpanEqual(t, block.Entries[0].Pattern.(*LiteralExpr).Span, source.Span{Pos: source.Pos{Line: 1, Col: 26}, End: source.Pos{Line: 1, Col: 27}})
}

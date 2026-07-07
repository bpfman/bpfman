package syntax

import "testing"

func TestFormatExprSource_AllExpressionFormsRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
	}{
		{"literal", `42`},
		{"quoted literal", `"hello"`},
		{"var ref", `$prog.record.id`},
		{"indexed var ref", `$items[0].name`},
		{"adapter", `file:$path.name`},
		{"interpolation", `"hello-${name}"`},
		{"binary", `$x + 1`},
		{"unary", `not-empty $x`},
		{"thread", `$src |> jq ".id"`},
		{"logical", `$a and not $b`},
		{"not", `not null $x`},
		{"negate", `-$x`},
		{"pure call", `zip [a b] [1 2]`},
		{"matches", `$prog matches { id: $want }`},
		{"list", `[1 ($x + 1) (not null $y)]`},
		{"record", `record { prog: $prog link: $link total: ($sum + 1) }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			expr := parseLetRHSExprForFormat(t, tc.src)
			formatted := FormatExprSource(expr)
			if _, err := parseSource(t, "let got = "+formatted); err != nil {
				t.Fatalf("reparse formatted expression %q as %q: %v", formatted, tc.src, err)
			}
		})
	}
}

func TestFormatExprSource_ComplexForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr Expr
		want string
	}{
		{
			name: "interpolation",
			expr: &InterpStringExpr{
				Segments: []InterpStringSegment{
					{Literal: "hello-"},
					{Expr: &VarRefExpr{Name: "name"}},
					{Literal: "-"},
					{Expr: &BinaryExpr{
						Left:  &VarRefExpr{Name: "lhs"},
						Op:    "+",
						Right: &VarRefExpr{Name: "rhs"},
					}},
				},
			},
			want: `"hello-${name}-${$lhs + $rhs}"`,
		},
		{
			name: "thread",
			expr: &ThreadExpr{
				LHS: &VarRefExpr{Name: "src"},
				Args: []Expr{
					&LiteralExpr{Text: "jq"},
					&LiteralExpr{Text: ".id", Quoted: true},
				},
			},
			want: `$src |> jq ".id"`,
		},
		{
			name: "matches",
			expr: &MatchesExpr{
				Target: &VarRefExpr{Name: "src"},
				Block: &MatchesBlockExpr{
					Exhaustive: true,
					Entries: []MatchEntry{
						{
							Path:    "status.id",
							Pattern: &VarRefExpr{Name: "want"},
						},
						{
							Path: "status.meta",
							SubBlock: &MatchesBlockExpr{
								Entries: []MatchEntry{{
									Path:    "name",
									Pattern: &LiteralExpr{Text: "demo", Quoted: true},
								}},
							},
						},
					},
				},
			},
			want: "$src matches exhaustive { status.id: $want\n status.meta: matches { name: \"demo\" } }",
		},
	}

	for _, tc := range tests {
		if got := FormatExprSource(tc.expr); got != tc.want {
			t.Fatalf("%s: FormatExprSource() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func parseLetRHSExprForFormat(t *testing.T, src string) Expr {
	t.Helper()
	prog, err := parseSource(t, "let got = "+src)
	if err != nil {
		t.Fatalf("parse expression %q: %v", src, err)
	}

	let, ok := prog.Stmts[0].(*LetStmt)
	if !ok {
		t.Fatalf("statement = %T, want *LetStmt", prog.Stmts[0])
	}

	return let.RHS
}

func TestFormatExprSource_MatchesRoundTripsThroughParser(t *testing.T) {
	t.Parallel()

	// Rendering a matches block from the AST and feeding it
	// back through the parser must succeed: the parser
	// explicitly rejects commas inside a matches block, so the
	// printed form must not emit them ("matches: ',' is not a
	// valid entry separator; entries are separated by
	// newlines"). The format-then-reparse loop is the
	// load-bearing contract here -- diagnostics and developer
	// tooling that round-trip the printed form rely on it being
	// a re-parseable string.
	expr := &MatchesExpr{
		Target: &VarRefExpr{Name: "src"},
		Block: &MatchesBlockExpr{
			Entries: []MatchEntry{
				{Path: "id", Pattern: &VarRefExpr{Name: "want"}},
				{Path: "name", Pattern: &LiteralExpr{Text: "demo", Quoted: true}},
			},
		},
	}
	src := "let r = " + FormatExprSource(expr)
	tokens, err := Tokenise(src)
	if err != nil {
		t.Fatalf("tokenise reformatted source %q: %v", src, err)
	}

	if _, err := Parse(tokens); err != nil {
		t.Fatalf("reparse reformatted source %q: %v", src, err)
	}
}

func TestFormatExprSource_CompoundPureCallArgsRoundTrip(t *testing.T) {
	t.Parallel()

	expr := &PureCallExpr{
		Name: "jq",
		Args: []Expr{
			&LiteralExpr{Text: ".", Quoted: true},
			&BinaryExpr{
				Left:  &VarRefExpr{Name: "x"},
				Op:    "+",
				Right: &LiteralExpr{Text: "1"},
			},
		},
	}
	src := "let r = " + FormatExprSource(expr)
	tokens, err := Tokenise(src)
	if err != nil {
		t.Fatalf("tokenise reformatted source %q: %v", src, err)
	}

	if _, err := Parse(tokens); err != nil {
		t.Fatalf("reparse reformatted source %q: %v", src, err)
	}

	if want := `jq "." ($x + 1)`; FormatExprSource(expr) != want {
		t.Fatalf("FormatExprSource() = %q, want %q", FormatExprSource(expr), want)
	}
}

func TestFormatExprSource_QuotedDollarRoundTrips(t *testing.T) {
	t.Parallel()

	expr := &LiteralExpr{Text: `printf "not an elf" > "$1"`, Quoted: true}
	got := FormatExprSource(expr)
	want := `'printf "not an elf" > "$1"'`
	if got != want {
		t.Fatalf("FormatExprSource() = %q, want %q", got, want)
	}
	tokens, err := Tokenise(got)
	if err != nil {
		t.Fatalf("tokenise reformatted quoted literal %q: %v", got, err)
	}

	if len(tokens) != 1 || tokens[0].Kind != TokenQuoted || tokens[0].Text != expr.Text {
		t.Fatalf("tokenised literal = %#v, want one quoted token with text %q", tokens, expr.Text)
	}
}

func TestFormatExprSource_IndexedVarRefRoundTrips(t *testing.T) {
	t.Parallel()

	expr := &BinaryExpr{
		Left:  &VarRefExpr{Name: "counts", Path: "[0]"},
		Op:    ">",
		Right: &LiteralExpr{Text: "0"},
	}
	got := FormatExprSource(expr)
	want := `$counts[0] > 0`
	if got != want {
		t.Fatalf("FormatExprSource() = %q, want %q", got, want)
	}
	src := "assert " + got
	tokens, err := Tokenise(src)
	if err != nil {
		t.Fatalf("tokenise reformatted source %q: %v", src, err)
	}

	if _, err := Parse(tokens); err != nil {
		t.Fatalf("parse reformatted source %q: %v", src, err)
	}
}

func TestFormatAssertClauseSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		clause AssertClause
		want   string
	}{
		{
			name: "expr",
			clause: &AssertExprClause{
				Expr: &BinaryExpr{
					Left:  &VarRefExpr{Name: "lhs"},
					Op:    "==",
					Right: &LiteralExpr{Text: "42"},
				},
			},
			want: `$lhs == 42`,
		},
		{
			name: "command",
			clause: &AssertCommandClause{
				Negate: true,
				Head:   "ok",
				Args: []Expr{
					&LiteralExpr{Text: "exec"},
					&LiteralExpr{Text: "false"},
				},
			},
			want: `not ok exec false`,
		},
	}

	for _, tc := range tests {
		if got := FormatAssertClauseSource(tc.clause); got != tc.want {
			t.Fatalf("%s: FormatAssertClauseSource() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

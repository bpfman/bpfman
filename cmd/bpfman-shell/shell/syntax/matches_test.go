package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseMatchesAssert parses src and expects the single statement to
// be an AssertStmt whose clause is an expression-shaped MatchesExpr.
func parseMatchesAssert(t *testing.T, src string) (*AssertStmt, *MatchesExpr) {
	t.Helper()
	prog, err := parseSource(t, src)
	require.NoError(t, err)
	stmt, ok := firstStmt(t, prog).(*AssertStmt)
	require.True(t, ok, "want AssertStmt, got %T", firstStmt(t, prog))
	clause, ok := stmt.Clause.(*AssertExprClause)
	require.True(t, ok, "want *AssertExprClause, got %T", stmt.Clause)
	matches, ok := clause.Expr.(*MatchesExpr)
	require.True(t, ok, "want *MatchesExpr, got %T", clause.Expr)
	require.NotNil(t, matches.Block)
	return stmt, matches
}

func TestParse_MatchesBlock_SingleEntry(t *testing.T) {
	t.Parallel()

	_, matches := parseMatchesAssert(t, `assert $prog matches { record.meta.name: foo }`)
	block := matches.Block
	target, ok := matches.Target.(*VarRefExpr)
	require.True(t, ok)
	assert.Equal(t, "prog", target.Name)

	require.Len(t, block.Entries, 1)
	assert.Equal(t, "record.meta.name", block.Entries[0].Path)
	assert.Empty(t, block.Entries[0].Predicate)
	lit, ok := block.Entries[0].Pattern.(*LiteralExpr)
	require.True(t, ok)
	assert.Equal(t, "foo", lit.Text)
}

func TestParse_MatchesBlock_InsideCommandParenArg(t *testing.T) {
	t.Parallel()

	// matches is a postfix expression operator at the
	// ComparisonExpr level, not a command-tail. A bare command
	// statement ends at the first `{`, so to call a builtin
	// with a matches expression as one of its arguments, the
	// argument enters expression syntax via a CommandParenArg
	// `(EXPR)`. This test pins that round-trip: `print ($x
	// matches { ... })` must parse as a CommandStmt whose
	// argument is the MatchesExpr at the ComparisonExpr level.
	// The `{` inside the paren must not terminate the command
	// statement; the depth-tracking rule says `{` is only a
	// terminator at top-level paren/bracket depth.
	prog, err := parseSource(t, `print ($x matches { id: $expected })`)
	require.NoError(t, err)
	stmt, ok := firstStmt(t, prog).(*CommandStmt)
	require.True(t, ok, "want CommandStmt, got %T", firstStmt(t, prog))
	require.Len(t, stmt.Args, 2)
	head, ok := stmt.Args[0].(*LiteralExpr)
	require.True(t, ok)
	assert.Equal(t, "print", head.Text)
	matches, ok := stmt.Args[1].(*MatchesExpr)
	require.True(t, ok, "want *MatchesExpr inside the parens, got %T", stmt.Args[1])
	require.NotNil(t, matches.Block)
	require.Len(t, matches.Block.Entries, 1)
	assert.Equal(t, "id", matches.Block.Entries[0].Path)
}

func TestParse_MatchesBlock_InsideLetExprParen(t *testing.T) {
	t.Parallel()

	// Same shape inside a let-expression RHS: takeStmtTokens
	// must keep collecting through the `{` while paren depth
	// is positive. The matches block reaches the expression
	// parser via ParenExpr.
	prog, err := parseSource(t, `let r = ($x matches { id: $expected })`)
	require.NoError(t, err)
	let, ok := firstStmt(t, prog).(*LetStmt)
	require.True(t, ok, "want LetStmt, got %T", firstStmt(t, prog))
	// The outer expression is a ParenExpr (or its unwrapped
	// inner) ending in a MatchesExpr.
	_ = let
}

func TestParse_MatchesBlock_InsideIfCondParen(t *testing.T) {
	t.Parallel()

	// parseCondition collects tokens up to the next `{` that
	// opens the branch body. Inside a paren-grouped condition
	// expression the inner `{` of a matches block must not
	// terminate the condition early; the depth rule keeps
	// collecting until paren depth returns to zero.
	src := "if ($x matches { id: $expected }) { print ok }"
	prog, err := parseSource(t, src)
	require.NoError(t, err)
	_, ok := firstStmt(t, prog).(*IfStmt)
	require.True(t, ok, "want IfStmt, got %T", firstStmt(t, prog))
}

func TestParse_MatchesBlock_MultiEntry_NewlineSeparated(t *testing.T) {
	t.Parallel()

	src := `assert $prog matches {
    record.meta.name: foo
    status.kernel.id: $pid
    status.kernel.tag: not-empty
}`
	_, matches := parseMatchesAssert(t, src)
	block := matches.Block
	require.Len(t, block.Entries, 3)

	assert.Equal(t, "record.meta.name", block.Entries[0].Path)
	assert.Equal(t, "foo", block.Entries[0].Pattern.(*LiteralExpr).Text)

	assert.Equal(t, "status.kernel.id", block.Entries[1].Path)
	assert.Equal(t, "pid", block.Entries[1].Pattern.(*VarRefExpr).Name)

	assert.Equal(t, "status.kernel.tag", block.Entries[2].Path)
	assert.Equal(t, "not-empty", block.Entries[2].Predicate)
	assert.Nil(t, block.Entries[2].Pattern)
}

// Commas are not a valid entry separator inside a matches block --
// the block is line-oriented (one path-pattern relation per line).
// See the def parameter list for the contrasting case where commas
// are the right tool: parameters are a value list, not a table.
//
// Two diagnostic shapes appear: a stand-alone `,` token (when
// surrounded by whitespace, as on a line of its own) hits the
// "',' is not a valid entry separator" path; a `,` glued to the
// preceding token (the common `1,` shape -- the lexer treats `,`
// as a word-interior character because the rest of the language
// has no syntactic use for it) hits the "trailing ',' on ..."
// path. Both share the "entries are separated by newlines"
// suffix; the test asserts that.
func TestParse_MatchesBlock_RejectsCommaSeparator(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
	}{
		{"trailing-glued", `assert $p matches { a.b: 1, c.d: 2 }`},
		{"trailing-glued-final", `assert $p matches { a.b: 1, }`},
		{"trailing-glued-multiline", `assert $p matches { a.b: 1,
                                            c.d: 2 }`},
		{"standalone-on-own-line", `assert $p matches {
    a.b: 1
    ,
    c.d: 2
}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseSource(t, tc.src)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "matches:")
			assert.Contains(t, err.Error(), "entries are separated by newlines")
		})
	}
}

// `;` is not a valid entry separator either: a matches block is
// line-oriented (one path-pattern relation per line), not a
// sequence of statements. The same diagnostic shape as the comma
// rejection points the user at the newline rule.
func TestParse_MatchesBlock_RejectsSemicolonSeparator(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
	}{
		{"between-entries", `assert $p matches { a.b: 1; c.d: 2 }`},
		{"trailing", `assert $p matches { a.b: 1; }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseSource(t, tc.src)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "';' is not a valid entry separator")
		})
	}
}

func TestParse_MatchesBlock_ColonSpacingForms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
	}{
		{"glued-trailing", `assert $p matches { a.b: x }`},
		{"standalone", `assert $p matches { a.b : x }`},
		{"glued-leading", `assert $p matches { a.b :x }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, matches := parseMatchesAssert(t, tc.src)
			block := matches.Block
			require.Len(t, block.Entries, 1)
			assert.Equal(t, "a.b", block.Entries[0].Path)
			assert.Equal(t, "x", block.Entries[0].Pattern.(*LiteralExpr).Text)
		})
	}
}

func TestParse_MatchesBlock_IndexedPath(t *testing.T) {
	t.Parallel()

	_, matches := parseMatchesAssert(t, `assert $p matches { items[0].id: 1 }`)
	block := matches.Block
	require.Len(t, block.Entries, 1)
	assert.Equal(t, "items[0].id", block.Entries[0].Path)
	assert.Equal(t, "1", block.Entries[0].Pattern.(*LiteralExpr).Text)
}

func TestParse_MatchesBlock_RejectsIndexedPathInExhaustive(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, `assert $p matches exhaustive { items[0].id: 1 }`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dotted or indexed path")
}

func TestParse_MatchesBlock_QuotedPattern(t *testing.T) {
	t.Parallel()

	_, matches := parseMatchesAssert(t, `assert $p matches { a.b: "hello world" }`)
	block := matches.Block
	require.Len(t, block.Entries, 1)
	lit := block.Entries[0].Pattern.(*LiteralExpr)
	assert.Equal(t, "hello world", lit.Text)
	assert.True(t, lit.Quoted)
}

// `not-empty' written bare is the unary-predicate pattern: assert
// the field is non-empty. Quoting it escapes back to a literal
// compare against the string "not-empty". This is the
// disambiguation rule for any bare-word that doubles as a keyword
// in pattern position; the same escape works for `true`, `false`,
// and any future predicate.
func TestParse_MatchesBlock_BareNotEmpty_IsPredicate(t *testing.T) {
	t.Parallel()

	_, matches := parseMatchesAssert(t, `assert $p matches { a.b: not-empty }`)
	block := matches.Block
	require.Len(t, block.Entries, 1)
	assert.Equal(t, "not-empty", block.Entries[0].Predicate, "bare not-empty must register the unary predicate")
	assert.Nil(t, block.Entries[0].Pattern, "predicate entry has no expression pattern")
}

func TestParse_MatchesBlock_BareNull_IsPredicate(t *testing.T) {
	t.Parallel()

	_, matches := parseMatchesAssert(t, `assert $p matches { a.b: null }`)
	block := matches.Block
	require.Len(t, block.Entries, 1)
	assert.Equal(t, "null", block.Entries[0].Predicate)
	assert.Nil(t, block.Entries[0].Pattern)
}

func TestParse_MatchesBlock_BareNilRejected(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, `assert $p matches { a.b: nil }`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil has been removed; use null")
}

func TestParse_MatchesBlock_QuotedNotEmpty_IsLiteral(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		`assert $p matches { a.b: "not-empty" }`,
		`assert $p matches { a.b: 'not-empty' }`,
	} {
		_, matches := parseMatchesAssert(t, src)
		block := matches.Block
		require.Len(t, block.Entries, 1)
		assert.Empty(t, block.Entries[0].Predicate, "quoted form must NOT trigger the predicate path: %s", src)
		lit, ok := block.Entries[0].Pattern.(*LiteralExpr)
		require.True(t, ok, "quoted form must produce a literal expression: %s", src)
		assert.Equal(t, "not-empty", lit.Text)
		assert.True(t, lit.Quoted, "the literal must remember it was quoted: %s", src)
	}
}

func TestParse_MatchesBlock_Errors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
	}{
		{"missing colon", `assert $p matches { a.b foo }`},
		{"missing pattern", `assert $p matches { a.b: }`},
		{"empty path", `assert $p matches { :foo }`},
		{"unterminated", `assert $p matches { a.b: foo`},
		{"matches at start of line", `matches { a.b: foo }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseSource(t, tc.src)
			require.Error(t, err)
		})
	}
}

func TestParse_MatchesBlock_RequiresMatchesKeyword(t *testing.T) {
	t.Parallel()

	// The block syntax is gated on the bare keyword "matches"
	// preceding `{`. With the keyword the assert's expression
	// becomes a MatchesExpr; without it the brace is not valid in
	// that position.
	prog, err := parseSource(t, `assert $p matches { a.b: foo }`)
	require.NoError(t, err)
	stmt, ok := firstStmt(t, prog).(*AssertStmt)
	require.True(t, ok)
	clause, ok := stmt.Clause.(*AssertExprClause)
	require.True(t, ok)
	_, isMatches := clause.Expr.(*MatchesExpr)
	assert.True(t, isMatches)
}

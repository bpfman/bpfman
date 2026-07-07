package syntax

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dumpString tokenises and parses src, dumps the resulting
// program to a string, and returns it for assertion. Tests use
// it as a one-liner so source samples stay readable.
func dumpString(t *testing.T, src string) string {
	t.Helper()
	tokens, err := Tokenise(src)
	require.NoError(t, err)
	prog, err := Parse(tokens)
	require.NoError(t, err)
	var b strings.Builder
	require.NoError(t, DumpAST(&b, prog))
	return b.String()
}

func TestDumpAST_LetWithLiteral(t *testing.T) {
	t.Parallel()

	out := dumpString(t, "let x = 42")
	// Root is *Program with a Stmts slice containing one LetStmt.
	assert.Contains(t, out, "Program {")
	assert.Contains(t, out, "Stmts: []syntax.Stmt (len = 1)")
	assert.Contains(t, out, "LetStmt {")
	assert.Contains(t, out, "Name: Ident {")
	assert.Contains(t, out, `Text: "x"`)
	assert.Contains(t, out, "LiteralExpr {")
	assert.Contains(t, out, `Text: "42"`)
}

func TestDumpAST_ArithmeticHasBothOperators(t *testing.T) {
	t.Parallel()

	// '4 * 2 + 1' produces both operators in the tree. The
	// precedence-shape assertion is a separate concern (and the
	// parser's responsibility to get right); the dumper's job
	// is to faithfully reflect whatever shape was built.
	out := dumpString(t, "let r = 4 * 2 + 1")
	assert.Contains(t, out, `Op: "+"`)
	assert.Contains(t, out, `Op: "*"`)
}

func TestDumpAST_DeferKill(t *testing.T) {
	t.Parallel()

	out := dumpString(t, "defer kill $p")
	assert.Contains(t, out, "DeferStmt {")
	assert.Contains(t, out, "CommandStmt {")
	// Args should hold a literal "kill" and a VarRefExpr "p".
	assert.Contains(t, out, `Text: "kill"`)
	assert.Contains(t, out, "VarRefExpr {")
	assert.Contains(t, out, `Name: "p"`)
}

func TestDumpAST_EmptyProgram(t *testing.T) {
	t.Parallel()

	// An empty input should produce a Program with no Stmts;
	// the dumper elides empty slices via the zero-value rule.
	out := dumpString(t, "")
	assert.Contains(t, out, "Program {")
	assert.NotContains(t, out, "Stmts:")
}

func TestDumpAST_SpanShorthand(t *testing.T) {
	t.Parallel()

	// source.Span renders as 'Span: line:col-line:col' on a single
	// line, not as a nested struct with two embedded source.Pos
	// values. Without the shorthand the dump's vertical
	// space would more than double.
	out := dumpString(t, "let x = 1")
	assert.Contains(t, out, "Span: 1:")
	assert.Contains(t, out, "-1:")
	assert.NotContains(t, out, "Line:")
	assert.NotContains(t, out, "Col:")
}

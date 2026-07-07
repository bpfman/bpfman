package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_IfBasic(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "if $count > 0 { bpfman program list }")
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 1)

	ifStmt, ok := prog.Stmts[0].(*IfStmt)
	require.True(t, ok)
	assert.Empty(t, ifStmt.Elifs)
	assert.Empty(t, ifStmt.Else)
	require.Len(t, ifStmt.Then, 1)

	_, ok = ifStmt.Then[0].(*CommandStmt)
	assert.True(t, ok)

	bin, ok := ifStmt.Cond.(*BinaryExpr)
	require.True(t, ok)
	assert.Equal(t, ">", bin.Op)
}

func TestParse_IfElseMultiLine(t *testing.T) {
	t.Parallel()

	input := "if $count > 0 {\n  let a = yes\n} else {\n  let a = no\n}"
	prog, err := parseSource(t, input)
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 1)

	ifStmt, ok := prog.Stmts[0].(*IfStmt)
	require.True(t, ok)
	assert.Empty(t, ifStmt.Elifs)
	require.Len(t, ifStmt.Then, 1)
	require.Len(t, ifStmt.Else, 1)
	_, ok = ifStmt.Then[0].(*LetStmt)
	assert.True(t, ok)
	_, ok = ifStmt.Else[0].(*LetStmt)
	assert.True(t, ok)
}

func TestParse_IfElifChain(t *testing.T) {
	t.Parallel()

	input := "if $x == 1 {\n let a = one\n} elif $x == 2 {\n let a = two\n} elif $x == 3 {\n let a = three\n} else {\n let a = other\n}"
	prog, err := parseSource(t, input)
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 1)

	ifStmt, ok := prog.Stmts[0].(*IfStmt)
	require.True(t, ok)
	assert.Len(t, ifStmt.Elifs, 2)
	assert.Len(t, ifStmt.Else, 1)
}

func TestParse_IfNested(t *testing.T) {
	t.Parallel()

	input := "if $a == 1 {\n if $b == 2 {\n let c = yes\n }\n}"
	prog, err := parseSource(t, input)
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 1)

	outer, ok := prog.Stmts[0].(*IfStmt)
	require.True(t, ok)
	require.Len(t, outer.Then, 1)
	_, ok = outer.Then[0].(*IfStmt)
	assert.True(t, ok)
}

func TestParse_IfMissingBrace(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "if $count > 0 bpfman program list")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected '{'")
}

func TestParse_IfUnterminatedBlock(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, "if $x == 1 {\n let a = yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unterminated block")
}

func TestParse_SemicolonSeparator(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "let a = 1 ; let b = 2")
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 2)
	_, ok := prog.Stmts[0].(*LetStmt)
	assert.True(t, ok)
	_, ok = prog.Stmts[1].(*LetStmt)
	assert.True(t, ok)
}

func TestParse_NewlineSeparator(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, "let a = 1\nlet b = 2\n")
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 2)
}

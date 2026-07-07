package driver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

func TestParseAndExpandWithBase_UsesAbsoluteSourcePositions(t *testing.T) {
	t.Parallel()

	prog, err := ParseAndExpandWithBase("main.bpfman", "", "let answer = 42", 40)
	require.NoError(t, err)
	require.Len(t, prog.Stmts, 1)

	letStmt, ok := prog.Stmts[0].(*syntax.LetStmt)
	require.True(t, ok, "expected LetStmt, got %T", prog.Stmts[0])
	assert.Equal(t, source.Pos{File: "main.bpfman", Line: 40, Col: 1}, letStmt.Pos)
}

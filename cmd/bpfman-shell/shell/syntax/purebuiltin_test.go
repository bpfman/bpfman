package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_PureCall_ArityZeroConsumesNoArgs(t *testing.T) {
	t.Parallel()

	const name = "__syntax_now__"
	registerPureBuiltin(name, 0)
	t.Cleanup(func() { unregisterPureBuiltin(name) })

	tokens, err := Tokenise("let t = " + name)
	require.NoError(t, err)
	prog, err := Parse(tokens)
	require.NoError(t, err)
	let, _ := prog.Stmts[0].(*LetStmt)
	call, ok := let.RHS.(*PureCallExpr)
	require.True(t, ok)
	assert.Equal(t, name, call.Name)
	assert.Empty(t, call.Args)
}

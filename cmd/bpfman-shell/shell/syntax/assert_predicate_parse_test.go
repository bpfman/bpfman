package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_AssertPredicatesBecomeExprClauses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
		call string
	}{
		{name: "path-exists", src: `assert path-exists "/tmp/probe"` + "\n", call: "path-exists"},
		{name: "contains", src: `assert contains $rc.stderr "boom"` + "\n", call: "contains"},
		{name: "null", src: `assert null $got.updated_at` + "\n", call: "null"},
		{name: "present", src: `assert present $ours` + "\n", call: "present"},
		{name: "missing", src: `assert missing $ours.last_error` + "\n", call: "missing"},
		{name: "empty", src: `assert empty $got.links` + "\n", call: "empty"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := parseSource(t, tc.src)
			require.NoError(t, err)
			stmt, ok := firstStmt(t, prog).(*AssertStmt)
			require.True(t, ok, "want AssertStmt, got %T", firstStmt(t, prog))
			clause, ok := stmt.Clause.(*AssertExprClause)
			require.True(t, ok, "want *AssertExprClause, got %T", stmt.Clause)
			call, ok := clause.Expr.(*PureCallExpr)
			require.True(t, ok, "want *PureCallExpr, got %T", clause.Expr)
			assert.Equal(t, tc.call, call.Name)
		})
	}
}

func TestParse_AssertNilRejected(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, `assert nil $x`+"\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil has been removed; use null")
}

func TestParse_LetNilRejected(t *testing.T) {
	t.Parallel()

	_, err := parseSource(t, `let x = nil`+"\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil has been removed; use null")
}

func TestParse_LetNullIsLiteral(t *testing.T) {
	t.Parallel()

	prog, err := parseSource(t, `let x = null`+"\n")
	require.NoError(t, err)
	let, ok := firstStmt(t, prog).(*LetStmt)
	require.True(t, ok, "want LetStmt, got %T", firstStmt(t, prog))
	lit, ok := let.RHS.(*LiteralExpr)
	require.True(t, ok, "want *LiteralExpr, got %T", let.RHS)
	assert.Equal(t, "null", lit.Text)
}

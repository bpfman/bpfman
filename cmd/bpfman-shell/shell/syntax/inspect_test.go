package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inspectVarRefs walks a parsed source string and returns the
// list of variable names referenced anywhere in the tree.
// Mirrors the canonical 'find every X' tool you would write
// against go/ast.Inspect.
func inspectVarRefs(t *testing.T, src string) []string {
	t.Helper()
	tokens, err := Tokenise(src)
	require.NoError(t, err)
	prog, err := Parse(tokens)
	require.NoError(t, err)
	var names []string
	Inspect(prog, func(n Node) bool {
		if v, ok := n.(*VarRefExpr); ok {
			names = append(names, v.Name)
		}
		return true
	})
	return names
}

func TestInspect_FindsAllVarRefs(t *testing.T) {
	t.Parallel()

	names := inspectVarRefs(t, "let r = $a + $b * $c")
	assert.ElementsMatch(t, []string{"a", "b", "c"}, names)
}

func TestInspect_DescendsIntoBindCmd(t *testing.T) {
	t.Parallel()

	names := inspectVarRefs(t, "let result <- bpfman program get $prog")
	assert.Contains(t, names, "prog")
}

func TestInspect_DescendsIntoDeferCmd(t *testing.T) {
	t.Parallel()

	names := inspectVarRefs(t, "defer kill $job")
	assert.Contains(t, names, "job")
}

func TestInspect_DescendsIntoForEachAndIfBodies(t *testing.T) {
	t.Parallel()

	src := `foreach x in $xs {
		if $x { print $a } else { print $b }
	}`
	names := inspectVarRefs(t, src)
	assert.ElementsMatch(t, []string{"xs", "x", "a", "b"}, names)
}

func TestInspect_SkipSubtreeReturnsFalse(t *testing.T) {
	t.Parallel()

	// Returning false from the visitor must skip the
	// subtree: a foreach over $xs with $a inside should
	// report only the outer $xs when the visitor stops at
	// ForEachStmt.
	tokens, err := Tokenise("foreach x in $xs { print $a }")
	require.NoError(t, err)
	prog, err := Parse(tokens)
	require.NoError(t, err)

	var names []string
	Inspect(prog, func(n Node) bool {
		if _, ok := n.(*ForEachStmt); ok {
			// Walk the loop's List subtree only, not the body.
			Inspect(n.(*ForEachStmt).List, func(inner Node) bool {
				if v, ok := inner.(*VarRefExpr); ok {
					names = append(names, v.Name)
				}
				return true
			})
			return false
		}
		return true
	})
	assert.Equal(t, []string{"xs"}, names)
}

func TestInspect_DescendsIntoMatchesEntries(t *testing.T) {
	t.Parallel()

	// Patterns on the right-hand side of a matches entry are
	// regular expressions and must be visited by Inspect just
	// like any other child Expr. Without this descent any
	// generic walker that uses Inspect to collect VarRefs
	// (notably the static checker's undefined-variable pass)
	// silently misses references that only appear inside a
	// matches block.
	names := inspectVarRefs(t, "let r = $got matches { id: $expected }")
	assert.ElementsMatch(t, []string{"got", "expected"}, names)
}

func TestInspect_DescendsIntoNestedMatchesSubBlock(t *testing.T) {
	t.Parallel()

	// SubBlock entries recurse into another MatchesBlockExpr;
	// the walker must hand control back to its own case so
	// nested entries are visited too, otherwise references
	// inside an inner block escape the traversal.
	src := "let r = $envelope matches { stdout: matches { id: $expected } }"
	names := inspectVarRefs(t, src)
	assert.ElementsMatch(t, []string{"envelope", "expected"}, names)
}

func TestInspect_PostOrderNilSentinel(t *testing.T) {
	t.Parallel()

	// f is called once more with nil after each subtree;
	// counters that increment on nil must equal counters
	// that increment on non-nil entries (each entry produces
	// exactly one nil exit).
	tokens, err := Tokenise("let x = 4 * 2 + 1")
	require.NoError(t, err)
	prog, err := Parse(tokens)
	require.NoError(t, err)

	var entries, exits int
	Inspect(prog, func(n Node) bool {
		if n == nil {
			exits++
		} else {
			entries++
		}
		return true
	})
	assert.Equal(t, entries, exits, "every entry should have a paired nil exit")
}

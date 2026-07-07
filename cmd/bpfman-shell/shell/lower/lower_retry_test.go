package lower

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// TestLower_NestedDefRejected pins the defence-in-depth gate on
// def hoisting. The static checker rejects any def declared
// outside the top level (nested in a def body, an if/elif/else
// branch, a foreach body, a poll body, etc), but Lower is
// callable on any parsed program with no checker as a
// precondition; lowerDefStmt unconditionally appended the
// resulting *ir.Def to the shared lowerState.defs slice, so a
// nested def reached at lowering time would still get
// globally hoisted at runtime registration. Reject the shape
// here so the IR Defs list can only carry top-level defs no
// matter how Lower is reached.
func TestLower_NestedDefRejected(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
	}{
		{
			name: "def inside def",
			src:  "def outer() { def inner() { return 1 } }",
		},
		{
			name: "def inside if",
			src:  "if true { def inside_if() { return 1 } }",
		},
		{
			name: "def inside foreach",
			src:  "foreach x in [1 2 3] { def inside_loop() { return 1 } }",
		},
		{
			name: "def inside poll",
			src:  "poll timeout 1s every 1ms { def inside_poll() { return 1 } }",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tokens, err := syntax.Tokenise(tc.src)
			require.NoError(t, err)
			prog, err := syntax.Parse(tokens)
			require.NoError(t, err)
			_, err = Lower(prog)
			require.Error(t, err, "expected the lowerer to reject %s", tc.name)
			assert.Contains(t, err.Error(), "def",
				"diagnostic should mention 'def' to point at the misplacement")
		})
	}
}

// TestLower_RetryRejectedOutsidePollAndDef pins the defence-in-
// depth invariant the runtime relies on: a RetryStmt only makes
// sense inside a poll attempt's lexical body or inside a helper
// def that is callable from a poll attempt. The static checker
// normally catches a misplaced `retry`, but Lower is callable on
// any parsed program (the checker is not a precondition), and
// the emitted retry sequence pops an attempt frame plus drains
// attempt-local defers. If those structures do not exist the
// resulting IR will dismantle the program-level frame and the
// runtime cannot recover. The lowerer must reject the shape
// itself rather than trust an upstream gate.
func TestLower_RetryRejectedOutsidePollAndDef(t *testing.T) {
	t.Parallel()

	src := "retry \"top-level\""
	tokens, err := syntax.Tokenise(src)
	require.NoError(t, err)
	prog, err := syntax.Parse(tokens)
	require.NoError(t, err)

	_, err = Lower(prog)
	require.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "retry") &&
			(strings.Contains(err.Error(), "outside") || strings.Contains(err.Error(), "poll")),
		"expected the lowerer to reject a top-level retry, got %q", err.Error())
}

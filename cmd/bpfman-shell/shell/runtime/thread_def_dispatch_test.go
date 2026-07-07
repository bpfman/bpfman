package runtime

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Thread (|>) is expression-position bind-dispatch with the rc
// envelope discarded; it must obey the same head-resolution rule
// as every other command-shaped construct that runs in value-
// producing position (bind RHS, defer, bind-collect). Without
// the unification a def shadowing nothing falls through to the
// external ExecBind path, so $value |> my_def silently runs as a
// subprocess and binds the empty primary back to the caller.

func TestThread_DispatchResolvesDefByHead(t *testing.T) {
	t.Parallel()

	// The minimal shape: thread a literal LHS into a def whose
	// body returns its parameter unchanged. With def-first
	// dispatch the script's `require` passes; without it the
	// thread falls through to the external lane, the primary
	// is empty, and the require fails.
	src := `
def id(x) {
  return $x
}
let got = "hello" |> id
require $got == "hello"
`
	err := runScriptError(t, src, nil)
	require.NoError(t, err, "thread |> def should resolve through the def-first policy")
}

func TestThread_DispatchAppendsLHSAsLastPositional(t *testing.T) {
	t.Parallel()

	// Two-parameter def: thread convention puts the LHS as the
	// last positional, so `"hello" |> suffix world` binds
	// extra=world and x=hello inside the def body. The
	// expected return string proves both the def-first
	// resolution and the LHS-appended-last argument ordering.
	src := `
def suffix(extra x) {
  return "${x}-${extra}"
}
let got = "hello" |> suffix world
require $got == "hello-world"
`
	err := runScriptError(t, src, nil)
	require.NoError(t, err, "thread |> def must place LHS at the last positional slot")
}

func TestThread_DispatchSurfacesDefFailureAtThreadSite(t *testing.T) {
	t.Parallel()

	// A failure inside the def's body must propagate out of the
	// thread expression rather than disappearing through the
	// fall-through to ExecBind. Without def-first dispatch the
	// thread never enters the def, so the guard inside `bad`
	// never runs and no error escapes; the bug presents as a
	// silent empty-primary bind. With the fix the guard inside
	// the def fires and the let statement halts with the
	// failure decorated against the thread site.
	src := `
def bad(x) {
  guard _ <- fail-command
  return $x
}
let got = "hello" |> bad
`
	err := runScriptError(t, src, map[string]int{"fail-command": 1})
	require.Error(t, err, "guard failure inside the def must escape the thread expression")
	assert.True(t, strings.Contains(err.Error(), "fail-command") || strings.Contains(strings.ToLower(err.Error()), "guard"), "error must mention the failing command or guard, got %q", err.Error())
}

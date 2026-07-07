package builtins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	_ "github.com/bpfman/bpfman/cmd/bpfman-shell/fixturemode"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

// callFire invokes handleFire with the given word-arg sequence
// and returns the error. Tests rely on this for arg-parsing
// failures that fail before any spawn: a successful spawn from
// the test binary is not possible because the test binary's main
// is the go test runner, not bpfman-shell's fixturemode dispatcher.
func callFire(t *testing.T, args ...string) (runtime.Value, error) {
	wargs := make([]runtime.Arg, len(args))
	for i, a := range args {
		wargs[i] = runtime.WordArg{Text: a}
	}
	return handleFire(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "fire",
		Args: wargs,
	})
}

func TestHandleFire_UnknownKind(t *testing.T) {
	t.Parallel()
	_, err := callFire(t, "nosuch", "/tmp/s", "/tmp/a", "--count=1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown kind "nosuch"`)
	assert.Contains(t, err.Error(), "registered:")
}

func TestHandleFire_TooFewPositionals(t *testing.T) {
	t.Parallel()
	_, err := callFire(t, "unlinkat", "/tmp/s", "--count=1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected 3 positional arguments")
}

func TestHandleFire_TooManyPositionals(t *testing.T) {
	t.Parallel()
	_, err := callFire(t, "unlinkat", "/tmp/s", "/tmp/a", "/tmp/extra", "--count=1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected 3 positional arguments")
}

func TestHandleFire_MissingCount(t *testing.T) {
	t.Parallel()
	_, err := callFire(t, "unlinkat", "/tmp/s", "/tmp/a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--count is required")
}

func TestHandleFire_BadCount(t *testing.T) {
	t.Parallel()
	_, err := callFire(t, "unlinkat", "/tmp/s", "/tmp/a", "--count=abc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--count")
}

func TestHandleFire_NegativeCount(t *testing.T) {
	t.Parallel()
	_, err := callFire(t, "unlinkat", "/tmp/s", "/tmp/a", "--count=-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--count must not be negative")
}

func TestHandleFire_BadWaves(t *testing.T) {
	t.Parallel()
	_, err := callFire(t, "unlinkat", "/tmp/s", "/tmp/a", "--count=1", "--waves=xyz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--waves")
}

func TestHandleFire_ZeroWaves(t *testing.T) {
	t.Parallel()
	_, err := callFire(t, "unlinkat", "/tmp/s", "/tmp/a", "--count=1", "--waves=0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--waves must be at least 1")
}

func TestHandleFire_UnknownFlag(t *testing.T) {
	t.Parallel()
	_, err := callFire(t, "unlinkat", "/tmp/s", "/tmp/a", "--count=1", "--bogus=x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown flag "--bogus=x"`)
}

// TestFireKinds_RegisteredAtInit verifies that the fixturemode
// package's init functions populated the registry by the time
// tests run. The Mode values are part of the contract with
// fixturemode.Run and must match.
func TestFireKinds_RegisteredAtInit(t *testing.T) {
	t.Parallel()

	want := map[string]driver.FireKind{
		"unlinkat": {Mode: "unlinkat-fire-worker", NeedsBinary: false},
		"kill":     {Mode: "kill-fire-worker", NeedsBinary: false},
		"uprobe":   {Mode: "uprobe-fire-worker", NeedsBinary: true},
	}
	for name, w := range want {
		got, ok := driver.FireKinds()[name]
		require.Truef(t, ok, "fire kind %q is not registered", name)
		assert.Equal(t, w.Mode, got.Mode, "kind %s mode", name)
		assert.Equal(t, w.NeedsBinary, got.NeedsBinary, "kind %s NeedsBinary", name)
		assert.NotEmpty(t, got.Summary, "kind %s should carry a Summary", name)
	}
}

// TestFireKindNames_Sorted verifies the diagnostic helper returns
// names in a stable order so error messages are reproducible.
func TestFireKindNames_Sorted(t *testing.T) {
	t.Parallel()
	names := driver.FireKindNames()
	for i := 1; i < len(names); i++ {
		assert.LessOrEqual(t, names[i-1], names[i], "driver.FireKindNames should be sorted")
	}
}

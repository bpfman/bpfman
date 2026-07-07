package residue

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindE2EUnmanagedProgramPins(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Pins that must be swept: basename carries the prefix.
	matches := []string{
		E2EUnmanagedPinPrefix + "schedcls_42",
		E2EUnmanagedPinPrefix + "kprobe_99",
	}
	// Entries that must be left alone: a name under a different
	// prefix, bpfman's own pins, and unrelated names.
	others := []string{
		"e2e_unmanaged_schedcls_1",
		"dispatcher_xdp_eth0",
		"some_other_program",
	}
	for _, name := range append(append([]string{}, matches...), others...) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o644))
	}
	// A directory whose name carries the prefix must be ignored: we
	// only sweep file pins.
	require.NoError(t, os.Mkdir(filepath.Join(dir, E2EUnmanagedPinPrefix+"subdir"), 0o755))

	got, err := findE2EUnmanagedProgramPins(dir)
	require.NoError(t, err)

	want := []string{
		filepath.Join(dir, E2EUnmanagedPinPrefix+"schedcls_42"),
		filepath.Join(dir, E2EUnmanagedPinPrefix+"kprobe_99"),
	}
	assert.ElementsMatch(t, want, got)
}

func TestFindE2EUnmanagedProgramPins_MissingRootIsNoResidue(t *testing.T) {
	t.Parallel()

	got, err := findE2EUnmanagedProgramPins(filepath.Join(t.TempDir(), "does-not-exist"))
	require.NoError(t, err)
	assert.Empty(t, got)
}

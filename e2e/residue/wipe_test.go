package residue_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/e2e/residue"
	"github.com/bpfman/bpfman/fs"
)

// TestScanWipe_RemovesRuntimeRoot exercises the wipe plan on a
// fixture that mimics a populated runtime root (lock file, DB
// files, bytecode cache subdirs) but with no bpffs mount, so the
// plan reduces to a single RemoveTree against the base. Apply
// should leave nothing behind.
func TestScanWipe_RemovesRuntimeRoot(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), "bpfman")
	require.NoError(t, os.MkdirAll(filepath.Join(base, "db"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(base, "sock"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(base, "programs", "X"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, ".lock"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(base, "db", "store.db"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(base, "programs", "X", "obj.o"), []byte("x"), 0o600))

	layout, err := fs.New(base)
	require.NoError(t, err)

	plan, err := residue.ScanWipe(layout)
	require.NoError(t, err)
	require.Len(t, plan, 1, "expected a single RemoveTree action (no bpffs mounted in test)")

	require.Empty(t, plan.Apply())
	_, err = os.Stat(base)
	assert.True(t, os.IsNotExist(err), "base should be gone after wipe")
}

// TestScanWipe_AbsentRuntimeRoot is a fresh-box invocation: the
// runtime root does not exist yet, so the plan is empty and
// applying it is a no-op success.
func TestScanWipe_AbsentRuntimeRoot(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), "bpfman")
	layout, err := fs.New(base)
	require.NoError(t, err)

	plan, err := residue.ScanWipe(layout)
	require.NoError(t, err)
	assert.True(t, plan.Empty(), "no actions expected when base is absent")
}

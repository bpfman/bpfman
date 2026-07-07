package runtime_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/fs/runtime"
)

// mockMounter records calls and can be configured to return errors.
type mockMounter struct {
	calls []string
	err   error
}

func (m *mockMounter) EnsureMounted(mountPoint string) error {
	m.calls = append(m.calls, mountPoint)
	return m.err
}

func TestEnsure_CreatesDirectories(t *testing.T) {
	t.Parallel()

	root, err := fs.New(filepath.Join(t.TempDir(), "bpfman"))
	require.NoError(t, err)

	mounter := &mockMounter{}
	ensuredRuntime, err := runtime.New(root, mounter, nil)
	require.NoError(t, err)
	assert.True(t, ensuredRuntime.Valid(), "ensured runtime should be valid")

	// Verify runtime directories were created
	for _, dir := range root.RuntimeDirs() {
		info, err := os.Stat(dir)
		require.NoError(t, err, "directory %s should exist", dir)
		assert.True(t, info.IsDir(), "%s should be a directory", dir)
	}
}

func TestEnsure_CallsMounter(t *testing.T) {
	t.Parallel()

	root, err := fs.New(filepath.Join(t.TempDir(), "bpfman"))
	require.NoError(t, err)

	mounter := &mockMounter{}
	_, err = runtime.New(root, mounter, nil)
	require.NoError(t, err)

	require.Len(t, mounter.calls, 1)
	assert.Equal(t, root.BPFFSMountPoint(), mounter.calls[0])
}

func TestEnsure_MounterError(t *testing.T) {
	t.Parallel()

	root, err := fs.New(filepath.Join(t.TempDir(), "bpfman"))
	require.NoError(t, err)

	expectedErr := errors.New("mount failed")
	mounter := &mockMounter{err: expectedErr}

	_, err = runtime.New(root, mounter, nil)
	assert.ErrorIs(t, err, expectedErr)
}

func TestEnsure_DirectoryCreationError(t *testing.T) {
	t.Parallel()

	// Create a file where a directory should be created
	tmpDir := t.TempDir()
	root, err := fs.New(filepath.Join(tmpDir, "bpfman"))
	require.NoError(t, err)

	// Create a file at the base path to block directory creation
	err = os.WriteFile(root.Base(), []byte("blocker"), 0o644)
	require.NoError(t, err)

	mounter := &mockMounter{}
	_, err = runtime.New(root, mounter, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "create directory")
}

func TestNoOpMounter_CreatesDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	mountPoint := filepath.Join(tmpDir, "test", "nested", "mount")

	mounter := runtime.NoOpMounter{}
	err := mounter.EnsureMounted(mountPoint)
	require.NoError(t, err)

	info, err := os.Stat(mountPoint)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestNoOpMounter_Idempotent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	mountPoint := filepath.Join(tmpDir, "mount")

	mounter := runtime.NoOpMounter{}

	// First call creates the directory
	err := mounter.EnsureMounted(mountPoint)
	require.NoError(t, err)

	// Second call succeeds (idempotent)
	err = mounter.EnsureMounted(mountPoint)
	require.NoError(t, err)
}

package fs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/fs"
)

func TestImageCache_RemoveCacheEntry_RemovesChild(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "cache")
	cache, err := fs.NewImageCache(root)
	require.NoError(t, err)
	require.NoError(t, cache.EnsureRoot())

	child := cache.CacheKeyDir("sha256_deadbeef")
	require.NoError(t, os.MkdirAll(child, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(child, "bytecode.o"), []byte("x"), 0644))

	require.NoError(t, cache.RemoveCacheEntry("sha256_deadbeef"))
	assert.NoDirExists(t, child)
}

func TestImageCache_RemoveCacheEntry_RejectsRoot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "cache")
	cache, err := fs.NewImageCache(root)
	require.NoError(t, err)
	require.NoError(t, cache.EnsureRoot())

	err = cache.RemoveCacheEntry("")
	assert.Error(t, err)
	var errOutside fs.ErrOutsideLayout
	assert.ErrorAs(t, err, &errOutside)
}

func TestImageCache_CreateTempDir_CleanupIdempotent(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "cache")
	cache, err := fs.NewImageCache(root)
	require.NoError(t, err)

	tmpDir, cleanup, err := cache.CreateTempDir()
	require.NoError(t, err)

	info, err := os.Stat(tmpDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	cleanup()
	cleanup()
	assert.NoDirExists(t, tmpDir)
}

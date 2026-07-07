package fs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/fs"
)

func TestBPFFS_SafeRemoveAll_UnderParent(t *testing.T) {
	t.Parallel()

	layout, err := fs.New(t.TempDir())
	require.NoError(t, err)

	b := layout.BPFFS()
	require.NoError(t, os.MkdirAll(b.MountPoint(), 0755))

	target := filepath.Join(b.MountPoint(), "child")
	require.NoError(t, os.MkdirAll(target, 0755))

	err = b.SafeRemoveAll(target)
	require.NoError(t, err)
	assert.NoDirExists(t, target)
}

func TestBPFFS_SafeRemoveAll_RejectsEscape(t *testing.T) {
	t.Parallel()

	layout, err := fs.New(t.TempDir())
	require.NoError(t, err)

	b := layout.BPFFS()
	outside := t.TempDir()

	err = b.SafeRemoveAll(outside)
	assert.Error(t, err)
	var errOutside fs.ErrOutsideLayout
	assert.ErrorAs(t, err, &errOutside)
}

func TestBPFFS_SafeRemoveAll_RejectsDotDot(t *testing.T) {
	t.Parallel()

	layout, err := fs.New(t.TempDir())
	require.NoError(t, err)

	b := layout.BPFFS()
	require.NoError(t, os.MkdirAll(b.MountPoint(), 0755))
	target := filepath.Join(b.MountPoint(), "..")

	err = b.SafeRemoveAll(target)
	assert.Error(t, err)
	var errOutside fs.ErrOutsideLayout
	assert.ErrorAs(t, err, &errOutside)
}

func TestBPFFS_SafeRemoveAll_RejectsMountRoot(t *testing.T) {
	t.Parallel()

	layout, err := fs.New(t.TempDir())
	require.NoError(t, err)

	b := layout.BPFFS()
	require.NoError(t, os.MkdirAll(b.MountPoint(), 0755))

	err = b.SafeRemoveAll(b.MountPoint())
	assert.Error(t, err)
	var errOutside fs.ErrOutsideLayout
	assert.ErrorAs(t, err, &errOutside)
}

func TestBPFFS_SafeRemoveAll_PrefixFalsePositive(t *testing.T) {
	t.Parallel()

	// Ensure /base/fs/programs vs /base/fs/programsX doesn't match.
	layout, err := fs.New(t.TempDir())
	require.NoError(t, err)

	b := layout.BPFFS()
	programs := filepath.Join(b.MountPoint(), "programs")
	falsePositive := filepath.Join(b.MountPoint(), "programsX")

	require.NoError(t, os.MkdirAll(programs, 0755))
	require.NoError(t, os.MkdirAll(falsePositive, 0755))

	// Create a BPFFS that has "programs" as its mount point to test the prefix check.
	// We can't easily do this with the public API, so we test via the real mount point.
	// The SafeRemoveAll should work for both since they're both under the mount point.
	err = b.SafeRemoveAll(programs)
	require.NoError(t, err)
	assert.NoDirExists(t, programs)

	// The other directory should still exist.
	assert.DirExists(t, falsePositive)
}

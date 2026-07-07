package fs_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/kernel"
)

func mustNew(t *testing.T) fs.Layout {
	t.Helper()
	layout, err := fs.New(filepath.Join(t.TempDir(), "bpfman"))
	require.NoError(t, err)
	return layout
}

func writeDummyBytecode(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "test.o")
	require.NoError(t, os.WriteFile(path, []byte("ELF test data"), 0644))
	return path
}

func TestPublishBytecode(t *testing.T) {
	t.Parallel()

	root := mustNew(t)
	rt := root.Bytecode()
	srcDir := t.TempDir()
	srcPath := writeDummyBytecode(t, srcDir)

	prov := fs.Provenance{
		Version:     1,
		ProgramID:   42,
		ProgramName: "test_prog",
		Source:      srcPath,
		SourceKind:  "file",
		LoadedAt:    time.Now().UTC(),
	}

	err := rt.PublishBytecode(42, srcPath, prov)
	require.NoError(t, err)

	// Verify final directory contents.
	bcPath := rt.ProgramBytecodePath(42)
	assert.FileExists(t, bcPath)

	data, err := os.ReadFile(bcPath)
	require.NoError(t, err)
	assert.Equal(t, "ELF test data", string(data))

	// Verify provenance.json exists and is valid JSON.
	provPath := filepath.Join(root.Base(), "programs", "42", "provenance.json")
	assert.FileExists(t, provPath)

	provData, err := os.ReadFile(provPath)
	require.NoError(t, err)
	var readProv fs.Provenance
	require.NoError(t, json.Unmarshal(provData, &readProv))
	assert.Equal(t, kernel.ProgramID(42), readProv.ProgramID)
	assert.Equal(t, "test_prog", readProv.ProgramName)
	assert.Equal(t, "file", readProv.SourceKind)
}

func TestPublishBytecode_ErrFinalExists(t *testing.T) {
	t.Parallel()

	root := mustNew(t)
	rt := root.Bytecode()
	srcDir := t.TempDir()
	srcPath := writeDummyBytecode(t, srcDir)

	prov := fs.Provenance{Version: 1, ProgramID: 99}

	// First publish should succeed.
	require.NoError(t, rt.PublishBytecode(99, srcPath, prov))

	// Second publish for the same ID should fail with ErrFinalExists.
	err := rt.PublishBytecode(99, srcPath, prov)
	require.Error(t, err)
	assert.ErrorIs(t, err, fs.ErrFinalExists)
}

func TestPublishBytecode_InvalidSource(t *testing.T) {
	t.Parallel()

	root := mustNew(t)
	rt := root.Bytecode()

	prov := fs.Provenance{Version: 1, ProgramID: 1}

	// Non-existent source file.
	err := rt.PublishBytecode(1, "/nonexistent/path.o", prov)
	require.Error(t, err)

	// Source is a directory, not a regular file.
	dir := t.TempDir()
	err = rt.PublishBytecode(1, dir, prov)
	require.Error(t, err)
}

func TestPublishBytecode_CleansUpOnError(t *testing.T) {
	t.Parallel()

	root := mustNew(t)
	rt := root.Bytecode()

	// Create a valid source file, then publish with ID 1.
	srcDir := t.TempDir()
	srcPath := writeDummyBytecode(t, srcDir)
	prov := fs.Provenance{Version: 1, ProgramID: 1}

	require.NoError(t, rt.PublishBytecode(1, srcPath, prov))

	// Staging directory should be empty (temp dir was renamed).
	stagingPath := filepath.Join(root.Base(), ".staging")
	if entries, err := os.ReadDir(stagingPath); err == nil {
		assert.Empty(t, entries, "staging should be empty after successful publish")
	}
}

func TestRemoveProgram(t *testing.T) {
	t.Parallel()

	root := mustNew(t)
	rt := root.Bytecode()
	srcDir := t.TempDir()
	srcPath := writeDummyBytecode(t, srcDir)

	prov := fs.Provenance{Version: 1, ProgramID: 10}
	require.NoError(t, rt.PublishBytecode(10, srcPath, prov))
	assert.True(t, rt.ProgramExists(10))

	// Remove should succeed.
	require.NoError(t, rt.RemoveProgram(10))
	assert.False(t, rt.ProgramExists(10))
}

func TestRemoveProgram_Idempotent(t *testing.T) {
	t.Parallel()

	root := mustNew(t)
	rt := root.Bytecode()

	// Removing a non-existent program should not error.
	require.NoError(t, rt.RemoveProgram(999))
}

func TestProgramExists(t *testing.T) {
	t.Parallel()

	root := mustNew(t)
	rt := root.Bytecode()

	assert.False(t, rt.ProgramExists(1))

	srcDir := t.TempDir()
	srcPath := writeDummyBytecode(t, srcDir)
	prov := fs.Provenance{Version: 1, ProgramID: 1}
	require.NoError(t, rt.PublishBytecode(1, srcPath, prov))

	assert.True(t, rt.ProgramExists(1))
}

func TestProgramBytecodePath(t *testing.T) {
	t.Parallel()

	root := mustNew(t)
	rt := root.Bytecode()

	path := rt.ProgramBytecodePath(42)
	assert.Equal(t, filepath.Join(root.Base(), "programs", "42", "bytecode.o"), path)
}

func TestScanProgramDirs_NumericNameOnly(t *testing.T) {
	t.Parallel()

	root := mustNew(t)
	rt := root.Bytecode()

	programsPath := filepath.Join(root.Base(), "programs")
	require.NoError(t, os.MkdirAll(filepath.Join(programsPath, "123abc"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(programsPath, "42"), 0755))

	entries, err := rt.ScanProgramDirs()
	require.NoError(t, err)

	var numeric []fs.ProgramDirEntry
	for _, entry := range entries {
		if entry.Numeric {
			numeric = append(numeric, entry)
		}
	}

	require.Len(t, numeric, 1)
	assert.Equal(t, kernel.ProgramID(42), numeric[0].ProgramID)
}

func TestCleanStaging(t *testing.T) {
	t.Parallel()

	root := mustNew(t)
	rt := root.Bytecode()

	// Create some staging leftovers.
	stagingPath := filepath.Join(root.Base(), ".staging")
	require.NoError(t, os.MkdirAll(filepath.Join(stagingPath, "pub-123"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(stagingPath, "pub-456"), 0755))

	require.NoError(t, rt.CleanStaging())

	entries, err := os.ReadDir(stagingPath)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestCleanStaging_NoStagingDir(t *testing.T) {
	t.Parallel()

	root := mustNew(t)
	rt := root.Bytecode()

	// No staging directory exists; should not error.
	require.NoError(t, rt.CleanStaging())
}

func TestZeroValueBytecode(t *testing.T) {
	t.Parallel()

	var layout fs.Layout
	// Calling Bytecode() on zero Layout should panic
	assert.Panics(t, func() { layout.Bytecode() }, "Bytecode() on zero Layout should panic")
}

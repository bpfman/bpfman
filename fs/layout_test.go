package fs_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/fs"
)

func TestNew_ValidAbsolutePaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"/run/bpfman", "/run/bpfman"},
		{"/tmp/test/bpfman", "/tmp/test/bpfman"},
		{"/var/run/bpfman", "/var/run/bpfman"},
		{"/bpfman", "/bpfman"},
	}
	for _, tt := range tests {
		layout, err := fs.New(tt.input)
		require.NoError(t, err, "New(%q)", tt.input)
		assert.Equal(t, tt.expected, layout.Base(), "New(%q)", tt.input)
	}
}

func TestNew_RejectsEmpty(t *testing.T) {
	t.Parallel()

	_, err := fs.New("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestNew_RejectsRelative(t *testing.T) {
	t.Parallel()

	relativePaths := []string{
		"run/bpfman",
		"./",
		"../",
		".",
		"..",
		"./foo",
		"../foo",
		"foo/bar",
	}
	for _, path := range relativePaths {
		_, err := fs.New(path)
		require.Error(t, err, "New(%q) should fail", path)
		assert.Contains(t, err.Error(), "absolute", "New(%q)", path)
	}
}

func TestNew_CleansPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		// Multiple slashes are cleaned
		{"//run//bpfman//", "/run/bpfman"},
		// Dot navigation
		{"/run/./bpfman", "/run/bpfman"},
		{"/run/../run/bpfman", "/run/bpfman"},
		// Trailing slashes
		{"/run/bpfman/", "/run/bpfman"},
	}
	for _, tt := range tests {
		layout, err := fs.New(tt.input)
		require.NoError(t, err, "New(%q)", tt.input)
		assert.Equal(t, tt.expected, layout.Base(), "New(%q)", tt.input)
	}
}

func TestZeroValueLayout(t *testing.T) {
	t.Parallel()

	var layout fs.Layout
	assert.False(t, layout.Valid(), "zero Layout should not be valid")

	// Methods on zero Layout should panic
	assert.Panics(t, func() { layout.Base() }, "Base() on zero Layout should panic")
	assert.Panics(t, func() { layout.DBPath() }, "DBPath() on zero Layout should panic")
	assert.Panics(t, func() { layout.SocketPath() }, "SocketPath() on zero Layout should panic")
	assert.Panics(t, func() { layout.RuntimeDirs() }, "RuntimeDirs() on zero Layout should panic")
}

func TestLayoutString(t *testing.T) {
	t.Parallel()

	// String() on zero Layout should not panic and return a safe representation
	var zero fs.Layout
	assert.Equal(t, "fs.Layout(<invalid>)", zero.String())

	// String() on valid Layout should include the path
	layout, err := fs.New("/run/bpfman")
	require.NoError(t, err)
	assert.Equal(t, "fs.Layout(/run/bpfman)", layout.String())
}

func TestRuntimeDirs(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	layout, err := fs.New(filepath.Join(parent, "bpfman"))
	require.NoError(t, err)

	dirs := layout.RuntimeDirs()
	require.Len(t, dirs, 3)
	assert.Equal(t, layout.Base(), dirs[0])
	assert.Equal(t, layout.DBDir(), dirs[1])
	assert.Equal(t, layout.SocketDir(), dirs[2])
}

func TestCSIDirs(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	layout, err := fs.New(filepath.Join(parent, "bpfman"))
	require.NoError(t, err)

	dirs := layout.CSIDirs()
	require.Len(t, dirs, 2)
	assert.Equal(t, layout.CSIDir(), dirs[0])
	assert.Equal(t, layout.CSIFSDir(), dirs[1])
}

func TestBytecode_ZeroValue(t *testing.T) {
	t.Parallel()

	var layout fs.Layout
	// Calling Bytecode() on zero Layout should panic
	assert.Panics(t, func() { layout.Bytecode() }, "Bytecode() on zero Layout should panic")
}

func TestBPFFS_ZeroValue(t *testing.T) {
	t.Parallel()

	var layout fs.Layout
	// Calling BPFFS() on zero Layout should panic
	assert.Panics(t, func() { layout.BPFFS() }, "BPFFS() on zero Layout should panic")
}

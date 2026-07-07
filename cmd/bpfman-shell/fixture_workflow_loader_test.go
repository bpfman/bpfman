package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFixtureExpectationDir_RejectsUnknownFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.bpfman"), []byte("print hi\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "expect.yaml"), []byte("stdout_linez:\n  - hi\n"), 0o644))

	_, err := readFixtureExpectationDir(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "field stdout_linez not found")
}

func TestLoadFixtureExpectationDir_RequiresExpectationFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.bpfman"), []byte("print hi\n"), 0o644))

	_, err := readFixtureExpectationDir(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "behaviour fixture must declare")
	assert.Contains(t, err.Error(), filepath.Join(dir, "expect.yaml"))
}

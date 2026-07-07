package fs_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/fs"
)

func TestScanner_DispatcherParsing_Strict(t *testing.T) {
	t.Parallel()

	layout, err := fs.New(t.TempDir())
	require.NoError(t, err)

	b := layout.BPFFS()
	require.NoError(t, os.MkdirAll(b.XDP(), 0755))

	validDir := filepath.Join(b.XDP(), "dispatcher_1_2_3")
	require.NoError(t, os.MkdirAll(validDir, 0755))
	invalidDir := filepath.Join(b.XDP(), "dispatcher_1_2_3_extra")
	require.NoError(t, os.MkdirAll(invalidDir, 0755))

	validLink := filepath.Join(b.XDP(), "dispatcher_1_2_link")
	require.NoError(t, os.WriteFile(validLink, []byte("x"), 0644))
	invalidLink := filepath.Join(b.XDP(), "dispatcher_1_x_link")
	require.NoError(t, os.WriteFile(invalidLink, []byte("x"), 0644))

	var malformed int
	scanner := b.Scanner().WithOnMalformed(func(path string, err error) {
		malformed++
	})

	state, err := scanner.Scan(context.Background())
	require.NoError(t, err)

	assert.Len(t, state.DispatcherDirs, 1)
	assert.Len(t, state.DispatcherLinkPins, 1)
	assert.Equal(t, 2, malformed)
}

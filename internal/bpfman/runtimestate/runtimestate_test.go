package runtimestate

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/fs"
)

func TestOpenMutableWaitsForRuntimeWriterLock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	layout, err := fs.New(dir)
	require.NoError(t, err)

	lockPath := filepath.Join(dir, ".lock")
	readyPath := filepath.Join(dir, "holder.ready")
	holder := exec.Command("flock", lockPath, "sh", "-c", `touch "$0"; sleep 1`, readyPath)
	holder.Stdout = os.Stdout
	holder.Stderr = os.Stderr
	require.NoError(t, holder.Start())
	t.Cleanup(func() {
		_ = holder.Process.Kill()
		_ = holder.Wait()
	})
	require.Eventually(t, func() bool {
		_, err := os.Stat(readyPath)
		return err == nil
	}, time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		opened, err := OpenMutable(context.Background(), layout, logger, 10*time.Millisecond)
		if opened != nil {
			_ = opened.Close()
		}
		return err != nil && strings.Contains(err.Error(), "timed out waiting for lock")
	}, time.Second, time.Millisecond)
	_, err = os.Stat(filepath.Join(dir, "store.db"))
	require.True(t, os.IsNotExist(err), "database was created while runtime lock was held")
}

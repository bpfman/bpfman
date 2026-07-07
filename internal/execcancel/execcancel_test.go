package execcancel

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfigureSendsInterruptToGrandchildInProcessGroup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	ack := filepath.Join(dir, "ack")
	grandchild := filepath.Join(dir, "grandchild.sh")
	require.NoError(t, os.WriteFile(grandchild, []byte(`#!/bin/sh
trap 'echo interrupted > "$1"; exit 0' INT
echo ready > "$2"
while :; do sleep 1; done
`), 0o755))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", `"$1" "$2" "$3"; :`, "sh", grandchild, ack, ready)
	cancelled := Configure(cmd)

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Run()
	}()

	require.Eventually(t, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	}, time.Second, 20*time.Millisecond)

	cancel()

	select {
	case err := <-errCh:
		require.Error(t, err)
		require.True(t, cancelled.Load(), "cancel callback did not signal the process group")
	case <-time.After(time.Second):
		t.Fatal("command did not exit after cancellation")
	}

	require.Eventually(t, func() bool {
		_, err := os.Stat(ack)
		return err == nil
	}, time.Second, 20*time.Millisecond)
}

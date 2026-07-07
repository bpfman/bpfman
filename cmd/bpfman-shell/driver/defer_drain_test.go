package driver

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/internal/cli"
)

// TestCancelledRun_DrainsDefersUnderCleanupContext pins the
// interrupt-cleanup contract: cancelling the root context mid-script
// (operator ^C, runner failfast abort, script timeout) aborts the
// in-flight statement, but the program-level defers still execute,
// under a fresh bounded context detached from the cancellation.
// Without that detachment every deferred action dies immediately
// with "context canceled" and the script leaks whatever its defers
// guarded.
func TestCancelledRun_DrainsDefersUnderCleanupContext(t *testing.T) {
	t.Parallel()

	marker := filepath.Join(t.TempDir(), "defer-ran")
	src := fmt.Sprintf("defer exec touch %s\nguard _ <- exec sleep 30\n", marker)

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel(fmt.Errorf("interrupted by signal: %w", context.Canceled))
	}()

	cli := &cli.CLI{Out: io.Discard, Err: io.Discard}
	start := time.Now()
	err := Run(ctx, Config{
		CLI:        cli,
		LineReader: NewScannerReader(strings.NewReader(src), nil),
		File:       "cancel-drain.bpfman",
	})
	elapsed := time.Since(start)

	require.Error(t, err, "a cancelled run must not report success")
	require.Less(t, elapsed, 10*time.Second, "cancellation must abort the sleep, not wait it out")

	_, statErr := os.Stat(marker)
	require.NoError(t, statErr, "deferred command must run under the cleanup context after cancellation")
}

// TestCancelledRun_DeferDrainIsBounded pins the other half of the
// contract: the cleanup context is time-boxed, so a deferred command
// that blocks cannot make interrupt shutdown unbounded.
//
//nolint:paralleltest // mutates the package-level deferDrainBudget; cannot run in parallel.
func TestCancelledRun_DeferDrainIsBounded(t *testing.T) {
	prev := deferDrainBudget
	deferDrainBudget = 500 * time.Millisecond
	t.Cleanup(func() { deferDrainBudget = prev })

	src := "defer exec sleep 30\nguard _ <- exec sleep 30\n"

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel(fmt.Errorf("interrupted by signal: %w", context.Canceled))
	}()

	cli := &cli.CLI{Out: io.Discard, Err: io.Discard}
	start := time.Now()
	err := Run(ctx, Config{
		CLI:        cli,
		LineReader: NewScannerReader(strings.NewReader(src), nil),
		File:       "cancel-drain-bounded.bpfman",
	})
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Less(t, elapsed, 5*time.Second, "a blocking defer must be cut off by the drain budget, not waited out")
}

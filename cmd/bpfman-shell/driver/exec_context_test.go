package driver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/internal/cli"
)

func TestRunExternal_ReturnsContextDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := RunExternal(ctx, shellSleepArgs("5"))
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded), "got %v", err)
	assert.Less(t, elapsed, time.Second)
}

func TestRunExternal_ReturnsContextCause(t *testing.T) {
	t.Parallel()

	cause := fmt.Errorf("test command budget expired: %w", context.DeadlineExceeded)
	ctx, cancel := context.WithTimeoutCause(t.Context(), 50*time.Millisecond, cause)
	defer cancel()

	_, err := RunExternal(ctx, shellSleepArgs("5"))

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, cause, err)
}

func TestRunExternalInherit_ReturnsContextDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	cli := &cli.CLI{Out: io.Discard, Err: io.Discard}
	start := time.Now()
	_, _, err := RunExternalInherit(ctx, cli, shellSleepArgs("5"))
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded), "got %v", err)
	assert.Less(t, elapsed, time.Second)
}

func TestRunExternal_AllowsInterruptHandlerToFinish(t *testing.T) {
	t.Parallel()

	ack := filepath.Join(t.TempDir(), "ack")
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := RunExternal(ctx, []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.QuotedArg{Text: "trap 'sleep 0.4; echo cleaned > \"$1\"; exit 0' INT; sleep 5"},
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: ack},
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Eventually(t, func() bool {
		_, statErr := os.Stat(ack)
		return statErr == nil
	}, time.Second, 20*time.Millisecond)
}

func shellSleepArgs(delay string) []runtime.Arg {
	return []runtime.Arg{
		runtime.WordArg{Text: "sh"},
		runtime.WordArg{Text: "-c"},
		runtime.QuotedArg{Text: "sleep " + delay},
	}
}

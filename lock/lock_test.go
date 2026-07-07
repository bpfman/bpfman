package lock_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/lock"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestRunCreatesLockDirOnFirstTouch covers the case where the
// lockfile's parent directory does not yet exist (no daemon has
// initialised the runtime root). acquireWriter must MkdirAll the
// parent so first-touch CLI invocations and scripted scenarios
// succeed, matching the existing O_CREATE behaviour for the lock
// file itself.
func TestRunCreatesLockDirOnFirstTouch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lockPath := filepath.Join(root, "deep", "missing", "parent", ".lock")

	err := lock.Run(context.Background(), lockPath, func(ctx context.Context, _ lock.WriterScope) error {
		return nil
	})
	require.NoError(t, err)
}

// TestRunPanicsOnSamePathReentry proves the deadlock tripwire: a nested
// Run for a path the context already holds is a programmer error (it
// would otherwise EWOULDBLOCK against the held flock until the deadline),
// so Run panics immediately rather than reusing or blocking. Callers
// already holding the path thread the held WriterScope to the callee.
func TestRunPanicsOnSamePathReentry(t *testing.T) {
	t.Parallel()

	lockPath := filepath.Join(t.TempDir(), ".lock")

	err := lock.Run(context.Background(), lockPath, func(ctx context.Context, _ lock.WriterScope) error {
		require.Panics(t, func() {
			_ = lock.Run(ctx, lockPath, func(context.Context, lock.WriterScope) error {
				return nil
			})
		})
		return nil
	})
	require.NoError(t, err)
}

// TestRunAllowsNestedDifferentLockPath proves the tripwire is path-scoped:
// holding one lock and acquiring a genuinely different path nests cleanly,
// since the two flocks cannot self-contend.
func TestRunAllowsNestedDifferentLockPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outer := filepath.Join(dir, "outer.lock")
	inner := filepath.Join(dir, "inner.lock")

	var innerRan bool
	err := lock.Run(context.Background(), outer, func(ctx context.Context, _ lock.WriterScope) error {
		return lock.Run(ctx, inner, func(context.Context, lock.WriterScope) error {
			innerRan = true
			return nil
		})
	})
	require.NoError(t, err)
	require.True(t, innerRan)
}

// TestRunEscapedContextDoesNotTripAfterRelease proves the tripwire is
// liveness-aware: a context that escapes its callback and outlives Run
// must not report the lock as held once it has been released, so reusing
// that context for a later same-path Run acquires cleanly rather than
// panicking on a stale breadcrumb.
func TestRunEscapedContextDoesNotTripAfterRelease(t *testing.T) {
	t.Parallel()

	lockPath := filepath.Join(t.TempDir(), ".lock")

	var escaped context.Context
	err := lock.Run(context.Background(), lockPath, func(ctx context.Context, _ lock.WriterScope) error {
		escaped = ctx
		return nil
	})
	require.NoError(t, err)

	var ran bool
	require.NotPanics(t, func() {
		err = lock.Run(escaped, lockPath, func(context.Context, lock.WriterScope) error {
			ran = true
			return nil
		})
	})
	require.NoError(t, err)
	require.True(t, ran)
}

func TestRunWithTimeoutSucceeds(t *testing.T) {
	t.Parallel()

	lockPath := filepath.Join(t.TempDir(), ".lock")

	var ran bool
	err := lock.RunWithTimeout(context.Background(), lockPath, testLogger(), time.Second, func(context.Context, lock.WriterScope) error {
		ran = true
		return nil
	})
	require.NoError(t, err)
	require.True(t, ran)
}

func TestRunWithTimeoutReturnsCallbackError(t *testing.T) {
	t.Parallel()

	lockPath := filepath.Join(t.TempDir(), ".lock")
	wantErr := errors.New("callback failed")

	err := lock.RunWithTimeout(context.Background(), lockPath, testLogger(), time.Second, func(context.Context, lock.WriterScope) error {
		return wantErr
	})
	require.ErrorIs(t, err, wantErr)
}

func TestRunWithTimeoutReturnsCallbackErrorAfterTimeout(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		lockPath := filepath.Join(t.TempDir(), ".lock")
		wantErr := errors.New("callback failed after timeout")

		err := lock.RunWithTimeout(context.Background(), lockPath, testLogger(), 10*time.Millisecond, func(context.Context, lock.WriterScope) error {
			time.Sleep(20 * time.Millisecond)
			return wantErr
		})
		require.ErrorIs(t, err, wantErr)

		var timeoutErr *lock.TimeoutError
		require.False(t, errors.As(err, &timeoutErr))
	})
}

// TestRunWithTimeoutDoesNotCancelCriticalSection proves the acquisition budget
// bounds only the wait for the lock: once acquired, the critical section runs
// under the caller's context, so the budget neither cancels it nor relabels it
// as a lock timeout, even when the work outlives the budget.
func TestRunWithTimeoutDoesNotCancelCriticalSection(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		lockPath := filepath.Join(t.TempDir(), ".lock")

		const budget = 10 * time.Millisecond
		var ctxErrDuringWork error
		ran := false
		err := lock.RunWithTimeout(context.Background(), lockPath, testLogger(), budget, func(ctx context.Context, _ lock.WriterScope) error {
			// Uncontended: acquired at once. Hold the critical section well
			// past the acquisition budget.
			time.Sleep(5 * budget)
			ctxErrDuringWork = ctx.Err()
			ran = true
			return nil
		})
		require.True(t, ran)
		require.NoError(t, err, "a slow critical section must not surface as a writer-lock timeout")
		require.NoError(t, ctxErrDuringWork, "the acquisition budget must not cancel the critical section")
	})
}

// TestRunWithTimeoutPanicsOnSamePathReentry proves the acquisition/work
// context split in RunWithTimeout preserves the re-entry tripwire: the
// callback runs under the breadcrumb-bearing work context, so a nested
// RunWithTimeout for the same path still fails fast rather than
// self-deadlocking on the flock.
func TestRunWithTimeoutPanicsOnSamePathReentry(t *testing.T) {
	t.Parallel()

	lockPath := filepath.Join(t.TempDir(), ".lock")

	err := lock.RunWithTimeout(context.Background(), lockPath, testLogger(), time.Second, func(ctx context.Context, _ lock.WriterScope) error {
		require.Panics(t, func() {
			_ = lock.RunWithTimeout(ctx, lockPath, testLogger(), time.Second, func(context.Context, lock.WriterScope) error {
				return nil
			})
		})
		return nil
	})
	require.NoError(t, err)
}

// TestRunWithTimeoutAllowsNestedDifferentPath proves the tripwire stays
// path-scoped through the split: holding one lock and acquiring a genuinely
// different path via RunWithTimeout nests cleanly.
func TestRunWithTimeoutAllowsNestedDifferentPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outer := filepath.Join(dir, "outer.lock")
	inner := filepath.Join(dir, "inner.lock")

	var innerRan bool
	err := lock.RunWithTimeout(context.Background(), outer, testLogger(), time.Second, func(ctx context.Context, _ lock.WriterScope) error {
		return lock.RunWithTimeout(ctx, inner, testLogger(), time.Second, func(context.Context, lock.WriterScope) error {
			innerRan = true
			return nil
		})
	})
	require.NoError(t, err)
	require.True(t, innerRan)
}

func TestRunWithTimeoutReturnsTypedTimeoutError(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		lockPath := filepath.Join(t.TempDir(), ".lock")

		held := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- lock.Run(context.Background(), lockPath, func(context.Context, lock.WriterScope) error {
				close(held)
				<-release
				return nil
			})
		}()
		<-held

		const timeout = 10 * time.Millisecond
		var ran bool
		err := lock.RunWithTimeout(context.Background(), lockPath, testLogger(), timeout, func(context.Context, lock.WriterScope) error {
			ran = true
			return nil
		})
		require.False(t, ran)

		var timeoutErr *lock.TimeoutError
		require.ErrorAs(t, err, &timeoutErr)
		require.Equal(t, lockPath, timeoutErr.Path)
		require.Equal(t, timeout, timeoutErr.Timeout)

		close(release)
		require.NoError(t, <-done)
	})
}

func TestRunWithTimeoutAcquiresAfterRelease(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		lockPath := filepath.Join(t.TempDir(), ".lock")

		held := make(chan struct{})
		release := make(chan struct{})
		holderDone := make(chan error, 1)
		go func() {
			holderDone <- lock.Run(context.Background(), lockPath, func(context.Context, lock.WriterScope) error {
				close(held)
				<-release
				return nil
			})
		}()
		<-held

		waiterDone := make(chan error, 1)
		// ran is written by the waiter goroutine and read here; the only
		// runtime ordering between them is the flock, which the race detector
		// does not model as a happens-before edge, so it must be atomic.
		var ran atomic.Bool
		go func() {
			waiterDone <- lock.RunWithTimeout(context.Background(), lockPath, testLogger(), 2*time.Second, func(context.Context, lock.WriterScope) error {
				ran.Store(true)
				return nil
			})
		}()

		synctest.Wait()
		require.False(t, ran.Load())

		close(release)
		require.NoError(t, <-waiterDone)
		require.True(t, ran.Load())
		require.NoError(t, <-holderDone)
	})
}

func TestRunWithTimeoutDoesNotTranslateParentDeadline(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		lockPath := filepath.Join(t.TempDir(), ".lock")

		held := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- lock.Run(context.Background(), lockPath, func(context.Context, lock.WriterScope) error {
				close(held)
				<-release
				return nil
			})
		}()
		<-held

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()

		var ran bool
		err := lock.RunWithTimeout(ctx, lockPath, testLogger(), 30*time.Second, func(context.Context, lock.WriterScope) error {
			ran = true
			return nil
		})
		require.False(t, ran)
		require.ErrorIs(t, err, context.DeadlineExceeded)

		var timeoutErr *lock.TimeoutError
		require.False(t, errors.As(err, &timeoutErr))

		close(release)
		require.NoError(t, <-done)
	})
}

func TestRunWithTimeoutZeroDoesNotTranslateParentCancellation(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		lockPath := filepath.Join(t.TempDir(), ".lock")

		held := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- lock.Run(context.Background(), lockPath, func(context.Context, lock.WriterScope) error {
				close(held)
				<-release
				return nil
			})
		}()
		<-held

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := lock.RunWithTimeout(ctx, lockPath, testLogger(), 0, func(context.Context, lock.WriterScope) error {
			return nil
		})
		require.ErrorIs(t, err, context.Canceled)

		var timeoutErr *lock.TimeoutError
		require.False(t, errors.As(err, &timeoutErr))

		close(release)
		require.NoError(t, <-done)
	})
}

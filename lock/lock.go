// Package lock provides a cross-process global writer lock using flock(2)
// to protect all mutations of /run/bpfman/... state.
//
// Design principle: "Illegal states unrepresentable" - use a non-forgeable
// scope token that proves the lock is held. Mutating operations require
// this token (compiler enforced). The context never carries the
// capability itself -- only a breadcrumb that lets Run fail fast on
// same-path re-entry.
//
// There are two ways to obtain proof of the lock:
//
//  1. Parents call Run(...) and receive a WriterScope capability.
//  2. Helpers receive a dup'd fd and call InheritedLockFromFD(...).
//
// Helpers never attempt to acquire the lock from a path; they only accept
// an inherited fd. This prevents subtle deadlocks and ensures namespace
// helpers cannot run without lock coverage.
package lock

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
)

// WriterLockFDEnvVar is the environment variable used to pass the lock
// file descriptor to child processes (e.g., bpfman-ns helper).
const WriterLockFDEnvVar = "BPFMAN_WRITER_LOCK_FD"

// WriterScope represents the dynamic execution region in which the
// global bpfman writer lock is held.
//
// Possession of a WriterScope is proof that the caller holds exclusive
// write access to /run/bpfman/... state. WriterScope is a capability, not
// a mutex: it cannot be constructed, locked, or unlocked by callers.
//
// A WriterScope is only obtained by executing code under lock.Run(...).
// The interface cannot be implemented outside this package due to the
// unexported marker method.
type WriterScope interface {
	// DupFD duplicates the lock fd for passing to a child process.
	// The child inherits the lock via the duped fd.
	DupFD() (*os.File, error)

	// FD returns the raw lock file descriptor (for logging/diagnostics).
	FD() int

	// writerScopeMarker is unexported to prevent external implementations.
	writerScopeMarker()
}

// writerScope is the concrete implementation of WriterScope.
// It holds the exclusive flock and cannot be constructed outside this package.
type writerScope struct {
	f *os.File
}

func (*writerScope) writerScopeMarker() {}

// FD returns the raw lock file descriptor.
func (s *writerScope) FD() int {
	return int(s.f.Fd())
}

// DupFD duplicates the lock fd for passing to a child process.
func (s *writerScope) DupFD() (*os.File, error) {
	dup, err := syscall.Dup(s.FD())
	if err != nil {
		return nil, fmt.Errorf("dup lock fd: %w", err)
	}

	return os.NewFile(uintptr(dup), "bpfman-writer-lock"), nil
}

// Run acquires the global writer lock, executes fn, then releases.
// The WriterScope proves to callees that the lock is held.
// Uses LOCK_EX|LOCK_NB with exponential backoff, respects ctx cancellation.
//
// Re-acquiring a path this context already holds would deadlock: a fresh
// acquisition opens a new file description, which flock(2) treats
// independently from the one already held. That can only be a caller that
// should have threaded its WriterScope, so fail fast.
//
// The marker is per-Run: its frame goes inactive when Run returns, so a
// context that escapes its callback (e.g. handed to a goroutine) will not
// falsely trip this check once the lock has been released.
func Run(ctx context.Context, lockPath string, fn func(context.Context, WriterScope) error) error {
	return run(ctx, ctx, lockPath, fn)
}

// run acquires the lock under acquireCtx and executes fn under workCtx. Run
// passes the same context for both; RunWithTimeout passes a deadline-bounded
// acquireCtx and the original workCtx, so its timeout bounds only acquisition
// and a slow critical section is not cancelled by the acquisition budget. The
// re-entry breadcrumb and the context fn observes both derive from workCtx.
func run(acquireCtx, workCtx context.Context, lockPath string, fn func(context.Context, WriterScope) error) error {
	held, _ := workCtx.Value(heldLocksKey{}).(*heldLocks)
	if held.holds(lockPath) {
		panic(fmt.Sprintf("lock.Run re-entered for already-held path %q: thread the WriterScope to the callee instead of re-acquiring", lockPath))
	}

	f, err := acquireWriter(acquireCtx, lockPath)
	if err != nil {
		return err
	}

	defer f.Close()

	frame := &heldLocks{path: lockPath, parent: held}
	frame.active.Store(true)
	defer frame.active.Store(false)

	workCtx = context.WithValue(workCtx, heldLocksKey{}, frame)
	return fn(workCtx, &writerScope{f: f})
}

// heldLocks is a context-scoped stack of lock frames held in the current
// call tree, used only by Run's same-path re-entry tripwire. It is not the
// lock capability; that remains the passed WriterScope. The active flag is
// liveness only -- it gates the debug breadcrumb, never authority -- and
// goes false when its Run returns so an escaped context cannot report a
// released lock as still held.
type heldLocks struct {
	path   string
	active atomic.Bool
	parent *heldLocks
}

type heldLocksKey struct{}

// holds reports whether path is held by a still-active frame anywhere in
// the stack. The nil receiver (no lock held yet) reports false.
func (h *heldLocks) holds(path string) bool {
	for n := h; n != nil; n = n.parent {
		if n.path == path && n.active.Load() {
			return true
		}
	}
	return false
}

// RunWithTiming wraps Run with timing logs for lock acquisition and release.
// The logger parameter is required; use Run directly if logging is not needed.
// Logs are tagged with component=lock for selective filtering.
func RunWithTiming(ctx context.Context, lockPath string, logger *slog.Logger, fn func(context.Context, WriterScope) error) error {
	return runWithTiming(ctx, ctx, lockPath, logger, fn)
}

// runWithTiming is RunWithTiming with split acquisition/work contexts; see run.
func runWithTiming(acquireCtx, workCtx context.Context, lockPath string, logger *slog.Logger, fn func(context.Context, WriterScope) error) error {
	logger = logger.With("component", "lock")
	start := time.Now()
	return run(acquireCtx, workCtx, lockPath, func(ctx context.Context, scope WriterScope) error {
		acquired := time.Now()
		logger.DebugContext(ctx, "lock acquired", "path", lockPath, "wait_ms", acquired.Sub(start).Milliseconds())
		defer func() {
			logger.DebugContext(ctx, "lock released", "path", lockPath, "held_ms", time.Since(acquired).Milliseconds())
		}()
		return fn(ctx, scope)
	})
}

// TimeoutError reports that a writer lock was not acquired before
// the configured timeout elapsed.
type TimeoutError struct {
	// Path is the lock file path whose acquisition timed out.
	Path string

	// Timeout is the wait budget that elapsed before the lock could be
	// acquired.
	Timeout time.Duration
}

// Error returns a message naming the lock path and the elapsed timeout.
func (e *TimeoutError) Error() string {
	return fmt.Sprintf("timed out waiting for lock %s after %v", e.Path, e.Timeout)
}

var errLockTimeout = errors.New("writer lock timeout")

// RunWithTimeout runs fn under the writer lock, bounding only the wait to
// acquire the lock by timeout; a zero timeout waits indefinitely. fn runs
// under the caller's ctx, so a slow critical section is never cancelled by the
// timeout. A *TimeoutError is returned only for a genuine failure to acquire;
// an inherited parent deadline is returned unchanged rather than relabelled as
// a lock timeout.
func RunWithTimeout(
	ctx context.Context,
	lockPath string,
	logger *slog.Logger,
	timeout time.Duration,
	fn func(context.Context, WriterScope) error,
) error {
	acquireCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		acquireCtx, cancel = context.WithTimeoutCause(ctx, timeout, errLockTimeout)
		defer cancel()
	}
	// The timeout bounds acquisition only: acquireCtx drives the wait, while
	// fn runs under the caller's ctx, so once the lock is held the budget can
	// neither cancel nor relabel the critical section.
	acquired := false
	err := runWithTiming(acquireCtx, ctx, lockPath, logger, func(ctx context.Context, scope WriterScope) error {
		acquired = true
		return fn(ctx, scope)
	})
	// Translate to a TimeoutError only for a genuine failure to ACQUIRE: the
	// deadline fired before the callback was entered (!acquired) and it was
	// our own acquisition timeout, not an inherited parent deadline (the
	// cause check).
	if err != nil && !acquired && timeout > 0 && errors.Is(err, context.DeadlineExceeded) && errors.Is(context.Cause(acquireCtx), errLockTimeout) {
		return &TimeoutError{Path: lockPath, Timeout: timeout}
	}
	return err
}

// acquireWriter opens the lock file and acquires exclusive lock.
// The parent directory is created on demand so first-touch
// invocations (no daemon has set up the runtime root yet) succeed
// in the same way O_CREATE handles the lock file itself.
func acquireWriter(ctx context.Context, path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("ensure lock dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	// Polling-style retry instead of a blocking flock so ctx
	// cancellation is honoured. Start at 1ms (an uncontended lock
	// is free immediately, and a contended one usually clears in a
	// handful of milliseconds because the work-under-lock at the
	// other end is typically a few sqlite writes plus a kernel-side
	// op of similar order). Double on every miss, capped at 500ms,
	// so deep queues do not spin hot but the common case sees
	// near-instant pickup as soon as the lock is released.
	backoff := 1 * time.Millisecond
	const maxBackoff = 500 * time.Millisecond

	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if err != syscall.EWOULDBLOCK {
			f.Close()
			return nil, fmt.Errorf("flock: %w", err)
		}

		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(backoff):
		}

		if backoff < maxBackoff {
			backoff *= 2
		}
	}
}

// InheritedLock represents a writer lock inherited by a helper process.
// Unlike WriterScope (which is managed by Run), InheritedLock is closeable
// because the helper genuinely owns this fd for its lifetime.
type InheritedLock struct {
	f *os.File
}

// InheritedLockFromFD creates an InheritedLock from an already-held lock fd.
// Used by helper processes that receive the lock fd via ExtraFiles.
// Verifies the fd actually holds the lock.
//
// LIMITATION: flock(LOCK_EX|LOCK_NB) can only verify "I can hold EX now",
// not "parent definitely held it before passing". This is acceptable:
// - Parent MUST acquire before spawning (enforced by type system)
// - Helper must hold the lock regardless of how it got it
// - If parent didn't hold it, helper now does (still correct)
func InheritedLockFromFD(fd int) (*InheritedLock, error) {
	f := os.NewFile(uintptr(fd), "bpfman-writer-lock")
	if f == nil {
		return nil, fmt.Errorf("invalid fd %d", fd)
	}

	// Verify we can hold the lock exclusively.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("fd %d does not hold writer lock: %w", fd, err)
	}

	return &InheritedLock{f: f}, nil
}

// Close releases the lock. Called by helpers when done.
func (l *InheritedLock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	return l.f.Close()
}

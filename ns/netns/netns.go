// Package netns provides network namespace identification and switching functions.
package netns

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// CurrentNSID returns the inode number of the network namespace
// this process was started in, captured the first time any caller
// asks.
//
// Despite its name, this does NOT read the calling thread's current
// netns. Per-thread reads of /proc/self/ns/net are unsafe under
// heavy parallel netns activity (an upstream library that fails to
// restore on its way out can leave a Go OS thread in a non-root
// netns, and the next goroutine that lands on it will read the
// wrong value). Every caller in this codebase wants the same
// source of truth bpfman uses to key its dispatcher records --
// which is the process-startup netns -- so returning that here
// closes the gap between storage-side and lookup-side keys.
//
// The capture is done lazily under a sync.Once rather than at
// package init. The contamination defence still works because the
// daemon, the CLI, and the test suite all reach this function from
// their main goroutine well before any worker goroutine has had a
// chance to call setns; the first call lands on a still-pristine
// thread and that result is cached for everyone else. The lazy
// shape matters for binaries that share this package transitively
// but legitimately run in environments where /proc may not be
// mounted at startup -- the uprobe helper subprocess, after the C
// constructor switches it into the target container's mount
// namespace, is the motivating case. The helper never calls this
// function, so the stat never runs and the helper does not crash
// in package init.
//
// Callers that genuinely need "this thread's current netns" must
// stat /proc/self/ns/net or /proc/<tid>/ns/net themselves.
func CurrentNSID() (uint64, error) {
	return getProcessNSID()
}

// NSID returns the inode number of the network namespace at the
// given path. If path is empty, returns the netns the process was
// started in (captured on first call), NOT the calling thread's
// current netns. The latter is per-thread and can be poisoned by
// upstream library bugs in concurrent programs; reading the
// captured process value insulates the caller from that hazard.
// Callers wanting "this thread's current netns" must explicitly
// stat /proc/self/ns/net or /proc/<tid>/ns/net themselves.
func NSID(path string) (uint64, error) {
	if path == "" {
		return getProcessNSID()
	}
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		return 0, fmt.Errorf("stat %s: %w", path, err)
	}
	return stat.Ino, nil
}

// processNSID caches the netns inode captured on first call.
// processNSIDErr remembers the failure mode if the capture stat
// failed, so subsequent callers see the same error instead of the
// stat being retried -- a retry might land on a different thread
// in a different netns and silently return a stale value, which
// is exactly what the capture is meant to prevent.
var (
	processNSIDOnce sync.Once
	processNSID     uint64
	processNSIDErr  error
)

func getProcessNSID() (uint64, error) {
	processNSIDOnce.Do(func() {
		var stat syscall.Stat_t
		if err := syscall.Stat("/proc/self/ns/net", &stat); err != nil {
			processNSIDErr = fmt.Errorf("netns: stat /proc/self/ns/net: %w", err)
			return
		}
		processNSID = stat.Ino
	})
	return processNSID, processNSIDErr
}

// Run executes fn in the network namespace specified by path.
// If path is empty, fn is executed in the current namespace (no switch).
// The original namespace is restored after fn returns, even if fn panics.
//
// FAILURE SEMANTICS:
//
// On the success path the calling OS thread is locked while fn
// runs and unlocked on return. If the deferred restore-to-the-
// original-netns fails (rare; would mean the kernel rejected
// setns back to the originally-open fd), the thread is in the
// target netns. To prevent that thread from being returned to
// Go's scheduler -- where the next goroutine that lands on it
// would inherit the wrong netns identity, silently corrupting
// any code that reads /proc/self/ns/net (which is per-thread)
// -- this function panics. The panic propagates out of Run,
// unwinds the goroutine, and the outer defer below skips
// runtime.UnlockOSThread (safeUnlock stays false). Go's runtime
// retires the thread on goroutine exit (see
// runtime.LockOSThread).
//
// Panic is the right escalation here because the alternative --
// silently returning while the goroutine remains pinned to a
// poisoned thread -- lets the goroutine continue doing work
// against the wrong netns until it happens to exit, leaking
// state corruption all the while. Loud failure beats quiet
// rot.
//
// In normal operation the restore succeeds, no panic fires,
// and the OS thread is unlocked cleanly.
//
// Usage:
//
//	err := netns.Run("/var/run/netns/target", func() error {
//	    // operations in target namespace
//	    return nil
//	})
func Run(path string, fn func() error) error {
	if path == "" {
		return fn()
	}

	runtime.LockOSThread()
	safeUnlock := false
	defer func() {
		if safeUnlock {
			runtime.UnlockOSThread()
		}
	}()

	// Open current namespace for restoration
	originalNS, err := os.Open("/proc/self/ns/net")
	if err != nil {
		safeUnlock = true // thread did not move
		return fmt.Errorf("open current netns: %w", err)
	}

	defer originalNS.Close()

	// Open target namespace
	targetNS, err := os.Open(path)
	if err != nil {
		safeUnlock = true // thread did not move
		return fmt.Errorf("open target netns %s: %w", path, err)
	}

	defer targetNS.Close()

	// Switch to target namespace
	if err := unix.Setns(int(targetNS.Fd()), unix.CLONE_NEWNET); err != nil {
		safeUnlock = true // setns failed; thread did not move
		return fmt.Errorf("setns to target netns: %w", err)
	}

	// Restore original namespace on return, even if fn panics.
	// On restore success, flip safeUnlock so the outer defer
	// runs UnlockOSThread. On restore failure, panic: the OS
	// thread is in a non-root netns and must not be returned
	// to the scheduler. The panic propagates out of Run,
	// unwinds the goroutine; safeUnlock stays false so the
	// outer defer skips the unlock; Go's runtime retires the
	// thread on goroutine exit.
	defer func() {
		if err := unix.Setns(int(originalNS.Fd()), unix.CLONE_NEWNET); err != nil {
			panic(fmt.Errorf("netns.Run: failed to restore original netns; OS thread is in target netns and cannot be safely returned to the scheduler: %w", err))
		}
		safeUnlock = true
	}()

	return fn()
}

// SPDX-License-Identifier: Apache-2.0

package netns

import (
	"fmt"
	"runtime"

	vishvananda "github.com/vishvananda/netns"
)

// CreateNamed creates a named netns at /run/netns/<name>.
//
// The calling goroutine is left in the netns it was in on
// entry. The function locks the calling OS thread internally
// to perform the kernel operations safely; on success the
// thread is unlocked before returning.
//
// FAILURE SEMANTICS:
//
// If creating the new netns fails (the kernel rejected the
// unshare/mount that NewNamed performs), the function panics:
// NewNamed may have already moved the thread into a
// partially-created netns, and silently returning would put a
// poisoned thread back into Go's scheduler.
//
// If restoring the original netns fails (rare; would mean the
// kernel rejected setns back to the originally-open fd), the
// function panics for the same reason: the thread is in the
// named netns and cannot be safely returned to the scheduler.
//
// The panic propagates out of CreateNamed, unwinds the
// goroutine, and the deferred unlock guard skips
// runtime.UnlockOSThread (because safeUnlock stays false).
// Go's runtime retires the thread on goroutine exit (see
// runtime.LockOSThread). Loud failure beats quiet rot:
// returning while the goroutine remains pinned to a poisoned
// thread would let it continue doing work against the wrong
// netns, leaking state corruption all the while.
//
// In normal operation the create and restore both succeed, no
// panic fires, and the OS thread is unlocked cleanly.
func CreateNamed(name string) error {
	runtime.LockOSThread()
	safeUnlock := false
	defer func() {
		if safeUnlock {
			runtime.UnlockOSThread()
		}
	}()

	origNs, err := vishvananda.Get()
	if err != nil {
		// Thread did not move; safe to unlock.
		safeUnlock = true
		return fmt.Errorf("get current netns: %w", err)
	}

	defer origNs.Close()

	newNs, err := vishvananda.NewNamed(name)
	if err != nil {
		// NewNamed may have moved the thread into a
		// partially-created netns; the OS thread state is
		// indeterminate. Panic so the goroutine unwinds and
		// the runtime retires the thread.
		panic(fmt.Errorf("netns.CreateNamed: NewNamed(%s) failed and may have left this OS thread in a partially-created netns; cannot safely continue: %w", name, err))
	}

	newNs.Close()

	if err := vishvananda.Set(origNs); err != nil {
		// Restore failed; thread is in the named netns.
		// Same reasoning as the NewNamed branch above.
		panic(fmt.Errorf("netns.CreateNamed: failed to restore original netns; OS thread is in named netns %q and cannot be safely returned to the scheduler: %w", name, err))
	}

	// Restore succeeded; thread is back at the original netns.
	safeUnlock = true
	return nil
}

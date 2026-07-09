// Test-fixture mode for the LSM family of e2e/scripts. When
// BPFMAN_SHELL_MODE=lsm-open-worker, bpfman-shell runs as a short-lived
// worker that sets its own comm to a caller-chosen marker and then opens
// a target file N times. Each open fires the security_file_open LSM
// hook; an LSM program that filters file_open by that marker comm counts
// exactly these opens, isolated from the host's own file activity.
//
// The comm is set through /proc/thread-self/comm after locking the
// goroutine to its OS thread, so the marker names the exact thread the
// opens run on. The Go runtime's own startup file activity happens
// before the marker is set and under the binary's default comm, so it is
// not attributed to the marker.
package fixturemode

import (
	"fmt"
	"os"
	"runtime"
	"strconv"

	"golang.org/x/sys/unix"
)

func runLsmOpenWorker(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("lsm-open-worker: usage: MARKER FILE COUNT (got %d args)", len(args))
	}
	marker := args[0]
	file := args[1]
	n, err := strconv.Atoi(args[2])
	if err != nil {
		return fmt.Errorf("lsm-open-worker: invalid COUNT %q: %w", args[2], err)
	}

	if n < 0 {
		return fmt.Errorf("lsm-open-worker: COUNT must not be negative (got %d)", n)
	}

	// Pin to the OS thread so the comm we set and the opens we make are
	// the same task the LSM program observes. Never unlocked: this is a
	// throwaway worker process.
	runtime.LockOSThread()

	// Setting comm opens /proc/thread-self/comm first -- that open runs
	// under the old comm, before the marker takes effect, so it is not
	// counted.
	if err := os.WriteFile("/proc/thread-self/comm", []byte(marker), 0o644); err != nil {
		return fmt.Errorf("lsm-open-worker: set comm %q: %w", marker, err)
	}

	for i := range n {
		fd, err := unix.Open(file, unix.O_RDONLY, 0)
		if err != nil {
			return fmt.Errorf("lsm-open-worker: open %s (%d/%d): %w", file, i+1, n, err)
		}
		_ = unix.Close(fd)
	}
	return nil
}

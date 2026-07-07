// Test-fixture mode for the e2e/scripts translations that need
// a stable-PID worker firing unlinkat(2) syscalls. Unlinkat fires
// sys_enter_unlinkat and sys_exit_unlinkat tracepoints.
//
// Calling unlinkat directly via golang.org/x/sys/unix gives
// deterministic syscall choice independent of host glibc and
// Go-runtime version. The worker chdirs into the script-owned
// tempdir that contains the sentinel prefix, then creates and
// removes the exact relative pathname "unlinkat-target". That gives
// BPF tracepoint fixtures a stable filename to assert while keeping
// concurrent workers on disjoint filesystem paths.
package fixturemode

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
)

func init() {
	driver.RegisterFireKind("unlinkat", driver.FireKind{
		Mode:        "unlinkat-fire-worker",
		Summary:     "Fire unlinkat(2) syscalls for sys_*_unlinkat tracepoint hooks.",
		NeedsBinary: false,
	})
}

func runUnlinkatFireWorker(args []string) error {
	if len(args) != 4 {
		return fmt.Errorf("unlinkat-fire-worker: usage: SENTINEL_PREFIX ACK_PREFIX N K (got %d args)", len(args))
	}
	sentinelPrefix := args[0]
	ackPrefix := args[1]
	n, err := strconv.Atoi(args[2])
	if err != nil {
		return fmt.Errorf("unlinkat-fire-worker: invalid N %q: %w", args[2], err)
	}

	k, err := strconv.Atoi(args[3])
	if err != nil {
		return fmt.Errorf("unlinkat-fire-worker: invalid K %q: %w", args[3], err)
	}

	workdir := filepath.Dir(sentinelPrefix)
	if err := os.Chdir(workdir); err != nil {
		return fmt.Errorf("unlinkat-fire-worker: chdir %s: %w", workdir, err)
	}
	for wave := 1; wave <= k; wave++ {
		sentinel := fmt.Sprintf("%s.%d", sentinelPrefix, wave)
		ack := fmt.Sprintf("%s.%d", ackPrefix, wave)
		for {
			if _, err := os.Stat(sentinel); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		for i := range n {
			path := "unlinkat-target"
			fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_WRONLY|syscall.O_TRUNC, 0o644)
			if err != nil {
				return fmt.Errorf("unlinkat-fire-worker: open wave=%d i=%d: %w", wave, i, err)
			}

			syscall.Close(fd)
			if err := unix.Unlinkat(unix.AT_FDCWD, path, 0); err != nil {
				return fmt.Errorf("unlinkat-fire-worker: unlinkat wave=%d i=%d: %w", wave, i, err)
			}
		}
		f, err := os.OpenFile(ack, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("unlinkat-fire-worker: create ack %s: %w", ack, err)
		}

		f.Close()
	}
	return nil
}

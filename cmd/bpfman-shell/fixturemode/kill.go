// Test-fixture mode for the e2e/scripts translations that need
// a stable-PID worker firing kill(2) syscalls.
package fixturemode

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
)

func init() {
	driver.RegisterFireKind("kill", driver.FireKind{
		Mode:        "kill-fire-worker",
		Summary:     "Fire kill(2) syscalls on self for syscalls/sys_enter_kill.",
		NeedsBinary: false,
	})
}

func runKillFireWorker(args []string) error {
	if len(args) != 4 {
		return fmt.Errorf("kill-fire-worker: usage: SENTINEL_PREFIX ACK_PREFIX N K (got %d args)", len(args))
	}
	sentinelPrefix := args[0]
	ackPrefix := args[1]
	n, err := strconv.Atoi(args[2])
	if err != nil {
		return fmt.Errorf("kill-fire-worker: invalid N %q: %w", args[2], err)
	}

	k, err := strconv.Atoi(args[3])
	if err != nil {
		return fmt.Errorf("kill-fire-worker: invalid K %q: %w", args[3], err)
	}

	sigCh := make(chan os.Signal, 1024)
	signal.Notify(sigCh, syscall.SIGUSR1)
	go func() {
		for range sigCh {
		}
	}()

	pid := os.Getpid()
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
			if err := syscall.Kill(pid, syscall.SIGUSR1); err != nil {
				return fmt.Errorf("kill-fire-worker: kill wave=%d i=%d: %w", wave, i, err)
			}
		}
		f, err := os.OpenFile(ack, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("kill-fire-worker: create ack %s: %w", ack, err)
		}

		f.Close()
	}
	return nil
}

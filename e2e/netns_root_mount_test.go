//go:build e2e

package e2e

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"

	"github.com/bpfman/bpfman/e2e/testnet"
)

// The bind-mount target is /run/netns/<testnet.RootNetns>, so
// command-line tools that take a netns name can target root via
// `ip netns exec <RootNetns> <cmd>`. Wrapping every
// netns-sensitive shell-out in `ip netns exec <ns>` makes the
// child explicitly setns into the named netns regardless of which
// Go OS thread performed the fork; that decouples test
// reliability from thread-state contamination upstream.

// setupRootNetnsMount bind-mounts /proc/self/ns/net at
// /run/netns/<testnet.RootNetns>. Idempotent: a previous run
// that crashed before unmounting just gets re-bind-mounted in
// place. Returns an error rather than panicking so TestMain can
// decide how to react.
func setupRootNetnsMount() error {
	if err := os.MkdirAll("/run/netns", 0o755); err != nil {
		return fmt.Errorf("mkdir /run/netns: %w", err)
	}

	target := "/run/netns/" + testnet.RootNetns
	// Match iproute2's `ip netns add` behaviour: create the
	// target with mode 0 and O_RDONLY. SELinux on Fedora can
	// reject other modes for bind-mount targets under
	// /run/netns. Existing file from a previous (crashed) run
	// is fine -- we just bind-mount over it.
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_RDONLY, 0)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("create %s: %w", target, err)
	}
	if err == nil {
		f.Close()
	}

	// If something is already mounted there, unmount first; we
	// can't tell from a stale empty file vs a stale bind-mount
	// without checking, so just attempt unmount and ignore the
	// "not mounted" error.
	_ = unix.Unmount(target, 0)
	if err := unix.Mount("/proc/self/ns/net", target, "none", unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("bind-mount /proc/self/ns/net -> %s: %w", target, err)
	}

	return nil
}

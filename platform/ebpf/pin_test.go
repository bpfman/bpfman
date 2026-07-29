package ebpf

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

type noopKernelLinkHandle struct{}

func (noopKernelLinkHandle) Close() error { return nil }

var errNotExist = fmt.Errorf("lookup: %w", os.ErrNotExist)

// newWaitTestAdapter builds an adapter whose kernel-link wait uses the
// given lookup and test-sized bounds, logging into logs.
func newWaitTestAdapter(logs *bytes.Buffer, fn func(link.ID) (kernelLinkHandle, error)) *kernelAdapter {
	return &kernelAdapter{
		logger: slog.New(slog.NewTextHandler(logs, nil)),
		linkWait: linkWaitParams{
			newLinkFromID: fn,
			deadline:      50 * time.Millisecond,
			settle:        10 * time.Millisecond,
			maxBackoff:    time.Millisecond,
		},
	}
}

// A first-probe ENOENT means our own Close dropped the last reference
// and the kernel freed the link synchronously inside close(2), so the
// wait must return at once: no settle, no second probe. Uses the
// production parameters so the asserted latency is the shipped one.
func TestWaitKernelLinkGoneReturnsOnFirstENOENT(t *testing.T) {
	t.Parallel()

	var calls int
	k := &kernelAdapter{
		logger:   slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		linkWait: defaultLinkWaitParams(),
	}
	k.linkWait.newLinkFromID = func(link.ID) (kernelLinkHandle, error) {
		calls++
		return nil, errNotExist
	}

	start := time.Now()
	k.waitKernelLinkGone(t.Context(), link.ID(42), "/sys/fs/bpf/demo/link", true)
	elapsed := time.Since(start)

	require.Equal(t, 1, calls, "first-probe ENOENT must end the wait without further probes")
	require.Less(t, elapsed, k.linkWait.settle, "first-probe ENOENT must not pay the settle period")
}

// ENOENT after the link has been seen alive is ambiguous: an external
// holder may have released it via a deferred (non-FD) put whose free
// is still unwinding behind the ID removal. The settle period must
// apply on that path.
func TestWaitKernelLinkGoneSettlesAfterExternalRelease(t *testing.T) {
	t.Parallel()

	var calls int
	k := newWaitTestAdapter(&bytes.Buffer{}, func(link.ID) (kernelLinkHandle, error) {
		calls++
		if calls == 1 {
			return noopKernelLinkHandle{}, nil
		}
		return nil, errNotExist
	})

	start := time.Now()
	k.waitKernelLinkGone(t.Context(), link.ID(42), "/sys/fs/bpf/demo/link", true)

	require.GreaterOrEqual(t, time.Since(start), k.linkWait.settle, "ENOENT after alive must observe the settle period")
	require.Greater(t, calls, 2, "the settle must keep probing after the alive observation")
}

// When the pin vanished out-of-band before our removal, our Close is
// not provably the last reference drop, so even a first-probe ENOENT
// is ambiguous and must observe the settle period.
func TestWaitKernelLinkGoneSettlesOnFirstENOENTWhenPinVanished(t *testing.T) {
	t.Parallel()

	var calls int
	k := newWaitTestAdapter(&bytes.Buffer{}, func(link.ID) (kernelLinkHandle, error) {
		calls++
		return nil, errNotExist
	})

	start := time.Now()
	k.waitKernelLinkGone(t.Context(), link.ID(42), "/sys/fs/bpf/demo/link", false)

	require.GreaterOrEqual(t, time.Since(start), k.linkWait.settle, "first-probe ENOENT without the close-was-last-drop proof must observe the settle period")
	require.Greater(t, calls, 1, "the settle must keep probing")
}

// ENOENT is only proof of completed teardown on the very first probe,
// where it means our own Close freed the link synchronously. EAGAIN on
// the first probe means the ID slot was recycled, so our link was
// freed by an external deferred put whose hook detach may still be
// unwinding -- a follow-up ENOENT must observe the settle period, not
// return instantly.
func TestWaitKernelLinkGoneSettlesAfterEAGAIN(t *testing.T) {
	t.Parallel()

	var calls int
	k := newWaitTestAdapter(&bytes.Buffer{}, func(link.ID) (kernelLinkHandle, error) {
		calls++
		if calls == 1 {
			return nil, fmt.Errorf("lookup: %w", unix.EAGAIN)
		}
		return nil, errNotExist
	})

	start := time.Now()
	k.waitKernelLinkGone(t.Context(), link.ID(42), "/sys/fs/bpf/demo/link", true)

	require.GreaterOrEqual(t, time.Since(start), k.linkWait.settle, "ENOENT after EAGAIN must observe the settle period")
	require.Greater(t, calls, 2, "the settle must keep probing after the EAGAIN observation")
}

// A transient lookup failure on the first probe leaves the link's
// state unknown, so a follow-up ENOENT is ambiguous in the same way:
// the settle period must apply.
func TestWaitKernelLinkGoneSettlesAfterTransientError(t *testing.T) {
	t.Parallel()

	var calls int
	var logs bytes.Buffer
	k := newWaitTestAdapter(&logs, func(link.ID) (kernelLinkHandle, error) {
		calls++
		if calls == 1 {
			return nil, fmt.Errorf("lookup: %w", unix.EMFILE)
		}
		return nil, errNotExist
	})

	start := time.Now()
	k.waitKernelLinkGone(t.Context(), link.ID(42), "/sys/fs/bpf/demo/link", true)

	require.GreaterOrEqual(t, time.Since(start), k.linkWait.settle, "ENOENT after a transient error must observe the settle period")
	require.Greater(t, calls, 2, "the settle must keep probing after the transient error")
}

// Reaching the deadline while the link is already gone and merely
// settling is success, not a wedged reference: the "still present"
// warning must not fire.
func TestWaitKernelLinkGoneNoWarnWhenGoneAtDeadline(t *testing.T) {
	t.Parallel()

	var calls int
	var logs bytes.Buffer
	k := newWaitTestAdapter(&logs, func(link.ID) (kernelLinkHandle, error) {
		calls++
		if calls == 1 {
			return noopKernelLinkHandle{}, nil
		}
		return nil, errNotExist
	})
	k.linkWait.settle = 10 * k.linkWait.deadline

	k.waitKernelLinkGone(t.Context(), link.ID(42), "/sys/fs/bpf/demo/link", true)

	require.NotContains(t, logs.String(), "kernel link still present after detach wait", "a gone-but-settling link is not still present")
}

// A link that stays alive means an external reference is pinning it;
// the wait must hold on until the deadline and then warn.
func TestWaitKernelLinkGoneWarnsAtDeadlineWhileAlive(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	k := newWaitTestAdapter(&logs, func(link.ID) (kernelLinkHandle, error) {
		return noopKernelLinkHandle{}, nil
	})

	start := time.Now()
	k.waitKernelLinkGone(t.Context(), link.ID(42), "/sys/fs/bpf/demo/link", true)

	require.GreaterOrEqual(t, time.Since(start), k.linkWait.deadline)
	require.Contains(t, logs.String(), "kernel link still present after detach wait")
}

func TestWaitKernelLinkGoneKeepsWaitingOnTransientLookupError(t *testing.T) {
	t.Parallel()

	var calls int
	ctx, cancel := context.WithCancel(t.Context())
	var logs bytes.Buffer
	k := newWaitTestAdapter(&logs, func(link.ID) (kernelLinkHandle, error) {
		calls++
		if calls == 1 {
			return nil, fmt.Errorf("lookup: %w", unix.EMFILE)
		}
		cancel()
		return noopKernelLinkHandle{}, nil
	})

	k.waitKernelLinkGone(ctx, link.ID(42), "/sys/fs/bpf/demo/link", true)

	require.Greater(t, calls, 1, "transient lookup errors must not terminate the wait")
	require.Contains(t, logs.String(), "kernel link lookup failed during detach wait")
}

func TestWaitKernelLinkGoneLogsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	var logs bytes.Buffer
	k := newWaitTestAdapter(&logs, func(link.ID) (kernelLinkHandle, error) {
		cancel()
		return noopKernelLinkHandle{}, nil
	})

	k.waitKernelLinkGone(ctx, link.ID(42), "/sys/fs/bpf/demo/link", true)

	require.Contains(t, logs.String(), "kernel link detach wait cancelled")
}

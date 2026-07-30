package ebpf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"golang.org/x/sys/unix"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
)

// ============================================================================
// CLI helpers - filesystem-based operations for scanning bpffs
// ============================================================================

// ListPinDir scans a bpffs directory and returns its contents.
func (k *kernelAdapter) ListPinDir(ctx context.Context, pinDir string, includeMaps bool) (*kernel.PinDirContents, error) {
	entries, err := os.ReadDir(pinDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read pin directory: %w", err)
	}

	result := &kernel.PinDirContents{}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		path := filepath.Join(pinDir, entry.Name())

		// Try to load as program first
		prog, err := ebpf.LoadPinnedProgram(path, nil)
		if err == nil {
			info, _ := prog.Info()
			if info != nil {
				id, _ := info.ID()
				ebpfMapIDs, _ := info.MapIDs()
				mapIDs := make([]kernel.MapID, len(ebpfMapIDs))
				for i, mid := range ebpfMapIDs {
					mapIDs[i] = kernel.MapID(mid)
				}
				result.Programs = append(result.Programs, kernel.PinnedProgram{
					ID:         kernel.ProgramID(id),
					Name:       info.Name,
					Type:       kernel.NewProgramType(prog.Type().String()),
					Tag:        info.Tag,
					PinnedPath: path,
					MapIDs:     mapIDs,
				})
			}
			prog.Close()
			continue
		}

		// Try as map if includeMaps
		if includeMaps {
			mp, err := ebpf.LoadPinnedMap(path, nil)
			if err == nil {
				info, _ := mp.Info()
				if info != nil {
					id, _ := info.ID()
					result.Maps = append(result.Maps, kernel.PinnedMap{
						ID:         kernel.MapID(id),
						Name:       info.Name,
						Type:       kernel.NewMapType(info.Type.String()),
						KeySize:    info.KeySize,
						ValueSize:  info.ValueSize,
						MaxEntries: info.MaxEntries,
						PinnedPath: path,
					})
				}
				mp.Close()
			}
		}
	}

	return result, nil
}

// GetPinned loads and returns info about a pinned program.
func (k *kernelAdapter) GetPinned(ctx context.Context, pinPath string) (*kernel.PinnedProgram, error) {
	prog, err := ebpf.LoadPinnedProgram(pinPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to load pinned program: %w", err)
	}
	defer prog.Close()

	info, err := prog.Info()
	if err != nil {
		return nil, fmt.Errorf("failed to get program info: %w", err)
	}

	id, ok := info.ID()
	if !ok {
		return nil, fmt.Errorf("failed to get program ID from kernel")
	}

	ebpfMapIDs, _ := info.MapIDs() // MapIDs may not be available on older kernels
	mapIDs := make([]kernel.MapID, len(ebpfMapIDs))
	for i, mid := range ebpfMapIDs {
		mapIDs[i] = kernel.MapID(mid)
	}

	return &kernel.PinnedProgram{
		ID:         kernel.ProgramID(id),
		Name:       info.Name,
		Type:       kernel.NewProgramType(prog.Type().String()),
		Tag:        info.Tag,
		PinnedPath: pinPath,
		MapIDs:     mapIDs,
	}, nil
}

// RepinMap loads a pinned map and re-pins it to a new path.
// This is used by CSI to expose maps to per-pod bpffs.
func (k *kernelAdapter) RepinMap(ctx context.Context, srcPath, dstPath string) error {
	m, err := ebpf.LoadPinnedMap(srcPath, nil)
	if err != nil {
		return fmt.Errorf("load pinned map %s: %w", srcPath, err)
	}
	defer m.Close()

	// Clone the map FD to get a map without pin path tracking.
	// This avoids the "invalid cross-device link" error when pinning
	// to a different bpffs instance, since cilium/ebpf tries to
	// rename/move the old pin when Pin() is called on an already-pinned map.
	cloned, err := m.Clone()
	if err != nil {
		return fmt.Errorf("clone map: %w", err)
	}
	defer cloned.Close()

	if err := cloned.Pin(dstPath); err != nil {
		return fmt.Errorf("re-pin map to %s: %w", dstPath, err)
	}
	return nil
}

// Unpin removes all pins from a directory.
func (k *kernelAdapter) Unpin(pinDir string) (int, error) {
	entries, err := os.ReadDir(pinDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read pin directory: %w", err)
	}

	count := 0
	for _, entry := range entries {
		path := filepath.Join(pinDir, entry.Name())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return count, fmt.Errorf("failed to unpin %s: %w", path, err)
		}
		count++
	}

	if err := os.Remove(pinDir); err != nil && !os.IsNotExist(err) {
		return count, fmt.Errorf("failed to remove pin directory: %w", err)
	}

	return count, nil
}

// DetachLink tears down a previously-attached link in four
// stages: explicit kernel-side Detach() where supported, removal
// of the bpffs pin, Close of the FD opened from the pin, then a
// bounded wait for the kernel link object to disappear.
//
// The invariant the stages rest on: the FD opened in stage 1 is
// held across the stage-2 pin removal, so the pin's put can never
// drop the link's last reference -- only our own Close in stage 3
// can. An FD close frees the link synchronously inside close(2)
// (bpf_link_put_direct), ID removal and hook detach both complete
// before Close returns, where every other put path defers to a
// workqueue. That ordering, not the pin removal itself, is what
// makes teardown synchronous and gives detach the contract
// callers expect: returned means no longer invoked.
//
// Stage 1 (open + Detach): open an FD from the pin, then, for
// link types that implement the kernel's bpf_link_ops.detach
// callback -- XDP, TCX, cgroup, netfilter, netkit, struct_ops,
// sockmap -- synchronously disconnect the program from its hook.
// Returns EOPNOTSUPP (wrapped as ebpf.ErrNotSupported) for
// perf-event / tracing link types (kprobe, uprobe, tracepoint,
// fentry/fexit), where there is no kernel-side sync detach API;
// those rely wholly on the invariant above.
//
// Stage 2 (Remove): removes the bpffs pin entry while the FD is
// still held.
//
// Stage 3 (Close): drops what is normally the last reference,
// tearing the link down synchronously inside close(2).
//
// Stage 4 (wait): insurance for the one case the invariant does
// not cover -- an external FD holder (bpftool, an inspection
// tool) releasing its reference through the deferred put path
// after our Close. Skipped when stage 1's Detach succeeded (the
// program is already provably off the hook); otherwise polls the
// kernel link ID, bounded, until the object is gone.
func (k *kernelAdapter) DetachLink(ctx context.Context, linkPinPath bpfman.LinkPath) error {
	pin := linkPinPath.String()
	k.logger.Debug("detaching link by removing pin", "link_pin_path", pin)

	// Stage 1: open an FD from the pin -- the pin holds the
	// link's only reference; the daemon keeps no link state --
	// and attempt the synchronous kernel-side detach for
	// supported link types. EOPNOTSUPP comes back for perf-event
	// / tracing link types where the kernel has no synchronous
	// detach API; those rely on the Remove-then-Close ordering
	// below.
	var detachLnk link.Link
	if lnk, err := link.LoadPinnedLink(pin, nil); err == nil {
		detachLnk = lnk
	} else if !errors.Is(err, os.ErrNotExist) {
		// cilium/ebpf wraps the underlying ENOENT in a string-formatted
		// error ("load pinned link: no such file or directory"), so the
		// older os.IsNotExist (which doesn't unwrap) misses it and the
		// expected race -- pin already removed by an earlier teardown
		// step -- gets logged as a WARN. errors.Is unwraps through fmt's
		// %w chain and treats both raw PathError and the cilium wrapper
		// as ENOENT.
		//
		// Any other failure aborts the detach: without an FD we can
		// neither attempt the kernel-side Detach nor observe the
		// link's release, so proceeding would remove the pin and
		// report success while the program may keep running --
		// exactly the contract violation this function exists to
		// rule out. The link record survives for a retry.
		return fmt.Errorf("load pinned link %s: %w", pin, err)
	}
	var syncDetached bool
	var kernelLinkID link.ID
	if detachLnk != nil {
		// Capture the kernel link ID while we still hold an FD;
		// stage 4 needs it to observe the object's release. An
		// Info failure aborts for the same reason a load failure
		// does: without the ID, stage 4 cannot verify the release
		// for the async-teardown link types.
		if info, err := detachLnk.Info(); err == nil {
			kernelLinkID = info.ID
		} else {
			if cerr := detachLnk.Close(); cerr != nil {
				k.logger.Warn("close loaded link", "link_pin_path", pin, "err", cerr)
			}
			return fmt.Errorf("link info %s: %w", pin, err)
		}
		if err := detachLnk.Detach(); err == nil {
			syncDetached = true
		} else if !errors.Is(err, ebpf.ErrNotSupported) {
			k.logger.Warn("link Detach failed", "link_pin_path", pin, "err", err)
			// continue: cleanup must still happen
		}
		// The FD is deliberately NOT closed yet: stage 4's
		// first-probe fast path is sound only if an FD is held
		// across the stage-2 pin removal, so the pin's put can
		// never drop the last reference. Stage 3 closes it.
	}

	// Stage 2: remove the pin.
	if err := os.Remove(pin); err != nil {
		if os.IsNotExist(err) {
			k.logger.Debug("link pin already gone", "link_pin_path", pin)
			if detachLnk != nil {
				if cerr := detachLnk.Close(); cerr != nil {
					k.logger.Warn("close loaded link", "link_pin_path", pin, "err", cerr)
				}
			}
			// The pin vanished out-of-band, so our Close is not
			// provably the last reference drop: the external
			// unlink's put may still be pending. Disable the
			// first-probe fast path; every ENOENT settles.
			if !syncDetached && kernelLinkID != 0 {
				k.waitKernelLinkGone(ctx, kernelLinkID, pin, false)
			}
			return nil
		}
		if detachLnk != nil {
			if cerr := detachLnk.Close(); cerr != nil {
				k.logger.Warn("close loaded link", "link_pin_path", pin, "err", cerr)
			}
		}
		return fmt.Errorf("remove link pin %s: %w", pin, err)
	}

	// Stage 3: close our link FD. This must stay after the
	// stage-2 pin removal: with the FD held across the pin's put,
	// only our own close can drop the last reference, and an FD
	// close tears the link down synchronously inside close(2) --
	// the invariant behind stage 4's first-probe fast path.
	if detachLnk != nil {
		if cerr := detachLnk.Close(); cerr != nil {
			k.logger.Warn("close loaded link", "link_pin_path", pin, "err", cerr)
		}
	}
	k.logger.Debug("link pin removed", "link_pin_path", pin)
	// Best-effort removal of the parent directory. bpfman's own
	// attach cannot race this -- every mutation, CLI or daemon,
	// holds the cross-process writer flock -- but the directory
	// lives on a shared bpffs that out-of-band actors can touch;
	// attach calls MkdirAll before pinning, so it recovers if the
	// directory disappears underneath it.
	os.Remove(filepath.Dir(pin))

	// Stage 4: only the async-teardown types need the wait; a
	// successful Detach already disconnected the program.
	if !syncDetached && kernelLinkID != 0 {
		k.waitKernelLinkGone(ctx, kernelLinkID, pin, true)
	}
	return nil
}

// waitKernelLinkGone polls until the kernel link object with the
// given ID has been released, bounded so a wedged reference can
// never hang teardown.
//
// The caller holds the link's FD across the pin removal (DetachLink
// stages 1-3), so the pin's put can never drop the last reference;
// only our own Close can, and an FD close frees the link
// synchronously inside close(2) -- ID removal and the hook detach
// both complete before Close returns. ENOENT on the first probe
// therefore means the teardown already happened, and the wait ends
// at once. ENOENT on any later probe is ambiguous: an external
// holder may have released the link via a deferred (non-FD) put
// whose hook detach is still unwinding behind the ID removal, so
// those paths observe a short settle period before returning.
//
// closeWasLastDrop states whether the caller's Close provably
// performed the last reference drop, which is what makes the
// first-probe fast path sound. When the pin vanished out-of-band
// the external unlink's put may still be pending, so the caller
// passes false and every ENOENT takes the settle path.
func (k *kernelAdapter) waitKernelLinkGone(ctx context.Context, id link.ID, pin string, closeWasLastDrop bool) {
	backoff := time.Millisecond
	start := time.Now()
	firstProbe := closeWasLastDrop
	lookupErrLogged := false
	var goneSince time.Time
	for {
		l, err := k.linkWait.newLinkFromID(id)
		switch {
		case err == nil:
			_ = l.Close()
			goneSince = time.Time{}
		case errors.Is(err, os.ErrNotExist):
			if firstProbe {
				return
			}
			if goneSince.IsZero() {
				goneSince = time.Now()
			}
			if time.Since(goneSince) >= k.linkWait.settle {
				return
			}
		case errors.Is(err, unix.EAGAIN):
			// The ID slot holds a link mid-creation, which for
			// our ID means it has been recycled by a concurrent
			// attach -- so our link was already freed by an
			// external deferred put whose hook detach may still
			// be unwinding. Not proof of settled teardown; keep
			// polling.
			goneSince = time.Time{}
		default:
			// Log once per wait: a persistent failure (EMFILE,
			// EPERM) would otherwise emit a warning every probe
			// for the full deadline.
			if !lookupErrLogged {
				k.logger.Warn("kernel link lookup failed during detach wait", "link_pin_path", pin, "kernel_link_id", id, "err", err)
				lookupErrLogged = true
			}
		}
		firstProbe = false

		if time.Since(start) >= k.linkWait.deadline {
			if goneSince.IsZero() {
				k.logger.Warn("kernel link still present after detach wait", "link_pin_path", pin, "kernel_link_id", id, "waited", k.linkWait.deadline)
			}
			return
		}
		select {
		case <-ctx.Done():
			k.logger.Warn("kernel link detach wait cancelled", "link_pin_path", pin, "kernel_link_id", id, "err", ctx.Err())
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, k.linkWait.maxBackoff)
	}
}

type kernelLinkHandle interface {
	Close() error
}

// linkWaitParams bounds waitKernelLinkGone. New fills in the
// defaults; tests construct adapters with faster ones.
type linkWaitParams struct {
	newLinkFromID func(id link.ID) (kernelLinkHandle, error)
	deadline      time.Duration
	settle        time.Duration
	maxBackoff    time.Duration
}

func defaultLinkWaitParams() linkWaitParams {
	return linkWaitParams{
		newLinkFromID: func(id link.ID) (kernelLinkHandle, error) { return link.NewFromID(id) },
		deadline:      3 * time.Second,
		settle:        100 * time.Millisecond,
		maxBackoff:    50 * time.Millisecond,
	}
}

// pinWithRetry creates the parent directory and invokes pin. If the
// pin fails because the directory vanished underneath it, it retries
// once. bpfman's own detach cannot race this -- every mutation, CLI
// or daemon, holds the cross-process writer flock -- but the
// directory lives on a shared bpffs where an out-of-band removal can
// strike between MkdirAll and pin.
//
// Generic over any string-derived path type (bpfman.LinkPath, plain
// string, future newtypes) so callers preserve their type discipline
// to the pin call site; the single cast to string lives here, at the
// cilium/ebpf boundary.
func pinWithRetry[P ~string](path P, pin func(string) error) error {
	s := string(path)
	// Two attempts total: one initial attempt plus one retry.
	for attempt := range 2 {
		if err := os.MkdirAll(filepath.Dir(s), 0755); err != nil {
			return fmt.Errorf("create pin directory: %w", err)
		}
		err := pin(s)
		if err == nil {
			return nil
		}
		if attempt == 0 && os.IsNotExist(err) {
			continue // directory removed between MkdirAll and Pin
		}
		return err
	}
	return fmt.Errorf("pin %s: directory removed between retries", s)
}

// RemovePin removes a program pin from bpffs. The typed parameter
// rejects link, map, and arbitrary-string paths at compile time.
// Returns nil if the path does not exist.
func (k *kernelAdapter) RemovePin(ctx context.Context, p bpfman.ProgPinPath) error {
	path := p.String()
	k.logger.Debug("removing pin", "path", path)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			k.logger.Debug("pin already gone", "path", path)
			return nil // Already gone
		}
		return fmt.Errorf("remove pin %s: %w", path, err)
	}
	k.logger.Debug("pin removed", "path", path)
	return nil
}

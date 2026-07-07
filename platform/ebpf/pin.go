package ebpf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"

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

// DetachLink tears down a previously-attached link in three
// stages: explicit kernel-side Detach() where supported, removal
// of the bpffs pin, then Close of the live *link.Link FD.
//
// Stage 1 (Detach): for link types that implement the kernel's
// bpf_link_ops.detach callback -- XDP, TCX, cgroup, netfilter,
// netkit, struct_ops, sockmap -- this synchronously disconnects
// the program from its hook before we touch the pin or the FD.
// Returns EOPNOTSUPP (wrapped as ebpf.ErrNotSupported) for
// perf-event / tracing link types (kprobe, uprobe, tracepoint,
// fentry/fexit), where there is no kernel-side sync detach API.
// Best-effort: a non-EOPNOTSUPP failure is logged but does not
// abort cleanup.
//
// Stage 2 (Remove): removes the bpffs pin entry. For perf-event
// link types where Detach() is not supported, this is the step
// that gets bpf_perf_link_release running synchronously enough
// for the program to stop firing -- the order Remove-then-Close
// (rather than the reverse) is load-bearing here.
//
// Stage 3 (Close): drops the last user reference to the link
// FD; the kernel reclaims the link object.
//
// For Detach-supporting types the program is already provably
// offline by the time we reach Close. For non-supporting types
// the test surface still observes async kernel teardown lag
// (see waitForDetachComplete in e2e/helpers.go) -- there is no
// userspace primitive to make perf-event teardown synchronous.
func (k *kernelAdapter) DetachLink(ctx context.Context, linkPinPath bpfman.LinkPath) error {
	pin := linkPinPath.String()
	k.logger.Debug("detaching link by removing pin", "link_pin_path", pin)

	// Stage 1: synchronous kernel-side detach for supported
	// link types. Prefer the in-process tracked link (saved by
	// trackLink at attach time) so we don't open a fresh FD;
	// fall back to LoadPinnedLink when the attach path closed
	// the original FD after pinning (TCX, XDP, dispatcher
	// extensions). Either way the resulting *link.Link is
	// asked to Detach. EOPNOTSUPP comes back for perf-event /
	// tracing link types where the kernel has no synchronous
	// detach API; those rely on the Remove-then-Close ordering
	// below.
	var detachLnk link.Link
	var detachLnkOpened bool
	if v, ok := k.liveLinks.Load(pin); ok {
		detachLnk = v.(link.Link)
	} else if lnk, err := link.LoadPinnedLink(pin, nil); err == nil {
		detachLnk = lnk
		detachLnkOpened = true
	} else if !errors.Is(err, os.ErrNotExist) {
		// cilium/ebpf wraps the underlying ENOENT in a string-formatted
		// error ("load pinned link: no such file or directory"), so the
		// older os.IsNotExist (which doesn't unwrap) misses it and the
		// expected race -- pin already removed by an earlier teardown
		// step -- gets logged as a WARN. errors.Is unwraps through fmt's
		// %w chain and treats both raw PathError and the cilium wrapper
		// as ENOENT.
		k.logger.Warn("LoadPinnedLink failed", "link_pin_path", pin, "err", err)
	}
	if detachLnk != nil {
		if err := detachLnk.Detach(); err != nil && !errors.Is(err, ebpf.ErrNotSupported) {
			k.logger.Warn("link Detach failed", "link_pin_path", pin, "err", err)
			// continue: cleanup must still happen
		}
		if detachLnkOpened {
			// We opened this FD ourselves; close it. The
			// tracked-link case (if any) is closed later
			// via releaseLink.
			_ = detachLnk.Close()
		}
	}

	// Stage 2: remove the pin.
	if err := os.Remove(pin); err != nil {
		if os.IsNotExist(err) {
			k.logger.Debug("link pin already gone", "link_pin_path", pin)
			// Pin gone, but a tracked link may still be live
			// (in-process attach by the same adapter). Drop it.
			if cerr := k.releaseLink(pin); cerr != nil {
				k.logger.Warn("close tracked link after missing pin", "link_pin_path", pin, "err", cerr)
			}
			return nil
		}
		return fmt.Errorf("remove link pin %s: %w", pin, err)
	}

	// Stage 3: close the FD via releaseLink (which also drops
	// the tracking-map entry).
	if err := k.releaseLink(pin); err != nil {
		k.logger.Warn("close tracked link", "link_pin_path", pin, "err", err)
	}
	k.logger.Debug("link pin removed", "link_pin_path", pin)
	// Best-effort removal of the parent directory. This races
	// with concurrent attach in non-daemon mode (no global lock),
	// but attach calls MkdirAll before pinning, so it recovers
	// if the directory disappears underneath it.
	os.Remove(filepath.Dir(pin))
	return nil
}

// pinWithRetry creates the parent directory and invokes pin. If the
// pin fails because a concurrent detach removed the directory, it
// retries once. This covers the race between detach (which removes
// empty link directories) and attach (which creates them) when
// running outside daemon mode with no global lock.
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

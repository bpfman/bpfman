// kfuncpool is the cross-process slot allocator backing
// `kfunc acquire`. Each running bpfman-shell process competes for
// one of the kmod-backed kernel-function slots exported by the
// bpfman_e2e_targets module. Exclusion is via flock on a per-slot
// lockfile under /run/bpfman-kfunc-pool, matching the net veth-pair
// pool's crash-safe shape.
package builtins

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

const (
	kfuncPoolSize  = 128
	kfuncPoolRoot  = "/run/bpfman-kfunc-pool"
	kfuncDebugRoot = "/sys/kernel/debug/bpfman_e2e"
)

type kfuncLease struct {
	slot     uint32
	lockFile *os.File
	origin   string
}

type kfuncAcquireRequest struct {
	root   string
	origin string
}

type kfuncProvenance struct {
	Origin     string `json:"origin,omitempty"`
	Slot       uint32 `json:"slot"`
	Name       string `json:"name,omitempty"`
	Trigger    string `json:"trigger,omitempty"`
	Count      string `json:"count,omitempty"`
	AcquiredAt string `json:"acquired_at,omitempty"`
	ReleasedAt string `json:"released_at,omitempty"`
}

var (
	kfuncLeaseMu sync.Mutex
	kfuncLeases  = map[*runtime.Kfunc]*kfuncLease{}
)

func acquireKfuncSlot(req kfuncAcquireRequest) (*runtime.Kfunc, *kfuncLease, error) {
	if _, err := os.Stat(kfuncDebugRoot); err != nil {
		return nil, nil, fmt.Errorf("kfunc pool: %s is not available: %w", kfuncDebugRoot, err)
	}

	root := req.root
	if root == "" {
		root = kfuncPoolRoot
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, nil, fmt.Errorf("kfunc pool: mkdir %s: %w", root, err)
	}

	cands, err := scanKfuncSlots(root)
	if err != nil {
		return nil, nil, err
	}

	slices.SortStableFunc(cands, func(a, b kfuncCandidate) int {
		return a.sortKey.Compare(b.sortKey)
	})

	for _, c := range cands {
		path := kfuncSlotLockPath(root, c.slot)
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("kfunc pool: open %s: %w", path, err)
		}

		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			f.Close()
			if errors.Is(err, syscall.EWOULDBLOCK) {
				continue
			}
			return nil, nil, fmt.Errorf("kfunc pool: flock %s: %w", path, err)
		}

		kf := kfuncForSlot(c.slot)
		if err := assertKfuncSlotAvailable(kf); err != nil {
			f.Close()
			return nil, nil, err
		}

		now := time.Now().UTC()
		fresh := kfuncProvenance{
			Origin:     req.origin,
			Slot:       c.slot,
			Name:       kf.Name,
			Trigger:    kf.Trigger,
			Count:      kf.Count,
			AcquiredAt: now.Format(time.RFC3339Nano),
		}
		if err := writeKfuncProvenance(f, fresh); err != nil {
			f.Close()
			return nil, nil, fmt.Errorf("kfunc pool: write %s: %w", path, err)
		}
		return kf, &kfuncLease{slot: c.slot, lockFile: f, origin: req.origin}, nil
	}
	return nil, nil, fmt.Errorf("kfunc pool: more than %d concurrent slots in flight", kfuncPoolSize)
}

func releaseKfuncSlot(lease *kfuncLease, kf *runtime.Kfunc) error {
	if lease == nil || lease.lockFile == nil {
		return nil
	}
	final := kfuncProvenance{
		Origin:     lease.origin,
		Slot:       lease.slot,
		Name:       kf.Name,
		Trigger:    kf.Trigger,
		Count:      kf.Count,
		ReleasedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if prev, err := readKfuncProvenanceFromFile(lease.lockFile); err == nil {
		final.AcquiredAt = prev.AcquiredAt
	}

	if err := writeKfuncProvenance(lease.lockFile, final); err != nil {
		lease.lockFile.Close()
		lease.lockFile = nil
		return fmt.Errorf("kfunc pool: write final provenance: %w", err)
	}

	err := lease.lockFile.Close()
	lease.lockFile = nil
	if err != nil {
		return fmt.Errorf("kfunc pool: close slot %d lockfile: %w", lease.slot, err)
	}
	return nil
}

func rememberKfuncLease(kf *runtime.Kfunc, lease *kfuncLease) {
	if kf == nil || lease == nil {
		return
	}
	kfuncLeaseMu.Lock()
	kfuncLeases[kf] = lease
	kfuncLeaseMu.Unlock()
}

func takeKfuncLease(kf *runtime.Kfunc) *kfuncLease {
	if kf == nil {
		return nil
	}
	kfuncLeaseMu.Lock()
	lease := kfuncLeases[kf]
	delete(kfuncLeases, kf)
	kfuncLeaseMu.Unlock()
	return lease
}

type kfuncCandidate struct {
	slot    uint32
	sortKey time.Time
}

func scanKfuncSlots(root string) ([]kfuncCandidate, error) {
	out := make([]kfuncCandidate, 0, kfuncPoolSize)
	for slot := range uint32(kfuncPoolSize) {
		path := kfuncSlotLockPath(root, slot)
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			out = append(out, kfuncCandidate{slot: slot})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("kfunc pool: stat %s: %w", path, err)
		}

		prev, _ := readKfuncProvenance(path)
		key := time.Time{}
		if prev.ReleasedAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, prev.ReleasedAt); err == nil {
				key = t
			}
		}
		if key.IsZero() {
			key = info.ModTime()
		}
		out = append(out, kfuncCandidate{slot: slot, sortKey: key})
	}
	return out, nil
}

func kfuncForSlot(slot uint32) *runtime.Kfunc {
	return &runtime.Kfunc{
		Index:   slot,
		Name:    fmt.Sprintf("bpfman_e2e_target_%d", slot),
		Trigger: fmt.Sprintf("%s/trigger_%03d", kfuncDebugRoot, slot),
		Count:   fmt.Sprintf("%s/count_%03d", kfuncDebugRoot, slot),
	}
}

func assertKfuncSlotAvailable(kf *runtime.Kfunc) error {
	if _, err := os.Stat(kf.Trigger); err != nil {
		return fmt.Errorf("kfunc pool: trigger %s is not available: %w", kf.Trigger, err)
	}
	if _, err := os.Stat(kf.Count); err != nil {
		return fmt.Errorf("kfunc pool: count %s is not available: %w", kf.Count, err)
	}
	return nil
}

func kfuncSlotLockPath(root string, slot uint32) string {
	return filepath.Join(root, fmt.Sprintf("%02d.lock", slot))
}

func readKfuncProvenance(path string) (kfuncProvenance, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return kfuncProvenance{}, fmt.Errorf("kfunc pool: read %s: %w", path, err)
	}

	var p kfuncProvenance
	if len(body) > 0 {
		_ = json.Unmarshal(body, &p)
	}
	return p, nil
}

func readKfuncProvenanceFromFile(f *os.File) (kfuncProvenance, error) {
	if _, err := f.Seek(0, 0); err != nil {
		return kfuncProvenance{}, err
	}

	body, err := readAll(f)
	if err != nil {
		return kfuncProvenance{}, err
	}

	var p kfuncProvenance
	if len(body) > 0 {
		_ = json.Unmarshal(body, &p)
	}
	return p, nil
}

func writeKfuncProvenance(f *os.File, p kfuncProvenance) error {
	if err := f.Truncate(0); err != nil {
		return err
	}

	if _, err := f.Seek(0, 0); err != nil {
		return err
	}

	enc := json.NewEncoder(f)
	return enc.Encode(p)
}

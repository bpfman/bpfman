package driver

import "sync"

// volumeLocks serialises node operations per volume id. The CSI spec lets
// the CO lose state and issue concurrent calls for the same volume; a
// plugin should handle that gracefully and may reject the second call with
// ABORTED. TryAcquire reports whether the caller took the lock -- a false
// result means an operation is already in flight for that volume.
type volumeLocks struct {
	mu       sync.Mutex
	inflight map[string]struct{}
}

func newVolumeLocks() *volumeLocks {
	return &volumeLocks{inflight: make(map[string]struct{})}
}

// TryAcquire takes the lock for volumeID without blocking. It returns
// false if an operation is already in flight for that volume.
func (l *volumeLocks) TryAcquire(volumeID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, held := l.inflight[volumeID]; held {
		return false
	}
	l.inflight[volumeID] = struct{}{}
	return true
}

// Release drops the lock for volumeID.
func (l *volumeLocks) Release(volumeID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.inflight, volumeID)
}

package runtime

import (
	"os"
)

// Mounter handles bpffs mounting during initialisation.
type Mounter interface {
	// EnsureMounted ensures a bpffs is mounted at mountPoint,
	// mounting one if none is present.
	EnsureMounted(mountPoint string) error
}

// RealMounter performs actual bpffs mounting using syscalls.
type RealMounter struct{}

// EnsureMounted mounts a bpffs at mountPoint unless one is already
// mounted there, checking the host's /proc/self/mountinfo.
func (RealMounter) EnsureMounted(mountPoint string) error {
	return EnsureMounted(DefaultMountInfoPath, mountPoint)
}

// NoOpMounter creates the mount point directory without mounting.
type NoOpMounter struct{}

// EnsureMounted creates mountPoint as a directory but performs no
// bpffs mount.
func (NoOpMounter) EnsureMounted(mountPoint string) error {
	return os.MkdirAll(mountPoint, 0o755)
}

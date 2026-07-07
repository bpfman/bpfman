package fs

import (
	"fmt"
	"path/filepath"
)

// DefaultRoot is the canonical bpfman runtime root. It is the
// default both for the bpfman CLI (--runtime-dir) and for the e2e
// suite when BPFMAN_E2E_SUITE_ROOT is unset, so production-shaped
// callers and production-shaped tests share one source of truth.
const DefaultRoot = "/run/bpfman"

// Layout is an immutable, validated filesystem layout. Fields are
// unexported; external packages cannot construct a non-zero Layout
// without calling New.
//
// Layout acts as a capability token following the same pattern as
// lock.WriterScope: possession of a valid Layout proves the base path
// has been validated.
//
// Layout is deliberately I/O free - it only computes and validates paths.
// Callers use fs/runtime.New() to create directories and mount
// bpffs before constructing a manager. This separation enables testing
// without root privileges or real filesystems.
type Layout struct {
	base string
}

// New creates a Layout for bpfman's runtime directory.
//
// The root path is used directly - callers must provide the full path
// including any "bpfman" suffix if desired.
//
// Examples:
//   - New("/run/bpfman") -> /run/bpfman
//   - New("/tmp/test/bpfman") -> /tmp/test/bpfman
//
// New rejects empty paths and relative paths.
func New(root string) (Layout, error) {
	if root == "" {
		return Layout{}, fmt.Errorf("fs: root path cannot be empty")
	}
	if !filepath.IsAbs(root) {
		return Layout{}, fmt.Errorf("fs: root path must be absolute, got %q", root)
	}
	return Layout{base: filepath.Clean(root)}, nil
}

// Valid reports whether l was constructed via New.
func (l Layout) Valid() bool {
	return l.base != ""
}

// String returns a string representation safe for logging.
// Unlike Base(), this never panics and can be used on zero-value Layouts.
func (l Layout) String() string {
	if !l.Valid() {
		return "fs.Layout(<invalid>)"
	}
	return "fs.Layout(" + l.base + ")"
}

// mustValid panics if l is a zero-value Layout.
// This catches programmer errors where a Layout is used without construction via New.
func (l Layout) mustValid() {
	if !l.Valid() {
		panic("fs: zero Layout used; construct via fs.New")
	}
}

// Base returns the runtime base path (e.g., /run/bpfman).
func (l Layout) Base() string {
	l.mustValid()
	return l.base
}

// Bytecode returns the regular-filesystem hierarchy domain for bytecode persistence.
func (l Layout) Bytecode() Bytecode {
	l.mustValid()
	return Bytecode{layout: l}
}

// BPFFS returns the bpffs hierarchy domain.
func (l Layout) BPFFS() BPFFS {
	l.mustValid()
	return BPFFS{layout: l}
}

// LockPath returns the global writer lock file path.
func (l Layout) LockPath() string {
	l.mustValid()
	return filepath.Join(l.base, ".lock")
}

// DBPath returns the full path to the SQLite database file.
func (l Layout) DBPath() string {
	l.mustValid()
	return filepath.Join(l.base, "db", "store.db")
}

// SocketPath returns the full path to the gRPC socket.
func (l Layout) SocketPath() string {
	l.mustValid()
	return filepath.Join(l.base, "sock", "bpfman.sock")
}

// CSISocketPath returns the full path to the CSI socket.
func (l Layout) CSISocketPath() string {
	l.mustValid()
	return filepath.Join(l.base, "csi", "csi.sock")
}

// BPFFSMountPoint returns the bpffs mount point path.
// Deprecated: use l.BPFFS().MountPoint() for consistency with the domain model.
func (l Layout) BPFFSMountPoint() string {
	return l.BPFFS().MountPoint()
}

// DBDir returns the directory containing the database file.
func (l Layout) DBDir() string {
	l.mustValid()
	return filepath.Join(l.base, "db")
}

// SocketDir returns the directory containing the gRPC socket.
func (l Layout) SocketDir() string {
	l.mustValid()
	return filepath.Join(l.base, "sock")
}

// CSIDir returns the CSI directory path.
func (l Layout) CSIDir() string {
	l.mustValid()
	return filepath.Join(l.base, "csi")
}

// CSIFSDir returns the CSI filesystem directory path.
func (l Layout) CSIFSDir() string {
	l.mustValid()
	return filepath.Join(l.base, "csi", "fs")
}

// RuntimeDirs returns the directories required for basic runtime operation.
// This includes Base() itself plus its required subdirectories.
// Callers should create these directories at startup.
func (l Layout) RuntimeDirs() []string {
	l.mustValid()
	return []string{l.base, l.DBDir(), l.SocketDir()}
}

// CSIDirs returns the directories required for CSI operation.
// Callers should create these directories only when CSI is enabled.
func (l Layout) CSIDirs() []string {
	l.mustValid()
	return []string{l.CSIDir(), l.CSIFSDir()}
}

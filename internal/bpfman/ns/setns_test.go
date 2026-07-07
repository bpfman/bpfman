//go:build bpfman_ns

package ns_test

import (
	"testing"

	"github.com/bpfman/bpfman/internal/bpfman/ns"
)

// TestConstructorWithSelfNamespace is the strongest proof that the
// constructor runs and the full setns path executes.
//
// The subprocess is launched with _BPFMAN_MNT_NS pointing at its
// own mount namespace (/proc/self/ns/mnt). This is a no-op
// namespace switch (same namespace) but it exercises the real code
// path: open the namespace file, call setns, clear the environment
// variable. The test asserts that the variable was cleared. If the
// constructor did not run the variable would still be present.
//
// Requires CAP_SYS_ADMIN; fails if not root. Skipped under QEMU
// user-mode emulation where setns is not supported.
//
// Build tag: bpfman_ns. Run via "make test-bpfman-ns" which adds
// -tags=bpfman_ns and sudo.
func TestConstructorWithSelfNamespace(t *testing.T) {
	t.Parallel()

	result := runHelper(t, []string{
		ns.MntNsEnvVar + "=/proc/self/ns/mnt",
	})
	if result.mntNsEnv != "cleared" {
		t.Fatalf("%s was not cleared by the constructor: env is %q", ns.MntNsEnvVar, result.mntNsEnv)
	}
	t.Logf("subprocess inode: %d (constructor cleared %s)", result.inode, ns.MntNsEnvVar)
}

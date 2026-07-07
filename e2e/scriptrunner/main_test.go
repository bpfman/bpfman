//go:build e2e

package scriptrunner

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/bpfman/bpfman/internal/registryfixture"
)

// e2eSuiteLockPath is the same system-wide flock the workload
// e2e binary holds. Sharing the path means the two test
// binaries are mutually exclusive on a single host: a
// developer can run `make test-e2e` and `make test-e2e-scripts`
// back-to-back without contention, but if they fire both in
// parallel the second one fails fast with a clear message
// instead of racing into shared kernel state. CI runs the two
// binaries on separate runners so the flock never contends
// there.
//
// Kept byte-identical to the workload binary's constant so the
// two definitions stay in sync.
const e2eSuiteLockPath = "/tmp/bpfman-e2e.lock"

// suiteLock is package-scoped so the open file (and thus the
// flock) lives for the full test process lifetime; closing
// the fd drops the lock. The OS releases everything on
// process exit.
var suiteLock *os.File
var closeSharedRegistry func()

func acquireSuiteLock() {
	f, err := os.OpenFile(e2eSuiteLockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scriptrunner: open suite lock %s: %v\n", e2eSuiteLockPath, err)
		os.Exit(1)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Fprintf(os.Stderr,
			"scriptrunner: another bpfman e2e test binary holds %s -- refusing to start.\n"+
				"    pid likely visible via: lsof %s   or   fuser %s\n"+
				"    if no such process exists, remove the lock file and retry.\n",
			e2eSuiteLockPath, e2eSuiteLockPath, e2eSuiteLockPath)
		f.Close()
		os.Exit(1)
	}

	suiteLock = f
}

// TestMain is the script runner's package-level setup. Smaller
// than the workload binary's TestMain because the scripts side
// has no helper-mode re-exec, no shared-runtime initialisation,
// no stale-dir cleanup (the address-pool builtin and short
// bpfman-shell lifetimes leave nothing of the kind the workload
// suite accumulates), and no self-exec discovery. Two load-
// bearing steps remain: refuse to run without root, take the
// suite-wide flock, and provision one shared anonymous image
// registry for child bpfman-shell processes. PATH is the
// caller's responsibility: bpfman-shell must be reachable via
// exec.LookPath when the test starts. The make recipe arranges
// this via `sudo env PATH=...`; direct invocations are expected
// to match.
func TestMain(m *testing.M) {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "scriptrunner: tests require root privileges")
		os.Exit(1)
	}
	acquireSuiteLock()
	if err := configureScriptEnvironment(); err != nil {
		fmt.Fprintf(os.Stderr, "scriptrunner: configure script environment: %v\n", err)
		os.Exit(1)
	}

	rc := m.Run()
	if closeSharedRegistry != nil {
		closeSharedRegistry()
	}
	os.Exit(rc)
}

func configureScriptEnvironment() error {
	if os.Getenv(registryfixture.RegistryEnv) == "" {
		host, closeFn, err := registryfixture.StartShared()
		if err != nil {
			return err
		}

		closeSharedRegistry = closeFn
		if err := os.Setenv(registryfixture.RegistryEnv, host); err != nil {
			closeFn()
			closeSharedRegistry = nil
			return err
		}
	}
	e2eDir := os.Getenv("BPFMAN_E2E_DIR")
	if e2eDir != "" {
		repoRoot := filepath.Dir(e2eDir)
		if err := os.Setenv("BPFMAN_E2E_REPO_ROOT", repoRoot); err != nil {
			return err
		}
	}
	return nil
}

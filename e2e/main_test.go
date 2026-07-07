//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"syscall"
	"testing"

	"github.com/bpfman/bpfman/ns/netns"
)

// e2eSuiteLockPath is a system-wide flock the suite holds for the
// duration of a run. The e2e tests share kernel state (bpffs,
// kprobes, perf events, network namespaces) and running two
// instances concurrently produces undefined results that look like
// flakes. Holding an exclusive non-blocking flock at TestMain entry
// makes a duplicate run fail fast with a clear message instead.
//
// The path deliberately does NOT start with "bpfman-e2e-" so it
// can't be picked up by cleanupStaleTestDirs's glob, which would
// unlink it on every run and break flock semantics (flock binds to
// an inode; replacing the inode silently invalidates other
// holders' locks).
const e2eSuiteLockPath = "/tmp/bpfman-e2e.lock"

// suiteLock is package-scoped only so the open file (and thus the
// flock) lives for the full test process lifetime; closing the fd
// drops the lock. The OS releases everything on process exit.
var suiteLock *os.File

func acquireSuiteLock() {
	f, err := os.OpenFile(e2eSuiteLockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: open suite lock %s: %v\n", e2eSuiteLockPath, err)
		os.Exit(1)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Fprintf(os.Stderr,
			"e2e: another e2e.test run holds %s -- refusing to start.\n"+
				"    pid likely visible via: lsof %s   or   fuser %s\n"+
				"    if no such process exists, remove the lock file and retry.\n",
			e2eSuiteLockPath, e2eSuiteLockPath, e2eSuiteLockPath)
		f.Close()
		os.Exit(1)
	}

	suiteLock = f
}

// e2eModeEnv selects an alternative process role. When set, the
// binary skips the Go test framework entirely and runs the named
// helper before exiting, so the same e2e.test binary serves as
// both the test driver and the uprobe attach target.
//
// Mode values are <verb>-<specifier>: the verb names the high-
// level behaviour, the specifier names which sibling helper to
// run. Picking a name per helper avoids retrofitting a "_2" or
// "_target" suffix once a second helper path is needed.
const (
	e2eModeEnv            = "BPFMAN_E2E_MODE"
	e2eModeWorkloadDriver = "workload-driver"
	// e2eModeHelperInitProbe lets a parent test re-exec the e2e
	// binary to verify package-init runs cleanly in a constrained
	// mount namespace. Used by helper_init_test.go to reproduce
	// the bpfman-ns subprocess environment where /proc may not
	// be mounted. The handler simply prints the marker line and
	// exits 0 -- if any imported package's init() touches a path
	// that requires /proc, the binary panics before reaching
	// here and the parent test sees the failure.
	e2eModeHelperInitProbe = "helper-init-probe"
	helperInitProbeMarker  = "HELPER_INIT_OK"
)

// selfExe is the absolute path of the running e2e.test binary,
// resolved once at TestMain time. Used by the uprobe tests both
// as the kernel's attach target (kernel resolves to inode +
// symbol-offset) and as the binary they re-exec via os/exec to
// fire the probe.
var selfExe string

func TestMain(m *testing.M) {
	// Helper-mode dispatch must run before the root check and
	// stale-dir cleanup: the parent test process invokes us via
	// exec.Command(os.Executable()) inheriting BPFMAN_E2E_MODE,
	// and the helper has nothing to clean up.
	switch os.Getenv(e2eModeEnv) {
	case e2eModeWorkloadDriver:
		runWorkloadDriver()
		os.Exit(0)
	case e2eModeHelperInitProbe:
		fmt.Println(helperInitProbeMarker)
		os.Exit(0)
	case "":
		// normal test driver mode
	default:
		fmt.Fprintf(os.Stderr, "unknown %s=%q\n", e2eModeEnv, os.Getenv(e2eModeEnv))
		os.Exit(2)
	}

	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "e2e tests require root privileges")
		os.Exit(1)
	}

	// Capture the process's startup netns inode now, on the
	// pristine TestMain goroutine (which runs on Go's main OS
	// thread before any test goroutine has had a chance to call
	// setns). The ns/netns package caches this under a sync.Once
	// and every later call to CurrentNSID / NSID("") returns the
	// captured value, regardless of which thread the caller has
	// landed on. Without this priming the first caller might be
	// a goroutine that an upstream library has left in a
	// non-root netns, and the cached value would be wrong for
	// the rest of the run. Failure here means /proc/self/ns/net
	// is unreadable, which makes the rest of the suite
	// meaningless -- abort loudly rather than carry on against a
	// stale cached zero.
	if _, err := netns.CurrentNSID(); err != nil {
		panic(fmt.Errorf("e2e: prime ns/netns capture: %v", err))
	}

	// Take exclusive lock before any cleanup or test setup so a
	// concurrent invocation aborts before stomping on shared
	// kernel/filesystem state.
	acquireSuiteLock()

	// Bind-mount /proc/self/ns/net at /run/netns/root so that
	// `ip netns exec root <cmd>` can target the test process's
	// root netns. Test commands that need to run in root can
	// then do so explicitly rather than depending on the
	// calling Go thread's current netns. See
	// netns_root_mount_test.go for rationale.
	if err := setupRootNetnsMount(); err != nil {
		fmt.Fprintf(os.Stderr, "setup root netns bind-mount: %v\n", err)
		os.Exit(1)
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "os.Executable: %v\n", err)
		os.Exit(1)
	}

	selfExe = exe

	if err := cleanupStaleTestDirs(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to clean stale test dirs: %v\n", err)
		os.Exit(1)
	}

	// Stand up the suite-wide runtime when shared mode is requested,
	// before any test runs. Tests pick it up via NewTestEnv.
	if sharedRuntimeMode() {
		if _, err := initSharedRuntime(); err != nil {
			fmt.Fprintf(os.Stderr, "shared runtime setup failed: %v\n", err)
			os.Exit(1)
		}

		code := m.Run()
		// Suite-end leak detection promotes a passing run to a
		// failure if any program or link survived: the t.Cleanup
		// chain is supposed to drain the manager to zero by the
		// time TestMain regains control.
		if leaked := teardownSharedRuntime(sharedRuntime); leaked && code == 0 {
			fmt.Fprintln(os.Stderr, "e2e suite teardown: residual state at suite end -- failing the run")
			code = 1
		}
		os.Exit(code)
	}

	os.Exit(m.Run())
}

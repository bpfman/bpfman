// Tests for the ns package's C constructor and namespace
// switching behaviour.
//
// # Background
//
// The ns package contains a C function (nsexec) that runs as a
// GCC constructor -- meaning it executes automatically when the
// binary starts, before Go's runtime initialises. This is the only
// point in the process lifetime where setns(CLONE_NEWNS) can work,
// because the process is still single-threaded.
//
// The constructor checks for the _BPFMAN_MNT_NS environment
// variable. If set, it opens that namespace path, calls setns to
// switch into it, and then clears the variable. If unset, it
// returns immediately and Go starts normally.
//
// # Why subprocess testing
//
// The constructor runs once, at process startup. By the time a
// normal test function executes the constructor has already
// finished. We cannot re-trigger it, so we cannot observe its
// behaviour from within the current process. To test it we must
// launch a fresh process where the constructor runs with controlled
// inputs and then inspect what happened.
//
// The tests use the standard Go subprocess pattern: the test binary
// re-executes itself with -test.run=^TestHelperProcess$ and a
// sentinel environment variable (_BPFMANNS_TEST_HELPER=1). The
// helper function detects this, does its work, and exits. The
// parent test examines the helper's output and exit status.
//
// # What each test proves
//
// TestGetCurrentMntNsInode: basic sanity -- the package built with
// CGO and GetCurrentMntNsInode works in the current process.
//
// TestConstructorWithoutNamespace: launches a subprocess without
// _BPFMAN_MNT_NS but with debug logging. Asserts that nsexec
// produced its expected debug message on stderr, proving the
// constructor fired. Does not require elevated privileges.
//
// TestConstructorWithSelfNamespace (build tag "bpfman_ns"): launches
// a subprocess with _BPFMAN_MNT_NS=/proc/self/ns/mnt. After setns
// the constructor clears the variable. The helper reports whether
// the variable is gone. This is the strongest proof: if the
// constructor did not run, the variable is still present and the
// test fails. Requires CAP_SYS_ADMIN (root) because setns demands
// it. Skipped under QEMU user-mode where setns is unsupported.
//
// # Cross-architecture testing
//
// These tests also run on arm64, ppc64le, and s390x under QEMU
// user-mode emulation (via go test -exec). The subprocess re-exec
// works transparently when binfmt_misc is registered for the
// target architecture; if not, the subprocess tests skip with an
// "exec format error" diagnostic.
//
// # What cross-architecture testing does and does not prove
//
// The cross-architecture tests under QEMU user-mode prove that the
// CGO code compiles, links, and executes on each target CPU
// architecture: the constructor fires and nsexec runs. They do not
// prove the setns path works on those architectures because QEMU
// user-mode translates instructions, not the kernel -- setns is a
// kernel namespace operation that QEMU user-mode cannot perform
// (it returns EINVAL). On a native runner for the target
// architecture (real hardware or QEMU system emulation with a full
// guest kernel) all tests run identically to amd64, including the
// setns proof. We consider QEMU system emulation too heavyweight
// for CI at present.
package ns_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/bpfman/bpfman/internal/bpfman/ns"
)

// TestHelperProcess is the subprocess entry point, not a real test.
// Other tests re-execute the test binary with _BPFMANNS_TEST_HELPER=1
// to reach this code. During a normal test run (no sentinel variable)
// the function returns immediately and costs nothing.
//
// By the time Go calls this function the C constructor has already
// run. If the parent set _BPFMAN_MNT_NS, the constructor either
// consumed it (setns succeeded, variable cleared) or killed the
// process (_exit(1) on failure). The helper reports the mount
// namespace inode and whether the variable survived, giving the
// parent everything it needs for its assertions.
func TestHelperProcess(t *testing.T) {
	t.Parallel()

	if os.Getenv("_BPFMANNS_TEST_HELPER") != "1" {
		return
	}
	defer os.Exit(0)

	inode, err := ns.GetCurrentMntNsInode()
	if err != nil {
		fmt.Fprintf(os.Stderr, "GetCurrentMntNsInode: %v\n", err)
		os.Exit(2)
	}
	if inode == 0 {
		fmt.Fprintf(os.Stderr, "GetCurrentMntNsInode returned zero\n")
		os.Exit(2)
	}

	mntNsVal := os.Getenv(ns.MntNsEnvVar)
	envState := "cleared"
	if mntNsVal != "" {
		envState = "present"
	}
	fmt.Printf("BPFMANNS_OK inode=%d mntns_env=%s\n", inode, envState)
}

// TestGetCurrentMntNsInode is a basic sanity check that the package
// built with CGO and GetCurrentMntNsInode returns a valid inode.
func TestGetCurrentMntNsInode(t *testing.T) {
	t.Parallel()

	inode, err := ns.GetCurrentMntNsInode()
	if err != nil {
		t.Fatalf("GetCurrentMntNsInode: %v", err)
	}
	if inode == 0 {
		t.Fatal("GetCurrentMntNsInode returned zero")
	}
	t.Logf("current mount namespace inode: %d", inode)
}

// TestConstructorWithoutNamespace proves that the constructor fires
// and nsexec runs even when no namespace switch is requested.
//
// The subprocess is launched without _BPFMAN_MNT_NS but with debug
// logging enabled. When nsexec runs and finds no namespace variable
// it emits a debug message and returns. The test asserts that this
// message appears on stderr. Without it the test could only prove
// "Go started," which would still pass if the constructor were
// dead.
func TestConstructorWithoutNamespace(t *testing.T) {
	t.Parallel()

	result := runHelper(t, []string{
		ns.LogLevelEnvVar + "=debug",
	})
	if !strings.Contains(result.stderr, "not set, returning to Go runtime") {
		t.Fatalf("nsexec did not run (no debug output on stderr)\nstderr:\n%s", result.stderr)
	}
	t.Logf("subprocess inode: %d", result.inode)
}

type helperResult struct {
	inode    uint64
	mntNsEnv string // "cleared" or "present"
	stderr   string
}

// runHelper re-executes the test binary as a subprocess with the
// given extra environment variables and parses the helper's output.
func runHelper(t *testing.T, extraEnv []string) helperResult {
	t.Helper()

	var stdout, stderr bytes.Buffer

	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Propagate only the minimum environment. QEMU_LD_PREFIX is
	// included so that binfmt_misc-triggered QEMU can find the
	// target architecture's dynamic libraries when the subprocess
	// re-executes a cross-compiled binary.
	cmd.Env = filterEnv(os.Environ(),
		"PATH", "HOME", "TMPDIR", "USER",
		"QEMU_LD_PREFIX",
	)
	cmd.Env = append(cmd.Env, "_BPFMANNS_TEST_HELPER=1")
	cmd.Env = append(cmd.Env, extraEnv...)

	err := cmd.Run()
	if err != nil {
		errOut := stderr.String()
		errMsg := err.Error()
		if strings.Contains(errMsg, "exec format error") {
			t.Skipf("cannot exec cross-compiled binary (binfmt_misc not registered?): %v", err)
		}
		if strings.Contains(errOut, "Operation not permitted") ||
			strings.Contains(errOut, "EPERM") {
			t.Fatalf("setns failed (run with sudo or use make test-bpfman-ns): %s", errOut)
		}
		if strings.Contains(errOut, "Invalid argument") ||
			strings.Contains(errOut, "EINVAL") {
			t.Skipf("setns not supported (QEMU user-mode cannot perform namespace operations):\n%s", errOut)
		}
		t.Fatalf("subprocess failed: %v\nstderr:\n%s\nstdout:\n%s", err, errOut, stdout.String())
	}

	return parseHelperOutput(t, stdout.String(), stderr.String())
}

func parseHelperOutput(t *testing.T, stdout, stderr string) helperResult {
	t.Helper()

	const marker = "BPFMANNS_OK inode="
	_, after, ok := strings.Cut(stdout, marker)
	if !ok {
		t.Fatalf("subprocess did not report\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}

	line := after
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	line = strings.TrimSpace(line)

	// Expected format: "12345 mntns_env=cleared"
	parts := strings.SplitN(line, " ", 2)
	inode, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		t.Fatalf("failed to parse inode from %q: %v", parts[0], err)
	}
	if inode == 0 {
		t.Fatal("subprocess reported inode 0")
	}

	var mntNsEnv string
	if len(parts) > 1 {
		if after, ok := strings.CutPrefix(parts[1], "mntns_env="); ok {
			mntNsEnv = after
		}
	}

	return helperResult{inode: inode, mntNsEnv: mntNsEnv, stderr: stderr}
}

func filterEnv(env []string, keys ...string) []string {
	keep := make(map[string]bool, len(keys))
	for _, k := range keys {
		keep[k] = true
	}
	var out []string
	for _, e := range env {
		if k, _, ok := strings.Cut(e, "="); ok && keep[k] {
			out = append(out, e)
		}
	}
	return out
}

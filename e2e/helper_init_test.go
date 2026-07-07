//go:build e2e

package e2e

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestPackageInitSurvivesAbsentProc reproduces the environment of
// the bpfman-ns helper subprocess and asserts that the e2e
// binary's package-init code paths complete without panicking
// when /proc is not mounted in the calling process's mount
// namespace.
//
// Why: the bpfman-ns helper runs after the C constructor in
// internal/bpfman/ns/nsexec.c switches it into a target container's
// mount namespace before Go's runtime starts. Many Kubernetes
// target containers (particularly stripped-down ones built
// from scratch images) do not mount procfs, so the helper
// finds itself running in a mount namespace where
// /proc/self/ns/net is unreachable. Any package init() that
// reads that path with fail-on-error semantics crashes the
// helper before main() runs; the parent that fork+exec'd the
// helper sees an empty SCM_RIGHTS socket close and reports a
// misleading "unexpected oob length" error several layers
// removed from the cause.
//
// Construction:
//
// We re-exec the e2e binary inside a dedicated mount namespace
// where /proc has been unmounted. The unshare(1) tool (from
// util-linux, present on every Fedora/Ubuntu image) takes care
// of unshare(CLONE_NEWNS) and the propagation mark; a tiny sh
// -c invocation then does the lazy umount and exec's the test
// binary in helper-init-probe mode. Doing the namespace work in
// a child process keeps the parent's Go runtime and OS threads
// untouched -- attempting the unshare in the test goroutine
// poisons the calling thread's mount namespace and Go's runtime
// requires /proc on its own threads for tracebacks and signal
// handling, leading to non-deterministic process-wide failures.
//
// Pass condition: child exits 0 and stdout contains the marker
// line. Failure modes: child non-zero exit (panic in init,
// observable via stderr) or missing marker.
//
// A regression in any imported package's init that introduces a
// /proc dependency surfaces here as a single targeted test
// failure.
func TestPackageInitSurvivesAbsentProc(t *testing.T) {
	t.Parallel()

	unsharePath, err := exec.LookPath("unshare")
	if err != nil {
		t.Skipf("unshare(1) not available, skipping: %v", err)
	}

	umountPath, err := exec.LookPath("umount")
	if err != nil {
		t.Skipf("umount(8) not available, skipping: %v", err)
	}

	shPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh not available, skipping: %v", err)
	}

	// `unshare --mount --propagation private`:
	//   creates a fresh mount namespace and marks "/" private
	//   so the inner umount stays local.
	// `sh -c '"$1" -l /proc; exec "$2"' helperInitProbe <umount> <bin>`:
	//   lazy-unmounts /proc inside the new namespace, then
	//   exec's the e2e binary so it inherits the constrained
	//   namespace as its own. Both umount and the target
	//   binary are passed in by absolute path so the script
	//   does not depend on a particular PATH being inherited
	//   into the constrained env -- the test must work on
	//   NixOS, Fedora, and Ubuntu, where the binaries live in
	//   different places. Lazy umount (-l) is used because
	//   busy-mount errors would obscure the diagnostic we care
	//   about; ENOENT after the umount is what matters.
	script := `"$1" -l /proc; exec "$2"`
	cmd := exec.Command(unsharePath,
		"--mount",
		"--propagation", "private",
		shPath, "-c", script,
		"helperInitProbe", // $0 (script name, unused)
		umountPath,        // $1
		selfExe,           // $2
	)
	cmd.Env = []string{
		e2eModeEnv + "=" + e2eModeHelperInitProbe,
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr != nil {
		t.Errorf("helper-init-probe subprocess failed: %v\n"+
			"This usually means a package init() in the import graph "+
			"depends on /proc being mounted, which the bpfman-ns helper "+
			"subprocess cannot guarantee.\n\nstdout:\n%s\nstderr:\n%s",
			runErr, stdout.String(), stderr.String())
		return
	}

	if !strings.Contains(stdout.String(), helperInitProbeMarker) {
		t.Errorf("helper-init-probe subprocess did not emit %q marker.\n"+
			"stdout:\n%s\nstderr:\n%s",
			helperInitProbeMarker, stdout.String(), stderr.String())
	}
}

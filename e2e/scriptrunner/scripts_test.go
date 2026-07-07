//go:build e2e

package scriptrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	k8slabels "k8s.io/apimachinery/pkg/labels"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/scriptmeta"
	"github.com/bpfman/bpfman/internal/execcancel"
	"github.com/bpfman/bpfman/internal/tcpolicy"
)

// TestBPFManScripts discovers every .bpfman script under
// e2e/scripts/ and runs each one as a subtest by invoking
// bpfman-shell from PATH. The caller is responsible for
// putting bpfman-shell on PATH; the test does no binary
// discovery or PATH manipulation. Under sudo this means the
// invocation has to bypass secure_path itself (the make recipe
// uses `sudo env PATH=...`; a developer invoking the binary
// directly should arrange their PATH similarly).
//
// The package's TestMain already enforces root and takes the
// suite-wide flock, so each subtest just runs the script
// directly with no further elevation.
//
// Subtests call t.Parallel() by default: scripts use the
// address-pool-backed `net veth-pair` builtin (see
// cmd/bpfman-shell/internal/builtins/netpool.go) and are safe
// to run concurrently by construction.
//
// Script labels: a `.bpfman` script can declare labels by putting
// `#pragma labels={"external":"true","program":"xdp"}` in the same
// header window.
// `serial=true` puts the script through the serial lane: it
// still enters the Go parallel queue so registration can complete
// and parallel-eligible scripts can start, but it takes a
// package-local serial mutex before executing. `exclusive=true`
// scripts do not call t.Parallel, so they run during registration
// while parallel subtests are still queued; use this only for
// scripts that mutate global bpfman state, such as `bpfman program
// delete --all`.
//
// BPFMAN_E2E_SCRIPT_SELECTOR selects scripts with a Kubernetes-style
// comma-separated label selector. The default selector is `!external`,
// so public-registry pulls run only when explicitly selected.
//
// Subtest names are the script's path relative to e2e/, so
// `go test -run 'TestBPFManScripts/scripts/Foo\.bpfman$'`
// selects one script (escape the dot and anchor the end to
// avoid prefix matches against longer names). Under stress mode
// (BPFMAN_E2E_SCRIPT_REPEATS>1, see below) the registered name
// for a parallel-eligible script gains a `#<r>` suffix, so the
// $-anchored form silently matches no subtests. The portable
// shapes are:
//
//   - one script, one run:
//     go test -run 'TestBPFManScripts/scripts/Foo\.bpfman$'
//   - one script, all repetitions:
//     BPFMAN_E2E_SCRIPT_REPEATS=10 \
//     go test -run 'TestBPFManScripts/scripts/Foo\.bpfman(#\d+)?$'
//   - one script, either mode (drop the anchor; prefix-matches
//     longer-named scripts that share the prefix):
//     go test -run 'TestBPFManScripts/scripts/Foo\.bpfman'
//
// BPFMAN_E2E_SCRIPT_REPEATS turns the corpus into a stress
// run: each script is registered N times as
// `scripts/<name>.bpfman#<r>` (r in [0, N)), with the outer
// loop being repeat and the inner loop being the script list.
// Registering in that order means consecutive subtests in the
// t.Parallel queue are different scripts, which gives the
// address pool's `net veth-pair` builtin maximum name
// diversity per dispatched wave. Unset or N=1 keeps the
// default one-pass behaviour and the unsuffixed subtest names.
func TestBPFManScripts(t *testing.T) {
	timeout := scriptTimeout()
	repeats := scriptRepeats()
	e2eDir := e2ePackageDir(t)

	// -test.failfast cannot interrupt an already-released
	// t.Parallel() wave, so the flag is a no-op for parallel
	// scripts. Drive the abort cooperatively instead, preserving
	// the parallel model: the first failing script cancels
	// abortCtx, which tears down in-flight scripts and makes
	// not-yet-started ones skip.
	failfast := false
	if f := flag.Lookup("test.failfast"); f != nil {
		failfast = f.Value.String() == "true"
	}
	abortCtx, abortCancel := context.WithCancel(context.Background())
	// t.Cleanup, not defer: with t.Parallel() the parent function
	// returns before the paused subtests resume, so a defer would
	// cancel abortCtx before any script runs and skip them all.
	// t.Cleanup runs only after every subtest completes.
	t.Cleanup(abortCancel)

	const sub = "scripts"
	absSub := filepath.Join(e2eDir, sub)
	matches, err := filepath.Glob(filepath.Join(absSub, "*.bpfman"))
	if err != nil {
		t.Fatalf("glob %s: %v", absSub, err)
	}

	if len(matches) == 0 {
		t.Fatalf("no .bpfman scripts found under %s/", absSub)
	}
	// Pre-scan once per pass so a script's header is read at
	// most once even under stress repeats. Metadata errors are
	// stored per script and reported inside that script's
	// subtest, so one malformed pragma does not abort
	// registration of the rest of the corpus.
	selector := scriptLabelSelector(t)
	serial := make(map[string]bool, len(matches))
	exclusive := make(map[string]bool, len(matches))
	metadata := make(map[string]scriptMetadata, len(matches))
	for _, abs := range matches {
		meta := readScriptMetadata(abs)
		metadata[abs] = meta
		if meta.err != nil {
			continue
		}
		if meta.mode.Labels.Get("serial") == "true" {
			serial[abs] = true
		}
		if meta.mode.Labels.Get("exclusive") == "true" {
			exclusive[abs] = true
		}
	}
	// Outer loop: repeat. Inner loop: scripts. Pass r=0 of
	// every script enters the dispatcher first, then r=1,
	// and so on; the t.Parallel queue therefore holds
	// [s1#0, s2#0, ..., sN#0, s1#1, ...] which preserves
	// wave diversity across repeats. Scripts marked serial
	// skip the repeat: they are serialised with other serial
	// scripts by scriptSerialMu, so extra registrations only
	// burn wall-clock time without tickling new race windows.
	for r := range repeats {
		for _, abs := range matches {
			if (serial[abs] || exclusive[abs]) && r > 0 {
				continue
			}
			rel := filepath.Join(sub, filepath.Base(abs))
			// Suffix only when the script actually
			// participates in the repeat cycle. A
			// serial script runs exactly once even under
			// stress, so it keeps its unsuffixed name;
			// otherwise `-test.run 'Foo#3'` against a
			// serial script would silently match no
			// subtests.
			name := filepath.ToSlash(rel)
			if repeats > 1 && !serial[abs] && !exclusive[abs] {
				name = fmt.Sprintf("%s#%d", name, r)
			}
			runSerial := serial[abs]
			runExclusive := exclusive[abs]
			meta := metadata[abs]
			skipReason := ""
			if meta.err == nil {
				skipReason = scriptSelectorSkipReason(selector, meta.mode.Labels)
				if skipReason == "" &&
					meta.mode.Labels.Get("requires-clsact-reclaim") == "true" &&
					!tcpolicy.ReclaimClsactOnDetach {
					skipReason = "tcpolicy.ReclaimClsactOnDetach is false; flip it to true to run this"
				}
			}
			t.Run(name, func(t *testing.T) {
				if meta.err != nil {
					t.Fatalf("read script metadata: %v", meta.err)
				}
				if skipReason != "" {
					t.Skip(skipReason)
				}
				if !runExclusive {
					t.Parallel()
				}
				if runSerial {
					scriptSerialMu.Lock()
					defer scriptSerialMu.Unlock()
				}
				if failfast {
					if abortCtx.Err() != nil {
						t.Skip("failfast: skipped after an earlier failure")
					}
					defer func() {
						if t.Failed() {
							abortCancel()
						}
					}()
				}
				if err := emitScriptTimelineMarker("script_start", t.Name()); err != nil {
					t.Fatalf("write script timeline start marker: %v", err)
				}

				defer func() {
					if err := emitScriptTimelineMarker("script_end", t.Name()); err != nil {
						t.Errorf("write script timeline end marker: %v", err)
					}
				}()
				runBPFManScript(t, e2eDir, rel, timeout, failfast, abortCtx)
			})
		}
	}
}

// e2ePackageDir returns the absolute path of the e2e directory
// where the .bpfman corpora and testdata live. Resolution order:
//
//  1. BPFMAN_E2E_DIR if set. This is the authoritative form and
//     is what the Makefile recipe passes (set to $(abspath e2e)),
//     so CI builds that ship the test binary in one filesystem
//     and run it in another always get the right path.
//
//  2. The runtime.Caller-derived path otherwise. Convenient for
//     direct local invocations of bin/e2e-scripts.test from any
//     cwd, but only works when the binary still has access to
//     its build-time source tree -- which is true for local
//     `go test` / `go build` runs and false for static binaries
//     built inside a container and extracted to a different host.
func e2ePackageDir(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("BPFMAN_E2E_DIR"); d != "" {
		return d
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed and BPFMAN_E2E_DIR is unset; cannot anchor e2e dir")
	}
	// file is .../e2e/scriptrunner/scripts_test.go; walk up
	// two levels to reach the e2e/ directory where the script
	// corpora and testdata live.
	dir := filepath.Dir(filepath.Dir(file))
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("runtime.Caller-derived e2e dir %s is not accessible (set BPFMAN_E2E_DIR for cross-filesystem builds): %v", dir, err)
	}
	return dir
}

// bpfmanShellTimeoutEnv overrides the per-script deadline.
// Without it each script gets bpfmanShellTimeoutDefault to
// complete; a deadline-exceeded run fails that one subtest
// cleanly rather than hanging the whole binary against
// go test's outer -timeout.
//
// bpfmanShellRepeatsEnv is the stress knob: each script is
// registered N times so the t.Parallel queue holds a wave-
// diverse mix the dispatcher can fan out at the configured
// -test.parallel concurrency. Unset or N<=1 keeps the default
// one-pass behaviour.
const (
	bpfmanShellTimeoutEnv         = "BPFMAN_E2E_SCRIPT_TIMEOUT"
	bpfmanShellTimeoutDefault     = 5 * time.Minute
	bpfmanShellRepeatsEnv         = "BPFMAN_E2E_SCRIPT_REPEATS"
	bpfmanShellRepeatsDefault     = 1
	bpfmanShellTimelineEnv        = "BPFMAN_E2E_SCRIPT_TIMELINE"
	bpfmanShellSelectorEnv        = "BPFMAN_E2E_SCRIPT_SELECTOR"
	bpfmanShellSelectorDefaultRaw = "!external"
	bpfmanShellTestPackage        = "github.com/bpfman/bpfman/e2e/scriptrunner"
)

var scriptTimelineMu sync.Mutex
var scriptSerialMu sync.Mutex

type scriptTimelineMarker struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test"`
}

func emitScriptTimelineMarker(action, testName string) error {
	path := os.Getenv(bpfmanShellTimelineEnv)
	if path == "" {
		return nil
	}
	scriptTimelineMu.Lock()
	defer scriptTimelineMu.Unlock()

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	defer func() { _ = f.Close() }()

	if err := json.NewEncoder(f).Encode(scriptTimelineMarker{
		Time:    time.Now(),
		Action:  action,
		Package: bpfmanShellTestPackage,
		Test:    testName,
	}); err != nil {
		return err
	}

	return nil
}

func scriptTimeout() time.Duration {
	raw := os.Getenv(bpfmanShellTimeoutEnv)
	if raw == "" {
		return bpfmanShellTimeoutDefault
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return bpfmanShellTimeoutDefault
	}

	return d
}

func scriptRepeats() int {
	raw := os.Getenv(bpfmanShellRepeatsEnv)
	if raw == "" {
		return bpfmanShellRepeatsDefault
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return bpfmanShellRepeatsDefault
	}

	return n
}

type scriptMetadata struct {
	mode scriptmeta.Mode
	err  error
}

func readScriptMetadata(path string) scriptMetadata {
	mode, err := scriptmeta.Read(path)
	if err != nil {
		return scriptMetadata{err: err}
	}

	return scriptMetadata{mode: mode}
}

func scriptLabelSelector(t *testing.T) k8slabels.Selector {
	t.Helper()
	raw, ok := os.LookupEnv(bpfmanShellSelectorEnv)
	if !ok {
		raw = bpfmanShellSelectorDefaultRaw
	}

	selector, err := k8slabels.Parse(raw)
	if err != nil {
		t.Fatalf("parse %s=%q: %v", bpfmanShellSelectorEnv, raw, err)
	}

	return selector
}

func scriptSelectorSkipReason(selector k8slabels.Selector, labels k8slabels.Set) string {
	if selector.Matches(labels) {
		return ""
	}
	return fmt.Sprintf("script labels %s do not match %s=%s", labels.String(), bpfmanShellSelectorEnv, selector.String())
}

// runBPFManScript executes one .bpfman script under a per-script
// deadline. Output goes to a single combined buffer (matching
// the Bash runners' user-visible shape) and is dumped via
// t.Fatalf on failure or via t.Logf on success; either way the
// Go test framework owns the framing so go test -json picks it
// up cleanly.
func runBPFManScript(t *testing.T, e2eDir, script string, timeout time.Duration, failfast bool, abortCtx context.Context) {
	t.Helper()
	timeoutCause := fmt.Errorf("script %s exceeded %s: %w", script, timeout, context.DeadlineExceeded)
	ctx, cancel := context.WithTimeoutCause(t.Context(), timeout, timeoutCause)
	defer cancel()
	if failfast {
		// Cancel this in-flight script when an earlier script
		// has failed, so the failfast abort propagates to the
		// bpfman-shell subprocess rather than waiting it out.
		stop := context.AfterFunc(abortCtx, cancel)
		defer stop()
	}

	cmd := exec.CommandContext(ctx, "bpfman-shell", script)
	// Script paths inside the corpus reference testdata
	// relative to e2e/, so the child has to run with cwd at
	// the package dir regardless of where the test binary was
	// invoked from. PATH is already set up at TestMain (BIN_DIR
	// prepended once, before any exec.Command).
	cmd.Dir = e2eDir
	out, err := runScriptCommand(ctx, cmd)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("%s timed out after %s: %v\n\n%s", script, timeout, context.Cause(ctx), out)
	}
	if failfast && abortCtx.Err() != nil {
		t.Skipf("%s skipped: failfast abort after an earlier failure", script)
	}
	if err != nil {
		t.Fatalf("%s failed: %v\n\n%s", script, err, out)
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		t.Logf("%s", s)
	}
}

func TestRunScriptCommand_AllowsInterruptHandlerToFinish(t *testing.T) {
	t.Parallel()

	ack := filepath.Join(t.TempDir(), "ack")
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", "trap 'sleep 0.4; echo cleaned > \"$1\"; exit 0' INT; sleep 5", "sh", ack)
	_, err := runScriptCommand(ctx, cmd)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Eventually(t, func() bool {
		_, statErr := os.Stat(ack)
		return statErr == nil
	}, time.Second, 20*time.Millisecond)
}

func runScriptCommand(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	cancelled := execcancel.Configure(cmd)
	// A signalled bpfman-shell drains its registered defers under a
	// bounded cleanup context (the driver's deferDrainBudget, 5s)
	// before exiting; concurrent siblings serialise their deferred
	// unloads on the global writer lock, so a contended drain can
	// legitimately need several seconds. execcancel's default Grace
	// (2s) SIGKILLs the shell before that completes and the
	// interrupted script leaks store state into every later run.
	// Outwait the drain budget; the hard bound on a wedged shell is
	// still here, just generous enough for cleanup to finish.
	cmd.WaitDelay = 10 * time.Second
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if cancelled.Load() {
		return bytes.Clone(out.Bytes()), context.Cause(ctx)
	}
	return bytes.Clone(out.Bytes()), err
}

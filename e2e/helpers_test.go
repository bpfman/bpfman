//go:build e2e

package e2e

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	ciliumebpf "github.com/cilium/ebpf"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/e2e/residue"
	"github.com/bpfman/bpfman/e2e/testnet"
	"github.com/bpfman/bpfman/fs"
	fsruntime "github.com/bpfman/bpfman/fs/runtime"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/lock"
	"github.com/bpfman/bpfman/logging"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/platform"
	"github.com/bpfman/bpfman/platform/ebpf"
	"github.com/bpfman/bpfman/platform/image/oci"
	"github.com/bpfman/bpfman/platform/image/verify"
	"github.com/bpfman/bpfman/platform/store/sqlite"
)

// TestEnv provides a test environment for e2e tests.
//
// By default (BPFMAN_E2E_ISOLATED_RUNTIME unset) NewTestEnv hands
// back a view onto the suite-wide runtime; the per-test cleanup
// (unmount bpffs, remove temp dir, close store) is then a no-op and
// the suite owns those operations end-to-end via teardownSharedRuntime
// in TestMain. Setting BPFMAN_E2E_ISOLATED_RUNTIME=1 opts each test
// out and gives it its own runtime, enabling fully-isolated parallel
// runs at the cost of cross-test concurrency coverage. See
// shared_runtime_test.go for the rationale.
type TestEnv struct {
	T           *testing.T
	Layout      fs.Layout
	Manager     *manager.Manager
	ImagePuller platform.ImagePuller
	logger      *slog.Logger
	baseDir     string // parent directory containing layout, cache
	closeEnv    func() error
	// shared is true when this TestEnv is a view onto the suite-wide
	// runtime rather than a per-test runtime; cleanup() is a no-op
	// in that case and the suite-end teardown owns global teardown.
	shared bool

	// scopeMu guards the per-test scope sets below. In shared mode
	// concurrent tests run against the same manager, so the
	// TestEnv's bookkeeping has to be safe against the test's own
	// callers parallelising helpers (none today, but cheap to keep
	// correct).
	scopeMu       sync.Mutex
	scopePrograms map[kernel.ProgramID]struct{}
	scopeLinks    map[bpfman.LinkID]struct{}
}

// NewTestEnv creates an isolated test environment for e2e testing.
// The environment includes:
//   - A unique runtime directory in /tmp/bpfman-e2e-<pid>-<testname>/
//   - A fresh SQLite database
//   - A bpffs mount
//   - A manager instance for BPF operations
//
// The environment is automatically cleaned up via t.Cleanup().
func NewTestEnv(t *testing.T) *TestEnv {
	t.Helper()

	// Shared-runtime mode: hand back a view onto the suite-wide
	// runtime that TestMain stood up. The per-test bpffs mount,
	// store, and manager are skipped; cleanup is a no-op. Note
	// that AssertCleanState and friends still operate on global
	// state in this mode. Today, running multiple tests
	// concurrently in shared mode will trip the global checks.
	if sharedRuntimeMode() {
		rt := requireSharedRuntimeForTest(t)
		env := &TestEnv{
			T:             t,
			Layout:        rt.layout,
			Manager:       rt.manager,
			ImagePuller:   rt.imagePuller,
			logger:        rt.logger.With("test", t.Name()),
			baseDir:       rt.baseDir,
			closeEnv:      nil,
			shared:        true,
			scopePrograms: make(map[kernel.ProgramID]struct{}),
			scopeLinks:    make(map[bpfman.LinkID]struct{}),
		}
		t.Cleanup(env.cleanup)
		return env
	}

	// Create unique directory for this test
	baseDir, err := os.MkdirTemp("", fmt.Sprintf("bpfman-e2e-%d-", os.Getpid()))
	if err != nil {
		t.Fatalf("failed to create temp directory: %v", err)
	}

	layout, err := fs.New(baseDir)
	if err != nil {
		t.Fatalf("invalid runtime directory: %v", err)
	}

	imageCacheBase, err := fs.NewImageCache(filepath.Join(layout.Base(), "cache", "image"))
	if err != nil {
		t.Fatalf("invalid image cache directory: %v", err)
	}

	imageCache, err := fs.EnsureCache(imageCacheBase)
	if err != nil {
		t.Fatalf("failed to ensure image cache: %v", err)
	}

	// Set up logger based on BPFMAN_LOG environment variable.
	// Examples:
	//   BPFMAN_LOG=debug           - all components at debug
	//   BPFMAN_LOG=info,store=debug - default info, store (SQL) at debug
	var logger *slog.Logger
	if envSpec := os.Getenv("BPFMAN_LOG"); envSpec != "" {
		var err error
		logger, err = logging.New(logging.Options{
			EnvSpec: envSpec,
			Format:  logging.FormatText,
			Output:  os.Stderr,
		})
		if err != nil {
			t.Fatalf("invalid BPFMAN_LOG spec: %v", err)
		}
	} else if false {
		// Disabled diagnostic path: route every record through
		// t.Logf at Info level so the verify: extension link
		// lines from the dispatcher rebuild paths surface in
		// test output on failure. Re-enable by changing
		// `if false` to a true condition.
		logger = slog.New(newTLogHandler(t, slog.LevelInfo))
	} else {
		// Default: only errors
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelError,
		}))
	}

	// Create store
	ctx := context.Background()
	var store platform.Store
	err = lock.RunWithTiming(ctx, layout.LockPath(), logger, func(ctx context.Context, writeLock lock.WriterScope) error {
		var err error
		store, err = sqlite.New(ctx, layout.DBPath(), logger, writeLock)
		return err
	})
	require.NoError(t, err, "failed to create store")

	// Create kernel adapter
	kernel := ebpf.New(ebpf.WithLogger(logger))

	// Ensure runtime directories and bpffs mount
	ensuredRuntime, err := fsruntime.New(layout, fsruntime.RealMounter{}, logger)
	require.NoError(t, err, "failed to ensure runtime")

	// Create signature verifier (disabled for tests)
	verifier := verify.NoSign()

	// Create image puller for OCI images
	puller, err := oci.NewPuller(
		imageCache,
		oci.WithLogger(logger),
		oci.WithVerifier(verifier),
	)
	require.NoError(t, err, "failed to create image puller")

	// Create manager
	mgr, err := manager.New(ensuredRuntime, puller, store, kernel, ebpf.NewProgramValidator(), logger)
	require.NoError(t, err, "failed to create manager")

	cleanup := func() error {
		return store.Close()
	}

	env := &TestEnv{
		T:           t,
		Layout:      layout,
		Manager:     mgr,
		ImagePuller: puller,
		logger:      logger,
		baseDir:     baseDir,
		closeEnv:    cleanup,
	}

	// Register cleanup
	t.Cleanup(func() {
		env.cleanup()
	})

	return env
}

// cleanup releases resources and removes test directories.
// Failures are reported and cause the test to fail.
//
// In shared-runtime mode this is a no-op: the bpffs mount, store,
// and base directory belong to the suite, not to any single test,
// and teardownSharedRuntime in TestMain owns those operations.
// Per-test resource lifecycle (programs loaded, links attached) is
// still managed by the test's own t.Cleanup callbacks via
// env.Detach / env.Unload, exactly as in isolated mode.
func (e *TestEnv) cleanup() {
	if e.shared {
		return
	}
	if e.closeEnv != nil {
		if err := e.closeEnv(); err != nil {
			e.T.Errorf("failed to close environment: %v", err)
		}
	}

	// Unmount bpffs that was mounted by NewTestEnv
	bpffsMount := e.Layout.BPFFSMountPoint()
	e.T.Logf("unmounting bpffs at %s", bpffsMount)
	if err := unmount(bpffsMount); err != nil {
		e.T.Errorf("failed to unmount bpffs: %v", err)
	}

	// Remove the test directory
	e.T.Logf("removing test directory %s", e.baseDir)
	if err := os.RemoveAll(e.baseDir); err != nil {
		e.T.Errorf("failed to remove %s: %v", e.baseDir, err)
	}
}

// runWithLock executes a function under the writer lock. Routes
// through lock.RunWithTiming so wait_ms / held_ms appear in the
// log stream tagged component=lock; emit at Debug level, so the
// default e2e logger (Error level) still drops them and the
// instrumentation is free unless explicitly opted into via
// BPFMAN_LOG=lock=debug (or a coarser BPFMAN_LOG=debug).
//
// Tags every entry with op=<calling-method> via runtime.Caller and
// test=<t.Name()> via the env logger, so a shared-runtime BPFMAN_LOG
// run can be slice-and-diced by operation (LoadFile, Attach, ...)
// or by test to find the outliers behind shared-mode wall-clock.
func (e *TestEnv) runWithLock(ctx context.Context, fn func(context.Context, lock.WriterScope) error) error {
	op := callerOp()
	return lock.RunWithTiming(ctx, e.Layout.LockPath(), e.logger.With("op", op), fn)
}

// callerOp returns the unqualified name of the function that
// called runWithLock -- e.g. "LoadFile", "Attach". Used to tag
// the lock-timing log entries so shared-runtime BPFMAN_LOG runs
// can be aggregated by operation type. Returns "?" if the stack
// inspection fails; callers must not rely on this for control
// flow.
func callerOp() string {
	pc, _, _, ok := runtime.Caller(2)
	if !ok {
		return "?"
	}

	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "?"
	}
	name := fn.Name()
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// LoadFile loads BPF programs from a local object file.
//
// Relative paths are resolved against BytecodeDir, the on-disk
// testdata/bpf/ object tree. This lets call sites keep their
// historical "testdata/bpf/foo.bpf.o" form regardless of cwd.
//
// Manager.Load conditionally acquires the writer lock for explicit
// map-owner joins and PinByName loads.
func (e *TestEnv) LoadFile(ctx context.Context, filePath string, programs []manager.ProgramSpec, opts manager.LoadOpts) ([]bpfman.Program, error) {
	if !filepath.IsAbs(filePath) {
		filePath = BytecodePath(filePath)
	}
	result, err := e.Manager.Load(ctx, manager.LoadSource{
		FilePath: filePath,
	}, programs, opts)
	if err == nil {
		e.trackPrograms(result)
	}

	return result, err
}

// Unload unloads a BPF program.
func (e *TestEnv) Unload(ctx context.Context, programID kernel.ProgramID) error {
	err := e.runWithLock(ctx, func(ctx context.Context, writeLock lock.WriterScope) error {
		return e.Manager.Unload(ctx, writeLock, programID)
	})
	if err == nil {
		e.untrackProgram(programID)
	}

	return err
}

// List returns the managed programs visible to this TestEnv.
//
// In isolated mode (the default) this is everything the manager
// knows about, since the manager is per-test. In shared-runtime
// mode the result is filtered to programs this TestEnv created via
// LoadFile / LoadImage and not yet Unloaded -- callers that wrote
// against the historical "shows my programs" expectation continue
// to work without scope-awareness leaking into every test.
func (e *TestEnv) List(ctx context.Context) ([]bpfman.Program, error) {
	result, err := e.Manager.ListPrograms(ctx)
	if err != nil {
		return nil, err
	}

	if !e.shared {
		return result, nil
	}
	mine := make([]bpfman.Program, 0, e.scopeProgramCount())
	for _, p := range result {
		if p.Status.Kernel == nil {
			continue
		}
		e.scopeMu.Lock()
		_, ok := e.scopePrograms[p.Status.Kernel.ID]
		e.scopeMu.Unlock()
		if ok {
			mine = append(mine, p)
		}
	}
	return mine, nil
}

// Get returns detailed information about a program.
func (e *TestEnv) Get(ctx context.Context, programID kernel.ProgramID) (bpfman.Program, error) {
	return e.Manager.Get(ctx, programID)
}

// Attach attaches a program using the given spec. The writer lock is
// acquired automatically and passed to the manager.
func (e *TestEnv) Attach(ctx context.Context, spec bpfman.AttachSpec) (bpfman.LinkRecord, error) {
	var result bpfman.Link
	err := e.runWithLock(ctx, func(ctx context.Context, writeLock lock.WriterScope) error {
		link, attachErr := e.Manager.Attach(ctx, writeLock, spec)
		result = link
		return attachErr
	})
	if err != nil {
		return bpfman.LinkRecord{}, err
	}

	e.trackLink(result.Record.ID)
	record, err := e.Manager.GetLink(ctx, result.Record.ID)
	if err != nil {
		return bpfman.LinkRecord{ID: result.Record.ID}, nil
	}

	return record, nil
}

// Detach detaches a link.
func (e *TestEnv) Detach(ctx context.Context, linkID bpfman.LinkID) error {
	err := e.runWithLock(ctx, func(ctx context.Context, writeLock lock.WriterScope) error {
		return e.Manager.Detach(ctx, writeLock, linkID)
	})
	if err == nil {
		e.untrackLink(linkID)
	}

	return err
}

// trackPrograms records every successfully loaded program in the
// TestEnv's local set so AssertProgramCount and AssertCleanState
// can return scope-local answers under shared mode. Cheap in
// isolated mode (the assertion helpers ignore the set there).
func (e *TestEnv) trackPrograms(progs []bpfman.Program) {
	if len(progs) == 0 {
		return
	}
	e.scopeMu.Lock()
	defer e.scopeMu.Unlock()
	if e.scopePrograms == nil {
		e.scopePrograms = make(map[kernel.ProgramID]struct{}, len(progs))
	}
	for _, p := range progs {
		if p.Status.Kernel == nil {
			continue
		}
		e.scopePrograms[p.Status.Kernel.ID] = struct{}{}
	}
}

func (e *TestEnv) untrackProgram(id kernel.ProgramID) {
	e.scopeMu.Lock()
	defer e.scopeMu.Unlock()
	delete(e.scopePrograms, id)
}

func (e *TestEnv) trackLink(id bpfman.LinkID) {
	e.scopeMu.Lock()
	defer e.scopeMu.Unlock()
	if e.scopeLinks == nil {
		e.scopeLinks = make(map[bpfman.LinkID]struct{})
	}
	e.scopeLinks[id] = struct{}{}
}

func (e *TestEnv) untrackLink(id bpfman.LinkID) {
	e.scopeMu.Lock()
	defer e.scopeMu.Unlock()
	delete(e.scopeLinks, id)
}

func (e *TestEnv) scopeProgramCount() int {
	e.scopeMu.Lock()
	defer e.scopeMu.Unlock()
	return len(e.scopePrograms)
}

func (e *TestEnv) scopeLinkCount() int {
	e.scopeMu.Lock()
	defer e.scopeMu.Unlock()
	return len(e.scopeLinks)
}

func (e *TestEnv) scopeContainsLink(id bpfman.LinkID) bool {
	e.scopeMu.Lock()
	defer e.scopeMu.Unlock()
	_, ok := e.scopeLinks[id]
	return ok
}

// ListLinks returns the managed links visible to this TestEnv.
// Scope-aware in shared-runtime mode; see List for the rationale.
func (e *TestEnv) ListLinks(ctx context.Context) ([]bpfman.LinkRecord, error) {
	all, err := e.Manager.ListLinks(ctx)
	if err != nil {
		return nil, err
	}

	if !e.shared {
		return all, nil
	}
	mine := make([]bpfman.LinkRecord, 0, e.scopeLinkCount())
	for _, l := range all {
		if e.scopeContainsLink(l.ID) {
			mine = append(mine, l)
		}
	}
	return mine, nil
}

// GetLink returns detailed information about a link.
func (e *TestEnv) GetLink(ctx context.Context, linkID bpfman.LinkID) (bpfman.LinkRecord, bpfman.LinkDetails, error) {
	record, err := e.Manager.GetLink(ctx, linkID)
	if err != nil {
		return bpfman.LinkRecord{}, nil, err
	}

	return record, record.Details, nil
}

// GetDispatcherSnapshot retrieves the full dispatcher snapshot for the
// given type, namespace, and interface.
func (e *TestEnv) GetDispatcherSnapshot(ctx context.Context, key dispatcher.Key) (platform.DispatcherSnapshot, error) {
	return e.Manager.GetDispatcherSnapshot(ctx, key)
}

// AssertCleanState verifies that no programs or links are managed.
func (e *TestEnv) AssertCleanState() {
	e.T.Helper()
	e.AssertProgramCount(0)
	e.AssertLinkCount(0)
}

// AssertProgramCount verifies the number of managed programs.
//
// In shared-runtime mode the assertion is scoped to programs this
// TestEnv created (via env.LoadFile / env.LoadImage and not yet
// Unloaded), since the manager's global view also contains other
// concurrent tests' programs. In isolated mode the per-test
// manager has only this test's programs, so a global list is
// equivalent; we keep the global path for that mode unchanged.
func (e *TestEnv) AssertProgramCount(expected int) {
	e.T.Helper()
	if e.shared {
		got := e.scopeProgramCount()
		require.Equal(e.T, expected, got, "unexpected scope-local program count (shared runtime mode); want=%d got=%d", expected, got)
		return
	}
	ctx := context.Background()
	programs, err := e.List(ctx)
	require.NoError(e.T, err, "failed to list programs")
	require.Len(e.T, programs, expected, "unexpected program count")
}

// AssertLinkCount verifies the total number of managed links.
// Scope-aware in shared-runtime mode; see AssertProgramCount.
func (e *TestEnv) AssertLinkCount(expected int) {
	e.T.Helper()
	if e.shared {
		got := e.scopeLinkCount()
		require.Equal(e.T, expected, got, "unexpected scope-local link count (shared runtime mode); want=%d got=%d", expected, got)
		return
	}
	ctx := context.Background()
	links, err := e.ListLinks(ctx)
	require.NoError(e.T, err, "failed to list links")
	require.Len(e.T, links, expected, "unexpected link count")
}

// RequireRoot fails the test if not running as root.
func RequireRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Fatal("test requires root privileges")
	}
}

// RequireIsolatedRuntime skips the test under the shared-runtime
// default (BPFMAN_E2E_ISOLATED_RUNTIME unset). Use it for tests
// whose assertions are globally scoped -- e.g. "after my last
// unload the shared bpffs pin is removed" -- which are correct in
// the per-test runtime where this test owns the entire bpffs, but
// cannot hold under shared mode where concurrent tests legitimately
// keep the same shared resources alive. The skip carries a reason
// so a shared-mode run still reports clearly that something was
// deliberately not exercised.
func RequireIsolatedRuntime(t *testing.T, reason string) {
	t.Helper()
	if sharedRuntimeMode() {
		t.Skipf("skipped under shared runtime (set BPFMAN_E2E_ISOLATED_RUNTIME=1 to exercise): %s", reason)
	}
}

// RequireBTF fails the test if kernel BTF is not available.
// BTF is required for fentry/fexit program types.
func RequireBTF(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/sys/kernel/btf/vmlinux"); os.IsNotExist(err) {
		t.Fatal("test requires kernel BTF support (/sys/kernel/btf/vmlinux)")
	}
}

// RequireKernelVersion fails the test if the kernel version is below the specified version.
// Useful for features like TCX which require kernel 6.6+.
func RequireKernelVersion(t *testing.T, major, minor int) {
	t.Helper()

	data, err := os.ReadFile("/proc/version")
	if err != nil {
		t.Fatalf("cannot read /proc/version: %v", err)
		return
	}

	// Parse kernel version from /proc/version
	// Format: "Linux version X.Y.Z-..."
	re := regexp.MustCompile(`Linux version (\d+)\.(\d+)`)
	matches := re.FindStringSubmatch(string(data))
	if len(matches) < 3 {
		t.Fatalf("cannot parse kernel version from /proc/version")
		return
	}

	kernelMajor, _ := strconv.Atoi(matches[1])
	kernelMinor, _ := strconv.Atoi(matches[2])

	if kernelMajor < major || (kernelMajor == major && kernelMinor < minor) {
		t.Fatalf("test requires kernel %d.%d+, have %d.%d", major, minor, kernelMajor, kernelMinor)
	}
}

// RequireTC fails the test if the tc command is not available.
func RequireTC(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("tc"); err != nil {
		t.Fatal("test requires tc command (iproute2)")
	}
}

// tcIngressFilters returns the TC filters attached to the ingress
// qdisc of the named network interface.
func tcIngressFilters(t *testing.T, ifaceName string) []netlink.Filter {
	t.Helper()
	link, err := netlink.LinkByName(ifaceName)
	require.NoError(t, err)
	filters, err := netlink.FilterList(link, netlink.HANDLE_MIN_INGRESS)
	require.NoError(t, err)
	return filters
}

// unmount unmounts a filesystem.
func unmount(path string) error {
	// Use lazy unmount to avoid "device busy" errors
	cmd := fmt.Sprintf("umount -l %q 2>/dev/null", path)
	return runCommand(cmd)
}

// runCommand executes a shell command.
func runCommand(cmd string) error {
	c := []string{"sh", "-c", cmd}
	proc := os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	}
	p, err := os.StartProcess("/bin/sh", c, &proc)
	if err != nil {
		return err
	}

	state, err := p.Wait()
	if err != nil {
		return err
	}

	if !state.Success() {
		return fmt.Errorf("command failed: %s", cmd)
	}
	return nil
}

// tcFilterCount returns the number of BPF tc filters on the given
// interface and direction by shelling out to tc(8). This matches the
// upstream Rust bpfman approach to verification.
func tcFilterCount(t *testing.T, iface, direction string) int {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Wrapped via `ip netns exec root` so the netns is
	// determined by the bind-mount at /run/netns/root rather
	// than by whichever Go thread happens to perform the fork.
	out, err := exec.CommandContext(ctx, "ip", "netns", "exec", testnet.RootNetns, "tc", "filter", "show", "dev", iface, direction).CombinedOutput()
	if err != nil {
		t.Logf("tc filter show dev %s %s: %v (output: %s)", iface, direction, err, out)
		return 0
	}

	count := 0
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.Contains(line, "pref") {
			count++
		}
	}
	return count
}

const staleTestDirPrefix = "bpfman-e2e-"

// cleanupStaleTestArtifacts removes leftover test interfaces,
// namespaces, and directories from previous runs. When the
// interface / netns sweep finds anything, the per-step action
// list is written to stderr so an interrupted prior run is
// announced rather than silently swept.
func cleanupStaleTestDirs() error {
	plan, failures, err := residue.CleanupStaleInterfaces(residue.DefaultBPFFS, residue.DefaultNetnsDir)
	if err != nil {
		return err
	}

	if !plan.Empty() {
		fmt.Fprintln(os.Stderr, "e2e pre-flight removed residue from a prior run:")
		plan.Describe(os.Stderr)
	}
	for _, f := range failures {
		fmt.Fprintf(os.Stderr, "e2e pre-flight FAIL: %s: %v\n", f.Action.Describe(), f.Err)
	}
	if len(failures) > 0 {
		return fmt.Errorf("e2e pre-flight: %d action(s) failed", len(failures))
	}

	tempDir := os.TempDir()
	pattern := filepath.Join(tempDir, staleTestDirPrefix+"*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob %s: %w", pattern, err)
	}

	for _, path := range matches {
		// Safety: verify path is under tempDir and has expected prefix.
		if err := validateStaleTestDir(path, tempDir); err != nil {
			return fmt.Errorf("refusing to remove %s: %w", path, err)
		}

		// Defensive: only stale TEST DIRECTORIES are in scope here.
		// Files matching the glob (notably the suite-lock file the
		// e2eSuiteLockPath now lives outside the prefix on principle,
		// but anything else that ends up in /tmp under the prefix
		// without being a directory is not ours to remove) are left
		// alone.
		fi, err := os.Lstat(path)
		if err != nil || !fi.IsDir() {
			continue
		}

		// Check if the PID in the directory name is still running
		parts := strings.Split(filepath.Base(path), "-")
		if len(parts) >= 3 {
			pid, err := strconv.Atoi(parts[2])
			if err == nil {
				// Check if process exists
				if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
					// Process still running, skip
					continue
				}
			}
		}

		// Attempt to unmount bpffs; ignore errors as it may already
		// be unmounted or never mounted.
		layout, err := fs.New(path)
		if err == nil {
			unmount(layout.BPFFSMountPoint())
		}

		// Remove the entire test directory
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}

	return nil
}

// validateStaleTestDir ensures path is safe to remove.
func validateStaleTestDir(path, tempDir string) error {
	// Must be absolute
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path is not absolute")
	}

	// Must be under tempDir
	cleanPath := filepath.Clean(path)
	cleanTempDir := filepath.Clean(tempDir)
	if !strings.HasPrefix(cleanPath, cleanTempDir+string(filepath.Separator)) {
		return fmt.Errorf("path %q is not under temp dir %q", cleanPath, cleanTempDir)
	}

	// Must have the expected prefix
	base := filepath.Base(cleanPath)
	if !strings.HasPrefix(base, staleTestDirPrefix) {
		return fmt.Errorf("path %q does not have prefix %q", base, staleTestDirPrefix)
	}

	// Must not be a top-level directory (sanity check)
	if cleanPath == "/" || strings.Count(cleanPath, string(filepath.Separator)) < 2 {
		return fmt.Errorf("path %q is too short", cleanPath)
	}

	return nil
}

// mapIDByName resolves a program's kernel map by name and returns its
// kernel-assigned ID. Useful for tests that need to read a specific
// named counter map without depending on the kernel's MapIDs ordering
// (which is not contractually stable when a program owns multiple
// maps).
func mapIDByName(t *testing.T, prog bpfman.Program, name string) kernel.MapID {
	t.Helper()
	require.NotNil(t, prog.Status.Kernel, "program has no kernel info")
	for _, m := range prog.Status.Maps {
		if m.Name == name {
			return m.ID
		}
	}
	t.Fatalf("program %d has no map named %q (maps: %+v)", prog.Status.Kernel.ID, name, prog.Status.Maps)
	return 0
}

// readArrayCounterByID opens a BPF_MAP_TYPE_ARRAY map by its
// kernel-assigned ID and returns the uint64 value at key 0. Used by
// tests where the BPF program filters in-kernel and writes to a
// single counter slot.
func readArrayCounterByID(t *testing.T, mapID kernel.MapID) uint64 {
	t.Helper()

	m, err := ciliumebpf.NewMapFromID(ciliumebpf.MapID(mapID))
	require.NoError(t, err, "open map ID %d", mapID)
	defer m.Close()

	var key uint32 = 0
	var val uint64
	err = m.Lookup(key, &val)
	require.NoError(t, err, "lookup key 0 in map ID %d", mapID)
	return val
}

// assertCounterQuiet drives `fire` and asserts that the named
// counter map on `prog` does not advance. Use after Detach to
// prove that detach actually stopped the BPF program firing, not
// just removed bpfman's link record. A perf-link that keeps
// firing post-detach would surface here as a non-zero delta even
// though `events * weight == count` passed pre-detach, because the
// singleton tests never fire post-detach traffic by themselves.
// Applies to every program type -- the bug is
// perf-link specific but the property "detach stopped it" is
// worth pinning down uniformly.
func assertCounterQuiet(t *testing.T, prog bpfman.Program, mapName string, fire func()) {
	t.Helper()
	mapID := mapIDByName(t, prog, mapName)
	before := readArrayCounterByID(t, mapID)
	fire()
	after := readArrayCounterByID(t, mapID)
	requireCounterEqual(t, before, after,
		"counter %q should be quiet after detach", mapName)
}

// requireCounterEqual asserts want == got and, on mismatch, prints
// both sides plus the delta in decimal. testify's require.Equal
// renders uint64 mismatches through spew which prefixes them with
// 0x; the surrounding test diagnostics (events, weights, before/
// after counts) are decimal, so the hex/decimal split makes failure
// triage harder than it needs to be. Using a plain t.Fatalf keeps
// every number in one base.
func requireCounterEqual(t *testing.T, want, got uint64, format string, args ...any) {
	t.Helper()
	if want == got {
		return
	}
	prefix := fmt.Sprintf(format, args...)
	t.Fatalf("%s: want=%d got=%d delta=%d", prefix, want, got, int64(got)-int64(want))
}

// QuiescenceResult is what waitDetachQuiescent reports back so callers
// can both check that the barrier was reached and fold the probe events
// into their expected counts.
type QuiescenceResult struct {
	// Probes is the total number of single-event workload firings
	// driven during the wait. Each probe still flows through the
	// kernel hook, so siblings of the just-detached program (which
	// remain attached) WILL count it; callers must add Probes to
	// the expected counter for any sibling that is still attached.
	Probes int
	// EventsCounted is the number of probes the detached program
	// itself counted before quiescence. Telemetry: under perfect
	// synchronous detach this is 0; values > 0 measure the kernel
	// deferral (RCU GP + workqueue) in events.
	EventsCounted int
	// Latency is the wall-clock time from first probe to declaring
	// quiescence. Telemetry only.
	Latency time.Duration
}

// QuiescenceProbe configures waitDetachQuiescent.
type QuiescenceProbe struct {
	// DetachedMap is the counter map of the just-detached program.
	// The barrier waits until this counter has been stable across
	// StableProbes consecutive probes.
	DetachedMap    kernel.MapID
	DetachedWeight uint64

	// ControlMap, if non-zero, is the counter map of a still-
	// attached sibling on the same hook. After the barrier, the
	// helper asserts ControlMap advanced by exactly
	// Probes*ControlWeight, catching the "workload is broken so the
	// hook never fires" false-negative case (where a never-firing
	// counter looks identical to a successfully detached one). Pass
	// 0 to skip -- typically only for singleton tests where the
	// pre-detach pass already proved the workload reaches the hook.
	ControlMap    kernel.MapID
	ControlWeight uint64

	// FireOne drives exactly one workload event that would hit the
	// program's hook if it were still attached.
	FireOne func()

	// StableProbes is how many consecutive non-moving counter reads
	// declare quiescence. Default 3.
	StableProbes int
	// Deadline is the upper bound on the entire wait. Default 500ms.
	Deadline time.Duration
}

// waitDetachQuiescent fires single workload events one at a time and
// reads the just-detached program's counter after each, returning when
// the counter has been stable for StableProbes consecutive probes
// (the detach has demonstrably taken effect kernel-side) or fails the
// test if the deadline expires while the counter is still moving.
//
// This is the only correct primitive for proving "this BPF program
// stopped firing" on bpf-perf-link types (kprobe, kretprobe, uprobe,
// uretprobe, tracepoint, fentry, fexit). The kernel exposes no
// synchronous teardown for those: pin removal is RCU-deferred via the bpffs
// inode's free_inode super_op, and the FD close path can only free
// when it drops the LAST ref, which it never does while the pin is
// alive. The deferred bpf_link_free is what eventually runs
// perf_event_detach_bpf_prog and removes the program from the
// trace_event's prog_array. Any fixed sleep is wrong on slow runners
// eventually; polling the program's own counter is the lagging
// observable that adapts to actual kernel timing.
//
// FireOne must drive exactly one workload event that would hit the
// program's hook if it were still attached. For tests that share a
// hook between several programs, every probe also lands on the still-
// attached siblings -- the caller adds the returned Probes to each
// sibling's expected counter to keep assertions exact.
//
// If ControlMap is set, the helper sanity-checks that ControlMap
// advanced by exactly Probes*ControlWeight after the barrier; this
// catches the false-negative where the workload no longer reaches the
// hook (counter never increments, looks like a clean detach).
func waitDetachQuiescent(t *testing.T, p QuiescenceProbe) QuiescenceResult {
	t.Helper()
	stableProbes := p.StableProbes
	if stableProbes <= 0 {
		stableProbes = 3
	}
	deadline := p.Deadline
	if deadline <= 0 {
		deadline = 500 * time.Millisecond
	}

	start := time.Now()
	initial := readArrayCounterByID(t, p.DetachedMap)
	var controlInitial uint64
	if p.ControlMap != 0 {
		controlInitial = readArrayCounterByID(t, p.ControlMap)
	}
	last := initial
	stable := 0
	probes := 0

	for time.Since(start) < deadline {
		p.FireOne()
		probes++
		now := readArrayCounterByID(t, p.DetachedMap)
		if now == last {
			stable++
			if stable >= stableProbes {
				result := QuiescenceResult{
					Probes:        probes,
					EventsCounted: int((now - initial) / p.DetachedWeight),
					Latency:       time.Since(start),
				}
				if p.ControlMap != 0 {
					controlDelta := readArrayCounterByID(t, p.ControlMap) - controlInitial
					expected := uint64(probes) * p.ControlWeight
					require.Equal(t, expected, controlDelta, "control sibling counter delta should equal probes(%d) * weight(%d) = %d after barrier; got %d. Workload likely not hitting the hook -- 'quiescence' would be a false positive", probes, p.ControlWeight, expected, controlDelta)
				}
				return result
			}
		} else {
			stable = 0
			last = now
		}
	}
	t.Fatalf("waitDetachQuiescent: counter still moving after %s and %d probes (delta=%d, weight=%d)",
		deadline, probes, last-initial, p.DetachedWeight)
	return QuiescenceResult{}
}

func waitKmodSlotDetachQuiescent(
	t *testing.T,
	slot KmodSlot,
	detachedMap kernel.MapID,
	detachedWeight uint64,
	controlMap kernel.MapID,
	controlWeight uint64,
) QuiescenceResult {
	t.Helper()
	return waitDetachQuiescent(t, QuiescenceProbe{
		DetachedMap:    detachedMap,
		DetachedWeight: detachedWeight,
		ControlMap:     controlMap,
		ControlWeight:  controlWeight,
		FireOne:        func() { slot.Fire(t, 1) },
	})
}

// uniqueWeights returns n distinct random uint64 weights derived
// from a fresh test-scoped seed. Used by tests that pass per-program
// weights as global data so the BPF program's counter is a verifiable
// function of (events x weight) rather than a bare event tally.
//
// Weights are forced to differ from each other so that a "wrong map"
// or "swapped indices" bug is detectable. The high bit is cleared so
// that events x weight cannot overflow for realistic event counts.
func uniqueWeights(t *testing.T, n int) []uint64 {
	t.Helper()
	r := newTestRand(t)
	seen := make(map[uint64]struct{}, n)
	out := make([]uint64, 0, n)
	for len(out) < n {
		w := r.Uint64() & ((1 << 40) - 1)
		if w == 0 {
			continue
		}
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
	}
	return out
}

// newTestRand returns a math/rand source seeded uniquely per test.
// The seed is logged so a failing test is reproducible when the
// random weights matter.
func newTestRand(t *testing.T) *rand.Rand {
	t.Helper()
	var seedBytes [8]byte
	_, err := cryptorand.Read(seedBytes[:])
	require.NoError(t, err, "read crypto/rand seed")
	seed := int64(binary.LittleEndian.Uint64(seedBytes[:]))
	t.Logf("test rand seed: %d", seed)
	return rand.New(rand.NewSource(seed))
}

// uint32LE encodes v as 4 little-endian bytes, the form bpfman
// global-data injection expects for `volatile const __u32`.
func uint32LE(v uint32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return b[:]
}

// uint64LE encodes v as 8 little-endian bytes, the form bpfman
// global-data injection expects for `volatile const __u64`.
func uint64LE(v uint64) []byte {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	return b[:]
}

// tLogHandler is an slog.Handler that emits records via t.Logf
// at the configured minimum level. Go's testing framework
// buffers t.Logf output per test and shows it only when the
// test fails (or is run with -v), so wiring bpfman's logger
// through it surfaces diagnostic lines exactly when they help
// without spamming successful runs. Each NewTestEnv call wires
// its own handler bound to its own *testing.T so logs are
// attributed to the right test even under -test.parallel.
type tLogHandler struct {
	t     *testing.T
	level slog.Leveler
	attrs []slog.Attr
	group string
}

func newTLogHandler(t *testing.T, level slog.Level) *tLogHandler {
	return &tLogHandler{t: t, level: level}
}

func (h *tLogHandler) Enabled(_ context.Context, lvl slog.Level) bool {
	return lvl >= h.level.Level()
}

func (h *tLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.t.Helper()
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s %s", r.Level, r.Message)
	for _, a := range h.attrs {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value.Any())
	}
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value.Any())
		return true
	})
	h.t.Logf("%s", b.String())
	return nil
}

func (h *tLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	nh := *h
	nh.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &nh
}

func (h *tLogHandler) WithGroup(name string) slog.Handler {
	nh := *h
	if h.group != "" {
		nh.group = h.group + "." + name
	} else {
		nh.group = name
	}
	return &nh
}

// kmodTargetsRoot is the debugfs directory the bpfman_e2e_targets
// kernel module exposes once loaded. Each entry trigger_NNN under
// it, when written, invokes bpfman_e2e_target_N once.
const kmodTargetsRoot = "/sys/kernel/debug/bpfman_e2e"

// kmodSlotPoolSize is the number of module slots the Go e2e suite
// leases concurrently. Must match BPFMAN_E2E_NUM_SLOTS in the module
// source.
const kmodSlotPoolSize = 128

var (
	kmodSlotPool     chan int
	kmodSlotPoolOnce sync.Once
)

func initKmodSlotPool() {
	kmodSlotPool = make(chan int, kmodSlotPoolSize)
	for i := range kmodSlotPoolSize {
		kmodSlotPool <- i
	}
}

// KmodSlot identifies one slot in the bpfman_e2e_targets module.
// Each fentry/fexit test that uses a slot owns it for its lifetime;
// no two tests share a slot, so attach/detach against the slot's
// function does not contend with sibling tests on the trampoline.
type KmodSlot struct {
	// Index is the slot number, 0 <= Index < kmodSlotPoolSize.
	Index int
	// Func is the kernel-resolvable name of the function this
	// slot exports. Use as bpfman ProgramSpec.AttachFunc when
	// loading a fentry/fexit program against this slot.
	Func string
	// TriggerPath is the debugfs file whose write(2) invokes
	// Func once. Test code uses this to drive events.
	TriggerPath string
}

// RequireKmodTargets skips the test if the bpfman_e2e_targets
// kernel module is not loaded. The module must be loaded once
// per host (typically via the NixOS module that ships its .ko)
// before any kmod-targeting test can run.
func RequireKmodTargets(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(kmodTargetsRoot); err != nil {
		t.Skipf("bpfman_e2e_targets kmod not available at %s: %v (load the module first)",
			kmodTargetsRoot, err)
	}
}

// acquireKmodSlot reserves an unused slot from the
// bpfman_e2e_targets module and registers a t.Cleanup that
// returns the slot to the pool when the test ends. Blocks if
// every slot is in use; in practice the pool size exceeds the
// concurrent count of kmod-targeting tests by a wide margin so
// this is effectively non-blocking.
func acquireKmodSlot(t *testing.T) KmodSlot {
	t.Helper()
	kmodSlotPoolOnce.Do(initKmodSlotPool)

	idx := <-kmodSlotPool
	t.Cleanup(func() { kmodSlotPool <- idx })

	return KmodSlot{
		Index:       idx,
		Func:        fmt.Sprintf("bpfman_e2e_target_%d", idx),
		TriggerPath: fmt.Sprintf("%s/trigger_%03d", kmodTargetsRoot, idx),
	}
}

// Fire issues n write(2) calls to the slot's trigger file, each
// invoking the slot's function once. Buffer contents are
// ignored by the kernel module; callers control event count by
// the number of writes.
func (s KmodSlot) Fire(t *testing.T, n int) {
	t.Helper()
	f, err := os.OpenFile(s.TriggerPath, os.O_WRONLY, 0)
	require.NoError(t, err, "open trigger %s", s.TriggerPath)
	defer f.Close()
	for i := range n {
		if _, err := f.Write([]byte{0}); err != nil {
			t.Fatalf("write trigger %s (event %d/%d): %v", s.TriggerPath, i+1, n, err)
		}
	}
}

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	bpfman "github.com/bpfman/bpfman"
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

// e2eIsolatedRuntimeEnv opts the suite out of its production-shaped
// default (one bpffs mount, one sqlite store, one manager instance
// shared across tests) and into per-test isolated runtimes (each
// test gets its own). Set BPFMAN_E2E_ISOLATED_RUNTIME=1 to opt in;
// the default (env unset) keeps the shared production-shaped path,
// which surfaces concurrency, resource-tracking, and cross-tenant
// interactions that the isolated lane papers over.
const e2eIsolatedRuntimeEnv = "BPFMAN_E2E_ISOLATED_RUNTIME"

// e2eSuiteRootEnv opts out of the production-shaped runtime root
// (/run/bpfman) and points the suite at a custom path instead.
// Useful when a real bpfman-rpc daemon is live on the host, when
// running multiple e2e processes side by side, or when a clean
// slate is genuinely needed for attribution. Unset by default so
// the suite exercises the same paths a real daemon does -- that is
// the only configuration where GC has to converge against actual
// inter-run residue, where kernel-program-pinned-but-not-in-db is
// a meaningful rule, and where end-of-suite teardown does work
// rather than just `rm -rf`.
const e2eSuiteRootEnv = "BPFMAN_E2E_SUITE_ROOT"

// e2eDaemonSocket is the canonical bpfman-rpc socket path. Its
// presence indicates a live daemon owns fs.DefaultRoot; the e2e
// suite refuses to run against the production layout in that
// case to prevent silent data corruption.
const e2eDaemonSocket = fs.DefaultRoot + "-sock/bpfman.sock"

// sharedRuntimeMode reports whether tests should reuse the suite-wide
// runtime (the default) instead of building one per test. Setting
// BPFMAN_E2E_ISOLATED_RUNTIME=1 inverts this and gives each test a
// fresh runtime.
func sharedRuntimeMode() bool {
	return os.Getenv(e2eIsolatedRuntimeEnv) != "1"
}

// suiteRuntime is the singleton runtime stood up by TestMain when
// shared mode is active. Tests read it via NewTestEnv; the package
// is not the access boundary -- tests still reach it through TestEnv
// so the rest of the helpers can stay mode-agnostic.
type suiteRuntime struct {
	layout      fs.Layout
	manager     *manager.Manager
	kernel      platform.KernelOperations
	imagePuller platform.ImagePuller
	logger      *slog.Logger
	baseDir     string
	closeStore  func() error

	// baselinePrograms / baselineLinks record the program and link
	// IDs that were already in the store when the suite opened the
	// DB. The end-of-suite leak detector excludes these so residue
	// inherited from a prior crashed run is not misreported as a
	// this-run leak: the production manager hands
	// crash recovery back to the operator and so legitimately keeps
	// rows around across runs in shared mode. The leak detector's
	// real job is to catch programs and links this run created and
	// did not clean up before TestMain returned -- exactly the
	// delta these baselines let us compute.
	baselinePrograms map[kernel.ProgramID]struct{}
	baselineLinks    map[bpfman.LinkID]struct{}
}

var (
	sharedRuntime     *suiteRuntime
	sharedRuntimeOnce sync.Once
	sharedRuntimeErr  error
)

// initSharedRuntime stands up the suite-wide runtime. Idempotent:
// the first call performs setup, subsequent calls return the cached
// instance. Caller is TestMain; tests just observe sharedRuntime.
func initSharedRuntime() (*suiteRuntime, error) {
	sharedRuntimeOnce.Do(func() {
		sharedRuntime, sharedRuntimeErr = buildSuiteRuntime()
	})
	return sharedRuntime, sharedRuntimeErr
}

func buildSuiteRuntime() (*suiteRuntime, error) {
	baseDir := os.Getenv(e2eSuiteRootEnv)
	if baseDir == "" {
		baseDir = fs.DefaultRoot
		if _, err := os.Stat(e2eDaemonSocket); err == nil {
			return nil, fmt.Errorf("refusing to run e2e against %s: bpfman-rpc daemon is live (socket present at %s); stop the daemon or set %s to a writable path elsewhere", baseDir, e2eDaemonSocket, e2eSuiteRootEnv)
		}
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create suite root %s: %w", baseDir, err)
	}

	layout, err := fs.New(baseDir)
	if err != nil {
		return nil, fmt.Errorf("invalid suite layout: %w", err)
	}

	imageCacheBase, err := fs.NewImageCache(filepath.Join(layout.Base(), "cache", "image"))
	if err != nil {
		return nil, fmt.Errorf("invalid image cache directory: %w", err)
	}

	imageCache, err := fs.EnsureCache(imageCacheBase)
	if err != nil {
		return nil, fmt.Errorf("ensure image cache: %w", err)
	}

	logger, err := buildLogger()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	var store platform.Store
	err = lock.RunWithTiming(ctx, layout.LockPath(), logger, func(ctx context.Context, writeLock lock.WriterScope) error {
		var err error
		store, err = sqlite.New(ctx, layout.DBPath(), logger, writeLock)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create suite store: %w", err)
	}

	kernel := ebpf.New(ebpf.WithLogger(logger))

	ensuredRuntime, err := fsruntime.New(layout, fsruntime.RealMounter{}, logger)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("ensure suite runtime: %w", err)
	}

	verifier := verify.NoSign()
	puller, err := oci.NewPuller(
		imageCache,
		oci.WithLogger(logger),
		oci.WithVerifier(verifier),
	)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("create image puller: %w", err)
	}

	mgr, err := manager.New(ensuredRuntime, puller, store, kernel, ebpf.NewProgramValidator(), logger)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("create suite manager: %w", err)
	}

	baselinePrograms, baselineLinks, err := snapshotBaseline(ctx, mgr)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("snapshot suite baseline: %w", err)
	}

	logger.Info("shared e2e runtime ready", "base", baseDir, "bpffs", layout.BPFFSMountPoint(), "baseline_programs", len(baselinePrograms), "baseline_links", len(baselineLinks))

	return &suiteRuntime{
		layout:           layout,
		manager:          mgr,
		kernel:           kernel,
		imagePuller:      puller,
		logger:           logger,
		baseDir:          baseDir,
		closeStore:       store.Close,
		baselinePrograms: baselinePrograms,
		baselineLinks:    baselineLinks,
	}, nil
}

// snapshotBaseline records the program and link IDs currently in the
// store so assertSuiteCleanState can subtract them at suite end. See
// the suiteRuntime baseline fields for the rationale. Returns an
// error if either listing fails; partial baselines would risk
// flagging inherited residue as a this-run leak.
func snapshotBaseline(ctx context.Context, mgr *manager.Manager) (map[kernel.ProgramID]struct{}, map[bpfman.LinkID]struct{}, error) {
	progs, err := mgr.ListPrograms(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list programs: %w", err)
	}

	progIDs := make(map[kernel.ProgramID]struct{}, len(progs))
	for _, p := range progs {
		progIDs[p.Record.ProgramID] = struct{}{}
	}

	links, err := mgr.ListLinks(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list links: %w", err)
	}

	linkIDs := make(map[bpfman.LinkID]struct{}, len(links))
	for _, l := range links {
		linkIDs[l.ID] = struct{}{}
	}

	return progIDs, linkIDs, nil
}

// teardownSharedRuntime asserts the suite finished clean, closes
// the store, unmounts the bpffs, and removes the suite root.
// Returns true if any leak was detected so TestMain can promote a
// passing exit code to a failure -- if tests passed individually
// but the suite as a whole left programs or links behind, that's a
// real bug and worth surfacing as a non-zero exit. Cleanup
// failures (close, unmount, rm) are logged but do not promote the
// exit code: they are operational, not behavioural, and shouldn't
// drown out the leak signal we actually care about.
func teardownSharedRuntime(rt *suiteRuntime) (leaked bool) {
	if rt == nil {
		return false
	}

	leaked = assertSuiteCleanState(rt)

	if rt.closeStore != nil {
		if err := rt.closeStore(); err != nil {
			fmt.Fprintf(os.Stderr, "e2e suite teardown: close store: %v\n", err)
		}
	}
	bpffsMount := rt.layout.BPFFSMountPoint()
	if err := unmount(bpffsMount); err != nil {
		fmt.Fprintf(os.Stderr, "e2e suite teardown: unmount %s: %v\n", bpffsMount, err)
	}

	if err := os.RemoveAll(rt.baseDir); err != nil {
		fmt.Fprintf(os.Stderr, "e2e suite teardown: remove %s: %v\n", rt.baseDir, err)
	}

	return leaked
}

// assertSuiteCleanState lists the manager's residual state at suite
// end and reports any leaked programs or links to stderr. Returns
// true if anything leaked. This is the final safety net of shared
// mode: every test's t.Cleanup is supposed to detach links and
// unload programs before the test returns, so post-suite the
// manager should be empty. Anything left over is a bug -- either a
// missing t.Cleanup, a Detach/Unload that didn't actually persist,
// or a manager-side leak.
//
// Records that were already in the store when the suite opened the
// DB are excluded: in shared mode the production manager hands
// crash recovery back to the operator, so a row inherited from a
// prior crashed run is not this run's leak. The suiteRuntime
// baseline maps capture that starting state, and the check below
// diffs the end-of-suite listing against them.
//
// For each leaked record the report also notes whether the kernel
// still has the corresponding object. "store ghost" means the row
// is in the manager's store but the kernel has already reclaimed
// the program/link, so the residue is purely a store-cleanup miss.
// "kernel residue too" means the kernel still has it, so teardown
// didn't reach the kernel detach/unload at all. The distinction
// narrows the search the next time this fires.
func assertSuiteCleanState(rt *suiteRuntime) bool {
	ctx := context.Background()
	leaked := false

	progResult, err := rt.manager.ListPrograms(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e suite teardown: list programs: %v\n", err)
	} else {
		var newProgs []bpfman.Program
		for _, p := range progResult {
			if _, inherited := rt.baselinePrograms[p.Record.ProgramID]; inherited {
				continue
			}
			newProgs = append(newProgs, p)
		}
		if len(newProgs) > 0 {
			leaked = true
			fmt.Fprintf(os.Stderr, "e2e suite teardown: %d program(s) leaked at suite end:\n", len(newProgs))
			for _, p := range newProgs {
				id := kernel.ProgramID(0)
				if p.Status.Kernel != nil {
					id = p.Status.Kernel.ID
				}
				fmt.Fprintf(os.Stderr, "  prog id=%d name=%q metadata=%v kernel=%s\n",
					id, p.Record.Meta.Name, p.Record.Meta.Metadata, kernelProgramPresence(ctx, rt, id))
			}
		}
	}

	links, err := rt.manager.ListLinks(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e suite teardown: list links: %v\n", err)
	} else {
		var newLinks []bpfman.LinkRecord
		for _, l := range links {
			if _, inherited := rt.baselineLinks[l.ID]; inherited {
				continue
			}
			newLinks = append(newLinks, l)
		}
		if len(newLinks) > 0 {
			leaked = true
			fmt.Fprintf(os.Stderr, "e2e suite teardown: %d link(s) leaked at suite end:\n", len(newLinks))
			for _, l := range newLinks {
				fmt.Fprintf(os.Stderr, "  link id=%d kind=%s prog=%d kernel=%s\n",
					l.ID, l.Kind, l.ProgramID, kernelLinkPresence(ctx, rt, l))
			}
		}
	}

	return leaked
}

// kernelProgramPresence reports whether the given program ID still
// exists in the kernel. "kernel residue too" means the kernel still
// has it, so teardown never reached the program unload. "store
// ghost" means the kernel has reclaimed it but the manager's store
// row stuck around. Any query error is treated as "kernel has
// reclaimed it" so we don't lose the leak report to a transient
// kernel lookup failure -- matches the existing inspect.GetProgram
// convention (inspect/inspect.go:672).
func kernelProgramPresence(ctx context.Context, rt *suiteRuntime, id kernel.ProgramID) string {
	if rt.kernel == nil || id == 0 {
		return "?"
	}
	if _, err := rt.kernel.GetProgramByID(ctx, id); err == nil {
		return "kernel residue too"
	}

	return "store ghost"
}

// kernelLinkPresence reports whether the given link is still in the
// kernel. Links without a captured kernel bpf_link ID cannot be
// correlated through bpftool-style kernel lookup. See
// kernelProgramPresence for the shape of the returned values.
func kernelLinkPresence(ctx context.Context, rt *suiteRuntime, l bpfman.LinkRecord) string {
	if rt.kernel == nil {
		return "?"
	}
	if l.KernelLinkID == nil {
		return "kernel link ID not captured"
	}
	if _, err := rt.kernel.GetLinkByID(ctx, *l.KernelLinkID); err == nil {
		return "kernel residue too"
	}

	return "store ghost"
}

// buildLogger composes the slog handler the same way NewTestEnv
// does, so per-test and shared modes log identically when
// BPFMAN_LOG is set.
func buildLogger() (*slog.Logger, error) {
	if envSpec := os.Getenv("BPFMAN_LOG"); envSpec != "" {
		l, err := logging.New(logging.Options{
			EnvSpec: envSpec,
			Format:  logging.FormatText,
			Output:  os.Stderr,
		})
		if err != nil {
			return nil, fmt.Errorf("invalid BPFMAN_LOG spec: %w", err)
		}

		return l, nil
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	})), nil
}

// requireSharedRuntimeForTest pulls the shared runtime out for use
// by NewTestEnv when sharedRuntimeMode() is true. Tests should
// never call this directly; it exists as the boundary helpers.go
// crosses to wire a TestEnv against the singleton.
func requireSharedRuntimeForTest(t *testing.T) *suiteRuntime {
	t.Helper()
	if sharedRuntime == nil {
		t.Fatalf("shared runtime requested but not initialised; TestMain must call initSharedRuntime in shared mode (the default; opt out with %s=1)", e2eIsolatedRuntimeEnv)
	}
	return sharedRuntime
}

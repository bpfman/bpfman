//go:build e2e

// Package grpcparallel exercises the bpfman gRPC daemon under
// concurrent client load. The test launches a real `bpfman serve`
// subprocess against a per-run tmp suite root, opens a shared gRPC
// client connection, and fans goroutines through one program-type
// lifecycle per Go sub-test. Each sub-test marks itself
// t.Parallel() so the daemon receives loads/attaches/detaches/
// unloads of different program types concurrently, on top of the
// within-sub-test goroutine fan-out.
//
// The .bpfman scripts under e2e/scripts/ remain the canonical
// correctness suite (exhaustive `matches exhaustive` blocks per
// program type); this test is deliberately thin and exists to
// stress the daemon-side surface: read RPCs running lockless
// alongside writer-flock-serialised mutators inside a single daemon
// process.
//
// Run from the repository root:
//
//	sudo make test-e2e-grpc
//
// Knobs:
//
//	BPFMAN_GRPC_GOROUTINES   goroutines per sub-test (default 32)
//	BPFMAN_GRPC_ITERATIONS   iterations per goroutine (default 4)
//	BPFMAN_GRPC_PROGRESS_INTERVAL progress log interval; accepts
//	                               durations such as 5s or bare seconds
//	                               (default 1s)
//	BPFMAN_BIN               path to bpfman binary (default: looked up on $PATH)
//
// The spawned daemon's stdout and stderr are redirected to
// /tmp/bpfman-grpc-daemon.log (truncated each run) so the test
// process's own stderr only carries the Go test framework's
// output. The path is printed at startup.
package grpcparallel

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/bpfman/bpfman/e2e"
	pb "github.com/bpfman/bpfman/server/pb"
)

// daemonLogPath is where the spawned bpfman daemon's stdout
// and stderr are written. Fixed location outside the per-run
// tmp root so RemoveAll(tmpRoot) on cleanup does not take the
// log with it; the file is overwritten on each test invocation
// rather than appended, so the contents always reflect the
// most recent run.
const daemonLogPath = "/tmp/bpfman-grpc-daemon.log"

var (
	// client is the shared gRPC client. pb.BpfmanClient is
	// goroutine-safe; one connection serves every sub-test's
	// goroutine pool, matching the production topology of a single
	// caller fanning RPCs into the daemon.
	client pb.BpfmanClient

	// testdataDir is the absolute path of the on-disk
	// testdata/bpf directory resolved during bootstrap.
	// Per-type specs join their .bpf.o filename to this path to
	// produce the absolute path the daemon's Load RPC opens.
	testdataDir string
)

func TestMain(m *testing.M) {
	cleanup, err := bootstrap()

	// Cleanup may need to run from two paths: the normal end of
	// TestMain, or a signal handler that fires when a caller
	// SIGINTs the test before m.Run returns. sync.Once makes the
	// second call a no-op.
	var once sync.Once
	runCleanup := func() {
		if cleanup != nil {
			once.Do(cleanup)
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "caught %s, shutting daemon down\n", sig)
		runCleanup()
		exitCode := 130 // 128 + SIGINT
		if sig == syscall.SIGTERM {
			exitCode = 143
		}
		os.Exit(exitCode)
	}()

	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: %v\n", err)
		runCleanup()
		os.Exit(1)
	}

	code := m.Run()
	runCleanup()
	os.Exit(code)
}

func bootstrap() (func(), error) {
	binary, err := resolveBpfmanBinary()
	if err != nil {
		return nil, fmt.Errorf("resolve binary: %w", err)
	}

	tmpRoot, err := os.MkdirTemp("", "bpfman-grpc-parallel-")
	if err != nil {
		return nil, fmt.Errorf("tmp root: %w", err)
	}

	cleanupRoot := func() { _ = os.RemoveAll(tmpRoot) }

	// The daemon opens .bpf.o files straight off the on-disk
	// testdata tree; e2e.BytecodeDir resolves where that tree
	// lives (BPFMAN_E2E_BYTECODE_DIR in CI, cwd locally).
	// BytecodePath returns an absolute path so the resolution is
	// independent of the daemon subprocess's working directory.
	testdataDir = e2e.BytecodePath("testdata/bpf")

	runtimeDir := filepath.Join(tmpRoot, "runtime")
	cacheDir := filepath.Join(tmpRoot, "cache")
	socketPath := filepath.Join(tmpRoot, "bpfman.sock")
	for _, d := range []string{runtimeDir, cacheDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			cleanupRoot()
			return nil, fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// Route the daemon's stdout/stderr to a fixed file outside
	// tmpRoot so it survives RemoveAll(tmpRoot) and can be
	// inspected after the test exits. Keeping it out of the
	// test binary's own stderr is what stops the daemon log
	// from interleaving with the test framework's PASS/FAIL
	// lines and from competing with another concurrent run's
	// tee for ownership of /tmp/bpfman-test-e2e-grpc.log.
	logFile, err := os.OpenFile(daemonLogPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		cleanupRoot()
		return nil, fmt.Errorf("open daemon log %s: %w", daemonLogPath, err)
	}

	fmt.Fprintf(os.Stderr, "daemon log: %s\n", daemonLogPath)

	ctx, cancel := context.WithCancel(context.Background())
	serverCmd := exec.CommandContext(ctx, binary, "serve",
		"--runtime-dir", runtimeDir,
		"--image-cache-dir", cacheDir,
		"--socket-path", socketPath,
	)
	serverCmd.Stdout = logFile
	serverCmd.Stderr = logFile
	serverCmd.SysProcAttr = &syscall.SysProcAttr{
		// Own process group so a stray ctx-cancel SIGKILL
		// does not also reach this test binary.
		Setpgid: true,
		// If the test binary dies for any reason -- Ctrl+C
		// before deferred cleanup runs, panic, SIGKILL from
		// the test runner -- the kernel sends the daemon
		// SIGKILL. Last line of defence against leaving a
		// daemon listening on the UDS path between runs.
		Pdeathsig: syscall.SIGKILL,
	}

	if err := serverCmd.Start(); err != nil {
		cancel()
		_ = logFile.Close()
		cleanupRoot()
		return nil, fmt.Errorf("start daemon: %w", err)
	}

	cleanupDaemon := func() {
		_ = syscall.Kill(serverCmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = serverCmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cancel()
			<-done
		}
		cancel()
		_ = logFile.Close()
		cleanupRoot()
	}

	if err := waitForSocket(socketPath, 10*time.Second); err != nil {
		cleanupDaemon()
		return nil, fmt.Errorf("wait for socket: %w", err)
	}

	conn, err := grpc.NewClient("unix:"+socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		cleanupDaemon()
		return nil, fmt.Errorf("dial: %w", err)
	}

	client = pb.NewBpfmanClient(conn)

	return func() {
		_ = conn.Close()
		cleanupDaemon()
	}, nil
}

// resolveBpfmanBinary returns the path to the bpfman binary.
// BPFMAN_BIN overrides everything; otherwise the binary must be
// on $PATH. The test does not try to locate the binary by
// walking the source tree: the build system (Makefile recipe,
// CI workflow) is the right place to decide where the binary
// lives, not the test.
func resolveBpfmanBinary() (string, error) {
	if env := os.Getenv("BPFMAN_BIN"); env != "" {
		return env, nil
	}
	p, err := exec.LookPath("bpfman")
	if err != nil {
		return "", fmt.Errorf("bpfman not found on PATH (override via BPFMAN_BIN): %w", err)
	}
	return p, nil
}

// testdataPath joins testdataDir with name. Used by per-type
// specs to resolve their .bpf.o filename to an absolute path the
// daemon can open.
func testdataPath(name string) string {
	return filepath.Join(testdataDir, name)
}

// waitForSocket polls until the daemon socket accepts a connect
// or the timeout expires.
func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("unix", path, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("daemon socket %s never became ready: %w", path, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func envInt(name string, def int) int {
	s := os.Getenv(name)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

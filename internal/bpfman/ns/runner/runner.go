package runner

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"syscall"

	"github.com/alecthomas/kong"

	"github.com/bpfman/bpfman/internal/bpfman/ns"
	"github.com/bpfman/bpfman/lock"
)

// NSCmd handles the bpfman-ns subcommand for attaching uprobes in other
// namespaces.
//
// The namespace switch happens via a CGO constructor (in the ns package)
// that runs before Go's runtime starts. The parent process sets _BPFMAN_MNT_NS
// environment variable, and the C code calls setns(CLONE_NEWNS) while still
// single-threaded.
type NSCmd struct {
	// Uprobe is the "uprobe" subcommand: it opens a target binary in
	// the entered container mount namespace and returns its fd to the
	// parent.
	Uprobe NSUprobeCmd `cmd:"" help:"Attach a uprobe program in the given container."`
}

// NSUprobeCmd opens the target binary in the container's mount namespace
// and returns its fd to the parent. When this code runs, the process is
// already in the target namespace (switched by the CGO constructor before
// Go started), so the binary path resolves against the container's
// filesystem.
//
// The parent passes a Unix socket via fd 3 (ExtraFiles[0]); the child sends
// the opened target-binary fd back over it. The parent, in bpfman's own
// namespace, reaches that inode through /proc/self/fd and performs the BPF
// link create and pin there, where the kernel reliably exposes a pinnable
// perf-event bpf_link. The function name, offset, and retprobe flag are not
// needed here -- the parent owns the attach -- so the child takes only the
// target path.
type NSUprobeCmd struct {
	// Target is the path of the binary to open. It resolves against the
	// container's filesystem because the C constructor already switched
	// the process into the target mount namespace before parsing.
	Target string `arg:"" help:"Target binary path (resolved in container namespace)."`
}

// getMntNsInode returns the inode of a mount namespace file.
func getMntNsInode(path string) uint64 {
	stat, err := os.Stat(path)
	if err != nil {
		return 0
	}

	sys, ok := stat.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return sys.Ino
}

// Run opens the target binary in the (already-entered) container mount
// namespace and sends its fd to the parent. The CGO constructor performed
// the setns before Go started, so cmd.Target resolves against the
// container's filesystem here.
//
// A Unix socket is inherited via fd 3, and the writer lock fd via the
// BPFMAN_WRITER_LOCK_FD environment variable. The child does no BPF work:
// it verifies the inherited writer lock, opens the target, and returns the
// fd; the parent performs the attach and pin in bpfman's own namespace.
func (cmd *NSUprobeCmd) Run() error {
	// Create a logger that writes to stderr, respecting BPFMAN_LOG if set.
	// Defaults to info level for less verbose output in normal operation.
	logLevel := slog.LevelInfo
	if spec := os.Getenv("BPFMAN_LOG"); spec != "" {
		// Simple level extraction: take first word if it looks like a level
		// (full spec parsing is overkill for the helper subprocess)
		switch spec {
		case "trace":
			logLevel = slog.Level(-8) // LevelTrace
		case "debug":
			logLevel = slog.LevelDebug
		case "warn", "warning":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		}
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))

	// Verify writer lock - helper cannot proceed without it.
	// The lock fd is passed via environment variable since its position
	// in ExtraFiles may vary.
	fdStr := os.Getenv(lock.WriterLockFDEnvVar)
	if fdStr == "" {
		logger.Error("writer lock fd not set", "env_var", lock.WriterLockFDEnvVar, "hint", "bpfman-ns must be spawned with lock fd")
		return fmt.Errorf("%s not set: bpfman-ns must be spawned with lock fd", lock.WriterLockFDEnvVar)
	}
	fd, err := strconv.Atoi(fdStr)
	if err != nil {
		logger.Error("invalid writer lock fd", "env_var", lock.WriterLockFDEnvVar, "value", fdStr, "error", err)
		return fmt.Errorf("invalid %s=%q: %w", lock.WriterLockFDEnvVar, fdStr, err)
	}

	scope, err := lock.InheritedLockFromFD(fd)
	if err != nil {
		logger.Error("lock verification failed", "fd", fd, "error", err, "hint", "parent must hold exclusive lock before spawning helper")
		return fmt.Errorf("lock verification failed: %w", err)
	}

	defer scope.Close()

	logger.Debug("writer lock verified", "fd", fd)

	// Log our current state
	currentMntNs := getMntNsInode("/proc/self/ns/mnt")

	logger.Info("bpfman-ns uprobe handler started", "pid", os.Getpid(), "ppid", os.Getppid(), "current_mnt_ns_inode", currentMntNs, "target", cmd.Target, "socket_fd", ns.SocketFD)

	// The socket returns the opened target-binary fd to the parent.
	socket := os.NewFile(uintptr(ns.SocketFD), "fdpass-socket")
	if socket == nil {
		logger.Error("failed to get socket fd", "fd", ns.SocketFD, "hint", "bpfman-ns must be invoked by the daemon, not directly")
		return fmt.Errorf("socket fd %d not available (bpfman-ns must be invoked by daemon)", ns.SocketFD)
	}
	defer socket.Close()

	// Open the target binary in the container's mount namespace. Only this
	// path resolution needs the target namespace; the parent performs the
	// uprobe attach and pin in bpfman's own namespace, reaching this same
	// inode through /proc/self/fd of the fd sent below.
	target, err := os.Open(cmd.Target)
	if err != nil {
		logger.Error("failed to open target binary in container namespace", "target", cmd.Target, "error", err, "current_mnt_ns_inode", currentMntNs, "hint", "ensure the target path exists in the container's filesystem")
		return fmt.Errorf("open target binary %q in container (mnt ns inode %d): %w", cmd.Target, currentMntNs, err)
	}

	defer target.Close()

	logger.Debug("opened target binary, sending fd to parent", "target", cmd.Target, "target_fd", int(target.Fd()), "socket_fd", ns.SocketFD)

	if err := ns.SendFd(socket, ns.TargetFDName, int(target.Fd())); err != nil {
		logger.Error("failed to send target fd to parent", "target", cmd.Target, "error", err)
		return fmt.Errorf("send target fd to parent: %w", err)
	}

	logger.Info("target fd sent to parent successfully", "target", cmd.Target)

	// Success is signalled by clean exit (exit 0); the parent holds its own
	// fd reference via SCM_RIGHTS.
	return nil
}

// NamespaceHelperInvocation captures the details of a namespace helper
// invocation. This is used when a binary re-execs itself to attach uprobes
// inside container mount namespaces.
type NamespaceHelperInvocation struct {
	// Args holds the helper command line with argv[0] removed, ready to
	// hand to the bpfman-ns argument parser.
	Args []string
}

// DetectNamespaceHelperInvocation checks if this invocation is for the
// namespace helper subprocess (used for container uprobe attachment).
//
// Detection logic:
//   - "bpfman-ns" -> helper mode
//   - "bpfman-rpc" -> not helper (valid, but different mode)
//   - anything else -> error (unknown mode)
//
// Returns the invocation details (with rewritten args for the helper parser),
// whether helper mode was detected, and any error for invalid configuration.
func DetectNamespaceHelperInvocation(argv []string, modeEnv string) (NamespaceHelperInvocation, bool, error) {
	if len(argv) == 0 || modeEnv == "" {
		return NamespaceHelperInvocation{}, false, nil
	}

	switch modeEnv {
	case ns.ModeBPFManNS:
		return NamespaceHelperInvocation{Args: argv[1:]}, true, nil
	case ns.ModeBPFManRPC:
		return NamespaceHelperInvocation{}, false, nil
	default:
		return NamespaceHelperInvocation{}, false, fmt.Errorf("unknown BPFMAN_MODE=%q; valid values: bpfman-ns, bpfman-rpc", modeEnv)
	}
}

// HandleNamespaceHelperInvocation detects namespace helper mode and runs the
// provided runner if detected. Returns whether the invocation was handled and
// any error from detection or the runner.
func HandleNamespaceHelperInvocation(argv []string, modeEnv string, run func(NamespaceHelperInvocation) error) (handled bool, err error) {
	inv, isHelper, err := DetectNamespaceHelperInvocation(argv, modeEnv)
	if err != nil {
		return false, err
	}

	if !isHelper {
		return false, nil
	}
	return true, run(inv)
}

// runNamespaceHelper is the default runner for namespace helper invocations.
// It parses the helper arguments and executes the command without mutating os.Args.
func runNamespaceHelper(inv NamespaceHelperInvocation) error {
	var cmd NSCmd

	parser, err := kong.New(&cmd, kong.Name(ns.ModeBPFManNS), kong.Description("Attach an eBPF program inside a container's mount namespace."), kong.UsageOnError())
	if err != nil {
		return fmt.Errorf("create parser: %w", err)
	}

	ctx, err := parser.Parse(inv.Args)
	if err != nil {
		return err
	}
	return ctx.Run()
}

// Run checks whether this process was re-execed as the bpfman-ns helper
// (container uprobe attachment) and, if so, parses and runs the helper
// command. The returned ran is true when the helper path was taken; the
// caller should then exit without proceeding to its normal CLI. err is set
// for an invalid invocation (ran false) or a helper failure (ran true).
func Run() (ran bool, err error) {
	return HandleNamespaceHelperInvocation(os.Args, os.Getenv(ns.ModeEnvVar), runNamespaceHelper)
}

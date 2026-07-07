package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bpfman/bpfman/config"
	driver "github.com/bpfman/bpfman/csi"
	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/internal/bpfman/runtimestate"
	"github.com/bpfman/bpfman/lock"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/ns/netns"
	"github.com/bpfman/bpfman/platform/image/oci"
	"github.com/bpfman/bpfman/platform/image/verify"
	pb "github.com/bpfman/bpfman/server/pb"
)

const (
	// DefaultCSIDriverName is the default CSI driver name.
	// Uses csi.bpfman.io for compatibility with bpfman-operator.
	DefaultCSIDriverName = "csi.bpfman.io"
	// DefaultCSIVersion is the default CSI driver version.
	DefaultCSIVersion = "0.1.0"
	// DefaultWriterLockTimeout is the daemon's default wait budget for
	// acquiring the global writer lock.
	DefaultWriterLockTimeout = 30 * time.Second
)

// RunConfig configures the server daemon.
type RunConfig struct {
	// Layout is the bpfman filesystem layout the daemon operates on.
	Layout fs.Layout

	// ImageCache is a capability token proving the OCI image cache
	// directory exists; the image puller is built from it.
	ImageCache fs.EnsuredImageCache

	// CSISupport starts the Kubernetes CSI driver alongside the gRPC
	// server when true.
	CSISupport bool

	// SocketPath overrides the Unix socket path; empty defaults to
	// Layout.SocketPath().
	SocketPath string

	// Logger is the structured logger used by the daemon and its
	// subsystems.
	Logger *slog.Logger

	// Config is the parsed bpfman configuration, supplying signature
	// verification settings.
	Config config.Config
}

// Run starts the bpfman daemon with the given configuration.
// This is the main entry point for the serve command.
// The context is used for cancellation - when cancelled, the server shuts down gracefully.
func Run(ctx context.Context, cfg RunConfig) error {
	layout := cfg.Layout

	// Capture the process's startup netns inode now, before any
	// goroutine that might call setns has been spawned. The
	// ns/netns package caches this under a sync.Once and every
	// later call to CurrentNSID / NSID("") returns the captured
	// value. Priming on the calling goroutine -- which is
	// whatever drove main() to here, still on a thread that has
	// not switched namespaces -- locks in a known-good value.
	// Subsequent manager attaches that resolve an empty netns
	// path to "root" via the NSID("") -> processNSID fallback
	// then see the right inode regardless of which thread the
	// request landed on. Failure here means /proc/self/ns/net is
	// unreadable, which makes the rest of the daemon meaningless
	// -- panic so a stack trace makes the startup failure
	// obvious rather than a generic error from every later
	// attach. Symmetric with the equivalent prime in the e2e
	// TestMain.
	if _, err := netns.CurrentNSID(); err != nil {
		panic(fmt.Errorf("server: prime ns/netns capture: %v", err))
	}

	logger := cfg.Logger
	// Wrap with context-aware handler to extract op_id from context.
	// This must happen at the server level since op_id is generated here.
	logger = manager.WithOpIDHandler(logger)

	opened, err := runtimestate.OpenMutable(ctx, layout, logger, DefaultWriterLockTimeout)
	if err != nil {
		return fmt.Errorf("open runtime: %w", err)
	}

	defer opened.Close()

	verifier, err := verify.FromSigningConfig(cfg.Config.Signing, logger)
	if err != nil {
		return fmt.Errorf("configure signature verifier: %w", err)
	}

	// Create image puller for OCI images
	puller, err := oci.NewPuller(
		cfg.ImageCache,
		oci.WithLogger(logger),
		oci.WithVerifier(verifier),
	)
	if err != nil {
		return fmt.Errorf("failed to create image puller: %w", err)
	}

	// Create manager for orchestrating store + kernel operations.
	// The manager is needed by CSI for reconciled program lookups.
	mgr, err := manager.New(opened.FS, puller, opened.Store, opened.Kernel, opened.Validator, logger)
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	// Start CSI driver if enabled, and arrange for it to stop on
	// root context cancellation. When CSI is disabled we spawn no
	// shutdown goroutine; the gRPC server's own ctx.Done handler
	// drives gRPC shutdown directly.
	if cfg.CSISupport {
		for _, dir := range layout.CSIDirs() {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("create CSI directory %s: %w", dir, err)
			}
		}

		nodeID, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to get hostname for node ID: %w", err)
		}

		csiSocketPath := layout.CSISocketPath()
		csiDriver := driver.New(
			DefaultCSIDriverName,
			DefaultCSIVersion,
			nodeID,
			"unix://"+csiSocketPath,
			logger,
			driver.WithProgramFinder(mgr),
			driver.WithKernel(opened.Kernel),
		)

		go func() {
			logger.Info("starting CSI driver", "socket", csiSocketPath, "driver", DefaultCSIDriverName)
			if err := csiDriver.Run(); err != nil {
				logger.Error("CSI driver failed", "error", err)
			}
		}()

		go func() {
			<-ctx.Done()
			logger.Info("stopping CSI driver")
			csiDriver.Stop()
		}()
	}

	// Start bpfman gRPC server
	srv := &Server{
		layout: layout,
		mgr:    mgr,
		logger: logger.With("component", "server"),
	}

	// Use override socket path if provided, otherwise use default from layout
	socketPath := cfg.SocketPath
	if socketPath == "" {
		socketPath = layout.SocketPath()
	}

	return srv.serve(ctx, socketPath)
}

// Server implements the bpfman gRPC service.
//
// The server runs on the cross-process writer flock alone, with no
// in-process serialisation. Mutating handlers (Unload, Attach,
// Detach) wrap their body in withWriterLock to acquire the
// file-based writer lock from the lock package; read handlers
// (List, Get, PullBytecode) run lockless and rely
// on the store and kernel adapter for safe concurrent access. The
// Load handler also takes no server-level lock; the manager handles
// its own conditional flock acquisition for explicit map-owner joins
// and LIBBPF_PIN_BY_NAME loads. Cross-process serialisation for
// shared-map loads is the manager's responsibility (see Manager.Load).
type Server struct {
	pb.UnimplementedBpfmanServer

	layout    fs.Layout
	mgr       *manager.Manager
	logger    *slog.Logger
	opCounter atomic.Uint64
}

// serve starts the gRPC server on the given Unix socket path.
func (s *Server) serve(ctx context.Context, socketPath string) error {
	// Ensure socket directory exists
	socketDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Remove existing socket file
	if err := os.RemoveAll(socketPath); err != nil {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	// Create Unix socket listener
	unixListener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", socketPath, err)
	}

	defer unixListener.Close()

	// Set socket permissions
	if err := os.Chmod(socketPath, 0660); err != nil {
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(s.rpcInterceptor()),
	)
	pb.RegisterBpfmanServer(grpcServer, s)

	// Track errors from the serving goroutine
	errChan := make(chan error, 1)

	// Start Unix socket server
	go func() {
		s.logger.InfoContext(ctx, "bpfman gRPC server listening", "socket", socketPath)
		if err := grpcServer.Serve(unixListener); err != nil {
			errChan <- fmt.Errorf("unix socket server: %w", err)
		}
	}()

	// Handle context cancellation for graceful shutdown
	go func() {
		<-ctx.Done()
		s.logger.InfoContext(ctx, "shutting down gRPC server")
		grpcServer.GracefulStop()
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		return nil
	case err := <-errChan:
		return err
	}
}

// rpcInterceptor returns a gRPC unary interceptor that assigns an
// operation ID to each request and logs handler errors centrally.
// Per-request locking is the handler's own responsibility -- see
// withWriterLock.
//
// Error logging is split by gRPC status code so operators only see
// genuine server faults at ERROR. Client-induced statuses (NotFound,
// InvalidArgument, etc.) and transport statuses (Canceled when a
// caller hangs up) drop to DEBUG so they remain visible when needed
// but do not pollute the default INFO-level stream.
func (s *Server) rpcInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (any, error) {
		opID := s.opCounter.Add(1)
		ctx = manager.ContextWithOpID(ctx, opID)

		resp, err := handler(ctx, req)
		if err != nil {
			code := status.Code(err)
			switch code {
			case codes.Internal, codes.Unknown, codes.DataLoss:
				s.logger.ErrorContext(ctx, "rpc handler failed", "method", info.FullMethod, "code", code, "error", err)
			default:
				s.logger.DebugContext(ctx, "rpc returned non-ok", "method", info.FullMethod, "code", code, "error", err)
			}
		}
		return resp, err
	}
}

// withWriterLock runs fn under the cross-process writer flock.
// The acquired writer scope is passed to fn so handlers that need
// to forward it (container uprobes passing the lock fd to the
// bpfman-ns helper) can do so directly, without a context
// round-trip.
func withWriterLock[T any](ctx context.Context, s *Server, fn func(context.Context, lock.WriterScope) (T, error)) (T, error) {
	var resp T
	// The wait is bounded by the caller's RPC deadline, not a server-imposed
	// budget: a client that sets a deadline gets it (surfaced as the parent
	// DeadlineExceeded/Canceled below), while a deadline-less client waits for
	// the lock as long as it takes, matching the daemon's prior behaviour and
	// avoiding starvation timeouts under heavy mutation contention.
	err := lock.RunWithTiming(ctx, s.layout.LockPath(), s.logger, func(ctx context.Context, writeLock lock.WriterScope) error {
		var fnErr error
		resp, fnErr = fn(ctx, writeLock)
		return fnErr
	})
	if err != nil {
		var zero T
		if errors.Is(err, context.DeadlineExceeded) {
			return zero, status.Error(codes.DeadlineExceeded, err.Error())
		}
		if errors.Is(err, context.Canceled) {
			return zero, status.Error(codes.Canceled, err.Error())
		}
		return zero, err
	}
	return resp, nil
}

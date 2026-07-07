package manager

import (
	"context"
	"log/slog"

	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/manager/action"
	"github.com/bpfman/bpfman/platform"
)

// opIDKey is the context key for operation IDs.
type opIDKey struct{}

// ContextWithOpID returns a new context with the given operation ID.
func ContextWithOpID(ctx context.Context, opID uint64) context.Context {
	return context.WithValue(ctx, opIDKey{}, opID)
}

// OpIDFromContext extracts the operation ID from context, or returns 0 if not set.
func OpIDFromContext(ctx context.Context) uint64 {
	if v := ctx.Value(opIDKey{}); v != nil {
		return v.(uint64)
	}
	return 0
}

// Manager orchestrates BPF program management using fetch/compute/execute.
type Manager struct {
	rt               fs.Runtime
	store            platform.Store
	kernel           platform.KernelOperations
	executor         action.Executor
	programValidator platform.ProgramValidator
	imagePuller      platform.ImagePuller // optional, nil if not configured
	logger           *slog.Logger
}

// New creates a new Manager with all required dependencies.
//
// Required parameters:
//   - rt: runtime capability token (from runtime.New()) proving directories and bpffs are ready
//   - store: database for program/link metadata
//   - kernel: kernel operations adapter
//   - programValidator: validates requested program names against object files
//   - logger: structured logger (nil uses slog.Default())
//
// Optional parameters:
//   - imagePuller: OCI image puller for loading programs from container images (nil to disable)
//
// The rt parameter is a capability token from fs/runtime.New()
// that proves the filesystem directories exist and bpffs is mounted.
//
// The logger should already be wrapped with WithOpIDHandler by the caller
// (typically the server) to enable op_id extraction from context.
func New(
	rt fs.Runtime,
	imagePuller platform.ImagePuller,
	store platform.Store,
	kernel platform.KernelOperations,
	programValidator platform.ProgramValidator,
	logger *slog.Logger,
) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}

	return &Manager{
		rt:               rt,
		store:            store,
		kernel:           kernel,
		programValidator: programValidator,
		imagePuller:      imagePuller,
		executor:         newExecutor(store, kernel, rt.Bytecode(), rt.BPFFS(), logger),
		logger:           logger.With("component", "manager"),
	}, nil
}

// Layout returns the filesystem layout.
func (m *Manager) Layout() fs.Layout {
	return m.rt.Layout()
}

// Runtime returns the filesystem runtime capability token.
func (m *Manager) Runtime() fs.Runtime {
	return m.rt
}

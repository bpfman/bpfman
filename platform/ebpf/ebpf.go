package ebpf

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"net"
	"sync"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"

	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/ns/netns"
	"github.com/bpfman/bpfman/platform"
)

// kernelAdapter implements platform.KernelOperations using cilium/ebpf.
type kernelAdapter struct {
	logger *slog.Logger

	// liveLinks holds the *link.Link returned by cilium/ebpf at
	// attach time, keyed by bpffs link pin path. For probe-style
	// attachments (tracepoint, k(ret)probe, u(ret)probe,
	// fentry/fexit) where multiple BPF programs share a kernel
	// hook, pin-removal alone does not run perf_event_free_bpf_prog
	// for the released link's program. DetachLink removes the pin
	// and then consumes this map -- the order matters.
	liveLinks sync.Map

	// testDisp holds lazily-loaded test dispatchers used as
	// verification targets when loading XDP/TC programs as
	// Extension type.
	testDisp testDispatchers
}

// trackLink remembers a live link so DetachLink can close it. A
// no-op when linkPinPath is empty (caller owns lnk and must Close).
func (k *kernelAdapter) trackLink(linkPinPath string, lnk link.Link) {
	if linkPinPath == "" {
		return
	}
	k.liveLinks.Store(linkPinPath, lnk)
}

// releaseLink Closes and forgets the live link tracked at
// linkPinPath, if any. Returns the Close error.
func (k *kernelAdapter) releaseLink(linkPinPath string) error {
	v, ok := k.liveLinks.LoadAndDelete(linkPinPath)
	if !ok {
		return nil
	}
	return v.(link.Link).Close()
}

// Option configures a kernelAdapter.
type Option func(*kernelAdapter)

// WithLogger sets the logger for kernel operations.
func WithLogger(logger *slog.Logger) Option {
	return func(k *kernelAdapter) {
		k.logger = logger
	}
}

// New creates a new kernel adapter.
func New(opts ...Option) platform.KernelOperations {
	k := &kernelAdapter{
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(k)
	}
	return k
}

// InterfaceByName resolves an interface name to its kernel ifindex,
// entering netnsPath first when non-empty so a namespaced interface
// (such as a pod's "eth0") is looked up in the namespace that owns it
// rather than in the daemon's own namespace. An empty path resolves
// in the current namespace.
func (k *kernelAdapter) InterfaceByName(_ context.Context, name, netnsPath string) (int, error) {
	var ifindex int
	err := netns.Run(netnsPath, func() error {
		iface, err := net.InterfaceByName(name)
		if err != nil {
			return err
		}

		ifindex = iface.Index
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("resolve interface %q in netns %q: %w: %w", name, netnsPath, err, platform.ErrInterfaceNotFound)
	}
	return ifindex, nil
}

// GetProgramByID retrieves a kernel program by its ID.
func (k *kernelAdapter) GetProgramByID(ctx context.Context, id kernel.ProgramID) (kernel.Program, error) {
	prog, err := ebpf.NewProgramFromID(ebpf.ProgramID(id))
	if err != nil {
		return kernel.Program{}, fmt.Errorf("program %d: %w", id, err)
	}
	defer prog.Close()

	info, err := prog.Info()
	if err != nil {
		return kernel.Program{}, fmt.Errorf("get info for program %d: %w", id, err)
	}

	kp := ToKernelProgram(info)
	if kp == nil {
		return kernel.Program{}, fmt.Errorf("program %d: nil ProgramInfo", id)
	}
	return *kp, nil
}

// GetProgramStatsByID retrieves runtime statistics for a BPF program.
// Returns nil if stats are not available (e.g., kernel.bpf_stats_enabled=0).
func (k *kernelAdapter) GetProgramStatsByID(ctx context.Context, id kernel.ProgramID) (*kernel.ProgramStats, error) {
	prog, err := ebpf.NewProgramFromID(ebpf.ProgramID(id))
	if err != nil {
		return nil, fmt.Errorf("program %d: %w", id, err)
	}
	defer prog.Close()

	stats, err := prog.Stats()
	if err != nil {
		// Stats unavailable (not enabled or not supported), not an error
		return nil, nil
	}

	return &kernel.ProgramStats{
		Runtime:         stats.Runtime,
		RunCount:        stats.RunCount,
		RecursionMisses: stats.RecursionMisses,
	}, nil
}

// GetLinkByID retrieves a kernel link by its ID.
func (k *kernelAdapter) GetLinkByID(ctx context.Context, id kernel.LinkID) (kernel.Link, error) {
	lnk, err := link.NewFromID(link.ID(id))
	if err != nil {
		return kernel.Link{}, fmt.Errorf("link %d: %w", id, err)
	}
	defer lnk.Close()

	info, err := lnk.Info()
	if err != nil {
		return kernel.Link{}, fmt.Errorf("get info for link %d: %w", id, err)
	}

	return infoToLink(info), nil
}

// GetMapByID retrieves a kernel map by its ID.
func (k *kernelAdapter) GetMapByID(ctx context.Context, id kernel.MapID) (kernel.Map, error) {
	m, err := ebpf.NewMapFromID(ebpf.MapID(id))
	if err != nil {
		return kernel.Map{}, fmt.Errorf("map %d: %w", id, err)
	}
	defer m.Close()

	info, err := m.Info()
	if err != nil {
		return kernel.Map{}, fmt.Errorf("get info for map %d: %w", id, err)
	}

	return infoToMap(info, id), nil
}

// Programs returns an iterator over kernel BPF programs.
func (k *kernelAdapter) Programs(ctx context.Context) iter.Seq2[kernel.Program, error] {
	return func(yield func(kernel.Program, error) bool) {
		var id ebpf.ProgramID
		for {
			nextID, err := ebpf.ProgramGetNextID(id)
			if err != nil {
				return // No more programs
			}

			id = nextID

			prog, err := ebpf.NewProgramFromID(id)
			if err != nil {
				if !yield(kernel.Program{}, err) {
					return
				}
				continue
			}

			info, err := prog.Info()
			prog.Close()
			if err != nil {
				if !yield(kernel.Program{}, err) {
					return
				}
				continue
			}

			kp := ToKernelProgram(info)
			if kp == nil {
				if !yield(kernel.Program{}, fmt.Errorf("program %d: nil ProgramInfo", id)) {
					return
				}
				continue
			}
			if !yield(*kp, nil) {
				return
			}
		}
	}
}

// Maps returns an iterator over kernel BPF maps.
func (k *kernelAdapter) Maps(ctx context.Context) iter.Seq2[kernel.Map, error] {
	return func(yield func(kernel.Map, error) bool) {
		var id ebpf.MapID
		for {
			nextID, err := ebpf.MapGetNextID(id)
			if err != nil {
				return
			}

			id = nextID

			m, err := ebpf.NewMapFromID(id)
			if err != nil {
				if !yield(kernel.Map{}, err) {
					return
				}
				continue
			}

			info, err := m.Info()
			m.Close()
			if err != nil {
				if !yield(kernel.Map{}, err) {
					return
				}
				continue
			}

			km := infoToMap(info, kernel.MapID(id))
			if !yield(km, nil) {
				return
			}
		}
	}
}

// Links returns an iterator over kernel BPF links.
func (k *kernelAdapter) Links(ctx context.Context) iter.Seq2[kernel.Link, error] {
	return func(yield func(kernel.Link, error) bool) {
		it := new(link.Iterator)
		defer it.Close()

		for it.Next() {
			info, err := it.Link.Info()
			if err != nil {
				if !yield(kernel.Link{}, err) {
					return
				}
				continue
			}

			kl := infoToLink(info)
			if !yield(kl, nil) {
				return
			}
		}

		if err := it.Err(); err != nil {
			yield(kernel.Link{}, err)
		}
	}
}

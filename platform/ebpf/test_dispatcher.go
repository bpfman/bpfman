package ebpf

import (
	"fmt"
	"sync"

	"github.com/cilium/ebpf"

	"github.com/bpfman/bpfman/dispatcher"
)

// testDispatchers holds lazily-loaded test dispatchers used as
// verification targets when loading XDP/TC programs as Extension
// type. These are minimal dispatchers loaded from the standard
// bytecode with one slot enabled; they exist purely so the verifier
// can check the extension's signature at load time. They remain alive
// for the lifetime of the kernel adapter.
type testDispatchers struct {
	mu   sync.Mutex
	xdp  *ebpf.Program
	tc   *ebpf.Program
	xdpC *ebpf.Collection
	tcC  *ebpf.Collection
}

// getXDP returns the test XDP dispatcher program, loading it lazily on
// first call.
func (td *testDispatchers) getXDP() (*ebpf.Program, error) {
	td.mu.Lock()
	defer td.mu.Unlock()

	if td.xdp != nil {
		return td.xdp, nil
	}

	cfg, err := dispatcher.NewXDPConfig(1)
	if err != nil {
		return nil, fmt.Errorf("create test XDP dispatcher config: %w", err)
	}

	spec, err := dispatcher.LoadXDPDispatcher(cfg)
	if err != nil {
		return nil, fmt.Errorf("load test XDP dispatcher spec: %w", err)
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, fmt.Errorf("create test XDP dispatcher collection: %w", err)
	}

	prog := coll.Programs["xdp_dispatcher"]
	if prog == nil {
		coll.Close()
		return nil, fmt.Errorf("xdp_dispatcher program not found in test collection")
	}

	td.xdpC = coll
	td.xdp = prog
	return prog, nil
}

// getTC returns the test TC dispatcher program, loading it lazily on
// first call.
func (td *testDispatchers) getTC() (*ebpf.Program, error) {
	td.mu.Lock()
	defer td.mu.Unlock()

	if td.tc != nil {
		return td.tc, nil
	}

	cfg, err := dispatcher.NewTCConfig(1)
	if err != nil {
		return nil, fmt.Errorf("create test TC dispatcher config: %w", err)
	}

	spec, err := dispatcher.LoadTCDispatcher(cfg)
	if err != nil {
		return nil, fmt.Errorf("load test TC dispatcher spec: %w", err)
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, fmt.Errorf("create test TC dispatcher collection: %w", err)
	}

	prog := coll.Programs["tc_dispatcher"]
	if prog == nil {
		coll.Close()
		return nil, fmt.Errorf("tc_dispatcher program not found in test collection")
	}

	td.tcC = coll
	td.tc = prog
	return prog, nil
}

package ebpf

import (
	"fmt"

	"github.com/cilium/ebpf"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
)

// loadTestDispatcher loads a minimal one-slot dispatcher of the given
// kind for use as an extension load's verifier target: loading an
// XDP/TC program as BPF_PROG_TYPE_EXT needs a target program whose
// BTF the kernel checks the extension's signature against. It returns
// the dispatcher program and a close function releasing the whole
// collection.
//
// The caller closes the collection as soon as the extension has
// loaded. The kernel takes its own reference on the target program
// (prog->aux->dst_prog) for as long as the extension exists, so the
// userspace FDs need not outlive the load call -- and must not: a
// quiescent daemon holds no bpf object FDs, and the gRPC e2e suite
// asserts exactly that against the daemon's fd table.
func loadTestDispatcher(programType bpfman.ProgramType) (*ebpf.Program, func(), error) {
	var (
		spec     *ebpf.CollectionSpec
		progName string
		err      error
	)
	switch programType {
	case bpfman.ProgramTypeXDP:
		cfg, cfgErr := dispatcher.NewXDPConfig(1, false)
		if cfgErr != nil {
			return nil, nil, fmt.Errorf("create test XDP dispatcher config: %w", cfgErr)
		}
		spec, err = dispatcher.LoadXDPDispatcher(cfg)
		progName = "xdp_dispatcher"
	case bpfman.ProgramTypeTC:
		cfg, cfgErr := dispatcher.NewTCConfig(1)
		if cfgErr != nil {
			return nil, nil, fmt.Errorf("create test TC dispatcher config: %w", cfgErr)
		}
		spec, err = dispatcher.LoadTCDispatcher(cfg)
		progName = "tc_dispatcher"
	default:
		return nil, nil, fmt.Errorf("no test dispatcher for program type %s", programType)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load test dispatcher spec for %s: %w", programType, err)
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, nil, fmt.Errorf("create test dispatcher collection for %s: %w", programType, err)
	}

	prog := coll.Programs[progName]
	if prog == nil {
		coll.Close()
		return nil, nil, fmt.Errorf("%s program not found in test dispatcher collection", progName)
	}
	return prog, func() { coll.Close() }, nil
}

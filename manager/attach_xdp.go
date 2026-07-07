package manager

import (
	"context"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/manager/action"
)

// attachXDP attaches an XDP program to a network interface using the
// dispatcher model for multi-program chaining.
//
// Every attach triggers a full dispatcher rebuild: a new dispatcher
// is loaded with updated .rodata config, all extensions are re-attached,
// and the XDP link is atomically swapped (or created for first attach).
//
// Pin paths follow the Rust bpfman convention:
//   - Dispatcher link: /sys/fs/bpf/bpfman/xdp/dispatcher_{nsid}_{ifindex}_link
//   - Dispatcher prog: /sys/fs/bpf/bpfman/xdp/dispatcher_{nsid}_{ifindex}_{revision}/dispatcher
//   - Extension links: /sys/fs/bpf/bpfman/xdp/dispatcher_{nsid}_{ifindex}_{revision}/link_{position}
func (m *Manager) attachXDP(ctx context.Context, spec bpfman.XDPAttachSpec) (bpfman.Link, error) {
	ifname := spec.Ifname()
	netnsPath := spec.Netns()
	ifindex, err := m.kernel.InterfaceByName(ctx, ifname, netnsPath)
	if err != nil {
		return bpfman.Link{}, err
	}

	priority := spec.Priority()
	proceedOn := spec.ProceedOn()
	if len(proceedOn) == 0 {
		proceedOn = []int32{
			bpfman.XDPActionPass.Int32(),
			bpfman.XDPActionDispatcherReturn.Int32(),
		}
	}

	proceedOnMask, err := dispatcher.ProceedOnMask(dispatcher.DispatcherTypeXDP, proceedOn...)
	if err != nil {
		return bpfman.Link{}, err
	}

	return m.dispatcherAttach(ctx, dispatcherAttachParams{
		programID: spec.ProgramID(),
		ifindex:   ifindex,
		ifname:    ifname,
		netnsPath: netnsPath,
		target:    ifname + ":xdp",
		dispType:  dispatcher.DispatcherTypeXDP,
		rebuildAction: func(prog bpfman.ProgramRecord) action.Action {
			return action.RebuildXDPDispatcher{
				ProgramID:   spec.ProgramID(),
				Ifindex:     uint32(ifindex),
				Ifname:      ifname,
				NetnsPath:   netnsPath,
				ProgPinPath: prog.Handles.PinPath,
				ProgramName: prog.Meta.Name,
				Priority:    priority,
				ProceedOn:   proceedOnMask,
				Metadata:    spec.Metadata(),
			}
		},
	})
}

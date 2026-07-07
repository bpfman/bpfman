package manager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/manager/action"
	"github.com/bpfman/bpfman/platform"
)

// executor interprets and executes actions.
type executor struct {
	store  platform.Store
	kernel platform.KernelOperations
	bcfs   fs.Bytecode
	bpffs  fs.BPFFS
	logger *slog.Logger
}

// newExecutor creates a new action executor.
func newExecutor(store platform.Store, kernel platform.KernelOperations, bcfs fs.Bytecode, bpffs fs.BPFFS, logger *slog.Logger) action.Executor {
	return &executor{
		store:  store,
		kernel: kernel,
		bcfs:   bcfs,
		bpffs:  bpffs,
		logger: logger,
	}
}

// Execute runs a single action, discarding any result.
func (e *executor) Execute(ctx context.Context, a action.Action) error {
	_, err := e.ExecuteResult(ctx, a)
	return err
}

// ExecuteResult runs a single action and returns its result.
// Actions that produce no value return (nil, error).
func (e *executor) ExecuteResult(ctx context.Context, a action.Action) (any, error) {
	switch a := a.(type) {
	case action.GetProgramFromStore:
		rec, err := e.store.Get(ctx, a.ProgramID)
		if err != nil {
			if errors.Is(err, platform.ErrRecordNotFound) {
				return nil, bpfman.ErrProgramNotFound{ID: a.ProgramID}
			}
			return nil, fmt.Errorf("get program %d: %w", a.ProgramID, err)
		}
		return rec, nil

	case action.LoadProgram:
		return e.kernel.Load(ctx, a.Spec, a.BPFFS)

	case action.CreateLink:
		return e.store.CreateLink(ctx, a.Spec)

	case action.DeleteLink:
		return nil, e.store.DeleteLink(ctx, a.LinkID)

	case action.CreatePendingLink:
		return e.store.CreatePendingLink(ctx, a.Spec, a.LinksDir)

	case action.FinaliseLink:
		return e.store.FinaliseLink(ctx, a.LinkID, a.KernelLinkID)

	case action.UnloadProgram:
		return nil, e.kernel.Unload(ctx, a.PinPath.String())

	case action.RemoveMapsPins:
		return nil, e.kernel.Unload(ctx, a.PinPath)

	case action.AttachTracepoint:
		return e.kernel.AttachTracepoint(ctx, a.ProgPinPath, a.Group, a.Name, a.LinkPinPath)

	case action.AttachKprobe:
		return e.kernel.AttachKprobe(ctx, a.ProgPinPath, a.FnName, a.Offset, a.Retprobe, a.LinkPinPath)

	case action.AttachUprobeLocal:
		return e.kernel.AttachUprobeLocal(ctx, a.ProgPinPath, a.Target, a.FnName, a.Offset, a.Pid, a.Retprobe, a.LinkPinPath)

	case action.AttachUprobeContainer:
		return e.kernel.AttachUprobeContainer(ctx, a.Scope, a.ProgPinPath, a.Target, a.FnName, a.Offset, a.Pid, a.Retprobe, a.LinkPinPath, a.ContainerPid)

	case action.AttachFentry:
		return e.kernel.AttachFentry(ctx, a.ProgPinPath, a.FnName, a.LinkPinPath)

	case action.AttachFexit:
		return e.kernel.AttachFexit(ctx, a.ProgPinPath, a.FnName, a.LinkPinPath)

	case action.AttachTCX:
		return e.kernel.AttachTCX(ctx, a.Ifindex, a.Direction, a.ProgPinPath, a.LinkPinPath, a.NetnsPath, a.Order)

	case action.DetachLink:
		return nil, e.kernel.DetachLink(ctx, a.PinPath)

	case action.PublishBytecode:
		return nil, e.bcfs.PublishBytecode(a.ProgramID, a.SourcePath, a.Provenance)

	case action.RemoveProgramDir:
		return nil, e.bcfs.RemoveProgramDir(a.Path)

	case action.RemoveDispatcherRevDir:
		return nil, e.bpffs.RemoveDispatcherRevDir(a.Path)

	case action.RebuildXDPDispatcher:
		return e.rebuildXDPDispatcher(ctx, a.ProgramID,
			xdpRebuildOps{ifindex: a.Ifindex, ifname: a.Ifname, netnsPath: a.NetnsPath},
			a.ProgPinPath, a.ProgramName, a.Priority, a.ProceedOn, a.Metadata)

	case action.RebuildTCDispatcher:
		return e.rebuildTCDispatcher(ctx, a.ProgramID,
			tcRebuildOps{
				ifindex:   a.Ifindex,
				ifname:    a.Ifname,
				direction: a.Direction,
				dispType:  a.DispType,
				netnsPath: a.NetnsPath,
			},
			a.ProgPinPath, a.ProgramName, a.Priority, a.ProceedOn, a.Metadata)

	case action.RebuildDispatcherForDetach:
		return nil, e.rebuildDispatcherForDetach(ctx, a.Key, a.ExcludeLinkID)

	case action.RemoveDispatcher:
		return nil, e.removeDispatcherIfEmpty(ctx, a.Key)

	default:
		return nil, fmt.Errorf("unknown action type: %T", a)
	}
}

// Ensure executor implements action.Executor.
var _ action.Executor = (*executor)(nil)

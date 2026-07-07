package manager

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/lock"
	"github.com/bpfman/bpfman/manager/action"
)

// attachTracepoint attaches a pinned program to a tracepoint.
func (m *Manager) attachTracepoint(ctx context.Context, spec bpfman.TracepointAttachSpec) (bpfman.Link, error) {
	group, name := spec.Group(), spec.Name()
	target := group + "/" + name
	return m.simpleAttach(ctx, attachParams{
		programID:     spec.ProgramID(),
		metadata:      spec.Metadata(),
		defaultTarget: target,
		prepare: func(_ bpfman.ProgramRecord, progPinPath bpfman.ProgPinPath) (attachPlan, error) {
			if err := m.validateTracepointExists(ctx, group, name); err != nil {
				return attachPlan{}, err
			}
			return attachPlan{
				target:  target,
				details: bpfman.TracepointDetails{Group: group, Name: name},
				attachAction: func(linkPinPath bpfman.LinkPath) action.Action {
					return action.AttachTracepoint{
						ProgPinPath: progPinPath,
						Group:       group,
						Name:        name,
						LinkPinPath: linkPinPath,
					}
				},
			}, nil
		},
	})
}

// validateTracepointExists rejects attaches whose group/name pair is
// not present in tracefs, so callers see ErrTracepointNotFound before
// any kernel work begins. An empty list from the kernel operations
// layer is treated as "cannot validate" (typically because tracefs is
// unavailable) and the attach is allowed to proceed to the kernel. On
// rejection the error carries up to three nearest-match suggestions
// computed from the tracefs listing.
func (m *Manager) validateTracepointExists(ctx context.Context, group, name string) error {
	tps, err := m.kernel.ListTracepoints(ctx)
	if err != nil {
		return fmt.Errorf("list tracepoints: %w", err)
	}

	if len(tps) == 0 {
		return nil
	}
	target := group + "/" + name
	if slices.Contains(tps, target) {
		return nil
	}
	return bpfman.ErrTracepointNotFound{
		Group:       group,
		Name:        name,
		Suggestions: nearestTracepoints(target, tps, 3),
	}
}

// attachKprobe attaches a pinned program to a kernel function.
// retprobe is derived from the program type stored in the database.
func (m *Manager) attachKprobe(ctx context.Context, spec bpfman.KprobeAttachSpec) (bpfman.Link, error) {
	fnName, offset := spec.FnName(), spec.Offset()
	return m.simpleAttach(ctx, attachParams{
		programID:     spec.ProgramID(),
		metadata:      spec.Metadata(),
		defaultTarget: fnName,
		prepare: func(prog bpfman.ProgramRecord, progPinPath bpfman.ProgPinPath) (attachPlan, error) {
			retprobe := prog.Load.ProgramType() == bpfman.ProgramTypeKretprobe
			return attachPlan{
				target:  fnName,
				details: bpfman.KprobeDetails{FnName: fnName, Offset: offset, Retprobe: retprobe},
				attachAction: func(linkPinPath bpfman.LinkPath) action.Action {
					return action.AttachKprobe{
						ProgPinPath: progPinPath,
						FnName:      fnName,
						Offset:      offset,
						Retprobe:    retprobe,
						LinkPinPath: linkPinPath,
					}
				},
			}, nil
		},
	})
}

// attachUprobe attaches a pinned program to a user-space function.
// retprobe is derived from the program type stored in the database.
//
// The scope parameter is required for container uprobes (containerPid > 0)
// to pass the lock fd to the helper subprocess. For local uprobes, scope
// is not used but accepted for API uniformity.
func (m *Manager) attachUprobe(ctx context.Context, scope lock.WriterScope, spec bpfman.UprobeAttachSpec) (bpfman.Link, error) {
	binaryTarget := spec.Target()
	fnName := spec.FnName()
	offset := spec.Offset()
	pid := spec.Pid()
	containerPid := spec.ContainerPid()
	return m.simpleAttach(ctx, attachParams{
		programID:     spec.ProgramID(),
		metadata:      spec.Metadata(),
		defaultTarget: binaryTarget + ":" + fnName,
		prepare: func(prog bpfman.ProgramRecord, progPinPath bpfman.ProgPinPath) (attachPlan, error) {
			retprobe := prog.Load.ProgramType() == bpfman.ProgramTypeUretprobe
			if fnName == "" && offset == 0 {
				return attachPlan{}, fmt.Errorf("uprobe attach requires a function name or a non-zero offset")
			}
			// Library-name resolution runs in bpfman's own
			// namespace, but a container uprobe's target is
			// opened inside the container's mount namespace,
			// where no resolution is implemented.
			if containerPid > 0 && !filepath.IsAbs(binaryTarget) {
				return attachPlan{}, fmt.Errorf("container uprobe target must be an absolute path (got %q; library-name resolution is not supported inside containers)", binaryTarget)
			}
			if containerPid > 0 && scope == nil {
				return attachPlan{}, fmt.Errorf("container uprobe requires lock scope (containerPid=%d)", containerPid)
			}
			var attachFn func(linkPinPath bpfman.LinkPath) action.Action
			if containerPid > 0 {
				attachFn = func(linkPinPath bpfman.LinkPath) action.Action {
					return action.AttachUprobeContainer{
						Scope:        scope,
						ProgPinPath:  progPinPath,
						Target:       binaryTarget,
						FnName:       fnName,
						Offset:       offset,
						Pid:          pid,
						Retprobe:     retprobe,
						LinkPinPath:  linkPinPath,
						ContainerPid: containerPid,
					}
				}
			} else {
				attachFn = func(linkPinPath bpfman.LinkPath) action.Action {
					return action.AttachUprobeLocal{
						ProgPinPath: progPinPath,
						Target:      binaryTarget,
						FnName:      fnName,
						Offset:      offset,
						Pid:         pid,
						Retprobe:    retprobe,
						LinkPinPath: linkPinPath,
					}
				}
			}
			return attachPlan{
				target:       binaryTarget + ":" + fnName,
				details:      bpfman.UprobeDetails{Target: binaryTarget, FnName: fnName, Offset: offset, PID: pid, Retprobe: retprobe, ContainerPid: containerPid},
				attachAction: attachFn,
			}, nil
		},
	})
}

// attachFentry attaches a pinned fentry program to its target kernel function.
// The target function was specified at load time and stored in the program's AttachFunc.
func (m *Manager) attachFentry(ctx context.Context, spec bpfman.FentryAttachSpec) (bpfman.Link, error) {
	return m.simpleAttach(ctx, attachParams{
		programID:     spec.ProgramID(),
		metadata:      spec.Metadata(),
		defaultTarget: fmt.Sprintf("fentry/program/%d", spec.ProgramID()),
		prepare: func(prog bpfman.ProgramRecord, progPinPath bpfman.ProgPinPath) (attachPlan, error) {
			fnName := prog.Load.AttachFunc()
			if fnName == "" {
				return attachPlan{}, fmt.Errorf("program %d has no attach function (fentry requires attach function at load time)", spec.ProgramID())
			}
			return attachPlan{
				target:  fnName,
				details: bpfman.FentryDetails{FnName: fnName},
				attachAction: func(linkPinPath bpfman.LinkPath) action.Action {
					return action.AttachFentry{
						ProgPinPath: progPinPath,
						FnName:      fnName,
						LinkPinPath: linkPinPath,
					}
				},
			}, nil
		},
	})
}

// attachFexit attaches a pinned fexit program to its target kernel function.
// The target function was specified at load time and stored in the program's AttachFunc.
func (m *Manager) attachFexit(ctx context.Context, spec bpfman.FexitAttachSpec) (bpfman.Link, error) {
	return m.simpleAttach(ctx, attachParams{
		programID:     spec.ProgramID(),
		metadata:      spec.Metadata(),
		defaultTarget: fmt.Sprintf("fexit/program/%d", spec.ProgramID()),
		prepare: func(prog bpfman.ProgramRecord, progPinPath bpfman.ProgPinPath) (attachPlan, error) {
			fnName := prog.Load.AttachFunc()
			if fnName == "" {
				return attachPlan{}, fmt.Errorf("program %d has no attach function (fexit requires attach function at load time)", spec.ProgramID())
			}
			return attachPlan{
				target:  fnName,
				details: bpfman.FexitDetails{FnName: fnName},
				attachAction: func(linkPinPath bpfman.LinkPath) action.Action {
					return action.AttachFexit{
						ProgPinPath: progPinPath,
						FnName:      fnName,
						LinkPinPath: linkPinPath,
					}
				},
			}, nil
		},
	})
}

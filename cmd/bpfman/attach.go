package main

import (
	"context"
	"fmt"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/cmd/bpfman/cliformat"
	"github.com/bpfman/bpfman/cmd/internal/args"
	"github.com/bpfman/bpfman/cmd/internal/runtime"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/lock"
)

// AttachCmd groups per-type attach subcommands.
type AttachCmd struct {
	// XDP attaches a loaded XDP program to a network interface via
	// the XDP dispatcher.
	XDP AttachXDPCmd `cmd:"" help:"Attach an XDP program to a network interface."`

	// TC attaches a loaded TC program to a network interface via the
	// TC dispatcher.
	TC AttachTCCmd `cmd:"" help:"Attach a TC program to a network interface."`

	// TCX attaches a loaded TCX program to a network interface using
	// native kernel ordering rather than a dispatcher.
	TCX AttachTCXCmd `cmd:"" help:"Attach a TCX program to a network interface."`

	// Tracepoint attaches a loaded program to a kernel tracepoint.
	Tracepoint AttachTracepointCmd `cmd:"" help:"Attach a program to a tracepoint."`

	// Kprobe attaches a loaded program to a kernel function probe.
	Kprobe AttachKprobeCmd `cmd:"" help:"Attach a program to a kernel probe."`

	// Uprobe attaches a loaded program to a user-space function probe.
	Uprobe AttachUprobeCmd `cmd:"" help:"Attach a program to a user-space probe."`

	// Fentry attaches a loaded program to a function-entry tracing
	// point.
	Fentry AttachFentryCmd `cmd:"" help:"Attach a program to a function entry tracing point."`

	// Fexit attaches a loaded program to a function-exit tracing
	// point.
	Fexit AttachFexitCmd `cmd:"" help:"Attach a program to a function exit tracing point."`
}

// runAttach is the common attach pattern: build the spec, create the
// manager, attach under the writer lock, and render the created link.
// The build callback returns the fully-configured spec, including its
// metadata; runAttach owns the attach and the rendering.
func runAttach(cli *runtime.CLI, ctx context.Context, flags *cliformat.OutputFlags, build func() (bpfman.AttachSpec, error)) error {
	format, err := flags.Format()
	if err != nil {
		return err
	}

	mgr, cleanup, err := newManager(ctx, cli)
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}
	defer cleanup()

	link, err := runtime.RunWithLockValue(ctx, cli, func(ctx context.Context, writeLock lock.WriterScope) (bpfman.Link, error) {
		spec, err := build()
		if err != nil {
			return bpfman.Link{}, err
		}
		return mgr.Attach(ctx, writeLock, spec)
	})
	if err != nil {
		return err
	}

	return cliformat.RenderLinkAttach(cli.Out, link, format)
}

// AttachXDPCmd attaches an XDP program to a network interface.
type AttachXDPCmd struct {
	// OutputFlags carries the -o/--output flag selecting text or
	// JSON rendering of the created link.
	cliformat.OutputFlags

	// MetadataFlags carries the repeatable -m/--metadata
	// key/value labels recorded on the new link.
	MetadataFlags

	// ProgramID is the kernel ID of the loaded program to attach.
	ProgramID kernel.ProgramID `arg:"" name:"program-id" help:"Program ID to attach."`

	// Iface is the name of the network interface to attach to.
	Iface string `arg:"" name:"iface" help:"Network interface."`

	// Priority is the program's position in the XDP dispatcher
	// chain; lower values run first and the value must be
	// non-negative.
	Priority int `short:"p" name:"priority" required:"" help:"Priority in chain (lower runs first; non-negative). Slot exhaustion (more than 10 attachments) is reported by the dispatcher, not by this flag."`

	// ProceedOn lists the XDP return actions for which the
	// dispatcher continues to the next program in the chain; it
	// defaults to pass and dispatcher_return.
	ProceedOn []bpfman.XDPAction `name:"proceed-on" sep:"," help:"XDP actions to proceed on (comma-separated or repeated). Values: aborted, drop, pass, tx, redirect, dispatcher_return." default:"pass,dispatcher_return"`

	// Netns is an optional path to the network namespace holding the
	// interface; empty attaches in the host namespace.
	Netns string `short:"n" name:"netns" help:"Network namespace path."`
}

// Run builds an XDP attach spec from the flags, attaches the program
// to the named interface under the writer lock, and renders the
// resulting link.
func (c *AttachXDPCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	return runAttach(cli, ctx, &c.OutputFlags, func() (bpfman.AttachSpec, error) {
		spec, err := bpfman.NewXDPAttachSpec(c.ProgramID, c.Iface, c.Priority)
		if err != nil {
			return nil, fmt.Errorf("invalid XDP spec: %w", err)
		}

		spec = spec.WithProceedOnActions(c.ProceedOn)
		if c.Netns != "" {
			spec = spec.WithNetns(c.Netns)
		}

		return spec.WithMetadata(args.MetadataMap(c.Metadata)), nil
	})
}

// AttachTCCmd attaches a TC program to a network interface.
type AttachTCCmd struct {
	// OutputFlags carries the -o/--output flag selecting text or
	// JSON rendering of the created link.
	cliformat.OutputFlags

	// MetadataFlags carries the repeatable -m/--metadata
	// key/value labels recorded on the new link.
	MetadataFlags

	// ProgramID is the kernel ID of the loaded program to attach.
	ProgramID kernel.ProgramID `arg:"" name:"program-id" help:"Program ID to attach."`

	// Iface is the name of the network interface to attach to.
	Iface string `arg:"" name:"iface" help:"Network interface."`

	// Direction selects whether the program runs on ingress or
	// egress traffic.
	Direction bpfman.TCDirection `arg:"" name:"direction" help:"Direction (ingress or egress)."`

	// Priority is the program's position in the TC dispatcher chain;
	// lower values run first and the value must be non-negative.
	Priority int `short:"p" name:"priority" required:"" help:"Priority in chain (lower runs first; non-negative). Slot exhaustion (more than 10 attachments) is reported by the dispatcher, not by this flag."`

	// ProceedOn lists the TC return actions for which the dispatcher
	// continues to the next program in the chain; it defaults to
	// pipe and dispatcher_return.
	ProceedOn []bpfman.TCAction `name:"proceed-on" sep:"," help:"TC actions to proceed on (comma-separated or repeated). Values: unspec, ok, reclassify, shot, pipe, stolen, queued, repeat, redirect, trap, dispatcher_return." default:"pipe,dispatcher_return"`

	// Netns is an optional path to the network namespace holding the
	// interface; empty attaches in the host namespace.
	Netns string `short:"n" name:"netns" help:"Network namespace path."`
}

// Run builds a TC attach spec from the flags, attaches the program to
// the named interface and direction under the writer lock, and
// renders the resulting link.
func (c *AttachTCCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	return runAttach(cli, ctx, &c.OutputFlags, func() (bpfman.AttachSpec, error) {
		spec, err := bpfman.NewTCAttachSpec(c.ProgramID, c.Iface, c.Direction, c.Priority)
		if err != nil {
			return nil, fmt.Errorf("invalid TC spec: %w", err)
		}

		spec = spec.WithProceedOnActions(c.ProceedOn)
		if c.Netns != "" {
			spec = spec.WithNetns(c.Netns)
		}

		return spec.WithMetadata(args.MetadataMap(c.Metadata)), nil
	})
}

// AttachTCXCmd attaches a TCX program to a network interface.
type AttachTCXCmd struct {
	// OutputFlags carries the -o/--output flag selecting text or
	// JSON rendering of the created link.
	cliformat.OutputFlags

	// MetadataFlags carries the repeatable -m/--metadata
	// key/value labels recorded on the new link.
	MetadataFlags

	// ProgramID is the kernel ID of the loaded program to attach.
	ProgramID kernel.ProgramID `arg:"" name:"program-id" help:"Program ID to attach."`

	// Iface is the name of the network interface to attach to.
	Iface string `arg:"" name:"iface" help:"Network interface."`

	// Direction selects whether the program runs on ingress or
	// egress traffic.
	Direction bpfman.TCDirection `arg:"" name:"direction" help:"Direction (ingress or egress)."`

	// Priority is the program's position in the TCX chain; lower
	// values run first. TCX uses native kernel ordering rather than
	// a dispatcher.
	Priority int `short:"p" name:"priority" required:"" help:"Priority in chain (lower runs first; non-negative). TCX uses native kernel ordering, not a dispatcher."`

	// Netns is an optional path to the network namespace holding the
	// interface; empty attaches in the host namespace.
	Netns string `short:"n" name:"netns" help:"Network namespace path."`
}

// Run builds a TCX attach spec from the flags, attaches the program
// to the named interface and direction under the writer lock, and
// renders the resulting link.
func (c *AttachTCXCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	return runAttach(cli, ctx, &c.OutputFlags, func() (bpfman.AttachSpec, error) {
		spec, err := bpfman.NewTCXAttachSpec(c.ProgramID, c.Iface, c.Direction, c.Priority)
		if err != nil {
			return nil, fmt.Errorf("invalid TCX spec: %w", err)
		}

		if c.Netns != "" {
			spec = spec.WithNetns(c.Netns)
		}

		return spec.WithMetadata(args.MetadataMap(c.Metadata)), nil
	})
}

// AttachTracepointCmd attaches a program to a tracepoint.
type AttachTracepointCmd struct {
	// OutputFlags carries the -o/--output flag selecting text or
	// JSON rendering of the created link.
	cliformat.OutputFlags

	// MetadataFlags carries the repeatable -m/--metadata
	// key/value labels recorded on the new link.
	MetadataFlags

	// ProgramID is the kernel ID of the loaded program to attach.
	ProgramID kernel.ProgramID `arg:"" name:"program-id" help:"Program ID to attach."`

	// Tracepoint names the kernel tracepoint in group/name form
	// (e.g. sched/sched_switch).
	Tracepoint bpfman.Tracepoint `arg:"" name:"tracepoint" help:"Tracepoint in group/name form (e.g. sched/sched_switch)."`
}

// Run builds a tracepoint attach spec from the flags, attaches the
// program to the named tracepoint under the writer lock, and renders
// the resulting link.
func (c *AttachTracepointCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	return runAttach(cli, ctx, &c.OutputFlags, func() (bpfman.AttachSpec, error) {
		spec, err := bpfman.NewTracepointAttachSpec(c.ProgramID, c.Tracepoint)
		if err != nil {
			return nil, fmt.Errorf("invalid tracepoint spec: %w", err)
		}

		return spec.WithMetadata(args.MetadataMap(c.Metadata)), nil
	})
}

// AttachKprobeCmd attaches a program to a kernel probe.
type AttachKprobeCmd struct {
	// OutputFlags carries the -o/--output flag selecting text or
	// JSON rendering of the created link.
	cliformat.OutputFlags

	// MetadataFlags carries the repeatable -m/--metadata
	// key/value labels recorded on the new link.
	MetadataFlags

	// ProgramID is the kernel ID of the loaded program to attach.
	ProgramID kernel.ProgramID `arg:"" name:"program-id" help:"Program ID to attach."`

	// FnName is the kernel function to probe.
	FnName string `arg:"" name:"fn-name" help:"Kernel function name to attach to."`

	// Offset is the byte offset within the function at which to
	// attach; it defaults to 0 (the function entry).
	Offset uint64 `name:"offset" help:"Offset within the function." default:"0"`
}

// Run builds a kprobe attach spec from the flags, attaches the
// program to the named kernel function under the writer lock, and
// renders the resulting link.
func (c *AttachKprobeCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	return runAttach(cli, ctx, &c.OutputFlags, func() (bpfman.AttachSpec, error) {
		spec, err := bpfman.NewKprobeAttachSpec(c.ProgramID, c.FnName)
		if err != nil {
			return nil, fmt.Errorf("invalid kprobe spec: %w", err)
		}

		if c.Offset != 0 {
			spec = spec.WithOffset(c.Offset)
		}

		return spec.WithMetadata(args.MetadataMap(c.Metadata)), nil
	})
}

// AttachUprobeCmd attaches a program to a user-space probe.
type AttachUprobeCmd struct {
	// OutputFlags carries the -o/--output flag selecting text or
	// JSON rendering of the created link.
	cliformat.OutputFlags

	// MetadataFlags carries the repeatable -m/--metadata
	// key/value labels recorded on the new link.
	MetadataFlags

	// ProgramID is the kernel ID of the loaded program to attach.
	ProgramID kernel.ProgramID `arg:"" name:"program-id" help:"Program ID to attach."`

	// Target is the binary or library to probe: an absolute path, or
	// a bare library name (e.g. libc) resolved like the dynamic
	// linker.
	Target string `arg:"" name:"target" help:"Absolute path to the target binary or library, or a bare library name (e.g. libc) resolved like the dynamic linker."`

	// FnName is the function within the target to probe; if empty,
	// Offset alone locates the attach point.
	FnName string `short:"f" name:"fn-name" help:"Function name to attach to."`

	// Offset is the byte offset within the target at which to
	// attach; it defaults to 0.
	Offset uint64 `name:"offset" help:"Offset within the function." default:"0"`

	// Pid restricts tracing to a single process ID; 0 traces all
	// processes.
	Pid int32 `name:"pid" help:"Only trace this process ID (0 traces all processes)."`

	// ContainerPid identifies the container whose mount namespace
	// resolves Target, enabling namespace-aware attachment.
	ContainerPid int32 `name:"container-pid" help:"Container PID for namespace-aware uprobe attachment."`
}

// Run builds a uprobe attach spec from the flags, attaches the
// program to the target binary or library under the writer lock, and
// renders the resulting link.
func (c *AttachUprobeCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	return runAttach(cli, ctx, &c.OutputFlags, func() (bpfman.AttachSpec, error) {
		spec, err := bpfman.NewUprobeAttachSpec(c.ProgramID, c.Target, c.Pid, c.ContainerPid)
		if err != nil {
			return nil, fmt.Errorf("invalid uprobe spec: %w", err)
		}

		if c.FnName != "" {
			spec = spec.WithFnName(c.FnName)
		}
		if c.Offset != 0 {
			spec = spec.WithOffset(c.Offset)
		}

		return spec.WithMetadata(args.MetadataMap(c.Metadata)), nil
	})
}

// AttachFentryCmd attaches a program to a function entry tracing point.
type AttachFentryCmd struct {
	// OutputFlags carries the -o/--output flag selecting text or
	// JSON rendering of the created link.
	cliformat.OutputFlags

	// MetadataFlags carries the repeatable -m/--metadata
	// key/value labels recorded on the new link.
	MetadataFlags

	// ProgramID is the kernel ID of the loaded fentry program to
	// attach. The traced function is fixed at load time, so no
	// further target is required.
	ProgramID kernel.ProgramID `arg:"" name:"program-id" help:"Program ID to attach."`
}

// Run builds an fentry attach spec from the program ID, attaches the
// program under the writer lock, and renders the resulting link.
func (c *AttachFentryCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	return runAttach(cli, ctx, &c.OutputFlags, func() (bpfman.AttachSpec, error) {
		spec, err := bpfman.NewFentryAttachSpec(c.ProgramID)
		if err != nil {
			return nil, fmt.Errorf("invalid fentry spec: %w", err)
		}

		return spec.WithMetadata(args.MetadataMap(c.Metadata)), nil
	})
}

// AttachFexitCmd attaches a program to a function exit tracing point.
type AttachFexitCmd struct {
	// OutputFlags carries the -o/--output flag selecting text or
	// JSON rendering of the created link.
	cliformat.OutputFlags

	// MetadataFlags carries the repeatable -m/--metadata
	// key/value labels recorded on the new link.
	MetadataFlags

	// ProgramID is the kernel ID of the loaded fexit program to
	// attach. The traced function is fixed at load time, so no
	// further target is required.
	ProgramID kernel.ProgramID `arg:"" name:"program-id" help:"Program ID to attach."`
}

// Run builds an fexit attach spec from the program ID, attaches the
// program under the writer lock, and renders the resulting link.
func (c *AttachFexitCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	return runAttach(cli, ctx, &c.OutputFlags, func() (bpfman.AttachSpec, error) {
		spec, err := bpfman.NewFexitAttachSpec(c.ProgramID)
		if err != nil {
			return nil, fmt.Errorf("invalid fexit spec: %w", err)
		}

		return spec.WithMetadata(args.MetadataMap(c.Metadata)), nil
	})
}

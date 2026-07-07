package main

import (
	"context"
	"fmt"

	"github.com/bpfman/bpfman/cmd/bpfman/cliformat"
	"github.com/bpfman/bpfman/cmd/internal/runtime"
	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/lock"
)

// DispatcherCmd groups dispatcher management subcommands.
type DispatcherCmd struct {
	// List lists dispatchers; it is the default subcommand when none
	// is given.
	List ListDispatchersCmd `cmd:"" default:"withargs" help:"List dispatchers."`

	// Get shows the details of a single dispatcher by its key.
	Get GetDispatcherCmd `cmd:"" help:"Get dispatcher details."`

	// Delete removes a dispatcher by its key; hidden because
	// dispatcher lifecycle is normally managed automatically.
	Delete DeleteDispatcherCmd `cmd:"" hidden:"" help:"Delete a dispatcher."`
}

// ListDispatchersCmd lists all dispatchers. Zero filter values mean
// unfiltered: nsid 0 and ifindex 0 never identify a real dispatcher,
// matching the zero DispatcherType sentinel.
type ListDispatchersCmd struct {
	// OutputFlags carries the -o/--output flag selecting text or
	// JSON rendering.
	cliformat.OutputFlags

	// Type filters the listing to one dispatcher type (xdp,
	// tc-ingress, tc-egress); the zero value lists all types.
	Type dispatcher.DispatcherType `name:"type" help:"Filter by dispatcher type (xdp, tc-ingress, tc-egress)."`

	// Nsid filters by network namespace ID; 0 means unfiltered.
	Nsid uint64 `name:"nsid" help:"Filter by network namespace ID."`

	// Ifindex filters by interface index; 0 means unfiltered.
	Ifindex uint32 `name:"ifindex" help:"Filter by interface index."`
}

// Run executes the list dispatchers command.
func (c *ListDispatchersCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	format, err := c.OutputFlags.Format()
	if err != nil {
		return err
	}

	mgr, cleanup, err := newManager(ctx, cli)
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}
	defer cleanup()

	summaries, err := mgr.ListDispatcherSummaries(ctx)
	if err != nil {
		return err
	}

	// Client-side key filters
	filter := dispatcher.KeyFilter{Type: c.Type, Nsid: c.Nsid, Ifindex: c.Ifindex}
	filtered := summaries[:0]
	for _, s := range summaries {
		if filter.Matches(s.Key) {
			filtered = append(filtered, s)
		}
	}
	summaries = filtered

	if len(summaries) == 0 && !format.IsStructured() {
		return nil
	}

	return cliformat.RenderDispatcherList(cli.Out, summaries, format)
}

// GetDispatcherCmd gets details of a dispatcher by its key.
type GetDispatcherCmd struct {
	// OutputFlags carries the -o/--output flag selecting text or
	// JSON rendering.
	cliformat.OutputFlags

	// Type is the dispatcher type (xdp, tc-ingress, tc-egress); part
	// of the key identifying the dispatcher.
	Type dispatcher.DispatcherType `arg:"" help:"Dispatcher type (xdp, tc-ingress, tc-egress)."`

	// Nsid is the network namespace ID; part of the key identifying
	// the dispatcher.
	Nsid uint64 `arg:"" help:"Network namespace ID."`

	// Ifindex is the interface index; part of the key identifying
	// the dispatcher.
	Ifindex uint32 `arg:"" help:"Interface index."`
}

// Run executes the get dispatcher command.
func (c *GetDispatcherCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	format, err := c.OutputFlags.Format()
	if err != nil {
		return err
	}

	key := dispatcher.NewKey(c.Type, c.Nsid, c.Ifindex)

	mgr, cleanup, err := newManager(ctx, cli)
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}
	defer cleanup()

	snap, err := mgr.GetDispatcherSnapshot(ctx, key)
	if err != nil {
		return err
	}

	return cliformat.RenderDispatcherSnapshot(cli.Out, snap, format)
}

// DeleteDispatcherCmd deletes a dispatcher by its key.
type DeleteDispatcherCmd struct {
	// Type is the dispatcher type (xdp, tc-ingress, tc-egress); part
	// of the key identifying the dispatcher.
	Type dispatcher.DispatcherType `arg:"" help:"Dispatcher type (xdp, tc-ingress, tc-egress)."`

	// Nsid is the network namespace ID; part of the key identifying
	// the dispatcher.
	Nsid uint64 `arg:"" help:"Network namespace ID."`

	// Ifindex is the interface index; part of the key identifying
	// the dispatcher.
	Ifindex uint32 `arg:"" help:"Interface index."`
}

// Run executes the delete dispatcher command.
func (c *DeleteDispatcherCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	key := dispatcher.NewKey(c.Type, c.Nsid, c.Ifindex)

	mgr, cleanup, err := newManager(ctx, cli)
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}
	defer cleanup()

	return runtime.RunWithLock(ctx, cli, func(ctx context.Context, writeLock lock.WriterScope) error {
		return mgr.DeleteDispatcherSnapshot(ctx, writeLock, key)
	})
}

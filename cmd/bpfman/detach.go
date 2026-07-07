package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/cmd/internal/runtime"
	"github.com/bpfman/bpfman/lock"
)

// DetachCmd detaches links.
type DetachCmd struct {
	// LinkIDs are the IDs of the links to detach; at least one is
	// required.
	LinkIDs []bpfman.LinkID `arg:"" name:"link-id" help:"Link IDs to detach." required:""`

	// IgnoreMissing treats a "link not found" error as success,
	// making a repeated detach (e.g. from a defer) idempotent.
	IgnoreMissing bool `name:"ignore-missing" help:"Treat 'link not found' as success rather than an error; useful for idempotent cleanup (e.g. defer)."`
}

// Run executes the detach command: mutation under lock, output outside.
func (c *DetachCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	mgr, cleanup, err := newManager(ctx, cli)
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}
	defer cleanup()

	return runtime.RunBatchMutation(ctx, cli, c.LinkIDs, "link", "detach",
		func(ctx context.Context, writeLock lock.WriterScope, id bpfman.LinkID) error {
			err := mgr.Detach(ctx, writeLock, id)
			if err != nil && c.IgnoreMissing {
				var notFound bpfman.ErrLinkNotFound
				if errors.As(err, &notFound) {
					return nil
				}
			}

			return err
		})
}

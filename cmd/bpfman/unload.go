package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/cmd/internal/runtime"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/lock"
)

// UnloadCmd unloads managed BPF programs by program ID.
type UnloadCmd struct {
	// ProgramIDs are the kernel program IDs to unload; at least one is
	// required.
	ProgramIDs []kernel.ProgramID `arg:"" name:"program-id" help:"Program IDs to unload." required:""`

	// IgnoreMissing treats a "program not found" error as success rather
	// than a failure, making the command idempotent for cleanup paths such
	// as defer.
	IgnoreMissing bool `name:"ignore-missing" help:"Treat 'program not found' as success rather than an error; useful for idempotent cleanup (e.g. defer)."`
}

// Run unloads each requested program ID under the writer lock as a batch
// mutation, with output emitted outside the lock. When IgnoreMissing is
// set, an unload that fails because the program is not found is treated
// as success.
func (c *UnloadCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	mgr, cleanup, err := newManager(ctx, cli)
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}
	defer cleanup()

	return runtime.RunBatchMutation(ctx, cli, c.ProgramIDs, "program", "unload",
		func(ctx context.Context, writeLock lock.WriterScope, id kernel.ProgramID) error {
			err := mgr.Unload(ctx, writeLock, id)
			if err != nil && c.IgnoreMissing {
				var notFound bpfman.ErrProgramNotFound
				if errors.As(err, &notFound) {
					return nil
				}
			}

			return err
		})
}

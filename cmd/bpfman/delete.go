package main

import (
	"context"
	"fmt"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/cmd/internal/runtime"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/lock"
	"github.com/bpfman/bpfman/manager"
)

// ProgramDeleteCmd deletes BPF programs with cascading cleanup.
// For each program: detaches all links, then unloads the program.
// With --recursive, also removes programs that depend on the target
// through map ownership (map_owner_id). With --all, every managed
// program is deleted.
type ProgramDeleteCmd struct {
	// Recursive also deletes programs that depend on the targets
	// through shared maps (map_owner_id dependents).
	Recursive bool `short:"r" name:"recursive" help:"Also delete programs that share maps with the target (map_owner_id dependents)."`

	// All deletes every managed program; mutually exclusive with
	// explicit program IDs.
	All bool `name:"all" help:"Delete all managed programs."`

	// ProgramIDs are the kernel IDs of the programs to delete;
	// omitted when --all is given.
	ProgramIDs []kernel.ProgramID `arg:"" name:"program-id" optional:"" help:"Program IDs to delete."`
}

// Validate ensures exactly one of --all or explicit program IDs is
// provided.
func (c *ProgramDeleteCmd) Validate() error {
	if c.All && len(c.ProgramIDs) > 0 {
		return fmt.Errorf("--all and explicit program IDs are mutually exclusive")
	}
	if !c.All && len(c.ProgramIDs) == 0 {
		return fmt.Errorf("provide at least one program ID or use --all")
	}
	return nil
}

// Run executes the program delete command with cascading cleanup.
func (c *ProgramDeleteCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	mgr, cleanup, err := newManager(ctx, cli)
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}
	defer cleanup()

	ids, err := mgr.ResolveDeleteProgramIDs(ctx, c.All, c.ProgramIDs)
	if err != nil {
		return err
	}

	return executeDeletePrograms(ctx, cli, mgr, ids, c.Recursive, c.All)
}

// executeDeletePrograms deletes the given programs with cascading
// cleanup. Locking is handled internally.
func executeDeletePrograms(ctx context.Context, cli *runtime.CLI, mgr *manager.Manager, ids []kernel.ProgramID, recursive bool, all bool) error {
	results := make([]runtime.BatchResult[kernel.ProgramID], 0, len(ids))

	lockErr := runtime.RunWithLock(ctx, cli, func(ctx context.Context, writeLock lock.WriterScope) error {
		deleteResults := mgr.DeletePrograms(ctx, writeLock, ids, manager.DeleteProgramsOpts{
			Recursive: recursive,
			All:       all,
		})
		for _, r := range deleteResults {
			results = append(results, runtime.BatchResult[kernel.ProgramID]{ID: r.ProgramID, Err: r.Err})
		}
		return nil
	})
	if lockErr != nil {
		return lockErr
	}

	return runtime.ReportBatchFailures(cli, "program", "delete", results)
}

// LinkDeleteCmd deletes BPF links with cascading cleanup.
// For each link: detaches the link, then unloads the program if it
// has no remaining links. With --recursive, also removes programs
// that depend on the orphaned program through map ownership.
type LinkDeleteCmd struct {
	// Recursive also deletes programs that share maps with programs
	// orphaned by the detach (map_owner_id dependents).
	Recursive bool `short:"r" name:"recursive" help:"Also delete programs that share maps with orphaned programs (map_owner_id dependents)."`

	// LinkIDs are the IDs of the links to delete; at least one is
	// required.
	LinkIDs []bpfman.LinkID `arg:"" name:"link-id" help:"Link IDs to delete." required:""`
}

// Run executes the link delete command with cascading cleanup.
func (c *LinkDeleteCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	mgr, cleanup, err := newManager(ctx, cli)
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}
	defer cleanup()

	results := make([]runtime.BatchResult[bpfman.LinkID], 0, len(c.LinkIDs))

	lockErr := runtime.RunWithLock(ctx, cli, func(ctx context.Context, writeLock lock.WriterScope) error {
		deleteResults := mgr.DeleteLinks(ctx, writeLock, c.LinkIDs, manager.DeleteLinksOpts{Recursive: c.Recursive})
		for _, r := range deleteResults {
			results = append(results, runtime.BatchResult[bpfman.LinkID]{ID: r.LinkID, Err: r.Err})
		}
		return nil
	})
	if lockErr != nil {
		return lockErr
	}

	return runtime.ReportBatchFailures(cli, "link", "delete", results)
}

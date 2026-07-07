package main

import (
	"context"
	"fmt"

	cmdruntime "github.com/bpfman/bpfman/cmd/internal/runtime"
	"github.com/bpfman/bpfman/internal/bpfman/runtimestate"
	"github.com/bpfman/bpfman/manager"
	"github.com/bpfman/bpfman/platform"
)

// newManager creates a manager for bpfman CLI commands.
func newManager(ctx context.Context, cli *cmdruntime.CLI) (*manager.Manager, func() error, error) {
	return newManagerWithImagePuller(ctx, cli, nil)
}

// newManagerWithImagePuller creates a manager using the supplied image
// puller. A nil puller is valid for commands that never load from OCI
// images.
func newManagerWithImagePuller(ctx context.Context, cli *cmdruntime.CLI, puller platform.ImagePuller) (*manager.Manager, func() error, error) {
	layout, err := cli.Layout()
	if err != nil {
		return nil, nil, fmt.Errorf("invalid runtime directory: %w", err)
	}

	logger := cli.Logger()

	opened, err := runtimestate.OpenMutable(ctx, layout, logger, cli.LockTimeout)
	if err != nil {
		return nil, nil, fmt.Errorf("open runtime: %w", err)
	}

	mgr, err := manager.New(opened.FS, puller, opened.Store, opened.Kernel, opened.Validator, logger)
	if err != nil {
		opened.Close()
		return nil, nil, fmt.Errorf("create manager: %w", err)
	}

	cleanup := func() error {
		return opened.Close()
	}

	return mgr, cleanup, nil
}

package main

import (
	"context"
	"fmt"

	"github.com/bpfman/bpfman/cmd/bpfman/cliformat"
	"github.com/bpfman/bpfman/cmd/internal/args"
	"github.com/bpfman/bpfman/cmd/internal/runtime"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/manager"
)

// LoadCmd loads a BPF program from an object file or OCI image.
type LoadCmd struct {
	// File loads programs from a local object file; it is the default
	// subcommand when "load" is given no further verb.
	File LoadFileCmd `cmd:"" default:"withargs" help:"Load from a local object file."`

	// Image loads programs from an OCI container image holding the
	// bytecode.
	Image LoadImageCmd `cmd:"" help:"Load from an OCI container image."`
}

// loadFlags carries the flags common to loading from a file and from an
// image: the output format, metadata, global data, the required program
// list, the application grouping, and an optional map-owner share. Each
// load command embeds it alongside its source-specific flags.
type loadFlags struct {
	cliformat.OutputFlags
	MetadataFlags
	GlobalDataFlags

	// Programs names every program to load, each given as TYPE:NAME or
	// TYPE:NAME:ATTACH_FUNC (comma-separated or repeated). For fentry/fexit
	// the ATTACH_FUNC component is required. The flag is required: there is
	// no untyped bulk load, because a section-derived type would be a guess
	// (classifier sections cannot distinguish tc from tcx).
	Programs []args.ProgramSpec `name:"programs" sep:"," required:"" help:"TYPE:NAME or TYPE:NAME:ATTACH_FUNC program to load (comma-separated or repeated). For fentry/fexit, ATTACH_FUNC is required. Every program to load must be named."`

	// Application groups the loaded programs under an application name,
	// stored as the bpfman.io/application metadata key.
	Application string `short:"a" name:"application" help:"Application name to group programs (stored as bpfman.io/application metadata)."`

	// MapOwnerID is the kernel program ID of an already-loaded program
	// whose maps these programs should share instead of creating their own.
	MapOwnerID kernel.ProgramID `name:"map-owner-id" help:"Program ID of another program to share maps with."`
}

// requestOpts builds the load-request options shared by both load
// sources from the common flags.
func (f loadFlags) requestOpts() manager.LoadRequestOpts {
	var globalData map[string][]byte
	if len(f.GlobalData) > 0 {
		globalData = args.GlobalDataMap(f.GlobalData)
	}

	return manager.LoadRequestOpts{
		UserMetadata: args.MetadataMap(f.Metadata),
		GlobalData:   globalData,
		Application:  f.Application,
		MapOwnerID:   f.MapOwnerID,
	}
}

// LoadFileCmd loads a BPF program from a local object file.
type LoadFileCmd struct {
	loadFlags

	// Path is the filesystem path to the BPF object file (.o) to load.
	Path string `arg:"" name:"path" help:"Path to the BPF object file (.o)."`
}

// Run loads the selected programs from the local object file at Path
// (applying metadata, global data, application grouping and any
// map-owner share) and renders the loaded programs in the chosen output
// format.
func (c *LoadFileCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	format, err := c.OutputFlags.Format()
	if err != nil {
		return err
	}

	mgr, cleanup, err := newManager(ctx, cli)
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}
	defer cleanup()

	return executeLoadFile(ctx, cli, mgr, c, format)
}

// executeLoadFile loads the selected programs from a local object file
// and renders them in the chosen output format.
func executeLoadFile(ctx context.Context, cli *runtime.CLI, mgr *manager.Manager, c *LoadFileCmd, format cliformat.OutputFormat) error {
	path, err := args.ParseObjectPath(c.Path)
	if err != nil {
		return err
	}

	// Manager.Load decides whether the request needs the writer flock:
	// ordinary loads stay lockless, while explicit map-owner joins and
	// PinByName loads serialise internally.
	req := manager.NewLoadRequest(manager.LoadSource{FilePath: path}, loadProgramSpecs(c.Programs), c.requestOpts())

	loaded, err := mgr.LoadFromRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to load programs: %w", err)
	}

	return cliformat.RenderLoadedPrograms(cli.Out, loaded, format)
}

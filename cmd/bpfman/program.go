package main

// ProgramCmd groups program management subcommands.
type ProgramCmd struct {
	// Load loads a BPF program into the kernel from a local object file or
	// an OCI image (the source is selected by the nested subcommand).
	Load LoadCmd `cmd:"" help:"Load a BPF program from an object file or image."`

	// Unload unloads one or more managed programs by program ID, removing
	// their kernel and store state.
	Unload UnloadCmd `cmd:"" help:"Unload a managed BPF program."`

	// Get prints the details of a single program identified by its program
	// ID.
	Get GetProgramCmd `cmd:"" help:"Get details of a program by program ID."`

	// List lists managed programs, optionally filtered; it is the default
	// subcommand when "program" is given no further verb.
	List ListProgramsCmd `cmd:"" default:"withargs" help:"List managed programs."`

	// Delete deletes a program and performs the associated cascading
	// cleanup (detaching its links and tearing down emptied dispatchers).
	Delete ProgramDeleteCmd `cmd:"" hidden:"" help:"Delete a program with cascading cleanup."`
}

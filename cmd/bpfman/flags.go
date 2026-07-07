package main

import (
	"github.com/bpfman/bpfman/cmd/internal/args"
)

// MetadataFlags carries the repeatable -m/--metadata KEY=VALUE flag,
// shared by program load (program metadata) and link attach (link
// metadata).
type MetadataFlags struct {
	// Metadata holds repeatable -m/--metadata KEY=VALUE pairs recorded
	// against the program at load time or the link at attach time.
	Metadata []args.KeyValue `short:"m" name:"metadata" help:"KEY=VALUE metadata (can be repeated)."`
}

// GlobalDataFlags provides global data flags.
type GlobalDataFlags struct {
	// GlobalData holds repeatable -g/--global NAME=HEX values used to
	// populate the program's global variables at load time.
	GlobalData []args.GlobalData `short:"g" name:"global" help:"NAME=HEX global data (can be repeated)."`
}

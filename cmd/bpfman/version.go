package main

import (
	"github.com/bpfman/bpfman/cmd/internal/runtime"
	"github.com/bpfman/bpfman/version"
)

// VersionCmd prints build version information.
type VersionCmd struct{}

// AllowRootless reports that the version command may run without root:
// it only prints build metadata and touches no kernel or bpffs state, so
// the CLI's root requirement is waived for it.
func (c *VersionCmd) AllowRootless() bool { return true }

// Run prints the long-form build version information (version, commit,
// build date and the like) to the CLI's output.
func (c *VersionCmd) Run(cli *runtime.CLI) error {
	return cli.PrintOut(version.Get().Long())
}

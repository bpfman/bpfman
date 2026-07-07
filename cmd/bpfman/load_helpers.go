package main

import (
	"github.com/bpfman/bpfman/cmd/internal/args"
	"github.com/bpfman/bpfman/manager"
)

func loadProgramSpecs(programs []args.ProgramSpec) []manager.ProgramSpec {
	out := make([]manager.ProgramSpec, len(programs))
	for i, prog := range programs {
		out[i] = manager.ProgramSpec{
			Name:       prog.Name,
			Type:       prog.Type,
			AttachFunc: prog.AttachFunc,
		}
	}
	return out
}

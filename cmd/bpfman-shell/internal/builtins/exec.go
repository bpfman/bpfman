package builtins

import (
	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

func init() {
	Register(driver.Builtin{
		Name:     "exec",
		Handler:  handleExec,
		Category: driver.CategoryIO,
		Usage:    "exec <command> [args | file:$var]...",
		Summary:  "Run a host command. Use 'file:$var' to materialise a structured value as a temp file.",
	})
}

func handleExec(c driver.Ctx) (runtime.Value, error) {
	return driver.RunExecStatement(c.Ctx, c.CLI, c.Args, c.Span)
}

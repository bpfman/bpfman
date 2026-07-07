package builtins

import (
	"fmt"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

func init() {
	Register(driver.Builtin{
		Name:     "file",
		Handler:  handleFile,
		Category: driver.CategoryIO,
		Usage:    "file temp $var[.path]",
		Summary:  "Write a value to a temp file; primary is the path (assignable).",
	})
}

func handleFile(c driver.Ctx) (runtime.Value, error) {
	args := c.Args
	if len(args) == 0 || driver.ArgText(args[0]) != "temp" {
		return runtime.Value{}, fmt.Errorf("usage: file temp $var[.path] | [expr] | \"literal\"")
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("file temp requires exactly one argument")
	}
	v, err := PrintValue(args[1])
	if err != nil {
		return runtime.Value{}, fmt.Errorf("file temp: %w", err)
	}

	path, err := driver.WriteValueToTemp(v)
	if err != nil {
		return runtime.Value{}, fmt.Errorf("file temp: %w", err)
	}

	if err := c.CLI.PrintOut(path + "\n"); err != nil {
		return runtime.Value{}, err
	}
	return runtime.StringValue(path), nil
}

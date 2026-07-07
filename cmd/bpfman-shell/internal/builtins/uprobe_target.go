// uprobe-target publishes a (binary, symbol) pair usable as a
// uprobe attach target without spawning a worker. The motivating
// use case is attach-only tests (Test*_LinkRoundTrip.bpfman) that
// need a valid uprobe target to exercise the link CRUD surface
// but do not want to drive the wave protocol that `fire uprobe`
// implies.
//
// The published symbol is the same cgo-defined function the
// `fire uprobe` worker calls, so a script using `uprobe-target`
// and one using `fire uprobe` agree on the same attach point.
package builtins

import (
	"fmt"
	"os"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/fixturemode"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

func init() {
	Register(driver.Builtin{
		Name:     "uprobe-target",
		Handler:  HandleUprobeTarget,
		Category: driver.CategoryIO,
		Usage:    "uprobe-target",
		Summary:  "Resolve a (path, symbol) pair usable as a uprobe attach target.",
		Detail: "Returns a structured value with .path (the absolute path of the running " +
			"bpfman-shell ELF, via os.Executable) and .symbol (the cgo'd attach point " +
			"defined in cmd/bpfman-shell/fixturemode/uprobe.go). Use in attach-only " +
			"tests that want a valid uprobe target without the wave-protocol overhead " +
			"of `fire uprobe`.",
	})
}

// HandleUprobeTarget is the registry handler for `uprobe-target`.
// Takes no arguments; returns a record with .path and .symbol.
func HandleUprobeTarget(c driver.Ctx) (runtime.Value, error) {
	if len(c.Args) != 0 {
		return runtime.Value{}, fmt.Errorf("uprobe-target: takes no arguments")
	}
	exe, err := os.Executable()
	if err != nil {
		return runtime.Value{}, fmt.Errorf("uprobe-target: resolve executable path: %w", err)
	}
	return runtime.ValueFromMap(map[string]any{
		"path":   exe,
		"symbol": fixturemode.UprobeTargetSymbol,
	}), nil
}

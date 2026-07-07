// uprobe is the e2e built-in for deterministic userspace-probe
// targets owned by the running bpfman-shell process.
package builtins

import (
	"debug/elf"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/fixturemode"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

func init() {
	Register(driver.Builtin{
		Name:     "uprobe",
		Handler:  handleUprobe,
		Category: driver.CategoryJobs,
		Usage:    "uprobe target  |  uprobe fire N",
		Summary:  "Resolve and fire the bpfman-shell uprobe fixture target.",
		Detail: "uprobe target returns the running bpfman-shell ELF path, " +
			"fixture symbol, the symbol's absolute file offset (for " +
			"offset-only attaches with no --fn-name), and current PID. " +
			"Attach uprobe/uretprobe programs to that path and symbol, " +
			"pass pid as expected_pid, then use uprobe fire N to " +
			"synchronously call the target N times.",
	})
}

func handleUprobe(c driver.Ctx) (runtime.Value, error) {
	if len(c.Args) == 0 {
		return runtime.Value{}, fmt.Errorf("uprobe: subcommand required (valid: target, fire)")
	}
	sub := driver.ArgText(c.Args[0])
	rest := c.Args[1:]
	switch sub {
	case "target":
		return handleUprobeTarget(rest)
	case "fire":
		return handleUprobeFire(rest)
	default:
		return runtime.Value{}, fmt.Errorf("uprobe: unknown subcommand %q (valid: target, fire)", sub)
	}
}

func handleUprobeTarget(args []runtime.Arg) (runtime.Value, error) {
	if len(args) != 0 {
		return runtime.Value{}, fmt.Errorf("uprobe target: takes no arguments")
	}
	exe, err := os.Executable()
	if err != nil {
		return runtime.Value{}, fmt.Errorf("uprobe target: resolve executable path: %w", err)
	}

	symbolOffset, err := elfSymbolFileOffset(exe, fixturemode.UprobeTargetSymbol)
	if err != nil {
		return runtime.Value{}, fmt.Errorf("uprobe target: %w", err)
	}
	return runtime.ValueFromMap(map[string]any{
		"path":          exe,
		"pid":           json.Number(strconv.Itoa(os.Getpid())),
		"symbol":        fixturemode.UprobeTargetSymbol,
		"symbol_offset": json.Number(strconv.FormatUint(symbolOffset, 10)),
		"go_symbol":     fixturemode.UprobeGoTargetSymbol,
	}), nil
}

// elfSymbolFileOffset resolves a symbol's absolute file offset in
// the ELF at path: the symbol table value is a virtual address,
// translated through the containing PT_LOAD segment. This is the
// offset shape an offset-only uprobe attach (no fn_name) takes,
// and the same translation cilium performs when it resolves a
// symbol itself.
func elfSymbolFileOffset(path, symbol string) (uint64, error) {
	f, err := elf.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open ELF %s: %w", path, err)
	}
	defer f.Close()
	syms, err := f.Symbols()
	if err != nil {
		return 0, fmt.Errorf("read symbols of %s: %w", path, err)
	}

	for _, s := range syms {
		if s.Name != symbol {
			continue
		}
		for _, p := range f.Progs {
			if p.Type == elf.PT_LOAD && s.Value >= p.Vaddr && s.Value < p.Vaddr+p.Memsz {
				return s.Value - p.Vaddr + p.Off, nil
			}
		}
		return 0, fmt.Errorf("symbol %s at %#x is outside every PT_LOAD segment of %s", symbol, s.Value, path)
	}
	return 0, fmt.Errorf("symbol %s not found in %s", symbol, path)
}

func handleUprobeFire(args []runtime.Arg) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("uprobe fire: requires N")
	}
	n, err := strconv.Atoi(driver.ArgText(args[0]))
	if err != nil {
		return runtime.Value{}, fmt.Errorf("uprobe fire: N: %w", err)
	}

	if n < 0 {
		return runtime.Value{}, fmt.Errorf("uprobe fire: N must not be negative (got %d)", n)
	}
	fixturemode.FireUprobeTarget(n)
	return runtime.ValueFromEnvelope(runtime.OkEnvelope()), nil
}

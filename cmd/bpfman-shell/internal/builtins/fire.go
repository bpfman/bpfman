// fire is the e2e built-in for spawning deterministic
// kernel-stimulus workers. It is a typed wrapper over start: the
// script states the kind of stimulus (unlinkat / kill / uprobe)
// and the wave-protocol parameters; fire owns binary resolution,
// env-var construction, and process shaping.
//
// fire is for syscall / signal / uprobe event generators only.
// A richer fixture surface, if needed, lives in its own subsystem,
// not in this registry.
package builtins

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

func init() {
	Register(driver.Builtin{
		Name:     "fire",
		Handler:  handleFire,
		Category: driver.CategoryJobs,
		Usage:    "fire <kind> <sentinel> <ack> --count=N [--waves=K]",
		Summary:  "Spawn a deterministic kernel-stimulus worker; primary is a $job handle (assignable).",
		Detail: "fire is a typed wrapper over start for e2e fixtures. <kind> selects " +
			"one of the registered kernel-event generators (unlinkat, kill, uprobe). " +
			"sentinel/ack are file-path prefixes for the wave protocol: the worker " +
			"blocks on sentinel.W, fires --count=N events, and creates ack.W per " +
			"wave W in 1..K. --waves defaults to 1. Uprobe kinds publish " +
			"target_binary on the returned $job so 'bpfman link attach uprobe " +
			"$work.target_binary' attaches to the running bpfman-shell ELF. " +
			"start env BPFMAN_SHELL_MODE=... remains valid as a debug escape hatch.",
	})
}

// handleFire is the builtin handler for `fire KIND SENTINEL ACK
// --count N [--waves K]`. It resolves the kind from the registry,
// composes the BPFMAN_SHELL_MODE env, and delegates to spawnJob
// with /proc/self/exe as the executable. `start env
// BPFMAN_SHELL_MODE=...` remains valid as a debug escape hatch
// because fire is sugar over start, not a replacement.
//
// --count is required (per-wave fire count). --waves defaults to
// 1. Both must be non-negative integers; --waves must be >= 1.
// An unknown kind is rejected at runtime with a list of the
// registered names.
//
// Both spellings are accepted for each flag: `--count N` (space
// separated) and `--count=N` (equals form). The space form is
// what the scripts use because the equals form's interpolation
// of a variable (`--count=$n`) tokenises into two args
// (`--count=` plus the value) under the shell's word-splitting
// rules; quoting the whole flag as `"--count=${n}"` keeps the
// equals form intact but adds noise at every call site, so the
// space form is the primary surface. The equals form is still
// recognised so a literal value (`--count=5`) and the kill-style
// `--key=value` spelling work without surprise.
func handleFire(c driver.Ctx) (runtime.Value, error) {
	var positional []string
	count := ""
	waves := "1"
	for i := 0; i < len(c.Args); {
		text := driver.ArgText(c.Args[i])
		switch {
		case text == "--count":
			if i+1 >= len(c.Args) {
				return runtime.Value{}, fmt.Errorf("fire: --count requires a value")
			}
			count = driver.ArgText(c.Args[i+1])
			i += 2
		case strings.HasPrefix(text, "--count="):
			count = strings.TrimPrefix(text, "--count=")
			i++
		case text == "--waves":
			if i+1 >= len(c.Args) {
				return runtime.Value{}, fmt.Errorf("fire: --waves requires a value")
			}
			waves = driver.ArgText(c.Args[i+1])
			i += 2
		case strings.HasPrefix(text, "--waves="):
			waves = strings.TrimPrefix(text, "--waves=")
			i++
		case strings.HasPrefix(text, "--"):
			return runtime.Value{}, fmt.Errorf("fire: unknown flag %q", text)
		default:
			positional = append(positional, text)
			i++
		}
	}
	if len(positional) != 3 {
		return runtime.Value{}, fmt.Errorf("fire: expected 3 positional arguments (KIND SENTINEL ACK), got %d", len(positional))
	}
	kindName, sentinel, ack := positional[0], positional[1], positional[2]
	kind, ok := driver.FireKinds()[kindName]
	if !ok {
		return runtime.Value{}, fmt.Errorf("fire: unknown kind %q (registered: %s)", kindName, strings.Join(driver.FireKindNames(), ", "))
	}
	if count == "" {
		return runtime.Value{}, fmt.Errorf("fire %s: --count is required", kindName)
	}
	if n, err := strconv.Atoi(count); err != nil {
		return runtime.Value{}, fmt.Errorf("fire %s: --count: %w", kindName, err)
	} else if n < 0 {
		return runtime.Value{}, fmt.Errorf("fire %s: --count must not be negative (got %d)", kindName, n)
	}
	if k, err := strconv.Atoi(waves); err != nil {
		return runtime.Value{}, fmt.Errorf("fire %s: --waves: %w", kindName, err)
	} else if k < 1 {
		return runtime.Value{}, fmt.Errorf("fire %s: --waves must be at least 1 (got %d)", kindName, k)
	}
	shellPath, err := os.Executable()
	if err != nil {
		return runtime.Value{}, fmt.Errorf("fire %s: resolve executable path: %w", kindName, err)
	}

	env := append(os.Environ(), "BPFMAN_SHELL_MODE="+kind.Mode)
	argv := []string{shellPath, sentinel, ack, count, waves}
	var targetBinary string
	if kind.NeedsBinary {
		targetBinary = shellPath
	}
	job, err := spawnJob(c.Ctx, c.Env, spawnSpec{
		Argv:         argv,
		Env:          env,
		Origin:       c.Pos.Cite(),
		TargetBinary: targetBinary,
	})
	if err != nil {
		return runtime.Value{}, fmt.Errorf("fire %s: %w", kindName, err)
	}
	return runtime.ValueFromJob(job), nil
}

// bpfman-shell is the development / test / ops companion to bpfman.
// It hosts the DSL script runner, inspection modes, and the
// test scaffolding subcommands (net, fire, reap). Production
// deployments ship only bin/bpfman; bin/bpfman-shell is for dev and CI.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/fixturemode"
	"github.com/bpfman/bpfman/internal/bpfman/ns/runner"
)

var errInterrupted = fmt.Errorf("interrupted by signal: %w", context.Canceled)

// main wires the process-level signal model. The root context is
// cancelled by SIGINT or SIGTERM and then propagated through the
// whole CLI lifecycle: script loading and every command the runner
// spawns.
//
// A small watcher goroutine catches a second SIGINT/SIGTERM
// after the first has been observed by the running program and
// hard-exits, so a wedged script can always be killed by typing
// ^C twice. The first signal cancels the root context; the second
// is the escape hatch. A single signal.Notify registration owns
// both steps so the first signal cannot be mistaken for the second.
func main() {
	switch ran, err := runner.Run(); {
	case ran && err != nil:
		fmt.Fprintf(os.Stderr, "bpfman-ns: error: %v\n", err)
		os.Exit(1)
	case err != nil:
		fmt.Fprintf(os.Stderr, "bpfman-shell: error: %v\n", err)
		os.Exit(1)
	case ran:
		return
	}

	// Mode dispatch: when BPFMAN_SHELL_MODE is set, bpfman-shell
	// acts as a test-fixture helper rather than a user-facing
	// script entry point. The dispatch runs before NewCLI so helper
	// invocations avoid user-facing CLI initialisation. Mirrors the
	// BPFMAN_MODE pattern used by the main bpfman binary for
	// bpfman-rpc / bpfman-ns.
	if mode := os.Getenv("BPFMAN_SHELL_MODE"); mode != "" {
		if err := fixturemode.Run(mode, os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "bpfman-shell: %v\n", err)
			os.Exit(1)
		}
		return
	}

	c, err := NewCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bpfman-shell: error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)

	go func() {
		<-sig
		cancel(errInterrupted)
		<-sig
		os.Exit(1)
	}()

	if err := c.Execute(ctx); err != nil {
		os.Exit(1)
	}
}

// bpfman is a minimal BPF program manager.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bpfman/bpfman/internal/bpfman/ns/runner"
)

func main() {
	// Check if we're being invoked as the namespace helper subprocess.
	// This is a completely different execution path with its own CLI.
	switch ran, err := runner.Run(); {
	case ran && err != nil:
		fmt.Fprintf(os.Stderr, "bpfman-ns: error: %v\n", err)
		os.Exit(1)
	case err != nil:
		fmt.Fprintf(os.Stderr, "bpfman: error: %v\n", err)
		os.Exit(1)
	case ran:
		return
	}

	c, err := NewCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bpfman: error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Second signal forces immediate exit. The first signal cancels ctx
	// for graceful shutdown; if the user sends another signal during
	// shutdown, exit immediately rather than waiting.
	go func() {
		<-ctx.Done()
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		os.Exit(1)
	}()

	if err := c.Execute(ctx); err != nil {
		os.Exit(1)
	}
}

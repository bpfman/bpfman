// bpfman-e2e-cleanup finds and removes kernel-side residue left
// behind by bpfman and its e2e suite: pinned XDP dispatcher links
// whose owning process is gone, clsact qdiscs hosting a
// tc_dispatcher filter, and the test interfaces / netns the e2e
// harness leaves behind when a run is interrupted.
//
// Default is dry-run: every invocation lists what would change
// and exits zero without touching the kernel. Pass --apply to
// actually mutate. This matches the audit-then-execute discipline
// of the shell scripts that this binary replaces.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cli, err := NewCLI()
	if err != nil {
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := cli.Execute(ctx); err != nil {
		os.Exit(1)
	}
}

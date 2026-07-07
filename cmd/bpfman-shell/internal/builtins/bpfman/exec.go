package bpfmanbuiltin

import (
	"context"
	"os"
	"os/exec"

	"github.com/bpfman/bpfman/internal/execcancel"
)

func newBPFManCommand(ctx context.Context, argv ...string) (*exec.Cmd, func() error) {
	bin := os.Getenv("BPFMAN_BIN")
	if bin == "" {
		bin = "bpfman"
	}

	cmd := exec.CommandContext(ctx, bin, argv...)
	cancelled := execcancel.Configure(cmd)
	return cmd, func() error {
		if cancelled.Load() {
			return context.Cause(ctx)
		}
		return nil
	}
}

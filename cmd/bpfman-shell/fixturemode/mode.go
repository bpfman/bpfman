package fixturemode

import "fmt"

// Run dispatches the BPFMAN_SHELL_MODE entry points. Each mode is a
// test-fixture helper that runs in place of the script runner,
// returning once its work completes. Run reports an error when mode
// names no known helper, and otherwise propagates whatever the
// selected helper returns.
func Run(mode string, args []string) error {
	switch mode {
	case "uprobe-fire-worker":
		return runUprobeFireWorker(args)
	case "unlinkat-fire-worker":
		return runUnlinkatFireWorker(args)
	case "kill-fire-worker":
		return runKillFireWorker(args)
	default:
		return fmt.Errorf("unknown BPFMAN_SHELL_MODE %q", mode)
	}
}

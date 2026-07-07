package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/alecthomas/kong"

	cmdruntime "github.com/bpfman/bpfman/cmd/internal/runtime"
	"github.com/bpfman/bpfman/e2e/residue"
	"github.com/bpfman/bpfman/fs"
	"github.com/bpfman/bpfman/inspect"
	"github.com/bpfman/bpfman/internal/bpfman/runtimestate"
)

var errSnapshotSetup = errors.New("snapshot setup")

// CLI is the bpfman-e2e-cleanup root. One command, one plan. The
// shared runtime.CLI is embedded for parity with the bpfman
// and bpfman-shell binaries: --runtime-dir points at bpfman's
// runtime directory so the inspect.Snapshot-driven orphan scan
// can find pinned objects under {runtime-dir}/fs.
type CLI struct {
	cmdruntime.CLI

	kctx *kong.Context `kong:"-"`

	// Apply, when set via --apply, executes the planned actions. Without it the command only lists what would change and exits zero (dry-run).
	Apply bool `name:"apply" help:"Execute the planned actions. Without this flag the command lists what would change and exits zero."`

	// Wipe, when set via --wipe, ignores the store and returns --runtime-dir to a fresh-box state: it unmounts the bpffs at the runtime root if mounted, then removes the runtime root tree wholesale (lock file, store DB, bytecode caches, every subdirectory). The kernel and network residue scans still run, so leaked test interfaces and namespaces are swept too. Use it when the store and bpffs have drifted out of sync.
	Wipe bool `name:"wipe" help:"Ignore the store and return --runtime-dir to a fresh-box state: unmount the bpffs at the runtime root if mounted, then remove the runtime root tree wholesale (lock file, store DB, bytecode caches, every subdirectory). The kernel and network e2e residue scans still run, so leaked test interfaces and namespaces are swept too. Use when the store and bpf fs have drifted out of sync. The next bpfman invocation rebuilds a clean tree from scratch."`
}

// NewCLI parses argv and returns the configured root.
func NewCLI() (*CLI, error) {
	var c CLI
	c.kctx = kong.Parse(&c, KongOptions()...)
	c.DefaultWriters()
	if err := c.InitLogger(); err != nil {
		return nil, fmt.Errorf("create logger: %w", err)
	}
	return &c, nil
}

// Execute builds the cleanup Plan and either prints it (dry-run)
// or applies it. Three scans contribute, in dependency order:
//
//  1. inspect.Snapshot-driven orphan scan over bpfman's
//     runtime FS and the kernel link table -- needs a working
//     --runtime-dir, falls back to a comment if the manager
//     cannot be constructed.
//  2. clsact qdiscs carrying tc_dispatcher filters on
//     test-named interfaces (classic TC attachments don't
//     appear in the snapshot).
//  3. test-named host interfaces and netns, plus any XDP / TCX
//     bpf_link pins they hold.
//
// Pin paths are deduplicated across the three scans so a pin
// caught by both the orphan and iface paths is listed once.
func (c *CLI) Execute(ctx context.Context) error {
	var plan residue.Plan

	// --wipe replaces the snapshot-based orphan scan (the store
	// it would read is about to be deleted) with a wholesale
	// removal of --runtime-dir. The escape hatch when the store
	// and bpf fs have drifted into a state the normal flows
	// cannot reconcile. The kernel- and network-side scans below
	// still run: the runtime-dir wipe cannot reach a leaked test
	// veth or netns, and a survivor holds its address-pool route
	// and poisons later runs' host-route prechecks.
	if c.Wipe {
		layout, err := c.Layout()
		if err != nil {
			return fmt.Errorf("resolve layout: %w", err)
		}

		wipePlan, err := residue.ScanWipe(layout)
		if err != nil {
			return fmt.Errorf("scan runtime dir for wipe: %w", err)
		}

		plan = append(plan, wipePlan...)
	} else {
		// 1. inspect.Snapshot-driven orphan scan. Snapshot setup
		// needs the bpfman runtime dir to exist; on a fresh box or
		// after a full teardown there's nothing to snapshot, so we
		// degrade to a comment line instead of failing the whole
		// command.
		if obs, err := c.snapshot(ctx); err == nil {
			plan = append(plan, residue.PlanFromObservation(obs)...)
		} else if errors.Is(err, errSnapshotSetup) {
			fmt.Fprintf(c.Out, "# note: no bpfman runtime at %s, skipping orphan scan (%v)\n", c.RuntimeDir, err)
		} else {
			fmt.Fprintf(c.Out, "# warning: snapshot bpfman state: %v\n", err)
		}
	}

	// 2. tc_dispatcher clsact on test-named interfaces.
	tcPlan, err := residue.ScanTCDispatcherResidue(residue.DefaultNetnsDir)
	if err != nil {
		return fmt.Errorf("scan TC dispatcher residue: %w", err)
	}

	plan = append(plan, tcPlan...)

	// 3. test ifaces, netns, and the bpf_link pins attached to
	// those ifaces. ip link del cascades clsact, so the qdisc
	// removals from step 2 are usually redundant for
	// ScanE2EResidue's iface universe; we keep them for cases
	// where the qdisc lives on a netdev outside the regex (e.g.
	// in a B<hex>N netns on a non-B<hex>N peer name).
	e2ePlan, err := residue.ScanE2EResidue(residue.DefaultBPFFS, residue.DefaultNetnsDir)
	if err != nil {
		return fmt.Errorf("scan e2e residue: %w", err)
	}

	plan = append(plan, e2ePlan...)

	plan = plan.Dedup()

	return c.finish(plan)
}

func (c *CLI) snapshot(ctx context.Context) (*inspect.Observation, error) {
	layout, err := c.Layout()
	if err != nil {
		return nil, fmt.Errorf("%w: invalid runtime directory: %w", errSnapshotSetup, err)
	}

	logger := c.Logger()

	opened, err := runtimestate.OpenSnapshotReader(ctx, layout, logger)
	if err != nil {
		return nil, fmt.Errorf("%w: open runtime: %w", errSnapshotSetup, err)
	}

	defer opened.Close()

	return opened.Snapshot(ctx)
}

// finish prints the plan in dry-run mode or applies it. Shared
// by the normal scan path and the --wipe path.
func (c *CLI) finish(plan residue.Plan) error {
	if !c.Apply {
		fmt.Fprintln(c.Out, "# dry run -- pass --apply to execute")
		if plan.Empty() {
			fmt.Fprintln(c.Out, "# nothing to do")
			return nil
		}
		plan.Describe(c.Out)
		return nil
	}

	failures := plan.Apply()
	for _, f := range failures {
		fmt.Fprintf(os.Stderr, "FAIL: %s: %v\n", f.Action.Describe(), f.Err)
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d of %d action(s) failed", len(failures), len(plan))
	}
	return nil
}

// KongOptions configures Kong for the cleanup binary.
func KongOptions() []kong.Option {
	return []kong.Option{
		kong.Name("bpfman-e2e-cleanup"),
		kong.Description("Find and remove kernel-side bpfman and e2e residue. Default is dry-run; pass --apply to mutate."),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
		kong.Groups{
			"global": "Global Flags:",
		},
		kong.ShortUsageOnError(),
		kong.Vars{
			"default_runtime_dir":     fs.DefaultRoot,
			"default_image_cache_dir": "/var/cache/bpfman",
		},
	}
}

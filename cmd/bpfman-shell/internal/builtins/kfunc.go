// kfunc is the e2e built-in for leased kernel-function attach
// slots exported by the bpfman_e2e_targets module. It gives
// fentry/fexit and kprobe/kretprobe tests an isolated function
// symbol plus debugfs trigger/count files.
package builtins

import (
	"fmt"
	"os"
	"strconv"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

func init() {
	Register(driver.Builtin{
		Name:     "kfunc",
		Handler:  handleKfunc,
		Category: driver.CategoryJobs,
		Usage:    "kfunc acquire  |  kfunc release $fn  |  kfunc fire $fn N",
		Summary:  "Lease and trigger kmod-backed kernel-function slots for e2e tests.",
		Detail: "kfunc acquires one kernel-function slot exported by the " +
			"bpfman_e2e_targets module. The returned $fn exposes index, name, " +
			"trigger, and count. Use $fn.name as a fentry/fexit or kprobe/" +
			"kretprobe attach target, kfunc fire to invoke it, and kfunc " +
			"release to return the slot to the cross-process pool.",
	})
}

func handleKfunc(c driver.Ctx) (runtime.Value, error) {
	if len(c.Args) == 0 {
		return runtime.Value{}, fmt.Errorf("kfunc: subcommand required (valid: acquire, fire, release)")
	}
	sub := driver.ArgText(c.Args[0])
	rest := c.Args[1:]
	switch sub {
	case "acquire":
		return handleKfuncAcquire(c.Pos.Cite(), rest)
	case "release":
		return handleKfuncRelease(rest)
	case "fire":
		return handleKfuncFire(rest)
	default:
		return runtime.Value{}, fmt.Errorf("kfunc: unknown subcommand %q (valid: acquire, fire, release)", sub)
	}
}

func handleKfuncAcquire(origin string, args []runtime.Arg) (runtime.Value, error) {
	if len(args) != 0 {
		return runtime.Value{}, fmt.Errorf("kfunc acquire: takes no arguments")
	}
	kf, lease, err := acquireKfuncSlot(kfuncAcquireRequest{origin: origin})
	if err != nil {
		return runtime.Value{}, fmt.Errorf("kfunc acquire: %w", err)
	}

	rememberKfuncLease(kf, lease)
	return runtime.ValueFromKfunc(kf), nil
}

func handleKfuncRelease(args []runtime.Arg) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("kfunc release: requires exactly one $fn argument")
	}
	kf, err := kfuncFromArg(args[0])
	if err != nil {
		return runtime.Value{}, fmt.Errorf("kfunc release: %w", err)
	}

	if kf.MarkReleased() {
		return runtime.ValueFromEnvelope(runtime.OkEnvelope()), nil
	}
	if lease := takeKfuncLease(kf); lease != nil {
		if err := releaseKfuncSlot(lease, kf); err != nil {
			return runtime.Value{}, err
		}
	}
	return runtime.ValueFromEnvelope(runtime.OkEnvelope()), nil
}

func handleKfuncFire(args []runtime.Arg) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("kfunc fire: requires $fn and N")
	}
	kf, err := ensureKfunc(args[0])
	if err != nil {
		return runtime.Value{}, fmt.Errorf("kfunc fire: %w", err)
	}

	n, err := strconv.Atoi(driver.ArgText(args[1]))
	if err != nil {
		return runtime.Value{}, fmt.Errorf("kfunc fire: N: %w", err)
	}

	if n < 0 {
		return runtime.Value{}, fmt.Errorf("kfunc fire: N must not be negative (got %d)", n)
	}

	f, err := os.OpenFile(kf.Trigger, os.O_WRONLY, 0)
	if err != nil {
		return runtime.Value{}, fmt.Errorf("kfunc fire: open %s: %w", kf.Trigger, err)
	}
	defer f.Close()
	for i := range n {
		if _, err := f.Write([]byte{0}); err != nil {
			return runtime.Value{}, fmt.Errorf("kfunc fire: write %s (%d/%d): %w", kf.Trigger, i+1, n, err)
		}
	}
	return runtime.ValueFromEnvelope(runtime.OkEnvelope()), nil
}

func kfuncFromArg(a runtime.Arg) (*runtime.Kfunc, error) {
	sva, ok := a.(runtime.StructuredValueArg)
	if !ok {
		return nil, fmt.Errorf("expected a $fn argument, got %T", a)
	}
	if sva.Value.Kind() != semantics.OriginKfunc {
		return nil, fmt.Errorf("expected a $fn argument, got a %s value", sva.Value.Kind())
	}
	kf, ok := sva.Value.Origin().(*runtime.Kfunc)
	if !ok {
		return nil, fmt.Errorf("$fn has no underlying handle (got %T)", sva.Value.Origin())
	}
	return kf, nil
}

func ensureKfunc(a runtime.Arg) (*runtime.Kfunc, error) {
	kf, err := kfuncFromArg(a)
	if err != nil {
		return nil, err
	}
	if kf.IsReleased() {
		return nil, fmt.Errorf("$fn has been released; operational use of the handle is invalid after release")
	}
	return kf, nil
}

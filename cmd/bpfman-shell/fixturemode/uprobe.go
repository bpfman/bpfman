// Test-fixture mode for the uprobe family of e2e/scripts translations.
// When BPFMAN_SHELL_MODE=uprobe-fire-worker, bpfman-shell runs as
// a stable-PID worker that fires bpfman_shell_uprobe_call_malloc
// N times per wave, gated by numbered sentinel/ack files.
//
// The cgo'd target symbol is declared with noinline + optimize(0)
// so the compiler cannot reduce the body to nothing and inline
// every caller, and the body has an unelidable side effect
// (malloc + free) so there are real instructions for the kernel
// uprobe to fire on. The DSL script attaches uprobes to the same
// bpfman-shell binary at this symbol, then drives the wave
// protocol via the sentinel/ack files.
//
// One binary, multiple modes, with the fixture co-located with
// the runner so there is no separate helper binary on disk and
// no dependency on locating libc paths (which break on NixOS,
// Guix, musl, and other non-standard layouts).
//
//nolint:misspell // GCC spells the attribute name as optimize("O0").
package fixturemode

// #include <stdlib.h>
// __attribute__((noinline, optimize("O0")))
// void bpfman_shell_uprobe_call_malloc(void) {
//     volatile void *p = malloc(1);
//     free((void *)p);
// }
import "C"

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
)

// UprobeTargetSymbol names the cgo'd target the
// `bpfman_shell_uprobe_call_malloc` function above defines. The
// `uprobe-target` builtin publishes this string so attach-only
// tests (e.g. Test*_LinkRoundTrip.bpfman) reach the same symbol
// the `fire uprobe` workload driver uses, without taking a
// compile-time dep on this package.
const UprobeTargetSymbol = "bpfman_shell_uprobe_call_malloc"

// UprobeGoTargetSymbol names GoUprobeTarget's ELF symbol. Go
// symbols carry their package path, so the name contains dots
// (and slashes) -- the shape that real-world Go targets such as
// the operator's `main.getCount` present, and that bpffs rejects
// in pin names. Tests attach here to cover that shape.
const UprobeGoTargetSymbol = "github.com/bpfman/bpfman/cmd/bpfman-shell/fixturemode.GoUprobeTarget"

// goUprobeSink gives GoUprobeTarget's body an unelidable side
// effect so the call cannot be optimised away.
var goUprobeSink uint64

// GoUprobeTarget is the Go-symbol probe target, entered once per
// fixture event alongside the cgo target. It exists so tests can
// attach to a dotted, package-qualified symbol -- the shape real
// Go uprobe targets such as the operator's `main.getCount`
// present -- while keeping exact-count assertions sound. That
// second property is why it must be a nosplit leaf, and the
// reasoning deserves spelling out.
//
// Every ordinary Go function secretly starts with a check: "do I
// have enough stack space to run?". Usually yes, and it carries
// on. Occasionally -- more likely under load -- the answer is no.
// Go then grows the goroutine's stack and, crucially, re-executes
// the function from its first instruction. An entry uprobe is a
// breakpoint on that first instruction, so one logical call can
// trip it twice: once before the stack grows, once after the
// restart. The result is a rare one-extra-hit flake in
// exact-count tests (observed as got=78 want=65 -- six fires for
// five calls -- under the e2e script matrix). The cgo target
// above never has this problem because GCC-compiled C functions
// carry no such check, which is why the C-symbol tests have
// always been exact.
//
// `//go:nosplit` tells the compiler this function must never grow
// the stack, so the check -- and with it the restart path -- is
// omitted entirely. The body compiles to two instructions (bump a
// counter, return): one call is always exactly one probe hit.
// `//go:noinline` keeps the symbol's body the thing that actually
// executes; the sink increment stops the optimiser emptying it.
//
// If you ever point an exact-count test at a different Go symbol,
// verify it the same way this one was:
//
//	objdump -d --disassemble=<symbol> bin/bpfman-shell
//
// The first instruction must not be the stack-bound check
// (`cmp 0x10(%r14),%rsp` on amd64). Ordinary Go functions fail
// that test; cgo symbols and nosplit leaves pass it.
//
//go:noinline
//go:nosplit
func GoUprobeTarget() {
	goUprobeSink++
}

func init() {
	driver.RegisterFireKind("uprobe", driver.FireKind{
		Mode:        "uprobe-fire-worker",
		Summary:     "Fire uprobe target symbol bpfman_shell_uprobe_call_malloc.",
		NeedsBinary: true,
	})
}

// invokeUprobeCallMalloc calls the cgo'd target symbol once,
// firing whichever uprobe (or uretprobe) is attached to it.
func invokeUprobeCallMalloc() {
	C.bpfman_shell_uprobe_call_malloc()
}

// FireUprobeTarget fires both fixture targets n times in the
// current process: the cgo'd symbol and the Go-symbol leaf. The
// synchronous `uprobe fire` builtin uses this when a script wants
// the bpfman-shell process itself to be the uprobe workload,
// avoiding the sentinel/ack worker protocol.
func FireUprobeTarget(n int) {
	for i := 0; i < n; i++ {
		invokeUprobeCallMalloc()
		GoUprobeTarget()
	}
}

func runUprobeFireWorker(args []string) error {
	if len(args) != 4 {
		return fmt.Errorf("uprobe-fire-worker: usage: SENTINEL_PREFIX ACK_PREFIX N K (got %d args)", len(args))
	}
	sentinelPrefix := args[0]
	ackPrefix := args[1]
	n, err := strconv.Atoi(args[2])
	if err != nil {
		return fmt.Errorf("uprobe-fire-worker: invalid N %q: %w", args[2], err)
	}

	k, err := strconv.Atoi(args[3])
	if err != nil {
		return fmt.Errorf("uprobe-fire-worker: invalid K %q: %w", args[3], err)
	}

	for wave := 1; wave <= k; wave++ {
		sentinel := fmt.Sprintf("%s.%d", sentinelPrefix, wave)
		ack := fmt.Sprintf("%s.%d", ackPrefix, wave)
		for {
			if _, err := os.Stat(sentinel); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		FireUprobeTarget(n)
		f, err := os.OpenFile(ack, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("uprobe-fire-worker: create ack %s: %w", ack, err)
		}

		f.Close()
	}
	return nil
}

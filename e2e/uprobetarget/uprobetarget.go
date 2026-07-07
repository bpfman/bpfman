//go:build e2e

// Package uprobetarget owns the cgo'd userspace function the
// bpfman e2e suites attach uprobes (and uretprobes) to. The
// function is declared with the GCC `used`, `noinline`, and
// `optimize("O0")` attributes so the C compiler keeps real
// instructions in the emitted code and the linker keeps the
// symbol live in the importing binary's ELF table.
//
// Two binaries import this package: e2e.test (in-process e2e
// suite) and e2e-grpc.test (parallel gRPC suite). Both resolve
// attachments to the same symbol name. The bpfman-shell uprobe
// worker mode owns its own equivalent function in
// cmd/bpfman-shell/uprobe_helper.go -- that fixture is the
// stable-PID worker driving wave-based scripts, not just an
// attach target, so the two roles stay deliberately distinct.
//
// Importers should reference Invoke (e.g. via a package-level
// var) to keep the Go-side wrapper reachable; without that the
// Go linker can dead-code-eliminate it and take the C symbol
// with it.
package uprobetarget

// #include <stdlib.h>
// __attribute__((used, noinline, optimize("O0")))
// void e2e_uprobe_call_malloc(void) {
//     volatile void *p = malloc(1);
//     free((void *)p);
// }
import "C"

// Symbol is the ELF symbol name the cgo'd function above
// resolves to in any importing binary. uprobe attach specs
// pass this string as the function-name argument.
const Symbol = "e2e_uprobe_call_malloc"

// Invoke calls the cgo'd target once. Useful both for firing
// the symbol (e.g. driving wave tests from a helper mode) and
// as the Go-side reference that pins the wrapper against
// linker dead-code elimination.
func Invoke() { C.e2e_uprobe_call_malloc() }

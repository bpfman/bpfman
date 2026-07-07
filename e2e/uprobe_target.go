//go:build e2e

// This file is the e2e suite's edge of the shared
// uprobetarget package. The cgo'd C function and its symbol
// live in e2e/uprobetarget so the parallel-gRPC test binary
// (e2e-grpc.test) can import the same fixture without
// duplicating it. The wrapper here exists because
// invokeUprobeCallMalloc is the name TestMain's helper-mode
// dispatch wires up via BPFMAN_E2E_MODE=uprobe-trigger-call-malloc;
// keeping it as a Go function in the e2e package preserves
// that dispatch surface unchanged.

package e2e

import "github.com/bpfman/bpfman/e2e/uprobetarget"

// invokeUprobeCallMalloc calls the shared cgo'd target,
// firing whichever kernel uprobe (or uretprobe) is attached to
// uprobetarget.Symbol. Used by TestMain's helper-mode dispatch
// and by the unit-level workload driver.
func invokeUprobeCallMalloc() {
	uprobetarget.Invoke()
}

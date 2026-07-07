package kernel

import "time"

// ProgramStats contains runtime statistics for a BPF program. The kernel
// only accumulates these while statistics collection is enabled via the
// sysctl kernel.bpf_stats_enabled=1; with it disabled the fields stay
// zero.
//
// Requirements:
//   - Linux 5.8+ for Runtime/RunCount
//   - Linux 5.12+ for RecursionMisses
type ProgramStats struct {
	// Runtime is the total accumulated time the program has spent
	// executing.
	Runtime time.Duration `json:"runtime"`

	// RunCount is the total number of times the program has executed.
	RunCount uint64 `json:"run_count"`

	// RecursionMisses is the number of times the kernel skipped running
	// the program because it was already executing on the same CPU
	// (recursion guard).
	RecursionMisses uint64 `json:"recursion_misses"`
}

// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program fexit counter object. Three fexit programs share
// one object file; tests attach all three to do_unlinkat with
// staggered detach. Same coverage-matrix shape as the multi-fentry
// variant. fexit uses BPF tracing trampolines rather than perf
// links.

#include "counter_common.bpf.h"

volatile const __u32 expected_pid = 0;
volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;

COUNTER_MAP(mfx_a_count);
COUNTER_MAP(mfx_b_count);
COUNTER_MAP(mfx_c_count);

SEC("fexit/do_unlinkat")
COUNTER_PROG(mfx_a, mfx_a_count, weight_a)

SEC("fexit/do_unlinkat")
COUNTER_PROG(mfx_b, mfx_b_count, weight_b)

SEC("fexit/do_unlinkat")
COUNTER_PROG(mfx_c, mfx_c_count, weight_c)

char _license[] SEC("license") = "Dual BSD/GPL";

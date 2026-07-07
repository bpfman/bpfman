// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program fentry counter object. Three fentry programs share
// one object file; tests attach all three to do_unlinkat with
// staggered detach. fentry programs use BPF tracing trampolines
// rather than perf links, so this is a coverage-matrix entry rather
// than a regression test for the same bug as the kprobe / uprobe
// variants.

#include "counter_common.bpf.h"

volatile const __u32 expected_pid = 0;
volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;

COUNTER_MAP(mfe_a_count);
COUNTER_MAP(mfe_b_count);
COUNTER_MAP(mfe_c_count);

SEC("fentry/do_unlinkat")
COUNTER_PROG(mfe_a, mfe_a_count, weight_a)

SEC("fentry/do_unlinkat")
COUNTER_PROG(mfe_b, mfe_b_count, weight_b)

SEC("fentry/do_unlinkat")
COUNTER_PROG(mfe_c, mfe_c_count, weight_c)

char _license[] SEC("license") = "Dual BSD/GPL";

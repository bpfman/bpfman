// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program uprobe counter object. Three uprobe programs share
// one object file; tests attach all three to the same userspace
// symbol (e2e_uprobe_call_malloc inside the e2e.test binary) with
// staggered detach to exercise same-hook multiplicity for uprobe
// specifically. See counter_common.bpf.h for the shared shape.

#include "counter_common.bpf.h"

volatile const __u32 expected_pid = 0;
volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;

COUNTER_MAP(mup_a_count);
COUNTER_MAP(mup_b_count);
COUNTER_MAP(mup_c_count);

SEC("uprobe/mup_a")
COUNTER_PROG(mup_a, mup_a_count, weight_a)

SEC("uprobe/mup_b")
COUNTER_PROG(mup_b, mup_b_count, weight_b)

SEC("uprobe/mup_c")
COUNTER_PROG(mup_c, mup_c_count, weight_c)

char _license[] SEC("license") = "Dual BSD/GPL";

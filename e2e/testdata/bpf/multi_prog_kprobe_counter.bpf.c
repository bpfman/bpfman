// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program kprobe counter object. Three kprobe programs share
// one object file; tests attach all three to the same kernel
// function (do_unlinkat) with staggered detach to exercise
// same-hook multiplicity for kprobe specifically. Each program owns
// a private weight global and a private LIBBPF_PIN_NONE 1-entry
// counter map; tests assert events * weight per program after each
// wave, so a still-firing detached program produces wrong
// arithmetic. See counter_common.bpf.h.

#include "counter_common.bpf.h"

volatile const __u32 expected_pid = 0;
volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;

COUNTER_MAP(mkp_a_count);
COUNTER_MAP(mkp_b_count);
COUNTER_MAP(mkp_c_count);

SEC("kprobe/mkp_a")
COUNTER_PROG(mkp_a, mkp_a_count, weight_a)

SEC("kprobe/mkp_b")
COUNTER_PROG(mkp_b, mkp_b_count, weight_b)

SEC("kprobe/mkp_c")
COUNTER_PROG(mkp_c, mkp_c_count, weight_c)

char _license[] SEC("license") = "Dual BSD/GPL";

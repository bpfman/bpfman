// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program tracepoint counter object for exercising the variadic
// `--programs` form of `bpfman program load file`.
//
// Three tracepoint programs share one object file. Each owns its own
// 1-entry counter map (LIBBPF_PIN_NONE; tests read by map ID).
//
// Each program filters on `expected_pid` (a load-time global) and
// increments its counter by its own per-program `weight_X` global on
// every matching event. Tests pass test-unique weights so the final
// counter value is a verifiable function of (burst count x weight),
// not just an event tally. This catches "wrong map", "globals not
// applied", and cross-test interference in ways a bare counter
// cannot.

#include "counter_common.bpf.h"

volatile const __u32 expected_pid = 0;
volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;

COUNTER_MAP(tp_a_count);
COUNTER_MAP(tp_b_count);
COUNTER_MAP(tp_c_count);

SEC("tracepoint/tp_a")
COUNTER_PROG(tp_a, tp_a_count, weight_a)

SEC("tracepoint/tp_b")
COUNTER_PROG(tp_b, tp_b_count, weight_b)

SEC("tracepoint/tp_c")
COUNTER_PROG(tp_c, tp_c_count, weight_c)

char _license[] SEC("license") = "Dual BSD/GPL";

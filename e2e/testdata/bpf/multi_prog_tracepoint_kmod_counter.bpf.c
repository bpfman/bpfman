// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program tracepoint counter object for the e2e kmod's
// bpfman_e2e/bpfman_e2e_ping event. Three tracepoint programs share
// one object file and filter on the leased slot index carried in the
// event payload.

#include "counter_common.bpf.h"

volatile const __u32 expected_slot = 0;
volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;

COUNTER_MAP(tp_a_count);
COUNTER_MAP(tp_b_count);
COUNTER_MAP(tp_c_count);

SEC("tracepoint/tp_a")
TRACEPOINT_SLOT_COUNTER_PROG(tp_a, tp_a_count, weight_a)

SEC("tracepoint/tp_b")
TRACEPOINT_SLOT_COUNTER_PROG(tp_b, tp_b_count, weight_b)

SEC("tracepoint/tp_c")
TRACEPOINT_SLOT_COUNTER_PROG(tp_c, tp_c_count, weight_c)

char _license[] SEC("license") = "Dual BSD/GPL";

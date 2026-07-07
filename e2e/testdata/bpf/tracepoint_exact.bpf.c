// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Single-program tracepoint counter for exact-equality e2e
// assertions. Filters on `expected_pid` and adds `weight` to a
// LIBBPF_PIN_NONE 1-entry array on every matching event. Tests
// drive the workload subprocess for a known number of events and
// assert `events * weight == count`. See counter_common.bpf.h for
// the shared building blocks.

#include "counter_common.bpf.h"

volatile const __u32 expected_pid = 0;
volatile const __u64 weight = 0;

COUNTER_MAP(tp_count);

SEC("tracepoint/tracepoint_kill_recorder")
COUNTER_PROG(tracepoint_kill_recorder, tp_count, weight)

char _license[] SEC("license") = "Dual BSD/GPL";

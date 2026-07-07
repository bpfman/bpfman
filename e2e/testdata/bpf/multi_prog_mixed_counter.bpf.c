// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Mixed-type multi-program counter object for exercising the variadic
// `--programs` form of `bpfman program load file` with heterogeneous
// program types (tracepoint, kprobe, kretprobe).
//
// Each program filters on `expected_pid` (a load-time global) and
// increments its counter by its own per-program `weight_X` global on
// every matching event. Tests pass test-unique weights so the final
// counter value is a verifiable function of (events x weight), not
// just an event tally.

#include "counter_common.bpf.h"

volatile const __u32 expected_pid = 0;
volatile const __u64 weight_tp = 0;
volatile const __u64 weight_kp = 0;
volatile const __u64 weight_krp = 0;

COUNTER_MAP(mtp_count);
COUNTER_MAP(mkp_count);
COUNTER_MAP(mkrp_count);

SEC("tracepoint/mixed_tp")
COUNTER_PROG(mixed_tp, mtp_count, weight_tp)

SEC("kprobe/mixed_kp")
COUNTER_PROG(mixed_kp, mkp_count, weight_kp)

SEC("kretprobe/mixed_krp")
COUNTER_PROG(mixed_krp, mkrp_count, weight_krp)

char _license[] SEC("license") = "Dual BSD/GPL";

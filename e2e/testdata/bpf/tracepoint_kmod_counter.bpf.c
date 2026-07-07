// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Tracepoint counter for scripts that attach to the e2e kmod's
// bpfman_e2e/bpfman_e2e_ping event. The tracepoint is shared across
// all tests, so the program filters on the leased slot index carried
// in the event payload.

#include "counter_common.bpf.h"

volatile const __u32 expected_slot = 0;
volatile const __u64 weight = 0;

COUNTER_MAP(tp_kmod_count);

SEC("tracepoint/tracepoint_kmod_recorder")
TRACEPOINT_SLOT_COUNTER_PROG(tracepoint_kmod_recorder, tp_kmod_count, weight)

char _license[] SEC("license") = "Dual BSD/GPL";

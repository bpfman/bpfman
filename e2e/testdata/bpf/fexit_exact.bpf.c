// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Single-program fexit counter for exact-equality e2e assertions.
// Attaches at the return of do_unlinkat, filters on `expected_pid`,
// and adds `weight` to a LIBBPF_PIN_NONE 1-entry array on every
// matching event. See counter_common.bpf.h.

#include "counter_common.bpf.h"

volatile const __u32 expected_pid = 0;
volatile const __u64 weight = 0;

COUNTER_MAP(fx_count);

SEC("fexit/do_unlinkat")
COUNTER_PROG(test_fexit, fx_count, weight)

char _license[] SEC("license") = "Dual BSD/GPL";

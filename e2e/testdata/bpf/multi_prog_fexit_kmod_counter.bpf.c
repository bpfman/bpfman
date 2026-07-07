// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program fexit counter object for the kmod-targeting e2e
// tests. Same shape as multi_prog_fentry_kmod_counter.bpf.c, but
// attaches at function return.

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

#include "counter_common.bpf.h"

volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;

COUNTER_MAP(mfx_a_count);
COUNTER_MAP(mfx_b_count);
COUNTER_MAP(mfx_c_count);

SEC("fexit/bpfman_e2e_target_0")
int mfx_a(void *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&mfx_a_count, &key);
  if (val)
    __sync_fetch_and_add(val, weight_a);
  return 0;
}

SEC("fexit/bpfman_e2e_target_0")
int mfx_b(void *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&mfx_b_count, &key);
  if (val)
    __sync_fetch_and_add(val, weight_b);
  return 0;
}

SEC("fexit/bpfman_e2e_target_0")
int mfx_c(void *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&mfx_c_count, &key);
  if (val)
    __sync_fetch_and_add(val, weight_c);
  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";

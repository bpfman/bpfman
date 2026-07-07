// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program kprobe/kretprobe counter object for kmod-targeting
// e2e tests. Three programs share one object file and attach to the
// same leased bpfman_e2e_targets slot. Each program owns a private
// weight global and a private LIBBPF_PIN_NONE 1-entry counter map. No
// PID filter is needed because the leased slot's only trigger is the
// test that owns it.

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

#include "counter_common.bpf.h"

volatile const __u64 weight_a = 0;
volatile const __u64 weight_b = 0;
volatile const __u64 weight_c = 0;

COUNTER_MAP(mkp_a_count);
COUNTER_MAP(mkp_b_count);
COUNTER_MAP(mkp_c_count);

SEC("kprobe/mkp_a")
int mkp_a(void *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&mkp_a_count, &key);
  if (val)
    __sync_fetch_and_add(val, weight_a);
  return 0;
}

SEC("kprobe/mkp_b")
int mkp_b(void *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&mkp_b_count, &key);
  if (val)
    __sync_fetch_and_add(val, weight_b);
  return 0;
}

SEC("kprobe/mkp_c")
int mkp_c(void *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&mkp_c_count, &key);
  if (val)
    __sync_fetch_and_add(val, weight_c);
  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";

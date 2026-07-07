// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Single-program fentry counter for the kmod-targeting e2e tests.
// Symmetric with fexit_kmod_exact.bpf.c, attaching at function
// entry instead of return.

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

volatile const __u64 weight = 0;

struct {
  __uint(type, BPF_MAP_TYPE_ARRAY);
  __type(key, __u32);
  __type(value, __u64);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_NONE);
} fe_count SEC(".maps");

SEC("fentry/bpfman_e2e_target_0")
int test_fentry(void *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&fe_count, &key);
  if (val)
    __sync_fetch_and_add(val, weight);
  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";

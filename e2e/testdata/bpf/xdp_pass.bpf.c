// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// XDP pass program with global data and a counter map for e2e testing.
// Returns XDP_PASS on every packet and increments a per-CPU counter.
// The global variables config_u8 and config_u32 are used to test
// global data round-tripping.
//
// Adapted from:
// https://github.com/bpfman/bpfman/tree/main/tests/integration-test/bpf

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

volatile const __u8 config_u8 = 0;
volatile const __u32 config_u32 = 0;

struct {
  __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
  __type(key, __u32);
  __type(value, __u64);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_NONE);
} xdp_pass_stats_map SEC(".maps");

SEC("xdp")
int pass(struct xdp_md *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&xdp_pass_stats_map, &key);
  if (val)
    (*val)++;
  return XDP_PASS;
}

char _license[] SEC("license") = "Dual BSD/GPL";

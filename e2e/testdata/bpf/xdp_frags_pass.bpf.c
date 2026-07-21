// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// An XDP program in the "xdp.frags" section, so it is compiled with
// BPF_F_XDP_HAS_FRAGS. It counts packets into a per-CPU map so an e2e
// test can drive traffic and prove the program runs on the packet path.
//
// Loading it exercises the extension conversion path: bpfman loads XDP
// programs as BPF_PROG_TYPE_EXT against a dispatcher, and must reset the
// section-derived attach type, or the load is refused with
// "unrecognized attach type".

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

struct {
  __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
  __type(key, __u32);
  __type(value, __u64);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_NONE);
} frags_pass_stats_map SEC(".maps");

SEC("xdp.frags")
int frags_pass(struct xdp_md *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&frags_pass_stats_map, &key);
  if (val)
    (*val)++;
  return XDP_PASS;
}

char _license[] SEC("license") = "Dual BSD/GPL";

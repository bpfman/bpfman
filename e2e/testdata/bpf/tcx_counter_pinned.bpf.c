// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// TCX counter program for e2e testing.
// Counts packets and returns TC_ACT_OK (TCX_NEXT equivalent).
//
// Adapted from:
// https://github.com/bpfman/bpfman/tree/main/examples/go-tcx-counter/bpf

#include <linux/bpf.h>
#include <linux/pkt_cls.h>

#include <bpf/bpf_helpers.h>

struct {
  __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
  __type(key, __u32);
  __type(value, __u64);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_BY_NAME);
} tcx_stats_map SEC(".maps");

SEC("classifier/tcx_stats")
int tcx_stats(struct __sk_buff *skb) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&tcx_stats_map, &key);
  if (val)
    (*val)++;
  return TC_ACT_OK;
}

char _license[] SEC("license") = "Dual BSD/GPL";

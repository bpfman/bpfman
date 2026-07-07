// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Tracepoint counter program for e2e testing -- pinned variant.
// Map is LIBBPF_PIN_BY_NAME so consumers such as the examples
// can read the pinned map by path. The bare
// `tracepoint_counter.bpf.c` sibling is a copy with LIBBPF_PIN_NONE
// (the libbpf default) for tests that read the counter by kernel
// map ID.
//
// Adapted from:
// https://github.com/bpfman/bpfman/tree/main/examples/go-tracepoint-counter/bpf

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

struct {
  __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
  __type(key, __u32);
  __type(value, __u64);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_BY_NAME);
} tracepoint_stats_map SEC(".maps");

SEC("tracepoint/tracepoint_kill_recorder")
int tracepoint_kill_recorder(void *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&tracepoint_stats_map, &key);
  if (val)
    (*val)++;
  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";

// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Tracepoint counter program for e2e testing.
// Increments a per-CPU counter when sys_enter_kill fires.
//
// Map pinning: LIBBPF_PIN_NONE (the libbpf default). Each loaded
// copy of this object owns a private map; tests that need
// counter-delta assertions read by kernel ID. The
// `tracepoint_counter_pinned.bpf.c` sibling is a copy with
// LIBBPF_PIN_BY_NAME for consumers such as the examples that
// depend on the pinned map path.
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
  __uint(pinning, LIBBPF_PIN_NONE);
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

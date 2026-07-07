// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Uprobe counter program for e2e testing.
// Increments a per-CPU counter on every uprobe hit.
//
// Adapted from:
// https://github.com/bpfman/bpfman/tree/main/examples/go-uprobe-counter/bpf

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

struct {
  __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
  __type(key, __u32);
  __type(value, __u64);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_NONE);
} uprobe_stats_map SEC(".maps");

SEC("uprobe/uprobe_counter")
int uprobe_counter(void *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&uprobe_stats_map, &key);
  if (val)
    (*val)++;
  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";

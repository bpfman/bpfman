// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Kprobe counter program for e2e testing.
// Increments a per-CPU counter on every kprobe hit.
//
// Adapted from:
// https://github.com/bpfman/bpfman/tree/main/examples/go-kprobe-counter/bpf

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

struct {
  __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
  __type(key, __u32);
  __type(value, __u64);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_NONE);
} kprobe_stats_map SEC(".maps");

SEC("kprobe/kprobe_counter")
int kprobe_counter(void *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&kprobe_stats_map, &key);
  if (val)
    (*val)++;
  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";

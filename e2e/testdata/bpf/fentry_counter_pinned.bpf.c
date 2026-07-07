// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Fentry/fexit counter program for e2e testing.
// Provides both fentry and fexit sections targeting do_unlinkat.
// Key 0 counts fentry invocations, key 1 counts fexit invocations.

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

struct {
  __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
  __type(key, __u32);
  __type(value, __u64);
  __uint(max_entries, 2);
  __uint(pinning, LIBBPF_PIN_BY_NAME);
} fentry_stats_map SEC(".maps");

SEC("fentry/do_unlinkat")
int BPF_PROG(test_fentry) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&fentry_stats_map, &key);
  if (val)
    (*val)++;
  return 0;
}

SEC("fexit/do_unlinkat")
int BPF_PROG(test_fexit) {
  __u32 key = 1;
  __u64 *val = bpf_map_lookup_elem(&fentry_stats_map, &key);
  if (val)
    (*val)++;
  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";

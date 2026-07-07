// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Multi-program kprobe object whose two programs share ONE
// LIBBPF_PIN_BY_NAME map. This is the case that distinguishes genuine
// map sharing (which must survive) from the gRPC server's count-based
// forced sharing (which fabricates ownership for private maps). Both
// programs reference the same pinned map, so a correct loader gives
// them the same kernel map -- without any map-owner relationship.

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

struct {
  __uint(type, BPF_MAP_TYPE_ARRAY);
  __type(key, __u32);
  __type(value, __u64);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_BY_NAME);
} shared_kprobe_map SEC(".maps");

SEC("kprobe/mkp_x")
int mkp_x(void *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&shared_kprobe_map, &key);
  if (val)
    (*val)++;
  return 0;
}

SEC("kprobe/mkp_y")
int mkp_y(void *ctx) {
  __u32 key = 0;
  __u64 *val = bpf_map_lookup_elem(&shared_kprobe_map, &key);
  if (val)
    (*val)++;
  return 0;
}

char _license[] SEC("license") = "Dual BSD/GPL";

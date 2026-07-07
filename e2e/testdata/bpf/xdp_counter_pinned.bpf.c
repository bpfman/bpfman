// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

// Local copy of the go-xdp-counter BPF program for e2e testing.
// Identical map layout to quay.io/bpfman-bytecode/go-xdp-counter so
// that tests reading xdp_stats_map work unchanged.
//
// Derived from:
// https://github.com/xdp-project/xdp-tutorial/tree/master/basic03-map-counter

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

#define XDP_ACTION_MAX (XDP_REDIRECT + 1)

struct datarec {
  __u64 rx_packets;
  __u64 rx_bytes;
} datarec;

struct {
  __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
  __type(key, __u32);
  __type(value, datarec);
  __uint(max_entries, XDP_ACTION_MAX);
  __uint(pinning, LIBBPF_PIN_BY_NAME);
} xdp_stats_map SEC(".maps");

static __always_inline __u32 xdp_stats_record_action(struct xdp_md *ctx,
                                                     __u32 action) {
  void *data_end = (void *)(long)ctx->data_end;
  void *data = (void *)(long)ctx->data;

  if (action >= XDP_ACTION_MAX)
    return XDP_ABORTED;

  struct datarec *rec = bpf_map_lookup_elem(&xdp_stats_map, &action);
  if (!rec)
    return XDP_ABORTED;

  __u64 bytes = data_end - data;

  rec->rx_packets++;
  rec->rx_bytes += bytes;

  return action;
}

SEC("xdp")
int xdp_stats(struct xdp_md *ctx) {
  __u32 action = XDP_PASS;

  return xdp_stats_record_action(ctx, action);
}

char _license[] SEC("license") = "Dual BSD/GPL";

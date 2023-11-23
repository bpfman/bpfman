// SPDX-License-Identifier: GPL-2.0-only
// Modifications Copyright Authors of bpfman

// Derived from:
// https://github.com/xdp-project/xdp-tutorial/tree/master/basic03-map-counter

#include <linux/bpf.h>

#include <bpf/bpf_helpers.h>

#define XDP_ACTION_MAX (XDP_REDIRECT + 1)

/* This is the data record stored in the map */
struct datarec {
  __u64 rx_packets;
  __u64 rx_bytes;
} datarec;

/* Lesson#1: See how a map is defined.
 * - Here an array with XDP_ACTION_MAX (max_)entries are created.
 * - The idea is to keep stats per (enum) xdp_action
 */
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

  /* Lookup in kernel BPF-side return pointer to actual data record */
  struct datarec *rec = bpf_map_lookup_elem(&xdp_stats_map, &action);
  if (!rec)
    return XDP_ABORTED;

  /* Calculate packet length */
  __u64 bytes = data_end - data;

  /* BPF_MAP_TYPE_PERCPU_ARRAY returns a data record specific to current
   * CPU and XDP hooks runs under Softirq, which makes it safe to update
   * without atomic operations.
   */
  rec->rx_packets++;
  rec->rx_bytes += bytes;

  return action;
}

SEC("xdp")
int xdp_stats(struct xdp_md *ctx) {
  __u32 action = XDP_PASS; /* XDP_PASS = 2 */

  return xdp_stats_record_action(ctx, action);
}

char _license[] SEC("license") = "Dual BSD/GPL";

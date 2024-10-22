// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright Authors of bpfman

#include <linux/bpf.h>
#include <linux/pkt_cls.h>
#include <linux/types.h>

#include <bpf/bpf_helpers.h>

/* This is the data record stored in the map */
struct datarec {
  __u64 rx_packets;
  __u64 rx_bytes;
} datarec;

struct {
  __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
  __type(key, __u32);
  __type(value, datarec);
  __uint(max_entries, 1);
  __uint(pinning, LIBBPF_PIN_BY_NAME);
} tcx_stats_map SEC(".maps");

void tcx_stats_record(struct __sk_buff *skb) {
  __u32 map_key = 0;

  void *data = (void *)(long)skb->data;
  void *data_end = (void *)(long)skb->data_end;

  if (data_end < data)
    return;

  /* Lookup in kernel BPF-side return pointer to actual data record */
  struct datarec *rec = bpf_map_lookup_elem(&tcx_stats_map, &map_key);

  if (!rec)
    return;

  /* Calculate packet length */
  __u64 bytes = data_end - data;

  rec->rx_packets++;
  rec->rx_bytes += bytes;
}

SEC("classifier/tcx_stats")
int tcx_stats(struct __sk_buff *skb) {
  tcx_stats_record(skb);
  return TCX_NEXT;
}
